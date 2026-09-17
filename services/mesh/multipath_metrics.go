package mesh

import (
	"math"
	"sync"
)

type PathMetric struct {
	NeighborID string
	LossRate   float64
	LatencyUs  float64
	JitterUs   float64
	Thruput    float64
	Score      float64
	Samples    int64
	Sent       int64
	Lost       int64
	Bytes      int64
	ElapsedUs  int64
}

type PathMetrics struct {
	mu   sync.Mutex
	path map[string]*PathMetric
}

func NewPathMetrics() *PathMetrics {
	return &PathMetrics{path: make(map[string]*PathMetric)}
}

func (pm *PathMetrics) key(neighborID, dest string) string {
	return neighborID + "|" + dest
}

func (pm *PathMetrics) Record(neighborID, dest string, bytes int, latencyUs float64, lost bool) {
	pm.mu.Lock(); defer pm.mu.Unlock()
	k := pm.key(neighborID, dest)
	m, ok := pm.path[k]
	if !ok {
		m = &PathMetric{NeighborID: neighborID, Score: 1.0}
		pm.path[k] = m
	}
	alpha := 0.125
	m.Samples++
	m.Sent++
	m.Bytes += int64(bytes)
	m.ElapsedUs += int64(latencyUs)
	lossVal := 0.0
	if lost { lossVal = 1.0; m.Lost++ }
	m.LossRate = (1-alpha)*m.LossRate + alpha*lossVal
	if !lost && latencyUs > 0 {
		if m.LatencyUs == 0 {
			m.LatencyUs = latencyUs
		} else {
			m.LatencyUs = (1-alpha)*m.LatencyUs + alpha*latencyUs
		}
		m.JitterUs = (1-alpha)*m.JitterUs + alpha*math.Abs(latencyUs-m.LatencyUs)
	}
	if m.ElapsedUs > 0 {
		m.Thruput = 1e6 * float64(m.Bytes) / float64(m.ElapsedUs)
	}
	lossScore := 1.0 - m.LossRate
	if lossScore < 0 { lossScore = 0 }
	latScore := 1.0
	switch {
	case m.LatencyUs > 500000: latScore = 0.1
	case m.LatencyUs > 200000: latScore = 0.3
	case m.LatencyUs > 100000: latScore = 0.5
	case m.LatencyUs > 50000: latScore = 0.7
	case m.LatencyUs > 20000: latScore = 0.85
	}
	jitterScore := 1.0
	if m.JitterUs > 0 {
		jitterRatio := m.JitterUs / (m.LatencyUs + 1)
		switch {
		case jitterRatio > 0.5: jitterScore = 0.5
		case jitterRatio > 0.25: jitterScore = 0.75
		}
	}
	m.Score = lossScore*0.4 + latScore*0.3 + jitterScore*0.3
}

func (pm *PathMetrics) Get(neighborID, dest string) (PathMetric, bool) {
	pm.mu.Lock(); defer pm.mu.Unlock()
	m, ok := pm.path[pm.key(neighborID, dest)]
	if !ok { return PathMetric{}, false }
	return *m, true
}

func (pm *PathMetrics) BestPaths(dest string, max int) []PathMetric {
	pm.mu.Lock(); defer pm.mu.Unlock()
	type scored struct { m PathMetric; score float64 }
	var candidates []scored
	for _, m := range pm.path {
		candidates = append(candidates, scored{m: *m, score: m.Score})
	}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	if max > len(candidates) { max = len(candidates) }
	out := make([]PathMetric, max)
	for i := 0; i < max; i++ { out[i] = candidates[i].m }
	return out
}

func (pm *PathMetrics) Snapshot() []PathMetric {
	pm.mu.Lock(); defer pm.mu.Unlock()
	out := make([]PathMetric, 0, len(pm.path))
	for _, m := range pm.path { out = append(out, *m) }
	return out
}