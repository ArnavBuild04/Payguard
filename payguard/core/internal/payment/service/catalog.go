package service

// skuDef is a hardcoded pricing catalog; the server never trusts a client-supplied amount.
type skuDef struct {
	AmountMinor int64
	Currency    string
}

var catalog = map[string]skuDef{
	"BUNDLE_10K":    {AmountMinor: 999, Currency: "USD"},
	"BUNDLE_50K":    {AmountMinor: 3999, Currency: "USD"},
	"BUNDLE_TICKET": {AmountMinor: 499, Currency: "USD"},
}

// lookupSKU returns false for both an unknown SKU and a retired one.
func lookupSKU(sku string) (skuDef, bool) {
	d, ok := catalog[sku]
	return d, ok
}
