package service

// skuDef is a hardcoded catalog for this MVP — Phase C's detector uses the same shape (SKU →
// expected grants) per PLAN.md's reconciliation matrix. The server computes price from here; it
// never trusts a client-supplied amount (hld.md edge case A4).
type skuDef struct {
	AmountMinor int64
	Currency    string
}

var catalog = map[string]skuDef{
	"BUNDLE_10K":    {AmountMinor: 999, Currency: "USD"},
	"BUNDLE_50K":    {AmountMinor: 3999, Currency: "USD"},
	"BUNDLE_TICKET": {AmountMinor: 499, Currency: "USD"},
}

// lookupSKU returns false for both an unknown SKU and one that's been retired from the catalog —
// hld.md's edge case A5 handles them identically (400 before the provider is ever called).
func lookupSKU(sku string) (skuDef, bool) {
	d, ok := catalog[sku]
	return d, ok
}
