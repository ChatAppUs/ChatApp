package mesh

import (
	"testing"
	"time"
)

// simKey is the shared mesh identity key. The engine's current security
// model is a shared static key across the mesh (per-device key exchange is
// documented as remaining work), so every simulated node holds the same key.
var simKey = func() *IdentityKey {
	k, err := NewIdentityKey()
	if err != nil {
		panic(err)
	}
	return &k
}()

// newSimNode builds a node on the simulation bus with a deterministic address.
func newSimNode(t *testing.T, bus *SimBus, id string, kind string, handler Handler, maxHops int) *Node {
	t.Helper()
	addr := "sim://" + id
	link := bus.Attach(addr, nil)
	n := NewNode(NodeConfig{
		DeviceID:  id,
		Key:       simKey,
		Kind:      kind,
		Transport: link,
		Handler:   handler,
		MaxHops:   maxHops,
	})
	// Re-attach the inbound callback now that the node exists.
	link.SetInbound(n.HandleInbound)
	return n
}

// wireChain wires a linear chain n0-n1-...-nN on the bus.
func wireChain(bus *SimBus, addrs []string) {
	for i := 0; i+1 < len(addrs); i++ {
		bus.Wire(addrs[i], addrs[i+1])
		bus.Wire(addrs[i+1], addrs[i])
	}
}

func simAddrs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "sim://d" + itoa(i)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

// TestSimChainMultiHopDelivery verifies end-to-end delivery across a chain of
// relay-consenting intermediaries using only the production engine code.
func TestSimChainMultiHopDelivery(t *testing.T) {
	bus := NewSimBus()
	const end = 6
	addrs := simAddrs(end + 1)
	got := make(chan string, 1)
	nodes := make([]*Node, end+1)
	for i := 0; i <= end; i++ {
		kind := "relay"
		if i == 0 || i == end {
			kind = "member"
			if i == end {
				h := func(p *Packet, pt []byte) {
					got <- string(pt)
				}
				nodes[i] = newSimNode(t, bus, "d"+itoa(i), kind, h, HopsForDevices(end+1))
				continue
			}
		}
		nodes[i] = newSimNode(t, bus, "d"+itoa(i), kind, nil, HopsForDevices(end+1))
	}
	wireChain(bus, addrs)
	// Seed neighbour knowledge directly (no beacon timing in the sim).
	for i := 0; i < end; i++ {
		a, bnode := nodes[i], nodes[i+1]
		a.routes.Upsert(&Beacon{DeviceID: bnode.DeviceID, Kind: bnode.Kind, Transport: "local_wifi", Addr: addrs[i+1]})
		bnode.routes.Upsert(&Beacon{DeviceID: a.DeviceID, Kind: a.Kind, Transport: "local_wifi", Addr: addrs[i]})
	}
	id, err := nodes[0].Send(KindMessage, nodes[end].DeviceID, []byte("hello across the chain"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if msg != "hello across the chain" {
			t.Fatalf("wrong payload %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("packet %s never delivered across %d hops", id, end)
	}
	for _, n := range nodes {
		n.Stop()
	}
}

// TestSimPartitionAndHeal verifies store-and-forward across a partition and
// delivery after the link is restored (partition healing).
func TestSimPartitionAndHeal(t *testing.T) {
	bus := NewSimBus()
	addrs := simAddrs(3)
	got := make(chan string, 1)
	a := newSimNode(t, bus, "d0", "member", nil, 16)
	m := newSimNode(t, bus, "d1", "relay", nil, 16)
	b := newSimNode(t, bus, "d2", "member", func(p *Packet, pt []byte) { got <- string(pt) }, 16)
	wireChain(bus, addrs)
	a.routes.Upsert(&Beacon{DeviceID: m.DeviceID, Kind: "relay", Transport: "local_wifi", Addr: addrs[1]})
	m.routes.Upsert(&Beacon{DeviceID: a.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[0]})
	m.routes.Upsert(&Beacon{DeviceID: b.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[2]})
	b.routes.Upsert(&Beacon{DeviceID: m.DeviceID, Kind: "relay", Transport: "local_wifi", Addr: addrs[1]})

	// Partition: a—m.
	bus.Unwire(addrs[0], addrs[1])
	bus.Unwire(addrs[1], addrs[0])
	if _, err := a.Send(KindMessage, b.DeviceID, []byte("while partitioned")); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		t.Fatalf("delivered despite partition: %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
	// Heal. On real radios the first beacon that crosses the restored
	// link re-announces the peer; deliver m's beacon to a to simulate it.
	bus.Wire(addrs[0], addrs[1])
	bus.Wire(addrs[1], addrs[0])
	beacon, err := MarshalBeacon(&Beacon{DeviceID: m.DeviceID, Kind: "relay", Transport: "local_wifi", Addr: addrs[1], Seq: 2})
	if err != nil {
		t.Fatal(err)
	}
	a.HandleInbound(addrs[1], beacon)
	select {
	case msg := <-got:
		if msg != "while partitioned" {
			t.Fatalf("wrong payload %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("queued packet not delivered after heal")
	}
	for _, n := range []*Node{a, m, b} {
		n.Stop()
	}
}

// TestSimLargeGridDelivery exercises a 2-D grid (simulating hundreds of
// devices) and verifies delivery between opposite corners.
func TestSimLargeGridDelivery(t *testing.T) {
	const side = 10 // 100 devices; CI-sized slice of the 500/5k/50k sweep in sim/cmd
	bus := NewSimBus()
	nodes := make(map[int]*Node)
	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			id := "d" + itoa(r*side+c)
			kind := "relay"
			if (r == 0 && c == 0) || (r == side-1 && c == side-1) {
				kind = "member"
			}
			nodes[r*side+c] = newSimNode(t, bus, id, kind, nil, HopsForDevices(side*side))
		}
	}
	addr := func(r, c int) string { return "sim://d" + itoa(r*side+c) }
	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			if c+1 < side {
				bus.Wire(addr(r, c), addr(r, c+1))
				bus.Wire(addr(r, c+1), addr(r, c))
			}
			if r+1 < side {
				bus.Wire(addr(r, c), addr(r+1, c))
				bus.Wire(addr(r+1, c), addr(r, c))
			}
		}
	}
	// Seed each node with its wired neighbours, using each neighbour's real
	// kind so relay consent matches the topology (corners are members).
	kindOf := func(r, c int) string {
		if (r == 0 && c == 0) || (r == side-1 && c == side-1) {
			return "member"
		}
		return "relay"
	}
	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			n := nodes[r*side+c]
			if c+1 < side {
				n.routes.Upsert(&Beacon{DeviceID: "d" + itoa(r*side+c+1), Kind: kindOf(r, c+1), Transport: "local_wifi", Addr: addr(r, c+1)})
			}
			if c > 0 {
				n.routes.Upsert(&Beacon{DeviceID: "d" + itoa(r*side+c-1), Kind: kindOf(r, c-1), Transport: "local_wifi", Addr: addr(r, c-1)})
			}
			if r+1 < side {
				n.routes.Upsert(&Beacon{DeviceID: "d" + itoa((r+1)*side+c), Kind: kindOf(r+1, c), Transport: "local_wifi", Addr: addr(r+1, c)})
			}
			if r > 0 {
				n.routes.Upsert(&Beacon{DeviceID: "d" + itoa((r-1)*side+c), Kind: kindOf(r-1, c), Transport: "local_wifi", Addr: addr(r-1, c)})
			}
		}
	}
	got := make(chan string, 1)
	corner := nodes[(side-1)*side+(side-1)]
	corner.handler = func(p *Packet, pt []byte) { got <- string(pt) }
	if _, err := nodes[0].Send(KindMessage, corner.DeviceID, []byte("corner to corner")); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if msg != "corner to corner" {
			t.Fatalf("wrong payload %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("grid delivery failed across %d devices", side*side)
	}
	if dropped := bus.Stats(); dropped > 0 {
		t.Fatalf("unexpected drops: %d", dropped)
	}
	for _, n := range nodes {
		n.Stop()
	}
}
