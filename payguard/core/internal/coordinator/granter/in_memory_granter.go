package granter

import (
	"context"
	"errors"
	"sync"
)

// InMemoryGranter is a scriptable AssetGranter/TicketGranter for tests.
type InMemoryGranter struct {
	mu       sync.Mutex
	GrantErr error
	granted  map[uint64]bool
}

func NewInMemoryGranter() *InMemoryGranter {
	return &InMemoryGranter{granted: make(map[uint64]bool)}
}

func (g *InMemoryGranter) Grant(_ context.Context, _ string, _ int64, orderID uint64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.GrantErr != nil {
		return g.GrantErr
	}
	g.granted[orderID] = true
	return nil
}

func (g *InMemoryGranter) Revoke(_ context.Context, _ string, _ int64, orderID uint64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.granted[orderID] {
		return errors.New("in-memory granter: nothing to revoke for this order")
	}
	delete(g.granted, orderID)
	return nil
}

func (g *InMemoryGranter) WasGranted(orderID uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.granted[orderID]
}
