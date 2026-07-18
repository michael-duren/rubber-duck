---
course: dsa-from-scratch
title: Data Structures & Algorithms from Scratch
language: python
description: >
  Python hands you a toolbox — list, dict, sorted — and lets you treat the
  contents as magic. This course takes the toolbox away. You will build a
  growable array, the classic sorts, a binary heap, a hash map, and graph
  traversal with your own hands, and each structure you build becomes a
  tool you reuse as the problems get harder — until the finale, where your
  map, your heap, and Kahn's algorithm combine into a dependency-aware
  build scheduler.
duration_hours: 12
tags: [data-structures, algorithms, python]
extended_reading:
  - title: "Kahn (1962), Topological sorting of large networks — the original paper"
    url: https://dl.acm.org/doi/10.1145/368996.369025
  - title: "Sorting algorithm animations — watch the algorithms you built"
    url: https://www.toptal.com/developers/sorting-algorithms
  - title: "listsort.txt — Tim Peters explains Timsort, the sort inside Python"
    url: https://github.com/python/cpython/blob/main/Objects/listsort.txt
  - title: "The Algorithm Design Manual (Skiena) — where to go next"
    url: https://www.algorist.com/
---

# Lesson: The Dynamic Array {#dynamic-array}

Everything in this course is built on one humble object: a block of memory
you can index in O(1). An array is fast for exactly one reason — element
`i` lives at `start + i × elementSize`, so reading `a[i]` is one multiply,
one add, one load, no matter how big the array is.

The catch is that a block of memory has a fixed size. Real programs don't
know their sizes up front, so every language grows arrays the same way
behind the scenes — Python's `list.append`, Go's `append`, C++'s
`vector::push_back` are all the same data structure. In this course you
don't get to use them; you get to *be* them.

## Length is not capacity

A growable array tracks two numbers:

```
data:   [ 7 | 3 | 9 | 5 | . | . | . | . ]
          <---- length=4 ---->
          <---------- capacity=8 ------->
```

- **length** — how many slots hold real values,
- **capacity** — how many slots exist before you must reallocate.

`push` normally just writes to slot `length` and increments it — O(1). The
interesting moment is `length == capacity`: the block is full, and blocks
can't grow in place (something else may live right after them in memory).
You must allocate a *bigger* block, copy everything across, and abandon the
old one.

Click through the moment the block runs out of room — amber is the incoming value, dashed grey is freed memory:

```d2
direction: down

old: "cap 4" {
  grid-rows: 1
  grid-gap: 0
  c0: "5" { width: 64; height: 64 }
  c1: "8" { width: 64; height: 64 }
  c2: "3" { width: 64; height: 64 }
  c3: "" { width: 64; height: 64; style.stroke-dash: 4 }
}

new: "cap 8" {
  grid-rows: 1
  grid-gap: 0
  style.opacity: 0
  c0: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
  c1: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
  c2: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
  c3: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
  c4: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
  c5: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
  c6: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
  c7: "" { width: 64; height: 64; style.stroke-dash: 4; style.opacity: 0 }
}

old -> new: { style.opacity: 0 }

steps: {
  "append(9): 9 takes the spare slot — len 4, cap 4": {
    old.c3.label: "9"
    old.c3.style.stroke: "#d97706"
    old.c3.style.stroke-width: 3
    old.c3.style.stroke-dash: 0
  }
  "append(2): no room — len 4 == cap 4": {
    old.style.stroke: "#dc2626"
    old.style.stroke-width: 3
    old.c3.style.stroke-width: 2
  }
  "grow: allocate a new array with cap 8": {
    new.style.opacity: 1
    new.c0.style.opacity: 1
    new.c1.style.opacity: 1
    new.c2.style.opacity: 1
    new.c3.style.opacity: 1
    new.c4.style.opacity: 1
    new.c5.style.opacity: 1
    new.c6.style.opacity: 1
    new.c7.style.opacity: 1
  }
  "copy all 4 elements into the new array": {
    new.c0.label: "5"
    new.c1.label: "8"
    new.c2.label: "3"
    new.c3.label: "9"
    new.c0.style.stroke: "#16a34a"
    new.c1.style.stroke: "#16a34a"
    new.c2.style.stroke: "#16a34a"
    new.c3.style.stroke: "#16a34a"
    new.c0.style.stroke-dash: 0
    new.c1.style.stroke-dash: 0
    new.c2.style.stroke-dash: 0
    new.c3.style.stroke-dash: 0
    old.style.stroke: "#9ca3af"
    old.style.stroke-dash: 4
    old.style.stroke-width: 2
    old.style.font-color: "#9ca3af"
    old.c0.style.stroke: "#9ca3af"
    old.c1.style.stroke: "#9ca3af"
    old.c2.style.stroke: "#9ca3af"
    old.c3.style.stroke: "#9ca3af"
    old.c0.style.stroke-dash: 4
    old.c1.style.stroke-dash: 4
    old.c2.style.stroke-dash: 4
    old.c3.style.stroke-dash: 4
    old.c0.style.font-color: "#9ca3af"
    old.c1.style.font-color: "#9ca3af"
    old.c2.style.font-color: "#9ca3af"
    old.c3.style.font-color: "#9ca3af"
  }
  "append(2) lands in slot 4 — len 5, cap 8": {
    new.c4.label: "2"
    new.c4.style.stroke: "#d97706"
    new.c4.style.stroke-width: 3
    new.c4.style.stroke-dash: 0
  }
}
```

## Why doubling, and not +1

How much bigger? This choice is the whole ballgame. Grow by one slot each
time and every push copies everything: pushing n items costs
1 + 2 + 3 + … + n ≈ n²/2 copies — quadratic, and your "fast array" dies at
scale.

Grow by *doubling* and the copies telescope. Reaching length n costs at
most 1 + 2 + 4 + … + n ≈ 2n copies total — the last doubling dominates and
everything before it sums to less. Spread ("amortized") over n pushes,
that's a constant ~2 copies per push. Occasional expensive pushes, O(1) on
average — this argument is called **amortized analysis**, and you'll meet
it again when your hash map resizes.

CPython's `list` does exactly this dance — it over-allocates by roughly
1.125× rather than a clean doubling, trading a little extra copying for
less wasted memory, but the amortized argument is identical. You'll use
plain doubling, which keeps the schedule easy to reason about (and to
test). For this challenge, `append` is off the table — you're building it.

## Challenge: Growable Array {#growable-array points=10}

Implement `DynArray`, a growable array of ints. The point is to do what
`list.append` does yourself, so inside your implementation treat lists as
fixed-size blocks: allocate with `[None] * capacity`, read and write by
index, and never call `append` (or `+=`, or slicing tricks) on the backing
store.

- `DynArray()` is an empty, usable array: length 0, capacity 0.
- Keep the starter's exact attributes: `self._data` is the backing block —
  a preallocated list of `None`s whose `len` IS the capacity — and
  `self._length` counts the slots in use.
- `push(v)` writes to the next free slot. When full, allocate a new block
  of **double** the capacity (0 grows to 1) as `[None] * newcap`, copy the
  old contents over with a loop, then push.
- `get(i)` returns the i'th element; callers only pass in-range indexes.
- `__len__` is the number of elements pushed; `capacity()` is the size of
  the backing block.

The tests inspect `capacity()` after every push, so the doubling schedule
(1, 2, 4, 8, …) is part of the contract.

### Starter

```python
class DynArray:
    """A growable array of ints.

    _data is the backing block: a preallocated list of None slots whose
    len() IS the capacity. _length counts the slots in use.
    """

    def __init__(self):
        self._data = []    # capacity 0: no slots allocated yet
        self._length = 0   # slots in use

    def push(self, v):
        # TODO: if self._length == len(self._data), allocate a new
        # [None] * newcap block (double the capacity, 0 -> 1) and copy the
        # old contents over with a loop; then write and bump _length
        pass

    def get(self, i):
        # TODO: return the i'th element (0-indexed, always in range)
        return 0

    def __len__(self):
        # TODO: the number of elements pushed
        return 0

    def capacity(self):
        # TODO: the size of the backing block
        return 0
```

### Tests

```python
from solution import DynArray


def next_pow2(n):
    c = 1
    while c < n:
        c *= 2
    return c


def test_new_array_is_empty():
    a = DynArray()
    assert len(a) == 0, f"len = {len(a)}, want 0"
    assert a.capacity() == 0, f"capacity = {a.capacity()}, want 0"


def test_push_and_get():
    a = DynArray()
    for i in range(100):
        a.push(i * i)
    assert len(a) == 100, f"len = {len(a)}, want 100"
    for i in range(100):
        got = a.get(i)
        assert got == i * i, f"get({i}) = {got}, want {i * i}"


def test_doubling_schedule():
    a = DynArray()
    for i in range(1, 201):
        a.push(i)
        want = next_pow2(i)
        assert a.capacity() == want, (
            f"after {i} pushes: capacity = {a.capacity()}, want {want}"
        )


def test_growth_preserves_contents():
    a = DynArray()
    # Push exactly past several capacity boundaries and re-check everything.
    for i in range(17):
        a.push(1000 - i)
        for j in range(i + 1):
            assert a.get(j) == 1000 - j, (
                f"after {i + 1} pushes, get({j}) = {a.get(j)}, want {1000 - j}"
            )
```

# Lesson: Sorting by Insertion {#sorting-by-insertion}

Sorting is the classic proving ground for algorithms because you can feel
the difference between a bad idea and a good one — n² versus n log n is
the difference between "coffee break" and "instant" at a million elements.
We start with the n² algorithm that's actually worth knowing.

## The invariant

Insertion sort is how most people sort a hand of cards: everything to your
left is already in order; pick up the next card and walk it left until it
sits correctly.

Formally, the algorithm maintains an **invariant** — a statement that's
true after every step: *after processing index i, the prefix `a[0..i]` is
sorted.* Each round takes the next element and shifts larger elements one
slot right until the hole is where it belongs:

```
sorted prefix | next
[ 3  7  9 ] | 5  ...      take 5
[ 3  7  9  9 ]            9 > 5, shift right
[ 3  7  7  9 ]            7 > 5, shift right
[ 3  5  7  9 ]            3 ≤ 5, drop 5 in the hole
```

Click through a full run on `[3 7 5 1]` — amber is the element being placed, green the sorted prefix:

```d2
direction: right

arr: "" {
  grid-rows: 1
  grid-gap: 0
  c0: "3" { width: 64; height: 64 }
  c1: "7" { width: 64; height: 64 }
  c2: "5" { width: 64; height: 64 }
  c3: "1" { width: 64; height: 64 }
}

steps: {
  "key = 7: 3 ≤ 7, already in place": {
    arr.c0.style.stroke: "#16a34a"
    arr.c1.style.stroke: "#d97706"
    arr.c1.style.stroke-width: 3
  }
  "key = 5: 7 > 5, so 7 shifts right": {
    arr.c0.style.stroke: "#16a34a"
    arr.c1.label: "→"
    arr.c1.style.stroke: "#dc2626"
    arr.c1.style.stroke-width: 3
    arr.c2.label: "7"
    arr.c2.style.stroke: "#d97706"
    arr.c2.style.stroke-width: 3
  }
  "5 drops into the gap: [3 5 7] sorted": {
    arr.c1.label: "5"
    arr.c1.style.stroke: "#16a34a"
    arr.c1.style.stroke-width: 2
    arr.c2.style.stroke: "#16a34a"
    arr.c2.style.stroke-width: 2
  }
  "key = 1: smaller than all three, all shift": {
    arr.c1.label: "→"
    arr.c2.label: "→"
    arr.c3.label: "→"
    arr.c1.style.stroke: "#dc2626"
    arr.c2.style.stroke: "#dc2626"
    arr.c3.style.stroke: "#dc2626"
    arr.c3.style.stroke-width: 3
  }
  "1 lands at the front: [1 3 5 7], done": {
    arr.c0.label: "1"
    arr.c1.label: "3"
    arr.c2.label: "5"
    arr.c3.label: "7"
    arr.c0.style.stroke: "#16a34a"
    arr.c1.style.stroke: "#16a34a"
    arr.c2.style.stroke: "#16a34a"
    arr.c3.style.stroke: "#16a34a"
    arr.c1.style.stroke-width: 2
    arr.c2.style.stroke-width: 2
    arr.c3.style.stroke-width: 2
  }
}
```

Thinking in invariants is the transferable skill here: every structure in
this course (heap property, load factor, BFS frontier) is defined by an
invariant its operations promise to preserve.

## Where n² is the right answer

Worst case (reverse-sorted input) every element walks all the way left:
about n²/2 shifts. But two properties make insertion sort a workhorse
rather than a toy:

- **Adaptive.** The cost is really O(n + inversions) — an *inversion*
  being a pair that's out of order. Nearly-sorted input has few
  inversions, so insertion sort runs in nearly linear time. Appending a
  handful of new records to a sorted file? Insertion sort is hard to beat.
- **Tiny constants.** For small n (a few dozen), its simple inner loop
  beats the fancy algorithms' bookkeeping. Production sorts — including
  Python's own — lean on insertion sort for small runs.

One more word you'll need later: a sort is **stable** if equal elements
keep their original relative order. Insertion sort is stable (it only
shifts *strictly larger* elements). Hold that thought for merge sort.

## Challenge: Insertion Sort {#insertion-sort points=10}

Sort a list of ints in place using insertion sort: for each index i from
1 up, shift larger elements of the sorted prefix right and insert `nums[i]`
where it belongs. The function mutates `nums` and returns `None`. No extra
list — and no `sorted()` or `list.sort()`, here or in any sorting
challenge: building the machinery is the point.

### Starter

```python
def insertion_sort(nums):
    """Sort nums in place, ascending. Returns None."""
    # TODO: for each i, walk nums[i] left through the sorted prefix
```

### Tests

```python
from solution import insertion_sort


def test_insertion_sort():
    cases = [
        ("empty", []),
        ("single", [42]),
        ("sorted", [1, 2, 3, 4, 5]),
        ("reverse", [5, 4, 3, 2, 1]),
        ("duplicates", [3, 1, 3, 1, 3]),
        ("negatives", [0, -5, 3, -5, 2, 0]),
        ("two", [2, 1]),
    ]
    for name, nums in cases:
        want = sorted(nums)
        insertion_sort(nums)
        assert nums == want, f"{name}: got {nums}, want {want}"


def test_insertion_sort_big():
    # Deterministic pseudo-random input via a small LCG.
    seed = 1
    nums = []
    for _ in range(1000):
        seed = (seed * 1664525 + 1013904223) % 2**32
        nums.append(seed % 10000)
    want = sorted(nums)
    insertion_sort(nums)
    for i, w in enumerate(want):
        assert nums[i] == w, f"big input: mismatch at {i}: got {nums[i]}, want {w}"
```

# Lesson: Divide and Conquer — Merge Sort {#divide-and-conquer}

Insertion sort does O(n²) work because each element learns only one thing
per comparison: "am I past my spot yet?" To sort faster, comparisons must
do more work — and the trick is to make sortedness *compose*.

## The key insight: merging is linear

If you have two *already sorted* lists, combining them into one sorted
list is easy: look at the two front elements, take the smaller, repeat.
Each step consumes one element, so merging m + n elements takes m + n
steps. No searching, no shifting — sorted inputs make the next output
element obvious.

```
a: [1  4  9]      b: [2  3  8]      out: []
    ^                 ^
take 1 (1<2)  → out [1]
take 2 (4>2)  → out [1 2]
take 3 (4>3)  → out [1 2 3]
take 4 (4<8)  → out [1 2 3 4]
take 8, then drain a's leftovers → [1 2 3 4 8 9]
```

Click through the merge of `[1 4 9]` and `[2 3 8]` — amber marks the pair under comparison, dashed grey the consumed cells:

```d2
direction: down

m: "" {
  grid-rows: 2
  grid-gap: 24

  lft: "left" {
    grid-rows: 1
    grid-gap: 0
    c0: "1" { width: 64; height: 64 }
    c1: "4" { width: 64; height: 64 }
    c2: "9" { width: 64; height: 64 }
  }

  rgt: "right" {
    grid-rows: 1
    grid-gap: 0
    c0: "2" { width: 64; height: 64 }
    c1: "3" { width: 64; height: 64 }
    c2: "8" { width: 64; height: 64 }
  }

  out: "out" {
    grid-rows: 1
    grid-gap: 0
    c0: "" { width: 64; height: 64 }
    c1: "" { width: 64; height: 64 }
    c2: "" { width: 64; height: 64 }
    c3: "" { width: 64; height: 64 }
    c4: "" { width: 64; height: 64 }
    c5: "" { width: 64; height: 64 }
  }
}

steps: {
  "compare 1 vs 2 → 1 wins, fills out[0]": {
    m.lft.c0.style.stroke: "#d97706"
    m.lft.c0.style.stroke-width: 3
    m.lft.c0.style.stroke-dash: 4
    m.lft.c0.style.font-color: "#9ca3af"
    m.rgt.c0.style.stroke: "#d97706"
    m.rgt.c0.style.stroke-width: 3
    m.out.c0.label: "1"
    m.out.c0.style.stroke: "#16a34a"
  }
  "compare 4 vs 2 → 2 wins, fills out[1]": {
    m.lft.c0.style.stroke: "#9ca3af"
    m.lft.c0.style.stroke-width: 2
    m.lft.c1.style.stroke: "#d97706"
    m.lft.c1.style.stroke-width: 3
    m.rgt.c0.style.stroke-dash: 4
    m.rgt.c0.style.font-color: "#9ca3af"
    m.out.c1.label: "2"
    m.out.c1.style.stroke: "#16a34a"
  }
  "compare 4 vs 3 → 3 wins, fills out[2]": {
    m.rgt.c0.style.stroke: "#9ca3af"
    m.rgt.c0.style.stroke-width: 2
    m.rgt.c1.style.stroke: "#d97706"
    m.rgt.c1.style.stroke-width: 3
    m.rgt.c1.style.stroke-dash: 4
    m.rgt.c1.style.font-color: "#9ca3af"
    m.out.c2.label: "3"
    m.out.c2.style.stroke: "#16a34a"
  }
  "compare 4 vs 8 → now 4 wins, fills out[3]": {
    m.rgt.c1.style.stroke: "#9ca3af"
    m.rgt.c1.style.stroke-width: 2
    m.lft.c1.style.stroke-dash: 4
    m.lft.c1.style.font-color: "#9ca3af"
    m.rgt.c2.style.stroke: "#d97706"
    m.rgt.c2.style.stroke-width: 3
    m.out.c3.label: "4"
    m.out.c3.style.stroke: "#16a34a"
  }
  "compare 9 vs 8 → 8 wins; right is empty": {
    m.lft.c1.style.stroke: "#9ca3af"
    m.lft.c1.style.stroke-width: 2
    m.lft.c2.style.stroke: "#d97706"
    m.lft.c2.style.stroke-width: 3
    m.rgt.c2.style.stroke-dash: 4
    m.rgt.c2.style.font-color: "#9ca3af"
    m.out.c4.label: "8"
    m.out.c4.style.stroke: "#16a34a"
  }
  "no compare left: 9 drains in — ≤ n compares": {
    m.lft.c2.style.stroke: "#9ca3af"
    m.lft.c2.style.stroke-width: 2
    m.lft.c2.style.stroke-dash: 4
    m.lft.c2.style.font-color: "#9ca3af"
    m.rgt.c2.style.stroke: "#9ca3af"
    m.rgt.c2.style.stroke-width: 2
    m.out.c5.label: "9"
    m.out.c5.style.stroke: "#16a34a"
  }
}
```

The only subtlety is the end: one side runs dry, and the other side's
remainder is already sorted — copy it straight across.

## Recursion does the rest

Merge sort is the one-line consequence: *split the list in half, sort
each half (recursively), merge.* A list of length 0 or 1 is already
sorted — that's the base case that stops the recursion.

Why is this n log n? Picture the recursion as a tree of levels. At the top
level you merge n elements. One level down, two merges of n/2 — still n
total. Every level does O(n) merge work, and halving n reaches 1 in log₂ n
steps: **log n levels × n work per level = O(n log n)**. At a million
elements that's ~20 million steps against insertion sort's ~500 billion.

What it costs you: merging needs somewhere to put the output, so merge
sort uses O(n) extra memory. In exchange you get two guarantees quicksort
won't give you: the n log n bound holds for *every* input, and the sort is
**stable** (on ties, take from the left half first — equal elements keep
their order). That stability is why Python's built-in sort *is* a
merge-sort descendant: **Timsort** — merge sort fused with the
insertion-sort-on-small-runs trick you just learned. Every `sorted()` call
you've ever made was running this lesson's algorithm with fancier
bookkeeping; by the end of the lesson you'll have built its heart
yourself.

## Challenge: Merge Two Sorted Lists {#merge points=10}

Write the merge step on its own: given two sorted lists, return a new
sorted list containing every element of both. Walk a cursor down each
input, always taking the smaller front element; on ties take from `a`
first. Don't modify the inputs, and don't sort — the inputs are already
sorted and your job is O(m+n), so `sorted()` (O(n log n)) misses the
point.

### Starter

```python
def merge(a, b):
    """Return a new sorted list with every element of a and b.

    a and b are each already sorted ascending; neither is modified.
    """
    # TODO: two cursors; take the smaller front element; drain leftovers
    return []
```

### Tests

```python
from solution import merge


def test_merge():
    cases = [
        ("both empty", [], [], []),
        ("left empty", [], [1, 2], [1, 2]),
        ("right empty", [1, 2], [], [1, 2]),
        ("interleaved", [1, 4, 9], [2, 3, 8], [1, 2, 3, 4, 8, 9]),
        ("all left first", [1, 2], [3, 4], [1, 2, 3, 4]),
        ("all right first", [3, 4], [1, 2], [1, 2, 3, 4]),
        ("duplicates", [1, 1, 2], [1, 2, 2], [1, 1, 1, 2, 2, 2]),
        ("single each", [5], [-5], [-5, 5]),
    ]
    for name, a, b, want in cases:
        got = merge(a, b)
        assert got == want, f"{name}: merge({a}, {b}) = {got}, want {want}"


def test_merge_does_not_modify_inputs():
    a = [1, 3, 5]
    b = [2, 4, 6]
    merge(a, b)
    assert a == [1, 3, 5] and b == [2, 4, 6], (
        f"inputs were modified: a={a} b={b}"
    )
```

## Challenge: Merge Sort {#mergesort points=15}

Now the full algorithm: split in half, recurse on each half, merge. Return
a **new** list and leave the input untouched. Reuse the `merge` you just
wrote — paste it into the starter's `_merge` helper (each challenge is
graded as a standalone file). As ever, `sorted()` and `list.sort()` stay
in the toolbox.

### Starter

```python
def merge_sort(nums):
    """Return a new list with the elements of nums in ascending order.

    nums itself is not modified.
    """
    # TODO: base case len <= 1; recurse on halves; _merge the results
    return []


def _merge(a, b):
    # Bring your merge from the previous challenge:
    # TODO
    return []
```

### Tests

```python
from solution import merge_sort


def test_merge_sort():
    cases = [
        [],
        [1],
        [2, 1],
        [1, 2, 3, 4, 5],
        [5, 4, 3, 2, 1],
        [3, 1, 3, 1, 3],
        [0, -5, 3, -5, 2, 0],
    ]
    for nums in cases:
        want = sorted(nums)
        got = merge_sort(nums)
        assert got == want, f"merge_sort({nums}) = {got}, want {want}"


def test_merge_sort_leaves_input_alone():
    nums = [3, 1, 2]
    merge_sort(nums)
    assert nums == [3, 1, 2], f"input was modified: {nums}"


def test_merge_sort_big():
    seed = 7
    nums = []
    for _ in range(5000):
        seed = (seed * 1664525 + 1013904223) % 2**32
        nums.append(seed % 100000 - 50000)
    want = sorted(nums)
    got = merge_sort(nums)
    assert len(got) == len(want), f"big input: got len {len(got)}, want {len(want)}"
    for i, w in enumerate(want):
        assert got[i] == w, f"big input: mismatch at {i}: got {got[i]}, want {w}"
```

# Lesson: Quicksort and Partitioning {#partitioning}

Merge sort splits mechanically down the middle and does its real work
while combining. Quicksort inverts that: do the real work while
*splitting*, and combining becomes free.

## Partition

Pick an element — the **pivot** — and rearrange the array so everything
less than the pivot is to its left and everything greater is to its right.
The pivot is now in its final sorted position, and the two sides can be
sorted independently, *in place*: no merge, no second array.

The simplest correct scheme is **Lomuto's**: pivot on the last element,
sweep a boundary through the array, and swap small elements back behind
the boundary.

```
pivot = a[hi] = 4;  i = boundary of the "< pivot" zone

[ 7  2  9  4 ]           i=0    scan 7: 7 ≥ 4, leave it
[ 7  2  9  4 ]           i=0    scan 2: 2 < 4 → swap into zone
[ 2  7  9  4 ]           i=1    scan 9: 9 ≥ 4, leave it
[ 2  4  9  7 ]                  finally swap pivot to the boundary
     ^ pivot placed: [<4] 4 [≥4]
```

Click through Lomuto's sweep on `[3 8 2 5 1 4]` — violet is the pivot, green the growing ≤-pivot zone:

```d2
direction: right

arr: "" {
  grid-rows: 1
  grid-gap: 0
  c0: "3" { width: 64; height: 64 }
  c1: "8" { width: 64; height: 64 }
  c2: "2" { width: 64; height: 64 }
  c3: "5" { width: 64; height: 64 }
  c4: "1" { width: 64; height: 64 }
  c5: "4" { width: 64; height: 64 }
}

steps: {
  "pivot 4 — walk j left→right; i marks the ≤-edge": {
    arr.c5.style.stroke: "#7c3aed"
    arr.c5.style.stroke-width: 3
  }
  "j=0: 3 ≤ 4 — joins the ≤-side, i=1": {
    arr.c0.style.stroke: "#16a34a"
  }
  "j=1: 8 > 4 — stays put, no swap": {
    arr.c5.style.stroke: "#7c3aed"
  }
  "j=2: 2 ≤ 4 — swap 8↔2, i=2": {
    arr.c1.label: "2"
    arr.c1.style.stroke: "#16a34a"
    arr.c2.label: "8"
  }
  "j=3: 5 > 4 — no swap": {
    arr.c5.style.stroke: "#7c3aed"
  }
  "j=4: 1 ≤ 4 — swap 1↔8, i=3": {
    arr.c2.label: "1"
    arr.c2.style.stroke: "#16a34a"
    arr.c4.label: "8"
  }
  "pivot → slot i=3: left ≤ 4, right > 4 — 4 is HOME": {
    arr.c3.label: "4"
    arr.c3.style.stroke: "#16a34a"
    arr.c3.style.stroke-width: 3
    arr.c3.style.font-color: "#7c3aed"
    arr.c5.label: "5"
    arr.c5.style.stroke: "#dc2626"
    arr.c5.style.stroke-width: 2
  }
}
```

Then recurse on `[2]` and `[9 7]`. Each partition pass is O(n), and if the
pivot lands near the middle each time, you halve the problem log n times:
O(n log n), like merge sort, but with no extra array and a tight,
cache-friendly inner loop. That constant-factor edge is why "quick" stuck.

## The catch: pivots can betray you

Partition splits where the *pivot* lands, and nothing guarantees the
middle. Always pivoting on the last element of an **already-sorted** array
splits n into (n−1, 0) every round: O(n²), plus a recursion n levels deep.
Sorted input is the common case in real systems, so naive quicksort blows
up on exactly the data you'll actually see. Two standard defenses:

- **Median-of-three**: pivot on the median of the first, middle, and last
  elements. Sorted and reverse-sorted inputs now split perfectly.
- **Random pivot**: no fixed input pattern can reliably hit the worst case.

Duplicates are the other ambush: with Lomuto, an all-equal array puts
*every* element on one side of the pivot — n² again even with
median-of-three. **Hoare's scheme** (two cursors converging from both
ends, swapping out-of-place pairs) splits all-equal input roughly in half,
and is what serious implementations build on. The tests below include
sorted, reverse-sorted, and all-equal arrays of 2000 elements — make your
pivot earn its keep.

In Python the punishment for a lazy pivot is harsher than a slowdown, and
it is worth seeing the two numbers side by side. On those exact inputs, a
naive last-element pivot does 1,999,000 comparisons and recurses **1999
frames deep**; median-of-three with Hoare partitioning does about 24,000
comparisons and recurses **11 frames deep** (log₂ 2000 ≈ 11). CPython caps
recursion at 1000 frames by default — so the naive version doesn't merely
crawl, it dies with `RecursionError` before it ever pays the full O(n²)
cost. The good pivot sits comfortably inside the limit.

That crash is a feature, not an obstacle: it is the interpreter telling you
your splits are degenerate. Raising the ceiling with
`sys.setrecursionlimit` silences the messenger and leaves you with the
quadratic sort. Fix the pivot instead.

Quicksort's trades, laid against merge sort: in place and fast in
practice, but not stable, and n log n only *probabilistically*. Real
libraries hedge: Python dodged the gamble entirely by building its sort on
merge sort (Timsort), and the quicksort-based hybrids in other languages'
standard libraries detect bad splits and fall back to heapsort — which
happens to be your next lesson.

## Challenge: Quicksort {#quicksort points=15}

Sort a list in place with quicksort (mutate `nums`, return `None`):
partition, then recurse on both sides. Any correct partition scheme
passes, but the tests include already-sorted, reverse-sorted, and
all-equal inputs of 2000 elements each — on those, naive last-element
Lomuto recurses ~2000 frames deep and dies with `RecursionError`. That is
deliberate: the crash is the test's way of asking for a better pivot, not
for `sys.setrecursionlimit`. Median-of-three pivoting with Hoare
partitioning sails through all of them. No `sorted()` or `list.sort()`.

### Starter

```python
def quicksort(nums):
    """Sort nums in place, ascending. Returns None."""
    # TODO: partition around a well-chosen pivot, recurse on both sides
```

### Tests

```python
from solution import quicksort


def check_sorts(name, nums):
    want = sorted(nums)
    quicksort(nums)
    assert len(nums) == len(want), f"{name}: length changed to {len(nums)}"
    for i, w in enumerate(want):
        assert nums[i] == w, f"{name}: mismatch at {i}: got {nums[i]}, want {w}"


def test_quicksort_small():
    cases = [
        ("empty", []),
        ("single", [1]),
        ("two", [2, 1]),
        ("sorted", [1, 2, 3, 4, 5]),
        ("reverse", [5, 4, 3, 2, 1]),
        ("duplicates", [3, 1, 3, 1, 3]),
        ("negatives", [0, -5, 3, -5, 2, 0]),
    ]
    for name, nums in cases:
        check_sorts(name, list(nums))


def test_quicksort_adversarial():
    n = 2000
    check_sorts("already sorted", list(range(n)))
    check_sorts("reverse sorted", [n - i for i in range(n)])
    check_sorts("all equal", [7] * n)


def test_quicksort_big():
    seed = 99
    nums = []
    for _ in range(5000):
        seed = (seed * 1664525 + 1013904223) % 2**32
        nums.append(seed % 100000 - 50000)
    check_sorts("big random", nums)
```

# Lesson: Binary Heaps and Priority Queues {#binary-heaps}

Sorting answers "give me everything, in order." Many problems only ever
ask a smaller question, over and over: *what's the smallest thing right
now?* A task scheduler wants the next deadline; Dijkstra wants the closest
unvisited node; your OS wants the highest-priority process. That interface
— insert things, repeatedly remove the minimum — is a **priority queue**.

Your growable array gives two bad implementations: keep it unsorted
(insert O(1), find-min O(n)) or keep it sorted (find-min O(1), insert
O(n)). The binary heap gets both to O(log n) by keeping the array only
*loosely* ordered — just ordered enough.

## A tree flattened into your array

A **min-heap** is a complete binary tree with one rule — the **heap
property**: *every parent ≤ its children.* Nothing is promised about
siblings or cousins; the only guarantee is along root-to-leaf paths. That
single rule already pins the minimum to the root.

The elegant part: because the tree is *complete* (every level full, last
level filling left to right), it needs no pointers. Lay the levels out in
an array, top to bottom, left to right:

```
index:  0   1   2   3   4   5
value: [2 | 5 | 3 | 9 | 7 | 8]
```

```d2
direction: down
n0: "[0] 2"
n1: "[1] 5"
n2: "[2] 3"
n3: "[3] 9"
n4: "[4] 7"
n5: "[5] 8"
n0 -> n1
n0 -> n2
n1 -> n3
n1 -> n4
n2 -> n5
```

The tree structure is pure index arithmetic:

- children of `i`: `2*i + 1` and `2*i + 2`
- parent of `i`: `(i - 1) // 2` (floor division)

Your dynamic array *is* the heap; the tree is a way of reading it.

## Sift up, sift down

Both operations follow the same plan: break the heap property at one spot,
then repair it locally until it holds again.

**Push**: append the new value at the end (the only spot that keeps the
tree complete). It may now be smaller than its parent — **sift up**: while
it's smaller than its parent, swap with the parent. The path to the root
has log n nodes, so at most log n swaps.

Click through `push(1)` on a min-heap — the new value swaps upward until its parent is smaller:

```d2
steps: {
  "push(1) — new value goes in the next free slot": {
    n0: "2" { width: 64; height: 64 }
    n1: "4" { width: 64; height: 64 }
    n2: "3" { width: 64; height: 64 }
    n3: "8" { width: 64; height: 64 }
    n4: "7" { width: 64; height: 64 }
    n5: "9" { width: 64; height: 64 }
    n6: "" { width: 64; height: 64; style.stroke-dash: 4 }
    n0 -> n1
    n0 -> n2
    n1 -> n3
    n1 -> n4
    n2 -> n5
    n2 -> n6
  }
  "1 sits below its parent 3 — heap rule broken?": {
    n6.label: "1"
    n6.style.stroke-dash: 0
    n6.style.stroke: "#d97706"
    n6.style.stroke-width: 3
  }
  "1 < 3 → swap with parent; 3 settles below": {
    n2.label: "1"
    n2.style.stroke: "#d97706"
    n2.style.stroke-width: 3
    n6.label: "3"
    n6.style.stroke: "#16a34a"
    n6.style.stroke-width: 2
  }
  "1 < 2 → swap again; 1 reaches the root": {
    n0.label: "1"
    n0.style.stroke: "#d97706"
    n0.style.stroke-width: 3
    n2.label: "2"
    n2.style.stroke: "#16a34a"
    n2.style.stroke-width: 2
  }
  "root is the minimum — O(log n) swaps, one per level": {
    n0.style.stroke: "#16a34a"
    n0.style.stroke-width: 2
    n1.style.stroke: "#16a34a"
    n3.style.stroke: "#16a34a"
    n4.style.stroke: "#16a34a"
    n5.style.stroke: "#16a34a"
  }
}
```

**Pop**: the answer is the root, but removing the root would tear a hole
in the middle. Instead, overwrite the root with the *last* element, shrink
the array by one, and repair — **sift down**: while the moved value is
larger than its smallest child, swap with that child. The "smallest child"
detail matters: promote the smaller child and it's ≤ its sibling too, so
the property holds at that level. Again ≤ log n swaps.

## Heapsort: the free sort hiding inside

Flip the comparison to make a **max**-heap and a sort falls out:

1. **Heapify** the array in place: sift down every non-leaf, from the last
   one (`n // 2 - 1`) back to the root. Bottom-up, the two subtrees below
   you are always already heaps. (Nice fact: done in this order, heapify
   is O(n), not O(n log n) — most nodes sit near the bottom with short
   sifts.)
2. Repeatedly swap the max (root) with the last element, shrink the heap
   boundary by one, and sift the new root down.

In-place like quicksort, guaranteed n log n like merge sort — heapsort is
the "no nasty surprises" sort, which is exactly why hybrid sorts use it as
their safety net. Its price is cache-hostile jumping around the array, so
it's usually a little slower in practice than quicksort's tight sweeps.

## Challenge: A Min-Heap {#min-heap points=20}

Implement `MinHeap` backed by a plain Python list — and on the backing
list, use `append` and `pop` freely now: you've earned them, and you know
what they cost. What you may *not* use is `heapq`; that module exists
because somebody once wrote exactly this code, and today that somebody is
you. Keep the exact `self._data` attribute from the starter: the tests
verify the heap *invariant* — every parent ≤ its children — directly on
your list after every operation, not just that the right answers come out.

### Starter

```python
class MinHeap:
    """A binary min-heap of ints stored flat in a list."""

    def __init__(self):
        self._data = []  # the flat array; the tree is index arithmetic

    def __len__(self):
        # TODO: how many values are in the heap
        return 0

    def push(self, v):
        # TODO: append v, then sift it up while it beats its parent
        pass

    def peek(self):
        # TODO: return the smallest value (only called when non-empty)
        return 0

    def pop(self):
        # TODO: save the root, move the last element to the root, shrink,
        # sift down (only called when non-empty)
        return 0
```

### Tests

```python
from solution import MinHeap


def heap_ok(h):
    """Verify the heap property directly on the backing list."""
    d = h._data
    for i in range(1, len(d)):
        parent = (i - 1) // 2
        assert d[parent] <= d[i], (
            f"heap property violated: _data[{parent}]={d[parent]} > "
            f"_data[{i}]={d[i]} (_data={d})"
        )


def test_push_maintains_invariant():
    h = MinHeap()
    for v in [5, 3, 8, 1, 9, 2, 7, 1, 6, 4]:
        h.push(v)
        heap_ok(h)
    assert len(h) == 10, f"len = {len(h)}, want 10"
    assert h.peek() == 1, f"peek = {h.peek()}, want 1"


def test_pop_drains_sorted():
    vals = [5, 3, 8, 1, 9, 2, 7, 1, 6, 4]
    h = MinHeap()
    for v in vals:
        h.push(v)
    for i, want in enumerate(sorted(vals)):
        got = h.pop()
        heap_ok(h)
        assert got == want, f"pop #{i} = {got}, want {want}"
    assert len(h) == 0, f"len after draining = {len(h)}, want 0"


def test_interleaved():
    h = MinHeap()
    h.push(10)
    h.push(4)
    assert h.pop() == 4
    h.push(2)
    h.push(8)
    assert h.peek() == 2
    assert h.pop() == 2
    assert h.pop() == 8
    assert h.pop() == 10


def test_many_values():
    seed = 3
    h = MinHeap()
    vals = []
    for _ in range(2000):
        seed = (seed * 1664525 + 1013904223) % 2**32
        vals.append(seed % 1000)  # plenty of duplicates
        h.push(vals[-1])
    heap_ok(h)
    for i, want in enumerate(sorted(vals)):
        got = h.pop()
        assert got == want, f"pop #{i} = {got}, want {want}"
```

## Challenge: Heapsort {#heapsort points=15}

Sort a list in place (mutate `nums`, return `None`): heapify it into a
**max**-heap (sift down from the last parent back to the root), then
repeatedly swap the root to the end of the shrinking heap region and sift
the new root down. The sift-down you wrote for the min-heap is the same
routine with the comparison flipped and an explicit heap-size boundary —
the starter sketches it as `_sift_down(nums, i, size)`. No `sorted()`,
`list.sort()`, or `heapq`.

### Starter

```python
def heapsort(nums):
    """Sort nums in place, ascending, using an in-place max-heap."""
    # TODO: heapify (sift down from n // 2 - 1 down to 0), then
    # swap-and-sift the shrinking heap region


def _sift_down(nums, i, size):
    """Restore the max-heap property for the tree rooted at i,
    considering only nums[:size]."""
    # TODO
```

### Tests

```python
from solution import heapsort


def test_heapsort():
    cases = [
        ("empty", []),
        ("single", [1]),
        ("two", [2, 1]),
        ("sorted", [1, 2, 3, 4, 5]),
        ("reverse", [5, 4, 3, 2, 1]),
        ("duplicates", [3, 1, 3, 1, 3]),
        ("all equal", [7, 7, 7, 7]),
        ("negatives", [0, -5, 3, -5, 2, 0]),
    ]
    for name, nums in cases:
        want = sorted(nums)
        heapsort(nums)
        assert nums == want, f"{name}: got {nums}, want {want}"


def test_heapsort_big():
    seed = 11
    nums = []
    for _ in range(5000):
        seed = (seed * 1664525 + 1013904223) % 2**32
        nums.append(seed % 100000 - 50000)
    want = sorted(nums)
    heapsort(nums)
    for i, w in enumerate(want):
        assert nums[i] == w, f"big input: mismatch at {i}: got {nums[i]}, want {w}"
```

# Lesson: Hash Maps {#hash-maps}

Arrays answer "what's at index 7?" in O(1). The question real programs ask
is "what's the value for `"user:42"`?" — lookup by *name*, not position. A
hash map answers it in O(1) by manufacturing an index out of the name.

## From string to index

Step one is a **hash function**: mash the bytes of the key into one
integer, deterministically (same key → same number, every time) and
*scrambled* (similar keys → wildly different numbers — real keys come in
families like `user:41`, `user:42`, and if similar keys clustered, so
would your data). **FNV-1a** is the classic minimal hash that earns both
properties with four lines:

```python
def fnv1a(s):
    h = 2166136261
    for byte in s.encode("utf-8"):
        h ^= byte                        # inject the byte into the low bits
        h = (h * 16777619) & 0xffffffff  # multiply smears it across 32 bits
    return h
```

The XOR pokes a byte into the state; the multiply (by an odd, empirically
chosen prime) smears its influence across the whole word before the next
byte lands. One Python wrinkle: ints never overflow here, so the
`& 0xffffffff` mask does the wrap-to-32-bits that fixed-width integers do
for free in other languages — forget it and your hash balloons into a
bignum. The constants are standardized — every FNV-1a implementation on
earth agrees — which is exactly why the tests can check your hash against
published vectors. (If you want the full story of *why* these two
constants, this platform's Build a Hash Map course dissects them bit by
bit.)

Step two folds the hash into a bucket index: `fnv1a(key) % nbuckets`.

## Collisions are the design, not the exception

Four billion hashes squeezed into 8 buckets means different keys *will*
share a bucket. **Separate chaining** absorbs that: each bucket holds a
chain of entries, and lookup walks the short chain comparing actual keys.

Here, in a 4-bucket map, `"lex"` and `"emit"` both fold into bucket 2, so
that bucket holds both — `"lex"` was put first and `"emit"` appended after
it. `"ast"` sits alone in bucket 3, and an empty bucket is an empty list:

```
buckets
┌───┐
│ 0 │──▶ []
├───┤
│ 1 │──▶ []
├───┤
│ 2 │──▶ [ ["lex", 4], ["emit", 2] ]
├───┤
│ 3 │──▶ [ ["ast", 9] ]
└───┘
```

Those bucket numbers are the real ones, not a convenient fiction: once you
write `fnv1a` in the challenge below, `fnv1a("lex") % 4` and
`fnv1a("emit") % 4` will both hand you 2. Collisions like that one aren't
rare bad luck — with 4 buckets and 3 keys, they're the expected case.

A word on what a "chain" is here. The classic textbook (and C or Go)
implementation makes each bucket a *linked list*: every entry carries a
pointer to the next one. Python has no reason to hand-roll that — the
idiomatic chain is simply a **list of `[key, value]` pairs** per bucket,
which is what the diagram above shows and what you'll build. Same chaining
idea, same short linear walk on lookup, none of the pointer bookkeeping.

All three operations start the same way — hash, mod, walk the bucket:

- **Get**: return the pair's value if some pair's key matches.
- **Put**: if the key exists, overwrite its value (size unchanged!);
  otherwise append a new pair to the bucket.
- **Delete**: remove the matching pair from the bucket. (In the
  linked-node version this is a fiddly unlink that needs the *previous*
  node; a list bucket makes it gentler, but the other pairs must survive
  untouched.)

Click through `put("dot", 7)` landing on a shared bucket — scan every entry first (no match), then append:

```d2
direction: right

key: 'put("dot", 7)' {
  shape: oval
  style.stroke: "#d97706"
  style.stroke-width: 3
}

buckets: {
  shape: sql_table
  "0": "∅"
  "1": "∅"
  "2": "•"
  "3": "∅"
  "4": "∅"
  "5": "∅"
}

e1: '("ada", 1)' { width: 92; height: 64 }
e2: '("bob", 4)' { width: 92; height: 64 }
e3: "" {
  width: 92
  height: 64
  style.stroke-dash: 4
  style.stroke: "#9ca3af"
}
nil: "∅" { shape: text }

buckets."2" -> e1
e1 -> e2
e2 -> e3
e3 -> nil

steps: {
  'hash("dot") = 0x9c…4a — mod 6 → bucket 2': {
    (buckets."2" -> e1)[0].style.stroke: "#d97706"
    (buckets."2" -> e1)[0].style.stroke-width: 3
  }
  '"ada" ≠ "dot" — keep walking': {
    e1.style.stroke: "#d97706"
    e1.style.stroke-width: 3
  }
  '"bob" ≠ "dot" — keep walking': {
    e1.style.stroke: "#16a34a"
    e1.style.stroke-width: 2
    e2.style.stroke: "#d97706"
    e2.style.stroke-width: 3
  }
  'new entry chained at bucket 2 — len 3, load factor ↑': {
    e2.style.stroke: "#16a34a"
    e2.style.stroke-width: 2
    e3.label: '("dot", 7)'
    e3.style.stroke: "#16a34a"
    e3.style.stroke-width: 3
    e3.style.stroke-dash: 0
    key.style.stroke: "#16a34a"
    key.style.stroke-width: 2
  }
}
```

## Load factor: your amortized argument returns

Chains only stay short if there are enough buckets. The **load factor** —
entries ÷ buckets — measures crowding. Past a threshold (0.75 is the
classic), allocate double the buckets and **rehash**: every entry's home
is `hash % nbuckets`, and nbuckets just changed, so every entry must be
re-placed. A resize is O(n) — and it's fine, for precisely the reason
`push` was fine in lesson one: doubling makes resizes geometrically rarer,
so the cost amortizes to O(1) per insert. One structure's trick becomes
another structure's foundation.

## Challenge: A Chained Hash Map {#hashmap points=20}

Build the whole thing — and because you're building the machinery `dict`
provides, no `dict` (or `set`) anywhere inside your implementation: plain
lists only.

- A module-level `fnv1a(s)` with exactly that name — the tests check it
  against published vectors. Start from 2166136261; for each byte of
  `s.encode("utf-8")`: XOR the byte in, then multiply by 16777619 and mask
  with `& 0xffffffff`.
- Keep the starter's exact attributes: `self._buckets` is a list of
  buckets, each bucket a list of `[key, value]` pairs; `self._size` counts
  live entries. A new map has 8 buckets.
- `put(key, value)` inserts or overwrites; overwriting never changes size.
  When a **new** key would push size ÷ buckets over 0.75, first double the
  bucket count and rehash every entry, then insert.
- `get(key)` returns the value, or `None` if the key is absent.
- `delete(key)` removes the key, returning `True`/`False` for whether it
  was present.
- `__len__` is the number of live entries.

### Starter

```python
def fnv1a(s):
    """32-bit FNV-1a hash of the string s."""
    # TODO: offset basis 2166136261; per byte of s.encode("utf-8"):
    # XOR, then * 16777619, masked with & 0xffffffff
    return 0


class HashMap:
    """A separately chained hash map with power-of-two buckets.

    _buckets is a list of buckets; each bucket is a list of [key, value]
    pairs. _size counts live entries.
    """

    def __init__(self):
        self._buckets = [[] for _ in range(8)]
        self._size = 0

    def put(self, key, value):
        # TODO: overwrite in place if key exists (size unchanged); else
        # grow-and-rehash first if needed, then append [key, value]
        pass

    def get(self, key):
        # TODO: return the value for key, or None if absent
        return None

    def delete(self, key):
        # TODO: remove key, return True if it was present, else False
        return False

    def __len__(self):
        # TODO: the number of live entries
        return 0
```

### Tests

```python
from solution import HashMap, fnv1a


def test_fnv1a_vectors():
    vectors = [
        ("", 0x811C9DC5),
        ("a", 0xE40C292C),
        ("hello", 0x4F9F2CAB),
        ("user:42", 0x2F6B7B82),
    ]
    for s, want in vectors:
        got = fnv1a(s)
        assert got == want, f"fnv1a({s!r}) = {got:#x}, want {want:#x}"


def test_put_get():
    m = HashMap()
    assert m.get("missing") is None, "get on empty map reported a hit"
    m.put("a", 1)
    m.put("b", 2)
    assert m.get("a") == 1, f'get("a") = {m.get("a")}, want 1'
    assert m.get("b") == 2, f'get("b") = {m.get("b")}, want 2'
    assert len(m) == 2, f"len = {len(m)}, want 2"


def test_overwrite():
    m = HashMap()
    m.put("k", 1)
    m.put("k", 2)
    assert m.get("k") == 2, f'get("k") = {m.get("k")}, want 2'
    assert len(m) == 1, f"len after overwrite = {len(m)}, want 1"


def test_delete():
    m = HashMap()
    for i in range(20):
        m.put(f"key-{i}", i)
    assert m.delete("key-7") is True, "delete(key-7) = False, want True"
    assert m.delete("key-7") is False, "second delete(key-7) = True, want False"
    assert m.delete("never-existed") is False, (
        "delete(never-existed) = True, want False"
    )
    assert m.get("key-7") is None, "get(key-7) after delete reported a hit"
    assert len(m) == 19, f"len = {len(m)}, want 19"
    # Every other key must have survived the removal.
    for i in range(20):
        if i == 7:
            continue
        got = m.get(f"key-{i}")
        assert got == i, f"get(key-{i}) = {got}, want {i}"


def test_growth():
    m = HashMap()
    assert len(m._buckets) == 8, (
        f"new map has {len(m._buckets)} buckets, want 8"
    )
    n = 100
    for i in range(n):
        m.put(f"key-{i}", i * 10)
    assert len(m._buckets) > 8, (
        f"map never grew: still {len(m._buckets)} buckets after {n} inserts"
    )
    lf = m._size / len(m._buckets)
    assert lf <= 0.75, (
        f"load factor {lf:.2f} exceeds 0.75 "
        f"({m._size} entries, {len(m._buckets)} buckets)"
    )
    # Buckets must be consistent: total chained entries == size.
    total = sum(len(bucket) for bucket in m._buckets)
    assert total == m._size == n, (
        f"bucket total {total}, size {m._size}, want both {n}"
    )
    # And every key must still resolve after the rehashes.
    for i in range(n):
        got = m.get(f"key-{i}")
        assert got == i * 10, f"after growth get(key-{i}) = {got}, want {i * 10}"
```

# Lesson: Graphs and Breadth-First Search {#graphs-and-traversal}

Arrays, heaps, and maps organize *values*. Graphs organize
*relationships*: files import files, servers link to servers, tasks block
tasks. A graph is just vertices plus edges — and nearly every "how do
these things connect?" question reduces to a graph traversal you can now
build from parts you own.

## Representing a graph

Number the vertices 0..n−1 and pick a representation:

- **Adjacency matrix** — an n×n grid of booleans. O(1) edge checks, but
  n² memory even when almost no edges exist. Real graphs are usually
  sparse; the matrix is usually waste.
- **Adjacency list** — for each vertex, the list of its neighbors: a
  dynamic array of dynamic arrays. Memory proportional to what actually
  exists (n + edges). This is the default, and it's what you'll use.

An **undirected** edge u—v appears in both lists (u in v's, v in u's). A
**directed** edge u→v appears only in u's. This lesson's challenge is
undirected; Kahn's algorithm next lesson needs directed.

## BFS: exploring in rings

Breadth-first search explores a graph the way a ripple crosses a pond:
the start vertex, then everything 1 edge away, then everything 2 away.
The machinery is a **queue** (first-in, first-out) holding the *frontier*
— discovered vertices whose neighbors haven't been examined yet:

```
dist[src] = 0, queue = [src]
while the queue isn't empty:
    u = dequeue
    for each neighbor v of u:
        if v hasn't been discovered:
            dist[v] = dist[u] + 1
            enqueue v
```

Click through the ripple — amber is the ring just discovered, green is finished:

```d2
direction: right

A: "A" { width: 64; height: 64 }
B: "B" { width: 64; height: 64 }
C: "C" { width: 64; height: 64 }
D: "D" { width: 64; height: 64 }
E: "E" { width: 64; height: 64 }
F: "F" { width: 64; height: 64 }

A -- B
A -- C
B -- D
C -- D
C -- E
D -- F
E -- F

steps: {
  "BFS from A — queue: [A], dist(A)=0": {
    A.style.stroke: "#d97706"
    A.style.stroke-width: 3
  }
  "pop A, discover B and C — dist 1, queue: [B, C]": {
    A.style.stroke: "#16a34a"
    A.style.stroke-width: 2
    B.style.stroke: "#d97706"
    B.style.stroke-width: 3
    C.style.stroke: "#d97706"
    C.style.stroke-width: 3
  }
  "pop B then C — D and E at dist 2, queue: [D, E]": {
    B.style.stroke: "#16a34a"
    B.style.stroke-width: 2
    C.style.stroke: "#16a34a"
    C.style.stroke-width: 2
    D.style.stroke: "#d97706"
    D.style.stroke-width: 3
    E.style.stroke: "#d97706"
    E.style.stroke-width: 3
  }
  "pop D, E — F at dist 3, queue: [F]": {
    D.style.stroke: "#16a34a"
    D.style.stroke-width: 2
    E.style.stroke: "#16a34a"
    E.style.stroke-width: 2
    F.style.stroke: "#d97706"
    F.style.stroke-width: 3
  }
  "queue empty — every dist is the SHORTEST hop count": {
    F.style.stroke: "#16a34a"
    F.style.stroke-width: 2
  }
}
```

Two details carry the whole proof:

- **Mark on enqueue, not on dequeue.** A vertex enters the queue the
  first time it's *seen* and never again — that's what makes the
  algorithm O(vertices + edges) instead of looping forever on cycles.
  A `dist` list doubles as the marker: initialize to −1, and −1 means
  "not seen yet".
- **FIFO order is the shortest-path guarantee.** The queue always holds
  vertices in nondecreasing distance order — you completely finish ring k
  before touching ring k+1 — so the first time you reach a vertex is via
  a fewest-edges path. Swap the queue for a stack and you get depth-first
  search: still a traversal, but the shortest-path promise evaporates.
  (Swap it for a min-heap keyed on path length and you've invented
  Dijkstra — that's the exact upgrade path, and you own a heap.)

A queue is one growable array plus a head index that advances on dequeue
— O(1) amortized on both ends. You built the hard part in lesson one.
That's also all `collections.deque` is doing for you, plus some
block-allocation polish; the trap to avoid is `list.pop(0)`, which shifts
every remaining element and turns your O(V+E) traversal quadratic.

## Challenge: Shortest Paths by BFS {#bfs points=15}

Given `n` vertices, an undirected edge list, and a source, return a list
where entry i is the number of edges on the shortest path from `src` to
`i`, or −1 if `i` is unreachable. Each edge is a `(u, v)` pair connecting
u and v **both ways**, so build the adjacency list first (both directions
per edge!), then BFS with a queue, marking on enqueue.

For the queue, the honest build is a plain list plus a head index that
advances on dequeue — you built the growable array, and a deque is just
that plus amortized front removal. `collections.deque` is allowed if you'd
rather, but the point is knowing what it does; `list.pop(0)` in a loop is
the O(n²) trap either way.

### Starter

```python
def bfs_distances(n, edges, src):
    """Return the shortest-path edge count from src to every vertex of an
    undirected graph, -1 for unreachable vertices.

    Vertices are 0..n-1; each edge (u, v) connects u and v both ways.
    """
    # TODO: adjacency list, then queue-driven BFS; dist[src] = 0
    return []
```

### Tests

```python
from solution import bfs_distances


def check_dist(name, got, want):
    assert got == want, f"{name}: got {got}, want {want}"


def test_single_vertex():
    check_dist("single", bfs_distances(1, [], 0), [0])


def test_path_graph():
    # 0-1-2-3-4 in a line
    edges = [(0, 1), (1, 2), (2, 3), (3, 4)]
    check_dist("line from 0", bfs_distances(5, edges, 0), [0, 1, 2, 3, 4])
    check_dist("line from 2", bfs_distances(5, edges, 2), [2, 1, 0, 1, 2])


def test_star_graph():
    edges = [(0, 1), (0, 2), (0, 3)]
    check_dist("star center", bfs_distances(4, edges, 0), [0, 1, 1, 1])
    check_dist("star leaf", bfs_distances(4, edges, 3), [1, 2, 2, 0])


def test_disconnected():
    edges = [(0, 1), (2, 3)]
    check_dist("disconnected", bfs_distances(4, edges, 0), [0, 1, -1, -1])


def test_cycle_terminates():
    edges = [(0, 1), (1, 2), (2, 0)]
    check_dist("triangle", bfs_distances(3, edges, 0), [0, 1, 1])


def test_shortcut_wins():
    # Long way around (0-1-2-3) vs direct edge 0-3.
    edges = [(0, 1), (1, 2), (2, 3), (0, 3)]
    check_dist("shortcut", bfs_distances(4, edges, 0), [0, 1, 2, 1])


def test_duplicate_edges():
    edges = [(0, 1), (0, 1), (1, 0)]
    check_dist("dup edges", bfs_distances(2, edges, 0), [0, 1])
```

# Lesson: Kahn's Algorithm {#kahns-algorithm}

Some graphs don't just connect things — they *constrain* them. A build
system's "compile db before api", a course's "lesson 3 before lesson 4",
a spreadsheet's "cell B depends on cell A": all directed edges meaning
*this must come before that*. The question they share: **in what order can
I process everything without ever breaking a constraint?** That order is
a **topological order**, and Kahn's algorithm computes it with two things
you've already built.

## Indegree: counting what blocks you

A topological order can exist only in a **DAG** — a directed acyclic
graph. (If a→b→c→a, no valid order exists: something must come before
itself.) The key number is each vertex's **indegree**: how many edges
point *into* it — how many prerequisites it's still waiting on.

Here `proto` blocks `db` and `api`; `api` waits on two prerequisites and
blocks two dependents:

```d2
direction: right
proto: "proto\nin: 0"
db: "db\nin: 1"
api: "api\nin: 2"
web: "web\nin: 1"
cli: "cli\nin: 1"
proto -> db
proto -> api
db -> api
api -> web
api -> cli
```

A vertex with indegree 0 is ready *right now*. Kahn's insight: process a
ready vertex, and its outgoing edges are satisfied — so decrement each
successor's indegree, and any successor that hits 0 becomes ready in turn.

```
compute indegree of every vertex
ready = all vertices with indegree 0
while ready isn't empty:
    u = take from ready
    append u to the order
    for each successor v of u:
        indegree[v] -= 1
        if indegree[v] == 0: add v to ready
```

Click through a full run on the build graph above — amber means ready (indegree 0), and proto is the only vertex that starts that way:

```d2
direction: right

proto: "proto (0)" { width: 112; height: 64 }
db: "db (1)" { width: 112; height: 64 }
api: "api (2)" { width: 112; height: 64 }
web: "web (1)" { width: 112; height: 64 }
cli: "cli (1)" { width: 112; height: 64 }

proto -> db
proto -> api
db -> api
api -> web
api -> cli

proto.style.stroke: "#d97706"
proto.style.stroke-width: 3

steps: {
  "order: [proto] — db is freed, api still waits": {
    proto.label: "proto ✓1"
    proto.style.stroke: "#9ca3af"
    proto.style.stroke-width: 2
    proto.style.stroke-dash: 4
    proto.style.font-color: "#9ca3af"
    (proto -> db)[0].style.stroke-dash: 4
    (proto -> db)[0].style.stroke: "#9ca3af"
    (proto -> api)[0].style.stroke-dash: 4
    (proto -> api)[0].style.stroke: "#9ca3af"
    db.label: "db (0)"
    db.style.stroke: "#d97706"
    db.style.stroke-width: 3
    api.label: "api (1)"
  }
  "order: [proto db] — api finally hits 0": {
    db.label: "db ✓2"
    db.style.stroke: "#9ca3af"
    db.style.stroke-width: 2
    db.style.stroke-dash: 4
    db.style.font-color: "#9ca3af"
    (db -> api)[0].style.stroke-dash: 4
    (db -> api)[0].style.stroke: "#9ca3af"
    api.label: "api (0)"
    api.style.stroke: "#d97706"
    api.style.stroke-width: 3
  }
  "order: [proto db api] — web and cli BOTH ready": {
    api.label: "api ✓3"
    api.style.stroke: "#9ca3af"
    api.style.stroke-width: 2
    api.style.stroke-dash: 4
    api.style.font-color: "#9ca3af"
    (api -> web)[0].style.stroke-dash: 4
    (api -> web)[0].style.stroke: "#9ca3af"
    (api -> cli)[0].style.stroke-dash: 4
    (api -> cli)[0].style.stroke: "#9ca3af"
    web.label: "web (0)"
    web.style.stroke: "#d97706"
    web.style.stroke-width: 3
    cli.label: "cli (0)"
    cli.style.stroke: "#d97706"
    cli.style.stroke-width: 3
  }
  "order: [proto db api web] — either was valid": {
    web.label: "web ✓4"
    web.style.stroke: "#9ca3af"
    web.style.stroke-width: 2
    web.style.stroke-dash: 4
    web.style.font-color: "#9ca3af"
  }
  "order: [proto db api web cli] — edges forward": {
    cli.label: "cli ✓5"
    cli.style.stroke: "#9ca3af"
    cli.style.stroke-width: 2
    cli.style.stroke-dash: 4
    cli.style.font-color: "#9ca3af"
  }
}
```

Every vertex is processed once, every edge relaxed once: O(V + E).

## Cycle detection falls out for free

If the graph has a cycle, every vertex on it waits on another cycle
member — none ever reaches indegree 0, none is ever processed. So when
the loop ends, just count: **fewer processed vertices than the graph has
means a cycle** (and the leftovers are exactly the vertices stuck in or
behind one). No separate cycle-finding pass needed. This is how real
build tools, package managers, and module loaders report "circular
dependency detected".

## Choosing among the ready: your heap returns

When several vertices are ready at once, *any* choice yields a valid
topological order — the `ready` container can be a queue, a stack,
whatever. But "whatever" makes output nondeterministic, and real tools
(and graders) want reproducible orders. Make `ready` a **min-heap** keyed
however you like and you get the single best-of-the-valid-orders — e.g.
keyed on name, the alphabetically-earliest valid order, every run. This
is why the priority queue lesson came before this one: swap the container,
upgrade the algorithm.

# Final Challenge: The Build Scheduler {#task-scheduler points=50}

Time to cash in the whole course. You're given task names and dependency
pairs `(a, b)` meaning *a must run before b*. Produce a schedule — a
permutation of the tasks satisfying every constraint — or report that the
dependencies contain a cycle.

To make the answer unique (and gradable): **among all valid schedules,
return the lexicographically smallest** — at every step, of all currently
runnable tasks, run the alphabetically first. And the deterministic-order
requirement is enforced at a size where "collect ready tasks and re-sort
every round" will feel it.

Everything you've built has a seat:

- a **hash map** from task name to index (yours from the hash-maps lesson
  works verbatim — or Python's built-in `dict`; you've earned the right to
  import what you can build),
- **adjacency lists** of each task's dependents,
- an **indegree count** per task,
- a **min-heap of strings** as the ready set — your `MinHeap` works
  unchanged, because Python's `<` already compares strings
  alphabetically. No `heapq`, and no `graphlib` (yes, the standard
  library will topo-sort for you; it exists because somebody wrote what
  you're about to write),
- **Kahn's loop**, counting processed tasks to detect cycles.

Return the schedule as a list of task names on success, or `None` if the
dependencies are cyclic. Task names are unique; every name in `deps`
appears in `tasks`; duplicate dependency pairs may appear.

### Starter

```python
def schedule(tasks, deps):
    """Return the lexicographically smallest ordering of tasks in which
    every dependency pair (before, after) is respected, or None if the
    dependencies contain a cycle."""
    # TODO:
    #   1. map each task name to an index
    #   2. build adjacency lists and indegrees (before -> after)
    #   3. push every indegree-0 task name into a string min-heap
    #   4. Kahn's loop: pop smallest, append, decrement successors
    #   5. if the schedule is short, there was a cycle
    return None
```

### Tests

```python
from solution import schedule


def check_schedule(name, tasks, deps, want):
    got = schedule(tasks, deps)
    assert got is not None, f"{name}: schedule reported a cycle on an acyclic input"
    assert got == want, f"{name}: got {got}, want {want}"


def check_cycle(name, tasks, deps):
    got = schedule(tasks, deps)
    assert got is None, f"{name}: want cycle detection (None), got {got}"


def test_no_deps_is_alphabetical():
    check_schedule("no deps", ["c", "a", "b"], [], ["a", "b", "c"])


def test_chain():
    check_schedule(
        "chain",
        ["a", "b", "c"],
        [("c", "b"), ("b", "a")],
        ["c", "b", "a"],
    )


def test_heap_beats_queue():
    # Ready set starts {b, c}; finishing b unlocks a, which must run
    # before c alphabetically. A FIFO ready set wrongly yields b, c, a.
    check_schedule("heap order", ["a", "b", "c"], [("b", "a")], ["b", "a", "c"])


def test_diamond():
    check_schedule(
        "diamond",
        ["web", "db", "api", "proto", "cli"],
        [
            ("proto", "db"), ("proto", "api"), ("db", "api"),
            ("api", "web"), ("api", "cli"),
        ],
        ["proto", "db", "api", "cli", "web"],
    )


def test_duplicate_deps():
    check_schedule(
        "duplicate deps",
        ["a", "b"],
        [("b", "a"), ("b", "a")],
        ["b", "a"],
    )


def test_cycle():
    check_cycle(
        "triangle cycle",
        ["a", "b", "c"],
        [("a", "b"), ("b", "c"), ("c", "a")],
    )


def test_self_cycle():
    check_cycle("self dep", ["a", "b"], [("a", "a")])


def test_partial_cycle():
    # d is fine, but a<->b deadlock each other (and block c).
    check_cycle(
        "partial cycle",
        ["a", "b", "c", "d"],
        [("a", "b"), ("b", "a"), ("b", "c")],
    )


def test_big_schedule():
    # task-000 .. task-199 with task-i before task-i+1: fully forced.
    n = 200
    tasks = [f"task-{n - 1 - i:03d}" for i in range(n)]  # shuffled-ish order
    deps = [(f"task-{i:03d}", f"task-{i + 1:03d}") for i in range(n - 1)]
    want = [f"task-{i:03d}" for i in range(n)]
    check_schedule("big chain", tasks, deps, want)
```
