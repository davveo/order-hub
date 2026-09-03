package application

import (
	"context"
	"fmt"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type CloseService struct {
	scenes  map[string]domain.SceneConfig
	repo    port.OrderRepository
	offer   port.OfferClient
	ledger  port.LedgerClient
	fulfill port.FulfillmentRegistry
	ids     port.IDGenerator
	clock   port.Clock
}

func NewCloseService(
	scenes map[string]domain.SceneConfig,
	repo port.OrderRepository,
	offer port.OfferClient,
	ledger port.LedgerClient,
	fulfill port.FulfillmentRegistry,
	ids port.IDGenerator,
	clock port.Clock,
) *CloseService {
	return &CloseService{scenes: scenes, repo: repo, offer: offer, ledger: ledger, fulfill: fulfill, ids: ids, clock: clock}
}

func (s *CloseService) Close(ctx context.Context, ident *port.Identity, orderID string) (*domain.Order, error) {
	o, err := s.repo.FindByID(ctx, ident.TenantID, orderID)
	if err != nil {
		return nil, err
	}
	if ident.UserID != "" && o.BuyerUserID != ident.UserID {
		return nil, domain.ErrOrderNotFound
	}
	if o.Status == domain.StatusClosed {
		return o, nil
	}
	scene, ok := s.scenes[o.Scene]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrSceneNotFound, o.Scene)
	}
	if !scene.AllowCloseAfterPaid {
		return nil, fmt.Errorf("%w: scene %s cannot close after paid", domain.ErrStatusNotAllowed, o.Scene)
	}
	switch o.Status {
	case domain.StatusPaid, domain.StatusFulfilling, domain.StatusCompleted:
	default:
		return nil, domain.ErrStatusNotAllowed
	}

	if o.Promotion.RedemptionID != "" && o.Amounts.Discount > 0 {
		if err := s.offer.Reverse(ctx, o.TenantID, o.Promotion.RedemptionID, "close:"+o.OrderID, 0, "order:"+o.OrderID+":close-reverse"); err != nil {
			return nil, err
		}
	} else if o.HasOfferReservation() && o.Promotion.RedemptionID == "" {
		if err := s.offer.Release(ctx, o.TenantID, o.Promotion.ReservationID, o.OrderID, "order:"+o.OrderID+":close-release"); err != nil {
			return nil, err
		}
	}

	remain := o.RefundableAmount()
	if remain > 0 && o.Amounts.LedgerPay > 0 {
		ledgerAmt, _ := splitRefund(o, remain)
		if ledgerAmt > 0 {
			asset := o.Ledger.AssetCode
			if asset == "" {
				asset = o.Amounts.Currency
			}
			if err := s.ledger.Credit(ctx, o.TenantID, o.BuyerUserID, asset, ledgerAmt, "order:close:"+o.OrderID, "order:capture:"+o.OrderID); err != nil {
				return nil, err
			}
		}
	}

	if adapter := s.fulfill.ForScene(o.Scene); adapter != nil {
		if err := adapter.Release(ctx, o); err != nil {
			return nil, err
		}
	}

	now := s.clock.Now()
	ev := domain.NewEvent(s.ids.EventID(), domain.EventClosed, o.TenantID, ident.TraceID, now, domain.OrderEventData(o))
	return s.repo.Transition(ctx, port.TransitionCmd{
		TenantID: o.TenantID,
		OrderID:  o.OrderID,
		From:     []domain.Status{domain.StatusPaid, domain.StatusFulfilling, domain.StatusCompleted},
		To:       domain.StatusClosed,
		Version:  o.Version,
		Event:    &ev,
	})
}
