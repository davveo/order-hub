package payment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type Mock struct {
	mu      sync.Mutex
	seq     int
	intents map[string]*port.PaymentIntent
}

func NewMock() *Mock { return &Mock{intents: map[string]*port.PaymentIntent{}} }

func (m *Mock) CreateIntent(_ context.Context, o *domain.Order) (*port.PaymentIntent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("pi_mock_%d", m.seq)
	it := &port.PaymentIntent{
		IntentID:  id,
		Channel:   "mock",
		Amount:    o.Amounts.ChannelPay,
		Currency:  o.Amounts.Currency,
		PayURL:    "mock://pay/" + id,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	m.intents[id] = it
	return it, nil
}

func (m *Mock) CloseIntent(_ context.Context, intentID string) error {
	m.mu.Lock()
	delete(m.intents, intentID)
	m.mu.Unlock()
	return nil
}

func (m *Mock) Refund(_ context.Context, _ domain.Refund) error { return nil }
