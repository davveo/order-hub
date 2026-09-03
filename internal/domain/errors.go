package domain

import "errors"

var (
	ErrInvalidArgument     = errors.New("invalid argument")
	ErrOverflow            = errors.New("amount overflow")
	ErrAmountInvariant     = errors.New("amount invariant violated")
	ErrQuoteStale          = errors.New("quote stale")
	ErrSceneNotFound       = errors.New("scene not found")
	ErrOrderNotFound       = errors.New("order not found")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
	ErrClientOrderConflict = errors.New("client_order_id conflict")
	ErrStatusNotAllowed    = errors.New("status not allowed")
	ErrAlreadyPaid         = errors.New("order already paid")
	ErrVersionConflict     = errors.New("version conflict")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrOfferReserve        = errors.New("offer reserve failed")
	ErrLedgerFreeze        = errors.New("ledger freeze failed")
	ErrInventoryReserve    = errors.New("inventory reserve failed")
	ErrRefundExceedsPaid   = errors.New("refund exceeds paid")
	ErrNotImplemented      = errors.New("not implemented")
)
