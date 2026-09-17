package mesh

import (
	"math"
	"sync"
	"time"
)

type LinkSample struct {
	RTT    time.Duration
	Lost   bool
	SentAt time.Time
	Bytes  int
}

type LinkFeedback struct {
	mu            sync.Mutex
	ewmaRTT       float64
	ewmaLoss      float64
	ewmaJitter    float64
	windowSize    int
	windowSamples []LinkSample
	windowIdx     int
	lastUpdate    time.Time
	estimatedBPS  float64
}

func NewLinkFeedback(windowSize int) *LinkFeedback {
	if windowSize <= 0 { windowSize = 64 }
	return &LinkFeedback{
		windowSize: windowSize, windowSamples: make([]LinkSample, windowSize),
		lastUpdate: time.Now(), estimatedBPS: 256 * 1024,
	}
}

func (lf *LinkFeedback) RecordSample(sample LinkSample) {
	lf.mu.Lock(); defer lf.mu.Unlock()
	rttUs := float64(sample.RTT.Microseconds())
	alpha := 0.25
	if lf.ewmaRTT == 0 {
		lf.ewmaRTT = rttUs
	} else {
		lf.ewmaJitter = (1-alpha)*lf.ewmaJitter + alpha*math.Abs(rttUs-lf.ewmaRTT)
		lf.ewmaRTT = (1-alpha)*lf.ewmaRTT + alpha*rttUs
	}
	lossVal := 0.0
	if sample.Lost { lossVal = 1.0 }
	lf.ewmaLoss = (1-alpha)*lf.ewmaLoss + alpha*lossVal
	if !sample.Lost && sample.Bytes > 0 && lf.ewmaRTT > 0 {
		rttSec := lf.ewmaRTT / 1e6
		if rttSec > 0 {
			lf.estimatedBPS = 0.875*lf.estimatedBPS + 0.125*float64(sample.Bytes)/rttSec
		}
	}
	lf.windowSamples[lf.windowIdx%lf.windowSize] = sample
	lf.windowIdx++
	lf.lastUpdate = time.Now()
}

func (lf *LinkFeedback) RTT() time.Duration {
	lf.mu.Lock(); defer lf.mu.Unlock()
	if lf.ewmaRTT <= 0 { return 50 * time.Millisecond }
	return time.Duration(lf.ewmaRTT) * time.Microsecond
}

func (lf *LinkFeedback) LossRate() float64 {
	lf.mu.Lock(); defer lf.mu.Unlock()
	return lf.ewmaLoss
}

func (lf *LinkFeedback) Jitter() time.Duration {
	lf.mu.Lock(); defer lf.mu.Unlock()
	return time.Duration(lf.ewmaJitter) * time.Microsecond
}

func (lf *LinkFeedback) EstimatedBPS() float64 {
	lf.mu.Lock(); defer lf.mu.Unlock()
	return lf.estimatedBPS
}

func (lf *LinkFeedback) WindowLossRate() float64 {
	lf.mu.Lock(); defer lf.mu.Unlock()
	count := lf.windowIdx
	if count > lf.windowSize { count = lf.windowSize }
	if count == 0 { return 0 }
	lost := 0
	for i := 0; i < count; i++ {
		if lf.windowSamples[i%lf.windowSize].Lost { lost++ }
	}
	return float64(lost) / float64(count)
}

func (lf *LinkFeedback) AdaptiveRate(currentBytesPerSecond int) int {
	lf.mu.Lock(); defer lf.mu.Unlock()
	rate := float64(currentBytesPerSecond)
	if rate <= 0 { rate = float64(defaultCongestionRate) }
	if lf.ewmaLoss > 0.05 {
		rate = rate * 0.5
	} else if lf.ewmaLoss < 0.01 {
		rate = rate + float64(defaultCongestionRate)/10
	}
	if lf.estimatedBPS > 0 && rate > lf.estimatedBPS*0.9 { rate = lf.estimatedBPS * 0.9 }
	if rate < 4096 { rate = 4096 }
	return int(rate)
}

func (lf *LinkFeedback) LinkQuality() float64 {
	lf.mu.Lock(); defer lf.mu.Unlock()
	lossPenalty := 1.0 - lf.ewmaLoss
	if lossPenalty < 0 { lossPenalty = 0 }
	rttScore := 1.0
	switch {
	case lf.ewmaRTT > 1000000: rttScore = 0.1
	case lf.ewmaRTT > 500000: rttScore = 0.3
	case lf.ewmaRTT > 200000: rttScore = 0.5
	case lf.ewmaRTT > 100000: rttScore = 0.7
	}
	return lossPenalty*0.6 + rttScore*0.4
}