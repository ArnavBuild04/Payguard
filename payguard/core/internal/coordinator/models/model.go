package models

import "time"

type OperationType string

const (
	OpCreditChips OperationType = "CREDIT_CHIPS"
	OpGrantAsset  OperationType = "GRANT_ASSET"
	OpGrantTicket OperationType = "GRANT_TICKET"
	OpRefundChips OperationType = "REFUND_CHIPS"
)

type Status string

const (
	StatusPending        Status = "PENDING"
	StatusSuccess        Status = "SUCCESS"
	StatusFailed         Status = "FAILED"
	StatusReversePending Status = "REVERSE_PENDING"
	StatusReversed       Status = "REVERSED"
)

// Transaction is one row PER GRANT, not per order — a bundle of N grants produces N rows, so one
// grant failing never blocks or hides the others (hld.md §6.3's partial-bundle case).
type Transaction struct {
	ID            uint64        `gorm:"primaryKey;autoIncrement;column:id" json:"id"`
	OrderID       uint64        `gorm:"column:order_id;not null;uniqueIndex:uq_transaction_op,priority:1" json:"order_id"`
	OperationType OperationType `gorm:"column:operation_type;not null;uniqueIndex:uq_transaction_op,priority:2" json:"operation_type"`
	Status        Status        `gorm:"column:status;not null" json:"status"`
	TenantID      string        `gorm:"column:tenant_id;not null" json:"tenant_id"`
	UserID        int64         `gorm:"column:user_id;not null" json:"user_id"`
	AmountMinor   int64         `gorm:"column:amount_minor;not null;default:0" json:"amount_minor"`
	Attempts      int           `gorm:"column:attempts;not null;default:0" json:"attempts"`
	LastError     string        `gorm:"column:last_error" json:"last_error,omitempty"`
	CreatedAt     time.Time     `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time     `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}
