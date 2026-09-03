package httpserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davveo/order-hub/internal/boot"
	"github.com/davveo/order-hub/internal/conf"
)

func TestHTTPPreviewCheckoutPay(t *testing.T) {
	rt, err := boot.Build(conf.Config{
		PostgresDSN: "memory",
		MockDeps:    true,
		HTTPAddr:    ":0",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	previewBody := []byte(`{
		"scene":"mall_checkout","channel":"app","coupon_ids":["coupon_001"],
		"items":[
			{"line_id":"line_1","object_type":"sku","object_id":"sku_1001","quantity":2,"unit_price":10000},
			{"line_id":"line_2","object_type":"service","object_id":"delivery","quantity":1,"unit_price":6800}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/preview", bytes.NewReader(previewBody))
	req.Header.Set("Authorization", "Bearer mock.u_123.tenant_001")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}

	checkoutBody := []byte(`{
		"client_order_id":"cli_http_1","scene":"mall_checkout","channel":"app","coupon_ids":["coupon_001"],
		"items":[
			{"line_id":"line_1","object_type":"sku","object_id":"sku_1001","quantity":2,"unit_price":10000},
			{"line_id":"line_2","object_type":"service","object_id":"delivery","quantity":1,"unit_price":6800}
		]
	}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(checkoutBody))
	req.Header.Set("Authorization", "Bearer mock.u_123.tenant_001")
	req.Header.Set("Idempotency-Key", "cli_http_1")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("checkout %d %s", w.Code, w.Body.String())
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			OrderID string `json:"order_id"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Code != 0 || env.Data.OrderID == "" || env.Data.Status != "PENDING_PAY" {
		t.Fatalf("%+v", env)
	}

	cb, _ := json.Marshal(map[string]any{
		"order_id": env.Data.OrderID, "tenant_id": "tenant_001", "success": true,
	})
	req = httptest.NewRequest(http.MethodPost, "/internal/v1/orders/callbacks/payment", bytes.NewReader(cb))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("callback %d %s", w.Code, w.Body.String())
	}
}

func TestAdminConsole(t *testing.T) {
	rt, err := boot.Build(conf.Config{
		PostgresDSN: "memory",
		MockDeps:    true,
		AdminToken:  "dev-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("Order Hub")) {
		t.Fatalf("root page %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("Order Hub")) {
		t.Fatalf("admin page %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/v1/stats", nil)
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("want 401 without token, got %d", w.Code)
	}

	checkoutBody := []byte(`{
		"client_order_id":"cli_admin_1","scene":"mall_checkout","channel":"app",
		"items":[{"line_id":"line_1","object_type":"sku","object_id":"sku_1001","quantity":1,"unit_price":1000}]
	}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(checkoutBody))
	req.Header.Set("Authorization", "Bearer mock.u_123.tenant_001")
	req.Header.Set("Idempotency-Key", "cli_admin_1")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("checkout %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/v1/orders", nil)
	req.Header.Set("X-Admin-Token", "dev-admin")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("admin list %d %s", w.Code, w.Body.String())
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				OrderID string `json:"order_id"`
				Status  string `json:"status"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Code != 0 || len(env.Data.Items) == 0 {
		t.Fatalf("%+v", env)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/v1/stats", nil)
	req.Header.Set("X-Admin-Token", "dev-admin")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("stats %d %s", w.Code, w.Body.String())
	}

	oid := env.Data.Items[0].OrderID
	req = httptest.NewRequest(http.MethodGet, "/admin/v1/orders/"+oid, nil)
	req.Header.Set("X-Admin-Token", "dev-admin")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("admin get %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/v1/orders/"+oid+"/cancel", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Admin-Token", "dev-admin")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("admin cancel %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/v1/compensations", nil)
	req.Header.Set("X-Admin-Token", "dev-admin")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("compensations %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/v1/seed", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Admin-Token", "dev-admin")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("seed %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"created"`)) {
		t.Fatalf("seed body %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	w = httptest.NewRecorder()
	rt.Engine.ServeHTTP(w, req)
	if !bytes.Contains(w.Body.Bytes(), []byte("测试下单")) || !bytes.Contains(w.Body.Bytes(), []byte("API Demo")) {
		t.Fatalf("admin page missing checkout/demo")
	}
}
