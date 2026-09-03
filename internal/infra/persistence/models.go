package persistence

import (
	"encoding/json"
	"time"

	"github.com/davveo/order-hub/internal/domain"
	"gorm.io/gorm"
)

type OrderPO struct {
	OrderID         string    `gorm:"column:order_id;primaryKey;size:64"`
	TenantID        string    `gorm:"column:tenant_id;size:64;index:idx_orders_buyer,priority:1;uniqueIndex:uk_orders_client,priority:1"`
	Scene           string    `gorm:"column:scene;size:32;index:idx_orders_buyer,priority:2"`
	Channel         string    `gorm:"column:channel;size:32"`
	BuyerUserID     string    `gorm:"column:buyer_user_id;size:64;index:idx_orders_buyer,priority:3;uniqueIndex:uk_orders_client,priority:2"`
	ClientOrderID   string    `gorm:"column:client_order_id;size:128;uniqueIndex:uk_orders_client,priority:3"`
	Status          string    `gorm:"column:status;size:32;index:idx_orders_timeout,priority:1"`
	Version         int64     `gorm:"column:version"`
	Currency        string    `gorm:"column:currency;size:16"`
	OriginalAmount  int64     `gorm:"column:original_amount"`
	DiscountAmount  int64     `gorm:"column:discount_amount"`
	PayableAmount   int64     `gorm:"column:payable_amount"`
	LedgerPayAmount int64     `gorm:"column:ledger_pay_amount"`
	ChannelPayAmount int64    `gorm:"column:channel_pay_amount"`
	PaidAmount      int64     `gorm:"column:paid_amount"`
	RefundedAmount  int64     `gorm:"column:refunded_amount"`
	QuoteID         string    `gorm:"column:quote_id;size:64"`
	ReservationID   string    `gorm:"column:reservation_id;size:64"`
	RedemptionID    string    `gorm:"column:redemption_id;size:64"`
	FreezeID        string    `gorm:"column:freeze_id;size:64"`
	AssetCode       string    `gorm:"column:asset_code;size:32"`
	PayMethod       string    `gorm:"column:pay_method;size:16"`
	PaymentIntentID string    `gorm:"column:payment_intent_id;size:64"`
	PaymentChannel  string    `gorm:"column:payment_channel;size:32"`
	ExpireAt        *time.Time `gorm:"column:expire_at;index:idx_orders_timeout,priority:2"`
	ContextJSON     []byte    `gorm:"column:context_json;type:jsonb"`
	CreatedAt       time.Time `gorm:"column:created_at;index:idx_orders_buyer,priority:4"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
	PaidAt          *time.Time `gorm:"column:paid_at"`
	CancelledAt     *time.Time `gorm:"column:cancelled_at"`
	CompletedAt     *time.Time `gorm:"column:completed_at"`
}

func (OrderPO) TableName() string { return "orders" }

type OrderLinePO struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	OrderID         string    `gorm:"column:order_id;size:64;uniqueIndex:uk_line,priority:1"`
	LineID          string    `gorm:"column:line_id;size:64;uniqueIndex:uk_line,priority:2"`
	ObjectType      string    `gorm:"column:object_type;size:32"`
	ObjectID        string    `gorm:"column:object_id;size:64"`
	Quantity        int64     `gorm:"column:quantity"`
	UnitPrice       int64     `gorm:"column:unit_price"`
	OriginalAmount  int64     `gorm:"column:original_amount"`
	DiscountAmount  int64     `gorm:"column:discount_amount"`
	PayableAmount   int64     `gorm:"column:payable_amount"`
	AttributesJSON  []byte    `gorm:"column:attributes_json;type:jsonb"`
	SnapshotJSON    []byte    `gorm:"column:snapshot_json;type:jsonb"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (OrderLinePO) TableName() string { return "order_lines" }

type OrderPromotionPO struct {
	ID                  int64     `gorm:"column:id;primaryKey;autoIncrement"`
	OrderID             string    `gorm:"column:order_id;size:64;index"`
	SourceType          string    `gorm:"column:source_type;size:32"`
	SourceID            string    `gorm:"column:source_id;size:64"`
	DiscountAmount      int64     `gorm:"column:discount_amount"`
	AllocationsJSON     []byte    `gorm:"column:allocations_json;type:jsonb"`
	RuleSnapshotVersion string    `gorm:"column:rule_snapshot_version;size:64"`
}

func (OrderPromotionPO) TableName() string { return "order_promotions" }

type OrderLedgerLegPO struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	OrderID   string    `gorm:"column:order_id;size:64;index"`
	Command   string    `gorm:"column:command;size:16"`
	BizNo     string    `gorm:"column:biz_no;size:128;uniqueIndex"`
	FreezeID  string    `gorm:"column:freeze_id;size:64"`
	AssetCode string    `gorm:"column:asset_code;size:32"`
	Amount    int64     `gorm:"column:amount"`
	Status    string    `gorm:"column:status;size:16"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (OrderLedgerLegPO) TableName() string { return "order_ledger_legs" }

type OutboxPO struct {
	EventID     string     `gorm:"column:event_id;primaryKey;size:64"`
	EventType   string     `gorm:"column:event_type;size:64"`
	TenantID    string     `gorm:"column:tenant_id;size:64"`
	Payload     []byte     `gorm:"column:payload;type:jsonb"`
	CreatedAt   time.Time  `gorm:"column:created_at;index"`
	PublishedAt *time.Time `gorm:"column:published_at;index"`
	Attempts    int        `gorm:"column:attempts"`
}

func (OutboxPO) TableName() string { return "order_events" }

type IdempotencyPO struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TenantID    string    `gorm:"column:tenant_id;size:64;uniqueIndex:uk_idem,priority:1"`
	Actor       string    `gorm:"column:actor;size:64;uniqueIndex:uk_idem,priority:2"`
	IdemKey     string    `gorm:"column:idempotency_key;size:128;uniqueIndex:uk_idem,priority:3"`
	RequestHash string    `gorm:"column:request_hash;size:64"`
	Response    []byte    `gorm:"column:response;type:jsonb"`
	OrderID     string    `gorm:"column:order_id;size:64"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (IdempotencyPO) TableName() string { return "idempotency_records" }

type RefundPO struct {
	RefundID      string    `gorm:"column:refund_id;primaryKey;size:64"`
	OrderID       string    `gorm:"column:order_id;size:64;index"`
	TenantID      string    `gorm:"column:tenant_id;size:64"`
	Amount        int64     `gorm:"column:amount"`
	Currency      string    `gorm:"column:currency;size:16"`
	Status        string    `gorm:"column:status;size:16"`
	Reason        string    `gorm:"column:reason;size:256"`
	ChannelRefund bool      `gorm:"column:channel_refund"`
	LedgerCredit  bool      `gorm:"column:ledger_credit"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

func (RefundPO) TableName() string { return "order_refunds" }

type CompensationPO struct {
	ID        int64      `gorm:"column:id;primaryKey;autoIncrement"`
	Kind      string     `gorm:"column:kind;size:32;index"`
	Ref       string     `gorm:"column:ref;size:128"`
	Payload   string     `gorm:"column:payload;type:text"`
	Status    string     `gorm:"column:status;size:16;index"`
	Attempts  int        `gorm:"column:attempts"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	DoneAt    *time.Time `gorm:"column:done_at"`
}

func (CompensationPO) TableName() string { return "saga_compensations" }

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&OrderPO{},
		&OrderLinePO{},
		&OrderPromotionPO{},
		&OrderLedgerLegPO{},
		&OutboxPO{},
		&IdempotencyPO{},
		&RefundPO{},
		&CompensationPO{},
	)
}

func orderToPO(o *domain.Order) OrderPO {
	ctx, _ := json.Marshal(o.Context)
	po := OrderPO{
		OrderID:          o.OrderID,
		TenantID:         o.TenantID,
		Scene:            o.Scene,
		Channel:          o.Channel,
		BuyerUserID:      o.BuyerUserID,
		ClientOrderID:    o.ClientOrderID,
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
		PaymentChannel:   o.Payment.Channel,
		ContextJSON:      ctx,
		CreatedAt:        o.CreatedAt,
		UpdatedAt:        o.UpdatedAt,
		PaidAt:           o.PaidAt,
		CancelledAt:      o.CancelledAt,
		CompletedAt:      o.CompletedAt,
	}
	if !o.ExpireAt.IsZero() {
		t := o.ExpireAt
		po.ExpireAt = &t
	}
	return po
}

func linesToPO(o *domain.Order) []OrderLinePO {
	out := make([]OrderLinePO, 0, len(o.Lines))
	for _, l := range o.Lines {
		attr, _ := json.Marshal(l.Attributes)
		snap, _ := json.Marshal(l.Snapshot)
		out = append(out, OrderLinePO{
			OrderID:        o.OrderID,
			LineID:         l.LineID,
			ObjectType:     l.ObjectType,
			ObjectID:       l.ObjectID,
			Quantity:       l.Quantity,
			UnitPrice:      l.UnitPrice,
			OriginalAmount: l.OriginalAmount,
			DiscountAmount: l.DiscountAmount,
			PayableAmount:  l.PayableAmount,
			AttributesJSON: attr,
			SnapshotJSON:   snap,
			CreatedAt:      o.CreatedAt,
		})
	}
	return out
}

func poToOrder(po OrderPO, lines []OrderLinePO) *domain.Order {
	o := &domain.Order{
		OrderID:       po.OrderID,
		TenantID:      po.TenantID,
		Scene:         po.Scene,
		Channel:       po.Channel,
		BuyerUserID:   po.BuyerUserID,
		ClientOrderID: po.ClientOrderID,
		Status:        domain.Status(po.Status),
		Version:       po.Version,
		Amounts: domain.Amounts{
			Currency:   po.Currency,
			Original:   po.OriginalAmount,
			Discount:   po.DiscountAmount,
			Payable:    po.PayableAmount,
			LedgerPay:  po.LedgerPayAmount,
			ChannelPay: po.ChannelPayAmount,
			Paid:       po.PaidAmount,
			Refunded:   po.RefundedAmount,
		},
		Promotion: domain.PromotionRef{
			QuoteID:       po.QuoteID,
			ReservationID: po.ReservationID,
			RedemptionID:  po.RedemptionID,
		},
		Ledger: domain.LedgerRef{
			FreezeID:  po.FreezeID,
			AssetCode: po.AssetCode,
			BizNo:     "order:freeze:" + po.OrderID,
		},
		Payment: domain.PaymentRef{
			PaymentIntentID: po.PaymentIntentID,
			Channel:         po.PaymentChannel,
		},
		PayMethod:   domain.PayMethod(po.PayMethod),
		CreatedAt:   po.CreatedAt,
		UpdatedAt:   po.UpdatedAt,
		PaidAt:      po.PaidAt,
		CancelledAt: po.CancelledAt,
		CompletedAt: po.CompletedAt,
	}
	if po.ExpireAt != nil {
		o.ExpireAt = *po.ExpireAt
	}
	if len(po.ContextJSON) > 0 {
		_ = json.Unmarshal(po.ContextJSON, &o.Context)
	}
	for _, l := range lines {
		line := domain.OrderLine{
			LineID:         l.LineID,
			ObjectType:     l.ObjectType,
			ObjectID:       l.ObjectID,
			Quantity:       l.Quantity,
			UnitPrice:      l.UnitPrice,
			OriginalAmount: l.OriginalAmount,
			DiscountAmount: l.DiscountAmount,
			PayableAmount:  l.PayableAmount,
		}
		if len(l.AttributesJSON) > 0 {
			_ = json.Unmarshal(l.AttributesJSON, &line.Attributes)
		}
		if len(l.SnapshotJSON) > 0 {
			_ = json.Unmarshal(l.SnapshotJSON, &line.Snapshot)
		}
		o.Lines = append(o.Lines, line)
	}
	return o
}
