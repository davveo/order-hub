package offerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
	"github.com/davveo/order-hub/internal/infra/httpx"
)

const (
	ReservationActive    = domain.OfferReservationActive
	ReservationCommitted = domain.OfferReservationCommitted
	ReservationReleased  = domain.OfferReservationReleased
)

type Client struct {
	base   string
	http   *http.Client
	apiKey string
}

func New(base, apiKey string) *Client {
	return &Client{base: trim(base), http: &http.Client{Timeout: 2 * time.Second}, apiKey: apiKey}
}

type quoteWire struct {
	QuoteID        string    `json:"quote_id"`
	Currency       string    `json:"currency"`
	OriginalAmount int64     `json:"original_amount"`
	DiscountAmount int64     `json:"discount_amount"`
	PayableAmount  int64     `json:"payable_amount"`
	ExpiresAt      time.Time `json:"expires_at"`
	ContextHash    string    `json:"context_hash"`
	Applications   []struct {
		SourceType     string              `json:"source_type"`
		SourceID       string              `json:"source_id"`
		DiscountAmount int64               `json:"discount_amount"`
		Allocations    []domain.Allocation `json:"allocations"`
	} `json:"applications"`
}

func (c *Client) Quote(ctx context.Context, req port.QuoteRequest) (*port.QuoteResult, error) {
	var original int64
	items := make([]map[string]any, 0, len(req.Items))
	for _, it := range req.Items {
		original += it.UnitPrice * it.Quantity
		item := map[string]any{
			"line_id": it.LineID, "object_type": it.ObjectType, "object_id": it.ObjectID,
			"quantity": it.Quantity, "unit_price": it.UnitPrice,
		}
		if len(it.Attributes) > 0 {
			attrs := map[string]any{}
			for k, v := range it.Attributes {
				attrs[k] = v
			}
			item["attributes"] = attrs
		}
		items = append(items, item)
	}
	body := map[string]any{
		"scene": req.Scene,
		"subject": map[string]any{
			"type": "user", "id": req.UserID, "attributes": req.Attributes,
		},
		"transaction": map[string]any{
			"id": req.OrderID, "amount": original, "currency": req.Currency, "channel": req.Channel,
		},
		"items":               items,
		"auto_best":           req.AutoBest,
		"selected_coupon_ids": req.CouponIDs,
		"context":             req.Context,
	}
	var wire quoteWire
	if err := c.do(ctx, http.MethodPost, req.TenantID, "", "/api/discount/v1/quotes", body, &wire); err != nil {
		return nil, err
	}
	return mapQuote(wire), nil
}

func mapQuote(wire quoteWire) *port.QuoteResult {
	byLine := map[string]int64{}
	promos := make([]domain.PromotionDetail, 0, len(wire.Applications))
	for _, app := range wire.Applications {
		promos = append(promos, domain.PromotionDetail{
			SourceType: app.SourceType, SourceID: app.SourceID,
			DiscountAmount: app.DiscountAmount, Allocations: app.Allocations,
		})
		for _, a := range app.Allocations {
			byLine[a.LineID] += a.DiscountAmount
		}
	}
	allocs := make([]domain.Allocation, 0, len(byLine))
	for lineID, amt := range byLine {
		allocs = append(allocs, domain.Allocation{LineID: lineID, DiscountAmount: amt})
	}
	return &port.QuoteResult{
		QuoteID:        wire.QuoteID,
		Currency:       wire.Currency,
		OriginalAmount: wire.OriginalAmount,
		DiscountAmount: wire.DiscountAmount,
		PayableAmount:  wire.PayableAmount,
		Allocations:    allocs,
		Promotions:     promos,
		ExpiresAt:      wire.ExpiresAt,
		ContextHash:    wire.ContextHash,
	}
}

func (c *Client) Reserve(ctx context.Context, tenantID, quoteID, orderID, idemKey string) (*port.ReservationResult, error) {
	var out port.ReservationResult
	err := c.do(ctx, http.MethodPost, tenantID, idemKey, "/api/discount/v1/reservations", map[string]any{
		"quote_id": quoteID, "business_order_id": orderID,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Commit(ctx context.Context, tenantID, reservationID, _, idemKey string) (string, error) {
	var out port.RedemptionResult
	err := c.do(ctx, http.MethodPost, tenantID, idemKey, "/api/discount/v1/reservations/"+reservationID+"/commit", map[string]any{}, &out)
	if err != nil {
		return "", err
	}
	return out.RedemptionID, nil
}

func (c *Client) Release(ctx context.Context, tenantID, reservationID, _, idemKey string) error {
	return c.do(ctx, http.MethodPost, tenantID, idemKey, "/api/discount/v1/reservations/"+reservationID+"/release", map[string]any{}, nil)
}

func (c *Client) Renew(ctx context.Context, tenantID, reservationID, _, idemKey string) error {
	return c.do(ctx, http.MethodPost, tenantID, idemKey, "/api/discount/v1/reservations/"+reservationID+"/renew", map[string]any{}, nil)
}

func (c *Client) Reverse(ctx context.Context, tenantID, redemptionID, refundID string, amount int64, idemKey string) error {
	_ = refundID
	body := map[string]any{}
	if amount > 0 {
		body["amount"] = amount
	}
	return c.do(ctx, http.MethodPost, tenantID, idemKey, "/api/discount/v1/redemptions/"+redemptionID+"/reverse", body, nil)
}

func (c *Client) GetReservation(ctx context.Context, tenantID, reservationID string) (*port.ReservationResult, error) {
	var out port.ReservationResult
	if err := c.do(ctx, http.MethodGet, tenantID, "", "/api/discount/v1/reservations/"+reservationID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetRedemption(ctx context.Context, tenantID, redemptionID string) (*port.RedemptionResult, error) {
	var out port.RedemptionResult
	if err := c.do(ctx, http.MethodGet, tenantID, "", "/api/discount/v1/redemptions/"+redemptionID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, tenantID, idemKey, path string, payload any, dest any) error {
	var rdr io.Reader
	if payload != nil && method != http.MethodGet {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tenantID != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return httpx.Decode(resp, dest)
}

func trim(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

type mockResv struct {
	quoteID      string
	orderID      string
	status       string
	expires      time.Time
	redemptionID string
}

type Mock struct {
	mu     sync.Mutex
	seq    int
	quotes map[string]*port.QuoteResult
	resv   map[string]*mockResv
	redeem map[string]*port.RedemptionResult
	renews map[string]int
}

func NewMock() *Mock {
	return &Mock{
		quotes: map[string]*port.QuoteResult{},
		resv:   map[string]*mockResv{},
		redeem: map[string]*port.RedemptionResult{},
		renews: map[string]int{},
	}
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

func (m *Mock) Reserve(_ context.Context, _, quoteID, orderID, _ string) (*port.ReservationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.quotes[quoteID]; !ok {
		return nil, domain.ErrOfferReserve
	}
	rid := "rsv_" + orderID
	exp := time.Now().Add(15 * time.Minute)
	m.resv[rid] = &mockResv{quoteID: quoteID, orderID: orderID, status: ReservationActive, expires: exp}
	return &port.ReservationResult{ReservationID: rid, QuoteID: quoteID, ExpiresAt: exp, Status: ReservationActive}, nil
}

func (m *Mock) Commit(_ context.Context, _, reservationID, _, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rsv, ok := m.resv[reservationID]
	if !ok {
		return "", domain.ErrOfferReserve
	}
	if rsv.status == ReservationReleased {
		return "", domain.ErrOfferReserve
	}
	if rsv.redemptionID != "" {
		return rsv.redemptionID, nil
	}
	rid := "rd_" + reservationID
	rsv.status = ReservationCommitted
	rsv.redemptionID = rid
	var discount int64
	if q := m.quotes[rsv.quoteID]; q != nil {
		discount = q.DiscountAmount
	}
	m.redeem[rid] = &port.RedemptionResult{RedemptionID: rid, ReservationID: reservationID, DiscountAmount: discount}
	return rid, nil
}

func (m *Mock) Release(_ context.Context, _, reservationID, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rsv, ok := m.resv[reservationID]; ok {
		rsv.status = ReservationReleased
	}
	return nil
}

func (m *Mock) Renew(_ context.Context, _, reservationID, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rsv, ok := m.resv[reservationID]
	if !ok || rsv.status != ReservationActive {
		return domain.ErrOfferReserve
	}
	m.renews[reservationID]++
	rsv.expires = time.Now().Add(15 * time.Minute)
	return nil
}

func (m *Mock) Reverse(_ context.Context, _, redemptionID, _ string, amount int64, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, ok := m.redeem[redemptionID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	remain := rd.DiscountAmount - rd.ReversedAmount
	if amount <= 0 {
		amount = remain
	}
	if amount == 0 {
		return nil
	}
	if amount < 0 || amount > remain {
		return domain.ErrInvalidArgument
	}
	rd.ReversedAmount += amount
	return nil
}

func (m *Mock) GetReservation(_ context.Context, _, reservationID string) (*port.ReservationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rsv, ok := m.resv[reservationID]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	return &port.ReservationResult{
		ReservationID: reservationID, QuoteID: rsv.quoteID, ExpiresAt: rsv.expires, Status: rsv.status,
	}, nil
}

func (m *Mock) GetRedemption(_ context.Context, _, redemptionID string) (*port.RedemptionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, ok := m.redeem[redemptionID]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	cp := *rd
	return &cp, nil
}

func (m *Mock) RenewCount(reservationID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.renews[reservationID]
}

func (m *Mock) ForceStatus(reservationID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rsv, ok := m.resv[reservationID]; ok {
		rsv.status = status
	}
}
