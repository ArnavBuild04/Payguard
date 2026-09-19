// Package providertest is a controllable provider.Provider test double.
package providertest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
)

// SimulatedProvider is a scriptable provider.Provider.
type SimulatedProvider struct {
	mu sync.Mutex

	payments      map[string]*provider.Payment
	unknownStatus map[string]bool
	nextID        int
	startedAt     int64

	CreateErr    error
	GetErr       error
	RefundErr    error
	CreateStatus provider.Status
}

// New seeds its id generator from the current time, so ids from one test run never collide with
// ids a prior run may have already persisted to a shared database (see cmd/mockprovider's store).
func New() *SimulatedProvider {
	return &SimulatedProvider{
		payments:      make(map[string]*provider.Payment),
		unknownStatus: make(map[string]bool),
		startedAt:     time.Now().UnixNano(),
	}
}

func (f *SimulatedProvider) CreatePayment(_ context.Context, req provider.CreateRequest) (*provider.Payment, error) {
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
		ID:          fmt.Sprintf("sim_pay_%d_%d", f.startedAt, f.nextID),
		Status:      status,
		RawStatus:   string(status),
		AmountMinor: req.AmountMinor,
		Currency:    req.Currency,
	}
	f.payments[p.ID] = p

	cp := *p
	return &cp, nil
}

func (f *SimulatedProvider) GetPayment(_ context.Context, id string) (*provider.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.GetErr != nil {
		return nil, f.GetErr
	}
	if f.unknownStatus[id] {
		raw := ""
		if p, ok := f.payments[id]; ok {
			raw = p.RawStatus
		}
		return nil, &providererr.UnknownStatusError{Raw: raw}
	}
	p, ok := f.payments[id]
	if !ok {
		return nil, providererr.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *SimulatedProvider) Refund(_ context.Context, id string, amountMinor int64) (*provider.Refund, error) {
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

// SetStatus mutates an already-created payment's stored status.
func (f *SimulatedProvider) SetStatus(id string, status provider.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.payments[id]; ok {
		p.Status = status
		p.RawStatus = string(status)
		delete(f.unknownStatus, id)
	}
}

// SetRawStatus marks a payment's status as unmappable, so GetPayment returns ErrUnknownStatus.
func (f *SimulatedProvider) SetRawStatus(id, raw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.payments[id]; ok {
		p.RawStatus = raw
		f.unknownStatus[id] = true
	}
}
