package models

import "time"

type Source string

const (
	SourceInGame   Source = "InGame"
	SourcePurchase Source = "Purchase"
)

type Account struct {
	TenantID  string    `gorm:"primaryKey;column:tenant_id" json:"tenant_id"`
	UserID    string    `gorm:"primaryKey;column:user_id" json:"user_id"`
	Balance   int64     `gorm:"column:balance" json:"balance"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

type LedgerEntry struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	TenantID    string    `gorm:"column:tenant_id;index" json:"tenant_id"`
	UserID      string    `gorm:"column:user_id;index" json:"user_id"`
	Amount      int64     `gorm:"column:amount" json:"amount"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"created_at"`
	Source      Source    `gorm:"column:source" json:"source"`
	ReferenceID string    `gorm:"column:reference_id" json:"reference_id"`
}
