package offerclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

func TestHTTPClientMatchesOfferHubContract(t *testing.T) {
	var got struct {
		method, path, tenant, apiKey, idem string
		body                               map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		got.tenant = r.Header.Get("X-Tenant-Id")
		got.apiKey = r.Header.Get("X-API-Key")
		got.idem = r.Header.Get("Idempotency-Key")
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &got.body)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/discount/v1/quotes":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"quote_id":"q1","currency":"CNY","original_amount":10000,"discount_amount":1000,"payable_amount":9000,"expires_at":"2030-01-01T00:00:00Z","applications":[{"source_type":"coupon","source_id":"c1","discount_amount":1000,"allocations":[{"line_id":"l1","discount_amount":1000}]}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/discount/v1/reservations":
			if got.idem == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":"IDEMPOTENCY_KEY_REQUIRED","message":"缺少 Idempotency-Key"}}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"reservation_id":"rsv1","status":"ACTIVE","expires_at":"2030-01-01T00:00:00Z"}}`))
		case r.URL.Path == "/api/discount/v1/reservations/rsv1/commit":
			_, _ = w.Write([]byte(`{"data":{"redemption_id":"rd1","reservation_id":"rsv1","discount_amount":1000}}`))
		case r.URL.Path == "/api/discount/v1/reservations/rsv1":
			_, _ = w.Write([]byte(`{"data":{"reservation_id":"rsv1","status":"COMMITTED","expires_at":"2030-01-01T00:00:00Z"}}`))
		case r.URL.Path == "/api/discount/v1/redemptions/rd1/reverse":
			_, _ = w.Write([]byte(`{"data":{"reversal_id":"rv1"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"not found"}}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "k_test")
	ctx := context.Background()
	q, err := c.Quote(ctx, port.QuoteRequest{
		TenantID: "tenant_001", UserID: "u1", OrderID: "ord1", Scene: "mall_checkout",
		Channel: "app", Currency: "CNY", CouponIDs: []string{"c1"}, AutoBest: false,
		Items: []domain.OrderLine{{LineID: "l1", ObjectType: "sku", ObjectID: "s1", Quantity: 1, UnitPrice: 10000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.QuoteID != "q1" || q.DiscountAmount != 1000 || len(q.Allocations) != 1 {
		t.Fatalf("quote %+v", q)
	}
	if got.tenant != "tenant_001" || got.apiKey != "k_test" {
		t.Fatalf("quote headers tenant=%s key=%s", got.tenant, got.apiKey)
	}
	if got.body["scene"] != "mall_checkout" {
		t.Fatalf("quote scene %v", got.body["scene"])
	}
	tx, _ := got.body["transaction"].(map[string]any)
	if tx["channel"] != "app" {
		t.Fatalf("transaction.channel %v", tx)
	}
	ids, _ := got.body["selected_coupon_ids"].([]any)
	if len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("selected_coupon_ids %v", got.body["selected_coupon_ids"])
	}

	res, err := c.Reserve(ctx, "tenant_001", "q1", "ord1", "order:ord1:reserve")
	if err != nil {
		t.Fatal(err)
	}
	if res.ReservationID != "rsv1" || got.idem != "order:ord1:reserve" {
		t.Fatalf("reserve %+v idem=%s", res, got.idem)
	}
	rid, err := c.Commit(ctx, "tenant_001", "rsv1", "ord1", "order:ord1:commit")
	if err != nil || rid != "rd1" {
		t.Fatalf("commit %s %v", rid, err)
	}
	gotRsv, err := c.GetReservation(ctx, "tenant_001", "rsv1")
	if err != nil || gotRsv.Status != "COMMITTED" {
		t.Fatalf("get rsv %+v %v", gotRsv, err)
	}
	if err := c.Reverse(ctx, "tenant_001", "rd1", "rfd1", 0, "order:rfd1:reverse"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.body["amount"]; ok {
		t.Fatalf("full reverse should omit amount, got %v", got.body)
	}
	if _, err := c.GetReservation(ctx, "tenant_001", "missing"); err != domain.ErrOrderNotFound {
		t.Fatalf("missing rsv err=%v", err)
	}
}

func TestMockReservationLifecycle(t *testing.T) {
	m := NewMock()
	ctx := context.Background()
	q, err := m.Quote(ctx, port.QuoteRequest{
		TenantID: "t1", UserID: "u", OrderID: "o1", Currency: "CNY", CouponIDs: []string{"c"},
		Items: []domain.OrderLine{{LineID: "l1", ObjectType: "sku", ObjectID: "s", Quantity: 1, UnitPrice: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rsv, err := m.Reserve(ctx, "t1", q.QuoteID, "o1", "k")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.GetReservation(ctx, "t1", rsv.ReservationID)
	if err != nil || got.Status != ReservationActive {
		t.Fatalf("%+v %v", got, err)
	}
	rid, err := m.Commit(ctx, "t1", rsv.ReservationID, "o1", "k")
	if err != nil || rid == "" {
		t.Fatal(err)
	}
	got, _ = m.GetReservation(ctx, "t1", rsv.ReservationID)
	if got.Status != ReservationCommitted {
		t.Fatalf("status %s", got.Status)
	}
	if err := m.Reverse(ctx, "t1", rid, "r1", 0, "k"); err != nil {
		t.Fatal(err)
	}
}
