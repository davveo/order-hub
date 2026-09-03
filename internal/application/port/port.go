package port

import (
	"context"
	"time"

	"github.com/davveo/order-hub/internal/domain"
)

type Identity struct {
	UserID     string
	TenantID   string
	Attributes map[string]any
	TraceID    string
}

type AuthClient interface {
	Introspect(ctx context.Context, token string) (*Identity, error)
}

type QuoteRequest struct {
	TenantID   string
	UserID     string
	OrderID    string
	Scene      string
	Channel    string
	Currency   string
	CouponIDs  []string
	AutoBest   bool
	Items      []domain.OrderLine
	Attributes map[string]any
	Context    map[string]any
}

type QuoteResult struct {
	QuoteID        string                   `json:"quote_id"`
	Currency       string                   `json:"currency"`
	OriginalAmount int64                    `json:"original_amount"`
	DiscountAmount int64                    `json:"discount_amount"`
	PayableAmount  int64                    `json:"payable_amount"`
	Allocations    []domain.Allocation      `json:"allocations,omitempty"`
	Promotions     []domain.PromotionDetail `json:"promotions,omitempty"`
	ExpiresAt      time.Time                `json:"expires_at"`
	ContextHash    string                   `json:"context_hash,omitempty"`
}

type ReservationResult struct {
	ReservationID string    `json:"reservation_id"`
	QuoteID       string    `json:"quote_id,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
	Status        string    `json:"status,omitempty"`
}

type RedemptionResult struct {
	RedemptionID   string `json:"redemption_id"`
	ReservationID  string `json:"reservation_id,omitempty"`
	DiscountAmount int64  `json:"discount_amount,omitempty"`
	ReversedAmount int64  `json:"reversed_amount,omitempty"`
}

type OfferClient interface {
	Quote(ctx context.Context, req QuoteRequest) (*QuoteResult, error)
	Reserve(ctx context.Context, tenantID, quoteID, orderID, idemKey string) (*ReservationResult, error)
	Commit(ctx context.Context, tenantID, reservationID, orderID, idemKey string) (redemptionID string, err error)
	Release(ctx context.Context, tenantID, reservationID, orderID, idemKey string) error
	Renew(ctx context.Context, tenantID, reservationID, orderID, idemKey string) error
	Reverse(ctx context.Context, tenantID, redemptionID, refundID string, amount int64, idemKey string) error
	GetReservation(ctx context.Context, tenantID, reservationID string) (*ReservationResult, error)
	GetRedemption(ctx context.Context, tenantID, redemptionID string) (*RedemptionResult, error)
}

type FreezeRequest struct {
	TenantID  string
	UserID    string
	OrderID   string
	AssetCode string
	Amount    int64
	BizNo     string
}

type LedgerClient interface {
	GetBalance(ctx context.Context, tenantID, userID, assetCode string) (int64, error)
	Freeze(ctx context.Context, req FreezeRequest) (freezeID string, err error)
	Capture(ctx context.Context, freezeID, bizNo string) error
	Release(ctx context.Context, freezeID, bizNo string) error
	Credit(ctx context.Context, tenantID, userID, assetCode string, amount int64, bizNo, relatedBizNo string) error
}

type PaymentIntent struct {
	IntentID  string
	Channel   string
	Amount    int64
	Currency  string
	PayURL    string
	ExpiresAt time.Time
}

type PaymentAdapter interface {
	CreateIntent(ctx context.Context, o *domain.Order) (*PaymentIntent, error)
	CloseIntent(ctx context.Context, intentID string) error
	Refund(ctx context.Context, refund domain.Refund) error
}

type FulfillmentAdapter interface {
	Reserve(ctx context.Context, o *domain.Order) error
	Commit(ctx context.Context, o *domain.Order) error
	Release(ctx context.Context, o *domain.Order) error
}

type FulfillmentRegistry interface {
	ForScene(scene string) FulfillmentAdapter
}

type PreviewSnapshot struct {
	QuoteID        string
	ContextHash    string
	Scene          string
	Channel        string
	Currency       string
	CouponIDs      []string
	Items          []domain.OrderLine
	Allocations    []domain.Allocation
	Promotions     []domain.PromotionDetail
	OriginalAmount int64
	DiscountAmount int64
	PayableAmount  int64
	LedgerPay      int64
	AssetCode      string
	ChannelPay     int64
	Ext            map[string]any
	ExpiresAt      time.Time
	SubjectAttrs   map[string]any
}

type PreviewCache interface {
	Put(ctx context.Context, tenantID, userID, quoteID string, snap PreviewSnapshot, ttl time.Duration) error
	GetByQuote(ctx context.Context, tenantID, quoteID string) (*PreviewSnapshot, error)
}

type IdempotencyRecord struct {
	TenantID    string
	Actor       string
	Key         string
	RequestHash string
	Response    []byte
	OrderID     string
	CreatedAt   time.Time
}

type CheckoutPersist struct {
	Order       *domain.Order
	Idempotency *IdempotencyRecord
	Event       domain.Event
}

type TransitionCmd struct {
	TenantID        string
	OrderID         string
	From            []domain.Status
	To              domain.Status
	Version         int64
	PaidAmount      *int64
	RefundedAdd     int64
	RedemptionID    string
	PaymentIntentID string
	Event           *domain.Event
	PaidAt          *time.Time
	CancelledAt     *time.Time
	CompletedAt     *time.Time
}

type OrderRepository interface {
	InsertCheckout(ctx context.Context, rec CheckoutPersist) error
	FindByID(ctx context.Context, tenantID, orderID string) (*domain.Order, error)
	FindByClientOrderID(ctx context.Context, tenantID, buyerID, clientOrderID string) (*domain.Order, error)
	FindIdempotency(ctx context.Context, tenantID, actor, key string) (*IdempotencyRecord, error)
	Transition(ctx context.Context, cmd TransitionCmd) (*domain.Order, error)
	UpdatePaymentIntent(ctx context.Context, tenantID, orderID, intentID, channel string) error
	UpdateRedemption(ctx context.Context, tenantID, orderID, redemptionID string) error
	ListExpiredPending(ctx context.Context, now time.Time, limit int) ([]domain.Order, error)
	ListPendingPayForRenew(ctx context.Context, now time.Time, window time.Duration, maxRenew, limit int) ([]domain.Order, error)
	BumpRenew(ctx context.Context, tenantID, orderID string, expectedCount int) error
	ListByBuyer(ctx context.Context, tenantID, buyerID string, status domain.Status, scene, cursor string, limit int) ([]domain.Order, string, error)
	UpdateIdempotencyResponse(ctx context.Context, tenantID, actor, key string, resp []byte) error
	InsertRefund(ctx context.Context, o *domain.Order, refund domain.Refund, event domain.Event) error
	InsertCompensation(ctx context.Context, t CompensationTicket) error
	ClaimCompensations(ctx context.Context, now time.Time, limit int) ([]CompensationTicket, error)
	UpdateCompensation(ctx context.Context, id int64, status, lastErr string, nextRetry *time.Time) error
	ListCompensations(ctx context.Context, status string, limit int) ([]CompensationTicket, error)
	FindCompensation(ctx context.Context, id int64) (*CompensationTicket, error)
	AdminList(ctx context.Context, f AdminListFilter) ([]domain.Order, string, error)
	CountOrdersByStatus(ctx context.Context, tenantID string) (map[string]int64, error)
	CountCompensationsByStatus(ctx context.Context) (map[string]int64, error)
	CountUnpublishedEvents(ctx context.Context) (int64, error)
	ListUnpublishedEvents(ctx context.Context, limit int) ([]OutboxRow, error)
	MarkEventPublished(ctx context.Context, eventID string) error
	ListForReconcile(ctx context.Context, tenantID string, limit int) ([]domain.Order, error)
	HasOpenCompensation(ctx context.Context, kind, tenantID, ref string) (bool, error)
}

type AdminListFilter struct {
	TenantID string
	Status   domain.Status
	Scene    string
	Query    string
	Cursor   string
	Limit    int
}

type CompensationTicket struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	TenantID  string     `json:"tenant_id"`
	Ref       string     `json:"ref"`
	Payload   string     `json:"payload"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error"`
	NextRetry *time.Time `json:"next_retry,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type OutboxRow struct {
	EventID  string
	Payload  []byte
	Attempts int
}

type EventPublisher interface {
	Publish(ctx context.Context, ev domain.Event) error
}

type IDGenerator interface {
	OrderID() string
	EventID() string
	RefundID() string
	IntentID() string
}

type Clock interface {
	Now() time.Time
}

type Locker interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (unlock func(), ok bool, err error)
}
