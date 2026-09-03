package domain

import "time"

const (
	EventCreated   = "order.created.v1"
	EventPaid      = "order.paid.v1"
	EventFulfilled = "order.fulfilled.v1"
	EventCompleted = "order.completed.v1"
	EventCancelled = "order.cancelled.v1"
	EventRefunded  = "order.refunded.v1"
)

type Event struct {
	EventID      string         `json:"event_id"`
	EventType    string         `json:"event_type"`
	EventVersion int            `json:"event_version"`
	TenantID     string         `json:"tenant_id"`
	OccurredAt   time.Time      `json:"occurred_at"`
	Source       string         `json:"source"`
	TraceID      string         `json:"trace_id"`
	Data         map[string]any `json:"data"`
}

func NewEvent(eventID, eventType, tenantID, traceID string, now time.Time, data map[string]any) Event {
	return Event{
		EventID:      eventID,
		EventType:    eventType,
		EventVersion: 1,
		TenantID:     tenantID,
		OccurredAt:   now.UTC(),
		Source:       "order-hub",
		TraceID:      traceID,
		Data:         data,
	}
}

func OrderEventData(o *Order) map[string]any {
	return map[string]any{
		"order_id":       o.OrderID,
		"user_id":        o.BuyerUserID,
		"scene":          o.Scene,
		"status":         string(o.Status),
		"payable_amount": o.Amounts.Payable,
		"currency":       o.Amounts.Currency,
		"redemption_id":  o.Promotion.RedemptionID,
		"freeze_id":      o.Ledger.FreezeID,
	}
}
