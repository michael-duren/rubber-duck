---
course: concurrency-gauntlet
title: The Concurrency Gauntlet
language: go
description: Six hard drills across Go's entire concurrency toolkit — select, injectable clocks, channel semaphores, barriers, singleflight, and pipeline plumbing — capped by a dependency-aware parallel job runner. The tests punish leaks, deadlocks, and ordering bugs.
duration_hours: 12
tags: [backend, concurrency, advanced, drills]
extended_reading:
  - title: The Go Memory Model
    url: https://go.dev/ref/mem
  - title: "Go Concurrency Patterns: Pipelines and cancellation"
    url: https://go.dev/blog/pipelines
  - title: Rethinking Classical Concurrency Patterns (Bryan C. Mills)
    url: https://www.youtube.com/watch?v=5zXAHh5tJqQ
  - title: package singleflight (the real one, for after you build yours)
    url: https://pkg.go.dev/golang.org/x/sync/singleflight
---

# Lesson: Select and the Or-Channel {#select-or-channel}

You know `select` picks a ready case uniformly at random. The drill here is
what `select` is *for*: composing cancellation signals. A done-channel is a
`chan struct{}` that is only ever closed, and closing it broadcasts to every
current and future receiver — that's the one channel operation with fan-out.

The classic composition is the **or-channel**: given N done-channels, produce
one channel that closes when *any* of them does. Two viable shapes:

```go
// Recursive: peel off up to 3 channels per layer; the last case recurses
// on the rest plus out itself, so a close of out (the parent dying) also
// wakes up and retires every recursive layer underneath it.
func or(chans ...<-chan struct{}) <-chan struct{} {
	switch len(chans) {
	case 0:
		return nil
	case 1:
		return chans[0]
	}
	out := make(chan struct{})
	go func() {
		defer close(out)
		switch len(chans) {
		case 2:
			select {
			case <-chans[0]:
			case <-chans[1]:
			}
		default:
			select {
			case <-chans[0]:
			case <-chans[1]:
			case <-chans[2]:
			case <-or(append(chans[3:], out)...):
			}
		}
	}()
	return out
}
```

```go
// Flat: one goroutine, reflect.Select over N cases, exits after one fires.
cases := make([]reflect.SelectCase, len(chans))
for i, ch := range chans {
	cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
}
out := make(chan struct{})
go func() {
	defer close(out)
	reflect.Select(cases) // blocks until any one channel is ready or closed
}()
```

The recursive shape is the one worth internalizing: each layer only ever
looks at 2 or 3 concrete channels plus one recursive call, so the select
statement stays a fixed size no matter how many inputs you started with —
but the goroutine count that buys you is roughly N/2, not N/3. Each layer
peels three channels off the front of its list into its select, then
recurses on the rest with its own `out` appended to the tail — appending
that `out` is what lets a close at the top retire every idle layer
underneath. Because each layer strips three but adds one back, the list
shrinks by two per layer, not three, so the recursion runs N/2 layers deep
— N/2 goroutines. (Those trailing `out` passengers pile up at the tail and
only surface as watched slots in the bottom layer or two; they never
displace a fresh input higher up.) Run the numbers and it lands exactly on
`N/2` for the inputs this lesson cares about: 6 inputs cost 3 goroutines,
30 cost 15, 100 cost 50. The flat
shape uses exactly one goroutine regardless of N, at the cost of building
a `reflect.SelectCase` slice and paying reflection overhead on every
call — worth it only if goroutine count matters more than that overhead.

The trap is the obvious third shape: one goroutine per input channel. It works
— and leaks a goroutine for every input that never closes. In a server that
builds an or-channel per request, that's an unbounded leak. The tests below
count goroutines. They will notice.

## Challenge: Or-Channel {#or-channel points=30}

Implement:

```go
func Or(chans ...<-chan struct{}) <-chan struct{}
```

The returned channel must close as soon as **any** input channel closes.
Required semantics:

- **Zero inputs:** return a channel that never closes. Returning `nil` is
  fine — receiving from a nil channel blocks forever.
- **One input:** you may return it directly.
- **Many inputs:** the result closes promptly (within a second is plenty)
  after the first input closes, even with 100 inputs.
- An input that is *already closed* when `Or` is called must close the
  result promptly too.
- **No leaks:** once the result has closed, every goroutine you started must
  exit — even though the other inputs are still open and will never close.
  The tests compare `runtime.NumGoroutine()` before and after (with settling
  time), across repeated calls with 100 inputs each.

### Starter

```go
package challenge

// Or returns a channel that closes as soon as any of the input
// channels closes.
func Or(chans ...<-chan struct{}) <-chan struct{} {
	// TODO: handle zero, one, and many inputs — without leaking a
	// goroutine per input after the result has closed.
	out := make(chan struct{})
	return out
}
```

### Tests

```go
package challenge

import (
	"runtime"
	"testing"
	"time"
)

func waitClosed(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
}

func TestZeroInputsNeverCloses(t *testing.T) {
	select {
	case <-Or():
		t.Fatal("Or() with no inputs must never close")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSingleInput(t *testing.T) {
	ch := make(chan struct{})
	out := Or(ch)
	if out == nil {
		t.Fatal("Or(ch) returned nil for a live input")
	}
	select {
	case <-out:
		t.Fatal("Or(ch) closed before ch did")
	case <-time.After(50 * time.Millisecond):
	}
	close(ch)
	waitClosed(t, out, "Or(ch) did not close after ch closed")
}

func TestAlreadyClosedInput(t *testing.T) {
	live := make(chan struct{})
	dead := make(chan struct{})
	close(dead)
	waitClosed(t, Or(live, dead), "Or must close promptly when an input is already closed")
}

func TestManyInputs(t *testing.T) {
	chans := make([]chan struct{}, 100)
	ins := make([]<-chan struct{}, 100)
	for i := range chans {
		chans[i] = make(chan struct{})
		ins[i] = chans[i]
	}
	out := Or(ins...)
	select {
	case <-out:
		t.Fatal("closed before any input closed")
	case <-time.After(50 * time.Millisecond):
	}
	close(chans[57])
	waitClosed(t, out, "Or over 100 channels did not close after one input closed")
}

func TestNoGoroutineLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	for iter := 0; iter < 5; iter++ {
		chans := make([]chan struct{}, 100)
		ins := make([]<-chan struct{}, 100)
		for i := range chans {
			chans[i] = make(chan struct{})
			ins[i] = chans[i]
		}
		out := Or(ins...)
		close(chans[13])
		waitClosed(t, out, "Or did not close")
		// The other 99 inputs stay open on purpose: any goroutine
		// still waiting on them after out closed is a leak.
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if runtime.NumGoroutine() <= base+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: %d goroutines before, %d after settling",
				base, runtime.NumGoroutine())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

# Lesson: Clocks You Can Control {#clocks-and-token-buckets}

`time.After` in a hot loop allocates a timer per iteration; a `time.Ticker`
you forget to `Stop` keeps firing (Go 1.23+ can at least garbage-collect one
you've dropped every reference to — older releases leaked it outright). You
know this. The deeper drill: **code
that reads the wall clock directly is untestable**, and rate limiters are
where people learn that the hard way — the naive test for "2 requests per
second" is a test that takes seconds and flakes in CI.

The fix is dependency injection at its smallest: take `now func() time.Time`
as a parameter and never call `time.Now` yourself. Tests hand you a fake
clock they advance by hand; production hands you `time.Now`.

The token bucket itself is two lines of math. A bucket holds up to `burst`
tokens and starts full. Tokens drip in continuously at `rate` per second:

```go
elapsed := now().Sub(last) // now is the injected func() time.Time
tokens = min(tokens+elapsed.Seconds()*float64(rate), float64(burst))
```

Each admitted request spends one token. The subtle bugs: forgetting to cap
at `burst`, doing integer math so half-tokens vanish, discarding fractional
progress on a denied call, and racing unsynchronized state under concurrent
callers.

That last one is the data race below: two goroutines both read-modify-write
the same `tokens` (red = the shared, lock-free state); with no lock there's
no happens-before ordering between them, so their updates can interleave and
lose a token.

```d2
direction: right

g1: "goroutine A\nAllow()"
g2: "goroutine B\nAllow()"
tokens: "tokens\nshared, no lock" {
  style.stroke: "#dc2626"
  style.stroke-width: 2
}

g1 -> tokens: "read + write"
g2 -> tokens: "read + write"
```

## Challenge: Deterministic Token Bucket {#token-bucket points=25}

Implement a `Limiter` with:

```go
func New(rate, burst int, now func() time.Time) *Limiter
func (l *Limiter) Allow() bool
```

Required semantics:

- The bucket starts **full** (`burst` tokens). Each `Allow` that returns
  true spends exactly one token; with no whole token available it returns
  false and spends nothing.
- Tokens accrue **continuously** at `rate` per second according to the
  injected clock: two 500ms waits at rate 1 add up to one whole token. A
  denied `Allow` must not discard fractional progress.
- Token count never exceeds `burst`, no matter how long the limiter sits
  idle.
- `Allow` never blocks and must be safe to call from many goroutines. With
  a frozen clock and burst 100, exactly 100 of 1000 concurrent calls may
  succeed.
- Only the injected `now` may be consulted — no `time.Now`, no sleeping.
  The tests drive a fake clock and never wait on real time.

### Starter

```go
package challenge

import "time"

// Limiter is a token-bucket rate limiter driven by an injected clock.
type Limiter struct {
	// TODO: your state here
}

// New returns a limiter holding burst tokens that refills at rate
// tokens per second, reading the current time from now.
func New(rate, burst int, now func() time.Time) *Limiter {
	// TODO
	return &Limiter{}
}

// Allow reports whether a token is available, spending it if so.
func (l *Limiter) Allow() bool {
	// TODO
	return false
}
```

### Tests

```go
package challenge

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestBurstThenDeny(t *testing.T) {
	clk := newFakeClock()
	l := New(1, 3, clk.now)
	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Fatalf("Allow() call %d = false, want true (burst is 3)", i+1)
		}
	}
	if l.Allow() {
		t.Fatal("Allow() = true with an empty bucket and a frozen clock")
	}
}

func TestRefillAtRate(t *testing.T) {
	clk := newFakeClock()
	l := New(2, 10, clk.now)
	for i := 0; i < 10; i++ {
		if !l.Allow() {
			t.Fatalf("draining burst: call %d denied", i+1)
		}
	}
	if l.Allow() {
		t.Fatal("bucket should be empty after draining the burst")
	}
	clk.advance(time.Second) // rate 2/s -> exactly 2 new tokens
	if !l.Allow() {
		t.Fatal("first token after 1s at rate 2 was denied")
	}
	if !l.Allow() {
		t.Fatal("second token after 1s at rate 2 was denied")
	}
	if l.Allow() {
		t.Fatal("third Allow after 1s at rate 2 must be denied")
	}
}

func TestFractionalAccrual(t *testing.T) {
	clk := newFakeClock()
	l := New(1, 1, clk.now)
	if !l.Allow() {
		t.Fatal("burst token denied")
	}
	clk.advance(500 * time.Millisecond)
	if l.Allow() {
		t.Fatal("half a token is not a whole token")
	}
	clk.advance(500 * time.Millisecond)
	if !l.Allow() {
		t.Fatal("two half-second waits at rate 1 must add up to one token (denied calls may not discard fractional progress)")
	}
	if l.Allow() {
		t.Fatal("that token was already spent")
	}
}

func TestCapAtBurst(t *testing.T) {
	clk := newFakeClock()
	l := New(5, 2, clk.now)
	if !l.Allow() || !l.Allow() {
		t.Fatal("burst of 2 should allow 2 calls")
	}
	clk.advance(time.Hour)
	if !l.Allow() || !l.Allow() {
		t.Fatal("bucket should refill back to burst")
	}
	if l.Allow() {
		t.Fatal("bucket must cap at burst, not bank an hour of tokens")
	}
}

func TestConcurrentAllowExactBudget(t *testing.T) {
	clk := newFakeClock()
	l := New(1, 100, clk.now)
	var allowed int32
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if l.Allow() {
					atomic.AddInt32(&allowed, 1)
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Allow blocked or deadlocked under concurrency")
	}
	if got := atomic.LoadInt32(&allowed); got != 100 {
		t.Fatalf("1000 concurrent calls against a frozen clock and burst 100 allowed %d, want exactly 100", got)
	}
}
```

# Lesson: Backpressure by Construction {#channel-semaphores}

A buffered channel *is* a semaphore: capacity = permits, send = acquire,
receive = release. That makes a bounded blocking queue the most honest data
structure in concurrent programming — when the consumer falls behind, the
producer physically stops. No unbounded slices quietly eating your heap.

The hard part is never the happy path; it's shutdown. Three rules make a
closeable queue coherent:

- Only the closer decides "no more input". `Put` after close is an error,
  and a `Put` *blocked at close time* must wake up and get that error too.
- Items already accepted are owed to consumers: after close, `Get` drains
  whatever remains before reporting "done".
- Every blocked party must be woken by `Close`. Forget one waiter and you
  have a goroutine leak that only shows up in production.

You can build this on channels plus a done-channel, or on `sync.Mutex` +
`sync.Cond` (two conditions: "not full" and "not empty"; re-check the
predicate in a loop after every `Wait`; `Broadcast` on close). Both work.
Pick one and get the edge cases right.

## Challenge: Bounded Blocking Queue {#bounded-queue points=30}

Implement a generic FIFO queue:

```go
var ErrClosed = errors.New("queue: closed") // keep this exported variable

func NewQueue[T any](capacity int) *Queue[T]
func (q *Queue[T]) Put(v T) error
func (q *Queue[T]) Get() (T, bool)
func (q *Queue[T]) Close()
```

Required semantics (capacity is always >= 1 in the tests; `Close` is called
at most once):

- `Put` blocks while the queue holds `capacity` items. It returns `nil` on
  success and `ErrClosed` if the queue is closed — **including** when the
  call was already blocked on a full queue when `Close` happened.
- `Get` blocks while the queue is empty and open. It returns `(item, true)`
  in FIFO order; after `Close` it keeps returning buffered items until the
  queue is drained, then returns `(zero, false)`. A `Get` blocked on an
  empty queue when `Close` happens returns `(zero, false)`.
- `Close` wakes every blocked `Put` and `Get`. Nothing stays parked.
- Everything must be safe under many concurrent producers and consumers.

### Starter

```go
package challenge

import "errors"

// ErrClosed is returned by Put once the queue has been closed.
var ErrClosed = errors.New("queue: closed")

// Queue is a bounded, blocking FIFO queue.
type Queue[T any] struct {
	items []T
}

// NewQueue returns an empty queue that holds at most capacity items.
func NewQueue[T any](capacity int) *Queue[T] {
	// TODO: enforce the capacity bound and the blocking semantics
	return &Queue[T]{}
}

// Put appends v, blocking while the queue is full.
func (q *Queue[T]) Put(v T) error {
	// TODO: this neither blocks nor respects capacity or Close
	q.items = append(q.items, v)
	return nil
}

// Get removes and returns the oldest item, blocking while the queue
// is empty and open. ok is false once the queue is closed and drained.
func (q *Queue[T]) Get() (v T, ok bool) {
	// TODO: this returns early instead of blocking
	if len(q.items) == 0 {
		return v, false
	}
	v = q.items[0]
	q.items = q.items[1:]
	return v, true
}

// Close marks the queue closed and wakes all blocked callers.
func (q *Queue[T]) Close() {
	// TODO
}
```

### Tests

```go
package challenge

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func within(t *testing.T, d time.Duration, msg string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal(msg)
	}
}

func TestFIFOOrder(t *testing.T) {
	q := NewQueue[int](4)
	go func() {
		for i := 0; i < 100; i++ {
			q.Put(i)
		}
		q.Close()
	}()
	var got []int
	within(t, 5*time.Second, "producer/consumer pair deadlocked", func() {
		for {
			v, ok := q.Get()
			if !ok {
				return
			}
			got = append(got, v)
		}
	})
	if len(got) != 100 {
		t.Fatalf("got %d values, want 100", len(got))
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("got[%d] = %d, want %d (FIFO order)", i, v, i)
		}
	}
}

func TestGetBlocksUntilPut(t *testing.T) {
	q := NewQueue[int](1)
	type res struct {
		v  int
		ok bool
	}
	got := make(chan res, 1)
	go func() {
		v, ok := q.Get()
		got <- res{v, ok}
	}()
	select {
	case r := <-got:
		t.Fatalf("Get returned (%v, %v) on an empty open queue; want it to block", r.v, r.ok)
	case <-time.After(100 * time.Millisecond):
	}
	if err := q.Put(42); err != nil {
		t.Fatalf("Put: %v", err)
	}
	select {
	case r := <-got:
		if !r.ok || r.v != 42 {
			t.Fatalf("Get = (%v, %v), want (42, true)", r.v, r.ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Get still blocked after a Put")
	}
}

func TestPutBlocksWhenFull(t *testing.T) {
	q := NewQueue[string](2)
	if err := q.Put("a"); err != nil {
		t.Fatalf("Put(a): %v", err)
	}
	if err := q.Put("b"); err != nil {
		t.Fatalf("Put(b): %v", err)
	}
	var landed int32
	go func() {
		q.Put("c")
		atomic.StoreInt32(&landed, 1)
	}()
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(&landed) == 1 {
		t.Fatal("Put returned immediately on a full queue; want it to block")
	}
	if v, ok := q.Get(); !ok || v != "a" {
		t.Fatalf("Get = (%q, %v), want (\"a\", true)", v, ok)
	}
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&landed) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("blocked Put was not released by a Get")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if v, ok := q.Get(); !ok || v != "b" {
		t.Fatalf("Get = (%q, %v), want (\"b\", true)", v, ok)
	}
	within(t, 2*time.Second, "third Get blocked", func() {
		if v, ok := q.Get(); !ok || v != "c" {
			t.Errorf("Get = (%q, %v), want (\"c\", true)", v, ok)
		}
	})
}

func TestCloseDrainsThenDone(t *testing.T) {
	q := NewQueue[int](5)
	for i := 1; i <= 3; i++ {
		if err := q.Put(i); err != nil {
			t.Fatalf("Put(%d): %v", i, err)
		}
	}
	q.Close()
	var got []int
	within(t, 2*time.Second, "Get blocked on a closed queue", func() {
		for {
			v, ok := q.Get()
			if !ok {
				return
			}
			got = append(got, v)
		}
	})
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("drained %v after Close, want [1 2 3]", got)
	}
	if err := q.Put(9); !errors.Is(err, ErrClosed) {
		t.Fatalf("Put after Close = %v, want ErrClosed", err)
	}
}

func TestCloseUnblocksPut(t *testing.T) {
	q := NewQueue[int](1)
	if err := q.Put(1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- q.Put(2) }()
	time.Sleep(150 * time.Millisecond)
	q.Close()
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Put blocked across Close returned %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not release the blocked Put")
	}
}

func TestCloseUnblocksGet(t *testing.T) {
	q := NewQueue[int](1)
	okCh := make(chan bool, 1)
	go func() {
		_, ok := q.Get()
		okCh <- ok
	}()
	time.Sleep(150 * time.Millisecond)
	q.Close()
	select {
	case ok := <-okCh:
		if ok {
			t.Fatal("Get on an empty closed queue reported ok = true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not release the blocked Get")
	}
}

func TestConcurrentProducersConsumers(t *testing.T) {
	q := NewQueue[int](2)
	const producers, perProducer, consumers = 4, 50, 3
	var wgProd sync.WaitGroup
	for p := 0; p < producers; p++ {
		wgProd.Add(1)
		go func() {
			defer wgProd.Done()
			for i := 0; i < perProducer; i++ {
				if err := q.Put(i); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
			}
		}()
	}
	var count, sum int64
	var wgCons sync.WaitGroup
	for c := 0; c < consumers; c++ {
		wgCons.Add(1)
		go func() {
			defer wgCons.Done()
			for {
				v, ok := q.Get()
				if !ok {
					return
				}
				atomic.AddInt64(&count, 1)
				atomic.AddInt64(&sum, int64(v))
			}
		}()
	}
	within(t, 5*time.Second, "producers deadlocked", wgProd.Wait)
	q.Close()
	within(t, 5*time.Second, "consumers deadlocked", wgCons.Wait)
	if got := atomic.LoadInt64(&count); got != producers*perProducer {
		t.Fatalf("consumed %d values, want %d", got, producers*perProducer)
	}
	wantSum := int64(producers) * int64(perProducer*(perProducer-1)/2)
	if got := atomic.LoadInt64(&sum); got != wantSum {
		t.Fatalf("sum of consumed values = %d, want %d", got, wantSum)
	}
}
```

# Lesson: Barriers and Rendezvous {#barriers-rendezvous}

`sync.WaitGroup` is a one-shot: N workers finish, one waiter proceeds. A
**cyclic barrier** is the reusable, symmetric version — N goroutines each
call `Await`, everyone blocks until the Nth arrives, then *all* of them are
released together and the barrier resets for the next round. Think lockstep
simulation phases, parallel iterative solvers, or "everyone reloads config
between batches".

The bug that kills naive implementations is **generation mixing**. A fast
goroutine released from round `k` loops around and calls `Await` for round
`k+1` while a slow sibling from round `k` hasn't woken up yet. If your state
is just a counter, the fast arrival corrupts the round the sleeper is still
in. The standard fix is a generation number: waiters sleep while
`gen == myGen`, and the last arrival increments `gen`, resets the count, and
broadcasts.

`sync.Cond` fits perfectly here — one mutex, `Wait` in a predicate loop,
`Broadcast` from the last arrival. You can also do it with a channel per
generation. Either way: nobody gets out early, and nobody's arrival leaks
into the next round.

## Challenge: Cyclic Barrier {#cyclic-barrier points=30}

Implement:

```go
func NewBarrier(n int) *Barrier
func (b *Barrier) Await()
```

Required semantics (n >= 1; every goroutine uses the barrier for its whole
lifetime):

- `Await` blocks until `n` goroutines (counting the caller) have called it
  in the current generation, then all `n` return and the barrier resets.
- **No early release:** with only `n-1` arrivals, nobody returns.
- **Reusable:** the same `Barrier` must work for many consecutive
  generations, including when fast goroutines re-enter `Await` for the next
  generation while slow ones are still waking from the last. The tests run
  4 goroutines through 25 generations and check an arrival counter per
  generation at every release.

### Starter

```go
package challenge

// Barrier is a reusable cyclic barrier for groups of n goroutines.
type Barrier struct {
	n int
}

// NewBarrier returns a barrier that releases waiters in groups of n.
func NewBarrier(n int) *Barrier {
	return &Barrier{n: n}
}

// Await blocks until n goroutines (including this one) have called
// Await in the current generation, then releases them all and resets
// the barrier for the next generation.
func (b *Barrier) Await() {
	// TODO: currently everyone sails straight through
}
```

### Tests

```go
package challenge

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReleasesAllTogether(t *testing.T) {
	const n = 4
	b := NewBarrier(n)
	for gen := 0; gen < 3; gen++ {
		var passed int32
		for i := 0; i < n-1; i++ {
			go func() {
				b.Await()
				atomic.AddInt32(&passed, 1)
			}()
		}
		time.Sleep(150 * time.Millisecond)
		if got := atomic.LoadInt32(&passed); got != 0 {
			t.Fatalf("generation %d: %d waiters passed with only %d of %d arrived", gen, got, n-1, n)
		}
		release := make(chan struct{})
		go func() {
			b.Await()
			close(release)
		}()
		select {
		case <-release:
		case <-time.After(2 * time.Second):
			t.Fatalf("generation %d: barrier deadlocked after all %d arrived", gen, n)
		}
		deadline := time.Now().Add(2 * time.Second)
		for atomic.LoadInt32(&passed) != n-1 {
			if time.Now().After(deadline) {
				t.Fatalf("generation %d: only %d of %d waiters were released",
					gen, atomic.LoadInt32(&passed), n-1)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestGenerationsDoNotMix(t *testing.T) {
	const n = 4
	const generations = 25
	b := NewBarrier(n)
	arrived := make([]int32, generations)
	var early int32
	var wg sync.WaitGroup
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for g := 0; g < generations; g++ {
				atomic.AddInt32(&arrived[g], 1)
				b.Await()
				if atomic.LoadInt32(&arrived[g]) != n {
					atomic.AddInt32(&early, 1)
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("barrier deadlocked across generations")
	}
	if got := atomic.LoadInt32(&early); got != 0 {
		t.Fatalf("%d Await returns happened before all %d goroutines had arrived in that generation", got, n)
	}
}
```

# Lesson: Duplicate Suppression {#duplicate-suppression}

Cache expires, 500 requests arrive in the same millisecond, all 500 miss,
all 500 hit the database with the identical query. That's a cache stampede,
and the cure is **singleflight**: if a call for key K is already in flight,
don't start another — wait for the running one and share its result.

The design is a map of in-flight calls guarded by a mutex, plus a
done-channel per call:

- First caller for K: insert a `call{done: make(chan struct{})}` under the
  lock, unlock, run `fn` **outside the lock** (never hold a mutex across
  user code), store the result, then clean up and `close(done)`.
- Every other caller for K: find the entry under the lock, unlock,
  `<-done`, read the shared result.

The trap is the *insert*, not the cleanup order: checking "is K already in
flight" and inserting a new call for it must happen as one atomic step under
the lock. Split it into two lock acquisitions — check, unlock, then
insert — and two callers arriving at the same instant can each conclude
they're first, and both invoke `fn`. Whether you delete the map entry before
or after closing `done` doesn't actually matter for correctness: `fn`'s
result is written once, before anyone can observe it, so a caller that finds
the entry still present just gets an already-final result a little late,
and one that finds it gone starts a fresh call — neither reads a value
that's "about to be overwritten." (For what it's worth, the real
`golang.org/x/sync/singleflight` releases its waiters *before* deleting the
map entry — the opposite of what you might guess — because both happen
inside one uninterrupted locked section, so no caller can observe one
without the other anyway.) What does matter: never hold the lock while `fn`
runs — that would serialize every key, not just repeats of the same one —
and always remove the entry eventually, so the next caller for K gets a
fresh flight. Also note what singleflight is *not*: it's not a cache. Once a
flight completes, the next caller starts a fresh one.

## Challenge: Singleflight From Scratch {#singleflight points=35}

Implement:

```go
type Group struct{ ... } // zero value must be ready to use

func (g *Group) Do(key string, fn func() (int, error)) (int, error)
```

Required semantics:

- While a call for `key` is in flight, every additional `Do(key, ...)`
  waits for it and returns the **same** `(value, error)` — their own `fn`
  is never invoked. The tests fire 50 concurrent callers at one key and
  require exactly one invocation.
- Errors are shared exactly like values.
- Calls for **different keys** must not block each other — the tests
  rendezvous two `fn`s for different keys and require them to overlap in
  time.
- No caching: once a flight completes, the next `Do` for that key invokes
  `fn` again.

### Starter

```go
package challenge

// Group deduplicates concurrent calls that share a key. The zero
// value is ready to use.
type Group struct {
	// TODO: track in-flight calls per key
}

// Do runs fn for key — unless a call for key is already in flight, in
// which case it waits for that call and shares its result.
func (g *Group) Do(key string, fn func() (int, error)) (int, error) {
	// TODO: currently every caller invokes fn — no suppression at all
	return fn()
}
```

### Tests

```go
package challenge

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestConcurrentCallersShareOneCall(t *testing.T) {
	var g Group
	var calls, started int32
	gate := make(chan struct{})
	const n = 50
	type out struct {
		v   int
		err error
	}
	outs := make(chan out, n)
	for i := 0; i < n; i++ {
		go func() {
			atomic.AddInt32(&started, 1)
			v, err := g.Do("hot-key", func() (int, error) {
				atomic.AddInt32(&calls, 1)
				<-gate
				return 42, nil
			})
			outs <- out{v, err}
		}()
	}
	waitFor(t, "no caller ever invoked fn", func() bool {
		return atomic.LoadInt32(&started) == n && atomic.LoadInt32(&calls) >= 1
	})
	time.Sleep(250 * time.Millisecond) // let every caller join the in-flight call
	close(gate)
	for i := 0; i < n; i++ {
		select {
		case r := <-outs:
			if r.err != nil || r.v != 42 {
				t.Fatalf("caller got (%d, %v), want (42, nil)", r.v, r.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d callers got a result", i, n)
		}
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("fn ran %d times for %d concurrent callers, want exactly 1", c, n)
	}
}

func TestDistinctKeysRunConcurrently(t *testing.T) {
	var g Group
	aStarted := make(chan struct{})
	bStarted := make(chan struct{})
	var overlapped int32
	type out struct {
		v   int
		err error
	}
	aDone := make(chan out, 1)
	bDone := make(chan out, 1)
	go func() {
		v, err := g.Do("a", func() (int, error) {
			close(aStarted)
			select {
			case <-bStarted:
				atomic.AddInt32(&overlapped, 1)
			case <-time.After(1 * time.Second):
			}
			return 1, nil
		})
		aDone <- out{v, err}
	}()
	go func() {
		v, err := g.Do("b", func() (int, error) {
			close(bStarted)
			select {
			case <-aStarted:
				atomic.AddInt32(&overlapped, 1)
			case <-time.After(1 * time.Second):
			}
			return 2, nil
		})
		bDone <- out{v, err}
	}()
	select {
	case r := <-aDone:
		if r.err != nil || r.v != 1 {
			t.Fatalf(`Do("a") = (%d, %v), want (1, nil)`, r.v, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(`Do("a") never returned`)
	}
	select {
	case r := <-bDone:
		if r.err != nil || r.v != 2 {
			t.Fatalf(`Do("b") = (%d, %v), want (2, nil)`, r.v, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(`Do("b") never returned`)
	}
	if atomic.LoadInt32(&overlapped) != 2 {
		t.Fatal("calls for distinct keys blocked each other; they must run concurrently")
	}
}

func TestErrorsAreShared(t *testing.T) {
	var g Group
	sentinel := errors.New("upstream exploded")
	var calls, started int32
	gate := make(chan struct{})
	const n = 10
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			atomic.AddInt32(&started, 1)
			_, err := g.Do("bad-key", func() (int, error) {
				atomic.AddInt32(&calls, 1)
				<-gate
				return 0, sentinel
			})
			errs <- err
		}()
	}
	waitFor(t, "no caller ever invoked fn", func() bool {
		return atomic.LoadInt32(&started) == n && atomic.LoadInt32(&calls) >= 1
	})
	time.Sleep(250 * time.Millisecond)
	close(gate)
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			if !errors.Is(err, sentinel) {
				t.Fatalf("caller got err %v, want the shared sentinel error", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d callers got a result", i, n)
		}
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("fn ran %d times, want exactly 1", c)
	}
}

func TestCompletedFlightIsNotCached(t *testing.T) {
	var g Group
	var calls int32
	fn := func() (int, error) {
		return int(atomic.AddInt32(&calls, 1)), nil
	}
	type out struct {
		v   int
		err error
	}
	res := make(chan out, 1)
	for want := 1; want <= 2; want++ {
		go func() {
			v, err := g.Do("k", fn)
			res <- out{v, err}
		}()
		select {
		case r := <-res:
			if r.err != nil || r.v != want {
				t.Fatalf("sequential call %d: got (%d, %v), want (%d, nil)", want, r.v, r.err, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("sequential call %d never returned", want)
		}
	}
}

func TestManyKeysUnderLoad(t *testing.T) {
	var g Group
	const keys = 5
	const callersPerKey = 40
	var started int32
	calls := make([]int32, keys)
	gate := make(chan struct{})
	type out struct {
		key int
		v   int
		err error
	}
	outs := make(chan out, keys*callersPerKey)
	for k := 0; k < keys; k++ {
		for i := 0; i < callersPerKey; i++ {
			go func(k int) {
				atomic.AddInt32(&started, 1)
				v, err := g.Do(fmt.Sprintf("key-%d", k), func() (int, error) {
					atomic.AddInt32(&calls[k], 1)
					<-gate
					return k * 100, nil
				})
				outs <- out{k, v, err}
			}(k)
		}
	}
	waitFor(t, "flights never started for every key", func() bool {
		if atomic.LoadInt32(&started) != keys*callersPerKey {
			return false
		}
		for k := 0; k < keys; k++ {
			if atomic.LoadInt32(&calls[k]) < 1 {
				return false
			}
		}
		return true
	})
	time.Sleep(250 * time.Millisecond)
	close(gate)
	for i := 0; i < keys*callersPerKey; i++ {
		select {
		case r := <-outs:
			if r.err != nil || r.v != r.key*100 {
				t.Fatalf("caller for key %d got (%d, %v), want (%d, nil)", r.key, r.v, r.err, r.key*100)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d callers finished", i, keys*callersPerKey)
		}
	}
	for k := 0; k < keys; k++ {
		if c := atomic.LoadInt32(&calls[k]); c != 1 {
			t.Fatalf("key %d: fn ran %d times, want exactly 1", k, c)
		}
	}
}
```

# Lesson: Pipeline Surgery {#pipeline-surgery}

Pipelines compose when every stage honors the same contract: consume until
the input closes, close your outputs when you're done, never strand a
goroutine. Two plumbing fittings show up constantly:

- **Tee** splits one stream into two, like the shell command — every value
  goes to *both* outputs.
- **Bridge** flattens a channel of channels (`<-chan <-chan T`) into one
  stream (`<-chan T`): read the next inner channel off the outer one, drain
  it completely, then move to the next. It shows up when a producer hands
  you a fresh channel per unit of work — pagination, one channel per
  shard — and consumers just want one linear stream instead of juggling N.

This lesson's challenge below is Tee only; Bridge is here so the name isn't
a surprise if you meet it in the wild.

Tee has a sharp edge: you must send each value to both outputs, but the two
consumers run at different speeds, and you can't just send to one then the
other — if the first consumer stalls, the second starves even though it's
ready. The idiomatic move is the **nil-channel trick**: in a loop, select
over both sends, and after a send succeeds set that channel variable to
`nil` so its case goes dormant until the next value:

```go
o1, o2 := out1, out2
for i := 0; i < 2; i++ {
	select {
	case o1 <- v:
		o1 = nil
	case o2 <- v:
		o2 = nil
	}
}
```

Note the honest consequence: tee advances one value at a time, so the fast
consumer can run at most one value ahead of the slow one. That's correct —
it's backpressure, not a bug. What *is* a bug: forgetting to close both
outputs, or leaving the forwarding goroutine alive after the input closes.

## Challenge: Tee {#tee-channel points=35}

Implement:

```go
func Tee(in <-chan int) (<-chan int, <-chan int)
```

Required semantics:

- Every value received from `in` is delivered to **both** outputs, each in
  the original input order.
- It must work with the two consumers running at different speeds, as long
  as both are actively consumed. (Lockstep coupling — the fast output
  waiting at most one value for the slow one — is expected and fine.)
- When `in` closes, both outputs close after delivering everything,
  including the zero-value case where `in` closes immediately.
- **No leaks:** after a full tee cycle completes, every goroutine you
  started must have exited. The tests run several cycles and compare
  `runtime.NumGoroutine()` before and after with settling time.

### Starter

```go
package challenge

// Tee fans every value from in out to both returned channels.
func Tee(in <-chan int) (<-chan int, <-chan int) {
	// TODO: forward every value to BOTH outputs, close both when in
	// closes, and don't leak the forwarding goroutine.
	out1 := make(chan int)
	out2 := make(chan int)
	close(out1)
	close(out2)
	return out1, out2
}
```

### Tests

```go
package challenge

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestBothOutputsGetEverything(t *testing.T) {
	const n = 200
	in := make(chan int)
	go func() {
		for i := 0; i < n; i++ {
			in <- i
		}
		close(in)
	}()
	out1, out2 := Tee(in)
	var got1, got2 []int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for v := range out1 { // fast consumer
			got1 = append(got1, v)
		}
	}()
	go func() {
		defer wg.Done()
		for v := range out2 { // slow consumer
			got2 = append(got2, v)
			if len(got2)%20 == 0 {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tee stalled with two consumers running at different speeds")
	}
	for name, got := range map[string][]int{"out1": got1, "out2": got2} {
		if len(got) != n {
			t.Fatalf("%s received %d values, want %d", name, len(got), n)
		}
		for i, v := range got {
			if v != i {
				t.Fatalf("%s[%d] = %d, want %d (input order must be preserved)", name, i, v, i)
			}
		}
	}
}

func TestClosesOnEmptyInput(t *testing.T) {
	in := make(chan int)
	close(in)
	out1, out2 := Tee(in)
	for i, out := range []<-chan int{out1, out2} {
		select {
		case v, ok := <-out:
			if ok {
				t.Fatalf("output %d yielded unexpected value %d", i+1, v)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("output %d was not closed after the input closed", i+1)
		}
	}
}

func TestNoGoroutineLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	for iter := 0; iter < 3; iter++ {
		in := make(chan int)
		go func() {
			for i := 0; i < 50; i++ {
				in <- i
			}
			close(in)
		}()
		out1, out2 := Tee(in)
		var wg sync.WaitGroup
		wg.Add(2)
		for _, out := range []<-chan int{out1, out2} {
			go func(c <-chan int) {
				defer wg.Done()
				for range c {
				}
			}(out)
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("tee cycle did not complete")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if runtime.NumGoroutine() <= base+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: %d goroutines before, %d after settling",
				base, runtime.NumGoroutine())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

# Final Challenge: Dependency-Aware Job Runner {#final-job-runner points=100}

Everything at once: a build-system core. Implement:

```go
type Job struct {
	Deps []string      // names of jobs that must succeed first
	Fn   func() error  // the work
}

func Run(jobs map[string]Job, parallelism int) error
```

Required semantics (`parallelism` is always >= 1 in the tests):

- **Validation first.** If any job lists a dependency that isn't a key in
  `jobs`, or the dependency graph contains a cycle, `Run` returns an error
  **before invoking any `Fn`**.
- **Ordering.** A job's `Fn` starts only after every one of its
  dependencies' `Fn`s has returned `nil`. Each job runs **exactly once**.
- **Parallelism.** At most `parallelism` `Fn`s execute at any moment — the
  tests track an atomic high-water mark of in-flight jobs. Independent jobs
  must actually overlap: a serial runner (high-water mark 1 with cap 3 and
  ten independent jobs) fails.
- **Fail fast.** When any `Fn` returns an error, no new jobs are started
  (jobs already running may finish), and `Run` returns that first error.
  Jobs depending on a failed job never run.
- Empty `jobs` map: return `nil`.
- `Run` must always return — the tests wrap every call in a deadlock guard.

Suggested shape: validate with Kahn's algorithm (indegree counting — it
gives you cycle detection *and* the ready set), then run a single
coordinator loop that launches ready jobs while a completion channel feeds
back results. No mutex needed if only the coordinator touches the graph
state.

That shape looks like this: the coordinator loop (violet) launches a
goroutine per ready job, keeping at most `parallelism` of them in flight;
each finished job reports on the completion channel, which feeds results
back so the coordinator can launch the next ready jobs.

```d2
direction: down

coord: "coordinator loop (owns graph state)" {
  style.stroke: "#a78bfa"
  style.stroke-width: 2
}

inflight: "at most parallelism in flight" {
  grid-rows: 1
  j1: "job Fn"
  j2: "job Fn"
  j3: "job Fn"
}

done: "completion channel"

coord -> inflight: "launch ready"
inflight -> done
done -> coord: "unblocks next ready"
```

### Starter

```go
package challenge

// Job is a unit of work with named dependencies.
type Job struct {
	Deps []string
	Fn   func() error
}

// Run executes every job exactly once, only after its dependencies
// have succeeded, with at most parallelism Fns in flight at once. The
// first Fn error stops scheduling and is returned. Unknown deps and
// dependency cycles are rejected up front, before any Fn runs.
func Run(jobs map[string]Job, parallelism int) error {
	// TODO: this ignores deps, cycles, fail-fast, and the cap
	for _, j := range jobs {
		if err := j.Fn(); err != nil {
			return err
		}
	}
	return nil
}
```

### Tests

```go
package challenge

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func runGuard(t *testing.T, jobs map[string]Job, parallelism int) error {
	t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- Run(jobs, parallelism) }()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return — deadlock or lost goroutine")
		return nil
	}
}

// tracker builds jobs that record dependency violations and run counts.
type tracker struct {
	mu         sync.Mutex
	runs       map[string]*int32
	violations int32
}

func newTracker() *tracker {
	return &tracker{runs: map[string]*int32{}}
}

func (tr *tracker) job(name string, deps ...string) Job {
	c := new(int32)
	tr.mu.Lock()
	tr.runs[name] = c
	tr.mu.Unlock()
	return Job{Deps: deps, Fn: func() error {
		for _, d := range deps {
			if atomic.LoadInt32(tr.runs[d]) == 0 {
				atomic.AddInt32(&tr.violations, 1)
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(c, 1)
		return nil
	}}
}

func (tr *tracker) check(t *testing.T) {
	t.Helper()
	if v := atomic.LoadInt32(&tr.violations); v != 0 {
		t.Fatalf("%d job starts happened before a dependency had finished", v)
	}
	for name, c := range tr.runs {
		if got := atomic.LoadInt32(c); got != 1 {
			t.Fatalf("job %q ran %d times, want exactly 1", name, got)
		}
	}
}

func TestDiamond(t *testing.T) {
	tr := newTracker()
	jobs := map[string]Job{
		"fetch":   tr.job("fetch"),
		"lint":    tr.job("lint", "fetch"),
		"compile": tr.job("compile", "fetch"),
		"link":    tr.job("link", "lint", "compile"),
	}
	if err := runGuard(t, jobs, 4); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	tr.check(t)
}

func TestLayeredGraph(t *testing.T) {
	tr := newTracker()
	jobs := map[string]Job{}
	var prev []string
	for l := 0; l < 3; l++ {
		var cur []string
		for i := 0; i < 6; i++ {
			name := fmt.Sprintf("l%d-%d", l, i)
			var deps []string
			if l > 0 {
				deps = []string{prev[i], prev[(i+1)%6]}
			}
			jobs[name] = tr.job(name, deps...)
			cur = append(cur, name)
		}
		prev = cur
	}
	if err := runGuard(t, jobs, 4); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	tr.check(t)
}

func TestCycleDetectedUpFront(t *testing.T) {
	var calls int32
	count := func() error { atomic.AddInt32(&calls, 1); return nil }
	jobs := map[string]Job{
		"a":    {Deps: []string{"b"}, Fn: count},
		"b":    {Deps: []string{"c"}, Fn: count},
		"c":    {Deps: []string{"a"}, Fn: count},
		"solo": {Fn: count},
	}
	if err := runGuard(t, jobs, 4); err == nil {
		t.Fatal("Run = nil, want a cycle error")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("%d Fns ran despite the cycle; cycles must be rejected before running anything", got)
	}
}

func TestUnknownDependency(t *testing.T) {
	var calls int32
	jobs := map[string]Job{
		"a": {Deps: []string{"ghost"}, Fn: func() error {
			atomic.AddInt32(&calls, 1)
			return nil
		}},
	}
	if err := runGuard(t, jobs, 2); err == nil {
		t.Fatal("Run = nil, want an error for a dependency on a job that does not exist")
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("a Fn ran despite referencing a missing dependency")
	}
}

func TestFailFast(t *testing.T) {
	boom := errors.New("boom")
	var afterBoom, afterGate int32
	jobs := map[string]Job{
		"boom": {Fn: func() error { return boom }},
		"gate": {Fn: func() error {
			time.Sleep(300 * time.Millisecond)
			return nil
		}},
		"needs-boom": {Deps: []string{"boom"}, Fn: func() error {
			atomic.AddInt32(&afterBoom, 1)
			return nil
		}},
	}
	for i := 0; i < 20; i++ {
		jobs[fmt.Sprintf("needs-gate-%d", i)] = Job{Deps: []string{"gate"}, Fn: func() error {
			atomic.AddInt32(&afterGate, 1)
			return nil
		}}
	}
	err := runGuard(t, jobs, 2)
	if !errors.Is(err, boom) {
		t.Fatalf("Run = %v, want the first job error (boom)", err)
	}
	if atomic.LoadInt32(&afterBoom) != 0 {
		t.Fatal("a job whose dependency failed was still run")
	}
	if got := atomic.LoadInt32(&afterGate); got != 0 {
		t.Fatalf("%d new jobs were scheduled after the failure; fail fast means zero", got)
	}
}

func TestParallelismCap(t *testing.T) {
	const parallelism = 3
	var inFlight, highWater int32
	fn := func() error {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			hw := atomic.LoadInt32(&highWater)
			if cur <= hw || atomic.CompareAndSwapInt32(&highWater, hw, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return nil
	}
	jobs := map[string]Job{}
	for i := 0; i < 10; i++ {
		jobs[fmt.Sprintf("job-%d", i)] = Job{Fn: fn}
	}
	if err := runGuard(t, jobs, parallelism); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	hw := atomic.LoadInt32(&highWater)
	if hw > parallelism {
		t.Fatalf("in-flight high-water mark %d exceeds parallelism %d", hw, parallelism)
	}
	if hw < 2 {
		t.Fatalf("in-flight high-water mark %d: independent jobs never overlapped; the runner must actually use its parallelism", hw)
	}
}

func TestEmptyAndSingle(t *testing.T) {
	if err := runGuard(t, map[string]Job{}, 4); err != nil {
		t.Fatalf("Run(empty) = %v, want nil", err)
	}
	ran := false
	single := map[string]Job{
		"only": {Fn: func() error { ran = true; return nil }},
	}
	if err := runGuard(t, single, 1); err != nil {
		t.Fatalf("Run(single) = %v, want nil", err)
	}
	if !ran {
		t.Fatal("the single job never ran")
	}
}
```
