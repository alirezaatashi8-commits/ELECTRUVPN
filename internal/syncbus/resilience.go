package syncbus

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DeadLetter holds failed traffic deltas after retries are exhausted.
type DeadLetter struct {
	Delta    TrafficDelta
	Reason   string
	FailedAt time.Time
	Attempts int
}

const (
	defaultDLQCap     = 2048
	defaultMaxAttempt = 3
)

// Resilience extends the bus with a bounded dead-letter queue and flush timeout.
type Resilience struct {
	dlqMu      sync.Mutex
	dlq        []DeadLetter
	dlqCap     int
	dlqDropped atomic.Uint64

	maxAttempts int
	flushTO     time.Duration
}

func (b *Bus) ensureResilience() {
	if b.res != nil {
		return
	}
	b.res = &Resilience{
		dlq:         make([]DeadLetter, 0, 64),
		dlqCap:      defaultDLQCap,
		maxAttempts: defaultMaxAttempt,
		flushTO:     8 * time.Second,
	}
}

// PushDeadLetter records a delta that could not be applied after retries.
// Oldest entries are dropped when the DLQ is full (never blocks callers).
func (b *Bus) PushDeadLetter(d TrafficDelta, reason string, attempts int) {
	b.ensureResilience()
	b.res.dlqMu.Lock()
	defer b.res.dlqMu.Unlock()
	if len(b.res.dlq) >= b.res.dlqCap {
		// drop oldest
		copy(b.res.dlq, b.res.dlq[1:])
		b.res.dlq = b.res.dlq[:len(b.res.dlq)-1]
		b.res.dlqDropped.Add(1)
	}
	b.res.dlq = append(b.res.dlq, DeadLetter{
		Delta: d, Reason: reason, FailedAt: time.Now(), Attempts: attempts,
	})
}

// DrainDeadLetters returns up to max items for inspection/replay (non-blocking).
func (b *Bus) DrainDeadLetters(max int) []DeadLetter {
	b.ensureResilience()
	b.res.dlqMu.Lock()
	defer b.res.dlqMu.Unlock()
	if max <= 0 || max > len(b.res.dlq) {
		max = len(b.res.dlq)
	}
	out := make([]DeadLetter, max)
	copy(out, b.res.dlq[:max])
	b.res.dlq = b.res.dlq[max:]
	return out
}

// DLQStats returns dead-letter counters.
func (b *Bus) DLQStats() map[string]uint64 {
	b.ensureResilience()
	b.res.dlqMu.Lock()
	n := uint64(len(b.res.dlq))
	b.res.dlqMu.Unlock()
	return map[string]uint64{
		"dlqLen":     n,
		"dlqCap":     uint64(b.res.dlqCap),
		"dlqDropped": b.res.dlqDropped.Load(),
	}
}

// FlushTimeout is the max time a DB apply may hold the traffic mutex path.
func (b *Bus) FlushTimeout() time.Duration {
	b.ensureResilience()
	return b.res.flushTO
}

// WithTimeout runs fn with a deadline. Prevents a stuck DB/sync from
// blocking the worker forever (goroutine leak / lock hold).
func WithTimeout(parent context.Context, d time.Duration, fn func(ctx context.Context) error) error {
	if d <= 0 {
		d = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- fn(ctx)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// fn may still run briefly; callers must use short DB ops.
		return ctx.Err()
	}
}

// Retry does exponential backoff for network-facing agent calls.
// Never retries forever — maxAttempts caps the loop so a filtered master
// cannot pin an agent goroutine indefinitely.
func Retry(maxAttempts int, base time.Duration, maxBackoff time.Duration, fn func(attempt int) error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	if maxBackoff <= 0 {
		maxBackoff = 5 * time.Second
	}
	var last error
	backoff := base
	for i := 1; i <= maxAttempts; i++ {
		last = fn(i)
		if last == nil {
			return nil
		}
		if i == maxAttempts {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	return last
}
