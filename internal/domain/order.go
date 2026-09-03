package domain

import "time"

type PayMethod string

const (
	PayMethodChannel PayMethod = "channel"
	PayMethodLedger  PayMethod = "ledger"
	PayMethodMix     PayMethod = "mix"
)

type OrderLine struct {
	LineID          string
	ObjectType      string
	ObjectID        string
	Quantity        int64
	UnitPrice       int64
	OriginalAmount  int64
	DiscountAmount  int64
	PayableAmount   int64
	Attributes      map[string]string
	Snapshot        map[string]any
}

type PromotionRef struct {
	QuoteID       string
	ReservationID string
	RedemptionID  string
}

type LedgerRef struct {
	FreezeID  string
	AssetCode string
	BizNo     string
}

type PaymentRef struct {
	PaymentIntentID string
	Channel         string
}

type PromotionDetail struct {
	SourceType          string
	SourceID            string
	DiscountAmount      int64
	Allocations         []Allocation
	RuleSnapshotVersion string
}

type Allocation struct {
	LineID         string
	DiscountAmount int64
}

type LedgerLeg struct {
	Command   string
	BizNo     string
	FreezeID  string
	AssetCode string
	Amount    int64
	Status    string
}

type Refund struct {
	RefundID      string
	OrderID       string
	TenantID      string
	Amount        int64
	Currency      string
	Status        string
	Reason        string
	ChannelRefund bool
	LedgerCredit  bool
	LedgerAmount  int64
	ChannelAmount int64
	Lines         []LineRefund
	CreatedAt     time.Time
}

type LineRefund struct {
	LineID string
	Amount int64
}

type Order struct {
	OrderID       string
	TenantID      string
	Scene         string
	Channel       string
	BuyerUserID   string
	ClientOrderID string
	Status        Status
	Version       int64
	Amounts       Amounts
	Promotion     PromotionRef
	Ledger        LedgerRef
	Payment       PaymentRef
	PayMethod     PayMethod
	ExpireAt      time.Time
	RenewCount    int
	Context       map[string]any
	Lines         []OrderLine
	Promotions    []PromotionDetail
	LedgerLegs    []LedgerLeg
	CreatedAt     time.Time
	UpdatedAt     time.Time
	PaidAt        *time.Time
	CancelledAt   *time.Time
	CompletedAt   *time.Time
}

func (o *Order) RefundableAmount() int64 {
	return o.Amounts.Paid - o.Amounts.Refunded
}

func (o *Order) HasLedgerFreeze() bool {
	return o.Ledger.FreezeID != "" || o.Amounts.LedgerPay > 0
}

func (o *Order) HasOfferReservation() bool {
	return o.Promotion.ReservationID != ""
}

func (o *Order) NeedsChannelPay() bool {
	return o.Amounts.ChannelPay > 0
}

func ResolvePayMethod(ledgerPay, channelPay int64) PayMethod {
	switch {
	case ledgerPay > 0 && channelPay > 0:
		return PayMethodMix
	case ledgerPay > 0:
		return PayMethodLedger
	default:
		return PayMethodChannel
	}
}
