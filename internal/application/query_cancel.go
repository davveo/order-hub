package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type QueryService struct {
	repo port.OrderRepository
}

func NewQueryService(repo port.OrderRepository) *QueryService {
	return &QueryService{repo: repo}
}

func (s *QueryService) Get(ctx context.Context, ident *port.Identity, orderID string) (*domain.Order, error) {
	o, err := s.repo.FindByID(ctx, ident.TenantID, orderID)
	if err != nil {
		return nil, err
	}
	if o.BuyerUserID != ident.UserID {
		return nil, domain.ErrOrderNotFound
	}
	return o, nil
}

func (s *QueryService) GetInternal(ctx context.Context, tenantID, orderID string) (*domain.Order, error) {
	return s.repo.FindByID(ctx, tenantID, orderID)
}

func (s *QueryService) List(ctx context.Context, ident *port.Identity, status, scene, cursor string, limit int) ([]domain.Order, string, error) {
	var st domain.Status
	if status != "" {
		parsed, ok := domain.ParseStatus(status)
		if !ok {
			return nil, "", fmt.Errorf("%w: status", domain.ErrInvalidArgument)
		}
		st = parsed
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListByBuyer(ctx, ident.TenantID, ident.UserID, st, scene, cursor, limit)
}

type AdminStats struct {
	Orders            map[string]int64 `json:"orders"`
	Compensations     map[string]int64 `json:"compensations"`
	UnpublishedEvents int64            `json:"unpublished_events"`
}

func (s *QueryService) AdminList(ctx context.Context, f port.AdminListFilter) ([]domain.Order, string, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	if f.Status != "" {
		if _, ok := domain.ParseStatus(string(f.Status)); !ok {
			return nil, "", fmt.Errorf("%w: status", domain.ErrInvalidArgument)
		}
	}
	return s.repo.AdminList(ctx, f)
}

func (s *QueryService) AdminStats(ctx context.Context, tenantID string) (*AdminStats, error) {
	orders, err := s.repo.CountOrdersByStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	comps, err := s.repo.CountCompensationsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.repo.CountUnpublishedEvents(ctx)
	if err != nil {
		return nil, err
	}
	return &AdminStats{Orders: orders, Compensations: comps, UnpublishedEvents: n}, nil
}

type CancelService struct {
	scenes  map[string]domain.SceneConfig
	repo    port.OrderRepository
	offer   port.OfferClient
	ledger  port.LedgerClient
	pay     port.PaymentAdapter
	fulfill port.FulfillmentRegistry
	ids     port.IDGenerator
	clock   port.Clock
}

func NewCancelService(
	scenes map[string]domain.SceneConfig,
	repo port.OrderRepository,
	offer port.OfferClient,
	ledger port.LedgerClient,
	pay port.PaymentAdapter,
	fulfill port.FulfillmentRegistry,
	ids port.IDGenerator,
	clock port.Clock,
) *CancelService {
	return &CancelService{scenes: scenes, repo: repo, offer: offer, ledger: ledger, pay: pay, fulfill: fulfill, ids: ids, clock: clock}
}

func (s *CancelService) Cancel(ctx context.Context, ident *port.Identity, orderID string) (*domain.Order, error) {
	o, err := s.repo.FindByID(ctx, ident.TenantID, orderID)
	if err != nil {
		return nil, err
	}
	if ident.UserID != "" && o.BuyerUserID != ident.UserID {
		return nil, domain.ErrOrderNotFound
	}
	return s.cancel(ctx, o, ident.TraceID)
}

func (s *CancelService) CancelExpired(ctx context.Context, o *domain.Order) error {
	_, err := s.cancel(ctx, o, "timeout-worker")
	if errors.Is(err, domain.ErrAlreadyPaid) || errors.Is(err, domain.ErrStatusNotAllowed) {
		return nil
	}
	return err
}

func (s *CancelService) cancel(ctx context.Context, o *domain.Order, traceID string) (*domain.Order, error) {
	if o.Status != domain.StatusPendingPay {
		if o.Status == domain.StatusPaid || o.Status == domain.StatusFulfilling || o.Status == domain.StatusCompleted {
			return nil, domain.ErrAlreadyPaid
		}
		return nil, domain.ErrStatusNotAllowed
	}
	now := s.clock.Now()
	ev := domain.NewEvent(s.ids.EventID(), domain.EventCancelled, o.TenantID, traceID, now, domain.OrderEventData(o))
	updated, err := s.repo.Transition(ctx, port.TransitionCmd{
		TenantID:    o.TenantID,
		OrderID:     o.OrderID,
		From:        []domain.Status{domain.StatusPendingPay},
		To:          domain.StatusCancelled,
		Version:     o.Version,
		CancelledAt: &now,
		Event:       &ev,
	})
	if err != nil {
		if errors.Is(err, domain.ErrVersionConflict) {
			cur, e := s.repo.FindByID(ctx, o.TenantID, o.OrderID)
			if e != nil {
				return nil, err
			}
			if cur.Status == domain.StatusPaid {
				return nil, domain.ErrAlreadyPaid
			}
			if cur.Status == domain.StatusCancelled {
				return cur, nil
			}
		}
		return nil, err
	}

	if updated.HasOfferReservation() {
		if err := s.offer.Release(ctx, updated.Promotion.ReservationID, updated.OrderID, "order:"+updated.OrderID+":release"); err != nil {
			_ = s.repo.InsertCompensation(ctx, port.CompensationTicket{
				Kind: "offer_release", TenantID: updated.TenantID, Ref: updated.Promotion.ReservationID, Payload: updated.OrderID,
			})
		}
	}
	if updated.HasLedgerFreeze() {
		if err := s.ledger.Release(ctx, updated.Ledger.FreezeID, "order:release:"+updated.OrderID); err != nil {
			_ = s.repo.InsertCompensation(ctx, port.CompensationTicket{
				Kind: "ledger_release", TenantID: updated.TenantID, Ref: updated.Ledger.FreezeID, Payload: updated.OrderID,
			})
		}
	}
	if adapter := s.fulfill.ForScene(updated.Scene); adapter != nil {
		if err := adapter.Release(ctx, updated); err != nil {
			_ = s.repo.InsertCompensation(ctx, port.CompensationTicket{
				Kind: "fulfill_release", TenantID: updated.TenantID, Ref: updated.OrderID, Payload: updated.Scene,
			})
		}
	}
	if updated.Payment.PaymentIntentID != "" && s.pay != nil {
		_ = s.pay.CloseIntent(ctx, updated.Payment.PaymentIntentID)
	}
	return updated, nil
}

type TimeoutWorker struct {
	repo   port.OrderRepository
	cancel *CancelService
	clock  port.Clock
	batch  int
}

func NewTimeoutWorker(repo port.OrderRepository, cancel *CancelService, clock port.Clock) *TimeoutWorker {
	return &TimeoutWorker{repo: repo, cancel: cancel, clock: clock, batch: 200}
}

func (w *TimeoutWorker) Tick(ctx context.Context) (int, error) {
	orders, err := w.repo.ListExpiredPending(ctx, w.clock.Now(), w.batch)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range orders {
		if err := w.cancel.CancelExpired(ctx, &orders[i]); err != nil {
			continue
		}
		n++
	}
	return n, nil
}
