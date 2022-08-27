package timer

import (
	"sync/atomic"
	"time"
)

// MultiTimer is a timer-like helper to be alerted on a series of (changing) deadlines.
// This is useful for example in cases where one wants to respond to external events with variable delays, i.e.
// want to wait a few seconds after each event to in case more are received but no longer than one minute since
// the first event was received.
//
// MultiTimer is not thread-safe, all operations on it (Set, Clear, etc.) and waiting on C() must be performed
// by the same goroutine.
//
// Each deadline is associated to a unique `witness`. This is used as an opaque identifier for the deadline and
// is the value returned by the C() channel. This is useful to tell which of the configured deadlines fired.
//
// Once an event is received from C, all deadlines are cleared and the timer needs to be Reset.
type MultiTimer struct {
	fired        atomic.Bool
	deadlines    map[any]time.Time
	outC         chan any
	confC        chan timerCfg
	timerFactory func(time.Time) timer
}

// IsSet returns if a deadline for the given witness is configured.
func (t *MultiTimer) IsSet(witness any) bool {
	if t.isFired() {
		return false
	}
	_, has := t.deadlines[witness]
	return has
}

// Clear clears one or more deadlines from the timer.
func (t *MultiTimer) Clear(witnesses ...any) {
	if t.isFired() {
		return
	}
	for _, witness := range witnesses {
		delete(t.deadlines, witness)
	}
	t.reconfigure()
}

// Reset clears all deadlines from the timer and stops the trigger goroutine
func (t *MultiTimer) Reset() {
	t.deadlines = nil
	t.reconfigure()
	if t.confC != nil {
		close(t.confC)
		t.confC = nil
	}
	t.fired.Store(false)
	select {
	case <-t.outC:
	default:
	}
}

// Set adds or updates a deadline.
func (t *MultiTimer) Set(witness any, deadline time.Time) {
	if t.isFired() {
		return
	}
	if t.deadlines == nil {
		t.deadlines = make(map[any]time.Time)
	}
	t.deadlines[witness] = deadline
	t.reconfigure()
}

// C returns the channel to read to be alerted once a deadline is reached.
// Note that once an event is read from this channel, all other deadlines are cleared
// and the timer needs to be reconfigured.
func (t *MultiTimer) C() <-chan any {
	return t.outC
}

func (t *MultiTimer) isFired() bool {
	return t.fired.Load()
}

//func xlog(format string, args ...any) {
//fmt.Fprintf(os.Stderr, format+"\n", args...)
//}

type timerCfg struct {
	witness  any
	deadline time.Time
}

func (t *MultiTimer) timerLoop(confC <-chan timerCfg) {
	var (
		timer timer
		cfg   timerCfg
		C     <-chan time.Time
		ok    bool
	)

	for {
		select {
		case cfg, ok = <-confC:
			if timer != nil && !timer.Stop() {
				<-timer.C()
			}
			timer = nil
			C = nil
			if !ok {
				return
			}
			if cfg.witness == nil {
				continue
			}
			timer = t.timerFactory(cfg.deadline)
			C = timer.C()
		case <-C:
			timer.Stop()
			timer = nil
			C = nil
			if t.fired.CompareAndSwap(false, true) {
				t.outC <- cfg.witness
			}
		}
	}
}

func (t *MultiTimer) reconfigure() {
	if t.timerFactory == nil {
		t.timerFactory = newGoTimer
	}
	var (
		witness  any
		deadline time.Time
	)
	for w, d := range t.deadlines {
		if deadline.IsZero() || deadline.After(d) {
			deadline = d
			witness = w
		}
	}

	if t.outC == nil {
		t.outC = make(chan any, 1)
	}
	if t.confC == nil {
		t.confC = make(chan timerCfg, 0)
		go t.timerLoop(t.confC)
	}
	t.confC <- timerCfg{
		witness:  witness,
		deadline: deadline,
	}
}

type timer interface {
	Stop() bool
	C() <-chan time.Time
}

func newGoTimer(deadline time.Time) timer {
	return &goTimerWrapper{
		Timer: time.NewTimer(time.Until(deadline)),
	}
}

type goTimerWrapper struct {
	*time.Timer
}

func (w *goTimerWrapper) C() <-chan time.Time {
	return w.Timer.C
}
