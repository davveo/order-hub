package outbox

import (
	"context"
	"encoding/json"
	"log"

	"github.com/davveo/order-hub/internal/domain"
)

type LogPublisher struct{}

func (LogPublisher) Publish(_ context.Context, ev domain.Event) error {
	b, _ := json.Marshal(ev)
	log.Printf("outbox %s %s", ev.EventType, string(b))
	return nil
}
