// ChatApp load-test harness.
//
// A real concurrent load generator for the API. It exercises the public and
// authenticated surface with a configurable concurrency and duration, and
// reports throughput (req/s) plus p50/p95/p99 latency. This is the tool that
// turns "505 routes and 20 features" into a measurable claim about serving
// concurrent users.
//
// Usage:
//
//	go run ./tests/loadtest -base http://localhost:8080 -c 200 -d 30s
//	go run ./tests/loadtest -base http://localhost:8080 -c 200 -d 30s -token <jwt>
//
// Flags:
//
//	-base   base URL of the API (default http://localhost:8080)
//	-c      number of concurrent workers (default 100)
//	-d      duration (default 30s)
//	-token  optional bearer token for authenticated endpoints
//	-mix    comma-separated endpoint weights, e.g. "health:1,feed:3,posts:2"
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	status int
	lat    time.Duration
}

type endpoint struct {
	method string
	path   string
	weight int
}

func main() {
	base := flag.String("base", "http://localhost:8080", "base URL of the API")
	conc := flag.Int("c", 100, "number of concurrent workers")
	dur := flag.Duration("d", 30*time.Second, "duration of the run")
	token := flag.String("token", "", "optional bearer token for authenticated endpoints")
	mix := flag.String("mix", "health:1,ready:1,countries:1,feed:3,posts:2,reels:1,stories:1,notifications:1,conversations:1", "comma-separated endpoint:weight mix")
	flag.Parse()

	var eps []endpoint
	for _, part := range strings.Split(*mix, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(kv) != 2 {
			fmt.Fprintf(os.Stderr, "bad mix entry %q\n", part)
			os.Exit(2)
		}
		w := 1
		fmt.Sscanf(kv[1], "%d", &w)
		eps = append(eps, endpoint{method: "GET", path: kv[0], weight: w})
	}
	// Build a weighted pick list.
	var pick []endpoint
	for _, e := range eps {
		for i := 0; i < e.weight; i++ {
			pick = append(pick, e)
		}
	}
	if len(pick) == 0 {
		fmt.Fprintln(os.Stderr, "no endpoints in mix")
		os.Exit(2)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var total, okCount, errCount int64
	var latSum int64
	var latMax time.Duration
	var mu sync.Mutex
	var lats []time.Duration

	ctx, cancel := context.WithTimeout(context.Background(), *dur)
	defer cancel()

	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				e := pick[time.Now().UnixNano()%int64(len(pick))]
				url := *base + "/" + strings.TrimPrefix(e.path, "/")
				req, _ := http.NewRequest(e.method, url, nil)
				if *token != "" {
					req.Header.Set("Authorization", "Bearer "+*token)
				}
				t0 := time.Now()
				resp, err := client.Do(req)
				lat := time.Since(t0)
				atomic.AddInt64(&total, 1)
				atomic.AddInt64(&latSum, int64(lat))
				mu.Lock()
				if lat > latMax {
					latMax = lat
				}
				lats = append(lats, lat)
				mu.Unlock()
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode < 500 {
					atomic.AddInt64(&okCount, 1)
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Percentiles.
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	pct := func(p float64) time.Duration {
		if len(lats) == 0 {
			return 0
		}
		idx := int(math.Ceil(p*float64(len(lats)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(lats) {
			idx = len(lats) - 1
		}
		return lats[idx]
	}

	fmt.Printf("ChatApp load test\n")
	fmt.Printf("  base:      %s\n", *base)
	fmt.Printf("  workers:   %d\n", *conc)
	fmt.Printf("  duration:  %s\n", *dur)
	fmt.Printf("  endpoints: %s\n", *mix)
	fmt.Printf("  requests:  %d\n", total)
	fmt.Printf("  ok (non-5xx): %d\n", okCount)
	fmt.Printf("  errors:    %d\n", errCount)
	fmt.Printf("  throughput: %.1f req/s\n", float64(total)/elapsed.Seconds())
	fmt.Printf("  p50:       %s\n", pct(0.50))
	fmt.Printf("  p95:       %s\n", pct(0.95))
	fmt.Printf("  p99:       %s\n", pct(0.99))
	fmt.Printf("  max:       %s\n", time.Duration(latMax))
}
