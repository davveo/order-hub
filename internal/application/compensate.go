package application

import (
	"context"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
)

const maxCompensationAttempts = 20

type CompensateWorker struct {
	repo    port.OrderRepository
	pay     *PaymentService
	offer   port.OfferClient
	ledger  port.LedgerClient
	fulfill port.FulfillmentRegistry
	clock   port.Clock
}

func NewCompensateWorker(
	repo port.OrderRepository,
	pay *PaymentService,
	offer port.OfferClient,
	ledger port.LedgerClient,
	fulfill port.FulfillmentRegistry,
	clock port.Clock,
) *CompensateWorker {
	return &CompensateWorker{repo: repo, pay: pay, offer: offer, ledger: ledger, fulfill: fulfill, clock: clock}
}

func (w *CompensateWorker) Tick(ctx context.Context) (int, error) {
	tickets, err := w.repo.ClaimCompensations(ctx, w.clock.Now(), 50)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range tickets {
		if err := w.handle(ctx, t); err != nil {
			status := "pending"
			var next *time.Time
			if t.Attempts >= maxCompensationAttempts {
				status = "failed"
			} else {
				d := w.clock.Now().Add(backoff(t.Attempts))
				next = &d
			}
			_ = w.repo.UpdateCompensation(ctx, t.ID, status, err.Error(), next)
			continue
		}
		_ = w.repo.UpdateCompensation(ctx, t.ID, "done", "", nil)
		n++
	}
	return n, nil
}

func (w *CompensateWorker) Retry(ctx context.Context, id int64) error {
	t, err := w.repo.FindCompensation(ctx, id)
	if err != nil {
		return err
	}
	if err := w.handle(ctx, *t); err != nil {
		next := w.clock.Now().Add(backoff(t.Attempts + 1))
		_ = w.repo.UpdateCompensation(ctx, t.ID, "pending", err.Error(), &next)
		return err
	}
	return w.repo.UpdateCompensation(ctx, t.ID, "done", "", nil)
}

func (w *CompensateWorker) List(ctx context.Context, status string, limit int) ([]port.CompensationTicket, error) {
	return w.repo.ListCompensations(ctx, status, limit)
}

func (w *CompensateWorker) handle(ctx context.Context, t port.CompensationTicket) error {
	switch t.Kind {
	case "after_paid":
		return w.pay.RetryAfterPaid(ctx, t.TenantID, t.Ref)
	case "offer_release":
		return w.offer.Release(ctx, t.Ref, t.Payload, "order:"+t.Payload+":release")
	case "ledger_release":
		return w.ledger.Release(ctx, t.Ref, "order:release:"+t.Payload)
	case "fulfill_release":
		o, err := w.repo.FindByID(ctx, t.TenantID, t.Ref)
		if err != nil {
			return err
		}
		if adapter := w.fulfill.ForScene(o.Scene); adapter != nil {
			return adapter.Release(ctx, o)
		}
		return nil
	case "offer_reverse":
		return w.offer.Reverse(ctx, t.Payload, t.Ref, "order:"+t.Ref+":reverse")
	default:
		return nil
	}
}

func backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	sec := 60
	if attempts < 7 {
		sec = 1 << (attempts - 1)
	}
	return time.Duration(sec) * time.Second
}
