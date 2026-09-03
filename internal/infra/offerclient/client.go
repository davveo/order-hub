package offerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
	"github.com/davveo/order-hub/internal/infra/httpx"
)

type Client struct {
	base   string
	http   *http.Client
	apiKey string
}

func New(base, apiKey string) *Client {
	return &Client{base: trim(base), http: &http.Client{Timeout: 800 * time.Millisecond}, apiKey: apiKey}
}

func (c *Client) Quote(ctx context.Context, req port.QuoteRequest) (*port.QuoteResult, error) {
	var original int64
	items := make([]map[string]any, 0, len(req.Items))
	for _, it := range req.Items {
		lineAmt := it.UnitPrice * it.Quantity
		original += lineAmt
		items = append(items, map[string]any{
			"line_id": it.LineID, "object_type": it.ObjectType, "object_id": it.ObjectID,
			"quantity": it.Quantity, "unit_price": it.UnitPrice, "amount": lineAmt, "attributes": it.Attributes,
		})
	}
	body := map[string]any{
		"tenant_id":  req.TenantID,
		"channel":    req.Channel,
		"auto_best":  req.AutoBest,
		"coupon_ids": req.CouponIDs,
		"subject": map[string]any{
			"type": "user", "id": req.UserID, "attributes": req.Attributes,
		},
		"transaction": map[string]any{
			"id": req.OrderID, "amount": original, "currency": req.Currency,
		},
		"items":   items,
		"context": req.Context,
	}
	return post[port.QuoteResult](ctx, c, "/api/discount/v1/quotes", body)
}

func (c *Client) Reserve(ctx context.Context, quoteID, orderID, idemKey string) (*port.ReservationResult, error) {
	return post[port.ReservationResult](ctx, c, "/api/discount/v1/reservations", map[string]any{
		"quote_id": quoteID, "business_order_id": orderID, "idempotency_key": idemKey,
	})
}

func (c *Client) Commit(ctx context.Context, reservationID, orderID, idemKey string) (string, error) {
	out, err := post[map[string]string](ctx, c, "/api/discount/v1/reservations/"+reservationID+"/commit", map[string]any{
		"business_order_id": orderID, "idempotency_key": idemKey,
	})
	if err != nil {
		return "", err
	}
	return (*out)["redemption_id"], nil
}

func (c *Client) Release(ctx context.Context, reservationID, orderID, idemKey string) error {
	_, err := post[map[string]any](ctx, c, "/api/discount/v1/reservations/"+reservationID+"/release", map[string]any{
		"business_order_id": orderID, "idempotency_key": idemKey,
	})
	return err
}

func (c *Client) Renew(ctx context.Context, reservationID, orderID, idemKey string) error {
	_, err := post[map[string]any](ctx, c, "/api/discount/v1/reservations/"+reservationID+"/renew", map[string]any{
		"business_order_id": orderID, "idempotency_key": idemKey,
	})
	return err
}

func (c *Client) Reverse(ctx context.Context, redemptionID, refundID, idemKey string) error {
	_, err := post[map[string]any](ctx, c, "/api/discount/v1/redemptions/"+redemptionID+"/reverse", map[string]any{
		"refund_id": refundID, "idempotency_key": idemKey,
	})
	return err
}

func post[T any](ctx context.Context, c *Client, path string, payload any) (*T, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out T
	if err := httpx.Decode(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func trim(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

type Mock struct {
	mu     sync.Mutex
	seq    int
	quotes map[string]*port.QuoteResult
	resv   map[string]string
	renews map[string]int
}

func NewMock() *Mock {
	return &Mock{quotes: map[string]*port.QuoteResult{}, resv: map[string]string{}, renews: map[string]int{}}
}

func (m *Mock) Quote(_ context.Context, req port.QuoteRequest) (*port.QuoteResult, error) {
	var original int64
	for _, it := range req.Items {
		original += it.UnitPrice * it.Quantity
	}
	var discount int64
	if len(req.CouponIDs) > 0 {
		discount = original / 10
	}
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("quote_%d", m.seq)
	m.mu.Unlock()
	allocs := make([]domain.Allocation, 0, len(req.Items))
	var used int64
	for i, it := range req.Items {
		lineOrig := it.UnitPrice * it.Quantity
		var d int64
		if i == len(req.Items)-1 {
			d = discount - used
		} else if original > 0 {
			d = lineOrig * discount / original
			used += d
		}
		allocs = append(allocs, domain.Allocation{LineID: it.LineID, DiscountAmount: d})
	}
	res := &port.QuoteResult{
		QuoteID:        id,
		Currency:       req.Currency,
		OriginalAmount: original,
		DiscountAmount: discount,
		PayableAmount:  original - discount,
		Allocations:    allocs,
		ExpiresAt:      time.Now().Add(5 * time.Minute),
	}
	if len(req.CouponIDs) > 0 {
		res.Promotions = []domain.PromotionDetail{{
			SourceType: "coupon", SourceID: req.CouponIDs[0], DiscountAmount: discount, Allocations: allocs,
		}}
	}
	m.mu.Lock()
	m.quotes[id] = res
	m.mu.Unlock()
	return res, nil
}

func (m *Mock) Reserve(_ context.Context, quoteID, orderID, _ string) (*port.ReservationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.quotes[quoteID]; !ok {
		return nil, domain.ErrOfferReserve
	}
	rid := "rsv_" + orderID
	m.resv[rid] = quoteID
	return &port.ReservationResult{ReservationID: rid, ExpiresAt: time.Now().Add(15 * time.Minute)}, nil
}

func (m *Mock) Commit(_ context.Context, reservationID, _, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.resv[reservationID]; !ok {
		return "", domain.ErrOfferReserve
	}
	return "rd_" + reservationID, nil
}

func (m *Mock) Release(_ context.Context, reservationID, _, _ string) error {
	m.mu.Lock()
	delete(m.resv, reservationID)
	m.mu.Unlock()
	return nil
}

func (m *Mock) Renew(_ context.Context, reservationID, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.resv[reservationID]; !ok {
		return domain.ErrOfferReserve
	}
	m.renews[reservationID]++
	return nil
}

func (m *Mock) Reverse(context.Context, string, string, string) error { return nil }

func (m *Mock) RenewCount(reservationID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.renews[reservationID]
}
