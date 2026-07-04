---
course: advanced-concurrency
title: Advanced Go Concurrency
language: go
description: Cancellation with context, sync primitives beyond Mutex, atomics and the memory model, bounded parallelism, and errgroup rebuilt from scratch.
duration_hours: 10
tags: [backend, concurrency, advanced]
extended_reading:
  - title: The Go Memory Model
    url: https://go.dev/ref/mem
  - title: "Go Concurrency Patterns: Pipelines and cancellation"
    url: https://go.dev/blog/pipelines
  - title: "Go Concurrency Patterns: Context"
    url: https://go.dev/blog/context
---

# Lesson: Context — Cancellation, Deadlines, Propagation {#context-cancellation}

Go gives you no way to kill a goroutine. That is deliberate: forcibly
stopping a thread mid-flight leaves locks held and invariants broken, so
cancellation in Go is *cooperative* — you ask, the goroutine complies.
`context.Context` is the standard way to ask.

A context carries three things across API boundaries: a cancellation
signal, an optional deadline, and request-scoped values. The signal is a
channel, closed exactly once:

```go
ctx, cancel := context.WithTimeout(parent, 2*time.Second)
defer cancel() // always: releases the timer and any child goroutines

select {
case res := <-work:
	return res, nil
case <-ctx.Done():
	return nil, ctx.Err() // context.Canceled or context.DeadlineExceeded
}
```

Contexts form a tree. `WithCancel`, `WithTimeout`, and `WithDeadline`
derive a child from a parent; canceling a parent cancels every
descendant, while canceling a child never affects the parent. This is
what makes the abstraction compose: an HTTP handler's context is
canceled when the client disconnects, and every database call, RPC, and
worker goroutine spawned under it unwinds automatically — provided each
one actually selects on `ctx.Done()` while blocking.

Two rules keep this honest. First, `ctx` is always the first parameter,
never stored in a struct — it belongs to a call chain, not an object.
Second, if you create a context, you `defer cancel()` even on success:
an un-canceled context pins its parent's resources and leaks the
goroutines watching it.

The pattern you will implement below — race several attempts, keep the
first success, cancel the rest — is the canonical use of a *locally
derived* cancel: winners cancel losers, without touching the caller's
context.

## Challenge: First Result Wins {#first-result points=30}

Implement:

```go
func FirstResult(ctx context.Context, fns []func(ctx context.Context) (string, error)) (string, error)
```

Semantics:

- Run every `fn` concurrently, each receiving a context derived from
  `ctx`.
- The first `fn` to return a nil error wins: return its string and
  cancel the context passed to the remaining fns, promptly.
- If every `fn` fails, return a non-nil error (any of the failures is
  acceptable).
- If `ctx` is canceled before any success, return promptly with a
  non-nil error.
- If `fns` is empty, return a non-nil error.

`FirstResult` must not wait for the losers to finish, but the losers
must observe cancellation through their context.

### Starter

```go
package challenge

import "context"

// FirstResult runs every fn concurrently and returns the first
// successful result, canceling the rest. It returns an error only if
// every fn fails, fns is empty, or ctx is done first.
func FirstResult(ctx context.Context, fns []func(ctx context.Context) (string, error)) (string, error) {
	// TODO: fan out, return the first success, cancel the losers.
	return "", nil
}
```

### Tests

```go
package challenge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func runFirstResult(t *testing.T, ctx context.Context, fns []func(context.Context) (string, error)) (string, error) {
	t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := FirstResult(ctx, fns)
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(3 * time.Second):
		t.Fatal("FirstResult did not return within 3s (deadlock?)")
		return "", nil
	}
}

func TestFirstSuccessWinsAndLosersAreCanceled(t *testing.T) {
	var canceled atomic.Int32
	blocker := func(ctx context.Context) (string, error) {
		<-ctx.Done()
		canceled.Add(1)
		return "", ctx.Err()
	}
	fns := []func(context.Context) (string, error){
		blocker,
		func(ctx context.Context) (string, error) { return "fast", nil },
		blocker,
	}
	got, err := runFirstResult(t, context.Background(), fns)
	if err != nil || got != "fast" {
		t.Fatalf("FirstResult = (%q, %v), want (%q, nil)", got, err, "fast")
	}
	deadline := time.Now().Add(2 * time.Second)
	for canceled.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("losing fns never observed cancellation (saw %d of 2)", canceled.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFailureThenSlowSuccess(t *testing.T) {
	fns := []func(context.Context) (string, error){
		func(ctx context.Context) (string, error) { return "", errors.New("nope") },
		func(ctx context.Context) (string, error) {
			select {
			case <-time.After(50 * time.Millisecond):
				return "slow", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	got, err := runFirstResult(t, context.Background(), fns)
	if err != nil || got != "slow" {
		t.Fatalf("FirstResult = (%q, %v), want (%q, nil): one failure must not sink the whole call", got, err, "slow")
	}
}

func TestAllFail(t *testing.T) {
	boom := errors.New("boom")
	fns := make([]func(context.Context) (string, error), 3)
	for i := range fns {
		fns[i] = func(ctx context.Context) (string, error) { return "", boom }
	}
	if _, err := runFirstResult(t, context.Background(), fns); err == nil {
		t.Fatal("want a non-nil error when every fn fails, got nil")
	}
}

func TestEmptyFns(t *testing.T) {
	if _, err := runFirstResult(t, context.Background(), nil); err == nil {
		t.Fatal("want a non-nil error for empty fns, got nil")
	}
}

func TestParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fns := []func(context.Context) (string, error){
		func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := runFirstResult(t, ctx, fns)
	if err == nil {
		t.Fatal("want a non-nil error after parent cancellation, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("returned %v after cancel; must return promptly", elapsed)
	}
}
```

# Lesson: sync Beyond Mutex {#sync-beyond-mutex}

`sync.Mutex` is the workhorse, but the `sync` package has sharper tools,
each encoding a specific access pattern.

**RWMutex** splits lockers into readers and writers: any number of
readers may hold `RLock` simultaneously, while `Lock` waits for
exclusivity. It only pays off when reads vastly outnumber writes *and*
the critical section is long enough to amortize the more expensive
bookkeeping — a read-mostly map of config values, not a counter
increment. Benchmark before reaching for it; under short critical
sections a plain Mutex is often faster.

**Cond** lets goroutines sleep until some condition over shared state
*might* have changed. The non-negotiable idiom is checking the condition
in a loop:

```go
mu.Lock()
for !ready {   // re-check: wakeups tell you to look, not that it's true
	cond.Wait() // atomically unlocks mu, sleeps, relocks on wake
}
mu.Unlock()
```

`Signal` wakes one waiter, `Broadcast` wakes all; between the wakeup and
reacquiring the lock, another goroutine may have consumed the condition,
which is exactly why the loop is mandatory.

**Once** is the most interesting one, because its guarantee is about the
*memory model*, not just counting. `once.Do(f)` promises that `f` has
fully completed before *any* call to `Do` returns — the completion of
`f` happens-before every return. That is what makes lazy initialization
safe to publish: whoever wins the race runs `f`, and everyone else
blocks until the result is visible. Note the trap this implies: a naive
`if !done { mu.Lock(); ... }` check outside the lock is a data race, and
the classic "double-checked locking" only works when the flag itself is
accessed atomically.

Go 1.21 added `sync.OnceValue`, which memoizes a function's result. You
will now build it yourself — because knowing why the obvious versions
are wrong is the actual lesson.

## Challenge: Build OnceValue {#once-value points=25}

Implement:

```go
func OnceValue[T any](f func() T) func() T
```

without using `sync.Once`, `sync.OnceValue`, or `sync.OnceValues` —
build the exactly-once machinery yourself with a mutex and/or atomics.

Requirements:

- `f` must not run until the returned function is first called (lazy).
- `f` runs **exactly once**, even when many goroutines call the
  returned function concurrently.
- Every caller — including callers that arrive while `f` is still
  running — gets the value `f` returned, never a zero value.
- Separate `OnceValue` wrappers are independent.

### Starter

```go
package challenge

// OnceValue returns a function that computes f's result on first call,
// caches it, and returns the cached value on every later call.
// f runs exactly once, no matter how many goroutines call concurrently.
// Do not use sync.Once / sync.OnceValue — build it yourself.
func OnceValue[T any](f func() T) func() T {
	// TODO: guard the single invocation of f.
	return func() T {
		var zero T
		return zero
	}
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

func callWithTimeout[T any](t *testing.T, f func() T) T {
	t.Helper()
	ch := make(chan T, 1)
	go func() { ch <- f() }()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("call did not return within 2s (deadlock?)")
		var zero T
		return zero
	}
}

func TestLazyAndCached(t *testing.T) {
	var calls atomic.Int64
	get := OnceValue(func() int {
		calls.Add(1)
		return 42
	})
	if n := calls.Load(); n != 0 {
		t.Fatalf("f ran %d times before the returned func was called; must be lazy", n)
	}
	if got := callWithTimeout(t, get); got != 42 {
		t.Fatalf("first call = %d, want 42", got)
	}
	if got := callWithTimeout(t, get); got != 42 {
		t.Fatalf("second call = %d, want 42", got)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("f ran %d times, want exactly 1", n)
	}
}

func TestConcurrentCallersOneInvocation(t *testing.T) {
	var calls atomic.Int64
	get := OnceValue(func() string {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond) // widen the race window
		return "ready"
	})

	const n = 64
	bad := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v := get(); v != "ready" {
				bad <- v
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent callers did not all return within 3s (deadlock?)")
	}
	close(bad)
	for v := range bad {
		t.Fatalf("a concurrent caller observed %q, want %q — callers must wait for f", v, "ready")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("f ran %d times under contention, want exactly 1", n)
	}
}

func TestIndependentInstances(t *testing.T) {
	a := OnceValue(func() int { return 1 })
	b := OnceValue(func() int { return 2 })
	if got := callWithTimeout(t, a); got != 1 {
		t.Fatalf("a() = %d, want 1", got)
	}
	if got := callWithTimeout(t, b); got != 2 {
		t.Fatalf("b() = %d, want 2", got)
	}
}
```

# Lesson: Atomics and the Memory Model {#atomics-memory-model}

The Go memory model answers one question: when is a read guaranteed to
observe a particular write? The answer is phrased as *happens-before* —
a partial order built from program order within a goroutine plus
synchronization edges between goroutines. Channel operations, mutex
lock/unlock, `WaitGroup.Wait`, `Once.Do`, and `sync/atomic` operations
all create those edges. A write in one goroutine and a read in another
with *no* edge between them is a data race, and a racy Go program has no
defined behavior at all: the compiler and CPU are free to reorder, cache
in registers, and tear multi-word values. "It's just a flag, worst case
I read a stale bool" is not a claim the language honors.

`sync/atomic` gives you synchronization at the granularity of a single
word. Since Go 1.19 the memory model guarantees atomics behave
sequentially consistent — an atomic write release-publishes everything
that happened before it, and an atomic read that observes it acquires
all of that history. That is why an `atomic.Bool` "initialized" flag
works: the flag write is the edge over which the initialized data
travels.

The composable primitive is compare-and-swap:

```go
for {
	old := counter.Load()
	if old >= limit {
		return false // full — no state change needed
	}
	if counter.CompareAndSwap(old, old+1) {
		return true // we won the race for this transition
	}
	// somebody else moved the state; re-read and retry
}
```

A CAS loop reads the current state, computes the successor state, and
commits only if nothing changed in between — lock-free optimistic
concurrency in five lines. Losing a CAS is not failure, it just means
another goroutine made progress; you retry against the fresh value.

Know the boundary: atomics suffice when the entire invariant fits in
one word (a counter with a cap, a state enum, a flag). The moment an
invariant spans two fields, two atomic operations are two separate
edges with a hole between them — that is mutex territory. And beware
ABA: CAS only proves the value is the same, not that nothing happened.

## Challenge: Lock-Free Slot Limiter {#slot-limiter points=30}

Implement a lock-free bounded slot allocator — the core of a
non-blocking semaphore — using only `sync/atomic` (no mutexes, no
channels):

```go
type Slots struct { /* ... */ }

func NewSlots(n int) *Slots      // capacity n
func (s *Slots) TryAcquire() bool // claim a slot; false if all in use
func (s *Slots) Release()         // return a slot; panic if none held
```

Invariants the tests enforce behaviorally, under heavy contention:

- Never more than `n` slots held at once.
- `TryAcquire` never blocks: it returns `false` when full.
- `Release` makes the slot available again; calling `Release` when no
  slot is held must panic (a limiter that silently grows its capacity
  is corrupted).

Use a `CompareAndSwap` loop for both transitions.

### Starter

```go
package challenge

// Slots is a lock-free bounded slot allocator: at most capacity slots
// can be held at any moment.
type Slots struct {
	capacity int64
	inUse    int64 // touch only via sync/atomic
}

// NewSlots returns an allocator with n free slots.
func NewSlots(n int) *Slots {
	return &Slots{capacity: int64(n)}
}

// TryAcquire claims a free slot without blocking. It reports whether a
// slot was acquired.
func (s *Slots) TryAcquire() bool {
	// TODO: CAS loop — read, check capacity, attempt the transition.
	return true
}

// Release returns a previously acquired slot. It panics if called when
// no slot is held.
func (s *Slots) Release() {
	// TODO: CAS loop — and guard against over-release.
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

func TestSequentialAcquireRelease(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s := NewSlots(2)
		if !s.TryAcquire() {
			t.Error("first TryAcquire = false, want true")
			return
		}
		if !s.TryAcquire() {
			t.Error("second TryAcquire = false, want true")
			return
		}
		if s.TryAcquire() {
			t.Error("third TryAcquire = true, want false (capacity 2)")
			return
		}
		s.Release()
		if !s.TryAcquire() {
			t.Error("TryAcquire after Release = false, want true")
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sequential acquire/release did not finish within 2s; TryAcquire must not block")
	}
}

func TestReleaseWithoutAcquirePanics(t *testing.T) {
	panicked := make(chan bool, 1)
	go func() {
		defer func() { panicked <- recover() != nil }()
		NewSlots(1).Release()
	}()
	select {
	case p := <-panicked:
		if !p {
			t.Fatal("Release with no held slot must panic")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Release with no held slot must panic, not block")
	}
}

func TestContendedInvariant(t *testing.T) {
	const (
		capacity   = 4
		goroutines = 16
		iters      = 2000
	)
	s := NewSlots(capacity)
	var (
		cur       atomic.Int64
		violation atomic.Bool
		acquired  atomic.Int64
		wg        sync.WaitGroup
	)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				if s.TryAcquire() {
					if cur.Add(1) > capacity {
						violation.Store(true)
					}
					acquired.Add(1)
					cur.Add(-1)
					s.Release()
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stress test did not finish within 5s (livelock or blocked TryAcquire?)")
	}
	if violation.Load() {
		t.Fatalf("more than %d slots were held at the same time", capacity)
	}
	if acquired.Load() == 0 {
		t.Fatal("no goroutine ever acquired a slot")
	}
	for i := 0; i < capacity; i++ {
		if !s.TryAcquire() {
			t.Fatalf("after the stress all slots should be free: acquire %d of %d failed", i+1, capacity)
		}
	}
	if s.TryAcquire() {
		t.Fatal("acquire beyond capacity succeeded after stress; slot count corrupted")
	}
}
```

# Lesson: Bounded Parallelism {#bounded-parallelism}

`go f()` is so cheap that the instinct is to launch one goroutine per
item. For CPU work that just adds scheduler pressure past `GOMAXPROCS`;
for I/O work it is worse — ten thousand goroutines means ten thousand
simultaneous connections hammering a database that wanted fifty. The
goroutines are cheap; what they *do* is not. Concurrency limits protect
the thing downstream.

The lightest-weight limiter is a buffered channel used as a counting
semaphore:

```go
sem := make(chan struct{}, limit)
for _, item := range items {
	sem <- struct{}{} // blocks while `limit` are in flight
	go func(item Item) {
		defer func() { <-sem }()
		process(item)
	}(item)
}
```

Acquire by sending, release by receiving; the buffer size *is* the
limit. The alternative shape — a fixed pool of `limit` worker goroutines
ranging over a shared channel — does the same job and is preferable
when workers carry per-worker state (a connection, a buffer).

Two details separate a toy from a correct implementation. **Order:**
results must often line up with inputs even though completion order is
arbitrary. Do not collect from a shared channel and re-sort — have
goroutine `i` write to `out[i]`. Distinct indices of a slice are
distinct memory; there is no race, and the `WaitGroup.Wait` edge makes
every write visible to the reader afterwards. **Failure:** when one
item fails, the remaining work is usually garbage. Combine the
semaphore with a derived context: record the first error, cancel, and
stop dispatching. That is the shape of the next challenge — and, not
coincidentally, of `errgroup`, which you will build right after.

## Challenge: Order-Preserving ParallelMap {#parallel-map points=35}

Implement:

```go
func ParallelMap(ctx context.Context, in []int, limit int, f func(context.Context, int) (int, error)) ([]int, error)
```

Semantics (assume `limit >= 1`):

- Apply `f` to every element, running at most `limit` calls to `f`
  concurrently — and actually use the budget: calls must overlap.
- `out[i]` corresponds to `in[i]` — results keep input order.
- Pass each `f` a context derived from `ctx`. When any call returns an
  error, cancel that context promptly, stop dispatching new work, and
  return the first error (the returned slice is then unspecified).
- Empty input returns an empty slice and nil error.
- Return only after every in-flight call has finished.

### Starter

```go
package challenge

import "context"

// ParallelMap applies f to every element of in with at most limit
// concurrent calls, preserving input order in the result. The first
// error cancels the remaining work and is returned.
func ParallelMap(ctx context.Context, in []int, limit int, f func(context.Context, int) (int, error)) ([]int, error) {
	// TODO: this sequential version is correct but not concurrent.
	// Add bounded parallelism, order preservation, and cancellation.
	out := make([]int, len(in))
	for i, v := range in {
		r, err := f(ctx, v)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}
```

### Tests

```go
package challenge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func runParallelMap(t *testing.T, ctx context.Context, in []int, limit int, f func(context.Context, int) (int, error)) ([]int, error) {
	t.Helper()
	type result struct {
		out []int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := ParallelMap(ctx, in, limit, f)
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("ParallelMap did not return within 5s (deadlock?)")
		return nil, nil
	}
}

func TestEmptyInput(t *testing.T) {
	out, err := runParallelMap(t, context.Background(), nil, 4,
		func(ctx context.Context, v int) (int, error) { return v, nil })
	if err != nil || len(out) != 0 {
		t.Fatalf("ParallelMap(nil) = (%v, %v), want empty slice and nil error", out, err)
	}
}

func TestOrderPreserved(t *testing.T) {
	in := make([]int, 30)
	for i := range in {
		in[i] = i
	}
	f := func(ctx context.Context, v int) (int, error) {
		// Later items finish sooner, to expose out-of-order writes.
		time.Sleep(time.Duration(30-v) * time.Millisecond)
		return v * 2, nil
	}
	out, err := runParallelMap(t, context.Background(), in, 8, f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(in))
	}
	for i, v := range in {
		if out[i] != v*2 {
			t.Fatalf("out[%d] = %d, want %d — results must keep input order", i, out[i], v*2)
		}
	}
}

func TestLimitRespectedAndUsed(t *testing.T) {
	const limit = 3
	var cur, peak atomic.Int64
	f := func(ctx context.Context, v int) (int, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		cur.Add(-1)
		return v, nil
	}
	in := make([]int, 24)
	if _, err := runParallelMap(t, context.Background(), in, limit, f); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p := peak.Load(); p > limit {
		t.Fatalf("peak concurrency %d exceeds limit %d", p, limit)
	}
	if p := peak.Load(); p < 2 {
		t.Fatalf("peak concurrency %d — calls never overlapped; the limit budget must actually be used", p)
	}
}

func TestFirstErrorCancels(t *testing.T) {
	boom := errors.New("boom")
	var live atomic.Int64 // calls that began with a still-live context
	f := func(ctx context.Context, v int) (int, error) {
		if ctx.Err() == nil {
			live.Add(1)
		}
		if v == 0 {
			return 0, boom
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return v, nil
		}
	}
	in := make([]int, 100)
	for i := range in {
		in[i] = i
	}
	start := time.Now()
	_, err := runParallelMap(t, context.Background(), in, 4, f)
	if err == nil {
		t.Fatal("want the first error back, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the error f returned (%v)", err, boom)
	}
	if n := live.Load(); n > 20 {
		t.Fatalf("%d of 100 calls still saw a live context after the failure; cancel promptly", n)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %v after an immediate failure; cancellation must cut the remaining work short", elapsed)
	}
}
```

# Lesson: errgroup From Scratch {#errgroup-from-scratch}

Almost every fan-out in production code has the same requirements:
launch N tasks, wait for all of them, surface the *first* error, and
usually bound how many run at once. `golang.org/x/sync/errgroup`
packages exactly that, and its API is tiny:

```go
var g errgroup.Group
g.SetLimit(4)
for _, u := range urls {
	g.Go(func() error { return fetch(u) })
}
if err := g.Wait(); err != nil { /* first failure */ }
```

The value is in the precise semantics, so state them before building:

- `Go(f)` runs `f` in its own goroutine. With a limit set, `Go` blocks
  until a slot frees up — backpressure at the *submission* point, so a
  producer loop can never race ahead of the pool.
- `Wait` blocks until every launched `f` has returned, then yields the
  first non-nil error. Not the last, not a combined bag: the first,
  because later failures are usually just fallout from the first one.
- The zero value is ready to use — the Go idiom of making the empty
  struct meaningful, like `bytes.Buffer` and `sync.Mutex`.

Every piece is something you have already built in this course: a
`WaitGroup` for "all done", a first-writer-wins slot for the error
(exactly the Once semantics from earlier — `sync.Once` is fair game
here), and a buffered-channel semaphore for the limit. The subtle part
is memory visibility: `Wait` reads the stored error *after*
`wg.Wait()`, and the failing goroutine wrote it *before* `wg.Done()`,
so the WaitGroup edge — not luck — is what makes the read safe.

The real errgroup also derives a context that is canceled on first
failure; you built that muscle in `ParallelMap`, so here we focus on
the group itself.

## Challenge: Build the Group {#group-from-scratch points=30}

Implement (the real `errgroup` is not available in the grader —
stdlib only):

```go
type Group struct { /* ... */ }

func (g *Group) SetLimit(n int)      // max n tasks in flight; call before any Go
func (g *Group) Go(f func() error)   // run f concurrently; block if at the limit
func (g *Group) Wait() error         // wait for all; return the first error
```

Requirements:

- The zero value of `Group` is usable without a limit.
- Tasks started with `Go` run concurrently, not synchronously.
- `Wait` returns only after every task has finished, and returns the
  first error that occurred (nil if none).
- After `SetLimit(n)`, at most `n` tasks are in flight at once; `Go`
  blocks while the group is full.

### Starter

```go
package challenge

// Group runs tasks concurrently, waits for all of them, and reports
// the first error. The zero value is ready to use.
type Group struct {
	// TODO: fields (WaitGroup, first-error slot, optional semaphore).
}

// SetLimit caps in-flight tasks at n. Must be called before any Go.
func (g *Group) SetLimit(n int) {
	// TODO
}

// Go runs f in a new goroutine, blocking first if the group is at its
// limit.
func (g *Group) Go(f func() error) {
	// TODO: this runs f synchronously and drops its error.
	f()
}

// Wait blocks until every task launched with Go has returned, then
// returns the first non-nil error, if any.
func (g *Group) Wait() error {
	// TODO
	return nil
}
```

### Tests

```go
package challenge

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// runGroup executes body (which should launch tasks and return Wait's
// result) under a watchdog so a deadlocked solution fails fast.
func runGroup(t *testing.T, body func() error) error {
	t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- body() }()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("Go/Wait did not finish within 3s (deadlock?)")
		return nil
	}
}

func TestZeroValueNoTasks(t *testing.T) {
	err := runGroup(t, func() error {
		var g Group
		return g.Wait()
	})
	if err != nil {
		t.Fatalf("Wait on an empty zero-value Group = %v, want nil", err)
	}
}

func TestAllSucceed(t *testing.T) {
	var ran atomic.Int64
	err := runGroup(t, func() error {
		var g Group
		for i := 0; i < 10; i++ {
			g.Go(func() error {
				ran.Add(1)
				return nil
			})
		}
		return g.Wait()
	})
	if err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if n := ran.Load(); n != 10 {
		t.Fatalf("ran %d tasks, want 10", n)
	}
}

func TestTasksRunConcurrently(t *testing.T) {
	var cur, peak atomic.Int64
	err := runGroup(t, func() error {
		var g Group
		for i := 0; i < 4; i++ {
			g.Go(func() error {
				n := cur.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(30 * time.Millisecond)
				cur.Add(-1)
				return nil
			})
		}
		return g.Wait()
	})
	if err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if p := peak.Load(); p < 2 {
		t.Fatalf("peak concurrency %d — Go must not run tasks synchronously", p)
	}
}

func TestFirstErrorWins(t *testing.T) {
	errFirst := errors.New("first")
	errLate := errors.New("late")
	err := runGroup(t, func() error {
		var g Group
		g.Go(func() error {
			time.Sleep(150 * time.Millisecond)
			return errLate
		})
		g.Go(func() error {
			time.Sleep(10 * time.Millisecond)
			return errFirst
		})
		g.Go(func() error { return nil })
		return g.Wait()
	})
	if !errors.Is(err, errFirst) {
		t.Fatalf("Wait = %v, want the first error to occur (%v)", err, errFirst)
	}
}

func TestWaitBlocksUntilAllDone(t *testing.T) {
	var finished atomic.Int64
	err := runGroup(t, func() error {
		var g Group
		for i := 0; i < 5; i++ {
			g.Go(func() error {
				time.Sleep(50 * time.Millisecond)
				finished.Add(1)
				return nil
			})
		}
		return g.Wait()
	})
	if err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if n := finished.Load(); n != 5 {
		t.Fatalf("Wait returned with only %d of 5 tasks finished", n)
	}
}

func TestSetLimit(t *testing.T) {
	var cur, peak atomic.Int64
	var elapsed time.Duration
	err := runGroup(t, func() error {
		var g Group
		g.SetLimit(2)
		start := time.Now()
		for i := 0; i < 6; i++ {
			g.Go(func() error {
				n := cur.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(40 * time.Millisecond)
				cur.Add(-1)
				return nil
			})
		}
		err := g.Wait()
		elapsed = time.Since(start)
		return err
	})
	if err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if p := peak.Load(); p > 2 {
		t.Fatalf("peak concurrency %d exceeds SetLimit(2)", p)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("6 tasks of 40ms at limit 2 finished in %v; the limit was not enforced", elapsed)
	}
}
```

# Final Challenge: Concurrent Crawler {#concurrent-crawler points=75}

Everything at once: bounded concurrency, deduplication under
contention, and — the hard part — clean termination.

Implement:

```go
func Crawl(start string, fetch func(url string) []string, workers int) []string
```

- `fetch(url)` returns the URLs linked from `url`. The graph may
  contain cycles, self-links, and links to URLs that link nowhere.
- Visit every URL reachable from `start`, calling `fetch` **exactly
  once per URL** — deduplication must hold even when two workers
  discover the same URL at the same moment.
- At most `workers` calls to `fetch` may be in flight at once (assume
  `workers >= 1`), and on a wide graph the budget must actually be used
  — fetches must overlap.
- Return the set of visited URLs (any order, no duplicates), only
  after all fetching has finished.

Termination is where naive designs deadlock: the crawl is finished when
no worker is fetching *and* no discovered URL is still waiting — but
each fetch can add new work. Track outstanding work explicitly (a
counter, a WaitGroup, or a coordinator goroutine) so your workers know
when to stop waiting for more.

### Starter

```go
package challenge

// Crawl visits every URL reachable from start, calling fetch exactly
// once per URL with at most `workers` fetches in flight, and returns
// the visited URLs in any order.
func Crawl(start string, fetch func(url string) []string, workers int) []string {
	// TODO: bounded workers, safe dedupe, and a termination scheme.
	fetch(start)
	return []string{start}
}
```

### Tests

```go
package challenge

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type graph map[string][]string

type crawlStats struct {
	mu     sync.Mutex
	visits map[string]int
	cur    atomic.Int64
	peak   atomic.Int64
}

func fetcher(g graph, st *crawlStats, delay time.Duration) func(string) []string {
	return func(url string) []string {
		n := st.cur.Add(1)
		for {
			p := st.peak.Load()
			if n <= p || st.peak.CompareAndSwap(p, n) {
				break
			}
		}
		st.mu.Lock()
		st.visits[url]++
		st.mu.Unlock()
		time.Sleep(delay)
		st.cur.Add(-1)
		return g[url]
	}
}

func runCrawl(t *testing.T, start string, fetch func(string) []string, workers int) []string {
	t.Helper()
	ch := make(chan []string, 1)
	go func() { ch <- Crawl(start, fetch, workers) }()
	select {
	case got := <-ch:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("Crawl did not return within 5s (deadlock or lost termination?)")
		return nil
	}
}

// reachable returns the sorted set of URLs reachable from start.
func reachable(g graph, start string) []string {
	seen := map[string]bool{start: true}
	queue := []string{start}
	var out []string
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		out = append(out, u)
		for _, v := range g[u] {
			if !seen[v] {
				seen[v] = true
				queue = append(queue, v)
			}
		}
	}
	sort.Strings(out)
	return out
}

func checkCrawl(t *testing.T, g graph, start string, workers int, delay time.Duration) *crawlStats {
	t.Helper()
	st := &crawlStats{visits: map[string]int{}}
	got := runCrawl(t, start, fetcher(g, st, delay), workers)
	want := reachable(g, start)

	sort.Strings(got)
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Errorf("Crawl returned %q more than once", got[i])
		}
	}
	if len(got) != len(want) {
		t.Fatalf("Crawl visited %d URLs %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("visited set mismatch: got %v, want %v", got, want)
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, u := range want {
		if st.visits[u] != 1 {
			t.Errorf("fetch(%q) was called %d times, want exactly 1", u, st.visits[u])
		}
	}
	if p := st.peak.Load(); p > int64(workers) {
		t.Errorf("peak concurrent fetches %d exceeds workers=%d", p, workers)
	}
	return st
}

func TestSingleURL(t *testing.T) {
	checkCrawl(t, graph{"a": nil}, "a", 2, time.Millisecond)
}

func TestCyclesAndSelfLinks(t *testing.T) {
	g := graph{
		"a": {"b", "a"},
		"b": {"c"},
		"c": {"a", "d"},
		"d": {},
	}
	checkCrawl(t, g, "a", 3, 5*time.Millisecond)
}

func TestLinksToUnknownPages(t *testing.T) {
	g := graph{
		"a": {"b", "ghost"},
		"b": {"ghost"},
	}
	checkCrawl(t, g, "a", 2, 2*time.Millisecond)
}

func TestWideGraphUsesWorkers(t *testing.T) {
	g := graph{}
	var root []string
	for i := 0; i < 8; i++ {
		c := fmt.Sprintf("c%d", i)
		root = append(root, c)
		links := []string{fmt.Sprintf("c%d", (i+1)%8), "root"}
		for j := 0; j < 4; j++ {
			k := fmt.Sprintf("c%d-g%d", i, j)
			links = append(links, k)
			g[k] = []string{"root", c} // back-edges create cycles
		}
		g[c] = links
	}
	g["root"] = root

	st := checkCrawl(t, g, "root", 4, 15*time.Millisecond)
	if p := st.peak.Load(); p < 2 {
		t.Fatalf("peak concurrent fetches %d on a wide graph with workers=4; fetches must overlap", p)
	}
}

func TestSingleWorkerIsSequential(t *testing.T) {
	g := graph{
		"a": {"b"},
		"b": {"c"},
		"c": {"d"},
		"d": nil,
	}
	st := checkCrawl(t, g, "a", 1, 5*time.Millisecond)
	if p := st.peak.Load(); p != 1 {
		t.Fatalf("peak concurrent fetches %d with workers=1, want exactly 1", p)
	}
}
```
