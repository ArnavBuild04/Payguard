package models

import "time"

type Source string

const (
	SourceInGame   Source = "InGame"
	SourcePurchase Source = "Purchase"
)

// Account is the mutable balance row; LedgerEntry is the audit trail.
type Account struct {
	TenantID  string    `gorm:"primaryKey;column:tenant_id" json:"tenant_id"`
	UserID    int64     `gorm:"primaryKey;column:user_id" json:"user_id"`
	Balance   int64     `gorm:"column:balance;not null;default:0" json:"balance"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// LedgerEntry is append-only; uq_ledger_idem is the idempotency boundary.
type LedgerEntry struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	TenantID     string    `gorm:"column:tenant_id;index;uniqueIndex:uq_ledger_idem,priority:1" json:"tenant_id"`
	UserID       int64     `gorm:"column:user_id;index" json:"user_id"`
	Amount       int64     `gorm:"column:amount;not null" json:"amount"`
	BalanceAfter int64     `gorm:"column:balance_after;not null" json:"balance_after"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	Source       Source    `gorm:"column:source;not null;uniqueIndex:uq_ledger_idem,priority:2" json:"source"`
	ReferenceID  string    `gorm:"column:reference_id;not null;uniqueIndex:uq_ledger_idem,priority:3" json:"reference_id"`
}
