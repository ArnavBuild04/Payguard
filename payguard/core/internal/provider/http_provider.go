package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
)

// rawStatus is the exact string the wire mock/PSP sends. Never compared loosely — see statusFromRaw.
type rawStatus string

const (
	rawProcessing rawStatus = "processing"
	rawSucceeded  rawStatus = "succeeded"
	rawFailed     rawStatus = "failed"
	rawRefunded   rawStatus = "refunded"
)

// statusFromRaw has no default branch: an unrecognized string is ErrUnknownStatus.
func statusFromRaw(s string) (Status, error) {
	switch rawStatus(s) {
	case rawProcessing:
		return StatusProcessing, nil
	case rawSucceeded:
		return StatusSucceeded, nil
	case rawFailed:
		return StatusFailed, nil
	case rawRefunded:
		return StatusRefunded, nil
	default:
		return "", &providererr.UnknownStatusError{Raw: s}
	}
}

type paymentWire struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
}

func (w paymentWire) toPayment() (*Payment, error) {
	status, err := statusFromRaw(w.Status)
	if err != nil {
		return nil, err
	}
	return &Payment{
		ID:          w.ID,
		Status:      status,
		RawStatus:   w.Status,
		AmountMinor: w.AmountMinor,
		Currency:    w.Currency,
		CreatedAt:   w.CreatedAt,
	}, nil
}

type refundWire struct {
	ID          string `json:"id"`
	PaymentID   string `json:"payment_id"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
}

func (w refundWire) toRefund() (*Refund, error) {
	status, err := statusFromRaw(w.Status)
	if err != nil {
		return nil, err
	}
	return &Refund{
		ID:          w.ID,
		PaymentID:   w.PaymentID,
		Status:      status,
		AmountMinor: w.AmountMinor,
	}, nil
}

type createRequestWire struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	WebhookURL  string `json:"webhook_url,omitempty"`
}

type refundRequestWire struct {
	AmountMinor int64 `json:"amount_minor"`
}

// httpProvider is the running system's Provider implementation.
type httpProvider struct {
	baseURL string
	client  *http.Client
}

// NewHTTPProvider builds a Provider backed by an HTTP PSP at baseURL.
func NewHTTPProvider(baseURL string, client *http.Client) Provider {
	return &httpProvider{baseURL: baseURL, client: client}
}

func (p *httpProvider) CreatePayment(ctx context.Context, req CreateRequest) (*Payment, error) {
	body, err := json.Marshal(createRequestWire{
		AmountMinor: req.AmountMinor,
		Currency:    req.Currency,
		WebhookURL:  req.WebhookURL,
	})
	if err != nil {
		return nil, fmt.Errorf("provider: encode create request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/payments", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider: build create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		// Timeout, connection refused, DNS failure — we do not know what happened. Never FAILED.
		return nil, providererr.ErrUnknown
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, providererr.ErrUnknown
	}

	var wire paymentWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, providererr.ErrUnknown
	}
	return wire.toPayment()
}

func (p *httpProvider) GetPayment(ctx context.Context, id string) (*Payment, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/v1/payments/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("provider: build get request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, providererr.ErrUnknown
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, providererr.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, providererr.ErrUnknown
	}

	var wire paymentWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, providererr.ErrUnknown
	}
	return wire.toPayment()
}

func (p *httpProvider) Refund(ctx context.Context, id string, amountMinor int64) (*Refund, error) {
	body, err := json.Marshal(refundRequestWire{AmountMinor: amountMinor})
	if err != nil {
		return nil, fmt.Errorf("provider: encode refund request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/payments/"+id+"/refund", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider: build refund request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, providererr.ErrUnknown
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, providererr.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, providererr.ErrUnknown
	}

	var wire refundWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, providererr.ErrUnknown
	}
	return wire.toRefund()
}
