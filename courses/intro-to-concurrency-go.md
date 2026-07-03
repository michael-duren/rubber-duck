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

```go
go func() {
	fmt.Println("hello from a goroutine")
}()
```

The `go` keyword starts the function in a new goroutine and returns
immediately. The program exits when `main` returns — it does **not** wait for
other goroutines, which is why synchronization matters.

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

```go
ch := make(chan int)
go func() { ch <- 42 }()
fmt.Println(<-ch) // 42
```

Close a channel to signal "no more values". Receivers can range over a
channel until it is closed.

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
