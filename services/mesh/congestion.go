package mesh

import (
	"sync"
	"time"
)

const (
	defaultCongestionRate  = 256 * 1024
	defaultCongestionBurst = 512 * 1024
)

type CongestionController struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
	admit  uint64
	block  uint64
}

func NewCongestionController(rateBytesPerSecond, burstBytes int) *CongestionController {
	if rateBytesPerSecond <= 0 {
		rateBytesPerSecond = defaultCongestionRate
	}
	if burstBytes <= 0 {
		burstBytes = defaultCongestionBurst
	}
	return &CongestionController{
		rate:   float64(rateBytesPerSecond),
		burst:  float64(burstBytes),
		tokens: float64(burstBytes),
		last:   time.Now(),
	}
}

func (c *CongestionController) Allow(bytes int, now time.Time) bool {
	if bytes <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.last) {
		now = c.last
	}
	c.tokens += now.Sub(c.last).Seconds() * c.rate
	if c.tokens > c.burst {
		c.tokens = c.burst
	}
	c.last = now
	if float64(bytes) > c.tokens {
		c.block++
		return false
	}
	c.tokens -= float64(bytes)
	c.admit++
	return true
}

func (c *CongestionController) Status() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"rate_bytes_per_second": int(c.rate),
		"burst_bytes":           int(c.burst),
		"admitted_packets":      c.admit,
		"blocked_packets":       c.block,
		"available_bytes":       int(c.tokens),
	}
}
