package authclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 800 * time.Millisecond}, apiKey: apiKey}
}

func (c *Client) Introspect(ctx context.Context, token string) (*port.Identity, error) {
	body, _ := json.Marshal(map[string]string{"token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/auth/introspect", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, domain.ErrUnauthorized
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("auth introspect status %d", resp.StatusCode)
	}
	var out struct {
		UserID     string         `json:"user_id"`
		TenantID   string         `json:"tenant_id"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.UserID == "" {
		return nil, domain.ErrUnauthorized
	}
	return &port.Identity{UserID: out.UserID, TenantID: out.TenantID, Attributes: out.Attributes}, nil
}

type Mock struct {
	DefaultTenant string
}

func (m Mock) Introspect(_ context.Context, token string) (*port.Identity, error) {
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return nil, domain.ErrUnauthorized
	}
	userID := token
	tenant := m.DefaultTenant
	if tenant == "" {
		tenant = "tenant_001"
	}
	if strings.HasPrefix(token, "mock.") {
		parts := strings.Split(token, ".")
		if len(parts) >= 2 {
			userID = parts[1]
		}
		if len(parts) >= 3 && parts[2] != "" {
			tenant = parts[2]
		}
	}
	return &port.Identity{
		UserID:   userID,
		TenantID: tenant,
		Attributes: map[string]any{
			"member_level": "gold",
			"is_new_user":  false,
		},
	}, nil
}
