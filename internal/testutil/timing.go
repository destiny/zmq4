// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// MessageExchange represents a message exchange pattern for E2E testing
type MessageExchange struct {
	MessageCount      int           // Total messages to exchange
	MinInterval       time.Duration // Minimum interval between messages
	MaxInterval       time.Duration // Maximum interval between messages
	Timeout           time.Duration // Overall timeout for the exchange
	BiDirectional     bool          // Whether messages flow both ways
	VerifyOrder       bool          // Whether to verify message ordering
	VerifyDelivery    bool          // Whether to verify all messages are delivered
}

// DefaultMessageExchange returns the standard E2E test message exchange pattern
func DefaultMessageExchange() *MessageExchange {
	return &MessageExchange{
		MessageCount:   10,
		MinInterval:    1 * time.Second,
		MaxInterval:    3 * time.Second,
		Timeout:        60 * time.Second,
		BiDirectional:  true,
		VerifyOrder:    true,
		VerifyDelivery: true,
	}
}

// MessageTimer generates variable intervals for message sending
type MessageTimer struct {
	minInterval time.Duration
	maxInterval time.Duration
	rand        *rand.Rand
	mu          sync.Mutex
}

// NewMessageTimer creates a new message timer with variable intervals
func NewMessageTimer(minInterval, maxInterval time.Duration) *MessageTimer {
	return &MessageTimer{
		minInterval: minInterval,
		maxInterval: maxInterval,
		rand:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// NextInterval returns the next random interval between min and max
func (mt *MessageTimer) NextInterval() time.Duration {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	if mt.minInterval == mt.maxInterval {
		return mt.minInterval
	}
	
	delta := mt.maxInterval - mt.minInterval
	randomDelta := time.Duration(mt.rand.Int63n(int64(delta)))
	
	return mt.minInterval + randomDelta
}

// MessageTracker tracks sent and received messages for verification
type MessageTracker struct {
	sent     map[string]time.Time
	received map[string]time.Time
	order    []string
	mu       sync.RWMutex
}

// NewMessageTracker creates a new message tracker
func NewMessageTracker() *MessageTracker {
	return &MessageTracker{
		sent:     make(map[string]time.Time),
		received: make(map[string]time.Time),
		order:    make([]string, 0),
	}
}

// MarkSent marks a message as sent
func (mt *MessageTracker) MarkSent(messageID string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.sent[messageID] = time.Now()
}

// MarkReceived marks a message as received
func (mt *MessageTracker) MarkReceived(messageID string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	
	mt.received[messageID] = time.Now()
	mt.order = append(mt.order, messageID)
}

// GetStats returns statistics about the message exchange
func (mt *MessageTracker) GetStats() MessageStats {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	stats := MessageStats{
		TotalSent:     len(mt.sent),
		TotalReceived: len(mt.received),
		MessageOrder:  make([]string, len(mt.order)),
		Latencies:     make(map[string]time.Duration),
	}
	
	copy(stats.MessageOrder, mt.order)
	
	// Calculate latencies
	for msgID, sentTime := range mt.sent {
		if recvTime, received := mt.received[msgID]; received {
			stats.Latencies[msgID] = recvTime.Sub(sentTime)
		}
	}
	
	return stats
}

// VerifyDelivery verifies that all sent messages were received
func (mt *MessageTracker) VerifyDelivery(t testing.TB) {
	stats := mt.GetStats()
	
	if stats.TotalSent != stats.TotalReceived {
		t.Errorf("Message delivery mismatch: sent %d, received %d", 
			stats.TotalSent, stats.TotalReceived)
	}
	
	// Check that every sent message was received
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	for msgID := range mt.sent {
		if _, received := mt.received[msgID]; !received {
			t.Errorf("Message %s was sent but not received", msgID)
		}
	}
}

// MessageStats holds statistics about message exchange
type MessageStats struct {
	TotalSent     int
	TotalReceived int
	MessageOrder  []string
	Latencies     map[string]time.Duration
}

// GetAverageLatency calculates average latency
func (ms *MessageStats) GetAverageLatency() time.Duration {
	if len(ms.Latencies) == 0 {
		return 0
	}
	
	var total time.Duration
	for _, latency := range ms.Latencies {
		total += latency
	}
	
	return total / time.Duration(len(ms.Latencies))
}

// GetMaxLatency returns the maximum latency
func (ms *MessageStats) GetMaxLatency() time.Duration {
	var max time.Duration
	for _, latency := range ms.Latencies {
		if latency > max {
			max = latency
		}
	}
	return max
}

// E2ETestRunner runs end-to-end tests with timing and verification
type E2ETestRunner struct {
	config  *MessageExchange
	timer   *MessageTimer
	tracker *MessageTracker
}

// NewE2ETestRunner creates a new E2E test runner
func NewE2ETestRunner(config *MessageExchange) *E2ETestRunner {
	if config == nil {
		config = DefaultMessageExchange()
	}
	
	return &E2ETestRunner{
		config:  config,
		timer:   NewMessageTimer(config.MinInterval, config.MaxInterval),
		tracker: NewMessageTracker(),
	}
}

// GetTimer returns the message timer
func (e *E2ETestRunner) GetTimer() *MessageTimer {
	return e.timer
}

// GetTracker returns the message tracker
func (e *E2ETestRunner) GetTracker() *MessageTracker {
	return e.tracker
}

// GetConfig returns the message exchange configuration
func (e *E2ETestRunner) GetConfig() *MessageExchange {
	return e.config
}

// VerifyResults verifies the test results
func (e *E2ETestRunner) VerifyResults(t testing.TB) {
	if e.config.VerifyDelivery {
		e.tracker.VerifyDelivery(t)
	}
	
	stats := e.tracker.GetStats()
	
	// Log statistics
	t.Logf("E2E Test Results:")
	t.Logf("  Messages sent: %d", stats.TotalSent)
	t.Logf("  Messages received: %d", stats.TotalReceived)
	t.Logf("  Average latency: %v", stats.GetAverageLatency())
	t.Logf("  Max latency: %v", stats.GetMaxLatency())
}

// TestTimeoutContext creates a context with timeout for testing
func TestTimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// WaitWithTimeout waits for a condition with timeout
func WaitWithTimeout(t testing.TB, condition func() bool, timeout time.Duration, checkInterval time.Duration) {
	ctx, cancel := TestTimeoutContext(timeout)
	defer cancel()
	
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for condition after %v", timeout)
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// StabilityTestRunner runs stability tests with repeated operations
type StabilityTestRunner struct {
	iterations   int
	duration     time.Duration
	concurrent   int
	errorHandler func(error)
}

// NewStabilityTestRunner creates a new stability test runner
func NewStabilityTestRunner(iterations int, duration time.Duration) *StabilityTestRunner {
	return &StabilityTestRunner{
		iterations: iterations,
		duration:   duration,
		concurrent: 1,
	}
}

// WithConcurrency sets the concurrency level for stability testing
func (s *StabilityTestRunner) WithConcurrency(concurrent int) *StabilityTestRunner {
	s.concurrent = concurrent
	return s
}

// WithErrorHandler sets an error handler for stability testing
func (s *StabilityTestRunner) WithErrorHandler(handler func(error)) *StabilityTestRunner {
	s.errorHandler = handler
	return s
}

// Run executes the stability test
func (s *StabilityTestRunner) Run(t testing.TB, testFunc func() error) {
	ctx, cancel := TestTimeoutContext(s.duration)
	defer cancel()
	
	var wg sync.WaitGroup
	errorChan := make(chan error, s.concurrent)
	
	// Start concurrent workers
	for i := 0; i < s.concurrent; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			for iter := 0; iter < s.iterations/s.concurrent; iter++ {
				select {
				case <-ctx.Done():
					return
				default:
					if err := testFunc(); err != nil {
						errorChan <- err
						if s.errorHandler != nil {
							s.errorHandler(err)
						}
					}
				}
			}
		}(i)
	}
	
	// Wait for completion or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		// Success
	case <-ctx.Done():
		t.Fatalf("Stability test timed out after %v", s.duration)
	case err := <-errorChan:
		t.Fatalf("Stability test failed: %v", err)
	}
	
	// Check for any remaining errors
	close(errorChan)
	for err := range errorChan {
		if err != nil {
			t.Errorf("Stability test error: %v", err)
		}
	}
}

// BenchmarkTimer provides timing utilities for benchmarks
type BenchmarkTimer struct {
	start time.Time
	laps  []time.Duration
	mu    sync.Mutex
}

// NewBenchmarkTimer creates a new benchmark timer
func NewBenchmarkTimer() *BenchmarkTimer {
	return &BenchmarkTimer{
		laps: make([]time.Duration, 0),
	}
}

// Start starts the benchmark timer
func (bt *BenchmarkTimer) Start() {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.start = time.Now()
}

// Lap records a lap time
func (bt *BenchmarkTimer) Lap() time.Duration {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	
	now := time.Now()
	lap := now.Sub(bt.start)
	bt.laps = append(bt.laps, lap)
	bt.start = now
	
	return lap
}

// GetLaps returns all recorded lap times
func (bt *BenchmarkTimer) GetLaps() []time.Duration {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	
	laps := make([]time.Duration, len(bt.laps))
	copy(laps, bt.laps)
	return laps
}