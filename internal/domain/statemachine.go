package domain

func CanTransition(from, to Status) bool {
	allowed, ok := transitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

var transitions = map[Status][]Status{
	StatusPendingPay:      {StatusPaid, StatusCancelled},
	StatusPaid:            {StatusFulfilling, StatusCompleted, StatusRefunding, StatusRefunded, StatusPartialRefunded, StatusCompensating, StatusClosed},
	StatusFulfilling:      {StatusCompleted, StatusRefunding, StatusRefunded, StatusPartialRefunded, StatusClosed},
	StatusCompleted:       {StatusRefunding, StatusRefunded, StatusPartialRefunded, StatusClosed},
	StatusRefunding:       {StatusRefunded, StatusPartialRefunded, StatusCompleted, StatusFulfilling},
	StatusPartialRefunded: {StatusRefunding, StatusRefunded},
	StatusCompensating:    {StatusPaid, StatusCompleted, StatusFulfilling},
}

func NextAfterPaid(autoComplete bool) Status {
	if autoComplete {
		return StatusCompleted
	}
	return StatusFulfilling
}

func Transition(o *Order, to Status) error {
	if o == nil {
		return ErrOrderNotFound
	}
	if !CanTransition(o.Status, to) {
		if o.Status == StatusPaid && to == StatusCancelled {
			return ErrAlreadyPaid
		}
		return ErrStatusNotAllowed
	}
	o.Status = to
	o.Version++
	return nil
}
