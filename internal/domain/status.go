package domain

type Status string

const (
	StatusPendingPay      Status = "PENDING_PAY"
	StatusPaid            Status = "PAID"
	StatusFulfilling      Status = "FULFILLING"
	StatusCompleted       Status = "COMPLETED"
	StatusCancelled       Status = "CANCELLED"
	StatusClosed          Status = "CLOSED"
	StatusRefunding       Status = "REFUNDING"
	StatusRefunded        Status = "REFUNDED"
	StatusPartialRefunded Status = "PARTIAL_REFUNDED"
	StatusCompensating    Status = "COMPENSATING"
)

func (s Status) String() string { return string(s) }

func ParseStatus(raw string) (Status, bool) {
	s := Status(raw)
	switch s {
	case StatusPendingPay, StatusPaid, StatusFulfilling, StatusCompleted,
		StatusCancelled, StatusClosed, StatusRefunding, StatusRefunded,
		StatusPartialRefunded, StatusCompensating:
		return s, true
	default:
		return "", false
	}
}
