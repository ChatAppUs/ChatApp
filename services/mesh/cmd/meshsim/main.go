// meshsim — large-scale offline-mesh sweep (500 / 5,000 / 50,000 devices).
//
// Builds a 2-D grid of simulated devices on the in-memory SimBus, seeds each
// node with its wired neighbours, then delivers a message between opposite
// corners using the production engine code (routing, store-and-forward,
// dedup, relay quotas). Reports delivery, hop counts, dedup drops and wall
// time. Run with -devices to size the sweep:
//
//	go run ./cmd/meshsim -devices 500
//	go run ./cmd/meshsim -devices 5000
//	go run ./cmd/meshsim -devices 50000
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	mesh "github.com/chatappus/chatapp/services/mesh"
)

func main() {
	devices := flag.Int("devices", 500, "number of devices in the simulated grid")
	maxHopsFlag := flag.Int("maxhops", 0, "override the packet TTL (0 = auto)")
	flag.Parse()

	side := 1
	for side*side < *devices {
		side++
	}
	total := side * side
	maxHops := *maxHopsFlag
	if maxHops <= 0 {
		maxHops = mesh.HopsForDevices(total)
	}

	bus := mesh.NewSimBus()
	nodes := make([]*mesh.Node, total)
	addr := func(i int) string { return fmt.Sprintf("sim://d%d", i) }

	key := mesh.MustKey()
	for i := 0; i < total; i++ {
		kind := "relay"
		if i == 0 || i == total-1 {
			kind = "member"
		}
		nodes[i] = mesh.NewNode(mesh.NodeConfig{
			DeviceID:  fmt.Sprintf("d%d", i),
			Key:       key,
			Kind:      kind,
			Transport: bus.Attach(addr(i), nil),
			MaxHops:   maxHops,
		})
		// Seed each node with its wired neighbours (no beacon timing).
		neighbours := make([]*mesh.Beacon, 0, 4)
		r, c := i/side, i%side
		kindOf := func(j int) string {
			if j == 0 || j == total-1 {
				return "member"
			}
			return "relay"
		}
		if c+1 < side {
			neighbours = append(neighbours, &mesh.Beacon{DeviceID: fmt.Sprintf("d%d", i+1), Kind: kindOf(i + 1), Transport: "local_wifi", Addr: addr(i + 1)})
		}
		if c > 0 {
			neighbours = append(neighbours, &mesh.Beacon{DeviceID: fmt.Sprintf("d%d", i-1), Kind: kindOf(i - 1), Transport: "local_wifi", Addr: addr(i - 1)})
		}
		if r+1 < side {
			neighbours = append(neighbours, &mesh.Beacon{DeviceID: fmt.Sprintf("d%d", i+side), Kind: kindOf(i + side), Transport: "local_wifi", Addr: addr(i + side)})
		}
		if r > 0 {
			neighbours = append(neighbours, &mesh.Beacon{DeviceID: fmt.Sprintf("d%d", i-side), Kind: kindOf(i - side), Transport: "local_wifi", Addr: addr(i - side)})
		}
		for _, b := range neighbours {
			nodes[i].Routes().Upsert(b)
		}
	}
	// Wire the physical edges after the routes are seeded.
	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			i := r*side + c
			if c+1 < side {
				bus.Wire(addr(i), addr(i+1))
				bus.Wire(addr(i+1), addr(i))
			}
			if r+1 < side {
				bus.Wire(addr(i), addr(i+side))
				bus.Wire(addr(i+side), addr(i))
			}
		}
	}

	got := make(chan *mesh.Packet, 1)
	dst := total - 1
	nodes[dst].SetHandler(func(p *mesh.Packet, _ []byte) { got <- p })

	start := time.Now()
	id, err := nodes[0].Send(mesh.KindMessage, fmt.Sprintf("d%d", dst), []byte("sweep"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "send failed:", err)
		os.Exit(1)
	}
	// Progress probe: how far the flood has spread.
	go func() {
		for range time.Tick(2 * time.Second) {
			reached := 0
			for _, n := range nodes {
				if n.Routes().Knows(id) {
					reached++
				}
			}
			fmt.Printf("progress reached=%d/%d\n", reached, total)
		}
	}()
	select {
	case p := <-got:
		fmt.Printf("devices=%d side=%d maxHops=%d delivered=true hops=%d ttl_left=%d wall=%s drops=%d id=%s\n",
			total, side, maxHops, p.Hops, p.TTL, time.Since(start).Round(time.Millisecond), bus.Stats(), id)
	case <-time.After(60 * time.Second):
		fmt.Printf("devices=%d side=%d maxHops=%d delivered=false wall=%s drops=%d id=%s\n",
			total, side, maxHops, time.Since(start).Round(time.Millisecond), bus.Stats(), id)
		os.Exit(1)
	}

	// Deterministic shutdown.
	for _, n := range nodes {
		n.Stop()
	}
}
