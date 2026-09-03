package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type HashInput struct {
	Scene     string
	Channel   string
	Currency  string
	CouponIDs []string
	Items     []OrderLine
	LedgerPay int64
	AssetCode string
	Ext       map[string]any
}

func ContextHash(in HashInput) string {
	coupons := append([]string(nil), in.CouponIDs...)
	sort.Strings(coupons)
	items := make([]map[string]any, 0, len(in.Items))
	for _, l := range in.Items {
		items = append(items, map[string]any{
			"line_id":     l.LineID,
			"object_type": l.ObjectType,
			"object_id":   l.ObjectID,
			"quantity":    l.Quantity,
			"unit_price":  l.UnitPrice,
			"attributes":  l.Attributes,
		})
	}
	payload, _ := json.Marshal(map[string]any{
		"scene":      in.Scene,
		"channel":    in.Channel,
		"currency":   in.Currency,
		"coupon_ids": coupons,
		"items":      items,
		"ledger_pay": in.LedgerPay,
		"asset_code": in.AssetCode,
		"ext":        in.Ext,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func CheckoutHash(clientOrderID, quoteID string, in HashInput) string {
	coupons := append([]string(nil), in.CouponIDs...)
	sort.Strings(coupons)
	items := make([]map[string]any, 0, len(in.Items))
	for _, l := range in.Items {
		items = append(items, map[string]any{
			"line_id":     l.LineID,
			"object_type": l.ObjectType,
			"object_id":   l.ObjectID,
			"quantity":    l.Quantity,
			"unit_price":  l.UnitPrice,
			"attributes":  l.Attributes,
		})
	}
	payload, _ := json.Marshal(map[string]any{
		"client_order_id": clientOrderID,
		"quote_id":        quoteID,
		"scene":           in.Scene,
		"channel":         in.Channel,
		"coupon_ids":      coupons,
		"items":           items,
		"ledger_pay":      in.LedgerPay,
		"asset_code":      in.AssetCode,
		"ext":             in.Ext,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func RequestHash(parts ...any) string {
	payload, _ := json.Marshal(parts)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
