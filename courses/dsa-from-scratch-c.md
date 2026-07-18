---
course: dsa-from-scratch
title: Data Structures & Algorithms from Scratch
language: c
description: >
  C hands you malloc, a block of memory, and nothing else — no list, no
  dictionary, no sort you didn't write. That is the best possible place to
  learn what those things actually are. Build a growable array, the classic
  sorts, a binary heap, a chained hash map, and graph traversal with your
  own hands, and each structure becomes a tool you reuse as the problems
  get harder — until the finale, where your map, your heap, and Kahn's
  algorithm combine into a dependency-aware build scheduler.
duration_hours: 12
tags: [data-structures, algorithms, c, memory]
extended_reading:
  - title: "Kahn (1962), Topological sorting of large networks — the original paper"
    url: https://dl.acm.org/doi/10.1145/368996.369025
  - title: "Sorting algorithm animations — watch the algorithms you built"
    url: https://www.toptal.com/developers/sorting-algorithms
  - title: "The Algorithm Design Manual (Skiena) — where to go next"
    url: https://www.algorist.com/
  - title: "qsort(3) — the sort C actually ships with"
    url: https://man7.org/linux/man-pages/man3/qsort.3.html
---

# Lesson: The Dynamic Array {#dynamic-array}

Everything in this course is built on one humble object: a block of memory
you can index in O(1). An array is fast for exactly one reason — element
`i` lives at `start + i * sizeof(int)`, so reading `a[i]` is one multiply,
one add, one load, no matter how big the array is. In C that's not an
abstraction over the hardware; it *is* the hardware, and `a[i]` is
literally defined as `*(a + i)`.

The catch is that a block of memory has a fixed size. `malloc(n * sizeof
(int))` gives you exactly n ints and not one more. Real programs don't know
their sizes up front, so every language grows arrays the same way behind
the scenes — C++'s `vector::push_back`, Go's `append`, Python's
`list.append` are all the same data structure, and in C you are the one who
has to build it.

## Length is not capacity

A growable array tracks two numbers:

```
data:   [ 7 | 3 | 9 | 5 | . | . | . | . ]
          <---- len=4 ------>
          <---------- cap=8 ------------>
```

- **len** — how many slots hold real values,
- **cap** — how many slots the malloc'd block actually holds.

`array_push` normally just writes to slot `len` and increments it — O(1).
The interesting moment is `len == cap`: the block is full, and a malloc'd
block cannot grow in place (the heap almost certainly put something else
right after it). You must allocate a *bigger* block, copy everything
across, and `free` the old one.

`realloc` will do that dance for you — and it's the right tool in real code
— but it hides exactly the thing you're here to see. Build it by hand once
(`malloc` the new block, copy, `free` the old), and `realloc` stops being
magic: it's this function, with an optimization where it can extend in
place if the heap happens to have room.

Watch the moment the block runs out of room — amber is the incoming value, dashed grey is freed memory:

```d2
direction: right

code: "" {
  grid-columns: 1
  grid-gap: 0
  l1: "append(x):" {
    height: 30
    style.stroke: "#d97706"
    style.stroke-width: 2
    style.font: mono
    style.bold: true
  }
  l2: "  if len == cap:" {
    height: 30
    style.stroke: "#9ca3af"
    style.font: mono
    style.bold: false
  }
  l3: "    grow: new block, 2×cap" {
    height: 30
    style.stroke: "#9ca3af"
    style.font: mono
    style.bold: false
  }
  l4: "    copy elements, free old" {
    height: 30
    style.stroke: "#9ca3af"
    style.font: mono
    style.bold: false
  }
  l5: "  a[len] = x; len += 1" {
    height: 30
    style.stroke: "#9ca3af"
    style.font: mono
    style.bold: false
  }
}

heap: "" {
  grid-columns: 1
  grid-gap: 12

  r0: "" {
    grid-rows: 1
    grid-gap: 0
    style.opacity: 0
    x: "9" {
      shape: circle
      width: 64
      height: 64
      style.opacity: 1
      style.stroke: "#d97706"
      style.stroke-width: 3
    }
    pad: "" { width: 448; height: 64; style.opacity: 0 }
  }

  r1: "" {
    grid-rows: 1
    grid-gap: 0
    style.opacity: 0
    old: "cap 4" {
      grid-rows: 1
      grid-gap: 0
      style.opacity: 1
      c0: "5" { width: 64; height: 64; style.opacity: 1 }
      c1: "8" { width: 64; height: 64; style.opacity: 1 }
      c2: "3" { width: 64; height: 64; style.opacity: 1 }
      c3: "" { width: 64; height: 64; style.opacity: 1; style.stroke-dash: 4 }
    }
    pad: "" { width: 256; height: 64; style.opacity: 0 }
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
}

code -> heap: { style.opacity: 0 }

steps: {
  "append(9): 9 takes the spare slot — len 4, cap 4": {
    heap.r1.old.c3.label: "9"
    heap.r1.old.c3.style.stroke: "#d97706"
    heap.r1.old.c3.style.stroke-width: 3
    heap.r1.old.c3.style.stroke-dash: 0
    heap.r0.x.style.stroke: "#9ca3af"
    heap.r0.x.style.stroke-width: 1
    heap.r0.x.style.stroke-dash: 4
    code.l1.style.stroke: "#9ca3af"
    code.l1.style.stroke-width: 1
    code.l1.style.bold: false
    code.l5.style.stroke: "#d97706"
    code.l5.style.stroke-width: 2
    code.l5.style.bold: true
  }
  "append(2): no room — len 4 == cap 4": {
    heap.r1.old.style.stroke: "#dc2626"
    heap.r1.old.style.stroke-width: 3
    heap.r1.old.c3.style.stroke-width: 2
    heap.r0.x.label: "2"
    heap.r0.x.style.stroke: "#d97706"
    heap.r0.x.style.stroke-width: 3
    heap.r0.x.style.stroke-dash: 0
    code.l5.style.stroke: "#9ca3af"
    code.l5.style.stroke-width: 1
    code.l5.style.bold: false
    code.l2.style.stroke: "#d97706"
    code.l2.style.stroke-width: 2
    code.l2.style.bold: true
  }
  "grow: allocate a new array with cap 8": {
    heap.new.style.opacity: 1
    heap.new.c0.style.opacity: 1
    heap.new.c1.style.opacity: 1
    heap.new.c2.style.opacity: 1
    heap.new.c3.style.opacity: 1
    heap.new.c4.style.opacity: 1
    heap.new.c5.style.opacity: 1
    heap.new.c6.style.opacity: 1
    heap.new.c7.style.opacity: 1
    code.l2.style.stroke: "#9ca3af"
    code.l2.style.stroke-width: 1
    code.l2.style.bold: false
    code.l3.style.stroke: "#d97706"
    code.l3.style.stroke-width: 2
    code.l3.style.bold: true
  }
  "copy all 4 elements, free the old array": {
    heap.new.c0.label: "5"
    heap.new.c1.label: "8"
    heap.new.c2.label: "3"
    heap.new.c3.label: "9"
    heap.new.c0.style.stroke: "#16a34a"
    heap.new.c1.style.stroke: "#16a34a"
    heap.new.c2.style.stroke: "#16a34a"
    heap.new.c3.style.stroke: "#16a34a"
    heap.new.c0.style.stroke-dash: 0
    heap.new.c1.style.stroke-dash: 0
    heap.new.c2.style.stroke-dash: 0
    heap.new.c3.style.stroke-dash: 0
    heap.r1.old.style.stroke: "#9ca3af"
    heap.r1.old.style.stroke-dash: 4
    heap.r1.old.style.stroke-width: 2
    heap.r1.old.style.font-color: "#9ca3af"
    heap.r1.old.c0.style.stroke: "#9ca3af"
    heap.r1.old.c1.style.stroke: "#9ca3af"
    heap.r1.old.c2.style.stroke: "#9ca3af"
    heap.r1.old.c3.style.stroke: "#9ca3af"
    heap.r1.old.c0.style.stroke-dash: 4
    heap.r1.old.c1.style.stroke-dash: 4
    heap.r1.old.c2.style.stroke-dash: 4
    heap.r1.old.c3.style.stroke-dash: 4
    heap.r1.old.c0.style.font-color: "#9ca3af"
    heap.r1.old.c1.style.font-color: "#9ca3af"
    heap.r1.old.c2.style.font-color: "#9ca3af"
    heap.r1.old.c3.style.font-color: "#9ca3af"
    code.l3.style.stroke: "#9ca3af"
    code.l3.style.stroke-width: 1
    code.l3.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
  }
  "append(2) lands in slot 4 — len 5, cap 8": {
    heap.new.c4.label: "2"
    heap.new.c4.style.stroke: "#d97706"
    heap.new.c4.style.stroke-width: 3
    heap.new.c4.style.stroke-dash: 0
    heap.r0.x.style.stroke: "#9ca3af"
    heap.r0.x.style.stroke-width: 1
    heap.r0.x.style.stroke-dash: 4
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l5.style.stroke: "#d97706"
    code.l5.style.stroke-width: 2
    code.l5.style.bold: true
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

## When malloc says no

`malloc` returns `NULL` when it can't give you the block, and in C nothing
stops the program for you — the failure just sits in the return value,
waiting to be ignored. Dereference that NULL and you crash somewhere else,
far from the cause.

A library function has no business calling `exit()` on its caller's
behalf, so the C convention is to *report*: return an error code (`0` for
success, `-1` for failure is the usual pair), leave the data structure
exactly as it was, and let the caller decide what to do about it. The
"leave it as it was" half matters as much as the return value — a push
that fails after freeing the old block turns an out-of-memory hiccup into
a lost array.

## Challenge: Growable Array {#growable-array points=10}

Implement a growable array of ints:

- `array_init` starts empty: `data` NULL, `len` and `cap` 0.
- `array_push` writes to the next free slot and returns 0. When
  `len == cap`, allocate a new block of **double** the capacity (0 grows
  to 1), copy the old contents over, free the old block, then push. If
  that allocation fails, return **-1** and leave the array exactly as it
  was — nothing freed, nothing lost.
- `array_get(a, i)` returns the i'th element; callers only pass in-range
  indexes.
- `array_free` releases the block and resets the struct to empty.

The tests inspect `a.cap` after every push, so the doubling schedule
(1, 2, 4, 8, …) is part of the contract. They also force a failing
allocation — by forging `len == cap == SIZE_MAX / 8` so the doubling
`malloc` asks for more memory than the machine can address — and expect
`-1` back with the struct untouched.

### Starter

```c
#include <stdlib.h>

struct array {
	int *data;   /* the malloc'd block; NULL when cap == 0 */
	size_t len;  /* slots in use */
	size_t cap;  /* slots allocated */
};

/* array_init prepares an empty array. */
void array_init(struct array *a) {
	/* TODO: data = NULL, len = 0, cap = 0 */
	(void)a;
}

/* array_push appends v, doubling the block when it is full (0 -> 1).
   Returns 0 on success, -1 if allocation fails (array left unchanged). */
int array_push(struct array *a, int v) {
	/* TODO: if len == cap, malloc a block of 2*cap (or 1); if malloc
	   returns NULL, return -1 without touching the array. Otherwise copy
	   the old contents across, free the old block, and point data at the
	   new one. Then store v at data[len], bump len, and return 0. */
	(void)a;
	(void)v;
	return 0;
}

/* array_get returns the i'th element (0-indexed, always in range). */
int array_get(const struct array *a, size_t i) {
	/* TODO */
	(void)a;
	(void)i;
	return 0;
}

/* array_free releases the block and resets the array to empty. */
void array_free(struct array *a) {
	/* TODO */
	(void)a;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

struct array {
	int *data;
	size_t len;
	size_t cap;
};

void array_init(struct array *a);
int array_push(struct array *a, int v);
int array_get(const struct array *a, size_t i);
void array_free(struct array *a);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static size_t next_pow2(size_t n) {
	size_t c = 1;
	while (c < n) {
		c *= 2;
	}
	return c;
}

static void test_init_is_empty(void) {
	struct array a;
	array_init(&a);
	if (a.len != 0 || a.cap != 0) {
		printf("    after array_init: len=%zu cap=%zu, wanted len=0 cap=0\n",
		       a.len, a.cap);
	}
	check(a.len == 0 && a.cap == 0, "test_init_is_empty");
	array_free(&a);
}

static void test_push_and_get(void) {
	struct array a;
	int ok = 1;
	size_t i;

	array_init(&a);
	for (i = 0; i < 100; i++) {
		if (array_push(&a, (int)(i * i)) != 0) {
			printf("    push %zu returned -1, wanted 0 (malloc should not fail here)\n", i);
			ok = 0;
			break;
		}
	}
	if (ok && a.len != 100) {
		printf("    after 100 pushes: len=%zu, wanted 100\n", a.len);
		ok = 0;
	}
	for (i = 0; i < 100 && ok; i++) {
		int got = array_get(&a, i);
		if (got != (int)(i * i)) {
			printf("    a[%zu] = %d, wanted %d\n", i, got, (int)(i * i));
			ok = 0;
		}
	}
	check(ok, "test_push_and_get");
	array_free(&a);
}

static void test_doubling_schedule(void) {
	struct array a;
	int ok = 1;
	size_t i;

	array_init(&a);
	for (i = 1; i <= 200; i++) {
		array_push(&a, (int)i);
		if (a.cap != next_pow2(i)) {
			printf("    after push %zu: cap=%zu, wanted %zu (doubling schedule 1, 2, 4, 8, ...)\n",
			       i, a.cap, next_pow2(i));
			ok = 0;
			break;
		}
	}
	check(ok, "test_doubling_schedule");
	array_free(&a);
}

static void test_growth_preserves_contents(void) {
	struct array a;
	int ok = 1;
	size_t i, j;

	array_init(&a);
	for (i = 0; i < 17 && ok; i++) {
		array_push(&a, 1000 - (int)i);
		for (j = 0; j <= i && ok; j++) {
			int got = array_get(&a, j);
			if (got != 1000 - (int)j) {
				printf("    after %zu pushes: a[%zu] = %d, wanted %d (contents lost while growing?)\n",
				       i + 1, j, got, 1000 - (int)j);
				ok = 0;
			}
		}
	}
	check(ok, "test_growth_preserves_contents");
	array_free(&a);
}

static void test_push_reports_malloc_failure(void) {
	struct array a;
	int ok = 1, rc;
	const size_t huge = SIZE_MAX / 8;

	array_init(&a);
	array_push(&a, 42); /* one real element, so data is a real block */

	/* Forge a full array so large that the doubling malloc must fail:
	   doubling SIZE_MAX/8 ints asks for ~SIZE_MAX bytes. */
	a.len = huge;
	a.cap = huge;
	rc = array_push(&a, 7);
	if (rc != -1) {
		printf("    push into a maxed-out array returned %d, wanted -1 (report malloc failure)\n", rc);
		ok = 0;
	}
	if (a.len != huge || a.cap != huge) {
		printf("    failed push changed the array: len=%zu cap=%zu, wanted len=cap=%zu (leave it untouched)\n",
		       a.len, a.cap, huge);
		ok = 0;
	}
	check(ok, "test_push_reports_malloc_failure");

	/* Un-forge before freeing. */
	a.len = 1;
	a.cap = 1;
	array_free(&a);
}

int main(void) {
	test_init_is_empty();
	test_push_and_get();
	test_doubling_schedule();
	test_growth_preserves_contents();
	test_push_reports_malloc_failure();
	return failed;
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

Watch a full run on `[3 7 5 1]` — amber is the element being placed, green the sorted prefix:

```d2
direction: right

code: "" {
  grid-columns: 1
  grid-gap: 0
  l1: " for i in 1..n-1:" {
    height: 30
    label.near: center-left
    style: {stroke: "#9ca3af"; font: mono}
  }
  l2: "   key = a[i]; j = i" {
    height: 30
    label.near: center-left
    style: {stroke: "#9ca3af"; font: mono}
  }
  l3: "   while j>0 and a[j-1]>key:" {
    height: 30
    label.near: center-left
    style: {stroke: "#9ca3af"; font: mono}
  }
  l4: "     a[j] = a[j-1]; j -= 1" {
    height: 30
    label.near: center-left
    style: {stroke: "#9ca3af"; font: mono}
  }
  l5: "   a[j] = key" {
    height: 30
    label.near: center-left
    style: {stroke: "#9ca3af"; font: mono}
  }
}

key: "∅" {
  shape: circle
  width: 64
  height: 64
  style.stroke: "#9ca3af"
}

arr: "" {
  grid-rows: 1
  grid-gap: 0
  c0: "3" { width: 64; height: 64 }
  c1: "7" { width: 64; height: 64 }
  c2: "5" { width: 64; height: 64 }
  c3: "1" { width: 64; height: 64 }
}

code -> key: {style.opacity: 0}
key -> arr: {style.opacity: 0}

code.l1.style.stroke: "#d97706"
code.l1.style.stroke-width: 2
code.l1.style.bold: true

steps: {
  "key = 7: 3 ≤ 7, already in place": {
    code.l1.style.stroke: "#9ca3af"
    code.l1.style.stroke-width: 1
    code.l1.style.bold: false
    code.l3.style.stroke: "#d97706"
    code.l3.style.stroke-width: 2
    code.l3.style.bold: true
    key.label: "7"
    key.style.stroke: "#d97706"
    key.style.stroke-width: 3
    arr.c0.style.stroke: "#16a34a"
    arr.c1.style.stroke: "#d97706"
    arr.c1.style.stroke-width: 3
  }
  "key = 5: 7 > 5, so 7 shifts right": {
    code.l3.style.stroke: "#9ca3af"
    code.l3.style.stroke-width: 1
    code.l3.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
    key.label: "5"
    arr.c0.style.stroke: "#16a34a"
    arr.c1.label: "→"
    arr.c1.style.stroke: "#dc2626"
    arr.c1.style.stroke-width: 3
    arr.c2.label: "7"
    arr.c2.style.stroke: "#d97706"
    arr.c2.style.stroke-width: 3
  }
  "5 drops into the gap: [3 5 7] sorted": {
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l5.style.stroke: "#d97706"
    code.l5.style.stroke-width: 2
    code.l5.style.bold: true
    key.label: "∅"
    key.style.stroke: "#9ca3af"
    key.style.stroke-width: 1
    arr.c1.label: "5"
    arr.c1.style.stroke: "#16a34a"
    arr.c1.style.stroke-width: 2
    arr.c2.style.stroke: "#16a34a"
    arr.c2.style.stroke-width: 2
  }
  "key = 1: smaller than all three, all shift": {
    code.l5.style.stroke: "#9ca3af"
    code.l5.style.stroke-width: 1
    code.l5.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
    key.label: "1"
    key.style.stroke: "#d97706"
    key.style.stroke-width: 3
    arr.c1.label: "→"
    arr.c2.label: "→"
    arr.c3.label: "→"
    arr.c1.style.stroke: "#dc2626"
    arr.c2.style.stroke: "#dc2626"
    arr.c3.style.stroke: "#dc2626"
    arr.c3.style.stroke-width: 3
  }
  "1 lands at the front: [1 3 5 7], done": {
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l5.style.stroke: "#d97706"
    code.l5.style.stroke-width: 2
    code.l5.style.bold: true
    key.label: "∅"
    key.style.stroke: "#9ca3af"
    key.style.stroke-width: 1
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

One C-specific trap lives in that inner loop. The natural way to write it
uses a descending index, and if you declare that index `size_t` — unsigned,
like everything else in this course — then `j >= 0` is **always true**, and
`j--` at zero wraps around to `SIZE_MAX` instead of going negative. Your
loop then reads wildly out of bounds. Use a signed index for the walk
(`long j`, or restructure the loop to compare `j > 0` and index `j - 1`).
Unsigned underflow is one of C's quietest bugs: perfectly defined behavior,
completely wrong answer.

## Where n² is the right answer

Worst case (reverse-sorted input) every element walks all the way left:
about n²/2 shifts. But two properties make insertion sort a workhorse
rather than a toy:

- **Adaptive.** The cost is really O(n + inversions) — an *inversion*
  being a pair that's out of order. Nearly-sorted input has few
  inversions, so insertion sort runs in nearly linear time. Appending a
  handful of new records to a sorted file? Insertion sort is hard to beat.
- **Tiny constants.** For small n (a few dozen), its simple inner loop
  beats the fancy algorithms' bookkeeping — no recursion, no function-call
  overhead, and it walks memory in a straight line, which the cache loves.
  Production sorts — including the `qsort` in your libc — switch to
  insertion sort for small subarrays.

One more word you'll need later: a sort is **stable** if equal elements
keep their original relative order. Insertion sort is stable (it only
shifts *strictly larger* elements). Hold that thought for merge sort.

## Challenge: Insertion Sort {#insertion-sort points=10}

Sort an array of `n` ints in place using insertion sort: for each index i
from 1 up, shift larger elements of the sorted prefix right and insert
`a[i]` where it belongs. No extra array, no `qsort`. Mind the unsigned
index trap above.

### Starter

```c
#include <stdlib.h>

/* insertion_sort sorts a[0..n-1] in place, ascending. */
void insertion_sort(int *a, size_t n) {
	/* TODO: for each i, walk a[i] left through the sorted prefix */
	(void)a;
	(void)n;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>

void insertion_sort(int *a, size_t n);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static int cmp_int(const void *x, const void *y) {
	int a = *(const int *)x, b = *(const int *)y;
	return (a > b) - (a < b);
}

/* sorts_like_qsort sorts a copy with qsort(3) and compares, printing the
   first difference it finds. */
static int sorts_like_qsort(const int *in, size_t n) {
	int *mine = malloc(n * sizeof(int) + 1);
	int *want = malloc(n * sizeof(int) + 1);
	size_t i;
	int ok = 1;

	for (i = 0; i < n; i++) {
		mine[i] = in[i];
		want[i] = in[i];
	}
	qsort(want, n, sizeof(int), cmp_int);
	insertion_sort(mine, n);
	for (i = 0; i < n; i++) {
		if (mine[i] != want[i]) {
			printf("    sorting %zu elements (input[0..2]: %d %d %d ...): a[%zu] = %d, wanted %d\n",
			       n, in[0], n > 1 ? in[1] : 0, n > 2 ? in[2] : 0,
			       i, mine[i], want[i]);
			ok = 0;
			break;
		}
	}
	free(mine);
	free(want);
	return ok;
}

int main(void) {
	int empty[1] = {0};
	int single[] = {42};
	int sorted[] = {1, 2, 3, 4, 5};
	int reverse[] = {5, 4, 3, 2, 1};
	int dups[] = {3, 1, 3, 1, 3};
	int negs[] = {0, -5, 3, -5, 2, 0};
	int two[] = {2, 1};
	int *big;
	unsigned int seed = 1;
	size_t i;

	check(sorts_like_qsort(empty, 0), "test_empty");
	check(sorts_like_qsort(single, 1), "test_single");
	check(sorts_like_qsort(sorted, 5), "test_sorted");
	check(sorts_like_qsort(reverse, 5), "test_reverse");
	check(sorts_like_qsort(dups, 5), "test_duplicates");
	check(sorts_like_qsort(negs, 6), "test_negatives");
	check(sorts_like_qsort(two, 2), "test_two");

	/* Deterministic pseudo-random input via a small LCG. */
	big = malloc(1000 * sizeof(int));
	for (i = 0; i < 1000; i++) {
		seed = seed * 1664525u + 1013904223u;
		big[i] = (int)(seed % 10000u);
	}
	check(sorts_like_qsort(big, 1000), "test_big_random");
	free(big);

	return failed;
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

Watch the merge of `[1 4 9]` and `[2 3 8]` — amber marks the pair under comparison, dashed grey the consumed cells:

```d2
grid-columns: 2
grid-gap: 40

m: "" {
  grid-rows: 3
  grid-gap: 12

  lft: "" {
    grid-rows: 2
    grid-gap: 0
    s0: "" { width: 40; height: 28; style: { stroke: transparent; fill: transparent } }
    p0: "i" { width: 64; height: 28; shape: oval; style: { stroke: "#d97706"; font-color: "#d97706"; fill: transparent } }
    p1: "i" { width: 64; height: 28; shape: oval; style: { stroke: transparent; font-color: transparent; fill: transparent } }
    p2: "i" { width: 64; height: 28; shape: oval; style: { stroke: transparent; font-color: transparent; fill: transparent } }
    n: "a" { width: 40; height: 64; style: { stroke: transparent; fill: transparent } }
    c0: "1" { width: 64; height: 64 }
    c1: "4" { width: 64; height: 64 }
    c2: "9" { width: 64; height: 64 }
  }

  rgt: "" {
    grid-rows: 2
    grid-gap: 0
    s0: "" { width: 40; height: 28; style: { stroke: transparent; fill: transparent } }
    p0: "j" { width: 64; height: 28; shape: oval; style: { stroke: "#d97706"; font-color: "#d97706"; fill: transparent } }
    p1: "j" { width: 64; height: 28; shape: oval; style: { stroke: transparent; font-color: transparent; fill: transparent } }
    p2: "j" { width: 64; height: 28; shape: oval; style: { stroke: transparent; font-color: transparent; fill: transparent } }
    n: "b" { width: 40; height: 64; style: { stroke: transparent; fill: transparent } }
    c0: "2" { width: 64; height: 64 }
    c1: "3" { width: 64; height: 64 }
    c2: "8" { width: 64; height: 64 }
  }

  out: "" {
    grid-rows: 1
    grid-gap: 0
    n: "out" { width: 40; height: 64; style: { stroke: transparent; fill: transparent } }
    c0: "" { width: 64; height: 64 }
    c1: "" { width: 64; height: 64 }
    c2: "" { width: 64; height: 64 }
    c3: "" { width: 64; height: 64 }
    c4: "" { width: 64; height: 64 }
    c5: "" { width: 64; height: 64 }
  }
}

code: "" {
  grid-columns: 1
  grid-gap: 0
  # transparent wrapper: the root grid stretches this container to the arrays'
  # height, and a default fill would paint the leftover space as a phantom row
  style: { fill: transparent; stroke: transparent }
  l1: "while i < len(a), j < len(b):" { height: 32; style: { stroke: "#9ca3af"; font: mono } }
  l2: "  if a[i] <= b[j]:" { height: 32; style: { stroke: "#9ca3af"; font: mono } }
  l3: "    take a[i]; i += 1" { height: 32; style: { stroke: "#9ca3af"; font: mono } }
  l4: "  else: take b[j]; j += 1" { height: 32; style: { stroke: "#9ca3af"; font: mono } }
  l5: "append the leftovers" { height: 32; style: { stroke: "#9ca3af"; font: mono } }
}

code.l1.style.stroke: "#d97706"
code.l1.style.stroke-width: 2
code.l1.style.bold: true

steps: {
  "compare 1 vs 2 → 1 wins, fills out[0]": {
    code.l1.style.stroke: "#9ca3af"
    code.l1.style.stroke-width: 1
    code.l1.style.bold: false
    code.l3.style.stroke: "#d97706"
    code.l3.style.stroke-width: 2
    code.l3.style.bold: true
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
    code.l3.style.stroke: "#9ca3af"
    code.l3.style.stroke-width: 1
    code.l3.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
    m.lft.p0.style.stroke: transparent
    m.lft.p0.style.font-color: transparent
    m.lft.p1.style.stroke: "#d97706"
    m.lft.p1.style.font-color: "#d97706"
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
    m.rgt.p0.style.stroke: transparent
    m.rgt.p0.style.font-color: transparent
    m.rgt.p1.style.stroke: "#d97706"
    m.rgt.p1.style.font-color: "#d97706"
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
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l3.style.stroke: "#d97706"
    code.l3.style.stroke-width: 2
    code.l3.style.bold: true
    m.rgt.p1.style.stroke: transparent
    m.rgt.p1.style.font-color: transparent
    m.rgt.p2.style.stroke: "#d97706"
    m.rgt.p2.style.font-color: "#d97706"
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
    code.l3.style.stroke: "#9ca3af"
    code.l3.style.stroke-width: 1
    code.l3.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
    m.lft.p1.style.stroke: transparent
    m.lft.p1.style.font-color: transparent
    m.lft.p2.style.stroke: "#d97706"
    m.lft.p2.style.font-color: "#d97706"
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
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l5.style.stroke: "#d97706"
    code.l5.style.stroke-width: 2
    code.l5.style.bold: true
    m.rgt.p2.style.stroke: transparent
    m.rgt.p2.style.font-color: transparent
    m.lft.p2.style.stroke: transparent
    m.lft.p2.style.font-color: transparent
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

In C, "produce a new array" means *someone has to own that memory*. The
cleanest contract — and the one this challenge uses — is that the **caller
provides the output buffer**: `merge` writes `na + nb` ints into `out` and
allocates nothing. That's the C convention you'll see everywhere
(`memcpy`, `snprintf`, `read`): the callee fills a buffer it does not own,
so ownership questions never arise.

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
sort uses O(n) extra memory. Allocate that scratch buffer **once**, up
front, and pass it down the recursion — a `malloc` inside the recursive
step would run n times and dominate your runtime (and give you n chances
to leak it). In exchange you get two guarantees quicksort won't give you:
the n log n bound holds for *every* input, and the sort is **stable** (on
ties, take from the left half first — equal elements keep their order).
That stability is why Python's built-in sort and Java's object sort are
merge-sort descendants (Timsort: merge sort fused with the
insertion-sort-on-small-runs trick you just learned).

## Challenge: Merge Two Sorted Arrays {#merge points=10}

Write the merge step on its own: given two sorted arrays, fill the
caller's `out` buffer (which has room for exactly `na + nb` ints) with
every element of both, in order. Walk a cursor down each input, always
taking the smaller front element; on ties take from `a` first. Don't sort
— the inputs are already sorted, and your job is O(na + nb). Allocate
nothing.

### Starter

```c
#include <stdlib.h>

/* merge writes the na+nb elements of a and b into out, in ascending
   order. a and b are each already sorted; out has room for na+nb ints. */
void merge(const int *a, size_t na, const int *b, size_t nb, int *out) {
	/* TODO: two cursors; take the smaller front element; drain leftovers */
	(void)a;
	(void)na;
	(void)b;
	(void)nb;
	(void)out;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>

void merge(const int *a, size_t na, const int *b, size_t nb, int *out);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static int merges_to(const int *a, size_t na, const int *b, size_t nb,
                     const int *want) {
	int out[32];
	size_t i;

	for (i = 0; i < 32; i++) {
		out[i] = -999;
	}
	merge(a, na, b, nb, out);
	for (i = 0; i < na + nb; i++) {
		if (out[i] != want[i]) {
			printf("    merging %zu + %zu elements: out[%zu] = %d, wanted %d (-999 means the slot was never written)\n",
			       na, nb, i, out[i], want[i]);
			return 0;
		}
	}
	return 1;
}

int main(void) {
	int a1[] = {1, 4, 9}, b1[] = {2, 3, 8}, w1[] = {1, 2, 3, 4, 8, 9};
	int a2[] = {1, 2}, b2[] = {3, 4}, w2[] = {1, 2, 3, 4};
	int a3[] = {3, 4}, b3[] = {1, 2}, w3[] = {1, 2, 3, 4};
	int a4[] = {1, 1, 2}, b4[] = {1, 2, 2}, w4[] = {1, 1, 1, 2, 2, 2};
	int a5[] = {5}, b5[] = {-5}, w5[] = {-5, 5};
	int only[] = {1, 2};
	int empty[1] = {0};
	int i, ok;

	check(merges_to(empty, 0, empty, 0, empty), "test_both_empty");
	check(merges_to(empty, 0, only, 2, only), "test_left_empty");
	check(merges_to(only, 2, empty, 0, only), "test_right_empty");
	check(merges_to(a1, 3, b1, 3, w1), "test_interleaved");
	check(merges_to(a2, 2, b2, 2, w2), "test_all_left_first");
	check(merges_to(a3, 2, b3, 2, w3), "test_all_right_first");
	check(merges_to(a4, 3, b4, 3, w4), "test_duplicates");
	check(merges_to(a5, 1, b5, 1, w5), "test_single_each");

	/* Inputs must not be modified. */
	ok = 1;
	{
		int src_a[] = {1, 3, 5};
		int src_b[] = {2, 4, 6};
		int out[6];
		merge(src_a, 3, src_b, 3, out);
		for (i = 0; i < 3; i++) {
			if (src_a[i] != 1 + 2 * i || src_b[i] != 2 + 2 * i) {
				printf("    merge modified its inputs: a[%d] = %d (wanted %d), b[%d] = %d (wanted %d)\n",
				       i, src_a[i], 1 + 2 * i, i, src_b[i], 2 + 2 * i);
				ok = 0;
			}
		}
	}
	check(ok, "test_inputs_unmodified");

	return failed;
}
```

## Challenge: Merge Sort {#mergesort points=15}

Now the full algorithm, sorting `a[0..n-1]` **in place** (the caller sees
their array sorted when you return). Split in half, recurse on each half,
merge the two halves through a scratch buffer, then copy the merged run
back. Allocate the scratch buffer **once** in `merge_sort` and pass it
into the recursion — not once per recursive call. Free it before you
return, and leak nothing.

Reuse the merge you just wrote (each challenge is graded as a standalone
file, so paste it in).

### Starter

```c
#include <stdlib.h>

/* merge_sort sorts a[0..n-1] in place, ascending. */
void merge_sort(int *a, size_t n) {
	/* TODO: malloc ONE scratch buffer of n ints, call the recursive
	   helper below, then free it */
	(void)a;
	(void)n;
}

/* sort_rec sorts a[lo..hi-1] using scratch as merge space. */
static void sort_rec(int *a, size_t lo, size_t hi, int *scratch) {
	/* TODO: base case (hi - lo <= 1); recurse on the halves; merge the
	   two sorted halves into scratch, then copy them back into a */
	(void)a;
	(void)lo;
	(void)hi;
	(void)scratch;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>

void merge_sort(int *a, size_t n);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static int cmp_int(const void *x, const void *y) {
	int a = *(const int *)x, b = *(const int *)y;
	return (a > b) - (a < b);
}

static int sorts_like_qsort(const int *in, size_t n) {
	int *mine = malloc(n * sizeof(int) + 1);
	int *want = malloc(n * sizeof(int) + 1);
	size_t i;
	int ok = 1;

	for (i = 0; i < n; i++) {
		mine[i] = in[i];
		want[i] = in[i];
	}
	qsort(want, n, sizeof(int), cmp_int);
	merge_sort(mine, n);
	for (i = 0; i < n; i++) {
		if (mine[i] != want[i]) {
			printf("    sorting %zu elements (input[0..2]: %d %d %d ...): a[%zu] = %d, wanted %d\n",
			       n, in[0], n > 1 ? in[1] : 0, n > 2 ? in[2] : 0,
			       i, mine[i], want[i]);
			ok = 0;
			break;
		}
	}
	free(mine);
	free(want);
	return ok;
}

int main(void) {
	int empty[1] = {0};
	int single[] = {1};
	int two[] = {2, 1};
	int sorted[] = {1, 2, 3, 4, 5};
	int reverse[] = {5, 4, 3, 2, 1};
	int dups[] = {3, 1, 3, 1, 3};
	int negs[] = {0, -5, 3, -5, 2, 0};
	int *big;
	unsigned int seed = 7;
	size_t i;

	check(sorts_like_qsort(empty, 0), "test_empty");
	check(sorts_like_qsort(single, 1), "test_single");
	check(sorts_like_qsort(two, 2), "test_two");
	check(sorts_like_qsort(sorted, 5), "test_sorted");
	check(sorts_like_qsort(reverse, 5), "test_reverse");
	check(sorts_like_qsort(dups, 5), "test_duplicates");
	check(sorts_like_qsort(negs, 6), "test_negatives");

	big = malloc(5000 * sizeof(int));
	for (i = 0; i < 5000; i++) {
		seed = seed * 1664525u + 1013904223u;
		big[i] = (int)(seed % 100000u) - 50000;
	}
	check(sorts_like_qsort(big, 5000), "test_big_random");
	free(big);

	return failed;
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
sorted independently, *in place*: no merge, no scratch buffer, no
allocation at all.

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

Watch Lomuto's sweep on `[3 8 2 5 1 4]` — violet is the pivot, green the growing ≤-pivot zone:

```d2
direction: right

code: "" {
  grid-columns: 1
  grid-gap: 0
  l1: " 1 p = a[hi]; i = lo" { height: 30; style.stroke: "#9ca3af"; style.font: mono; label.near: center-left }
  l2: " 2 for j in lo..hi-1:" { height: 30; style.stroke: "#9ca3af"; style.font: mono; label.near: center-left }
  l3: " 3   if a[j] < p:" { height: 30; style.stroke: "#9ca3af"; style.font: mono; label.near: center-left }
  l4: " 4     swap(a[i], a[j]); i += 1" { height: 30; style.stroke: "#9ca3af"; style.font: mono; label.near: center-left }
  l5: " 5 swap(a[i], a[hi])" { height: 30; style.stroke: "#9ca3af"; style.font: mono; label.near: center-left }
}

viz: "" {
  grid-columns: 1
  style.stroke: transparent
  style.fill: transparent
  grid-gap: 16
  mk: "" {
    grid-rows: 1
    style.stroke: transparent
    style.fill: transparent
    grid-gap: 0
    sp: "" { width: 284; height: 60; style.opacity: 0 }
    pv: "pivot" {
      shape: diamond
      width: 100
      height: 60
      style.stroke: "#7c3aed"
      style.stroke-width: 2
    }
  }
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
}

viz.mk.pv -> viz.arr.c5: {
  style.stroke: "#7c3aed"
  style.stroke-width: 2
}

steps: {
  "pivot 4 — walk j left→right; i marks the ≤-edge": {
    viz.arr.c5.style.stroke: "#7c3aed"
    viz.arr.c5.style.stroke-width: 3
    code.l1.style.stroke: "#d97706"
    code.l1.style.stroke-width: 2
    code.l1.style.bold: true
  }
  "j=0: 3 ≤ 4 — joins the ≤-side, i=1": {
    viz.arr.c0.style.stroke: "#16a34a"
    code.l1.style.stroke: "#9ca3af"
    code.l1.style.stroke-width: 1
    code.l1.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
  }
  "j=1: 8 > 4 — stays put, no swap": {
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l3.style.stroke: "#d97706"
    code.l3.style.stroke-width: 2
    code.l3.style.bold: true
  }
  "j=2: 2 ≤ 4 — swap 8↔2, i=2": {
    viz.arr.c1.label: "2"
    viz.arr.c1.style.stroke: "#16a34a"
    viz.arr.c2.label: "8"
    code.l3.style.stroke: "#9ca3af"
    code.l3.style.stroke-width: 1
    code.l3.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
  }
  "j=3: 5 > 4 — no swap": {
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l3.style.stroke: "#d97706"
    code.l3.style.stroke-width: 2
    code.l3.style.bold: true
  }
  "j=4: 1 ≤ 4 — swap 1↔8, i=3": {
    viz.arr.c2.label: "1"
    viz.arr.c2.style.stroke: "#16a34a"
    viz.arr.c4.label: "8"
    code.l3.style.stroke: "#9ca3af"
    code.l3.style.stroke-width: 1
    code.l3.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
  }
  "pivot → slot i=3: left ≤ 4, right > 4 — 4 is HOME": {
    viz.arr.c3.label: "4"
    viz.arr.c3.style.stroke: "#16a34a"
    viz.arr.c3.style.stroke-width: 3
    viz.arr.c3.style.font-color: "#7c3aed"
    viz.arr.c5.label: "5"
    viz.arr.c5.style.stroke: "#dc2626"
    viz.arr.c5.style.stroke-width: 2
    (viz.mk.pv -> viz.arr.c5)[0].style.stroke: "#9ca3af"
    (viz.mk.pv -> viz.arr.c5)[0].style.stroke-dash: 4
    code.l4.style.stroke: "#9ca3af"
    code.l4.style.stroke-width: 1
    code.l4.style.bold: false
    code.l5.style.stroke: "#d97706"
    code.l5.style.stroke-width: 2
    code.l5.style.bold: true
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
up on exactly the data you'll actually see. That depth is its own hazard
in C: recursion depth here grows with *n*, not log n, and a few million
elements is a genuine stack overflow — a crash, not a slowdown. (The
standard defense, which the reference solution uses, is to recurse into
the *smaller* side and loop on the larger. Depth is then bounded by
log₂ n even when the splits are terrible.) Two standard
defenses:

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
grow with n — which is how an O(n²) sort and a stack overflow reach
production together. Make your pivot earn its keep.

Quicksort's trades, laid against merge sort: in place and fast in
practice, but not stable, and n log n only *probabilistically*. Real
libraries hedge, and the two you're most likely to link against hedge in
opposite directions. C++'s `std::sort` is an **introsort**: quicksort that
counts its own recursion depth and, when a run of bad pivots pushes it
past ~2 log n, bails out to heapsort — which happens to be your next
lesson. glibc's `qsort` goes the other way: it is a *merge sort*, which is
why the C library function you'd assume sorts in place may quietly
`malloc` a scratch buffer — and only if that allocation fails does it fall
back to an in-place quicksort (itself an introsort, with heapsort as *its*
own safety net).

That glibc choice has a postscript worth your time. In 2023 its
maintainers replaced the merge sort with an introsort — faster, and no
allocation — and then **reverted it in January 2024**. Nothing in the C
standard or POSIX ever promised `qsort` was stable, but merge sort had
made it stable for decades, and enough real programs had quietly come to
depend on that accident that removing it broke them. Stability, from the
last lesson, is not an academic footnote: it is a property people build
on whether or not you documented it.

## Challenge: Quicksort {#quicksort points=15}

Sort `a[0..n-1]` in place with quicksort: partition, then recurse on both
sides. Any correct partition scheme passes, but the tests include
already-sorted, reverse-sorted, and all-equal inputs of a few thousand
elements — median-of-three pivoting with Hoare partitioning sails through
all of them. Allocate nothing.

### Starter

```c
#include <stdlib.h>

/* quicksort sorts a[0..n-1] in place, ascending. */
void quicksort(int *a, size_t n) {
	/* TODO: partition around a well-chosen pivot, recurse on both sides
	   (a static helper taking lo/hi bounds is the usual shape) */
	(void)a;
	(void)n;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>

void quicksort(int *a, size_t n);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static int cmp_int(const void *x, const void *y) {
	int a = *(const int *)x, b = *(const int *)y;
	return (a > b) - (a < b);
}

static int sorts_like_qsort(const int *in, size_t n) {
	int *mine = malloc(n * sizeof(int) + 1);
	int *want = malloc(n * sizeof(int) + 1);
	size_t i;
	int ok = 1;

	for (i = 0; i < n; i++) {
		mine[i] = in[i];
		want[i] = in[i];
	}
	qsort(want, n, sizeof(int), cmp_int);
	quicksort(mine, n);
	for (i = 0; i < n; i++) {
		if (mine[i] != want[i]) {
			printf("    sorting %zu elements (input[0..2]: %d %d %d ...): a[%zu] = %d, wanted %d\n",
			       n, in[0], n > 1 ? in[1] : 0, n > 2 ? in[2] : 0,
			       i, mine[i], want[i]);
			ok = 0;
			break;
		}
	}
	free(mine);
	free(want);
	return ok;
}

int main(void) {
	int empty[1] = {0};
	int single[] = {1};
	int two[] = {2, 1};
	int small_sorted[] = {1, 2, 3, 4, 5};
	int small_reverse[] = {5, 4, 3, 2, 1};
	int dups[] = {3, 1, 3, 1, 3};
	int negs[] = {0, -5, 3, -5, 2, 0};
	const size_t n = 2000;
	int *adversary;
	unsigned int seed = 99;
	size_t i;

	check(sorts_like_qsort(empty, 0), "test_empty");
	check(sorts_like_qsort(single, 1), "test_single");
	check(sorts_like_qsort(two, 2), "test_two");
	check(sorts_like_qsort(small_sorted, 5), "test_sorted");
	check(sorts_like_qsort(small_reverse, 5), "test_reverse");
	check(sorts_like_qsort(dups, 5), "test_duplicates");
	check(sorts_like_qsort(negs, 6), "test_negatives");

	adversary = malloc(n * sizeof(int));

	for (i = 0; i < n; i++) {
		adversary[i] = (int)i;
	}
	check(sorts_like_qsort(adversary, n), "test_already_sorted_2000");

	for (i = 0; i < n; i++) {
		adversary[i] = (int)(n - i);
	}
	check(sorts_like_qsort(adversary, n), "test_reverse_sorted_2000");

	for (i = 0; i < n; i++) {
		adversary[i] = 7;
	}
	check(sorts_like_qsort(adversary, n), "test_all_equal_2000");

	free(adversary);

	{
		int *big = malloc(5000 * sizeof(int));
		for (i = 0; i < 5000; i++) {
			seed = seed * 1664525u + 1013904223u;
			big[i] = (int)(seed % 100000u) - 50000;
		}
		check(sorts_like_qsort(big, 5000), "test_big_random");
		free(big);
	}

	return failed;
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
level filling left to right), it needs no pointers — no `struct node`, no
`malloc` per element, no cache-missing pointer chase. Lay the levels out
in an array, top to bottom, left to right:

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

Your dynamic array *is* the heap; the tree is a way of reading it. (That
parent formula is one more place to keep indexes honest: with `size_t i`,
the root's `(0 - 1) / 2` underflows to something enormous. Guard the
`i > 0` case before computing a parent.)

## Sift up, sift down

Both operations follow the same plan: break the heap property at one spot,
then repair it locally until it holds again.

**Push**: append the new value at the end (the only spot that keeps the
tree complete — reuse the doubling growth from lesson one). It may now be
smaller than its parent — **sift up**: while it's smaller than its parent,
swap with the parent. The path to the root has log n nodes, so at most
log n swaps.

Watch `push(1)` on a min-heap — the new value swaps upward until its parent is smaller:

```d2
steps: {
  "push(1) — new value goes in the next free slot": {
    grid-columns: 2
    grid-gap: 40

    panel: "" {
      style.stroke: transparent
      style.fill: transparent
      grid-columns: 1
      grid-gap: 28

      code: "" {
        grid-columns: 1
        grid-gap: 0
        l1: "push(x): a.append(x)" {
          height: 30
          style.stroke: "#d97706"
          style.stroke-width: 2
          style.bold: true
          style.font: mono
        }
        l2: "i = len(a) - 1" {
          height: 30
          style.stroke: "#9ca3af"
          style.font: mono
        }
        l3: "while i > 0 and a[i] < a[par]:" {
          height: 30
          style.stroke: "#9ca3af"
          style.font: mono
        }
        l4: "  swap(a[i], a[par]); i = par" {
          height: 30
          style.stroke: "#9ca3af"
          style.font: mono
        }
      }

      arr: "" {
        grid-rows: 1
        grid-gap: 0
        a0: "2" { width: 44; height: 44 }
        a1: "4" { width: 44; height: 44 }
        a2: "3" { width: 44; height: 44 }
        a3: "8" { width: 44; height: 44 }
        a4: "7" { width: 44; height: 44 }
        a5: "9" { width: 44; height: 44 }
        a6: "" { width: 44; height: 44; style.stroke-dash: 4 }
      }
    }

    tree: "" {
      style.stroke: transparent
      style.fill: transparent
      n0: "2" { shape: circle; width: 44; height: 44 }
      n1: "4" { shape: circle; width: 44; height: 44 }
      n2: "3" { shape: circle; width: 44; height: 44 }
      n3: "8" { shape: circle; width: 44; height: 44 }
      n4: "7" { shape: circle; width: 44; height: 44 }
      n5: "9" { shape: circle; width: 44; height: 44 }
      n6: "" { shape: circle; width: 44; height: 44; style.stroke-dash: 4 }
      n0 -> n1
      n0 -> n2
      n1 -> n3
      n1 -> n4
      n2 -> n5
      n2 -> n6
    }
  }
  "1 sits below its parent 3 — heap rule broken?": {
    panel.code.l1.style.stroke: "#9ca3af"
    panel.code.l1.style.stroke-width: 1
    panel.code.l1.style.bold: false
    panel.code.l3.style.stroke: "#d97706"
    panel.code.l3.style.stroke-width: 2
    panel.code.l3.style.bold: true

    tree.n6.label: "1"
    tree.n6.style.stroke-dash: 0
    tree.n6.style.stroke: "#d97706"
    tree.n6.style.stroke-width: 3
    panel.arr.a6.label: "1"
    panel.arr.a6.style.stroke-dash: 0
    panel.arr.a6.style.stroke: "#d97706"
    panel.arr.a6.style.stroke-width: 3
  }
  "1 < 3 → swap with parent; 3 settles below": {
    panel.code.l3.style.stroke: "#9ca3af"
    panel.code.l3.style.stroke-width: 1
    panel.code.l3.style.bold: false
    panel.code.l4.style.stroke: "#d97706"
    panel.code.l4.style.stroke-width: 2
    panel.code.l4.style.bold: true

    tree.n2.label: "1"
    tree.n2.style.stroke: "#d97706"
    tree.n2.style.stroke-width: 3
    tree.n6.label: "3"
    tree.n6.style.stroke: "#16a34a"
    tree.n6.style.stroke-width: 2
    panel.arr.a2.label: "1"
    panel.arr.a2.style.stroke: "#d97706"
    panel.arr.a2.style.stroke-width: 3
    panel.arr.a6.label: "3"
    panel.arr.a6.style.stroke: "#16a34a"
    panel.arr.a6.style.stroke-width: 2
  }
  "1 < 2 → swap again; 1 reaches the root": {
    tree.n0.label: "1"
    tree.n0.style.stroke: "#d97706"
    tree.n0.style.stroke-width: 3
    tree.n2.label: "2"
    tree.n2.style.stroke: "#16a34a"
    tree.n2.style.stroke-width: 2
    panel.arr.a0.label: "1"
    panel.arr.a0.style.stroke: "#d97706"
    panel.arr.a0.style.stroke-width: 3
    panel.arr.a2.label: "2"
    panel.arr.a2.style.stroke: "#16a34a"
    panel.arr.a2.style.stroke-width: 2
  }
  "root is the minimum — O(log n) swaps, one per level": {
    panel.code.l4.style.stroke: "#9ca3af"
    panel.code.l4.style.stroke-width: 1
    panel.code.l4.style.bold: false
    panel.code.l3.style.stroke: "#d97706"
    panel.code.l3.style.stroke-width: 2
    panel.code.l3.style.bold: true

    tree.n0.style.stroke: "#16a34a"
    tree.n0.style.stroke-width: 2
    tree.n1.style.stroke: "#16a34a"
    tree.n3.style.stroke: "#16a34a"
    tree.n4.style.stroke: "#16a34a"
    tree.n5.style.stroke: "#16a34a"
    panel.arr.a0.style.stroke: "#16a34a"
    panel.arr.a0.style.stroke-width: 2
    panel.arr.a1.style.stroke: "#16a34a"
    panel.arr.a3.style.stroke: "#16a34a"
    panel.arr.a4.style.stroke: "#16a34a"
    panel.arr.a5.style.stroke: "#16a34a"
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
   one (`n/2 - 1`) back to the root. Bottom-up, the two subtrees below you
   are always already heaps. (Nice fact: done in this order, heapify is
   O(n), not O(n log n) — most nodes sit near the bottom with short sifts.)
2. Repeatedly swap the max (root) with the last element, shrink the heap
   boundary by one, and sift the new root down.

In-place like quicksort, guaranteed n log n like merge sort — heapsort is
the "no nasty surprises" sort, which is exactly why introsort uses it as
its safety net. Its price is cache-hostile jumping around the array, so
it's usually a little slower in practice than quicksort's tight sweeps.

## Challenge: A Min-Heap {#min-heap points=20}

Implement `struct minheap`, backed by a growable int array (same doubling
you built in lesson one — `heap_init` starts empty, and `heap_push` grows
the block when `len == cap`). Keep the exact struct from the starter: the
tests verify the heap *invariant* — every parent ≤ its children — directly
on your `data` array after every operation, not just that the right
answers come out. `heap_free` must leave nothing leaked.

### Starter

```c
#include <stdlib.h>

/* minheap is a binary min-heap of ints stored flat in a grown array. */
struct minheap {
	int *data;
	size_t len;
	size_t cap;
};

/* heap_init prepares an empty heap. */
void heap_init(struct minheap *h) {
	/* TODO */
	(void)h;
}

/* heap_push adds v: append it (growing the block if full), then sift it
   up while it beats its parent. */
void heap_push(struct minheap *h, int v) {
	/* TODO */
	(void)h;
	(void)v;
}

/* heap_peek returns the smallest value. Only called when len > 0. */
int heap_peek(const struct minheap *h) {
	/* TODO */
	(void)h;
	return 0;
}

/* heap_pop removes and returns the smallest value: save the root, move
   the last element to the root, shrink, sift down. Only called when
   len > 0. */
int heap_pop(struct minheap *h) {
	/* TODO */
	(void)h;
	return 0;
}

/* heap_free releases the block and resets the heap to empty. */
void heap_free(struct minheap *h) {
	/* TODO */
	(void)h;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>

struct minheap {
	int *data;
	size_t len;
	size_t cap;
};

void heap_init(struct minheap *h);
void heap_push(struct minheap *h, int v);
int heap_peek(const struct minheap *h);
int heap_pop(struct minheap *h);
void heap_free(struct minheap *h);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* heap_ok verifies the heap property directly on the backing array,
   printing the first parent/child pair that violates it. */
static int heap_ok(const struct minheap *h) {
	size_t i;
	for (i = 1; i < h->len; i++) {
		size_t parent = (i - 1) / 2;
		if (h->data[parent] > h->data[i]) {
			printf("    heap property violated: data[%zu]=%d > its child data[%zu]=%d (len=%zu)\n",
			       parent, h->data[parent], i, h->data[i], h->len);
			return 0;
		}
	}
	return 1;
}

static int cmp_int(const void *x, const void *y) {
	int a = *(const int *)x, b = *(const int *)y;
	return (a > b) - (a < b);
}

static void test_push_maintains_invariant(void) {
	int vals[] = {5, 3, 8, 1, 9, 2, 7, 1, 6, 4};
	struct minheap h;
	int ok = 1;
	size_t i;

	heap_init(&h);
	for (i = 0; i < 10; i++) {
		heap_push(&h, vals[i]);
		if (ok && !heap_ok(&h)) {
			printf("    (that happened right after pushing %d, push #%zu)\n", vals[i], i + 1);
			ok = 0;
		}
	}
	check(ok, "test_push_maintains_invariant");
	if (h.len != 10) {
		printf("    len = %zu after 10 pushes, wanted 10\n", h.len);
	}
	check(h.len == 10, "test_len_after_pushes");
	if (heap_peek(&h) != 1) {
		printf("    peek = %d, wanted 1 (the minimum of everything pushed)\n", heap_peek(&h));
	}
	check(heap_peek(&h) == 1, "test_peek_is_min");
	heap_free(&h);
}

static void test_pop_drains_sorted(void) {
	int vals[] = {5, 3, 8, 1, 9, 2, 7, 1, 6, 4};
	int want[10];
	struct minheap h;
	int ok = 1;
	size_t i;

	heap_init(&h);
	for (i = 0; i < 10; i++) {
		heap_push(&h, vals[i]);
		want[i] = vals[i];
	}
	qsort(want, 10, sizeof(int), cmp_int);

	for (i = 0; i < 10; i++) {
		int got = heap_pop(&h);
		if (ok && got != want[i]) {
			printf("    pop #%zu = %d, wanted %d (pops must come out ascending)\n",
			       i + 1, got, want[i]);
			ok = 0;
		}
		if (ok && !heap_ok(&h)) {
			printf("    (heap property broken after pop #%zu)\n", i + 1);
			ok = 0;
		}
	}
	check(ok, "test_pop_drains_sorted");
	if (h.len != 0) {
		printf("    len = %zu after popping everything, wanted 0\n", h.len);
	}
	check(h.len == 0, "test_len_after_draining");
	heap_free(&h);
}

static void test_interleaved(void) {
	struct minheap h;
	int ok = 1, got;

	heap_init(&h);
	heap_push(&h, 10);
	heap_push(&h, 4);
	got = heap_pop(&h);
	if (got != 4) {
		printf("    push 10, push 4, then pop = %d, wanted 4\n", got);
		ok = 0;
	}
	heap_push(&h, 2);
	heap_push(&h, 8);
	got = heap_peek(&h);
	if (got != 2) {
		printf("    peek with {10, 2, 8} in the heap = %d, wanted 2\n", got);
		ok = 0;
	}
	got = heap_pop(&h);
	if (got != 2) {
		printf("    first drain pop = %d, wanted 2\n", got);
		ok = 0;
	}
	got = heap_pop(&h);
	if (got != 8) {
		printf("    second drain pop = %d, wanted 8\n", got);
		ok = 0;
	}
	got = heap_pop(&h);
	if (got != 10) {
		printf("    third drain pop = %d, wanted 10\n", got);
		ok = 0;
	}
	check(ok, "test_interleaved");
	heap_free(&h);
}

static void test_many_values(void) {
	const size_t n = 2000;
	int *vals = malloc(n * sizeof(int));
	struct minheap h;
	unsigned int seed = 3;
	int ok = 1;
	size_t i;

	heap_init(&h);
	for (i = 0; i < n; i++) {
		seed = seed * 1664525u + 1013904223u;
		vals[i] = (int)(seed % 1000u); /* plenty of duplicates */
		heap_push(&h, vals[i]);
	}
	if (!heap_ok(&h)) {
		printf("    (after %zu pushes)\n", n);
		ok = 0;
	}
	qsort(vals, n, sizeof(int), cmp_int);
	for (i = 0; i < n; i++) {
		int got = heap_pop(&h);
		if (ok && got != vals[i]) {
			printf("    pop #%zu of %zu = %d, wanted %d\n", i + 1, n, got, vals[i]);
			ok = 0;
		}
	}
	check(ok, "test_many_values");
	free(vals);
	heap_free(&h);
}

int main(void) {
	test_push_maintains_invariant();
	test_pop_drains_sorted();
	test_interleaved();
	test_many_values();
	return failed;
}
```

## Challenge: Heapsort {#heapsort points=15}

Sort `a[0..n-1]` in place: heapify it into a **max**-heap (sift down from
the last parent back to the root), then repeatedly swap the root to the
end of the shrinking heap region and sift the new root down. The sift-down
you wrote for the min-heap is the same routine with the comparison flipped
and an explicit heap-size boundary. Allocate nothing.

(The function is named `heapsort_ints` because BSD-derived libcs already
export a `heapsort` and we'd rather not fight the linker.)

### Starter

```c
#include <stdlib.h>

/* sift_down restores the max-heap property for the tree rooted at i,
   considering only a[0..size-1]. */
static void sift_down(int *a, size_t i, size_t size) {
	/* TODO: while a child is bigger, swap with the BIGGEST child */
	(void)a;
	(void)i;
	(void)size;
}

/* heapsort_ints sorts a[0..n-1] in place, ascending, via a max-heap. */
void heapsort_ints(int *a, size_t n) {
	/* TODO: heapify (sift down from n/2-1 back to 0), then swap-and-sift */
	(void)a;
	(void)n;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>

void heapsort_ints(int *a, size_t n);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static int cmp_int(const void *x, const void *y) {
	int a = *(const int *)x, b = *(const int *)y;
	return (a > b) - (a < b);
}

static int sorts_like_qsort(const int *in, size_t n) {
	int *mine = malloc(n * sizeof(int) + 1);
	int *want = malloc(n * sizeof(int) + 1);
	size_t i;
	int ok = 1;

	for (i = 0; i < n; i++) {
		mine[i] = in[i];
		want[i] = in[i];
	}
	qsort(want, n, sizeof(int), cmp_int);
	heapsort_ints(mine, n);
	for (i = 0; i < n; i++) {
		if (mine[i] != want[i]) {
			printf("    sorting %zu elements (input[0..2]: %d %d %d ...): a[%zu] = %d, wanted %d\n",
			       n, in[0], n > 1 ? in[1] : 0, n > 2 ? in[2] : 0,
			       i, mine[i], want[i]);
			ok = 0;
			break;
		}
	}
	free(mine);
	free(want);
	return ok;
}

int main(void) {
	int empty[1] = {0};
	int single[] = {1};
	int two[] = {2, 1};
	int sorted[] = {1, 2, 3, 4, 5};
	int reverse[] = {5, 4, 3, 2, 1};
	int dups[] = {3, 1, 3, 1, 3};
	int all_equal[] = {7, 7, 7, 7};
	int negs[] = {0, -5, 3, -5, 2, 0};
	int *big;
	unsigned int seed = 11;
	size_t i;

	check(sorts_like_qsort(empty, 0), "test_empty");
	check(sorts_like_qsort(single, 1), "test_single");
	check(sorts_like_qsort(two, 2), "test_two");
	check(sorts_like_qsort(sorted, 5), "test_sorted");
	check(sorts_like_qsort(reverse, 5), "test_reverse");
	check(sorts_like_qsort(dups, 5), "test_duplicates");
	check(sorts_like_qsort(all_equal, 4), "test_all_equal");
	check(sorts_like_qsort(negs, 6), "test_negatives");

	big = malloc(5000 * sizeof(int));
	for (i = 0; i < 5000; i++) {
		seed = seed * 1664525u + 1013904223u;
		big[i] = (int)(seed % 100000u) - 50000;
	}
	check(sorts_like_qsort(big, 5000), "test_big_random");
	free(big);

	return failed;
}
```

# Lesson: Hash Maps {#hash-maps}

Arrays answer "what's at index 7?" in O(1). The question real programs ask
is "what's the value for `"user:42"`?" — lookup by *name*, not position. A
hash map answers it in O(1) by manufacturing an index out of the name. C
gives you no dictionary at all, so this is the structure you'll end up
writing (or vendoring) in every nontrivial C program you ever work on.

## From string to index

Step one is a **hash function**: mash the bytes of the key into one
integer, deterministically (same key → same number, every time) and
*scrambled* (similar keys → wildly different numbers — real keys come in
families like `user:41`, `user:42`, and if similar keys clustered, so
would your data). **FNV-1a** is the classic minimal hash that earns both
properties with four lines:

```c
uint32_t fnv1a(const char *s) {
	uint32_t hash = 2166136261u;
	const unsigned char *p;

	for (p = (const unsigned char *)s; *p != '\0'; p++) {
		hash ^= *p;        /* inject the byte into the low bits */
		hash *= 16777619u; /* multiply smears it across all 32 bits */
	}
	return hash;
}
```

The XOR pokes a byte into the state; the multiply (by an odd, empirically
chosen prime) smears its influence across the whole word before the next
byte lands. The constants are standardized — every FNV-1a implementation
on earth agrees — which is exactly why the tests can check your hash
against published vectors.

Two C details are load-bearing, and both are in the code above:

- **`uint32_t`, not `int`.** Unsigned overflow wraps by definition — that's
  the "modulo 2³²" the algorithm needs, for free. Signed overflow is
  *undefined behavior*, and a compiler is entitled to do something surprising
  with it.
- **`unsigned char`, not `char`.** Plain `char` may be signed, so a byte
  like `0xE9` would promote to a *negative* int (`0xFFFFFFE9`) and the XOR
  would corrupt bits far beyond the one byte it was supposed to touch.

(If you want the full bit-level story of *why* these two constants and what
the sign-extension bug does to your buckets, this platform's Build a Hash
Map course dissects it byte by byte.)

Step two folds the hash into a bucket index: `fnv1a(key) % nbuckets`.

## Collisions are the design, not the exception

Four billion hashes squeezed into 8 buckets means different keys *will*
share a bucket. **Separate chaining** absorbs that: each bucket holds the
head of a linked list of entries, and lookup walks the short chain
comparing actual keys.

Here, in a 4-bucket map, `"lex"` and `"emit"` both fold into bucket 2, so
that chain holds both — `"emit"` went in last, and a prepend leaves it at
the head. `"ast"` sits alone in bucket 3, and empty buckets are just NULL:

```
buckets
┌───┐
│ 0 │──▶ NULL
├───┤
│ 1 │──▶ NULL
├───┤
│ 2 │──▶ ["emit" = 2] ──▶ ["lex" = 4] ──▶ NULL
├───┤
│ 3 │──▶ ["ast" = 9] ──▶ NULL
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
  node — track it as you walk, and mind the head-of-chain case. (The
  slick C idiom is a **pointer-to-pointer** walk: keep a `struct hm_entry
  **pp` pointing at the *pointer that points to* the current node, and the
  head case stops being special — `*pp = (*pp)->next` unlinks either way.)

Two C-only responsibilities the garbage-collected languages hide from you:

- **The map owns its keys.** The caller's `const char *key` might be a
  stack buffer that's gone next line. `strdup` it on insert, `free` it on
  delete, and free every key plus every node in `hm_free`. One catch:
  `strdup` is a POSIX function, not ISO C, so under the `-std=c17` this
  course compiles with it stays hidden and you'd get an
  implicit-declaration error. The starter opens with
  `#define _POSIX_C_SOURCE 200809L` (before any `#include`) to ask for it
  — or skip `strdup` entirely and copy the key yourself:
  `malloc(strlen(key) + 1)` then `strcpy`, which is all `strdup` does.
- **Compare with `strcmp`, never `==`.** `key_a == key_b` on two `char *`
  asks "is this the same *address*?", not "is this the same *text*?" — it
  will appear to work in toy tests (where string literals get pooled) and
  then fail on real, heap-allocated keys.

Watch `put("dot", 7)` landing on a chained bucket — walk the whole chain first (no match), then prepend at the head:

```d2
direction: right

code: "" {
  grid-columns: 1
  grid-gap: 0
  l0: 'put("dot", 7)' {
    shape: oval
    height: 36
    style.stroke: "#d97706"
    style.stroke-width: 3
    style.font: mono
  }
  l1: "idx = hash(k) % len(buckets)" {
    height: 27
    style.stroke: "#9ca3af"
    style.font: mono
  }
  l2: "for node in buckets[idx]:" {
    height: 27
    style.stroke: "#9ca3af"
    style.font: mono
  }
  l3: "  if node.key == k: update" {
    height: 27
    style.stroke: "#9ca3af"
    style.font: mono
  }
  l4: "no match: prepend (k, v)" {
    height: 27
    style.stroke: "#9ca3af"
    style.font: mono
  }
}

buckets: {
  shape: sql_table
  style.font-size: 14
  "0": "∅"
  "1": "∅"
  "2": "•"
  "3": "∅"
  "4": "∅"
  "5": "∅"
}

chain: "" {
  grid-rows: 1
  grid-gap: 24
  style.stroke: transparent
  style.fill: transparent

  e3: "" {
    shape: oval
    width: 108
    height: 58
    style.stroke-dash: 4
    style.stroke: "#9ca3af"
  }
  e1: '("ada", 1)' { shape: oval; width: 108; height: 58 }
  e2: '("bob", 4)' { shape: oval; width: 108; height: 58 }
  nil: "NULL" { shape: text; width: 48; height: 58 }

  e3 -> e1
  e1 -> e2
  e2 -> nil
}

buckets -> chain: { style.opacity: 0 }
buckets."2" -> chain.e3

steps: {
  'hash("dot") = 0x9c…4a — mod 6 → bucket 2': {
    (buckets."2" -> chain.e3)[0].style.stroke: "#d97706"
    (buckets."2" -> chain.e3)[0].style.stroke-width: 3
    code.l1.style.stroke: "#d97706"
    code.l1.style.stroke-width: 2
    code.l1.style.bold: true
  }
  '"ada" ≠ "dot" — keep walking': {
    chain.e1.style.stroke: "#d97706"
    chain.e1.style.stroke-width: 3
    code.l1.style.stroke: "#9ca3af"
    code.l1.style.stroke-width: 1
    code.l1.style.bold: false
    code.l3.style.stroke: "#d97706"
    code.l3.style.stroke-width: 2
    code.l3.style.bold: true
  }
  '"bob" ≠ "dot" — no match anywhere': {
    chain.e1.style.stroke: "#16a34a"
    chain.e1.style.stroke-width: 2
    chain.e2.style.stroke: "#d97706"
    chain.e2.style.stroke-width: 3
  }
  'no match → prepend: ("dot", 7) is the new head': {
    chain.e2.style.stroke: "#16a34a"
    chain.e2.style.stroke-width: 2
    chain.e3.label: '("dot", 7)'
    chain.e3.style.stroke: "#16a34a"
    chain.e3.style.stroke-width: 3
    chain.e3.style.stroke-dash: 0
    code.l0.style.stroke: "#16a34a"
    code.l0.style.stroke-width: 2
    code.l3.style.stroke: "#9ca3af"
    code.l3.style.stroke-width: 1
    code.l3.style.bold: false
    code.l4.style.stroke: "#d97706"
    code.l4.style.stroke-width: 2
    code.l4.style.bold: true
  }
}
```

## Load factor: your amortized argument returns

Chains only stay short if there are enough buckets. The **load factor** —
entries ÷ buckets — measures crowding. Past a threshold (0.75 is the
classic), allocate double the buckets and **rehash**: every entry's home
is `hash % nbuckets`, and nbuckets just changed, so every entry must be
re-placed. A resize is O(n) — and it's fine, for precisely the reason
`array_push` was fine in lesson one: doubling makes resizes geometrically
rarer, so the cost amortizes to O(1) per insert. One structure's trick
becomes another structure's foundation. (You can even rehash without a
single `malloc` of a new entry: the nodes already exist, so just relink
them into the new bucket array.)

## Challenge: A Chained Hash Map {#hashmap points=20}

Build the whole thing: FNV-1a (keep the exact `fnv1a` name — the tests
check it against published vectors), separate chaining with the starter's
`hm_entry` struct, put/get/delete/len, and growth: when a `hm_put` of a
**new** key would push the load factor over 0.75, double the bucket count
and rehash every entry before inserting. `hm_free` must free every entry
and every strdup'd key — the tests are built to run clean under a leak
checker.

### Starter

```c
#define _POSIX_C_SOURCE 200809L /* expose strdup under -std=c17 (see above) */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* hm_entry is one key/value pair in a bucket's chain. */
struct hm_entry {
	char *key;   /* owned by the map (strdup'd on insert) */
	int value;
	struct hm_entry *next;
};

/* hashmap is a separately-chained map with power-of-two buckets. */
struct hashmap {
	struct hm_entry **buckets;
	size_t nbuckets;
	size_t size;
};

/* hm_init prepares an empty map with 8 buckets, all NULL. */
void hm_init(struct hashmap *m) {
	/* TODO: calloc 8 buckets */
	(void)m;
}

/* fnv1a is the 32-bit FNV-1a hash of s. */
uint32_t fnv1a(const char *s) {
	/* TODO: basis 2166136261u; per byte (as unsigned char!): XOR, then
	   multiply by 16777619u */
	(void)s;
	return 0;
}

/* hm_put inserts or overwrites key. Overwriting never changes size. If
   inserting a NEW key would make size/nbuckets exceed 0.75, first double
   the bucket count and rehash everything. The map copies the key. */
void hm_put(struct hashmap *m, const char *key, int value) {
	/* TODO */
	(void)m;
	(void)key;
	(void)value;
}

/* hm_get writes the value for key into *value_out and returns 1, or
   returns 0 if the key is absent. */
int hm_get(const struct hashmap *m, const char *key, int *value_out) {
	/* TODO */
	(void)m;
	(void)key;
	(void)value_out;
	return 0;
}

/* hm_delete removes key, returning 1 if it was present. It frees the
   entry and its key. */
int hm_delete(struct hashmap *m, const char *key) {
	/* TODO */
	(void)m;
	(void)key;
	return 0;
}

/* hm_len is the number of live entries. */
size_t hm_len(const struct hashmap *m) {
	/* TODO */
	(void)m;
	return 0;
}

/* hm_free releases every entry, every key, and the bucket array. */
void hm_free(struct hashmap *m) {
	/* TODO */
	(void)m;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

struct hm_entry {
	char *key;
	int value;
	struct hm_entry *next;
};

struct hashmap {
	struct hm_entry **buckets;
	size_t nbuckets;
	size_t size;
};

void hm_init(struct hashmap *m);
uint32_t fnv1a(const char *s);
void hm_put(struct hashmap *m, const char *key, int value);
int hm_get(const struct hashmap *m, const char *key, int *value_out);
int hm_delete(struct hashmap *m, const char *key);
size_t hm_len(const struct hashmap *m);
void hm_free(struct hashmap *m);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static void check_hash(const char *s, uint32_t want, int *ok) {
	uint32_t got = fnv1a(s);
	if (got != want) {
		printf("    fnv1a(\"%s\") = 0x%08x, wanted 0x%08x\n",
		       s, (unsigned)got, (unsigned)want);
		*ok = 0;
	}
}

static void test_fnv1a_vectors(void) {
	/* Published FNV-1a 32-bit test vectors. */
	int ok = 1;
	check_hash("", 0x811c9dc5u, &ok);
	check_hash("a", 0xe40c292cu, &ok);
	check_hash("hello", 0x4f9f2cabu, &ok);
	check_hash("user:42", 0x2f6b7b82u, &ok);
	check(ok, "test_fnv1a_vectors");
}

static void test_put_get(void) {
	struct hashmap m;
	int v = 0, ok = 1;

	hm_init(&m);
	if (hm_get(&m, "missing", &v)) {
		printf("    hm_get(\"missing\") on an empty map claimed the key exists\n");
		ok = 0;
	}
	hm_put(&m, "a", 1);
	hm_put(&m, "b", 2);
	if (!hm_get(&m, "a", &v)) {
		printf("    hm_get(\"a\") found nothing after hm_put(\"a\", 1)\n");
		ok = 0;
	} else if (v != 1) {
		printf("    hm_get(\"a\") wrote %d, wanted 1\n", v);
		ok = 0;
	}
	if (!hm_get(&m, "b", &v)) {
		printf("    hm_get(\"b\") found nothing after hm_put(\"b\", 2)\n");
		ok = 0;
	} else if (v != 2) {
		printf("    hm_get(\"b\") wrote %d, wanted 2\n", v);
		ok = 0;
	}
	if (hm_len(&m) != 2) {
		printf("    hm_len = %zu after 2 distinct puts, wanted 2\n", hm_len(&m));
		ok = 0;
	}
	check(ok, "test_put_get");
	hm_free(&m);
}

static void test_overwrite(void) {
	struct hashmap m;
	int v = 0, ok = 1;

	hm_init(&m);
	hm_put(&m, "k", 1);
	hm_put(&m, "k", 2);
	if (!hm_get(&m, "k", &v)) {
		printf("    hm_get(\"k\") found nothing after two puts of \"k\"\n");
		ok = 0;
	} else if (v != 2) {
		printf("    hm_get(\"k\") wrote %d, wanted 2 (the second put wins)\n", v);
		ok = 0;
	}
	if (hm_len(&m) != 1) {
		printf("    hm_len = %zu after putting \"k\" twice, wanted 1 (overwrite, not insert)\n",
		       hm_len(&m));
		ok = 0;
	}
	check(ok, "test_overwrite_keeps_size");
	hm_free(&m);
}

static void test_key_is_copied(void) {
	struct hashmap m;
	char buf[16];
	int v = 0, ok = 1;

	hm_init(&m);
	strcpy(buf, "scratch");
	hm_put(&m, buf, 99);
	/* Scribble over the caller's buffer: a map that stored the pointer
	   instead of copying the key now has a corrupted key. */
	strcpy(buf, "XXXXXXX");
	if (!hm_get(&m, "scratch", &v)) {
		printf("    \"scratch\" vanished after the caller reused their buffer — did hm_put store the pointer instead of copying the key?\n");
		ok = 0;
	} else if (v != 99) {
		printf("    hm_get(\"scratch\") wrote %d, wanted 99\n", v);
		ok = 0;
	}
	check(ok, "test_key_is_copied");
	hm_free(&m);
}

static void test_delete(void) {
	struct hashmap m;
	char key[32];
	int v = 0, ok = 1, i;

	hm_init(&m);
	for (i = 0; i < 20; i++) {
		sprintf(key, "key-%d", i);
		hm_put(&m, key, i);
	}
	if (!hm_delete(&m, "key-7")) {
		printf("    hm_delete(\"key-7\") returned 0, wanted 1 (the key was present)\n");
		ok = 0;
	}
	if (hm_delete(&m, "key-7")) { /* second delete must miss */
		printf("    deleting \"key-7\" twice claimed to succeed twice\n");
		ok = 0;
	}
	if (hm_delete(&m, "never-existed")) {
		printf("    hm_delete(\"never-existed\") returned 1, wanted 0\n");
		ok = 0;
	}
	if (hm_get(&m, "key-7", &v)) {
		printf("    \"key-7\" is still readable after being deleted\n");
		ok = 0;
	}
	if (hm_len(&m) != 19) {
		printf("    hm_len = %zu after 20 puts and 1 delete, wanted 19\n", hm_len(&m));
		ok = 0;
	}
	/* Every other key must have survived the unlink. */
	for (i = 0; i < 20 && ok; i++) {
		if (i == 7) {
			continue;
		}
		sprintf(key, "key-%d", i);
		if (!hm_get(&m, key, &v)) {
			printf("    \"%s\" vanished when \"key-7\" was deleted (unlinked the wrong entry?)\n", key);
			ok = 0;
		} else if (v != i) {
			printf("    hm_get(\"%s\") wrote %d, wanted %d\n", key, v, i);
			ok = 0;
		}
	}
	check(ok, "test_delete");
	hm_free(&m);
}

static void test_growth(void) {
	const int n = 100;
	struct hashmap m;
	char key[32];
	int v = 0, ok = 1, i;
	size_t total = 0, b;

	hm_init(&m);
	if (m.nbuckets != 8) {
		printf("    nbuckets = %zu after hm_init, wanted 8\n", m.nbuckets);
		ok = 0;
	}
	for (i = 0; i < n; i++) {
		sprintf(key, "key-%d", i);
		hm_put(&m, key, i * 10);
	}
	if (m.nbuckets <= 8) { /* the map never grew */
		printf("    nbuckets = %zu after %d puts, wanted > 8 (the map never grew)\n",
		       m.nbuckets, n);
		ok = 0;
	}
	if ((double)m.size / (double)m.nbuckets > 0.75) {
		printf("    load factor = %zu/%zu = %.2f, wanted <= 0.75\n",
		       m.size, m.nbuckets, (double)m.size / (double)m.nbuckets);
		ok = 0;
	}
	/* Chains must be consistent: total chained entries == size. */
	for (b = 0; b < m.nbuckets; b++) {
		struct hm_entry *e;
		for (e = m.buckets[b]; e != NULL; e = e->next) {
			total++;
		}
	}
	if (total != m.size || m.size != (size_t)n) {
		printf("    %zu entries chained in the buckets, size=%zu, after %d puts (all three should be %d)\n",
		       total, m.size, n, n);
		ok = 0;
	}
	/* And every key must still resolve after the rehashes. */
	for (i = 0; i < n && ok; i++) {
		sprintf(key, "key-%d", i);
		if (!hm_get(&m, key, &v)) {
			printf("    \"%s\" vanished during a rehash\n", key);
			ok = 0;
		} else if (v != i * 10) {
			printf("    hm_get(\"%s\") wrote %d, wanted %d\n", key, v, i * 10);
			ok = 0;
		}
	}
	check(ok, "test_growth_and_rehash");
	hm_free(&m);
}

int main(void) {
	test_fnv1a_vectors();
	test_put_get();
	test_overwrite();
	test_key_is_copied();
	test_delete();
	test_growth();
	return failed;
}
```

# Lesson: Graphs and Breadth-First Search {#graphs-and-traversal}

Arrays, heaps, and maps organize *values*. Graphs organize
*relationships*: files include files, servers link to servers, tasks block
tasks. A graph is just vertices plus edges — and nearly every "how do
these things connect?" question reduces to a graph traversal you can now
build from parts you own.

## Representing a graph

Number the vertices 0..n−1 and pick a representation:

- **Adjacency matrix** — an n×n grid of bytes. O(1) edge checks, but n²
  memory even when almost no edges exist. Real graphs are usually sparse;
  the matrix is usually waste.
- **Adjacency list** — for each vertex, the list of its neighbors: an
  array of growable arrays (lesson one, doing real work). Memory
  proportional to what actually exists (n + edges). This is the default,
  and it's what you'll use.

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

Watch the ripple — amber is the ring just discovered, green is finished:

```d2
direction: right

panel: "" {
  style.stroke: transparent
  style.fill: transparent
  grid-columns: 1
  grid-gap: 12

  code: "" {
    grid-columns: 1
    grid-gap: 0
    l1: "q = [start]; mark start" {
      height: 30
      style.stroke: "#9ca3af"
      style.font: mono
    }
    l2: "while q not empty:" {
      height: 30
      style.stroke: "#9ca3af"
      style.font: mono
    }
    l3: "v = q.pop_front()" {
      height: 30
      style.stroke: "#9ca3af"
      style.font: mono
    }
    l4: "for w in adj[v]:" {
      height: 30
      style.stroke: "#9ca3af"
      style.font: mono
    }
    l5: "if unseen: mark, push w" {
      height: 30
      style.stroke: "#9ca3af"
      style.font: mono
    }
  }

  q: "q: ∅" {
    shape: queue
    height: 60
  }
}

A: "A" { shape: circle; width: 56; height: 56 }
B: "B" { shape: circle; width: 56; height: 56 }
C: "C" { shape: circle; width: 56; height: 56 }
D: "D" { shape: circle; width: 56; height: 56 }
E: "E" { shape: circle; width: 56; height: 56 }
F: "F" { shape: circle; width: 56; height: 56 }

panel -- A: "" { style.opacity: 0 }

A -- B
A -- C
B -- D
C -- D
C -- E
D -- F
E -- F

steps: {
  "start: mark A as seen the moment it is enqueued": {
    A.style.stroke: "#d97706"
    A.style.stroke-width: 3
    panel.q.label: "q: A"
    panel.code.l1.style.stroke: "#d97706"
    panel.code.l1.style.stroke-width: 2
    panel.code.l1.style.bold: true
  }
  "pop A — its unseen neighbors B, C enter at dist 1": {
    A.style.stroke: "#16a34a"
    A.style.stroke-width: 2
    B.style.stroke: "#d97706"
    B.style.stroke-width: 3
    C.style.stroke: "#d97706"
    C.style.stroke-width: 3
    panel.q.label: "q: B C"
    panel.code.l1.style.stroke: "#9ca3af"
    panel.code.l1.style.stroke-width: 1
    panel.code.l1.style.bold: false
    panel.code.l3.style.stroke: "#d97706"
    panel.code.l3.style.stroke-width: 2
    panel.code.l3.style.bold: true
  }
  "pop B, then C — D and E marked and pushed, dist 2": {
    B.style.stroke: "#16a34a"
    B.style.stroke-width: 2
    C.style.stroke: "#16a34a"
    C.style.stroke-width: 2
    D.style.stroke: "#d97706"
    D.style.stroke-width: 3
    E.style.stroke: "#d97706"
    E.style.stroke-width: 3
    panel.q.label: "q: D E"
    panel.code.l3.style.stroke: "#9ca3af"
    panel.code.l3.style.stroke-width: 1
    panel.code.l3.style.bold: false
    panel.code.l5.style.stroke: "#d97706"
    panel.code.l5.style.stroke-width: 2
    panel.code.l5.style.bold: true
  }
  "pop D, E — F pushed once; E sees F already marked": {
    D.style.stroke: "#16a34a"
    D.style.stroke-width: 2
    E.style.stroke: "#16a34a"
    E.style.stroke-width: 2
    F.style.stroke: "#d97706"
    F.style.stroke-width: 3
    panel.q.label: "q: F"
    panel.code.l5.style.stroke: "#9ca3af"
    panel.code.l5.style.stroke-width: 1
    panel.code.l5.style.bold: false
    panel.code.l4.style.stroke: "#d97706"
    panel.code.l4.style.stroke-width: 2
    panel.code.l4.style.bold: true
  }
  "q empty — each dist is the fewest hops possible": {
    F.style.stroke: "#16a34a"
    F.style.stroke-width: 2
    panel.q.label: "q: ∅"
    panel.code.l4.style.stroke: "#9ca3af"
    panel.code.l4.style.stroke-width: 1
    panel.code.l4.style.bold: false
    panel.code.l2.style.stroke: "#d97706"
    panel.code.l2.style.stroke-width: 2
    panel.code.l2.style.bold: true
  }
}
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

Because every vertex is enqueued at most once, the queue in C is beautifully
simple: one `malloc`'d array of n ints plus a `head` and `tail` index. No
wraparound, no ring buffer, no resizing — enqueue writes at `tail++`,
dequeue reads at `head++`, and the queue is empty when `head == tail`.

## Challenge: Shortest Paths by BFS {#bfs points=15}

Given `n` vertices, an undirected edge list, and a source, fill `dist` so
that `dist[i]` is the number of edges on the shortest path from `src` to
`i`, or −1 if `i` is unreachable. The caller owns `dist` (it has room for
n ints). Build the adjacency structure first (both directions per edge!),
then BFS with a queue, marking on enqueue. Free everything you allocate.

### Starter

```c
#include <stdlib.h>

/* bfs_distances fills dist[0..n-1] with the shortest-path edge count from
   src to each vertex of an undirected graph, or -1 if unreachable.
   Vertices are 0..n-1; each edge {u, v} connects u and v both ways. */
void bfs_distances(size_t n, const size_t (*edges)[2], size_t nedges,
                   size_t src, int *dist) {
	/* TODO: build adjacency lists (edges[i][0] <-> edges[i][1]), set every
	   dist to -1, then run a queue-driven BFS from src with dist[src] = 0 */
	(void)n;
	(void)edges;
	(void)nedges;
	(void)src;
	(void)dist;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>

void bfs_distances(size_t n, const size_t (*edges)[2], size_t nedges,
                   size_t src, int *dist);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static int distances_are(size_t n, const size_t (*edges)[2], size_t nedges,
                         size_t src, const int *want) {
	int dist[16];
	size_t i;

	for (i = 0; i < 16; i++) {
		dist[i] = -999;
	}
	bfs_distances(n, edges, nedges, src, dist);
	for (i = 0; i < n; i++) {
		if (dist[i] != want[i]) {
			printf("    %zu vertices, %zu edges, src=%zu: dist[%zu] = %d, wanted %d (-999 means the slot was never written)\n",
			       n, nedges, src, i, dist[i], want[i]);
			return 0;
		}
	}
	return 1;
}

int main(void) {
	/* 0-1-2-3-4 in a line */
	size_t line[][2] = {{0, 1}, {1, 2}, {2, 3}, {3, 4}};
	int line_from0[] = {0, 1, 2, 3, 4};
	int line_from2[] = {2, 1, 0, 1, 2};

	size_t star[][2] = {{0, 1}, {0, 2}, {0, 3}};
	int star_center[] = {0, 1, 1, 1};
	int star_leaf[] = {1, 2, 2, 0};

	size_t split[][2] = {{0, 1}, {2, 3}};
	int split_want[] = {0, 1, -1, -1};

	size_t triangle[][2] = {{0, 1}, {1, 2}, {2, 0}};
	int triangle_want[] = {0, 1, 1};

	/* Long way around (0-1-2-3) vs the direct edge 0-3. */
	size_t shortcut[][2] = {{0, 1}, {1, 2}, {2, 3}, {0, 3}};
	int shortcut_want[] = {0, 1, 2, 1};

	size_t dup[][2] = {{0, 1}, {0, 1}, {1, 0}};
	int dup_want[] = {0, 1};

	int single_want[] = {0};

	check(distances_are(1, NULL, 0, 0, single_want), "test_single_vertex");
	check(distances_are(5, line, 4, 0, line_from0), "test_line_from_0");
	check(distances_are(5, line, 4, 2, line_from2), "test_line_from_2");
	check(distances_are(4, star, 3, 0, star_center), "test_star_center");
	check(distances_are(4, star, 3, 3, star_leaf), "test_star_leaf");
	check(distances_are(4, split, 2, 0, split_want), "test_disconnected");
	check(distances_are(3, triangle, 3, 0, triangle_want),
	      "test_cycle_terminates");
	check(distances_are(4, shortcut, 4, 0, shortcut_want),
	      "test_shortcut_wins");
	check(distances_are(2, dup, 3, 0, dup_want), "test_duplicate_edges");

	return failed;
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

Watch a full run on the build graph above — amber means ready (indegree 0), and proto is the only vertex that starts that way:

```d2
direction: right

f: "" {
  grid-columns: 1
  grid-gap: 10
  style.stroke: transparent
  style.fill: transparent

  panel: "" {
    grid-rows: 1
    grid-gap: 16
    code: "" {
      grid-columns: 1
      grid-gap: 0
    l1: " q = nodes with indeg 0" { height: 22 }
    l2: " while q not empty:" { height: 22 }
    l3: "   v = q.pop(); order += v" { height: 22 }
    l4: "   for w in out[v]: indeg[w]-=1" { height: 22 }
    l5: "     if indeg[w]==0: q.push(w)" { height: 22 }
    l1.style.stroke: "#9ca3af"
    l1.style.font: mono
    l1.style.font-size: 13
    l1.label.near: center-left
    l2.style.stroke: "#9ca3af"
    l2.style.font: mono
    l2.style.font-size: 13
    l2.label.near: center-left
    l3.style.stroke: "#9ca3af"
    l3.style.font: mono
    l3.style.font-size: 13
    l3.label.near: center-left
    l4.style.stroke: "#9ca3af"
    l4.style.font: mono
    l4.style.font-size: 13
    l4.label.near: center-left
    l5.style.stroke: "#9ca3af"
    l5.style.font: mono
    l5.style.font-size: 13
    l5.label.near: center-left
    }
    rq: "ready: [ proto ]" {
      shape: queue
      width: 240
      height: 60
    }
  }

  g: "" {
    direction: right
    proto: "proto (0)" { shape: package; width: 100; height: 52 }
    db: "db (1)" { shape: package; width: 100; height: 52 }
    api: "api (2)" { shape: package; width: 100; height: 52 }
    web: "web (1)" { shape: package; width: 100; height: 52 }
    cli: "cli (1)" { shape: package; width: 100; height: 52 }

    proto -> db
    proto -> api
    db -> api
    api -> web
    api -> cli

    proto.style.stroke: "#d97706"
    proto.style.stroke-width: 3
  }
}

f.panel.code.l1.style.stroke: "#d97706"
f.panel.code.l1.style.stroke-width: 2
f.panel.code.l1.style.bold: true

steps: {
  "order: [proto] — db is freed, api still waits": {
    f.panel.code.l1.style.stroke: "#9ca3af"
    f.panel.code.l1.style.stroke-width: 1
    f.panel.code.l1.style.bold: false
    f.panel.code.l3.style.stroke: "#d97706"
    f.panel.code.l3.style.stroke-width: 2
    f.panel.code.l3.style.bold: true
    f.panel.rq.label: "ready: [ db ]"
    f.g.proto.label: "proto ✓1"
    f.g.proto.style.stroke: "#9ca3af"
    f.g.proto.style.stroke-width: 2
    f.g.proto.style.stroke-dash: 4
    f.g.proto.style.font-color: "#9ca3af"
    f.g.(proto -> db)[0].style.stroke-dash: 4
    f.g.(proto -> db)[0].style.stroke: "#9ca3af"
    f.g.(proto -> api)[0].style.stroke-dash: 4
    f.g.(proto -> api)[0].style.stroke: "#9ca3af"
    f.g.db.label: "db (0)"
    f.g.db.style.stroke: "#d97706"
    f.g.db.style.stroke-width: 3
    f.g.api.label: "api (1)"
  }
  "order: [proto db] — api finally hits 0": {
    f.panel.code.l3.style.stroke: "#9ca3af"
    f.panel.code.l3.style.stroke-width: 1
    f.panel.code.l3.style.bold: false
    f.panel.code.l4.style.stroke: "#d97706"
    f.panel.code.l4.style.stroke-width: 2
    f.panel.code.l4.style.bold: true
    f.panel.rq.label: "ready: [ api ]"
    f.g.db.label: "db ✓2"
    f.g.db.style.stroke: "#9ca3af"
    f.g.db.style.stroke-width: 2
    f.g.db.style.stroke-dash: 4
    f.g.db.style.font-color: "#9ca3af"
    f.g.(db -> api)[0].style.stroke-dash: 4
    f.g.(db -> api)[0].style.stroke: "#9ca3af"
    f.g.api.label: "api (0)"
    f.g.api.style.stroke: "#d97706"
    f.g.api.style.stroke-width: 3
  }
  "order: [proto db api] — web and cli BOTH ready": {
    f.panel.code.l4.style.stroke: "#9ca3af"
    f.panel.code.l4.style.stroke-width: 1
    f.panel.code.l4.style.bold: false
    f.panel.code.l5.style.stroke: "#d97706"
    f.panel.code.l5.style.stroke-width: 2
    f.panel.code.l5.style.bold: true
    f.panel.rq.label: "ready: [ web, cli ]"
    f.g.api.label: "api ✓3"
    f.g.api.style.stroke: "#9ca3af"
    f.g.api.style.stroke-width: 2
    f.g.api.style.stroke-dash: 4
    f.g.api.style.font-color: "#9ca3af"
    f.g.(api -> web)[0].style.stroke-dash: 4
    f.g.(api -> web)[0].style.stroke: "#9ca3af"
    f.g.(api -> cli)[0].style.stroke-dash: 4
    f.g.(api -> cli)[0].style.stroke: "#9ca3af"
    f.g.web.label: "web (0)"
    f.g.web.style.stroke: "#d97706"
    f.g.web.style.stroke-width: 3
    f.g.cli.label: "cli (0)"
    f.g.cli.style.stroke: "#d97706"
    f.g.cli.style.stroke-width: 3
  }
  "order: [proto db api web] — either was valid": {
    f.panel.code.l5.style.stroke: "#9ca3af"
    f.panel.code.l5.style.stroke-width: 1
    f.panel.code.l5.style.bold: false
    f.panel.code.l3.style.stroke: "#d97706"
    f.panel.code.l3.style.stroke-width: 2
    f.panel.code.l3.style.bold: true
    f.panel.rq.label: "ready: [ cli ]"
    f.g.web.label: "web ✓4"
    f.g.web.style.stroke: "#9ca3af"
    f.g.web.style.stroke-width: 2
    f.g.web.style.stroke-dash: 4
    f.g.web.style.font-color: "#9ca3af"
  }
  "order: [proto db api web cli] — queue empty, done": {
    f.panel.code.l3.style.stroke: "#9ca3af"
    f.panel.code.l3.style.stroke-width: 1
    f.panel.code.l3.style.bold: false
    f.panel.code.l2.style.stroke: "#d97706"
    f.panel.code.l2.style.stroke-width: 2
    f.panel.code.l2.style.bold: true
    f.panel.rq.label: "ready: [ ]"
    f.g.cli.label: "cli ✓5"
    f.g.cli.style.stroke: "#9ca3af"
    f.g.cli.style.stroke-width: 2
    f.g.cli.style.stroke-dash: 4
    f.g.cli.style.font-color: "#9ca3af"
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

In C there's one wrinkle worth naming now, before it bites you in the
final challenge: a heap of *strings* can't compare with `<`. Your
`heap_push`/`heap_pop` become a heap of `const char *`, and every
comparison that was `a < b` becomes `strcmp(a, b) < 0`. The sift-up and
sift-down logic doesn't change by a single line — only the comparison
does. That is the moment a data structure stops being a thing you wrote
once and starts being a tool.

# Final Challenge: The Build Scheduler {#task-scheduler points=50}

Time to cash in the whole course. You're given task names and dependency
pairs `{a, b}` meaning *a must run before b*. Produce a schedule — a
permutation of the tasks satisfying every constraint — or report that the
dependencies contain a cycle.

To make the answer unique (and gradable): **among all valid schedules,
return the lexicographically smallest** — at every step, of all currently
runnable tasks, run the `strcmp`-first one. The deterministic-order
requirement is enforced at a size where "scan all tasks for the smallest
ready one each round" is O(n²) and re-sorting every round is worse; the
heap is the right tool.

Everything you've built has a seat:

- a **hash map** from task name to index — C has no dictionary, so the map
  you wrote two lessons ago *is* the lookup (an O(n) `strcmp` scan per
  name would turn the build step into the slow part),
- **adjacency lists** of each task's dependents (growable arrays),
- an **indegree count** per task,
- a **min-heap of `const char *`** as the ready set — your `minheap` with
  `int` swapped for `const char *` and `<` swapped for `strcmp(...) < 0`,
- **Kahn's loop**, counting processed tasks to detect cycles.

Fill `out[0..n-1]` with pointers **from the `tasks` array** (don't copy the
strings — the caller owns them and outlives you) and return 0. On a cycle,
return −1 and leave `out` alone. Task names are unique; every name in
`deps` appears in `tasks`; duplicate dependency pairs may appear. Free
everything you allocate, on both the success and the cycle path.

### Starter

```c
#include <stdlib.h>
#include <string.h>

/* schedule fills out[0..n-1] with the lexicographically smallest ordering
   of tasks in which every dependency pair (deps[i][0] before deps[i][1])
   is respected, and returns 0. If the dependencies contain a cycle it
   returns -1. out has room for n pointers; they point into tasks. */
int schedule(size_t n, const char **tasks, size_t ndeps,
             const char *(*deps)[2], const char **out) {
	/* TODO:
	     1. map each task name to its index (your hash map)
	     2. build adjacency lists and indegrees (deps[i][0] -> deps[i][1])
	     3. push every indegree-0 task name into a string min-heap
	     4. Kahn's loop: pop the smallest, append it to out, decrement each
	        successor's indegree, pushing any that reach 0
	     5. if you placed fewer than n tasks, there was a cycle: return -1
	   Free every allocation before you return, on both paths. */
	(void)n;
	(void)tasks;
	(void)ndeps;
	(void)deps;
	(void)out;
	return -1;
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int schedule(size_t n, const char **tasks, size_t ndeps,
             const char *(*deps)[2], const char **out);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* schedules_to asserts schedule() succeeds and produces exactly want. */
static int schedules_to(size_t n, const char **tasks, size_t ndeps,
                        const char *(*deps)[2], const char **want) {
	const char *out[256];
	size_t i;
	int rc;

	for (i = 0; i < 256; i++) {
		out[i] = NULL;
	}
	rc = schedule(n, tasks, ndeps, deps, out);
	if (rc != 0) {
		printf("    schedule() of %zu tasks / %zu deps returned %d, wanted 0 (there is no cycle here)\n",
		       n, ndeps, rc);
		return 0;
	}
	for (i = 0; i < n; i++) {
		if (out[i] == NULL) {
			printf("    out[%zu] was never written (wanted \"%s\")\n", i, want[i]);
			return 0;
		}
		if (strcmp(out[i], want[i]) != 0) {
			printf("    out[%zu] = \"%s\", wanted \"%s\"\n", i, out[i], want[i]);
			return 0;
		}
	}
	return 1;
}

/* detects_cycle asserts schedule() reports a cycle. */
static int detects_cycle(size_t n, const char **tasks, size_t ndeps,
                         const char *(*deps)[2]) {
	const char *out[256];
	int rc = schedule(n, tasks, ndeps, deps, out);
	if (rc != -1) {
		printf("    schedule() of %zu tasks / %zu deps returned %d, wanted -1 (these deps contain a cycle)\n",
		       n, ndeps, rc);
		return 0;
	}
	return 1;
}

static void test_no_deps_is_alphabetical(void) {
	const char *tasks[] = {"c", "a", "b"};
	const char *want[] = {"a", "b", "c"};
	check(schedules_to(3, tasks, 0, NULL, want),
	      "test_no_deps_is_alphabetical");
}

static void test_chain(void) {
	const char *tasks[] = {"a", "b", "c"};
	const char *deps[][2] = {{"c", "b"}, {"b", "a"}};
	const char *want[] = {"c", "b", "a"};
	check(schedules_to(3, tasks, 2, deps, want), "test_chain");
}

static void test_heap_beats_queue(void) {
	/* Ready set starts {b, c}; finishing b unlocks a, which must run
	   before c alphabetically. A FIFO ready set wrongly yields b, c, a. */
	const char *tasks[] = {"a", "b", "c"};
	const char *deps[][2] = {{"b", "a"}};
	const char *want[] = {"b", "a", "c"};
	check(schedules_to(3, tasks, 1, deps, want), "test_heap_beats_queue");
}

static void test_diamond(void) {
	const char *tasks[] = {"web", "db", "api", "proto", "cli"};
	const char *deps[][2] = {
		{"proto", "db"}, {"proto", "api"}, {"db", "api"},
		{"api", "web"}, {"api", "cli"},
	};
	const char *want[] = {"proto", "db", "api", "cli", "web"};
	check(schedules_to(5, tasks, 5, deps, want), "test_diamond");
}

static void test_duplicate_deps(void) {
	const char *tasks[] = {"a", "b"};
	const char *deps[][2] = {{"b", "a"}, {"b", "a"}};
	const char *want[] = {"b", "a"};
	check(schedules_to(2, tasks, 2, deps, want), "test_duplicate_deps");
}

static void test_cycle(void) {
	const char *tasks[] = {"a", "b", "c"};
	const char *deps[][2] = {{"a", "b"}, {"b", "c"}, {"c", "a"}};
	check(detects_cycle(3, tasks, 3, deps), "test_triangle_cycle");
}

static void test_self_cycle(void) {
	const char *tasks[] = {"a", "b"};
	const char *deps[][2] = {{"a", "a"}};
	check(detects_cycle(2, tasks, 1, deps), "test_self_cycle");
}

static void test_partial_cycle(void) {
	/* d is fine, but a and b deadlock each other (and block c). */
	const char *tasks[] = {"a", "b", "c", "d"};
	const char *deps[][2] = {{"a", "b"}, {"b", "a"}, {"b", "c"}};
	check(detects_cycle(4, tasks, 3, deps), "test_partial_cycle");
}

static void test_big_schedule(void) {
	/* task-000 .. task-199, each forced before the next. */
	enum { N = 200 };
	static char names[N][16];
	const char *tasks[N];
	const char *want[N];
	static const char *deps[N - 1][2];
	size_t i;

	for (i = 0; i < N; i++) {
		sprintf(names[i], "task-%03d", (int)i);
		want[i] = names[i];
		tasks[i] = names[N - 1 - i]; /* shuffled-ish input order */
	}
	for (i = 0; i + 1 < N; i++) {
		deps[i][0] = names[i];
		deps[i][1] = names[i + 1];
	}
	check(schedules_to(N, tasks, N - 1, deps, want), "test_big_schedule");
}

int main(void) {
	test_no_deps_is_alphabetical();
	test_chain();
	test_heap_beats_queue();
	test_diamond();
	test_duplicate_deps();
	test_cycle();
	test_self_cycle();
	test_partial_cycle();
	test_big_schedule();
	return failed;
}
```
