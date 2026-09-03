package memory

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type OrderRepo struct {
	mu       sync.Mutex
	orders   map[string]*domain.Order
	idem     map[string]*port.IdempotencyRecord
	events   []port.OutboxRow
	refunds  []domain.Refund
	tickets  []port.CompensationTicket
	seq      int64
	clientIx map[string]string
}

func NewOrderRepo() *OrderRepo {
	return &OrderRepo{
		orders:   map[string]*domain.Order{},
		idem:     map[string]*port.IdempotencyRecord{},
		clientIx: map[string]string{},
	}
}

func clone(o *domain.Order) *domain.Order {
	if o == nil {
		return nil
	}
	c := *o
	c.Lines = append([]domain.OrderLine(nil), o.Lines...)
	c.Promotions = append([]domain.PromotionDetail(nil), o.Promotions...)
	c.LedgerLegs = append([]domain.LedgerLeg(nil), o.LedgerLegs...)
	return &c
}

func idemKey(tenant, actor, key string) string { return tenant + "|" + actor + "|" + key }
func clientKey(tenant, buyer, cid string) string {
	return tenant + "|" + buyer + "|" + cid
}

func (r *OrderRepo) InsertCheckout(_ context.Context, rec port.CheckoutPersist) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec.Order == nil {
		return domain.ErrInvalidArgument
	}
	ck := clientKey(rec.Order.TenantID, rec.Order.BuyerUserID, rec.Order.ClientOrderID)
	if _, ok := r.clientIx[ck]; ok {
		return domain.ErrClientOrderConflict
	}
	if rec.Idempotency != nil {
		ik := idemKey(rec.Idempotency.TenantID, rec.Idempotency.Actor, rec.Idempotency.Key)
		if old, ok := r.idem[ik]; ok {
			if old.RequestHash != rec.Idempotency.RequestHash {
				return domain.ErrIdempotencyConflict
			}
			return domain.ErrIdempotencyConflict
		}
		cp := *rec.Idempotency
		r.idem[ik] = &cp
	}
	r.orders[rec.Order.TenantID+"|"+rec.Order.OrderID] = clone(rec.Order)
	r.clientIx[ck] = rec.Order.OrderID
	payload, _ := json.Marshal(rec.Event)
	r.events = append(r.events, port.OutboxRow{EventID: rec.Event.EventID, Payload: payload})
	return nil
}

func (r *OrderRepo) FindByID(_ context.Context, tenantID, orderID string) (*domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.orders {
		if o.OrderID == orderID && (tenantID == "" || o.TenantID == tenantID) {
			return clone(o), nil
		}
	}
	return nil, domain.ErrOrderNotFound
}

func (r *OrderRepo) FindByClientOrderID(_ context.Context, tenantID, buyerID, clientOrderID string) (*domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.clientIx[clientKey(tenantID, buyerID, clientOrderID)]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	return clone(r.orders[tenantID+"|"+id]), nil
}

func (r *OrderRepo) FindIdempotency(_ context.Context, tenantID, actor, key string) (*port.IdempotencyRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.idem[idemKey(tenantID, actor, key)]
	if !ok {
		return nil, nil
	}
	cp := *rec
	return &cp, nil
}

func (r *OrderRepo) UpdateIdempotencyResponse(_ context.Context, tenantID, actor, key string, resp []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.idem[idemKey(tenantID, actor, key)]
	if !ok {
		return nil
	}
	rec.Response = append([]byte(nil), resp...)
	return nil
}

func (r *OrderRepo) Transition(_ context.Context, cmd port.TransitionCmd) (*domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[cmd.TenantID+"|"+cmd.OrderID]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	if o.Version != cmd.Version {
		return nil, domain.ErrVersionConflict
	}
	if len(cmd.From) > 0 {
		okFrom := false
		for _, s := range cmd.From {
			if o.Status == s {
				okFrom = true
				break
			}
		}
		if !okFrom {
			return nil, domain.ErrVersionConflict
		}
	}
	if err := domain.Transition(o, cmd.To); err != nil {
		return nil, err
	}
	if cmd.PaidAmount != nil {
		o.Amounts.Paid = *cmd.PaidAmount
	}
	if cmd.RefundedAdd > 0 {
		o.Amounts.Refunded += cmd.RefundedAdd
	}
	if cmd.RedemptionID != "" {
		o.Promotion.RedemptionID = cmd.RedemptionID
	}
	if cmd.PaymentIntentID != "" {
		o.Payment.PaymentIntentID = cmd.PaymentIntentID
	}
	o.PaidAt = cmd.PaidAt
	o.CancelledAt = cmd.CancelledAt
	o.CompletedAt = cmd.CompletedAt
	o.UpdatedAt = time.Now()
	if cmd.Event != nil {
		payload, _ := json.Marshal(cmd.Event)
		r.events = append(r.events, port.OutboxRow{EventID: cmd.Event.EventID, Payload: payload})
	}
	return clone(o), nil
}

func (r *OrderRepo) UpdatePaymentIntent(_ context.Context, tenantID, orderID, intentID, channel string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[tenantID+"|"+orderID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	o.Payment.PaymentIntentID = intentID
	o.Payment.Channel = channel
	return nil
}

func (r *OrderRepo) UpdateRedemption(_ context.Context, tenantID, orderID, redemptionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[tenantID+"|"+orderID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	o.Promotion.RedemptionID = redemptionID
	return nil
}

func (r *OrderRepo) ListExpiredPending(_ context.Context, now time.Time, limit int) ([]domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Order
	for _, o := range r.orders {
		if o.Status == domain.StatusPendingPay && !o.ExpireAt.IsZero() && o.ExpireAt.Before(now) {
			out = append(out, *clone(o))
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (r *OrderRepo) ListByBuyer(_ context.Context, tenantID, buyerID string, status domain.Status, scene, cursor string, limit int) ([]domain.Order, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []*domain.Order
	for _, o := range r.orders {
		if o.TenantID != tenantID || o.BuyerUserID != buyerID {
			continue
		}
		if status != "" && o.Status != status {
			continue
		}
		if scene != "" && o.Scene != scene {
			continue
		}
		all = append(all, o)
	}
	out := make([]domain.Order, 0, len(all))
	for _, o := range all {
		out = append(out, *clone(o))
	}
	if len(out) > limit && limit > 0 {
		return out[:limit], out[limit-1].OrderID, nil
	}
	_ = cursor
	return out, "", nil
}

func (r *OrderRepo) InsertRefund(_ context.Context, _ *domain.Order, refund domain.Refund, _ domain.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refunds = append(r.refunds, refund)
	return nil
}

func (r *OrderRepo) InsertCompensation(_ context.Context, t port.CompensationTicket) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	t.ID = r.seq
	if t.Status == "" {
		t.Status = "pending"
	}
	t.CreatedAt = time.Now()
	r.tickets = append(r.tickets, t)
	return nil
}

func (r *OrderRepo) ClaimCompensations(_ context.Context, now time.Time, limit int) ([]port.CompensationTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []port.CompensationTicket
	for i := range r.tickets {
		t := &r.tickets[i]
		if t.Status != "pending" && t.Status != "running" {
			continue
		}
		if t.NextRetry != nil && t.NextRetry.After(now) {
			continue
		}
		t.Attempts++
		t.Status = "running"
		out = append(out, *t)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *OrderRepo) UpdateCompensation(_ context.Context, id int64, status, lastErr string, nextRetry *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.tickets {
		if r.tickets[i].ID == id {
			r.tickets[i].Status = status
			r.tickets[i].LastError = lastErr
			r.tickets[i].NextRetry = nextRetry
			return nil
		}
	}
	return domain.ErrOrderNotFound
}

func (r *OrderRepo) ListCompensations(_ context.Context, status string, limit int) ([]port.CompensationTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []port.CompensationTicket
	for i := len(r.tickets) - 1; i >= 0; i-- {
		if status != "" && r.tickets[i].Status != status {
			continue
		}
		out = append(out, r.tickets[i])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *OrderRepo) FindCompensation(_ context.Context, id int64) (*port.CompensationTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.tickets {
		if r.tickets[i].ID == id {
			t := r.tickets[i]
			return &t, nil
		}
	}
	return nil, domain.ErrOrderNotFound
}

func (r *OrderRepo) ListPendingPayForRenew(_ context.Context, now time.Time, window time.Duration, maxRenew, limit int) ([]domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deadline := now.Add(window)
	var out []domain.Order
	for _, o := range r.orders {
		if o.Status != domain.StatusPendingPay || o.Promotion.ReservationID == "" {
			continue
		}
		if o.ExpireAt.Before(now) || o.ExpireAt.After(deadline) || o.RenewCount >= maxRenew {
			continue
		}
		out = append(out, *clone(o))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *OrderRepo) BumpRenew(_ context.Context, tenantID, orderID string, expectedCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[tenantID+"|"+orderID]
	if !ok {
		return domain.ErrOrderNotFound
	}
	if o.Status != domain.StatusPendingPay || o.RenewCount != expectedCount {
		return domain.ErrVersionConflict
	}
	o.RenewCount++
	return nil
}

func (r *OrderRepo) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.orders)
}

func (r *OrderRepo) TicketCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, t := range r.tickets {
		if t.Status == "pending" || t.Status == "running" {
			n++
		}
	}
	return n
}

func (r *OrderRepo) ListUnpublishedEvents(_ context.Context, limit int) ([]port.OutboxRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) > limit {
		return r.events[:limit], nil
	}
	return append([]port.OutboxRow(nil), r.events...), nil
}

func (r *OrderRepo) MarkEventPublished(_ context.Context, eventID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := r.events[:0]
	for _, e := range r.events {
		if e.EventID != eventID {
			filtered = append(filtered, e)
		}
	}
	r.events = filtered
	return nil
}
