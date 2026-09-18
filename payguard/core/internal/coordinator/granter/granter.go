// Package granter defines the DTC's boundary to the asset/ticket grant services — "their own grant
// tables, stubs for now, real interface" per hld.md §1. Grant/Revoke are keyed by orderID so a
// redelivered fan-out (hld.md edge case F1/E2) is a no-op at the grant service, not a double grant.
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
