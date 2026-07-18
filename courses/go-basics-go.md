---
course: go-basics
title: Go Basics
language: go
description: Start from zero and learn the core of the Go language — variables, functions, control flow, slices, maps, pointers, structs, errors, and interfaces — by writing and testing real code.
duration_hours: 8
tags: [go, fundamentals]
extended_reading:
  - title: A Tour of Go
    url: https://go.dev/tour/
  - title: Effective Go
    url: https://go.dev/doc/effective_go
  - title: Go by Example
    url: https://gobyexample.com/
---

# Lesson: Hello, Go {#hello-go}

Every Go file starts by declaring which **package** it belongs to. Executable
programs live in `package main` and start at `func main()`; the challenges in
this course live in `package challenge`, and the grader runs your code with
`go test`, so you never need a `main` function here.

```go
package main

import "fmt"

func main() {
	fmt.Println("Hello, Go!")
}
```

Variables are declared with `var`, or with the short form `:=` inside
functions. Go is statically typed, but the compiler infers the type from the
right-hand side:

```go
var city string = "Oslo" // explicit type
count := 3               // inferred as int
pi := 3.14               // inferred as float64
ok := true               // inferred as bool
```

A variable declared without a value gets its type's **zero value**: `0` for
numbers, `""` for strings, `false` for bools. There are no uninitialized
variables in Go — this matters more than it sounds, and you'll lean on it
constantly.

Strings are joined with `+`, and `fmt.Sprintf` builds a string from a format
template without printing it:

```go
name := "Ada"
greeting := "Hello, " + name + "!"
same := fmt.Sprintf("Hello, %s!", name) // %s = insert a string here
```

One more piece for the challenge below: `==` compares two values, and `if`
runs a block only when its condition is true (no parentheses needed):

```go
if name == "" {
	// runs only when name is empty
}
```

The Control Flow lesson covers branching properly; this is all you need
for now.

Unused variables and unused imports are **compile errors** in Go, not
warnings. It feels strict at first; it keeps every file honest.

## Challenge: Greet {#greet points=5}

Implement `Greet(name string) string` so that it returns `"Hello, <name>!"`.
If `name` is the empty string, return `"Hello, Gopher!"` instead.

### Starter

```go
package challenge

func Greet(name string) string {
	// TODO: return "Hello, <name>!", or "Hello, Gopher!" for an empty name
	return ""
}
```

### Tests

```go
package challenge

import "testing"

func TestGreet(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"named", "Ada", "Hello, Ada!"},
		{"another name", "Linus", "Hello, Linus!"},
		{"empty falls back to Gopher", "", "Hello, Gopher!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Greet(c.in); got != c.want {
				t.Errorf("Greet(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
```

# Lesson: Functions and Multiple Returns {#functions}

A Go function declares its parameter types after the names, and its return
type after the parameter list:

```go
func add(a int, b int) int {
	return a + b
}

func addAll(a, b, c int) int { // consecutive params of one type: name them together
	return a + b + c
}
```

The signature feature you'll use most: functions can return **more than one
value**. Where other languages return a tuple or throw an exception, Go
functions simply return everything the caller needs:

```go
func divide(a, b int) (int, int) {
	return a / b, a % b // quotient and remainder
}

q, r := divide(7, 3) // q = 2, r = 1
```

Callers must do something with every returned value. To deliberately ignore
one, assign it to the **blank identifier** `_`:

```go
q, _ := divide(7, 3) // keep the quotient, drop the remainder
```

Multiple returns are the backbone of Go's error handling, which you'll meet
in a later lesson — a function returns `(result, error)` and the caller
checks both. Get comfortable with the shape now.

Integer division in Go truncates toward zero: `7 / 3` is `2`, and `7 % 3` is
`1`. This challenge sticks to positive numbers, so no surprises yet.

## Challenge: DivMod {#divmod points=10}

Implement `DivMod(a, b int) (int, int)` returning the quotient and remainder
of dividing `a` by `b`. You may assume `b` is never zero.

### Starter

```go
package challenge

func DivMod(a, b int) (int, int) {
	// TODO: return the quotient and the remainder
	return 0, 0
}
```

### Tests

```go
package challenge

import "testing"

func TestDivMod(t *testing.T) {
	cases := []struct {
		name           string
		a, b           int
		wantQ, wantR   int
	}{
		{"seven by three", 7, 3, 2, 1},
		{"exact division", 10, 5, 2, 0},
		{"smaller dividend", 3, 7, 0, 3},
		{"by one", 9, 1, 9, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q, r := DivMod(c.a, c.b)
			if q != c.wantQ || r != c.wantR {
				t.Errorf("DivMod(%d, %d) = (%d, %d), want (%d, %d)",
					c.a, c.b, q, r, c.wantQ, c.wantR)
			}
		})
	}
}
```

# Lesson: Control Flow {#control-flow}

Go has one loop keyword: `for`. It covers every looping shape you know from
other languages:

```go
for i := 0; i < 5; i++ { } // classic three-part loop

for count < 10 { }         // while-style: just a condition

for { }                    // infinite loop: break out when done
```

`if` needs no parentheses around the condition, and can start with a short
statement whose variable is scoped to the `if`/`else` chain:

```go
if n := len(input); n > 100 {
	fmt.Println("too long:", n)
}
```

`switch` in Go does **not** fall through by default — each case breaks on its
own, so you list the interesting cases without sprinkling `break`
everywhere. A `switch` with no expression is a cleaner way to write an
if/else-if chain:

```go
switch {
case score >= 90:
	grade = "A"
case score >= 80:
	grade = "B"
default:
	grade = "C"
}
```

One more tool for the challenge: `strconv.Itoa(n)` converts an int to its
decimal string (`strconv.Itoa(42)` is `"42"`), and appending to a slice looks
like `out = append(out, item)` — the next lesson digs into what that really
does.

## Challenge: FizzBuzz {#fizzbuzz points=10}

The classic. Implement `FizzBuzz(n int) []string` returning one entry per
number from 1 to `n`: `"Fizz"` for multiples of 3, `"Buzz"` for multiples of
5, `"FizzBuzz"` for multiples of both, and the number itself (as a string)
otherwise. For `n <= 0`, return an empty (or nil) slice.

### Starter

```go
package challenge

func FizzBuzz(n int) []string {
	// TODO: one entry per number from 1 to n
	return nil
}
```

### Tests

```go
package challenge

import "testing"

func TestFizzBuzz(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want []string
	}{
		{"first five", 5, []string{"1", "2", "Fizz", "4", "Buzz"}},
		{"fifteen ends in FizzBuzz", 15, []string{
			"1", "2", "Fizz", "4", "Buzz", "Fizz", "7", "8", "Fizz", "Buzz",
			"11", "Fizz", "13", "14", "FizzBuzz",
		}},
		{"one", 1, []string{"1"}},
		{"zero is empty", 0, nil},
		{"negative is empty", -3, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FizzBuzz(c.n)
			if len(got) != len(c.want) {
				t.Fatalf("FizzBuzz(%d) has %d entries, want %d: %v",
					c.n, len(got), len(c.want), got)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("FizzBuzz(%d)[%d] = %q, want %q", c.n, i, got[i], c.want[i])
				}
			}
		})
	}
}
```

# Lesson: Slices {#slices}

A slice is Go's workhorse sequence type: a lightweight view onto an
underlying array, carrying a pointer, a length, and a capacity. You'll use
slices where other languages use lists or dynamic arrays.

```go
nums := []int{1, 2, 3}      // slice literal
nums = append(nums, 4)      // append returns the (possibly new) slice
first := nums[0]            // index
part := nums[1:3]           // half-open range: elements 1 and 2
n := len(nums)              // length
```

Two things to internalize:

- **`append` returns a new slice value.** If the underlying array is full,
  `append` allocates a bigger one and copies. Always write
  `s = append(s, x)` — dropping the return value is a bug.
- **Slices share memory.** `part := nums[1:3]` does not copy; writing
  through `part` changes `nums` too. When a challenge asks for a *new*
  slice, build one (e.g. start with `make([]int, 0, len(in))` and append)
  rather than mutating the input.

`make([]int, length)` creates a slice of zeros at a given length;
`make([]int, 0, capacity)` creates an empty slice with room pre-reserved.
The idiomatic loop over a slice is `range`:

```go
for i, v := range nums {
	fmt.Println(i, v) // index and value
}
```

A `nil` slice is safe to `len()`, `range` over, and `append` to — another
place zero values quietly do the right thing.

## Challenge: Reverse {#reverse points=10}

Implement `Reverse(nums []int) []int` returning a **new** slice with the
elements in reverse order. The input slice must not be modified.

### Starter

```go
package challenge

func Reverse(nums []int) []int {
	// TODO: build and return a new, reversed slice — don't mutate nums
	return nil
}
```

### Tests

```go
package challenge

import "testing"

func TestReverse(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want []int
	}{
		{"basic", []int{1, 2, 3}, []int{3, 2, 1}},
		{"even length", []int{1, 2, 3, 4}, []int{4, 3, 2, 1}},
		{"single", []int{7}, []int{7}},
		{"empty", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Reverse(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("Reverse(%v) = %v, want %v", c.in, got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("Reverse(%v) = %v, want %v", c.in, got, c.want)
				}
			}
		})
	}
}

func TestReverseDoesNotMutateInput(t *testing.T) {
	in := []int{1, 2, 3}
	Reverse(in)
	for i, want := range []int{1, 2, 3} {
		if in[i] != want {
			t.Fatalf("input was mutated: %v", in)
		}
	}
}
```

# Lesson: Maps {#maps}

A map associates keys with values. Create one with `make` or a literal, and
index it like a slice:

```go
ages := map[string]int{"ada": 36}
ages["linus"] = 55         // insert or overwrite
n := ages["ada"]           // lookup: 36
missing := ages["nobody"]  // lookup miss: the zero value, 0
```

That last line is important: reading a missing key is **not** an error — you
get the value type's zero value. When you need to distinguish "stored zero"
from "not present", use the two-value form, conventionally named `ok`:

```go
if age, ok := ages["nobody"]; ok {
	fmt.Println("found:", age)
}
```

`delete(ages, "ada")` removes a key; `len(ages)` counts entries; `range`
iterates (in deliberately **randomized** order — never depend on map order).
And one gotcha worth knowing early: reading from a `nil` map is fine, but
writing to one panics — create maps with `make` or a literal before
inserting.

For the challenge you'll also want `strings.Fields`, which splits a string
around any run of whitespace:

```go
strings.Fields("the quick  brown") // []string{"the", "quick", "brown"}
```

The counting pattern below is one of the most common map idioms in Go — a
lookup miss yields `0`, so you can increment without checking existence:

```go
counts := make(map[string]int)
for _, w := range words {
	counts[w]++ // works even the first time we see w
}
```

## Challenge: Word Count {#word-count points=10}

Implement `WordCount(s string) map[string]int` returning how many times each
whitespace-separated word appears in `s`. Words are case-sensitive: `"Go"`
and `"go"` are different words.

### Starter

```go
package challenge

func WordCount(s string) map[string]int {
	// TODO: split s into words and count each one
	return nil
}
```

### Tests

```go
package challenge

import "testing"

func TestWordCount(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]int
	}{
		{"repeated word", "the quick the", map[string]int{"the": 2, "quick": 1}},
		{"case sensitive", "Go go", map[string]int{"Go": 1, "go": 1}},
		{"extra whitespace", "  a  b \t a\n", map[string]int{"a": 2, "b": 1}},
		{"empty", "", map[string]int{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WordCount(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("WordCount(%q) = %v, want %v", c.in, got, c.want)
			}
			for word, n := range c.want {
				if got[word] != n {
					t.Errorf("WordCount(%q)[%q] = %d, want %d", c.in, word, got[word], n)
				}
			}
		})
	}
}
```

# Lesson: Pointers {#pointers}

Go passes everything **by value**: when you call a function, it gets a copy
of each argument. Assign to the copy and the caller sees nothing:

```go
func bump(n int) {
	n++ // modifies the copy; the caller's variable is untouched
}
```

A **pointer** holds the address of a value, letting a function reach back
and modify the original. `&x` takes the address of `x`; `*p` follows the
pointer to the value it points at (both reading and writing):

```go
func bump(n *int) {
	*n++ // follow the pointer, increment the value it points at
}

x := 5
bump(&x) // pass x's address
// x is now 6
```

That's the entire mechanism — Go has no pointer arithmetic, and the garbage
collector means you never free anything. A pointer's zero value is `nil`,
and following a `nil` pointer panics, so functions that accept pointers
either document that callers pass real addresses or check first.

You've already been using reference-like behavior without pointers: slices
and maps internally hold pointers to their data, which is why a function can
append-to-a-map or modify slice elements it was passed. Explicit pointers
matter for plain values — ints, strings, and (next lesson) structs.

## Challenge: Pointer Basics {#pointer-basics points=10}

Implement two functions: `Double(n *int)` doubles the value `n` points at,
in place. `Swap(a, b *int)` exchanges the values `a` and `b` point at. You
may assume the pointers are never nil.

### Starter

```go
package challenge

func Double(n *int) {
	// TODO: double the value n points at
}

func Swap(a, b *int) {
	// TODO: exchange the values a and b point at
}
```

### Tests

```go
package challenge

import "testing"

func TestDouble(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"positive", 5, 10},
		{"zero", 0, 0},
		{"negative", -3, -6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := c.in
			Double(&n)
			if n != c.want {
				t.Errorf("Double(&%d) left %d, want %d", c.in, n, c.want)
			}
		})
	}
}

func TestSwap(t *testing.T) {
	x, y := 1, 2
	Swap(&x, &y)
	if x != 2 || y != 1 {
		t.Errorf("after Swap, x = %d, y = %d; want x = 2, y = 1", x, y)
	}
}
```

# Lesson: Structs and Methods {#structs-methods}

A **struct** groups named fields into one type — Go's building block where
other languages reach for classes:

```go
type Rectangle struct {
	Width  float64
	Height float64
}

r := Rectangle{Width: 3, Height: 4}
fmt.Println(r.Width) // 3
```

A **method** is a function with a *receiver* — the value it's called on —
declared between `func` and the method name:

```go
func (r Rectangle) Area() float64 {
	return r.Width * r.Height
}

r.Area() // 12
```

The receiver above is a **value receiver**: the method gets a copy of the
struct, so it can read fields but its writes are invisible to the caller —
exactly the pass-by-value rule from the pointers lesson. A method that needs
to *modify* the struct takes a **pointer receiver**:

```go
func (r *Rectangle) Scale(factor float64) {
	r.Width *= factor // through the pointer: the caller's Rectangle changes
	r.Height *= factor
}
```

Go smooths over the syntax: `r.Scale(2)` works on a plain `r Rectangle`
variable — the compiler takes the address for you. The rule of thumb:
methods that only read can use value receivers; methods that write must use
pointer receivers; and a type should be consistent about which it uses once
it has any pointer-receiver methods.

## Challenge: Rectangle {#rectangle points=10}

Define methods on the provided `Rectangle` struct: `Area() float64` and
`Perimeter() float64` return the rectangle's area and perimeter, and
`Scale(factor float64)` multiplies **both** dimensions by `factor`,
modifying the rectangle in place — choose your receivers accordingly.

### Starter

```go
package challenge

type Rectangle struct {
	Width  float64
	Height float64
}

func (r Rectangle) Area() float64 {
	// TODO
	return 0
}

func (r Rectangle) Perimeter() float64 {
	// TODO
	return 0
}

// Scale must modify the rectangle in place — is this the right receiver?
func (r Rectangle) Scale(factor float64) {
	// TODO
}
```

### Tests

```go
package challenge

import "testing"

func TestArea(t *testing.T) {
	r := Rectangle{Width: 3, Height: 4}
	if got := r.Area(); got != 12 {
		t.Errorf("Area() = %v, want 12", got)
	}
}

func TestPerimeter(t *testing.T) {
	r := Rectangle{Width: 3, Height: 4}
	if got := r.Perimeter(); got != 14 {
		t.Errorf("Perimeter() = %v, want 14", got)
	}
}

func TestScaleModifiesInPlace(t *testing.T) {
	r := Rectangle{Width: 3, Height: 4}
	r.Scale(2)
	if r.Width != 6 || r.Height != 8 {
		t.Errorf("after Scale(2), rectangle is %+v; want Width 6, Height 8", r)
	}
	if got := r.Area(); got != 48 {
		t.Errorf("Area() after Scale(2) = %v, want 48", got)
	}
}
```

# Lesson: Errors {#errors}

Go has no exceptions. A function that can fail returns an `error` as its
last result, and the caller checks it immediately — the multiple-return
shape from earlier, doing its real job:

```go
f, err := os.Open("config.json")
if err != nil {
	return err // couldn't open: stop here and report why
}
```

`error` is just a value (an interface — next lesson makes that click), and
`nil` means "no error". Create errors with `errors.New` for fixed messages
or `fmt.Errorf` when the message needs details:

```go
errors.New("inventory empty")
fmt.Errorf("no such item %q", name)
```

A package can export a **sentinel error** — a named error value callers can
test for with `errors.Is`:

```go
var ErrNotFound = errors.New("not found")

if errors.Is(err, ErrNotFound) {
	// handle the specific "not found" case
}
```

To add context to a sentinel without breaking that check, **wrap** it with
`fmt.Errorf`'s `%w` verb — `errors.Is` sees through wrapping:

```go
fmt.Errorf("removing %q: %w", name, ErrNotFound)
```

Returning the sentinel bare works too; wrap when the extra context would
help whoever reads the error.

Two conventions to adopt from day one. First, when returning a non-nil
error, return the **zero value** for the result — callers must not use the
result when `err != nil`, and a zero value keeps accidents boring. Second,
handle the error and move on — the `if err != nil` blocks that pepper Go
code are the language *making failure paths visible*, and you write them
without thinking within a week.

## Challenge: Safe Divide {#safe-divide points=15}

Implement `SafeDivide(a, b float64) (float64, error)`: return `a / b`, or a
non-nil error (and zero result) when `b` is zero.

### Starter

```go
package challenge

func SafeDivide(a, b float64) (float64, error) {
	// TODO: divide, or return an error when b is zero
	return 0, nil
}
```

### Tests

```go
package challenge

import "testing"

func TestSafeDivide(t *testing.T) {
	cases := []struct {
		name string
		a, b float64
		want float64
	}{
		{"basic", 10, 2, 5},
		{"fractional", 1, 4, 0.25},
		{"zero numerator", 0, 5, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SafeDivide(c.a, c.b)
			if err != nil {
				t.Fatalf("SafeDivide(%v, %v) returned unexpected error: %v", c.a, c.b, err)
			}
			if got != c.want {
				t.Errorf("SafeDivide(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestSafeDivideByZero(t *testing.T) {
	got, err := SafeDivide(3, 0)
	if err == nil {
		t.Fatal("SafeDivide(3, 0) returned nil error, want non-nil")
	}
	if got != 0 {
		t.Errorf("SafeDivide(3, 0) = %v with error, want zero result", got)
	}
}
```

# Lesson: Interfaces {#interfaces}

An **interface** names a set of methods. Any type that has those methods
satisfies the interface — automatically, with no `implements` declaration:

```go
type Shape interface {
	Area() float64
}
```

Your `Rectangle` from two lessons ago already satisfies `Shape`: it has an
`Area() float64` method, and that's the whole test. This *implicit*
satisfaction is Go's quiet superpower — a package can define an interface
for what it needs, and types written years earlier, in packages that have
never heard of it, just fit.

Code that takes an interface works with any satisfying type:

```go
func Describe(s Shape) {
	fmt.Println("area:", s.Area())
}

Describe(Rectangle{Width: 3, Height: 4}) // area: 12
Describe(Circle{Radius: 1})              // area: 3.14…
```

You've been using interfaces all along: `error` is an interface with one
method (`Error() string`), and `fmt.Println` accepts anything. Go style
keeps interfaces **small** — one or two methods is the norm (`io.Reader`,
`fmt.Stringer`, `error`) — and defines them where they're *used*, not where
types are declared.

For circle math, `math.Pi` is the stdlib's π constant, and a circle's area
is `math.Pi * r * r`.

## Challenge: Shapes {#shapes points=15}

The starter defines the `Shape` interface. Implement `Area() float64` on
`Circle` and `Square` so both satisfy it, then implement
`TotalArea(shapes []Shape) float64` returning the sum of all areas.

### Starter

```go
package challenge

type Shape interface {
	Area() float64
}

type Circle struct {
	Radius float64
}

type Square struct {
	Side float64
}

func (c Circle) Area() float64 {
	// TODO: math.Pi * radius * radius (import "math")
	return 0
}

func (s Square) Area() float64 {
	// TODO
	return 0
}

func TotalArea(shapes []Shape) float64 {
	// TODO: sum every shape's Area
	return 0
}
```

### Tests

```go
package challenge

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestCircleArea(t *testing.T) {
	c := Circle{Radius: 2}
	if got := c.Area(); !almostEqual(got, 4*math.Pi) {
		t.Errorf("Circle{2}.Area() = %v, want %v", got, 4*math.Pi)
	}
}

func TestSquareArea(t *testing.T) {
	s := Square{Side: 3}
	if got := s.Area(); !almostEqual(got, 9) {
		t.Errorf("Square{3}.Area() = %v, want 9", got)
	}
}

func TestTotalArea(t *testing.T) {
	shapes := []Shape{
		Circle{Radius: 1},
		Square{Side: 2},
		Square{Side: 3},
	}
	want := math.Pi + 4 + 9
	if got := TotalArea(shapes); !almostEqual(got, want) {
		t.Errorf("TotalArea = %v, want %v", got, want)
	}
}

func TestTotalAreaEmpty(t *testing.T) {
	if got := TotalArea(nil); !almostEqual(got, 0) {
		t.Errorf("TotalArea(nil) = %v, want 0", got)
	}
}
```

# Final Challenge: Inventory Tracker {#final points=50}

Time to combine everything: structs, maps, pointer receivers, errors, and
zero values. Build an in-memory inventory that tracks item quantities.

Implement:

- `NewInventory() *Inventory` — a ready-to-use, empty inventory (remember:
  maps must be created before writing).
- `(*Inventory) Add(name string, qty int) error` — add `qty` of an item,
  creating it if new. Reject `qty <= 0` with a non-nil error.
- `(*Inventory) Remove(name string, qty int) error` — remove `qty` of an
  item. Reject `qty <= 0` with a non-nil error. If the item doesn't exist
  (or its count is zero), return `ErrNotFound` (wrapping it is fine —
  the tests use `errors.Is`). If there aren't enough on hand, return some
  other non-nil error and leave the count unchanged. Removing the last of
  an item may either delete the key or keep it at zero — a later `Remove`
  of that item must report `ErrNotFound` either way.
- `(*Inventory) Count(name string) int` — how many of an item are on hand
  (0 for unknown items — let the map's zero value work for you).
- `(*Inventory) Total() int` — total quantity across all items.

### Starter

```go
package challenge

import "errors"

// ErrNotFound is returned by Remove when the item isn't in the inventory.
var ErrNotFound = errors.New("item not found")

type Inventory struct {
	// TODO: choose your fields
}

func NewInventory() *Inventory {
	// TODO
	return nil
}

func (inv *Inventory) Add(name string, qty int) error {
	// TODO
	return nil
}

func (inv *Inventory) Remove(name string, qty int) error {
	// TODO
	return nil
}

func (inv *Inventory) Count(name string) int {
	// TODO
	return 0
}

func (inv *Inventory) Total() int {
	// TODO
	return 0
}
```

### Tests

```go
package challenge

import (
	"errors"
	"testing"
)

func TestAddAndCount(t *testing.T) {
	inv := NewInventory()
	if err := inv.Add("apple", 3); err != nil {
		t.Fatalf("Add(apple, 3) returned error: %v", err)
	}
	if err := inv.Add("apple", 2); err != nil {
		t.Fatalf("second Add(apple, 2) returned error: %v", err)
	}
	if got := inv.Count("apple"); got != 5 {
		t.Errorf("Count(apple) = %d, want 5", got)
	}
	if got := inv.Count("banana"); got != 0 {
		t.Errorf("Count(banana) = %d for unknown item, want 0", got)
	}
}

func TestAddRejectsNonPositive(t *testing.T) {
	inv := NewInventory()
	if err := inv.Add("apple", 0); err == nil {
		t.Error("Add(apple, 0) returned nil error, want non-nil")
	}
	if err := inv.Add("apple", -2); err == nil {
		t.Error("Add(apple, -2) returned nil error, want non-nil")
	}
	if got := inv.Count("apple"); got != 0 {
		t.Errorf("Count(apple) = %d after rejected adds, want 0", got)
	}
}

func TestRemove(t *testing.T) {
	inv := NewInventory()
	if err := inv.Add("apple", 5); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := inv.Remove("apple", 2); err != nil {
		t.Fatalf("Remove(apple, 2) returned error: %v", err)
	}
	if got := inv.Count("apple"); got != 3 {
		t.Errorf("Count(apple) = %d after removing 2 of 5, want 3", got)
	}
}

func TestRemoveUnknownIsErrNotFound(t *testing.T) {
	inv := NewInventory()
	err := inv.Remove("ghost", 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove(ghost, 1) = %v, want ErrNotFound", err)
	}
}

func TestRemoveTooManyLeavesCountUnchanged(t *testing.T) {
	inv := NewInventory()
	if err := inv.Add("apple", 2); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := inv.Remove("apple", 5); err == nil {
		t.Error("Remove(apple, 5) with 2 on hand returned nil error, want non-nil")
	}
	if got := inv.Count("apple"); got != 2 {
		t.Errorf("Count(apple) = %d after failed remove, want 2", got)
	}
}

func TestRemoveRejectsNonPositive(t *testing.T) {
	inv := NewInventory()
	if err := inv.Add("apple", 2); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := inv.Remove("apple", 0); err == nil {
		t.Error("Remove(apple, 0) returned nil error, want non-nil")
	}
	if err := inv.Remove("apple", -1); err == nil {
		t.Error("Remove(apple, -1) returned nil error, want non-nil")
	}
}

func TestRemoveAllThenNotFound(t *testing.T) {
	inv := NewInventory()
	if err := inv.Add("apple", 2); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := inv.Remove("apple", 2); err != nil {
		t.Fatalf("Remove(apple, 2) returned error: %v", err)
	}
	if got := inv.Count("apple"); got != 0 {
		t.Errorf("Count(apple) = %d after removing all, want 0", got)
	}
	if err := inv.Remove("apple", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove after emptying = %v, want ErrNotFound", err)
	}
}

func TestTotal(t *testing.T) {
	inv := NewInventory()
	if got := inv.Total(); got != 0 {
		t.Errorf("Total() on empty inventory = %d, want 0", got)
	}
	if err := inv.Add("apple", 3); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := inv.Add("banana", 4); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if got := inv.Total(); got != 7 {
		t.Errorf("Total() = %d, want 7", got)
	}
}
```
