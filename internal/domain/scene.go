package domain

import "time"

const MaxReservationRenew = 3

const (
	OfferReservationActive    = "ACTIVE"
	OfferReservationCommitted = "COMMITTED"
	OfferReservationReleased  = "RELEASED"
)

type OfferMode string

const (
	OfferRequiredOrSkip OfferMode = "required_or_skip"
	OfferOptional       OfferMode = "optional"
	OfferNone           OfferMode = "none"
)

type LedgerMode string

const (
	LedgerNone          LedgerMode = "none"
	LedgerOptional      LedgerMode = "optional"
	LedgerFreezeCapture LedgerMode = "freeze_capture"
)

type InventoryMode string

const (
	InventoryNone            InventoryMode = "none"
	InventoryReserveOnCreate InventoryMode = "reserve_on_create"
)

type FulfillmentType string

const (
	FulfillmentShipping     FulfillmentType = "shipping"
	FulfillmentVirtualGrant FulfillmentType = "virtual_grant"
	FulfillmentEntitlement  FulfillmentType = "entitlement"
	FulfillmentNone         FulfillmentType = "none"
)

type SceneConfig struct {
	Name                string
	Currency            string
	Offer               OfferMode
	Ledger              LedgerMode
	Inventory           InventoryMode
	PayTimeout          time.Duration
	AutoCompleteOnPaid  bool
	Fulfillment         FulfillmentType
	AllowCloseAfterPaid bool
}

func DefaultScenes() map[string]SceneConfig {
	return map[string]SceneConfig{
		"mall_checkout": {
			Name:               "mall_checkout",
			Currency:           "CNY",
			Offer:              OfferRequiredOrSkip,
			Ledger:             LedgerOptional,
			Inventory:          InventoryReserveOnCreate,
			PayTimeout:         15 * time.Minute,
			AutoCompleteOnPaid: false,
			Fulfillment:        FulfillmentShipping,
		},
		"point_mall": {
			Name:               "point_mall",
			Currency:           "POINT",
			Offer:              OfferOptional,
			Ledger:             LedgerFreezeCapture,
			Inventory:          InventoryNone,
			PayTimeout:         15 * time.Minute,
			AutoCompleteOnPaid: true,
			Fulfillment:        FulfillmentVirtualGrant,
		},
		"membership": {
			Name:                "membership",
			Currency:            "CNY",
			Offer:               OfferOptional,
			Ledger:              LedgerOptional,
			Inventory:           InventoryNone,
			PayTimeout:          30 * time.Minute,
			AutoCompleteOnPaid:  true,
			Fulfillment:         FulfillmentEntitlement,
			AllowCloseAfterPaid: true,
		},
		"course": {
			Name:                "course",
			Currency:            "CNY",
			Offer:               OfferOptional,
			Ledger:              LedgerNone,
			Inventory:           InventoryNone,
			PayTimeout:          30 * time.Minute,
			AutoCompleteOnPaid:  true,
			Fulfillment:         FulfillmentEntitlement,
			AllowCloseAfterPaid: true,
		},
		"saas_subscription": {
			Name:                "saas_subscription",
			Currency:            "CNY",
			Offer:               OfferOptional,
			Ledger:              LedgerNone,
			Inventory:           InventoryNone,
			PayTimeout:          30 * time.Minute,
			AutoCompleteOnPaid:  true,
			Fulfillment:         FulfillmentEntitlement,
			AllowCloseAfterPaid: true,
		},
	}
}

func (s SceneConfig) NeedsOffer(hasQuote bool, couponN int) bool {
	if s.Offer == OfferNone {
		return false
	}
	return hasQuote || couponN > 0 || s.Offer == OfferRequiredOrSkip
}

func (s SceneConfig) NeedsLedger(ledgerPay int64) bool {
	if s.Ledger == LedgerNone {
		return false
	}
	if s.Ledger == LedgerFreezeCapture {
		return true
	}
	return ledgerPay > 0
}

func (s SceneConfig) NeedsInventory() bool {
	return s.Inventory == InventoryReserveOnCreate
}
