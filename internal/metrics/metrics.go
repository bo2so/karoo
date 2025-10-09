package metrics

import (
	"sync/atomic"
)

type Metrics struct {
	UpConnected    atomic.Bool
	SharesOK       atomic.Uint64
	SharesBad      atomic.Uint64
	ClientsActive  atomic.Int64
	LastNotifyUnix atomic.Int64
	LastSetDiff    atomic.Int64
}

func New() *Metrics {
	return &Metrics{}
}
