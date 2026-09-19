// Package granter defines the DTC's boundary to the asset/ticket grant services.
package granter

import "context"

type AssetGranter interface {
	Grant(ctx context.Context, tenantID string, userID int64, orderID uint64) error
	Revoke(ctx context.Context, tenantID string, userID int64, orderID uint64) error
}

type TicketGranter interface {
	Grant(ctx context.Context, tenantID string, userID int64, orderID uint64) error
	Revoke(ctx context.Context, tenantID string, userID int64, orderID uint64) error
}
