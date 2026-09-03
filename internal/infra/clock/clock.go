package clock

import "time"

type System struct{}

func (System) Now() time.Time { return time.Now() }

type Frozen struct{ T time.Time }

func (f Frozen) Now() time.Time { return f.T }
