package granter

import (
	"context"
	"errors"
	"sync"
)

// Fake is a scriptable AssetGranter/TicketGranter for the coordinator service's own tests — no HTTP
// round trip, controllable failures, so a partial-bundle scenario (hld.md §6.3) is exercisable
// without needing a real grant service stopped.
type Fake struct {
	mu       sync.Mutex
	GrantErr error // when non-nil, every Grant call fails with this error
	granted  map[uint64]bool
}

func NewFake() *Fake {
	return &Fake{granted: make(map[uint64]bool)}
}

func (f *Fake) Grant(_ context.Context, _ string, _ int64, orderID uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.GrantErr != nil {
		return f.GrantErr
	}
	f.granted[orderID] = true
	return nil
}

func (f *Fake) Revoke(_ context.Context, _ string, _ int64, orderID uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.granted[orderID] {
		return errors.New("fake granter: nothing to revoke for this order")
	}
	delete(f.granted, orderID)
	return nil
}

func (f *Fake) WasGranted(orderID uint64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.granted[orderID]
}
