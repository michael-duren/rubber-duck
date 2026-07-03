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

# Lesson: Goroutines Basics {#goroutines-basics}

A `threading.Thread` is an OS-backed thread managed by the Python runtime.
The GIL means threads don't run Python bytecode in true parallel, but they do
overlap I/O and blocking work, and this course uses them to practice
coordinating concurrent tasks — the coordination patterns are the point.

```python
import threading

def worker():
    print("hello from a thread")

threading.Thread(target=worker).start()
```

`Thread.start()` begins running `target` in a new thread and returns
immediately. The program doesn't wait for background threads to finish on
its own — call `.join()` on a thread to block until it completes, which is
why synchronization matters.

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
"no more values" — the receiving side treats it as a close signal.

```python
import queue
import threading

q = queue.Queue()
threading.Thread(target=lambda: q.put(42)).start()
print(q.get())  # 42
```

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
