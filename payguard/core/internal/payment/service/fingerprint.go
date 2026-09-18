package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// fingerprint hashes (user_id, sku, amount) so a reused idempotency key with a different body is
// detectable as a client bug (hld.md edge case A2), not silently served the old order.
func fingerprint(userID int64, sku string, amountMinor int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%d", userID, sku, amountMinor)))
	return hex.EncodeToString(sum[:])
}
