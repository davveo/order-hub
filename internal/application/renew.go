package application

import (
	"context"
	"fmt"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type RenewService struct {
	repo  port.OrderRepository
	offer port.OfferClient
	clock port.Clock
	max   int
}

func NewRenewService(repo port.OrderRepository, offer port.OfferClient, clock port.Clock) *RenewService {
	return &RenewService{repo: repo, offer: offer, clock: clock, max: domain.MaxReservationRenew}
}

func (s *RenewService) Renew(ctx context.Context, ident *port.Identity, orderID string) (*domain.Order, error) {
	o, err := s.repo.FindByID(ctx, ident.TenantID, orderID)
	if err != nil {
		return nil, err
	}
	if ident.UserID != "" && o.BuyerUserID != ident.UserID {
		return nil, domain.ErrOrderNotFound
	}
	if err := s.renewOne(ctx, o); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, o.TenantID, o.OrderID)
}

func (s *RenewService) Tick(ctx context.Context) (int, error) {
	orders, err := s.repo.ListPendingPayForRenew(ctx, s.clock.Now(), 5*time.Minute, s.max, 100)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range orders {
		if err := s.renewOne(ctx, &orders[i]); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

func (s *RenewService) renewOne(ctx context.Context, o *domain.Order) error {
	if o.Status != domain.StatusPendingPay {
		return domain.ErrStatusNotAllowed
	}
	if o.Promotion.ReservationID == "" {
		return nil
	}
	if o.RenewCount >= s.max {
		return fmt.Errorf("%w: renew limit", domain.ErrStatusNotAllowed)
	}
	n := o.RenewCount + 1
	if err := s.offer.Renew(ctx, o.Promotion.ReservationID, o.OrderID, fmt.Sprintf("order:%s:renew:%d", o.OrderID, n)); err != nil {
		return err
	}
	return s.repo.BumpRenew(ctx, o.TenantID, o.OrderID, o.RenewCount)
}
