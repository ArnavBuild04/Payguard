package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
)

// TestCreatePayment_Success asserts a 2xx maps cleanly and the Idempotency-Key header goes out.
func TestCreatePayment_Success(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"pay_1","status":"processing","amount_minor":1000,"currency":"USD","created_at":"2024-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: time.Second})
	payment, err := p.CreatePayment(context.Background(), CreateRequest{
		IdempotencyKey: "k-1",
		AmountMinor:    1000,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payment.Status != StatusProcessing {
		t.Fatalf("status = %v, want PROCESSING", payment.Status)
	}
	if gotKey != "k-1" {
		t.Fatalf("Idempotency-Key header = %q, want %q", gotKey, "k-1")
	}
}

// TestCreatePayment_Timeout asserts a timeout maps to ErrUnknown, never a failure.
func TestCreatePayment_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"pay_1","status":"succeeded"}`))
	}))
	defer srv.Close()

	// Client-side deadline is shorter than the handler's honest delay.
	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: 5 * time.Millisecond})
	_, err := p.CreatePayment(context.Background(), CreateRequest{IdempotencyKey: "k-1", AmountMinor: 1000, Currency: "USD"})
	if !errors.Is(err, providererr.ErrUnknown) {
		t.Fatalf("err = %v, want ErrUnknown", err)
	}
}

// TestCreatePayment_NonTwoXX asserts a non-2xx also maps to ErrUnknown, not a confirmed failure.
func TestCreatePayment_NonTwoXX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: time.Second})
	_, err := p.CreatePayment(context.Background(), CreateRequest{IdempotencyKey: "k-1", AmountMinor: 1000, Currency: "USD"})
	if !errors.Is(err, providererr.ErrUnknown) {
		t.Fatalf("err = %v, want ErrUnknown", err)
	}
}

// TestGetPayment_UnknownStatus asserts an unrecognized status surfaces as ErrUnknownStatus.
func TestGetPayment_UnknownStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"pay_1","status":"under_review","amount_minor":1000,"currency":"USD"}`))
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: time.Second})
	_, err := p.GetPayment(context.Background(), "pay_1")
	if !errors.Is(err, providererr.ErrUnknownStatus) {
		t.Fatalf("err = %v, want ErrUnknownStatus", err)
	}
}

// TestGetPayment_NotFound asserts a 404 maps to ErrNotFound, not a generic ErrUnknown.
func TestGetPayment_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: time.Second})
	_, err := p.GetPayment(context.Background(), "pay_missing")
	if !errors.Is(err, providererr.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGetPayment_DrivesToTerminal_WithoutWebhooks asserts polling alone reaches a terminal state.
func TestGetPayment_DrivesToTerminal_WithoutWebhooks(t *testing.T) {
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		status := "processing"
		if polls >= 3 {
			status = "succeeded"
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"pay_1","status":"` + status + `","amount_minor":1000,"currency":"USD"}`))
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: time.Second})

	var final *Payment
	for i := 0; i < 5; i++ {
		payment, err := p.GetPayment(context.Background(), "pay_1")
		if err != nil {
			t.Fatalf("poll %d: unexpected error: %v", i, err)
		}
		if payment.Status == StatusSucceeded {
			final = payment
			break
		}
	}
	if final == nil {
		t.Fatal("polling never observed a terminal state")
	}
}

// TestRefund_Success asserts Refund follows the same honest-mapping rules as Create/Get.
func TestRefund_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"re_1","payment_id":"pay_1","status":"succeeded","amount_minor":500}`))
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: time.Second})
	refund, err := p.Refund(context.Background(), "pay_1", 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refund.Status != StatusSucceeded {
		t.Fatalf("status = %v, want SUCCEEDED", refund.Status)
	}
}

func TestRefund_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := NewHTTPProvider(srv.URL, &http.Client{Timeout: time.Second})
	_, err := p.Refund(context.Background(), "pay_missing", 500)
	if !errors.Is(err, providererr.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
