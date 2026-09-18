// Package providertest gives other packages (payment, coordinator, reconcile — Phase B1/C) a
// controllable provider.Provider for their own tests, without a real HTTP round trip. It is exported
// (not a _test.go file) so it can be imported across package boundaries; the mock service under
// cmd/mockprovider is the "always honest" implementation used by the running system, and this fake is
// where timeouts, 404s, unmappable statuses, and out-of-order webhooks actually get exercised — see
// hld.md §9.5.
package providertest

import (
	"context"
	"fmt"
	"sync"

	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
)

// FakeProvider is a scriptable provider.Provider. Zero value via New() is usable: CreatePayment/
// GetPayment/Refund succeed by default, unless one of the Err/Status hooks below is set.
type FakeProvider struct {
	mu sync.Mutex

	// payments holds every payment CreatePayment has produced, keyed by ID, so GetPayment reflects
	// state SetStatus mutates later in the test — this is what lets a test simulate "the provider
	// actually succeeded after our timeout."
	payments      map[string]*provider.Payment
	unknownStatus map[string]bool // ids whose stored status has no valid mapping (SetRawStatus)
	nextID        int

	// CreateErr, when non-nil, is returned by every CreatePayment call instead of succeeding.
	CreateErr error
	// GetErr, when non-nil, is returned by every GetPayment call instead of succeeding.
	GetErr error
	// RefundErr, when non-nil, is returned by every Refund call instead of succeeding.
	RefundErr error

	// CreateStatus is the Status a successful CreatePayment assigns to the new payment.
	// Defaults to provider.StatusProcessing if unset.
	CreateStatus provider.Status
}

func New() *FakeProvider {
	return &FakeProvider{
		payments:      make(map[string]*provider.Payment),
		unknownStatus: make(map[string]bool),
	}
}

func (f *FakeProvider) CreatePayment(_ context.Context, req provider.CreateRequest) (*provider.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.CreateErr != nil {
		return nil, f.CreateErr
	}

	status := f.CreateStatus
	if status == "" {
		status = provider.StatusProcessing
	}

	f.nextID++
	p := &provider.Payment{
		ID:          fmt.Sprintf("fake_pay_%d", f.nextID),
		Status:      status,
		RawStatus:   string(status),
		AmountMinor: req.AmountMinor,
		Currency:    req.Currency,
	}
	f.payments[p.ID] = p

	// Return a copy so a caller mutating the result can't corrupt the fake's internal state.
	cp := *p
	return &cp, nil
}

func (f *FakeProvider) GetPayment(_ context.Context, id string) (*provider.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.GetErr != nil {
		return nil, f.GetErr
	}
	if f.unknownStatus[id] {
		return nil, providererr.ErrUnknownStatus
	}
	p, ok := f.payments[id]
	if !ok {
		return nil, providererr.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *FakeProvider) Refund(_ context.Context, id string, amountMinor int64) (*provider.Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.RefundErr != nil {
		return nil, f.RefundErr
	}
	if _, ok := f.payments[id]; !ok {
		return nil, providererr.ErrNotFound
	}
	return &provider.Refund{
		ID:          "re_" + id,
		PaymentID:   id,
		Status:      provider.StatusSucceeded,
		AmountMinor: amountMinor,
	}, nil
}

// SetStatus mutates an already-created payment's stored state. A test drives PROVIDER_AHEAD by
// creating a payment (status PROCESSING), simulating a client-side timeout on that same call, then
// calling SetStatus(id, StatusSucceeded) to represent the provider confirming after our deadline.
func (f *FakeProvider) SetStatus(id string, status provider.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.payments[id]; ok {
		p.Status = status
		p.RawStatus = string(status)
		delete(f.unknownStatus, id)
	}
}

// SetRawStatus marks an already-created payment as having a raw status with no valid mapping, so
// GetPayment on this id returns providererr.ErrUnknownStatus — reproducing hld.md's
// UNKNOWN_PROVIDER_STATE without needing an honest mock to lie.
func (f *FakeProvider) SetRawStatus(id, raw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.payments[id]; ok {
		p.RawStatus = raw
		f.unknownStatus[id] = true
	}
}
