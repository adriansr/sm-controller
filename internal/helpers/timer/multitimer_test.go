package timer

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMultiTimer(t *testing.T) {
	for title, fn := range map[string]testFn{
		"no deadline, no event": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				10: done(),
			}
		},

		"closest deadline triggers": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(5))
					timer.Set("B", intTime(3))
				},
				3: expect(t, timer, "B"),
				8: done(),
			}
		},

		"deadline extend": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(5))
					timer.Set("B", intTime(3))
				},
				2: func() {
					timer.Set("B", intTime(6))
				},
				5: expect(t, timer, "A"),
				8: done(),
			}
		},

		"deadline reduce": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(6))
					timer.Set("B", intTime(4))
				},
				2: func() {
					timer.Set("A", intTime(3))
				},
				3: expect(t, timer, "A"),
				8: done(),
			}
		},

		"no trigger if reused without reset": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(1))
				},
				1: expect(t, timer, "A"),
				2: func() {
					timer.Set("B", intTime(3))
				},
				8: done(),
			}
		},

		"reuse after reset": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(1))
					timer.Set("B", intTime(2))
				},
				1: expect(t, timer, "A"),
				2: func() {
					timer.Reset()
					timer.Set("A", intTime(3))
				},
				3: expect(t, timer, "A"),
				8: done(),
			}
		},

		"clear after set": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(1))
					timer.Clear("A")
					timer.Set("B", intTime(2))
				},
				2: expect(t, timer, "B"),
				8: done(),
			}
		},

		"all clear": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(2))
					timer.Set("B", intTime(3))
				},
				1: func() {
					timer.Clear("A", "B")
				},
				8: done(),
			}
		},

		"trigger auto clears": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(1))
					timer.Set("B", intTime(3))
				},
				1: expect(t, timer, "A"),
				2: func() {
					require.False(t, timer.IsSet("B"))
					timer.Reset()
					timer.Set("C", intTime(4))
				},
				4: expect(t, timer, "C"),
				8: done(),
			}
		},

		"reset without trigger": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(2))
					timer.Set("B", intTime(3))
					timer.Reset()
				},
				8: done(),
			}
		},

		"clear": func(t *testing.T, timer *MultiTimer) map[int64]func() {
			return map[int64]func(){
				0: func() {
					timer.Set("A", intTime(2))
				},
				1: func() {
					timer.Clear("A")
				},
				8: done(),
			}
		},
	} {
		t.Run(title, func(t *testing.T) {
			runTest(t, fn)
		})
	}
}

type testFn func(*testing.T, *MultiTimer) map[int64]func()

func runTest(t *testing.T, testFn testFn) {
	var (
		timer        MultiTimer
		timeProvider testTimerProvider
	)
	timer.timerFactory = timeProvider.new
	cases := testFn(t, &timer)

	for clock, running := int64(0), true; running; clock++ {
		// This name is to add context to failures
		t.Run(fmt.Sprintf("%s[t=%d]", t.Name(), clock), func(t *testing.T) {
			timeProvider.tick(clock)
			if _, found := cases[clock]; found {
				fn := testFn(t, &timer)[clock]
				if fn == nil {
					running = false
					return
				}
				fn()
			} else {
				expectNothing(t, timer.C())()
			}
		})
	}
}

func intTime(i int64) time.Time {
	return time.UnixMilli(i)
}

func expect(t *testing.T, timer *MultiTimer, w any) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		select {
		case x := <-timer.C():
			require.Equal(t, w, x)
		case <-ctx.Done():
			t.Fatalf("expected event %v not received", w)
		}
	}
}

func expectNothing(t *testing.T, C <-chan any) func() {
	return func() {
		select {
		case w := <-C:
			t.Fatalf("received unexpected event %v", w)
		default:
		}
	}
}

func done() func() {
	return nil
}

type testTimerProvider struct {
	mu             sync.Mutex
	deadline       int64
	c              chan time.Time
	lastClock      int64
	empty, running bool
}

func (t *testTimerProvider) new(deadline time.Time) timer {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Two timers in parallel? Shouldn't happen
	if t.running {
		panic(t.running)
	}
	t.deadline = deadline.UnixMilli()
	t.c = make(chan time.Time, 1)
	t.empty = true
	if !intTime(t.lastClock).Before(deadline) {
		t.c <- intTime(t.lastClock)
		t.empty = false
	}
	t.running = true
	return t
}

func (t *testTimerProvider) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	close(t.c)
	t.running = false
	return t.empty
}

func (t *testTimerProvider) C() <-chan time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.c
}

func (t *testTimerProvider) tick(clock int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastClock = clock
	if t.running && t.empty && t.deadline <= clock {
		t.c <- intTime(clock)
		t.empty = false
	}
}
