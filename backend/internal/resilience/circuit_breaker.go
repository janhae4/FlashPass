package resilience

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open: database temporarily unavailable")
)

type State string

const (
	StateClosed   State = "CLOSED"
	StateOpen     State = "OPEN"
	StateHalfOpen State = "HALF_OPEN"
)

type CircuitBreaker struct {
	mu sync.Mutex

	state            State
	failureThreshold int
	consecutiveFails int
	cooldown         time.Duration
	lastStateChange  time.Time
}

func NewCircuitBreaker(failureThreshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		lastStateChange:  time.Now(),
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.checkCooldownLocked()
	return cb.state
}

func (cb *CircuitBreaker) checkCooldownLocked() {
	if cb.state == StateOpen && time.Since(cb.lastStateChange) >= cb.cooldown {
		cb.state = StateHalfOpen
		cb.lastStateChange = time.Now()
	}
}

// Execute wraps an operation with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	cb.checkCooldownLocked()

	// If circuit is OPEN -> Fast-Fail immediately in 0ms without hitting downstream DB
	if cb.state == StateOpen {
		cb.mu.Unlock()
		return ErrCircuitOpen
	}

	isHalfOpen := cb.state == StateHalfOpen
	cb.mu.Unlock()

	// Execute operation
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.consecutiveFails++
		// If consecutive errors reach threshold or canary probe fails -> Trip circuit to OPEN
		if cb.consecutiveFails >= cb.failureThreshold || isHalfOpen {
			cb.state = StateOpen
			cb.lastStateChange = time.Now()
		}
		return err
	}

	// Operation succeeded
	if isHalfOpen || cb.state == StateClosed {
		cb.consecutiveFails = 0
		cb.state = StateClosed
		cb.lastStateChange = time.Now()
	}

	return nil
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.consecutiveFails = 0
	cb.lastStateChange = time.Now()
}
