---
course: goroutines-from-scratch
title: Goroutines from Scratch
language: go
description: >-
  Rebuild the Go scheduler's core machinery in userspace — parking, channel
  mutexes, futures, a cooperative run queue, and an M:N worker pool — and
  finish with a deterministic mini runtime on a virtual clock.
duration_hours: 12
tags: [concurrency, runtime, internals, advanced]
extended_reading:
  - title: Kavya Joshi — The Scheduler Saga (GopherCon 2018)
    url: https://www.youtube.com/watch?v=YHRO5WQGh0k
  - title: Dmitry Vyukov — Scalable Go Scheduler Design Doc
    url: https://golang.org/s/go11sched
  - title: Go runtime HACKING.md
    url: https://github.com/golang/go/blob/master/src/runtime/HACKING.md
  - title: runtime/proc.go — the scheduler itself
    url: https://github.com/golang/go/blob/master/src/runtime/proc.go
---

# Lesson: The Anatomy of a Goroutine {#anatomy-of-a-goroutine}

You have written `go f()` a thousand times. This course is about what happens
next — and the best way to understand it is to build the machinery yourself,
in ordinary Go, one piece at a time.

Start with the object itself. A goroutine is not a thread. It is a small heap
structure the runtime calls a `g`, defined in `runtime/runtime2.go`:

```go
// Heavily abridged from runtime/runtime2.go.
type g struct {
	stack       stack   // [stack.lo, stack.hi) — the goroutine's stack bounds
	stackguard0 uintptr // stack-growth (and preemption!) check value
	sched       gobuf   // saved SP, PC, etc. — the "context" in context switch
	atomicstatus atomic.Uint32 // _Grunnable, _Grunning, _Gwaiting, ...
	goid        uint64
	waitreason  waitReason // why it's parked ("chan receive", "sleep", ...)
}
```

Three things make a `g` cheap where an OS thread is expensive:

- **Tiny, growable stacks.** A goroutine starts with a ~2 KB stack (since Go
  1.19 the runtime may pick a larger start size based on the historical
  average). Every function prologue compares the stack pointer against
  `stackguard0`; on overflow, `morestack` allocates a bigger stack and copies
  the old one over — contiguous stacks. An OS thread reserves megabytes of
  virtual address space up front and can never shrink it.
- **Userspace context switches.** Switching goroutines means saving a handful
  of registers into `g.sched` (a `gobuf`: SP, PC, and a pointer to the `g`)
  and loading another goroutine's. No syscall, no kernel scheduler, roughly
  the cost of a function call. The assembly routines are `gogo` and `mcall`.
- **M:N scheduling.** Millions of Gs are multiplexed onto a few OS threads.

The runtime's scheduler is described by three letters you'll see constantly
in `runtime/proc.go`:

- **G** — a goroutine: stack + saved registers + status.
- **M** — a machine: an actual OS thread that executes Gs.
- **P** — a processor: a scheduling context holding a local run queue of
  runnable Gs (plus allocation caches). There are exactly `GOMAXPROCS` Ps.
  An M must hold a P to run Go code; an M without a P is parked or sitting
  in a syscall.

`go f()` compiles to a call to `runtime.newproc`, which allocates (or
recycles) a `g`, seeds its `gobuf` so it will "return into" `f`, marks it
`_Grunnable`, and drops it on the current P's run queue. That's all — the
statement returns immediately, and `f` runs whenever a P picks it up. This is
also why `main` returning kills the program without waiting for anyone:
nothing counts outstanding goroutines. The runtime deliberately has no
"join"; if you want one, you build it from synchronization primitives —
which is exactly your first challenge.

## Challenge: A WaitGroup from Scratch {#spawn-waitall points=20}

Build the "join" primitive the runtime doesn't give you. Implement two
package-level functions:

```go
func Spawn(f func())  // start f concurrently; return immediately
func WaitAll()        // block until every spawned function has returned
```

Semantics your implementation must satisfy:

- `WaitAll` returns immediately when nothing is in flight (including before
  the first `Spawn` ever happens).
- `WaitAll` blocks until **every** spawned function has returned — including
  functions spawned *by* spawned functions. As long as a running spawned
  function calls `Spawn` before it returns, the count never touches zero, so
  transitive work is covered automatically.
- The package is reusable: after `WaitAll` returns you can `Spawn` again and
  `WaitAll` again.

**`sync.WaitGroup` is banned** — using it would be building a WaitGroup out
of a WaitGroup. (The grader can't truly detect it; this is between you and
the duck.) The intended shape is an atomic counter plus a channel used as a
one-shot park/unpark signal: `Spawn` increments before starting the
goroutine, a deferred decrement detects the drop to zero and wakes waiters.
A `sync.Mutex` guarding the counter alongside a "currently idle" channel is
also a fine design. Think hard about the classic bug: incrementing *inside*
the new goroutine instead of before it starts — a `WaitAll` racing that
increment sees zero and returns early.

### Starter

```go
package challenge

// Spawn starts f concurrently and tracks it until it returns.
func Spawn(f func()) {
	// TODO: track this work so WaitAll can wait for it.
	go f()
}

// WaitAll blocks until every function passed to Spawn has returned.
// It returns immediately if nothing is in flight.
func WaitAll() {
	// TODO: park until the in-flight count drops to zero.
}
```

### Tests

```go
package challenge

import (
	"sync/atomic"
	"testing"
	"time"
)

func waitAllDone() chan struct{} {
	done := make(chan struct{})
	go func() {
		WaitAll()
		close(done)
	}()
	return done
}

func TestWaitAllEmpty(t *testing.T) {
	select {
	case <-waitAllDone():
	case <-time.After(2 * time.Second):
		t.Fatal("WaitAll blocked even though nothing was spawned")
	}
}

func testWaitAllRound(t *testing.T, hold time.Duration) {
	t.Helper()
	const n = 8
	var finished atomic.Int64
	release := make(chan struct{})
	for i := 0; i < n; i++ {
		Spawn(func() {
			<-release
			finished.Add(1)
		})
	}

	done := waitAllDone()
	select {
	case <-done:
		close(release)
		t.Fatal("WaitAll returned while spawned functions were still running")
	case <-time.After(hold):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitAll did not return promptly after all spawned functions finished")
	}
	if got := finished.Load(); got != n {
		t.Fatalf("finished = %d, want %d", got, n)
	}
}

func TestWaitAllWaitsForAll(t *testing.T) {
	// Two rounds with different hold times: WaitAll must stay blocked
	// for the whole hold, then return within 1s of the release. No
	// fixed delay can satisfy both rounds — only real tracking can.
	testWaitAllRound(t, 250*time.Millisecond)
	testWaitAllRound(t, 1500*time.Millisecond)
}

func TestSpawnFromSpawned(t *testing.T) {
	var finished atomic.Int64
	Spawn(func() {
		time.Sleep(50 * time.Millisecond)
		Spawn(func() {
			time.Sleep(50 * time.Millisecond)
			Spawn(func() {
				time.Sleep(50 * time.Millisecond)
				finished.Add(1)
			})
			finished.Add(1)
		})
		finished.Add(1)
	})

	select {
	case <-waitAllDone():
	case <-time.After(5 * time.Second):
		t.Fatal("WaitAll did not return")
	}
	if got := finished.Load(); got != 3 {
		t.Fatalf("finished = %d, want 3 — WaitAll returned before nested Spawns completed", got)
	}
}
```

# Lesson: Parking and Wakeups {#parking-and-wakeups}

What actually happens when a goroutine blocks — on a channel receive, a mutex,
a timer? The one thing that must *not* happen is an OS thread going to sleep
or, worse, spinning. Threads are the scarce resource; blocking one on every
blocked goroutine would collapse M:N scheduling back to 1:1.

The runtime's answer is a pair of functions you'll see all over `proc.go`:

- **`gopark`** — called *by the blocking goroutine, on its own stack*. It
  flips the G's status from `_Grunning` to `_Gwaiting`, records a
  `waitreason` (that's the "chan receive" you see in goroutine dumps), then
  switches to the scheduler via `mcall`. Crucially, the G is now on **no run
  queue at all**. It is not scheduled, not polled, costs zero CPU. It lives
  only in whatever wait-list the blocking primitive keeps — a channel's
  waiter queue, a mutex's semaphore list — as a `sudog` (a small "G waiting
  on this thing" node; one G can sit in several wait-lists at once thanks to
  `select`).
- **`goready`** — called by whoever unblocks it (the sender, the unlocker).
  It flips the G back to `_Grunnable` and pushes it onto a run queue. The M
  never stopped: after `gopark` it immediately picked up another runnable G.

`sync.Mutex` is a consumer of this machinery: its fast path is a single
atomic CAS in userspace, and only the contended slow path calls into the
runtime semaphore (`semacquire`/`semrelease`), which is a futex-style
"sleep until someone releases" built on gopark/goready.

You can't call `gopark` from user code — but you don't need to, because
**a channel operation is a gopark/goready pair in a trench coat**. A blocked
`ch <- x` parks you on the channel's sender wait-list; a receive readies you,
FIFO. So a buffered channel of capacity 1 is a complete lock implementation:

- `Lock` = put the token in (`ch <- struct{}{}`) — blocks (parks!) while the
  buffer is full, i.e. while someone else holds the lock.
- `Unlock` = take the token out (`<-ch`) — wakes exactly one parked waiter.
- `TryLock` = a `select` with a `default` arm: the non-blocking attempt.

Fairness note: waiters on a channel are queued FIFO by the runtime, which is
a *nicer* property than `sync.Mutex` guarantees in its normal (barging) mode.
You get that for free.

## Challenge: A Mutex from a Channel {#chan-mutex points=25}

Implement a mutex whose entire blocking behavior is delegated to the
runtime's channel park/unpark machinery:

```go
type Mutex struct { /* your state — a chan struct{} of capacity 1 */ }

func NewMutex() *Mutex
func (m *Mutex) Lock()          // parks until the lock is available
func (m *Mutex) TryLock() bool  // never blocks; true iff it acquired the lock
func (m *Mutex) Unlock()        // releases; wakes one parked Lock, FIFO
```

Required semantics:

- Mutual exclusion: between `Lock` (or a successful `TryLock`) and the
  matching `Unlock`, no other `Lock`/`TryLock` may succeed.
- `TryLock` returns immediately: `true` and holds the lock, or `false`.
- **`Unlock` of an unlocked Mutex must panic** (with any message). Detect it
  with the non-blocking receive pattern — if there's no token to remove, the
  mutex wasn't locked. Like the real `sync.Mutex`, there is no ownership:
  any goroutine may unlock a locked mutex.
- Do not use `sync.Mutex`/`sync.RWMutex` or spin loops; the point is that
  one buffered channel already is the lock. (Honor system again.)

The tests hammer it from 8 goroutines with a critical-section collision
detector: every entry does an atomic increment of an "occupancy" gauge and
fails if it ever reads anything but 1.

### Starter

```go
package challenge

// Mutex is a mutual-exclusion lock built on a buffered channel.
type Mutex struct {
	// TODO: what state does a mutex need?
}

// NewMutex returns an unlocked Mutex.
func NewMutex() *Mutex {
	return &Mutex{}
}

// Lock acquires the mutex, parking until it is available.
func (m *Mutex) Lock() {
	// TODO
}

// TryLock attempts to acquire the mutex without blocking.
func (m *Mutex) TryLock() bool {
	// TODO
	return false
}

// Unlock releases the mutex, waking one parked Lock.
// Unlocking an unlocked Mutex panics.
func (m *Mutex) Unlock() {
	// TODO
}
```

### Tests

```go
package challenge

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestLockUnlockTryLock(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		m := NewMutex()
		if !m.TryLock() {
			t.Error("TryLock on a fresh Mutex = false, want true")
			return
		}
		if m.TryLock() {
			t.Error("TryLock on a held Mutex = true, want false")
			return
		}
		m.Unlock()
		if !m.TryLock() {
			t.Error("TryLock after Unlock = false, want true")
			return
		}
		m.Unlock()
		m.Lock()
		if m.TryLock() {
			t.Error("TryLock while Lock is held = true, want false")
			return
		}
		m.Unlock()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out — Lock/TryLock/Unlock deadlocked")
	}
}

func TestLockBlocksWhileHeld(t *testing.T) {
	m := NewMutex()
	m.Lock()
	acquired := make(chan struct{})
	go func() {
		m.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatal("second Lock succeeded while the mutex was held")
	case <-time.After(250 * time.Millisecond):
	}
	m.Unlock()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("parked Lock was not woken by Unlock")
	}
	m.Unlock()
}

func TestMutualExclusion(t *testing.T) {
	const (
		goroutines = 8
		iters      = 400
	)
	m := NewMutex()
	var occupancy, collisions, total atomic.Int64
	done := make(chan struct{})
	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < iters; i++ {
				m.Lock()
				if occupancy.Add(1) != 1 {
					collisions.Add(1)
				}
				runtime.Gosched() // widen the window so broken locks collide
				total.Add(1)
				occupancy.Add(-1)
				m.Unlock()
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < goroutines; g++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for workers — Lock/Unlock likely deadlocked")
		}
	}
	if c := collisions.Load(); c > 0 {
		t.Fatalf("%d critical-section collisions — Lock does not provide mutual exclusion", c)
	}
	if got, want := total.Load(), int64(goroutines*iters); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
}

func TestUnlockOfUnlockedPanics(t *testing.T) {
	m := NewMutex()
	defer func() {
		if recover() == nil {
			t.Fatal("Unlock of an unlocked Mutex did not panic")
		}
	}()
	m.Unlock()
}
```

# Lesson: Communication as Scheduling {#communication-as-scheduling}

It's tempting to picture a channel as a little thread-safe queue. Look at
`runtime/chan.go` and a different picture emerges: a channel is a
**scheduling data structure**. The `hchan` struct holds a ring buffer, yes —
but also two wait-lists of parked goroutines, `recvq` and `sendq`.

The interesting path is the rendezvous. When a goroutine sends on a channel
where a receiver is already parked, the runtime doesn't touch the buffer at
all: `send` copies the value **directly into the parked receiver's stack
frame** (a rare case of one goroutine writing another's stack), then calls
`goready` on it. Communication and scheduling are literally the same
operation — moving a value moves a goroutine from `_Gwaiting` to
`_Grunnable`. The woken receiver is even placed in the P's `runnext` slot,
so it typically runs next, keeping producer–consumer pairs hot in cache.

One channel operation is special: `close`. A send wakes *one* receiver;
`close` walks `recvq` and readies **every** parked goroutine (and makes all
future receives complete immediately with the zero value). That makes
`close` the runtime's broadcast primitive — the idiomatic one-shot event.
`context.Done()` is exactly this: a channel nobody ever sends on, closed
once to wake all listeners, forever.

Combine broadcast-on-close with "write the value before closing, read it
after receiving" (close is a release/acquire pair under the memory model —
the write *happens before* any receive that observes the close), and you get
a **future**: a cell that starts empty, is resolved exactly once, and wakes
every waiter — past and future — with the same value.

```go
// The one-shot broadcast pattern:
done := make(chan struct{})
var val string

// resolver (exactly once):
val = "ready"  // write...
close(done)    // ...then publish

// any number of waiters, before or after resolution:
<-done
use(val) // guaranteed to see "ready"
```

Waiting must also be *cancellable* — a `Get` that can outlive the caller's
interest is a goroutine leak factory. `select` is how a goroutine parks on
several events at once (one `sudog` per case, first ready wins), which makes
context integration one arm each.

## Challenge: Futures {#futures points=25}

Implement a generic, resolve-once future:

```go
type Future[T any] struct { /* ... */ }

// NewFuture returns an unresolved future and its resolve function.
func NewFuture[T any]() (*Future[T], func(T))

// Get blocks until the future is resolved or ctx is done.
func (f *Future[T]) Get(ctx context.Context) (T, error)
```

Required semantics:

- `Get` before resolution blocks (parks — no polling loops).
- Any number of `Get` callers, before or after resolution, all receive the
  same resolved value with a nil error.
- If `ctx` is cancelled while waiting, `Get` returns the zero value of `T`
  and `ctx.Err()`.
- Calling the resolve function a **second time panics** (with any message).
  Guard it with an atomic compare-and-swap, not a mutex — you built locks
  already; this one only needs a once-flag.

Build it exactly like the lesson's pattern: value field + `chan struct{}`
closed on resolve; `Get` is a two-arm `select`.

### Starter

```go
package challenge

import "context"

// Future is a write-once cell that any number of readers can wait on.
type Future[T any] struct {
	// TODO: a value, a done channel, a resolve-once guard.
}

// NewFuture returns an unresolved Future and the function that resolves it.
// Resolving twice panics.
func NewFuture[T any]() (*Future[T], func(T)) {
	f := &Future[T]{}
	resolve := func(v T) {
		// TODO: store v, then broadcast to every waiter.
	}
	return f, resolve
}

// Get returns the resolved value, blocking until resolution or ctx is done.
func (f *Future[T]) Get(ctx context.Context) (T, error) {
	// TODO: park on both the future and the context.
	var zero T
	return zero, nil
}
```

### Tests

```go
package challenge

import (
	"context"
	"testing"
	"time"
)

func TestResolveThenGet(t *testing.T) {
	f, resolve := NewFuture[int]()
	resolve(42)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := f.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if v != 42 {
		t.Fatalf("Get() = %d, want 42", v)
	}
}

func testGetBlocksRound(t *testing.T, hold time.Duration) {
	t.Helper()
	f, resolve := NewFuture[string]()
	type result struct {
		v   string
		err error
	}
	results := make(chan result, 3)
	for i := 0; i < 3; i++ {
		go func() {
			v, err := f.Get(context.Background())
			results <- result{v, err}
		}()
	}

	select {
	case <-results:
		t.Fatal("Get returned before the future was resolved")
	case <-time.After(hold):
	}

	resolve("ready")
	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatalf("Get() error = %v, want nil", r.err)
			}
			if r.v != "ready" {
				t.Fatalf("Get() = %q, want %q", r.v, "ready")
			}
		case <-time.After(time.Second):
			t.Fatal("a Get caller was not woken promptly by resolve")
		}
	}
}

func TestGetBlocksUntilResolveAndWakesEveryone(t *testing.T) {
	// Two rounds with different holds: Get must stay blocked for the
	// whole hold yet wake within 1s of resolve. A Get faked with a
	// fixed sleep cannot satisfy both rounds — only a real broadcast can.
	testGetBlocksRound(t, 250*time.Millisecond)
	testGetBlocksRound(t, 1500*time.Millisecond)
}

func TestGetHonorsContext(t *testing.T) {
	f, _ := NewFuture[int]()
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		_, err := f.Get(ctx)
		errs <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-errs:
		if err != context.Canceled {
			t.Fatalf("Get() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Get did not return after context cancellation")
	}
}

func TestResolveTwicePanics(t *testing.T) {
	_, resolve := NewFuture[int]()
	resolve(1)
	defer func() {
		if recover() == nil {
			t.Fatal("second resolve did not panic")
		}
	}()
	resolve(2)
}
```

# Lesson: The Run Queue {#the-run-queue}

Where do runnable goroutines actually wait their turn? Each P owns a **local
run queue**: a fixed 256-slot ring buffer, plus one special `runnext` slot
for a goroutine that should run immediately (that's where a freshly-unblocked
channel receiver goes). Overflow — and Gs with no P affinity — lands on a
mutex-protected **global run queue**. Every M loops forever in `schedule()`:

```go
// The scheduler, squinted at:
for {
	var gp *g
	// Every 61st tick, check the global queue *before* the local
	// ring, so a busy P can't starve the global queue forever.
	if schedtick%61 == 0 {
		gp = globrunqget()
	}
	if gp == nil {
		gp = runqget(pp)    // local ring — the common, lock-free case
	}
	if gp == nil {
		gp = findRunnable() // global queue, netpoll, then steal from other Ps
	}
	execute(gp)                // gogo(&gp.sched) — does not return until gp stops
}
```

`execute` hands the CPU to the goroutine, and here is the part this lesson
is really about: **the scheduler only gets the CPU back when the goroutine
gives it back**. A G re-enters the scheduler at defined points — it blocks
(`gopark`), it finishes (`goexit`), it calls `runtime.Gosched()`, it hits a
stack check that doubles as a preemption point. Between those points it owns
the thread outright. This is *cooperative scheduling*, and it was essentially
the whole story before Go 1.14 (a tight `for {}` loop could famously wedge
the garbage collector). The next lesson covers the preemptive backstop; here
we build the cooperative core, because it *is* the core.

### The handoff pattern

You will now build a scheduler in userspace, and the trick is worth spelling
out carefully, because it's the same trick you'll use in the final challenge.

We can't save and restore registers from Go code. But we don't need to: the
runtime already knows how to park and resume goroutines — through channels.
So we represent each *task* as a real goroutine that is **almost always
parked**, and enforce this invariant:

> At most one goroutine — either the scheduler or exactly one task — is
> ever *doing observable work*. Everyone else is parked on a channel
> receive, or has already done its part and is on its way to park.

Give each task an unbuffered `resume` channel, and the scheduler one `park`
channel shared by all tasks:

- Scheduler resumes a task: `t.resume <- struct{}{}` … then immediately
  blocks on `<-park`. The send wakes the task; the receive parks the
  scheduler. Control has been *handed off* — one runnable goroutine, still.
- Task yields: `park <- struct{}{}` … then immediately blocks on
  `<-t.resume`. Mirror image: scheduler wakes, task parks.

Each unbuffered send is a synchronous rendezvous: the receiver wakes with
the value (remember the direct stack-to-stack copy from last lesson), and
the sender's very next statement is its own blocking receive. Strictly
speaking both sides are runnable for that instant — but the handing-off
side does nothing observable between waking its partner and parking, and
that is all the invariant needs. A yield is therefore *two* handoffs:
task → scheduler (decide who's next) → next task. That's a real context
switch, built from parts you already understand — and because at most one
goroutine is ever doing observable work, execution is fully deterministic
even though `GOMAXPROCS` might be 32. Concurrency without parallelism, on
purpose.

A finished task is just a task whose function returned: signal the scheduler
one last time and never wait for `resume` again.

## Challenge: A Round-Robin Cooperative Scheduler {#round-robin points=40}

Build the heart of this course: a deterministic, single-threaded, cooperative
round-robin scheduler.

```go
type Sched struct { /* ... */ }

func NewSched() *Sched
func (s *Sched) Go(f func(yield func())) // register a task (before Run)
func (s *Sched) Run()                    // run all tasks to completion
```

Required semantics — these fully determine the execution order:

- `Run` maintains a FIFO run queue, initially holding the tasks in the order
  `Go` registered them.
- `Run` pops the front task and hands control to it. The task runs until it
  calls `yield()` or its function returns. On `yield()` the task goes to the
  **back** of the queue; on return it is removed.
- Exactly one task executes at any instant, and `Run` itself doesn't proceed
  while one does. Task bodies may therefore touch shared data structures
  without any locking — that's the property the tests exploit, and the
  property that makes this a scheduler rather than a thread pool.
- `Run` returns when the queue is empty. `Go` is only called before `Run`.

So three tasks A, B, C where A yields twice, B yields once, and C never
yields execute as: `A0 B0 C0 A1 B1 A2`.

Implement it with the lesson's handoff pattern: spawn one goroutine per task,
parked on its own `resume` channel; `yield` is a send on the shared `park`
channel followed by a receive on `resume`. Don't let a new task's goroutine
run before its first turn — park it on `resume` *before* calling `f`. And
make sure the "task finished" signal is distinguishable from "task yielded"
(a flag written before the final park-send is safely visible to the
scheduler: the channel handoff orders it).

### Starter

```go
package challenge

// Sched is a single-threaded, cooperative, round-robin scheduler.
type Sched struct {
	fns []func(yield func())
	// TODO: channels for the scheduler <-> task handoff.
}

// NewSched returns an empty scheduler.
func NewSched() *Sched {
	return &Sched{}
}

// Go registers a task. The task runs only during Run, and must call yield()
// to let other tasks run.
func (s *Sched) Go(f func(yield func())) {
	s.fns = append(s.fns, f)
}

// Run executes every registered task to completion, switching tasks only at
// yield() calls, in strict round-robin order.
func (s *Sched) Run() {
	// TODO: replace this — it runs each task to completion with no
	// interleaving, and yield does nothing.
	for _, f := range s.fns {
		f(func() {})
	}
}
```

### Tests

```go
package challenge

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tracer records step markers and detects concurrent execution behaviorally.
type tracer struct {
	mu      sync.Mutex
	steps   []string
	running atomic.Int64
	overlap atomic.Int64
}

func (tr *tracer) mark(s string) {
	if tr.running.Add(1) != 1 {
		tr.overlap.Add(1)
	}
	runtime.Gosched() // widen the window: concurrent tasks will collide here
	tr.mu.Lock()
	tr.steps = append(tr.steps, s)
	tr.mu.Unlock()
	tr.running.Add(-1)
}

func (tr *tracer) trace() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return strings.Join(tr.steps, " ")
}

func runSched(t *testing.T, s *Sched) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		s.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return — scheduler deadlocked")
	}
}

func TestRunEmpty(t *testing.T) {
	runSched(t, NewSched())
}

func TestSingleTask(t *testing.T) {
	var tr tracer
	s := NewSched()
	s.Go(func(yield func()) {
		tr.mark("solo0")
		yield()
		tr.mark("solo1")
	})
	runSched(t, s)
	if got, want := tr.trace(), "solo0 solo1"; got != want {
		t.Fatalf("trace = %q, want %q", got, want)
	}
}

func TestRoundRobinTrace(t *testing.T) {
	var tr tracer
	s := NewSched()
	s.Go(func(yield func()) {
		tr.mark("A0")
		yield()
		tr.mark("A1")
		yield()
		tr.mark("A2")
	})
	s.Go(func(yield func()) {
		tr.mark("B0")
		yield()
		tr.mark("B1")
	})
	s.Go(func(yield func()) {
		tr.mark("C0")
	})
	runSched(t, s)

	if got, want := tr.trace(), "A0 B0 C0 A1 B1 A2"; got != want {
		t.Fatalf("trace = %q, want %q", got, want)
	}
	if n := tr.overlap.Load(); n != 0 {
		t.Fatalf("%d overlapping steps — tasks ran concurrently, but exactly one task may run at a time", n)
	}
}
```

# Lesson: Going M:N {#going-mn}

One P and its run queue gave us determinism. The real runtime wants the
opposite: `GOMAXPROCS` Ps chewing through goroutines in parallel, with no
single point of contention. Two design decisions in Vyukov's scheduler make
that scale:

**Distributed queues + work stealing.** A single shared run queue would mean
every M fighting over one lock (this was, roughly, the pre-Go-1.1 scheduler,
and it was a bottleneck). Instead each P has its own lock-free ring, and an
M that runs dry goes hunting in `findRunnable`: global queue, netpoll, then
up to four rounds of picking random victim Ps and **stealing half** their
run queue. Stealing half amortizes: one steal buys a while of quiet. A
"spinning" M convention avoids both stampedes and lost wakeups when work
appears.

**A watchdog thread: sysmon.** Cooperative scheduling has failure modes, so
the runtime runs one M *without a P* — it never runs Go code — in a loop
(sleeping 20µs–10ms) doing what parked Ms cannot:

- **Preempting hogs.** Any G running >10ms gets flagged: `stackguard0` is
  poisoned so the next function-prologue stack check traps into the
  scheduler; since Go 1.14 sysmon also sends the thread a signal (SIGURG),
  whose handler parks the G at the nearest safe point — *asynchronous*
  preemption, no cooperation needed. The tight-loop wedge is gone.
- **Rescuing Ps from syscalls.** An M stuck in a blocking syscall drags its
  P with it; sysmon notices and hands the P to another M (`handoffp`), so
  10,000 goroutines doing file I/O don't strand the scheduler.

The shape to internalize: **N workers, one source of work, work conservation**
— no worker idles while tasks wait, and the number of executors is fixed
regardless of the number of tasks. That decoupling ("how many things can
happen at once" vs "how many things there are to do") is the entire point of
M:N. Your challenge is its minimal form: the classic bounded worker pool.
You'll build the *contended* single-queue version the runtime rejected — for
a fixed worker count it's perfectly good engineering (a buffered channel is
a fine queue at this scale); just know that per-worker deques + stealing is
what it grows into when workers multiply and the queue becomes the hot spot.

One correctness point worth sweating: **worker lifetime**. The runtime never
leaks Ms; your pool must not leak goroutines. "Queue is closed and drained"
is the shutdown signal, and `RunOn` must not return until every worker has
finished its last task and exited.

## Challenge: An M:N Worker Pool {#run-on points=30}

```go
func RunOn(workers int, tasks []func())
```

Execute all `tasks` using **exactly `workers`** concurrently-running worker
goroutines pulling from one shared FIFO queue. Required semantics:

- Every task runs exactly once. `RunOn` returns only after all tasks have
  completed *and* all worker goroutines have exited (no leaks).
- Exactly `workers` workers: under load the number of tasks executing
  concurrently reaches `workers` and never exceeds it. The tests measure
  this behaviorally with a high-water mark and a rendezvous barrier that
  only `workers` concurrent tasks can pass.
- The queue is FIFO: with `workers == 1`, tasks execute in slice order.
- Assume `workers >= 1`; `tasks` may be empty. Tasks may block; don't let
  one slow task stall the other workers.

The clean implementation is a buffered channel pre-loaded with all tasks and
then closed, plus `workers` goroutines ranging over it. You may use
`sync.WaitGroup` here — you built one in lesson 1; you've earned it.

### Starter

```go
package challenge

// RunOn executes every task using exactly `workers` concurrent worker
// goroutines pulling from a shared FIFO queue, then returns once all tasks
// are done and all workers have exited.
func RunOn(workers int, tasks []func()) {
	// TODO: this ignores `workers` and runs everything on the caller's
	// goroutine — no concurrency at all.
	for _, task := range tasks {
		task()
	}
}
```

### Tests

```go
package challenge

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func runOnWithTimeout(t *testing.T, workers int, tasks []func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		RunOn(workers, tasks)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunOn did not return")
	}
}

func TestAllTasksRunExactlyOnce(t *testing.T) {
	const n = 100
	counts := make([]atomic.Int64, n)
	tasks := make([]func(), n)
	for i := range tasks {
		tasks[i] = func() { counts[i].Add(1) }
	}
	runOnWithTimeout(t, 4, tasks)
	for i := range counts {
		if c := counts[i].Load(); c != 1 {
			t.Fatalf("task %d ran %d times, want exactly 1", i, c)
		}
	}
}

func TestSingleWorkerRunsInOrder(t *testing.T) {
	const n = 20
	var mu sync.Mutex
	var order []int
	tasks := make([]func(), n)
	for i := range tasks {
		tasks[i] = func() {
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
		}
	}
	runOnWithTimeout(t, 1, tasks)
	if len(order) != n {
		t.Fatalf("ran %d tasks, want %d", len(order), n)
	}
	for i, v := range order {
		if v != i {
			t.Fatalf("order[%d] = %d, want %d — single worker must drain the queue FIFO", i, v, i)
		}
	}
}

func TestExactlyNWorkers(t *testing.T) {
	const (
		workers = 3
		n       = 9
	)
	var current, highWater, timeouts atomic.Int64
	release := make(chan struct{})
	var releaseOnce sync.Once
	tasks := make([]func(), n)
	for i := range tasks {
		tasks[i] = func() {
			c := current.Add(1)
			for {
				h := highWater.Load()
				if c <= h || highWater.CompareAndSwap(h, c) {
					break
				}
			}
			// Rendezvous: only opens once `workers` tasks are in flight
			// at the same time.
			if c == workers {
				releaseOnce.Do(func() { close(release) })
			}
			select {
			case <-release:
			case <-time.After(2 * time.Second):
				timeouts.Add(1)
			}
			current.Add(-1)
		}
	}
	runOnWithTimeout(t, workers, tasks)
	if timeouts.Load() > 0 {
		t.Fatalf("tasks timed out at the rendezvous — fewer than %d tasks ever ran concurrently", workers)
	}
	if h := highWater.Load(); h != workers {
		t.Fatalf("concurrency high-water mark = %d, want exactly %d", h, workers)
	}
}

func TestBlockedTaskDoesNotStallOthers(t *testing.T) {
	// tasks[0] blocks until the *last* task has run. Real workers keep
	// pulling from the queue past the blocked task; an implementation
	// that runs tasks in fixed batches of `workers` deadlocks here.
	const n = 12
	gate := make(chan struct{})
	var ran atomic.Int64
	tasks := make([]func(), n)
	tasks[0] = func() {
		<-gate
		ran.Add(1)
	}
	for i := 1; i < n-1; i++ {
		tasks[i] = func() { ran.Add(1) }
	}
	tasks[n-1] = func() {
		ran.Add(1)
		close(gate)
	}
	runOnWithTimeout(t, 2, tasks)
	if got := ran.Load(); got != n {
		t.Fatalf("ran %d tasks, want %d", got, n)
	}
}

func TestNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	tasks := make([]func(), 50)
	for i := range tasks {
		tasks[i] = func() { time.Sleep(time.Millisecond) }
	}
	runOnWithTimeout(t, 4, tasks)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutines: %d before RunOn, %d after — workers leaked", before, runtime.NumGoroutine())
}
```

# Final Challenge: A Mini Runtime with a Virtual Clock {#mini-runtime points=80}

Time to combine everything. One piece of the real scheduler remains: *time*.
When a goroutine calls `time.Sleep`, the runtime doesn't spin and doesn't
dedicate a thread. It puts a timer into the P's **timer heap** (a min-heap
ordered by expiry) and parks the G. `findRunnable` checks the heap on every
pass; and when an M finds *nothing* runnable anywhere, it computes the delay
until the earliest timer and sleeps in the netpoller with exactly that
timeout. The scheduler literally fast-forwards to the next interesting
moment. Timers, network readiness, and run queues all merge into one
question: *what runs next, and if nothing, how long until something does?*

Your mini runtime does the same — but on a **virtual clock**, which is what
the real runtime would use if it could: no waiting, just jump `now` to the
next deadline. Virtual time makes the runtime fully deterministic (and is
exactly how simulation testing frameworks and the Go playground's fake time
work: sleep a virtual year, finish in a millisecond).

```go
type Runtime struct { /* ... */ }
type Task struct { /* ... */ }

func New() *Runtime
func (r *Runtime) Go(f func(t *Task)) // register a task
func (r *Runtime) Run()               // drive everything to completion
func (r *Runtime) Now() int           // current virtual time, in ticks

func (t *Task) Yield()          // back of the run queue
func (t *Task) Sleep(ticks int) // park until Now() >= wake-up time
```

Exact semantics — the tests assert precise traces, so follow these to the
letter:

1. `Run` drives a FIFO run queue, seeded with tasks in `Go`-call order.
   Exactly one task executes at any instant (lesson 4's handoff pattern);
   `Run` returns when no tasks remain, runnable or sleeping.
2. `Yield` moves the calling task to the back of the run queue.
3. `Sleep(n)` with `n > 0` parks the task with wake-up deadline
   `Now() + n`. `Sleep(n)` with `n <= 0` is equivalent to `Yield`.
4. The clock only advances when the run queue is **empty** and at least one
   task is sleeping: set the clock to the *earliest* pending deadline, then
   move **every** task whose deadline has arrived (`deadline <= Now()`) to
   the run queue — multiple tasks waking at the same tick enter the queue
   in **spawn order** (the order their `Go` calls happened).
5. `Go` may also be called from inside a running task: the new task goes to
   the back of the run queue (it first runs at the current virtual time) and
   takes the next spawn-order index.
6. The clock starts at 0, never advances while anything is runnable, and
   `Now()` remains correct after `Run` returns.

Implementation notes: this is lesson 4's scheduler plus a `sleepers`
collection. The task-side `Sleep` records its deadline (reading `r.now` from
a task is safe — the scheduler is parked while you run, and the channel
handoff orders the accesses), marks itself sleeping, and does the usual
park-send / resume-receive. The scheduler-side loop is:

```go
for {
	// 1. drain the run queue (tasks may yield back into it, spawn, sleep)
	// 2. no runnable and no sleepers? -> return
	// 3. now = earliest deadline; wake all due sleepers in spawn order
}
```

No real time anywhere: a task that sleeps a million virtual ticks completes
instantly — and the tests check exactly that.

### Starter

```go
package challenge

// Runtime is a deterministic cooperative scheduler with a virtual clock.
type Runtime struct {
	tasks []func(*Task)
	now   int
	// TODO: run queue, sleepers, handoff channels, spawn counter.
}

// Task is the handle a running task uses to yield and sleep.
type Task struct {
	rt *Runtime
	// TODO: resume channel, state, deadline, spawn index.
}

// New returns a runtime with the clock at 0 and no tasks.
func New() *Runtime {
	return &Runtime{}
}

// Go registers a task. It may be called before Run or from a running task.
func (r *Runtime) Go(f func(*Task)) {
	r.tasks = append(r.tasks, f)
}

// Now returns the current virtual time in ticks.
func (r *Runtime) Now() int {
	return r.now
}

// Run executes all tasks to completion, advancing the virtual clock only
// when every remaining task is asleep.
func (r *Runtime) Run() {
	// TODO: replace this — it runs tasks to completion in registration
	// order and never advances the clock.
	for i := 0; i < len(r.tasks); i++ {
		r.tasks[i](&Task{rt: r})
	}
}

// Yield moves the calling task to the back of the run queue.
func (t *Task) Yield() {
	// TODO
}

// Sleep parks the calling task until Now() >= the current time plus ticks.
// Sleep(n) for n <= 0 is equivalent to Yield.
func (t *Task) Sleep(ticks int) {
	// TODO: virtual time only — no time.Sleep!
}
```

### Tests

```go
package challenge

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder logs "label@tick" markers and detects concurrent execution.
type recorder struct {
	mu      sync.Mutex
	steps   []string
	running atomic.Int64
	overlap atomic.Int64
}

func (rec *recorder) mark(rt *Runtime, label string) {
	if rec.running.Add(1) != 1 {
		rec.overlap.Add(1)
	}
	runtime.Gosched()
	rec.mu.Lock()
	rec.steps = append(rec.steps, fmt.Sprintf("%s@%d", label, rt.Now()))
	rec.mu.Unlock()
	rec.running.Add(-1)
}

func (rec *recorder) trace() string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return strings.Join(rec.steps, " ")
}

func runRT(t *testing.T, rt *Runtime) time.Duration {
	t.Helper()
	done := make(chan struct{})
	start := time.Now()
	go func() {
		rt.Run()
		close(done)
	}()
	select {
	case <-done:
		return time.Since(start)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return — runtime deadlocked")
		return 0
	}
}

func TestVirtualClockTrace(t *testing.T) {
	var rec recorder
	rt := New()
	rt.Go(func(tk *Task) {
		rec.mark(rt, "A0")
		tk.Sleep(2)
		rec.mark(rt, "A1")
		tk.Sleep(2)
		rec.mark(rt, "A2")
	})
	rt.Go(func(tk *Task) {
		rec.mark(rt, "B0")
		tk.Sleep(1)
		rec.mark(rt, "B1")
		tk.Sleep(3)
		rec.mark(rt, "B2")
	})
	rt.Go(func(tk *Task) {
		rec.mark(rt, "C0")
		tk.Yield()
		rec.mark(rt, "C1")
		tk.Sleep(4)
		rec.mark(rt, "C2")
	})
	runRT(t, rt)

	want := "A0@0 B0@0 C0@0 C1@0 B1@1 A1@2 A2@4 B2@4 C2@4"
	if got := rec.trace(); got != want {
		t.Fatalf("trace = %q\nwant    %q", got, want)
	}
	if rt.Now() != 4 {
		t.Fatalf("Now() after Run = %d, want 4", rt.Now())
	}
	if n := rec.overlap.Load(); n != 0 {
		t.Fatalf("%d overlapping steps — exactly one task may execute at a time", n)
	}
}

func TestGoDuringRun(t *testing.T) {
	var rec recorder
	rt := New()
	rt.Go(func(tk *Task) {
		rec.mark(rt, "P0")
		rt.Go(func(tk *Task) {
			rec.mark(rt, "D0")
			tk.Sleep(2)
			rec.mark(rt, "D1")
		})
		tk.Sleep(1)
		rec.mark(rt, "P1")
	})
	runRT(t, rt)

	want := "P0@0 D0@0 P1@1 D1@2"
	if got := rec.trace(); got != want {
		t.Fatalf("trace = %q\nwant    %q", got, want)
	}
	if rt.Now() != 2 {
		t.Fatalf("Now() after Run = %d, want 2", rt.Now())
	}
}

func TestVirtualTimeIsFree(t *testing.T) {
	rt := New()
	rt.Go(func(tk *Task) {
		tk.Sleep(1_000_000)
	})
	elapsed := runRT(t, rt)
	if rt.Now() != 1_000_000 {
		t.Fatalf("Now() after Run = %d, want 1000000", rt.Now())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run took %v to sleep 1e6 virtual ticks — the clock must jump, not wait", elapsed)
	}
}

func TestSleepZeroIsYield(t *testing.T) {
	var rec recorder
	rt := New()
	rt.Go(func(tk *Task) {
		rec.mark(rt, "X0")
		tk.Sleep(0)
		rec.mark(rt, "X1")
	})
	rt.Go(func(tk *Task) {
		rec.mark(rt, "Y0")
	})
	runRT(t, rt)

	want := "X0@0 Y0@0 X1@0"
	if got := rec.trace(); got != want {
		t.Fatalf("trace = %q\nwant    %q", got, want)
	}
	if rt.Now() != 0 {
		t.Fatalf("Now() after Run = %d, want 0 — Sleep(0) must not advance the clock", rt.Now())
	}
}
```
