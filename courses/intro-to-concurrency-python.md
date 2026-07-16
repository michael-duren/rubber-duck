---
course: intro-to-concurrency
title: Introduction to Concurrency
language: python
description: Learn threads, queues, and how to reason about shared state.
duration_hours: 6
tags: [backend, concurrency]
extended_reading:
  - title: threading — Thread-based parallelism
    url: https://docs.python.org/3/library/threading.html
  - title: queue — A synchronized queue class
    url: https://docs.python.org/3/library/queue.html
---

# Lesson: Threads Basics {#threads-basics}

A `threading.Thread` is an OS-backed thread managed by the Python runtime.
In CPython (the runtime almost everyone uses), the Global Interpreter Lock
(GIL) lets only one thread execute Python bytecode at a time, so threads
don't run Python bytecode in true parallel — but the GIL is released during
I/O and by some C-level operations, so threads still overlap I/O and
blocking work. This course uses threads to practice coordinating concurrent
tasks — the coordination patterns are the point, not raw CPU speedup.

Concurrency is about *structuring* work as independent tasks; parallelism is
about *running* them at once. The violet panel is concurrency (one core,
tasks interleaved over time); the emerald panel is parallelism (many cores
running tasks simultaneously). Because of the GIL, CPython threads sit in the
violet picture for Python bytecode — interleaved, not truly parallel.

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

```python
import threading

def worker():
    print("hello from a thread")

threading.Thread(target=worker).start()
```

`Thread.start()` begins running `target` in a new thread and returns
immediately — the calling thread keeps going without waiting. Python's
default (non-daemon) threads do keep the process alive until they finish,
even if you never call `.join()`, but that isn't the same as your own code
knowing when a thread is done. Call `.join()` on a thread to block until it
completes, which is how you safely use a value a thread produced — the
`sum_nums` challenge below needs both halves' sums before it can add them,
so it must join both threads first. (A thread started with `daemon=True`
is the exception: the process kills it outright on exit, unjoined.)

### Getting a value back from a thread

There's a catch: whatever `target` returns goes nowhere — `Thread` throws
it away. To collect what a thread computed, hand it a place to write and
read that place back after you've joined. A pre-sized list works well
because each thread can own one index:

```python
import threading

results = [0, 0]

def work(i, part):
    results[i] = sum(part)

t0 = threading.Thread(target=work, args=(0, [1, 2, 3]))
t1 = threading.Thread(target=work, args=(1, [4, 5, 6]))
t0.start()
t1.start()
t0.join()
t1.join()
print(results[0] + results[1])  # 21
```

Each thread writes only its own slot, so the two never step on each other
even though they run at the same time — pointing both threads at the *same*
slot would let one overwrite the other's result. And read `results` only
*after* both `.join()` calls return: that's the point at which every thread
is guaranteed to have finished writing.

## Challenge: Run Work Concurrently {#concurrent-sum points=10}

Implement `sum_nums(nums)` so that it splits the list in half and sums each
half in its own thread, combining the results.

### Starter

```python
def sum_nums(nums):
    # TODO: sum each half in its own thread
    return 0
```

### Tests

```python
from solution import sum_nums


def test_sum_nums():
    cases = [
        ("basic", [1, 2, 3], 6),
        ("empty", [], 0),
        ("single", [42], 42),
        ("negatives", [-1, 1, -2, 2], 0),
    ]
    for name, nums, want in cases:
        got = sum_nums(nums)
        assert got == want, f"{name}: sum_nums({nums}) = {got}, want {want}"
```

# Lesson: Channels {#channels}

Python has no built-in channel type, but `queue.Queue` plays the same role:
one thread puts values in, another gets them out, and the queue itself
handles the locking. A `None` put onto the queue is a common convention for
"no more values" — a **sentinel** value the receiving side treats as a close
signal, rather than real data.

The amber producer puts values into the queue (violet); the cyan consumer
gets them. This producer/consumer shape is the same idea Go models with a
channel.

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

```python
import queue
import threading

q = queue.Queue()
threading.Thread(target=lambda: q.put(42)).start()
print(q.get())  # 42
```

### Draining a queue until the sentinel

The sentinel is a convention the two sides agree on, not something the queue
does for you, so the receiver decides when to stop by watching for it. The
usual shape is a loop that `get`s until it sees `None`:

```python
while True:
    v = q.get()
    if v is None:
        break
    print(v)
```

`Queue.get()` blocks while the queue is empty, so this loop simply parks the
thread until the next value — or the sentinel — arrives; there's no
busy-waiting to manage. Draining a *single* queue is one loop like this. To
consume two queues at once — as the fan-in challenge asks — give each its
own draining loop in its own thread, since each `get()` blocks
independently, and emit your own `None` downstream only once both inputs
have delivered theirs.

### Chaining stages: a pipeline

A *pipeline* is a chain of stages joined by queues, where each stage is a
thread that gets values from an upstream queue, does some work, and puts the
results on a downstream queue. A middle stage is both a consumer and a
producer: it drains its input until the sentinel, then puts its *own* `None`
on its output so the next stage knows when to stop — the same
drain-until-`None`-then-signal shape from above, one link further down the
chain.

```python
import queue
import threading

# stage 1: emit the numbers, then the sentinel
nums = queue.Queue()
def generate():
    for n in [1, 2, 3]:
        nums.put(n)
    nums.put(None)

# stage 2: square each value and forward it
squares = queue.Queue()
def square():
    while True:
        n = nums.get()
        if n is None:
            break
        squares.put(n * n)
    squares.put(None)  # forward end-of-stream downstream

# stage 3: sum whatever comes out of the last queue
result = [0]
def total():
    s = 0
    while True:
        v = squares.get()
        if v is None:
            break
        s += v
    result[0] = s

threads = [threading.Thread(target=f) for f in (generate, square, total)]
for t in threads:
    t.start()
for t in threads:
    t.join()
print(result[0])  # 14
```

Each stage putting its *own* `None` on its output queue is what makes the
shutdowns cascade: stage 1's sentinel ends stage 2's loop, which then puts
the sentinel that ends stage 3's loop. Joining every thread before reading
`result` is what makes that read safe — by the time `total`'s thread has
finished, it has already written the final sum, exactly like reading the
pre-sized list only after both `.join()` calls back in the threads lesson.

## Challenge: Fan In {#fan-in points=15}

Implement `merge(a, b)` where `a` and `b` are `queue.Queue` instances that
will each eventually receive a `None` sentinel marking end-of-stream. Return
a new `queue.Queue` that yields every value from both inputs (order between
them doesn't matter) and then yields `None` once both are exhausted.

### Starter

```python
def merge(a, b):
    # TODO
    return None
```

### Tests

```python
import queue
import threading

from solution import merge


def test_merge():
    a = queue.Queue()
    b = queue.Queue()
    threading.Thread(target=lambda: (a.put(1), a.put(3), a.put(None))).start()
    threading.Thread(target=lambda: (b.put(2), b.put(None))).start()

    out = merge(a, b)
    got = []
    while True:
        v = out.get()
        if v is None:
            break
        got.append(v)
    assert sorted(got) == [1, 2, 3]
```

# Final Challenge: Build a Pipeline {#final points=50}

Combine everything: implement `pipeline(nums)` that feeds the input through
a three-stage pipeline — generate, square, sum — connected by queues, with
each stage running in its own thread.

### Starter

```python
def pipeline(nums):
    # TODO: generate -> square -> sum, one thread per stage
    return 0
```

### Tests

```python
from solution import pipeline


def test_pipeline():
    cases = [
        ("basic", [1, 2, 3], 14),
        ("empty", [], 0),
        ("one", [5], 25),
    ]
    for name, nums, want in cases:
        got = pipeline(nums)
        assert got == want, f"{name}: pipeline({nums}) = {got}, want {want}"
```
