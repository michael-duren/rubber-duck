---
course: intro-to-concurrency
title: Introduction to Concurrency
language: go
description: Learn goroutines, channels, and how to reason about shared state.
duration_hours: 6
tags: [backend, concurrency]
extended_reading:
  - title: The Go Memory Model
    url: https://go.dev/ref/mem
  - title: Share Memory By Communicating
    url: https://go.dev/blog/codelab-share
---

# Lesson: Goroutines Basics {#goroutines-basics}

A goroutine is a lightweight thread managed by the Go runtime. Starting one
costs a few kilobytes of stack, so programs routinely run thousands of them.

Concurrency is about *structuring* work as independent tasks; parallelism is
about *running* them at once. The violet panel is concurrency (one core,
tasks interleaved over time); the emerald panel is parallelism (many cores
running tasks simultaneously).

```d2
grid-rows: 2
grid-gap: 30

concurrency: "Concurrency" {
  style.stroke: "#a78bfa"
  style.stroke-width: 2
  desc: "one core: A and B interleaved over time" {
    shape: text
  }
  timeline: "" {
    grid-rows: 1
    grid-gap: 0
    s1: A
    s2: B
    s3: A
    s4: B
    s5: A
    s6: B
  }
}

parallelism: "Parallelism" {
  style.stroke: "#34d399"
  style.stroke-width: 2
  desc: "two cores: A and B run at the same time" {
    shape: text
  }
  core1: "Core 1" {
    grid-rows: 1
    grid-gap: 0
    a1: A
    a2: A
    a3: A
  }
  core2: "Core 2" {
    grid-rows: 1
    grid-gap: 0
    b1: B
    b2: B
    b3: B
  }
}
```

```go
go func() {
	fmt.Println("hello from a goroutine")
}()
```

The `go` keyword starts the function in a new goroutine and returns
immediately. The program exits when `main` returns — it does **not** wait for
other goroutines, which is why synchronization matters.

### Waiting for goroutines: sync.WaitGroup

Since the program won't wait for goroutines on its own, you need something
that will. `sync.WaitGroup` is a counter built for exactly that: call
`Add(n)` before starting `n` goroutines, have each one call `Done()` when it
finishes (usually via `defer`), and call `Wait()` to block until every
`Done()` has landed.

```go
var wg sync.WaitGroup
results := make([]int, 2)

wg.Add(2)
go func() {
	defer wg.Done()
	results[0] = workA()
}()
go func() {
	defer wg.Done()
	results[1] = workB()
}()

wg.Wait()
fmt.Println(results[0] + results[1])
```

Each goroutine writes to its own slot in `results`, so there's no data race
even though both run at the same time — two goroutines writing to the
*same* variable without synchronization is a race, and `go test -race`
would catch it. Read `results` only after `Wait()` returns, once every
goroutine is guaranteed to have finished writing.

## Challenge: Run Work Concurrently {#concurrent-sum points=10}

Implement `Sum(nums []int) int` so that it splits the slice in half and sums
each half in its own goroutine, combining the results.

### Starter

```go
package challenge

func Sum(nums []int) int {
	// TODO: sum each half in its own goroutine
	return 0
}
```

### Tests

```go
package challenge

import "testing"

func TestSum(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want int
	}{
		{"basic", []int{1, 2, 3}, 6},
		{"empty", nil, 0},
		{"single", []int{42}, 42},
		{"negatives", []int{-1, 1, -2, 2}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sum(c.in); got != c.want {
				t.Errorf("Sum(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
```

# Lesson: Channels {#channels}

Channels connect goroutines: one sends, another receives. Unbuffered channels
synchronize both sides — a send blocks until a receiver is ready.

The amber producer sends values into the channel (violet); the cyan consumer
receives them. This producer/consumer shape is the same idea Python models
with a `queue.Queue`.

```d2
direction: right

producer: "Producer\nsends / puts" {
  style.stroke: "#d97706"
  style.stroke-width: 2
}

chan: "channel (Go)\nqueue (Python)" {
  shape: queue
  style.stroke: "#a78bfa"
  style.stroke-width: 2
}

consumer: "Consumer\nreceives / gets" {
  style.stroke: "#22d3ee"
  style.stroke-width: 2
}

producer -> chan: "value"
chan -> consumer: "value"
```

```go
ch := make(chan int)
go func() { ch <- 42 }()
fmt.Println(<-ch) // 42
```

Close a channel to signal "no more values". Receivers can range over a
channel until it is closed. Only the sending side should close a channel —
closing an already-closed channel, or sending on a closed one, panics.

```go
ch := make(chan int)
go func() {
	defer close(ch)
	for i := 1; i <= 3; i++ {
		ch <- i
	}
}()
for v := range ch { // 1, 2, 3 — the loop ends when ch is closed and drained
	fmt.Println(v)
}
```

The sender closes `ch` (here with `defer`) once it has nothing left to send;
that close is what lets the `range` loop terminate instead of blocking
forever on a fourth receive. This producer-closes-then-receiver-ranges shape
is the backbone of every channel program below.

A receive can also ask whether the channel is still open:

```go
v, ok := <-ch // ok is false once ch is closed and drained
```

`ok` is `false` only once the channel is both closed *and* drained — every
value sent before the close has already been received; `v` is the zero
value in that case.

### Reading from multiple channels: select

`select` lets a goroutine wait on more than one channel operation at once.
It blocks until one case is ready, then runs that case; if several are
ready at the same time, it picks one at random. Combined with the
comma-ok form above, `select` can read from two channels until both are
drained, retiring whichever one closes first:

```go
for a != nil || b != nil {
	select {
	case v, ok := <-a:
		if !ok {
			a = nil // never selects again — a nil channel blocks forever
			continue
		}
		fmt.Println("from a:", v)
	case v, ok := <-b:
		if !ok {
			b = nil
			continue
		}
		fmt.Println("from b:", v)
	}
}
```

Setting a drained channel variable to `nil` is the trick that retires it:
a receive on a `nil` channel never becomes ready, so `select` simply stops
considering that case and keeps servicing the other one.

### Chaining stages: a pipeline

A *pipeline* is a chain of stages joined by channels, where each stage is a
goroutine that receives from an upstream channel, does some work, and sends
downstream. A middle stage is both a receiver and a sender: it ranges over
its input and closes its output once that input runs dry — the same
producer-closes-then-receiver-ranges shape from above, one link further down
the chain.

```go
// stage 1: emit the numbers, then close
nums := make(chan int)
go func() {
	defer close(nums)
	for _, n := range []int{1, 2, 3} {
		nums <- n
	}
}()

// stage 2: square each value and forward it
squares := make(chan int)
go func() {
	defer close(squares)
	for n := range nums { // ends when stage 1 closes nums
		squares <- n * n
	}
}()

// stage 3: the caller consumes the last channel
total := 0
for sq := range squares { // ends when stage 2 closes squares
	total += sq
}
fmt.Println(total) // 14
```

Each stage closing *its own* output channel is what makes the shutdowns
cascade: stage 1 closing `nums` lets stage 2's `range` finish, which fires
its `defer close(squares)`, which in turn lets the final loop end. Running
the last stage in the calling goroutine is what makes it safe to read
`total` afterward — the `range` over `squares` only returns once every
upstream stage has closed, so it doubles as the synchronization point.

## Challenge: Fan In {#fan-in points=15}

Implement `Merge(a, b <-chan int) <-chan int` returning a channel that yields
every value from both inputs and closes when both are exhausted.

### Starter

```go
package challenge

func Merge(a, b <-chan int) <-chan int {
	// TODO
	return nil
}
```

### Tests

```go
package challenge

import (
	"sort"
	"testing"
)

func TestMerge(t *testing.T) {
	a := make(chan int)
	b := make(chan int)
	go func() { a <- 1; a <- 3; close(a) }()
	go func() { b <- 2; close(b) }()

	var got []int
	for v := range Merge(a, b) {
		got = append(got, v)
	}
	sort.Ints(got)
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
```

# Final Challenge: Build a Pipeline {#final points=50}

Combine everything: implement `Pipeline(nums []int) int` that feeds the input
through a three-stage channel pipeline — generate, square, sum — with each
stage in its own goroutine.

### Starter

```go
package challenge

func Pipeline(nums []int) int {
	// TODO: generate -> square -> sum, one goroutine per stage
	return 0
}
```

### Tests

```go
package challenge

import "testing"

func TestPipeline(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want int
	}{
		{"basic", []int{1, 2, 3}, 14},
		{"empty", nil, 0},
		{"one", []int{5}, 25},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Pipeline(c.in); got != c.want {
				t.Errorf("Pipeline(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
```
