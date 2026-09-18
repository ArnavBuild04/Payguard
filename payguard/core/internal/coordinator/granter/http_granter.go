package granter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// httpGranter is the one implementation behind both NewHTTPAssetGranter and NewHTTPTicketGranter —
// asset and ticket grants are identical shape today ("stubs for now, real interface"); resource is
// the only thing that differs, and it becomes the wire path (/v1/{resource}/grant).
type httpGranter struct {
	baseURL  string
	resource string
	client   *http.Client
}

func NewHTTPAssetGranter(baseURL string, client *http.Client) AssetGranter {
	return &httpGranter{baseURL: baseURL, resource: "assets", client: client}
}

func NewHTTPTicketGranter(baseURL string, client *http.Client) TicketGranter {
	return &httpGranter{baseURL: baseURL, resource: "tickets", client: client}
}

type grantRequestWire struct {
	TenantID string `json:"tenant_id"`
	UserID   int64  `json:"user_id"`
	OrderID  uint64 `json:"order_id"`
}

func (g *httpGranter) Grant(ctx context.Context, tenantID string, userID int64, orderID uint64) error {
	return g.call(ctx, "grant", tenantID, userID, orderID)
}

func (g *httpGranter) Revoke(ctx context.Context, tenantID string, userID int64, orderID uint64) error {
	return g.call(ctx, "revoke", tenantID, userID, orderID)
}

func (g *httpGranter) call(ctx context.Context, action, tenantID string, userID int64, orderID uint64) error {
	body, err := json.Marshal(grantRequestWire{TenantID: tenantID, UserID: userID, OrderID: orderID})
	if err != nil {
		return fmt.Errorf("granter: encode request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/%s/%s", g.baseURL, g.resource, action)
	req, err := newRequest(ctx, url, body)
	if err != nil {
		return fmt.Errorf("granter: build request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("granter: %s %s: %w", action, g.resource, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("granter: %s %s: unexpected status %d", action, g.resource, resp.StatusCode)
	}
	return nil
}

func newRequest(ctx context.Context, url string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
