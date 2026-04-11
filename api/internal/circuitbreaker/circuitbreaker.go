package circuitbreaker

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// CircuitBreaker states
const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half-open"
)

// Config holds circuit breaker configuration
type Config struct {
	MaxFailures   int           // Number of failures before opening circuit
	ResetTimeout  time.Duration // Time to wait before trying half-open
	Timeout       time.Duration // Maximum time for a call to complete
	MaxConcurrent int           // Maximum concurrent calls allowed
}

// DefaultConfig returns a default circuit breaker config
func DefaultConfig() Config {
	return Config{
		MaxFailures:   5,
		ResetTimeout:  30 * time.Second,
		Timeout:       10 * time.Second,
		MaxConcurrent: 100,
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open
var ErrCircuitOpen = errors.New("circuit breaker is open")

// ErrTimeout is returned when a call exceeds the timeout
var ErrTimeout = errors.New("call timed out")

// ErrMaxConcurrent is returned when maximum concurrent calls are exceeded
var ErrMaxConcurrent = errors.New("maximum concurrent calls exceeded")

// State represents the current state of the circuit breaker
type State int

const (
	closed State = iota
	open
	halfOpen
)

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	mu              sync.RWMutex
	state           State
	failures        int
	successes       int
	lastFailureTime time.Time
	config          Config
	semaphore       chan struct{}
	// Metrics
	TotalRequests   int64
	TotalFailures   int64
	TotalSuccesses  int64
	TotalRejects    int64
	LastStateChange time.Time
	OnStateChange   func(oldState, newState string)
}

// New creates a new circuit breaker with the given config
func New(cfg Config) *CircuitBreaker {
	return &CircuitBreaker{
		state:           closed,
		config:          cfg,
		semaphore:       make(chan struct{}, cfg.MaxConcurrent),
		LastStateChange: time.Now(),
	}
}

// GetState returns the current state as a string
func (cb *CircuitBreaker) GetState() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.stateString()
}

// IsReady returns true if calls are allowed
func (cb *CircuitBreaker) IsReady() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case closed:
		return true
	case open:
		if time.Since(cb.lastFailureTime) > cb.config.ResetTimeout {
			cb.transitionTo(halfOpen)
			return true
		}
		return false
	case halfOpen:
		return true
	}
	return false
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.Lock()

	// Check if circuit allows requests
	if !cb.allowRequest() {
		cb.TotalRejects++
		cb.mu.Unlock()
		return ErrCircuitOpen
	}

	cb.TotalRequests++
	cb.mu.Unlock()

	// Check concurrent limit
	select {
	case cb.semaphore <- struct{}{}:
		defer func() { <-cb.semaphore }()
	default:
		cb.recordFailure()
		return ErrMaxConcurrent
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(ctx, cb.config.Timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		if err != nil {
			cb.recordFailure()
			return err
		}
		cb.recordSuccess()
		return nil
	case <-ctx.Done():
		cb.recordFailure()
		return ErrTimeout
	}
}

// allowRequest checks if a request is allowed (must be called with lock held)
func (cb *CircuitBreaker) allowRequest() bool {
	switch cb.state {
	case closed:
		return true
	case open:
		if time.Since(cb.lastFailureTime) > cb.config.ResetTimeout {
			cb.transitionTo(halfOpen)
			return true
		}
		return false
	case halfOpen:
		return true
	}
	return false
}

// recordSuccess records a successful call
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.TotalSuccesses++

	if cb.state == halfOpen {
		cb.successes++
		if cb.successes >= 3 {
			cb.transitionTo(closed)
		}
	}
}

// recordFailure records a failed call
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.TotalFailures++
	cb.lastFailureTime = time.Now()

	if cb.state == halfOpen {
		cb.transitionTo(open)
		return
	}

	cb.failures++
	if cb.failures >= cb.config.MaxFailures {
		cb.transitionTo(open)
	}
}

// transitionTo changes the circuit breaker state
func (cb *CircuitBreaker) transitionTo(newState State) {
	oldState := cb.state
	if oldState == newState {
		return
	}

	cb.state = newState
	cb.LastStateChange = time.Now()

	switch newState {
	case closed:
		cb.failures = 0
		cb.successes = 0
	case open:
	case halfOpen:
		cb.successes = 0
	}

	if cb.OnStateChange != nil {
		cb.OnStateChange(cb.stateString(), stateToString(newState))
	}

	log.Printf("Circuit breaker state changed: %s -> %s", stateToString(oldState), stateToString(newState))
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(closed)
}

// Open manually opens the circuit breaker
func (cb *CircuitBreaker) Open() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(open)
}

// GetMetrics returns circuit breaker metrics
func (cb *CircuitBreaker) GetMetrics() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	rejectRate := float64(0)
	if cb.TotalRequests > 0 {
		rejectRate = float64(cb.TotalRejects) / float64(cb.TotalRequests) * 100
	}

	return map[string]interface{}{
		"state":             cb.stateString(),
		"total_requests":    cb.TotalRequests,
		"total_successes":   cb.TotalSuccesses,
		"total_failures":    cb.TotalFailures,
		"total_rejects":     cb.TotalRejects,
		"reject_rate_pct":   rejectRate,
		"last_state_change": cb.LastStateChange,
	}
}

func (cb *CircuitBreaker) stateString() string {
	return stateToString(cb.state)
}

func stateToString(s State) string {
	switch s {
	case closed:
		return StateClosed
	case open:
		return StateOpen
	case halfOpen:
		return StateHalfOpen
	default:
		return "unknown"
	}
}
