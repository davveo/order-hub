package application

import (
	"context"
	"errors"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/davveo/order-hub/internal/domain"
)

type ReconDiff struct {
	OrderID           string `json:"order_id"`
	TenantID          string `json:"tenant_id"`
	Scene             string `json:"scene"`
	OrderStatus       string `json:"order_status"`
	Kind              string `json:"kind"`
	ReservationID     string `json:"reservation_id,omitempty"`
	ReservationStatus string `json:"reservation_status,omitempty"`
	RedemptionID      string `json:"redemption_id,omitempty"`
	FreezeID          string `json:"freeze_id,omitempty"`
	LedgerPay         int64  `json:"ledger_pay,omitempty"`
	TicketCreated     bool   `json:"ticket_created,omitempty"`
	Message           string `json:"message,omitempty"`
}

type ReconResult struct {
	Scanned int         `json:"scanned"`
	Diffs   []ReconDiff `json:"diffs"`
}

type ReconService struct {
	repo  port.OrderRepository
	offer port.OfferClient
}

func NewReconService(repo port.OrderRepository, offer port.OfferClient) *ReconService {
	return &ReconService{repo: repo, offer: offer}
}

func (s *ReconService) Run(ctx context.Context, tenantID string, apply bool) (*ReconResult, error) {
	orders, err := s.repo.ListForReconcile(ctx, tenantID, 200)
	if err != nil {
		return nil, err
	}
	out := &ReconResult{Diffs: []ReconDiff{}}
	for i := range orders {
		o := &orders[i]
		out.Scanned++
		if o.HasOfferReservation() {
			if diffs := s.checkOffer(ctx, o, apply); len(diffs) > 0 {
				out.Diffs = append(out.Diffs, diffs...)
			}
		}
		if o.Amounts.LedgerPay > 0 && o.Ledger.FreezeID == "" && paidLike(o.Status) {
			out.Diffs = append(out.Diffs, ReconDiff{
				OrderID: o.OrderID, TenantID: o.TenantID, Scene: o.Scene,
				OrderStatus: string(o.Status), Kind: "ledger_freeze_missing",
				LedgerPay: o.Amounts.LedgerPay, Message: "已付订单账本金额>0 但缺少 freeze_id",
			})
		}
	}
	return out, nil
}

func (s *ReconService) checkOffer(ctx context.Context, o *domain.Order, apply bool) []ReconDiff {
	rsv, err := s.offer.GetReservation(ctx, o.TenantID, o.Promotion.ReservationID)
	if err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			return []ReconDiff{{
				OrderID: o.OrderID, TenantID: o.TenantID, Scene: o.Scene,
				OrderStatus: string(o.Status), Kind: "offer_reservation_missing",
				ReservationID: o.Promotion.ReservationID,
				Message:       "订单有 reservation_id，OfferHub 查无此占用",
			}}
		}
		return []ReconDiff{{
			OrderID: o.OrderID, TenantID: o.TenantID, Scene: o.Scene,
			OrderStatus: string(o.Status), Kind: "offer_lookup_error",
			ReservationID: o.Promotion.ReservationID, Message: err.Error(),
		}}
	}
	var diffs []ReconDiff
	switch {
	case paidLike(o.Status) && rsv.Status == domain.OfferReservationActive:
		d := ReconDiff{
			OrderID: o.OrderID, TenantID: o.TenantID, Scene: o.Scene,
			OrderStatus: string(o.Status), Kind: "offer_commit_missing",
			ReservationID: o.Promotion.ReservationID, ReservationStatus: rsv.Status,
			Message: "已付订单优惠占用仍为 ACTIVE，应 commit",
		}
		if apply {
			d.TicketCreated = s.ensureTicket(ctx, "after_paid", o.TenantID, o.OrderID, o.Promotion.ReservationID)
		}
		diffs = append(diffs, d)
	case (o.Status == domain.StatusCancelled || o.Status == domain.StatusClosed) && rsv.Status == domain.OfferReservationActive:
		d := ReconDiff{
			OrderID: o.OrderID, TenantID: o.TenantID, Scene: o.Scene,
			OrderStatus: string(o.Status), Kind: "offer_release_missing",
			ReservationID: o.Promotion.ReservationID, ReservationStatus: rsv.Status,
			Message: "已取消/关闭订单优惠占用仍为 ACTIVE，应 release",
		}
		if apply {
			d.TicketCreated = s.ensureTicket(ctx, "offer_release", o.TenantID, o.Promotion.ReservationID, o.OrderID)
		}
		diffs = append(diffs, d)
	case rsv.Status == domain.OfferReservationCommitted && o.Promotion.RedemptionID == "":
		d := ReconDiff{
			OrderID: o.OrderID, TenantID: o.TenantID, Scene: o.Scene,
			OrderStatus: string(o.Status), Kind: "offer_redemption_missing",
			ReservationID: o.Promotion.ReservationID, ReservationStatus: rsv.Status,
			Message: "占用已 COMMITTED 但订单缺少 redemption_id",
		}
		if apply {
			rid, err := s.offer.Commit(ctx, o.TenantID, o.Promotion.ReservationID, o.OrderID, "order:"+o.OrderID+":commit")
			if err == nil && rid != "" {
				_ = s.repo.UpdateRedemption(ctx, o.TenantID, o.OrderID, rid)
				d.RedemptionID = rid
				d.Message = "已回填 redemption_id"
			} else if err != nil {
				d.Message = err.Error()
				d.TicketCreated = s.ensureTicket(ctx, "after_paid", o.TenantID, o.OrderID, o.Promotion.ReservationID)
			}
		}
		diffs = append(diffs, d)
	}
	return diffs
}

func (s *ReconService) ensureTicket(ctx context.Context, kind, tenantID, ref, payload string) bool {
	open, err := s.repo.HasOpenCompensation(ctx, kind, tenantID, ref)
	if err != nil || open {
		return false
	}
	if err := s.repo.InsertCompensation(ctx, port.CompensationTicket{
		Kind: kind, TenantID: tenantID, Ref: ref, Payload: payload,
	}); err != nil {
		return false
	}
	return true
}

func paidLike(st domain.Status) bool {
	switch st {
	case domain.StatusPaid, domain.StatusFulfilling, domain.StatusCompleted, domain.StatusCompensating,
		domain.StatusRefunding, domain.StatusRefunded, domain.StatusPartialRefunded:
		return true
	default:
		return false
	}
}
