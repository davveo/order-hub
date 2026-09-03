package fulfillment

import (
	"context"
	"sync"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type Registry struct {
	byScene map[string]port.FulfillmentAdapter
	def     port.FulfillmentAdapter
}

func NewRegistry(scenes map[string]domain.SceneConfig) *Registry {
	r := &Registry{byScene: map[string]port.FulfillmentAdapter{}, def: Noop{}}
	shipping := &InMemory{kind: "shipping"}
	virtual := &InMemory{kind: "virtual"}
	entitlement := &InMemory{kind: "entitlement"}
	for name, sc := range scenes {
		switch sc.Fulfillment {
		case domain.FulfillmentShipping:
			r.byScene[name] = shipping
		case domain.FulfillmentVirtualGrant:
			r.byScene[name] = virtual
		case domain.FulfillmentEntitlement:
			r.byScene[name] = entitlement
		default:
			r.byScene[name] = Noop{}
		}
	}
	return r
}

func (r *Registry) ForScene(scene string) port.FulfillmentAdapter {
	if a, ok := r.byScene[scene]; ok {
		return a
	}
	return r.def
}

type Noop struct{}

func (Noop) Reserve(context.Context, *domain.Order) error { return nil }
func (Noop) Commit(context.Context, *domain.Order) error  { return nil }
func (Noop) Release(context.Context, *domain.Order) error { return nil }

type InMemory struct {
	kind string
	mu   sync.Mutex
	held map[string]string
}

func (a *InMemory) Reserve(_ context.Context, o *domain.Order) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.held == nil {
		a.held = map[string]string{}
	}
	a.held[o.OrderID] = "reserved:" + a.kind
	return nil
}

func (a *InMemory) Commit(_ context.Context, o *domain.Order) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.held == nil {
		a.held = map[string]string{}
	}
	a.held[o.OrderID] = "committed:" + a.kind
	return nil
}

func (a *InMemory) Release(_ context.Context, o *domain.Order) error {
	a.mu.Lock()
	if a.held != nil {
		delete(a.held, o.OrderID)
	}
	a.mu.Unlock()
	return nil
}
