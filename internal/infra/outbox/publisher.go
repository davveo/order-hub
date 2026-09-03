package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type LogPublisher struct{}

func (LogPublisher) Publish(_ context.Context, ev domain.Event) error {
	b, _ := json.Marshal(ev)
	log.Printf("outbox %s %s", ev.EventType, string(b))
	return nil
}

type HTTPPublisher struct {
	URL    string
	Client *http.Client
}

func (p HTTPPublisher) Publish(ctx context.Context, ev domain.Event) error {
	if p.URL == "" {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Type", ev.EventType)
	req.Header.Set("X-Tenant-Id", ev.TenantID)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook http %d", resp.StatusCode)
	}
	return nil
}

type Fanout struct {
	Pubs []port.EventPublisher
}

func (f Fanout) Publish(ctx context.Context, ev domain.Event) error {
	var first error
	for _, p := range f.Pubs {
		if p == nil {
			continue
		}
		if err := p.Publish(ctx, ev); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func NewPublisher(webhookURL string, logOutbox bool) port.EventPublisher {
	var pubs []port.EventPublisher
	if webhookURL != "" {
		pubs = append(pubs, HTTPPublisher{URL: webhookURL, Client: &http.Client{Timeout: 2 * time.Second}})
	}
	if logOutbox || len(pubs) == 0 {
		pubs = append(pubs, LogPublisher{})
	}
	if len(pubs) == 1 {
		return pubs[0]
	}
	return Fanout{Pubs: pubs}
}
