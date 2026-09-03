package ledgerclient

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
)

type Client struct {
	base   string
	http   *http.Client
	apiKey string
}

func New(base, apiKey string) *Client {
	return &Client{base: trim(base), http: &http.Client{Timeout: 800 * time.Millisecond}, apiKey: apiKey}
}

func (c *Client) GetBalance(ctx context.Context, tenantID, userID, assetCode string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/ledger/balances?tenant_id=%s&user_id=%s&asset_code=%s", c.base, tenantID, userID, assetCode), nil)
	if err != nil {
		return 0, err
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("ledger balance status %d", resp.StatusCode)
	}
	var out struct {
		Available int64 `json:"available"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Available, nil
}

func (c *Client) Freeze(ctx context.Context, req port.FreezeRequest) (string, error) {
	out, err := post[map[string]string](ctx, c, "/api/v1/ledger/freeze", req)
	if err != nil {
		return "", err
	}
	return (*out)["freeze_id"], nil
}

func (c *Client) Capture(ctx context.Context, freezeID, bizNo string) error {
	_, err := post[map[string]any](ctx, c, "/api/v1/ledger/capture", map[string]string{"freeze_id": freezeID, "biz_no": bizNo})
	return err
}

func (c *Client) Release(ctx context.Context, freezeID, bizNo string) error {
	_, err := post[map[string]any](ctx, c, "/api/v1/ledger/release", map[string]string{"freeze_id": freezeID, "biz_no": bizNo})
	return err
}

func (c *Client) Credit(ctx context.Context, tenantID, userID, assetCode string, amount int64, bizNo, relatedBizNo string) error {
	_, err := post[map[string]any](ctx, c, "/api/v1/ledger/credit", map[string]any{
		"tenant_id": tenantID, "user_id": userID, "asset_code": assetCode, "amount": amount,
		"biz_no": bizNo, "related_biz_no": relatedBizNo, "source_system": "order",
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
	if resp.StatusCode == 422 {
		return nil, domain.ErrLedgerFreeze
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ledger %s status %d", path, resp.StatusCode)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
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
	mu       sync.Mutex
	seq      int
	balance  map[string]int64
	freezes  map[string]freeze
	defaultB int64
}

type freeze struct {
	user   string
	asset  string
	amount int64
	open   bool
}

func NewMock() *Mock {
	return &Mock{
		balance:  map[string]int64{},
		freezes:  map[string]freeze{},
		defaultB: 100000,
	}
}

func (m *Mock) key(tenant, user, asset string) string { return tenant + "|" + user + "|" + asset }

func (m *Mock) GetBalance(_ context.Context, tenantID, userID, assetCode string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(tenantID, userID, assetCode)
	if v, ok := m.balance[k]; ok {
		return v, nil
	}
	m.balance[k] = m.defaultB
	return m.defaultB, nil
}

func (m *Mock) Freeze(_ context.Context, req port.FreezeRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(req.TenantID, req.UserID, req.AssetCode)
	bal, ok := m.balance[k]
	if !ok {
		bal = m.defaultB
		m.balance[k] = bal
	}
	if req.Amount > bal {
		return "", domain.ErrLedgerFreeze
	}
	m.balance[k] = bal - req.Amount
	m.seq++
	id := fmt.Sprintf("fz_%d", m.seq)
	m.freezes[id] = freeze{user: k, asset: req.AssetCode, amount: req.Amount, open: true}
	return id, nil
}

func (m *Mock) Capture(_ context.Context, freezeID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fz, ok := m.freezes[freezeID]
	if !ok {
		return domain.ErrLedgerFreeze
	}
	fz.open = false
	m.freezes[freezeID] = fz
	return nil
}

func (m *Mock) Release(_ context.Context, freezeID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fz, ok := m.freezes[freezeID]
	if !ok {
		return nil
	}
	if fz.open {
		m.balance[fz.user] += fz.amount
		fz.open = false
		m.freezes[freezeID] = fz
	}
	return nil
}

func (m *Mock) Credit(_ context.Context, tenantID, userID, assetCode string, amount int64, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(tenantID, userID, assetCode)
	m.balance[k] += amount
	return nil
}
