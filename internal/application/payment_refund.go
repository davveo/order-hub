package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type PaymentCallback struct {
	OrderID         string
	TenantID        string
	PaymentIntentID string
	Success         bool
	PaidAmount      int64
	Channel         string
	TraceID         string
}

type PaymentService struct {
	scenes  map[string]domain.SceneConfig
	repo    port.OrderRepository
	offer   port.OfferClient
	ledger  port.LedgerClient
	pay     port.PaymentAdapter
	fulfill port.FulfillmentRegistry
	ids     port.IDGenerator
	clock   port.Clock
}

func NewPaymentService(
	scenes map[string]domain.SceneConfig,
	repo port.OrderRepository,
	offer port.OfferClient,
	ledger port.LedgerClient,
	pay port.PaymentAdapter,
	fulfill port.FulfillmentRegistry,
	ids port.IDGenerator,
	clock port.Clock,
) *PaymentService {
	return &PaymentService{scenes: scenes, repo: repo, offer: offer, ledger: ledger, pay: pay, fulfill: fulfill, ids: ids, clock: clock}
}

func (s *PaymentService) RecreateIntent(ctx context.Context, ident *port.Identity, orderID string) (*port.PaymentIntent, error) {
	o, err := s.repo.FindByID(ctx, ident.TenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.BuyerUserID != ident.UserID {
		return nil, domain.ErrOrderNotFound
	}
	if o.Status != domain.StatusPendingPay {
		return nil, domain.ErrStatusNotAllowed
	}
	if !o.NeedsChannelPay() {
		return nil, fmt.Errorf("%w: no channel pay", domain.ErrInvalidArgument)
	}
	intent, err := s.pay.CreateIntent(ctx, o)
	if err != nil {
		return nil, err
	}
	_ = s.repo.UpdatePaymentIntent(ctx, o.TenantID, o.OrderID, intent.IntentID, intent.Channel)
	return intent, nil
}

func (s *PaymentService) ConfirmLedger(ctx context.Context, ident *port.Identity, orderID string) (*domain.Order, error) {
	o, err := s.repo.FindByID(ctx, ident.TenantID, orderID)
	if err != nil {
		return nil, err
	}
	if ident.UserID != "" && o.BuyerUserID != ident.UserID {
		return nil, domain.ErrOrderNotFound
	}
	if o.NeedsChannelPay() {
		return nil, fmt.Errorf("%w: channel pay required", domain.ErrStatusNotAllowed)
	}
	return s.onPaid(ctx, o, o.Amounts.Payable, ident.TraceID)
}

func (s *PaymentService) OnPaymentCallback(ctx context.Context, cb PaymentCallback) (*domain.Order, error) {
	if cb.OrderID == "" {
		return nil, fmt.Errorf("%w: order_id", domain.ErrInvalidArgument)
	}
	o, err := s.repo.FindByID(ctx, cb.TenantID, cb.OrderID)
	if err != nil {
		return nil, err
	}
	if !cb.Success {
		return o, nil
	}
	if cb.PaidAmount > 0 && cb.PaidAmount != o.Amounts.ChannelPay && o.Amounts.ChannelPay > 0 {
		return nil, fmt.Errorf("%w: paid amount mismatch", domain.ErrAmountInvariant)
	}
	if o.Payment.PaymentIntentID != "" && cb.PaymentIntentID != "" && o.Payment.PaymentIntentID != cb.PaymentIntentID {
		return nil, fmt.Errorf("%w: payment intent mismatch", domain.ErrInvalidArgument)
	}
	paid := o.Amounts.Payable
	return s.onPaid(ctx, o, paid, cb.TraceID)
}

func (s *PaymentService) onPaid(ctx context.Context, o *domain.Order, paid int64, traceID string) (*domain.Order, error) {
	if o.Status != domain.StatusPendingPay {
		if o.Status == domain.StatusPaid || o.Status == domain.StatusFulfilling || o.Status == domain.StatusCompleted {
			return o, nil
		}
		return nil, domain.ErrStatusNotAllowed
	}
	now := s.clock.Now()
	ev := domain.NewEvent(s.ids.EventID(), domain.EventPaid, o.TenantID, traceID, now, domain.OrderEventData(o))
	updated, err := s.repo.Transition(ctx, port.TransitionCmd{
		TenantID:   o.TenantID,
		OrderID:    o.OrderID,
		From:       []domain.Status{domain.StatusPendingPay},
		To:         domain.StatusPaid,
		Version:    o.Version,
		PaidAmount: &paid,
		PaidAt:     &now,
		Event:      &ev,
	})
	if err != nil {
		if errors.Is(err, domain.ErrVersionConflict) {
			cur, e := s.repo.FindByID(ctx, o.TenantID, o.OrderID)
			if e == nil && (cur.Status == domain.StatusPaid || cur.Status == domain.StatusFulfilling || cur.Status == domain.StatusCompleted) {
				return cur, nil
			}
		}
		return nil, err
	}

	if err := s.afterPaid(ctx, updated); err != nil {
		_ = s.repo.InsertCompensation(ctx, "after_paid", updated.OrderID, err.Error())
	}
	return s.repo.FindByID(ctx, updated.TenantID, updated.OrderID)
}

func (s *PaymentService) afterPaid(ctx context.Context, o *domain.Order) error {
	scene := s.scenes[o.Scene]
	if o.HasOfferReservation() {
		rid, err := s.offer.Commit(ctx, o.Promotion.ReservationID, o.OrderID, "order:"+o.OrderID+":commit")
		if err != nil {
			return err
		}
		if rid != "" {
			_ = s.repo.UpdateRedemption(ctx, o.TenantID, o.OrderID, rid)
			o.Promotion.RedemptionID = rid
		}
	}
	if o.HasLedgerFreeze() {
		if err := s.ledger.Capture(ctx, o.Ledger.FreezeID, "order:capture:"+o.OrderID); err != nil {
			return err
		}
	}
	if adapter := s.fulfill.ForScene(o.Scene); adapter != nil {
		if err := adapter.Commit(ctx, o); err != nil {
			return err
		}
	}
	next := domain.NextAfterPaid(scene.AutoCompleteOnPaid)
	now := s.clock.Now()
	cmd := port.TransitionCmd{
		TenantID: o.TenantID,
		OrderID:  o.OrderID,
		From:     []domain.Status{domain.StatusPaid},
		To:       next,
		Version:  o.Version,
	}
	if next == domain.StatusCompleted {
		cmd.CompletedAt = &now
		ev := domain.NewEvent(s.ids.EventID(), domain.EventCompleted, o.TenantID, "", now, domain.OrderEventData(o))
		cmd.Event = &ev
	} else {
		ev := domain.NewEvent(s.ids.EventID(), domain.EventFulfilled, o.TenantID, "", now, domain.OrderEventData(o))
		cmd.Event = &ev
	}
	_, err := s.repo.Transition(ctx, cmd)
	return err
}

func (s *PaymentService) Complete(ctx context.Context, tenantID, orderID string) (*domain.Order, error) {
	o, err := s.repo.FindByID(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.Status != domain.StatusFulfilling && o.Status != domain.StatusPaid {
		return nil, domain.ErrStatusNotAllowed
	}
	now := s.clock.Now()
	ev := domain.NewEvent(s.ids.EventID(), domain.EventCompleted, o.TenantID, "", now, domain.OrderEventData(o))
	return s.repo.Transition(ctx, port.TransitionCmd{
		TenantID:    o.TenantID,
		OrderID:     o.OrderID,
		From:        []domain.Status{domain.StatusFulfilling, domain.StatusPaid},
		To:          domain.StatusCompleted,
		Version:     o.Version,
		CompletedAt: &now,
		Event:       &ev,
	})
}

type RefundCmd struct {
	Amount int64
	Reason string
}

type RefundService struct {
	repo    port.OrderRepository
	offer   port.OfferClient
	ledger  port.LedgerClient
	pay     port.PaymentAdapter
	ids     port.IDGenerator
	clock   port.Clock
}

func NewRefundService(repo port.OrderRepository, offer port.OfferClient, ledger port.LedgerClient, pay port.PaymentAdapter, ids port.IDGenerator, clock port.Clock) *RefundService {
	return &RefundService{repo: repo, offer: offer, ledger: ledger, pay: pay, ids: ids, clock: clock}
}

func (s *RefundService) Refund(ctx context.Context, ident *port.Identity, orderID string, cmd RefundCmd) (*domain.Refund, error) {
	o, err := s.repo.FindByID(ctx, ident.TenantID, orderID)
	if err != nil {
		return nil, err
	}
	if ident.UserID != "" && o.BuyerUserID != ident.UserID {
		return nil, domain.ErrOrderNotFound
	}
	switch o.Status {
	case domain.StatusPaid, domain.StatusFulfilling, domain.StatusCompleted, domain.StatusPartialRefunded:
	default:
		return nil, domain.ErrStatusNotAllowed
	}
	if cmd.Amount <= 0 {
		cmd.Amount = o.RefundableAmount()
	}
	if cmd.Amount <= 0 || cmd.Amount > o.RefundableAmount() {
		return nil, domain.ErrRefundExceedsPaid
	}
	now := s.clock.Now()
	refund := domain.Refund{
		RefundID:  s.ids.RefundID(),
		OrderID:   o.OrderID,
		TenantID:  o.TenantID,
		Amount:    cmd.Amount,
		Currency:  o.Amounts.Currency,
		Status:    "processing",
		Reason:    cmd.Reason,
		CreatedAt: now,
	}
	full := cmd.Amount == o.RefundableAmount() && o.Amounts.Refunded+cmd.Amount == o.Amounts.Paid
	next := domain.StatusPartialRefunded
	if full {
		next = domain.StatusRefunded
	}
	if o.NeedsChannelPay() && s.pay != nil {
		refund.ChannelRefund = true
		if err := s.pay.Refund(ctx, refund); err != nil {
			return nil, err
		}
	}
	if o.HasLedgerFreeze() && o.Amounts.LedgerPay > 0 {
		refund.LedgerCredit = true
		share := o.Amounts.LedgerPay
		if !full {
			share = o.Amounts.LedgerPay * cmd.Amount / o.Amounts.Paid
		}
		if err := s.ledger.Credit(ctx, o.TenantID, o.BuyerUserID, o.Ledger.AssetCode, share, "order:refund:"+refund.RefundID, "order:capture:"+o.OrderID); err != nil {
			return nil, err
		}
	}
	if o.Promotion.RedemptionID != "" {
		_ = s.offer.Reverse(ctx, o.Promotion.RedemptionID, refund.RefundID, "order:"+refund.RefundID+":reverse")
	}
	refund.Status = "succeeded"
	ev := domain.NewEvent(s.ids.EventID(), domain.EventRefunded, o.TenantID, ident.TraceID, now, map[string]any{
		"order_id":  o.OrderID,
		"refund_id": refund.RefundID,
		"amount":    refund.Amount,
	})
	if _, err := s.repo.Transition(ctx, port.TransitionCmd{
		TenantID:    o.TenantID,
		OrderID:     o.OrderID,
		From:        []domain.Status{o.Status},
		To:          next,
		Version:     o.Version,
		RefundedAdd: cmd.Amount,
		Event:       &ev,
	}); err != nil {
		return nil, err
	}
	_ = s.repo.InsertRefund(ctx, o, refund, ev)
	return &refund, nil
}

type OutboxWorker struct {
	repo      port.OrderRepository
	publisher port.EventPublisher
}

func NewOutboxWorker(repo port.OrderRepository, publisher port.EventPublisher) *OutboxWorker {
	return &OutboxWorker{repo: repo, publisher: publisher}
}

func (w *OutboxWorker) Tick(ctx context.Context) (int, error) {
	rows, err := w.repo.ListUnpublishedEvents(ctx, 200)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		var ev domain.Event
	if err := json.Unmarshal(row.Payload, &ev); err != nil {
			continue
		}
		if w.publisher != nil {
			if err := w.publisher.Publish(ctx, ev); err != nil {
				continue
			}
		}
		if err := w.repo.MarkEventPublished(ctx, row.EventID); err != nil {
			continue
		}
		n++
	}
	return n, nil
}
