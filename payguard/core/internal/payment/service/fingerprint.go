package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// fingerprint hashes (user_id, sku, amount) to detect a reused idempotency key with a different body.
func fingerprint(userID int64, sku string, amountMinor int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%d", userID, sku, amountMinor)))
	return hex.EncodeToString(sum[:])
}
