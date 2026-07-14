---
course: dsa-from-scratch
title: Data Structures & Algorithms from Scratch
language: go
description: >
  Every language hands you a toolbox — slices, maps, sort — and lets you
  treat the contents as magic. This course takes the toolbox away. You will
  build a growable array, the classic sorts, a binary heap, a hash map, and
  graph traversal with your own hands, and each structure you build becomes
  a tool you reuse as the problems get harder — until the finale, where your
  map, your heap, and Kahn's algorithm combine into a dependency-aware build
  scheduler.
duration_hours: 12
tags: [data-structures, algorithms, go]
extended_reading:
  - title: "Kahn (1962), Topological sorting of large networks — the original paper"
    url: https://dl.acm.org/doi/10.1145/368996.369025
  - title: "Sorting algorithm animations — watch the algorithms you built"
    url: https://www.toptal.com/developers/sorting-algorithms
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
behind the scenes — Go's `append`, Python's `list.append`, C++'s
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

`Push` normally just writes to slot `length` and increments it — O(1). The
interesting moment is `length == capacity`: the block is full, and blocks
can't grow in place (something else may live right after them in memory).
You must allocate a *bigger* block, copy everything across, and abandon the
old one.

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

Go's `append` does precisely this dance (doubling for small slices,
tapering to ~1.25× for large ones). For this challenge, `append` is off
the table — you're building it.

## Challenge: Growable Array {#growable-array points=10}

Implement `Array`, a growable array of ints. Do **not** use `append` — the
point is to do what it does yourself:

- The zero value `Array{}` is an empty, usable array (capacity 0).
- `Push` writes to the next free slot. When full, allocate a new backing
  slice of **double** the capacity (0 grows to 1), copy the old contents
  over with a loop or `copy`, then push.
- `Get(i)` returns the i'th element; callers only pass in-range indexes.

The tests inspect `Cap()` after every push, so the doubling schedule
(1, 2, 4, 8, …) is part of the contract.

### Starter

```go
package challenge

// Array is a growable array of ints. The zero value is empty and usable.
type Array struct {
	data   []int // backing storage; len(data) is the capacity
	length int   // slots in use
}

// Push appends v, doubling the backing array when it is full (0 -> 1).
func (a *Array) Push(v int) {
	// TODO: grow if a.length == len(a.data), then write and bump length
}

// Get returns the i'th element (0-indexed, always in range).
func (a *Array) Get(i int) int {
	// TODO
	return 0
}

// Len is the number of elements pushed.
func (a *Array) Len() int {
	// TODO
	return 0
}

// Cap is the size of the backing array.
func (a *Array) Cap() int {
	// TODO
	return 0
}
```

### Tests

```go
package challenge

import "testing"

func nextPow2(n int) int {
	c := 1
	for c < n {
		c *= 2
	}
	return c
}

func TestZeroValueIsEmpty(t *testing.T) {
	var a Array
	if a.Len() != 0 || a.Cap() != 0 {
		t.Fatalf("zero value: Len=%d Cap=%d, want 0 0", a.Len(), a.Cap())
	}
}

func TestPushAndGet(t *testing.T) {
	var a Array
	for i := 0; i < 100; i++ {
		a.Push(i * i)
	}
	if a.Len() != 100 {
		t.Fatalf("Len = %d, want 100", a.Len())
	}
	for i := 0; i < 100; i++ {
		if got := a.Get(i); got != i*i {
			t.Fatalf("Get(%d) = %d, want %d", i, got, i*i)
		}
	}
}

func TestDoublingSchedule(t *testing.T) {
	var a Array
	for i := 1; i <= 200; i++ {
		a.Push(i)
		want := nextPow2(i)
		if a.Cap() != want {
			t.Fatalf("after %d pushes: Cap = %d, want %d", i, a.Cap(), want)
		}
	}
}

func TestGrowthPreservesContents(t *testing.T) {
	var a Array
	// Push exactly past several capacity boundaries and re-check everything.
	for i := 0; i < 17; i++ {
		a.Push(1000 - i)
		for j := 0; j <= i; j++ {
			if a.Get(j) != 1000-j {
				t.Fatalf("after %d pushes, Get(%d) = %d, want %d",
					i+1, j, a.Get(j), 1000-j)
			}
		}
	}
}
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
  Go's — switch to insertion sort for small subarrays.

One more word you'll need later: a sort is **stable** if equal elements
keep their original relative order. Insertion sort is stable (it only
shifts *strictly larger* elements). Hold that thought for merge sort.

## Challenge: Insertion Sort {#insertion-sort points=10}

Sort a slice of ints in place using insertion sort: for each index i from
1 up, shift larger elements of the sorted prefix right and insert `a[i]`
where it belongs. No extra array, no library sort.

### Starter

```go
package challenge

// InsertionSort sorts nums in place, ascending.
func InsertionSort(nums []int) {
	// TODO: for each i, walk nums[i] left through the sorted prefix
}
```

### Tests

```go
package challenge

import (
	"sort"
	"testing"
)

func TestInsertionSort(t *testing.T) {
	cases := []struct {
		name string
		in   []int
	}{
		{"empty", []int{}},
		{"single", []int{42}},
		{"sorted", []int{1, 2, 3, 4, 5}},
		{"reverse", []int{5, 4, 3, 2, 1}},
		{"duplicates", []int{3, 1, 3, 1, 3}},
		{"negatives", []int{0, -5, 3, -5, 2, 0}},
		{"two", []int{2, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := make([]int, len(c.in))
			copy(got, c.in)
			want := make([]int, len(c.in))
			copy(want, c.in)
			sort.Ints(want)

			InsertionSort(got)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("InsertionSort(%v) = %v, want %v", c.in, got, want)
				}
			}
		})
	}
}

func TestInsertionSortBig(t *testing.T) {
	// Deterministic pseudo-random input via a small LCG.
	seed := uint32(1)
	in := make([]int, 1000)
	for i := range in {
		seed = seed*1664525 + 1013904223
		in[i] = int(seed % 10000)
	}
	want := make([]int, len(in))
	copy(want, in)
	sort.Ints(want)

	InsertionSort(in)
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("big input: mismatch at %d: got %d, want %d", i, in[i], want[i])
		}
	}
}
```

# Lesson: Divide and Conquer — Merge Sort {#divide-and-conquer}

Insertion sort does O(n²) work because each element learns only one thing
per comparison: "am I past my spot yet?" To sort faster, comparisons must
do more work — and the trick is to make sortedness *compose*.

## The key insight: merging is linear

If you have two *already sorted* arrays, combining them into one sorted
array is easy: look at the two front elements, take the smaller, repeat.
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

The only subtlety is the end: one side runs dry, and the other side's
remainder is already sorted — copy it straight across.

## Recursion does the rest

Merge sort is the one-line consequence: *split the array in half, sort
each half (recursively), merge.* An array of length 0 or 1 is already
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
their order). That stability is why Python's built-in sort and Java's
object sort are merge-sort descendants (Timsort: merge sort fused with
the insertion-sort-on-small-runs trick you just learned).

## Challenge: Merge Two Sorted Slices {#merge points=10}

Write the merge step on its own: given two sorted slices, produce a new
sorted slice containing every element of both. Walk a cursor down each
input, always taking the smaller front element; on ties take from `a`
first. Don't sort — the inputs are already sorted, and your job is O(m+n).

### Starter

```go
package challenge

// Merge returns a new sorted slice with every element of a and b.
// a and b are each already sorted ascending.
func Merge(a, b []int) []int {
	// TODO: two cursors; take the smaller front element; drain leftovers
	return nil
}
```

### Tests

```go
package challenge

import "testing"

func TestMerge(t *testing.T) {
	cases := []struct {
		name string
		a, b []int
		want []int
	}{
		{"both empty", nil, nil, []int{}},
		{"left empty", nil, []int{1, 2}, []int{1, 2}},
		{"right empty", []int{1, 2}, nil, []int{1, 2}},
		{"interleaved", []int{1, 4, 9}, []int{2, 3, 8}, []int{1, 2, 3, 4, 8, 9}},
		{"all left first", []int{1, 2}, []int{3, 4}, []int{1, 2, 3, 4}},
		{"all right first", []int{3, 4}, []int{1, 2}, []int{1, 2, 3, 4}},
		{"duplicates", []int{1, 1, 2}, []int{1, 2, 2}, []int{1, 1, 1, 2, 2, 2}},
		{"single each", []int{5}, []int{-5}, []int{-5, 5}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Merge(c.a, c.b)
			if len(got) != len(c.want) {
				t.Fatalf("Merge(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("Merge(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
				}
			}
		})
	}
}

func TestMergeDoesNotModifyInputs(t *testing.T) {
	a := []int{1, 3, 5}
	b := []int{2, 4, 6}
	Merge(a, b)
	if a[0] != 1 || a[1] != 3 || a[2] != 5 || b[0] != 2 || b[1] != 4 || b[2] != 6 {
		t.Fatalf("inputs were modified: a=%v b=%v", a, b)
	}
}
```

## Challenge: Merge Sort {#mergesort points=15}

Now the full algorithm: split in half, recurse on each half, merge. Return
a **new** slice and leave the input untouched. Reuse the `Merge` you just
wrote — paste it in below your `MergeSort` (each challenge is graded as a
standalone file).

### Starter

```go
package challenge

// MergeSort returns a new slice with the elements of nums in ascending
// order. nums itself is not modified.
func MergeSort(nums []int) []int {
	// TODO: base case len <= 1; recurse on halves; merge
	return nil
}

// Bring your Merge from the previous challenge:
func merge(a, b []int) []int {
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

func TestMergeSort(t *testing.T) {
	cases := [][]int{
		{},
		{1},
		{2, 1},
		{1, 2, 3, 4, 5},
		{5, 4, 3, 2, 1},
		{3, 1, 3, 1, 3},
		{0, -5, 3, -5, 2, 0},
	}
	for _, in := range cases {
		want := make([]int, len(in))
		copy(want, in)
		sort.Ints(want)

		got := MergeSort(in)
		if len(got) != len(want) {
			t.Fatalf("MergeSort(%v) = %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("MergeSort(%v) = %v, want %v", in, got, want)
			}
		}
	}
}

func TestMergeSortLeavesInputAlone(t *testing.T) {
	in := []int{3, 1, 2}
	MergeSort(in)
	if in[0] != 3 || in[1] != 1 || in[2] != 2 {
		t.Fatalf("input was modified: %v", in)
	}
}

func TestMergeSortBig(t *testing.T) {
	seed := uint32(7)
	in := make([]int, 5000)
	for i := range in {
		seed = seed*1664525 + 1013904223
		in[i] = int(seed%100000) - 50000
	}
	want := make([]int, len(in))
	copy(want, in)
	sort.Ints(want)

	got := MergeSort(in)
	if len(got) != len(want) {
		t.Fatalf("big input: got len %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("big input: mismatch at %d: got %d, want %d", i, got[i], want[i])
		}
	}
}
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
and is what serious implementations build on.

The tests below include sorted, reverse-sorted, and all-equal arrays of
2000 elements. Be clear about what they do and don't prove: at that size a
naive last-element pivot still *finishes* — 2 million comparisons is
nothing for a modern CPU — so it will pass. The tests check correctness;
whether your pivot is any good is a question you have to ask yourself.
Here is the honest scoreboard on those exact inputs, measured:

```
sorted, n=2000      naive Lomuto   1,999,000 comparisons, recursion depth 1999
                    med3 + Hoare      23,951 comparisons, recursion depth   11
all-equal, n=2000   naive Lomuto   1,999,000 comparisons, recursion depth 1999
                    med3 + Hoare      25,726 comparisons, recursion depth   11
```

Both columns are the same algorithm passing the same tests. One of them is
doing 83× the work at 180× the stack depth, and both of those multipliers
grow with n — which is how an O(n²) sort reaches production. Make your
pivot earn its keep.

Quicksort's trades, laid against merge sort: in place and fast in
practice, but not stable, and n log n only *probabilistically*. Real
libraries hedge: Go's `sort.Ints` uses a quicksort variant (pdqsort) that
detects bad splits and falls back to heapsort — which happens to be your
next lesson.

## Challenge: Quicksort {#quicksort points=15}

Sort a slice in place with quicksort: partition, then recurse on both
sides. Any correct partition scheme passes, but the tests include
already-sorted, reverse-sorted, and all-equal inputs of a few thousand
elements — median-of-three pivoting with Hoare partitioning sails through
all of them.

### Starter

```go
package challenge

// Quicksort sorts nums in place, ascending.
func Quicksort(nums []int) {
	// TODO: partition around a well-chosen pivot, recurse on both sides
}
```

### Tests

```go
package challenge

import (
	"sort"
	"testing"
)

func checkSorts(t *testing.T, name string, in []int) {
	t.Helper()
	want := make([]int, len(in))
	copy(want, in)
	sort.Ints(want)

	Quicksort(in)
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("%s: mismatch at %d: got %d, want %d", name, i, in[i], want[i])
		}
	}
}

func TestQuicksortSmall(t *testing.T) {
	cases := []struct {
		name string
		in   []int
	}{
		{"empty", []int{}},
		{"single", []int{1}},
		{"two", []int{2, 1}},
		{"sorted", []int{1, 2, 3, 4, 5}},
		{"reverse", []int{5, 4, 3, 2, 1}},
		{"duplicates", []int{3, 1, 3, 1, 3}},
		{"negatives", []int{0, -5, 3, -5, 2, 0}},
	}
	for _, c := range cases {
		in := make([]int, len(c.in))
		copy(in, c.in)
		checkSorts(t, c.name, in)
	}
}

func TestQuicksortAdversarial(t *testing.T) {
	n := 2000

	sorted := make([]int, n)
	for i := range sorted {
		sorted[i] = i
	}
	checkSorts(t, "already sorted", sorted)

	reverse := make([]int, n)
	for i := range reverse {
		reverse[i] = n - i
	}
	checkSorts(t, "reverse sorted", reverse)

	equal := make([]int, n)
	for i := range equal {
		equal[i] = 7
	}
	checkSorts(t, "all equal", equal)
}

func TestQuicksortBig(t *testing.T) {
	seed := uint32(99)
	in := make([]int, 5000)
	for i := range in {
		seed = seed*1664525 + 1013904223
		in[i] = int(seed%100000) - 50000
	}
	checkSorts(t, "big random", in)
}
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

- children of `i`: `2i + 1` and `2i + 2`
- parent of `i`: `(i - 1) / 2` (integer division)

Your dynamic array *is* the heap; the tree is a way of reading it.

## Sift up, sift down

Both operations follow the same plan: break the heap property at one spot,
then repair it locally until it holds again.

**Push**: append the new value at the end (the only spot that keeps the
tree complete). It may now be smaller than its parent — **sift up**: while
it's smaller than its parent, swap with the parent. The path to the root
has log n nodes, so at most log n swaps.

**Pop**: the answer is the root, but removing the root would tear a hole
in the middle. Instead, overwrite the root with the *last* element, shrink
the array by one, and repair — **sift down**: while the moved value is
larger than its smallest child, swap with that child. The "smallest child"
detail matters: promote the smaller child and it's ≤ its sibling too, so
the property holds at that level. Again ≤ log n swaps.

## Heapsort: the free sort hiding inside

Flip the comparison to make a **max**-heap and a sort falls out:

1. **Heapify** the array in place: sift down every non-leaf, from the last
   one (`n/2 - 1`) back to the root. Bottom-up, the two subtrees below you
   are always already heaps. (Nice fact: done in this order, heapify is
   O(n), not O(n log n) — most nodes sit near the bottom with short sifts.)
2. Repeatedly swap the max (root) with the last element, shrink the heap
   boundary by one, and sift the new root down.

In-place like quicksort, guaranteed n log n like merge sort — heapsort is
the "no nasty surprises" sort, which is exactly why hybrid sorts use it as
their safety net. Its price is cache-hostile jumping around the array, so
it's usually a little slower in practice than quicksort's tight sweeps.

## Challenge: A Min-Heap {#min-heap points=20}

Implement `MinHeap` backed by a plain int slice (use `append` freely now —
you've earned it, and you know what it costs). Keep the exact `data` field
from the starter: the tests verify the heap *invariant* — every parent ≤
its children — directly on your array after every operation, not just that
the right answers come out.

### Starter

```go
package challenge

// MinHeap is a binary min-heap of ints stored flat in a slice.
type MinHeap struct {
	data []int
}

// Len reports how many values are in the heap.
func (h *MinHeap) Len() int {
	// TODO
	return 0
}

// Push adds v: append it, then sift it up while it beats its parent.
func (h *MinHeap) Push(v int) {
	// TODO
}

// Peek returns the smallest value. Only called when Len() > 0.
func (h *MinHeap) Peek() int {
	// TODO
	return 0
}

// Pop removes and returns the smallest value: save the root, move the
// last element to the root, shrink, sift down. Only called when Len() > 0.
func (h *MinHeap) Pop() int {
	// TODO
	return 0
}
```

### Tests

```go
package challenge

import (
	"sort"
	"testing"
)

// heapOK verifies the heap property directly on the backing slice.
func heapOK(t *testing.T, h *MinHeap) {
	t.Helper()
	for i := 1; i < len(h.data); i++ {
		parent := (i - 1) / 2
		if h.data[parent] > h.data[i] {
			t.Fatalf("heap property violated: data[%d]=%d > data[%d]=%d (data=%v)",
				parent, h.data[parent], i, h.data[i], h.data)
		}
	}
}

func TestPushMaintainsInvariant(t *testing.T) {
	var h MinHeap
	for _, v := range []int{5, 3, 8, 1, 9, 2, 7, 1, 6, 4} {
		h.Push(v)
		heapOK(t, &h)
	}
	if h.Len() != 10 {
		t.Fatalf("Len = %d, want 10", h.Len())
	}
	if h.Peek() != 1 {
		t.Fatalf("Peek = %d, want 1", h.Peek())
	}
}

func TestPopDrainsSorted(t *testing.T) {
	in := []int{5, 3, 8, 1, 9, 2, 7, 1, 6, 4}
	var h MinHeap
	for _, v := range in {
		h.Push(v)
	}
	want := make([]int, len(in))
	copy(want, in)
	sort.Ints(want)

	for i, w := range want {
		got := h.Pop()
		heapOK(t, &h)
		if got != w {
			t.Fatalf("pop #%d = %d, want %d", i, got, w)
		}
	}
	if h.Len() != 0 {
		t.Fatalf("Len after draining = %d, want 0", h.Len())
	}
}

func TestInterleaved(t *testing.T) {
	var h MinHeap
	h.Push(10)
	h.Push(4)
	if got := h.Pop(); got != 4 {
		t.Fatalf("Pop = %d, want 4", got)
	}
	h.Push(2)
	h.Push(8)
	if got := h.Peek(); got != 2 {
		t.Fatalf("Peek = %d, want 2", got)
	}
	if got := h.Pop(); got != 2 {
		t.Fatalf("Pop = %d, want 2", got)
	}
	if got := h.Pop(); got != 8 {
		t.Fatalf("Pop = %d, want 8", got)
	}
	if got := h.Pop(); got != 10 {
		t.Fatalf("Pop = %d, want 10", got)
	}
}

func TestManyValues(t *testing.T) {
	seed := uint32(3)
	var h MinHeap
	n := 2000
	vals := make([]int, n)
	for i := range vals {
		seed = seed*1664525 + 1013904223
		vals[i] = int(seed % 1000) // plenty of duplicates
		h.Push(vals[i])
	}
	heapOK(t, &h)
	sort.Ints(vals)
	for i, w := range vals {
		if got := h.Pop(); got != w {
			t.Fatalf("pop #%d = %d, want %d", i, got, w)
		}
	}
}
```

## Challenge: Heapsort {#heapsort points=15}

Sort a slice in place: heapify it into a **max**-heap (sift down from the
last parent back to the root), then repeatedly swap the root to the end of
the shrinking heap region and sift the new root down. The sift-down you
wrote for the min-heap is the same routine with the comparison flipped and
an explicit heap-size boundary.

### Starter

```go
package challenge

// Heapsort sorts nums in place, ascending, using an in-place max-heap.
func Heapsort(nums []int) {
	// TODO: heapify (sift down from n/2-1 to 0), then swap-and-sift
}

// siftDown restores the max-heap property for the tree rooted at i,
// considering only nums[:size].
func siftDown(nums []int, i, size int) {
	// TODO
}
```

### Tests

```go
package challenge

import (
	"sort"
	"testing"
)

func TestHeapsort(t *testing.T) {
	cases := []struct {
		name string
		in   []int
	}{
		{"empty", []int{}},
		{"single", []int{1}},
		{"two", []int{2, 1}},
		{"sorted", []int{1, 2, 3, 4, 5}},
		{"reverse", []int{5, 4, 3, 2, 1}},
		{"duplicates", []int{3, 1, 3, 1, 3}},
		{"all equal", []int{7, 7, 7, 7}},
		{"negatives", []int{0, -5, 3, -5, 2, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := make([]int, len(c.in))
			copy(in, c.in)
			want := make([]int, len(c.in))
			copy(want, c.in)
			sort.Ints(want)

			Heapsort(in)
			for i := range want {
				if in[i] != want[i] {
					t.Fatalf("Heapsort(%v) = %v, want %v", c.in, in, want)
				}
			}
		})
	}
}

func TestHeapsortBig(t *testing.T) {
	seed := uint32(11)
	in := make([]int, 5000)
	for i := range in {
		seed = seed*1664525 + 1013904223
		in[i] = int(seed%100000) - 50000
	}
	want := make([]int, len(in))
	copy(want, in)
	sort.Ints(want)

	Heapsort(in)
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("big input: mismatch at %d: got %d, want %d", i, in[i], want[i])
		}
	}
}
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

```go
func fnv1a(s string) uint32 {
	hash := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		hash ^= uint32(s[i]) // inject the byte into the low bits
		hash *= 16777619     // multiply smears it across all 32 bits
	}
	return hash
}
```

The XOR pokes a byte into the state; the multiply (by an odd, empirically
chosen prime) smears its influence across the whole word before the next
byte lands. The constants are standardized — every FNV-1a implementation
on earth agrees — which is exactly why the tests can check your hash
against published vectors. (If you want the full story of *why* these two
constants, this platform's Build a Hash Map course dissects them bit by
bit.)

Step two folds the hash into a bucket index: `fnv1a(key) % nbuckets`.

## Collisions are the design, not the exception

Four billion hashes squeezed into 8 buckets means different keys *will*
share a bucket. **Separate chaining** absorbs that: each bucket holds the
head of a linked list of entries, and lookup walks the short chain
comparing actual keys.

Here, in a 4-bucket map, `"lex"` and `"emit"` both fold into bucket 2, so
that chain holds both — `"emit"` went in last, and a prepend leaves it at
the head. `"ast"` sits alone in bucket 3, and empty buckets are just nil:

```
buckets
┌───┐
│ 0 │──▶ ∅
├───┤
│ 1 │──▶ ∅
├───┤
│ 2 │──▶ ["emit" = 2] ──▶ ["lex" = 4] ──▶ ∅
├───┤
│ 3 │──▶ ["ast" = 9] ──▶ ∅
└───┘
```

Those bucket numbers are the real ones, not a convenient fiction: once you
write `fnv1a` in the challenge below, `fnv1a("lex") % 4` and
`fnv1a("emit") % 4` will both hand you 2. Collisions like that one aren't
rare bad luck — with 4 buckets and 3 keys, they're the expected case.

All three operations start the same way — hash, mod, walk the chain:

- **Get**: return the entry's value if some chain node's key matches.
- **Put**: if the key exists, overwrite its value (size unchanged!);
  otherwise prepend a new entry to the chain.
- **Delete**: unlink the matching node. Unlinking needs the *previous*
  node — track it as you walk, and mind the head-of-chain case.

## Load factor: your amortized argument returns

Chains only stay short if there are enough buckets. The **load factor** —
entries ÷ buckets — measures crowding. Past a threshold (0.75 is the
classic), allocate double the buckets and **rehash**: every entry's home
is `hash % nbuckets`, and nbuckets just changed, so every entry must be
re-placed. A resize is O(n) — and it's fine, for precisely the reason
`Push` was fine in lesson one: doubling makes resizes geometrically rarer,
so the cost amortizes to O(1) per insert. One structure's trick becomes
another structure's foundation.

## Challenge: A Chained Hash Map {#hashmap points=20}

Build the whole thing: FNV-1a (keep the exact `fnv1a` name — the tests
check it against published vectors), separate chaining with the starter's
`entry` struct, `Put`/`Get`/`Delete`/`Len`, and growth: when a `Put` of a
**new** key would push the load factor over 0.75, double the bucket count
and rehash every entry before inserting.

### Starter

```go
package challenge

// entry is one key/value pair in a bucket's chain.
type entry struct {
	key   string
	value int
	next  *entry
}

// HashMap is a separately-chained hash map with power-of-two buckets.
type HashMap struct {
	buckets []*entry
	size    int
}

// NewHashMap returns an empty map with 8 buckets.
func NewHashMap() *HashMap {
	// TODO
	return nil
}

// fnv1a is the 32-bit FNV-1a hash of s.
func fnv1a(s string) uint32 {
	// TODO: offset basis 2166136261; per byte: XOR, then * 16777619
	return 0
}

// Put inserts or overwrites key. Overwriting never changes size. If
// inserting a NEW key would make size/buckets exceed 0.75, first double
// the bucket count and rehash everything.
func (m *HashMap) Put(key string, value int) {
	// TODO
}

// Get returns the value for key and whether it was present.
func (m *HashMap) Get(key string) (int, bool) {
	// TODO
	return 0, false
}

// Delete removes key, reporting whether it was present.
func (m *HashMap) Delete(key string) bool {
	// TODO
	return false
}

// Len is the number of live entries.
func (m *HashMap) Len() int {
	// TODO
	return 0
}
```

### Tests

```go
package challenge

import (
	"fmt"
	"testing"
)

func TestFNV1aVectors(t *testing.T) {
	vectors := []struct {
		in   string
		want uint32
	}{
		{"", 0x811c9dc5},
		{"a", 0xe40c292c},
		{"hello", 0x4f9f2cab},
		{"user:42", 0x2f6b7b82},
	}
	for _, v := range vectors {
		if got := fnv1a(v.in); got != v.want {
			t.Fatalf("fnv1a(%q) = %#x, want %#x", v.in, got, v.want)
		}
	}
}

func TestPutGet(t *testing.T) {
	m := NewHashMap()
	if _, ok := m.Get("missing"); ok {
		t.Fatal("Get on empty map reported a hit")
	}
	m.Put("a", 1)
	m.Put("b", 2)
	if v, ok := m.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %d,%v want 1,true", v, ok)
	}
	if v, ok := m.Get("b"); !ok || v != 2 {
		t.Fatalf("Get(b) = %d,%v want 2,true", v, ok)
	}
	if m.Len() != 2 {
		t.Fatalf("Len = %d, want 2", m.Len())
	}
}

func TestOverwrite(t *testing.T) {
	m := NewHashMap()
	m.Put("k", 1)
	m.Put("k", 2)
	if v, _ := m.Get("k"); v != 2 {
		t.Fatalf("Get(k) = %d, want 2", v)
	}
	if m.Len() != 1 {
		t.Fatalf("Len after overwrite = %d, want 1", m.Len())
	}
}

func TestDelete(t *testing.T) {
	m := NewHashMap()
	for i := 0; i < 20; i++ {
		m.Put(fmt.Sprintf("key-%d", i), i)
	}
	if !m.Delete("key-7") {
		t.Fatal("Delete(key-7) = false, want true")
	}
	if m.Delete("key-7") {
		t.Fatal("second Delete(key-7) = true, want false")
	}
	if m.Delete("never-existed") {
		t.Fatal("Delete(never-existed) = true, want false")
	}
	if _, ok := m.Get("key-7"); ok {
		t.Fatal("Get(key-7) after delete reported a hit")
	}
	if m.Len() != 19 {
		t.Fatalf("Len = %d, want 19", m.Len())
	}
	// Every other key must have survived the unlink.
	for i := 0; i < 20; i++ {
		if i == 7 {
			continue
		}
		key := fmt.Sprintf("key-%d", i)
		if v, ok := m.Get(key); !ok || v != i {
			t.Fatalf("Get(%s) = %d,%v want %d,true", key, v, ok, i)
		}
	}
}

func TestGrowth(t *testing.T) {
	m := NewHashMap()
	if len(m.buckets) != 8 {
		t.Fatalf("new map has %d buckets, want 8", len(m.buckets))
	}
	const n = 100
	for i := 0; i < n; i++ {
		m.Put(fmt.Sprintf("key-%d", i), i*10)
	}
	if len(m.buckets) <= 8 {
		t.Fatalf("map never grew: still %d buckets after %d inserts", len(m.buckets), n)
	}
	if lf := float64(m.size) / float64(len(m.buckets)); lf > 0.75 {
		t.Fatalf("load factor %.2f exceeds 0.75 (%d entries, %d buckets)",
			lf, m.size, len(m.buckets))
	}
	// Chains must be consistent: total chained entries == size.
	total := 0
	for _, e := range m.buckets {
		for ; e != nil; e = e.next {
			total++
		}
	}
	if total != m.size || m.size != n {
		t.Fatalf("chain total %d, size %d, want both %d", total, m.size, n)
	}
	// And every key must still resolve after the rehashes.
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%d", i)
		if v, ok := m.Get(key); !ok || v != i*10 {
			t.Fatalf("after growth Get(%s) = %d,%v want %d,true", key, v, ok, i*10)
		}
	}
}
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

Two details carry the whole proof:

- **Mark on enqueue, not on dequeue.** A vertex enters the queue the
  first time it's *seen* and never again — that's what makes the
  algorithm O(vertices + edges) instead of looping forever on cycles.
  A `dist` array doubles as the marker: initialize to −1, and −1 means
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

## Challenge: Shortest Paths by BFS {#bfs points=15}

Given `n` vertices, an undirected edge list, and a source, return a slice
where entry i is the number of edges on the shortest path from `src` to
`i`, or −1 if `i` is unreachable. Build the adjacency list first (both
directions per edge!), then BFS with a queue, marking on enqueue.

### Starter

```go
package challenge

// BFSDistances returns the shortest-path edge count from src to every
// vertex of an undirected graph, -1 for unreachable vertices.
// Vertices are 0..n-1; each edge {u, v} connects u and v both ways.
func BFSDistances(n int, edges [][2]int, src int) []int {
	// TODO: adjacency list, then queue-driven BFS; dist[src] = 0
	return nil
}
```

### Tests

```go
package challenge

import "testing"

func checkDist(t *testing.T, name string, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
}

func TestSingleVertex(t *testing.T) {
	checkDist(t, "single", BFSDistances(1, nil, 0), []int{0})
}

func TestPathGraph(t *testing.T) {
	// 0-1-2-3-4 in a line
	edges := [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}}
	checkDist(t, "line from 0", BFSDistances(5, edges, 0), []int{0, 1, 2, 3, 4})
	checkDist(t, "line from 2", BFSDistances(5, edges, 2), []int{2, 1, 0, 1, 2})
}

func TestStarGraph(t *testing.T) {
	edges := [][2]int{{0, 1}, {0, 2}, {0, 3}}
	checkDist(t, "star center", BFSDistances(4, edges, 0), []int{0, 1, 1, 1})
	checkDist(t, "star leaf", BFSDistances(4, edges, 3), []int{1, 2, 2, 0})
}

func TestDisconnected(t *testing.T) {
	edges := [][2]int{{0, 1}, {2, 3}}
	checkDist(t, "disconnected", BFSDistances(4, edges, 0), []int{0, 1, -1, -1})
}

func TestCycleTerminates(t *testing.T) {
	edges := [][2]int{{0, 1}, {1, 2}, {2, 0}}
	checkDist(t, "triangle", BFSDistances(3, edges, 0), []int{0, 1, 1})
}

func TestShortcutWins(t *testing.T) {
	// Long way around (0-1-2-3) vs direct edge 0-3.
	edges := [][2]int{{0, 1}, {1, 2}, {2, 3}, {0, 3}}
	checkDist(t, "shortcut", BFSDistances(4, edges, 0), []int{0, 1, 2, 1})
}

func TestDuplicateEdges(t *testing.T) {
	edges := [][2]int{{0, 1}, {0, 1}, {1, 0}}
	checkDist(t, "dup edges", BFSDistances(2, edges, 0), []int{0, 1})
}
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
pairs `{a, b}` meaning *a must run before b*. Produce a schedule — a
permutation of the tasks satisfying every constraint — or report that the
dependencies contain a cycle.

To make the answer unique (and gradable): **among all valid schedules,
return the lexicographically smallest** — at every step, of all currently
runnable tasks, run the alphabetically first. And the deterministic-order
requirement is enforced at a size where "collect ready tasks and re-sort
every round" will feel it.

Everything you've built has a seat:

- a **hash map** from task name to index (yours from the hash-maps
  lesson works verbatim — or Go's built-in `map`; you've earned the
  right to import what you can build),
- **adjacency lists** of each task's dependents,
- an **indegree count** per task,
- a **min-heap of strings** as the ready set — your `MinHeap` with `int`
  swapped for `string` (strings compare with `<` in Go),
- **Kahn's loop**, counting processed tasks to detect cycles.

Return `(schedule, true)` on success, `(nil, false)` if the dependencies
are cyclic. Task names are unique; every name in `deps` appears in
`tasks`; duplicate dependency pairs may appear.

### Starter

```go
package challenge

// Schedule returns the lexicographically smallest ordering of tasks in
// which every dependency pair {before, after} is respected, and true.
// If the dependencies contain a cycle it returns nil, false.
func Schedule(tasks []string, deps [][2]string) ([]string, bool) {
	// TODO:
	//   1. map each task name to an index
	//   2. build adjacency lists and indegrees (deps[i][0] -> deps[i][1])
	//   3. push every indegree-0 task name into a string min-heap
	//   4. Kahn's loop: pop smallest, append, decrement successors
	//   5. if the schedule is short, there was a cycle
	return nil, false
}
```

### Tests

```go
package challenge

import (
	"fmt"
	"testing"
)

func checkSchedule(t *testing.T, name string, tasks []string, deps [][2]string, want []string) {
	t.Helper()
	got, ok := Schedule(tasks, deps)
	if !ok {
		t.Fatalf("%s: Schedule reported a cycle on an acyclic input", name)
	}
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
}

func checkCycle(t *testing.T, name string, tasks []string, deps [][2]string) {
	t.Helper()
	if got, ok := Schedule(tasks, deps); ok {
		t.Fatalf("%s: want cycle detection, got schedule %v", name, got)
	}
}

func TestNoDepsIsAlphabetical(t *testing.T) {
	checkSchedule(t, "no deps",
		[]string{"c", "a", "b"}, nil,
		[]string{"a", "b", "c"})
}

func TestChain(t *testing.T) {
	checkSchedule(t, "chain",
		[]string{"a", "b", "c"},
		[][2]string{{"c", "b"}, {"b", "a"}},
		[]string{"c", "b", "a"})
}

func TestHeapBeatsQueue(t *testing.T) {
	// Ready set starts {b, c}; finishing b unlocks a, which must run
	// before c alphabetically. A FIFO ready set wrongly yields b, c, a.
	checkSchedule(t, "heap order",
		[]string{"a", "b", "c"},
		[][2]string{{"b", "a"}},
		[]string{"b", "a", "c"})
}

func TestDiamond(t *testing.T) {
	checkSchedule(t, "diamond",
		[]string{"web", "db", "api", "proto", "cli"},
		[][2]string{
			{"proto", "db"}, {"proto", "api"}, {"db", "api"},
			{"api", "web"}, {"api", "cli"},
		},
		[]string{"proto", "db", "api", "cli", "web"})
}

func TestDuplicateDeps(t *testing.T) {
	checkSchedule(t, "duplicate deps",
		[]string{"a", "b"},
		[][2]string{{"b", "a"}, {"b", "a"}},
		[]string{"b", "a"})
}

func TestCycle(t *testing.T) {
	checkCycle(t, "triangle cycle",
		[]string{"a", "b", "c"},
		[][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}})
}

func TestSelfCycle(t *testing.T) {
	checkCycle(t, "self dep",
		[]string{"a", "b"},
		[][2]string{{"a", "a"}})
}

func TestPartialCycle(t *testing.T) {
	// d is fine, but a<->b deadlock each other (and block c).
	checkCycle(t, "partial cycle",
		[]string{"a", "b", "c", "d"},
		[][2]string{{"a", "b"}, {"b", "a"}, {"b", "c"}})
}

func TestBigSchedule(t *testing.T) {
	// task-000 .. task-199 with task-i before task-i+1: fully forced.
	n := 200
	tasks := make([]string, n)
	for i := range tasks {
		tasks[i] = fmt.Sprintf("task-%03d", n-1-i) // shuffled-ish input order
	}
	var deps [][2]string
	for i := 0; i < n-1; i++ {
		deps = append(deps, [2]string{
			fmt.Sprintf("task-%03d", i), fmt.Sprintf("task-%03d", i+1),
		})
	}
	want := make([]string, n)
	for i := range want {
		want[i] = fmt.Sprintf("task-%03d", i)
	}
	checkSchedule(t, "big chain", tasks, deps, want)
}
```
