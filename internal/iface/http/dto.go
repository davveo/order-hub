package httpserver

import (
	"time"

	"github.com/davveo/order-hub/internal/application"
	"github.com/davveo/order-hub/internal/domain"
)

type lineDTO struct {
	LineID     string            `json:"line_id"`
	ObjectType string            `json:"object_type"`
	ObjectID   string            `json:"object_id"`
	Quantity   int64             `json:"quantity"`
	UnitPrice  int64             `json:"unit_price"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Snapshot   map[string]any    `json:"snapshot,omitempty"`
}

type ledgerPayDTO struct {
	AssetCode string `json:"asset_code"`
	Amount    int64  `json:"amount"`
}

type previewReq struct {
	Scene     string         `json:"scene" binding:"required"`
	Channel   string         `json:"channel"`
	CouponIDs []string       `json:"coupon_ids"`
	AutoBest  bool           `json:"auto_best"`
	Items     []lineDTO      `json:"items" binding:"required"`
	LedgerPay *ledgerPayDTO  `json:"ledger_pay"`
	Ext       map[string]any `json:"ext"`
}

type checkoutReq struct {
	ClientOrderID string         `json:"client_order_id" binding:"required"`
	Scene         string         `json:"scene" binding:"required"`
	Channel       string         `json:"channel"`
	QuoteID       string         `json:"quote_id"`
	CouponIDs     []string       `json:"coupon_ids"`
	Items         []lineDTO      `json:"items"`
	PayMethod     string         `json:"pay_method"`
	LedgerPay     *ledgerPayDTO  `json:"ledger_pay"`
	Ext           map[string]any `json:"ext"`
}

type refundReq struct {
	Amount int64  `json:"amount"`
	Reason string `json:"reason"`
	Lines  []struct {
		LineID string `json:"line_id"`
		Amount int64  `json:"amount"`
	} `json:"lines"`
}

type paymentCallbackReq struct {
	OrderID         string `json:"order_id" binding:"required"`
	TenantID        string `json:"tenant_id"`
	PaymentIntentID string `json:"payment_intent_id"`
	Success         bool   `json:"success"`
	PaidAmount      int64  `json:"paid_amount"`
	Channel         string `json:"channel"`
}

func toLines(in []lineDTO) []domain.OrderLine {
	out := make([]domain.OrderLine, 0, len(in))
	for _, l := range in {
		out = append(out, domain.OrderLine{
			LineID:     l.LineID,
			ObjectType: l.ObjectType,
			ObjectID:   l.ObjectID,
			Quantity:   l.Quantity,
			UnitPrice:  l.UnitPrice,
			Attributes: l.Attributes,
			Snapshot:   l.Snapshot,
		})
	}
	return out
}

func toLedger(p *ledgerPayDTO) *application.LedgerPay {
	if p == nil {
		return nil
	}
	return &application.LedgerPay{AssetCode: p.AssetCode, Amount: p.Amount}
}

type orderView struct {
	OrderID          string         `json:"order_id"`
	ClientOrderID    string         `json:"client_order_id"`
	Scene            string         `json:"scene"`
	Channel          string         `json:"channel"`
	Status           string         `json:"status"`
	Version          int64          `json:"version"`
	Currency         string         `json:"currency"`
	OriginalAmount   int64          `json:"original_amount"`
	DiscountAmount   int64          `json:"discount_amount"`
	PayableAmount    int64          `json:"payable_amount"`
	LedgerPayAmount  int64          `json:"ledger_pay_amount"`
	ChannelPayAmount int64          `json:"channel_pay_amount"`
	PaidAmount       int64          `json:"paid_amount"`
	RefundedAmount   int64          `json:"refunded_amount"`
	QuoteID          string         `json:"quote_id,omitempty"`
	ReservationID    string         `json:"reservation_id,omitempty"`
	RedemptionID     string         `json:"redemption_id,omitempty"`
	FreezeID         string         `json:"freeze_id,omitempty"`
	AssetCode        string         `json:"asset_code,omitempty"`
	PayMethod        string         `json:"pay_method"`
	PaymentIntentID  string         `json:"payment_intent_id,omitempty"`
	ExpireAt         *time.Time     `json:"expire_at,omitempty"`
	RenewCount       int            `json:"renew_count,omitempty"`
	Lines            []lineDTO      `json:"lines,omitempty"`
	Ext              map[string]any `json:"ext,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	PaidAt           *time.Time     `json:"paid_at,omitempty"`
	CancelledAt      *time.Time     `json:"cancelled_at,omitempty"`
	CompletedAt      *time.Time     `json:"completed_at,omitempty"`
}

func viewOrder(o *domain.Order) orderView {
	v := orderView{
		OrderID:          o.OrderID,
		ClientOrderID:    o.ClientOrderID,
		Scene:            o.Scene,
		Channel:          o.Channel,
		Status:           string(o.Status),
		Version:          o.Version,
		Currency:         o.Amounts.Currency,
		OriginalAmount:   o.Amounts.Original,
		DiscountAmount:   o.Amounts.Discount,
		PayableAmount:    o.Amounts.Payable,
		LedgerPayAmount:  o.Amounts.LedgerPay,
		ChannelPayAmount: o.Amounts.ChannelPay,
		PaidAmount:       o.Amounts.Paid,
		RefundedAmount:   o.Amounts.Refunded,
		QuoteID:          o.Promotion.QuoteID,
		ReservationID:    o.Promotion.ReservationID,
		RedemptionID:     o.Promotion.RedemptionID,
		FreezeID:         o.Ledger.FreezeID,
		AssetCode:        o.Ledger.AssetCode,
		PayMethod:        string(o.PayMethod),
		PaymentIntentID:  o.Payment.PaymentIntentID,
		RenewCount:       o.RenewCount,
		Ext:              o.Context,
		CreatedAt:        o.CreatedAt,
		PaidAt:           o.PaidAt,
		CancelledAt:      o.CancelledAt,
		CompletedAt:      o.CompletedAt,
	}
	if !o.ExpireAt.IsZero() {
		t := o.ExpireAt
		v.ExpireAt = &t
	}
	for _, l := range o.Lines {
		v.Lines = append(v.Lines, lineDTO{
			LineID:     l.LineID,
			ObjectType: l.ObjectType,
			ObjectID:   l.ObjectID,
			Quantity:   l.Quantity,
			UnitPrice:  l.UnitPrice,
			Attributes: l.Attributes,
			Snapshot:   l.Snapshot,
		})
	}
	return v
}
