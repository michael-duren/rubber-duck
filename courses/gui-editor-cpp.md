---
course: gui-editor
title: Build a Cross-Platform GUI Text Editor
language: cpp
description: >
  There is no "text widget" — just a rectangle of pixels the OS lets you
  draw into. Build a native GUI text editor from first principles in modern
  C++: a real X11 event loop (with Win32 and Cocoa treated side by side), a
  software framebuffer, bitmap font rendering, UTF-8 decoding from scratch,
  a piece-table document model, word-wrap layout, hit testing, selections,
  damage-driven redraws, the famously weird X11 clipboard protocol, and
  grouped undo — culminating in a headless EditorCore driven entirely by
  synthetic events.
duration_hours: 25
tags: [cpp, systems, gui, editors]
extended_reading:
  - title: "X Window System protocol specification"
    url: https://www.x.org/releases/current/doc/xproto/x11protocol.html
  - title: "The XCB tutorial"
    url: https://xcb.freedesktop.org/tutorial/
  - title: "About Messages and Message Queues (Win32)"
    url: https://learn.microsoft.com/en-us/windows/win32/winmsg/about-messages-and-message-queues
  - title: "ICCCM — Inter-Client Communication Conventions (the selections chapter)"
    url: https://www.x.org/releases/X11R7.7/doc/xorg-docs/icccm/icccm.html
  - title: "VS Code's text buffer reimplementation (piece tree)"
    url: https://code.visualstudio.com/blogs/2018/03/23/text-buffer-reimplementation
  - title: "Data Structures for Text Sequences (Charles Crowley)"
    url: https://www.cs.unm.edu/~crowley/papers/sds.pdf
  - title: "Text Rendering Hates You"
    url: https://faultlore.com/blah/text-hates-you/
  - title: "Rope science (xi-editor docs)"
    url: https://xi-editor.io/docs/rope_science_00.html
  - title: "UTF-8 Everywhere"
    url: https://utf8everywhere.org/
---

# Lesson: You Own Every Pixel {#you-own-every-pixel}

Open TextEdit or Notepad and it looks like the operating system is doing the
work: there's a window, there's text, the caret blinks. It is tempting to
imagine an OS call like `os_create_text_editor()`. There isn't one. What the
OS actually gives you is astonishingly little:

- a **window** — a rectangle of pixels it agrees to composite onto the
  screen for you, and
- an **event stream** — a queue of "the user pressed J", "the mouse moved
  to (412, 88)", "your window is now 800×600", "please redraw yourself".

Everything else — every glyph, the caret, the selection highlight, the
scrollbar — is pixels *you* computed and handed over. (Toolkit widgets like
GTK's `TextView` or Win32's `EDIT` control are libraries layered on exactly
this; in this course we live at the layer they're built on. If you've
wondered how those widgets could ever be written, this is how.)

That inversion is the first thing to internalize. A command-line program
*calls* the OS when it wants something. A GUI program is called *by* the OS
— or more precisely, it spends its life in a loop asking "what happened?"
and reacting:

```cpp
// The heart of every GUI program ever written, on every platform:
while (running) {
    Event ev = wait_for_next_event();   // blocks until something happens
    handle(ev);                          // mutate state, maybe draw
}
```

This is the **event loop**. X11 spells it `xcb_wait_for_event`; Win32
spells it `GetMessage`/`DispatchMessage`; Cocoa hides it inside
`[NSApplication run]`. The names differ, the shape never does. Your editor
will be a pure function of the events it has received — which is also what
will make it *testable*: feed it a scripted list of events and assert on
the state, no screen required. Every graded challenge in this course works
that way, and the final challenge drives a whole editor core with synthetic
keystrokes and mouse clicks.

The solid arrows run once per event — the loop blocks in the violet oval
until something happens; the dashed arrow to the red oval is the one event
that breaks out of it (`running = false`).

```d2
direction: right

wait: "wait_for_next_event()" {
  shape: oval
  style.stroke: "#a78bfa"
  style.stroke-width: 2
}
dispatch: "handle(ev)\ndispatch by type"
update: "update state,\nrepaint if needed"
quit: "running = false" {
  shape: oval
  style.stroke: "#dc2626"
  style.stroke-width: 2
}

wait -> dispatch: "event"
dispatch -> update
update -> wait: "loop"
dispatch -> quit: "Quit" {style.stroke-dash: 4}
```

### The event queue is a real queue

Events arrive faster than you handle them — the X server doesn't wait for
you. They pile up in a queue, and that queue has two properties worth
respecting from day one:

- **Order matters.** A `MouseDown` at (10,10) followed by `MouseMove` to
  (50,10) is a drag. Reordered, it's nonsense. Events are handled strictly
  first-in, first-out.
- **Some events are collapsible.** If the user sweeps the mouse across the
  window, you might receive 300 `MouseMove` events in one frame. Nothing
  observable depends on the intermediate positions — only the latest one.
  Handling all 300 (each triggering hit-testing and maybe a repaint) is how
  editors get laggy under fast mouse movement. The same goes for paint
  requests: if the OS asked you to redraw three times before you got around
  to it, you redraw *once*. Real platforms bake this in — Win32 coalesces
  `WM_PAINT` and `WM_MOUSEMOVE` in the queue itself; X11 clients compress
  runs of `MotionNotify` by hand, exactly like you're about to.

One rule keeps coalescing honest: **only adjacent events of the same kind
may collapse**. A `MouseMove, MouseDown, MouseMove` sequence must survive
intact — collapsing across the click would eat a drag gesture.

### Quit is an event too

There is no OS callback that kills your process when the user clicks the
close button (on X11 and Cocoa at least — we'll meet the details in lesson
3). You *receive an event* asking you to close, and you decide what to do:
save prompts, veto, or exit the loop. That's why the loop condition is
`running` and not `true` — quitting is just the one event that makes you
stop pumping.

## Challenge: Pump the Queue {#pump-the-queue points=12}

Model the innermost piece of the editor: draining one batch of queued
events. Implement `pump`, which takes the queued events in arrival order
and decides which ones actually get delivered to the application:

- **FIFO**: delivered events keep their relative order.
- **Coalescing**: in any run of *consecutive* `MouseMove` events, deliver
  only the last one. In any run of consecutive `Paint` events, deliver only
  the last one. Runs are broken by any event of a different type — never
  coalesce across an intervening event.
- **Quit**: when a `Quit` event is reached, set `quit = true` and stop.
  `Quit` itself is not delivered, and events after it are never examined —
  not even for coalescing.

This is exactly the shape of the drain loop you'll later wrap around
`xcb_poll_for_event`: pull everything that's pending, compress it, then act.

### Starter

```cpp
#include <vector>

// The portable event type. Every platform backend translates its native
// events into this before the editor core ever sees them.
enum class EventType {
    KeyDown,    // key holds the key code
    MouseMove,  // x, y: pointer position in window coordinates
    MouseDown,  // x, y: click position
    Paint,      // the window needs redrawing
    Quit,       // the user asked to close the window
};

struct Event {
    EventType type;
    int x = 0;
    int y = 0;
    int key = 0;
};

struct PumpResult {
    std::vector<Event> handled;  // events delivered, in order
    bool quit = false;           // true if a Quit event was reached
};

// Deliver queued events in order, coalescing runs of consecutive
// MouseMove (keep the last) and consecutive Paint (keep the last),
// stopping at Quit (which is not delivered).
PumpResult pump(const std::vector<Event>& queue) {
    // TODO
    (void)queue;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static bool same(const Event& a, const Event& b) {
    return a.type == b.type && a.x == b.x && a.y == b.y && a.key == b.key;
}

int main() {
    using ET = EventType;

    {   // Plain FIFO delivery, no coalescible runs.
        std::vector<Event> q = {
            {ET::KeyDown, 0, 0, 'a'},
            {ET::MouseDown, 5, 6, 0},
            {ET::KeyDown, 0, 0, 'b'},
        };
        PumpResult r = pump(q);
        check(r.handled.size() == 3 && !r.quit &&
              same(r.handled[0], q[0]) && same(r.handled[1], q[1]) &&
              same(r.handled[2], q[2]),
              "test_fifo_order_preserved");
    }

    {   // A run of MouseMove collapses to the last one.
        std::vector<Event> q = {
            {ET::MouseMove, 1, 1, 0},
            {ET::MouseMove, 2, 2, 0},
            {ET::MouseMove, 3, 3, 0},
            {ET::KeyDown, 0, 0, 'x'},
        };
        PumpResult r = pump(q);
        check(r.handled.size() == 2 &&
              same(r.handled[0], {ET::MouseMove, 3, 3, 0}) &&
              same(r.handled[1], {ET::KeyDown, 0, 0, 'x'}),
              "test_mousemove_run_keeps_last");
    }

    {   // Paint runs collapse too.
        std::vector<Event> q = {
            {ET::Paint, 0, 0, 0},
            {ET::Paint, 0, 0, 0},
            {ET::Paint, 0, 0, 0},
        };
        PumpResult r = pump(q);
        check(r.handled.size() == 1 && r.handled[0].type == ET::Paint,
              "test_paint_run_keeps_one");
    }

    {   // A different event breaks the run: no coalescing across it.
        std::vector<Event> q = {
            {ET::MouseMove, 1, 1, 0},
            {ET::MouseDown, 1, 1, 0},
            {ET::MouseMove, 9, 9, 0},
        };
        PumpResult r = pump(q);
        check(r.handled.size() == 3 &&
              same(r.handled[0], {ET::MouseMove, 1, 1, 0}) &&
              same(r.handled[1], {ET::MouseDown, 1, 1, 0}) &&
              same(r.handled[2], {ET::MouseMove, 9, 9, 0}),
              "test_no_coalescing_across_click");
    }

    {   // Quit stops the pump; later events are never delivered.
        std::vector<Event> q = {
            {ET::KeyDown, 0, 0, 'q'},
            {ET::Quit, 0, 0, 0},
            {ET::KeyDown, 0, 0, 'z'},
            {ET::MouseMove, 7, 7, 0},
        };
        PumpResult r = pump(q);
        check(r.quit && r.handled.size() == 1 &&
              same(r.handled[0], {ET::KeyDown, 0, 0, 'q'}),
              "test_quit_stops_delivery");
    }

    {   // Quit inside what would otherwise be a MouseMove run.
        std::vector<Event> q = {
            {ET::MouseMove, 1, 1, 0},
            {ET::MouseMove, 2, 2, 0},
            {ET::Quit, 0, 0, 0},
            {ET::MouseMove, 3, 3, 0},
        };
        PumpResult r = pump(q);
        check(r.quit && r.handled.size() == 1 &&
              same(r.handled[0], {ET::MouseMove, 2, 2, 0}),
              "test_quit_ends_mousemove_run");
    }

    {   // Empty queue: nothing handled, no quit.
        PumpResult r = pump({});
        check(r.handled.empty() && !r.quit, "test_empty_queue");
    }

    return failed;
}
```
# Lesson: The Platform Seam {#the-platform-seam}

Before touching an OS API, decide where portability lives. The classic
mistake is to sprinkle `#ifdef _WIN32` through the whole program until every
file knows about three operating systems. The fix is one deliberate seam: a
small set of types the *editor* is written against, with one implementation
of that seam per platform. Get the seam right and the editor core — the
document, layout, selection, undo, everything you'll build from lesson 6
onward — compiles and runs on a machine with no display at all. (That's not
hypothetical: it's exactly how this course's grader runs your code.)

A workable seam for an editor is tiny:

```cpp
// platform.h — the only header the editor core sees.
struct PlatformWindow {
    virtual ~PlatformWindow() = default;
    virtual void set_title(const std::string& title) = 0;
    virtual void invalidate(Rect dirty) = 0;   // "please send me a Paint for this"
    virtual Size size() const = 0;
    virtual void blit(const Framebuffer& fb) = 0;  // pixels -> screen
};

struct Platform {
    virtual ~Platform() = default;
    virtual std::unique_ptr<PlatformWindow> create_window(int w, int h) = 0;
    virtual std::optional<Event> wait_event() = 0;  // nullopt = shutting down
    virtual std::string clipboard_read() = 0;
    virtual void clipboard_write(const std::string& text) = 0;
};
```

Two C++ decisions are hiding in those dozen lines, and they're worth making
consciously:

- **Abstract base vs `std::variant`.** You could model the seam as
  `std::variant<X11Platform, Win32Platform, CocoaPlatform>` and `std::visit`
  your way through. But only one alternative can ever exist per build —
  you'll never hold "an X11 *or* Win32 platform" at runtime on one OS — so
  the variant buys closed-set exhaustiveness you don't need and costs you a
  compile-time dependency on every platform's headers everywhere. An
  abstract base with exactly one derived class linked per platform keeps
  each backend's headers quarantined in its own `.cpp` (or `.mm`) file.
  Variants shine for *events* (a closed set of small value types); virtual
  dispatch shines for *backends* (open set, one alive at a time, holding OS
  resources).
- **Pimpl by construction.** Because the editor only ever sees
  `PlatformWindow*`, the `xcb_connection_t*` and `xcb_window_t` members live
  in `X11Window : PlatformWindow` inside `platform_x11.cpp`. The interface
  *is* the pimpl — no separate idiom needed, no OS header ever leaks into
  the core's translation units.

### Value types for geometry

The seam traffics in points, sizes, and rectangles constantly: mouse
positions, dirty regions, window sizes. These should be **value types** —
small structs passed by value, compared field by field, copied freely — not
objects with identity. There is nothing to encapsulate in an `x` and a `y`;
wrapping them in getters adds friction and nothing else. This is the "rule
of zero" end of the design spectrum: no destructor, no copy control,
aggregate initialization (`Rect{0, 0, 800, 600}`) just works.

The one place geometry earns real code is **rectangle algebra**. Editors
compute with rects all day: "clip this fill to the window", "what part of
the damage is visible", "merge these two dirty regions". Two operations
carry almost all of it:

- `intersect(a, b)` — the overlapping region (used for clipping). If the
  rects don't overlap, the result is *empty*.
- `union_of(a, b)` — the smallest rect containing both (used for merging
  damage). An empty rect is the identity: union with it returns the other
  rect unchanged, because "no damage" plus "this damage" is just "this
  damage".

A rect with `w <= 0` or `h <= 0` is empty. Treat *all* empty rects as
interchangeable — code that checks `r.empty()` before using `r` never cares
which degenerate coordinates it holds.

One convention to fix now and never revisit: rects are **half-open**. A
rect at x=0 with w=10 covers pixel columns 0 through 9 — `x + w` is
one-past-the-end, exactly like STL iterators. Half-open ranges make
adjacency tests (`a.x + a.w == b.x`) and width math (`right - left ==
width`) come out without the `±1` fudging that plagues inclusive ranges.

### Handles: what the OS actually hands you

Everything a windowing system gives you is a **handle**: an opaque token
standing for a resource the OS owns on your behalf. On X11 a window is a
`uint32_t` ID valid only within one connection; on Win32 it's an `HWND`; on
macOS an `NSWindow*`. The same goes for graphics contexts, shared-memory
segments, and the file-watch descriptors we'll meet in the final lesson.
Each has a matching destroy call (`xcb_destroy_window`, `DestroyWindow`,
`close`), and each one leaks — or worse, dangles — if some error path
misses the cleanup.

C programs handle this with discipline and `goto cleanup`. C++ has a better
answer, and it's the most important idiom in this course's platform layer:
**RAII** — acquire the resource in a constructor, release it in the
destructor, let scope do the bookkeeping. Once a handle lives inside an
RAII wrapper there is *no code path that leaks it*: early returns,
exceptions, and error branches all run the destructor.

The subtlety is copying. If a wrapper owning window #42 gets copied, both
copies' destructors destroy window #42 — a double-free. So an owning handle
wrapper must be **move-only**: copying is deleted (there can't be two
owners), and moving *transfers* ownership, leaving the source holding
nothing so its destructor does nothing. This is the "rule of five" in its
most common practical form: you wrote a destructor, so you must decide all
five special members. (The geometry structs above follow the rule of
*zero*: no resources, so no special members at all. Most types should be
one or the other — a type with a destructor but default-generated copies is
the red flag.)

You already know this shape: it's `std::unique_ptr`, generalized. A
`unique_ptr` with a custom deleter *can* wrap handles, but an `int`-valued
handle where `-1` means "nothing" fits awkwardly into a pointer-shaped API,
so platform layers routinely write a small dedicated wrapper. Three fine
points the second challenge will hold you to:

- **Moved-from means empty.** After `b = std::move(a)`, `a` must hold the
  invalid handle so its destructor is a no-op. Forgetting this is the #1
  RAII bug: the resource dies twice.
- **Move-assignment releases the old resource first.** If `b` already owned
  something, that something must be closed before `b` takes over `a`'s
  handle — otherwise it leaks silently.
- **Self-move must not destroy the resource.** `x = std::move(x)` is legal
  C++ (it arises in generic code) and must leave `x` usable.

`release()` (give the handle up without closing — for handing ownership to
an API that takes it) and `reset()` (close now, optionally adopt a new
handle) round out the interface, mirroring `unique_ptr` deliberately:
familiar names make your code cheaper to read.

## Challenge: Points and Rects {#point-and-rect points=10}

Implement the geometry kit the rest of the course leans on. Every later
rendering challenge hands you this code back pre-written, so make it right
once, here.

Semantics:

- `Rect::empty()` — true iff `w <= 0` or `h <= 0`.
- `Rect::contains(Point)` — half-open on both axes: `{0,0,10,10}` contains
  (0,0) and (9,9) but not (10,10). Empty rects contain nothing.
- `intersect(a, b)` — the overlap; when there is none, return something
  `empty()` (returning `{0,0,0,0}` is the tidy choice). If either input is
  empty, the result is empty. The tests only inspect exact fields of
  *non-empty* results.
- `union_of(a, b)` — bounding box of both; if one input is empty, return
  the other unchanged; if both are empty, return an empty rect.

### Starter

```cpp
#include <algorithm>

struct Point {
    int x = 0;
    int y = 0;
};

struct Rect {
    int x = 0;
    int y = 0;
    int w = 0;
    int h = 0;

    bool empty() const {
        // TODO
        return true;
    }

    bool contains(Point p) const {
        // TODO: half-open — x <= p.x < x + w, same for y
        (void)p;
        return false;
    }
};

// Overlapping region of a and b; empty if they don't overlap.
Rect intersect(Rect a, Rect b) {
    // TODO
    (void)a;
    (void)b;
    return {};
}

// Smallest rect containing both. Empty rects are the identity element.
Rect union_of(Rect a, Rect b) {
    // TODO
    (void)a;
    (void)b;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static bool same(Rect a, Rect b) {
    return a.x == b.x && a.y == b.y && a.w == b.w && a.h == b.h;
}

int main() {
    check(!Rect{0, 0, 10, 10}.empty(), "test_nonempty");
    check(Rect{5, 5, 0, 10}.empty(), "test_zero_width_is_empty");
    check(Rect{5, 5, 10, -3}.empty(), "test_negative_height_is_empty");

    Rect r{0, 0, 10, 10};
    check(r.contains({0, 0}), "test_contains_top_left");
    check(r.contains({9, 9}), "test_contains_bottom_right_inside");
    check(!r.contains({10, 10}), "test_half_open_excludes_edge");
    check(!r.contains({-1, 5}), "test_excludes_left_of_rect");
    check(!Rect{0, 0, 0, 0}.contains({0, 0}), "test_empty_contains_nothing");

    check(same(intersect({0, 0, 10, 10}, {5, 5, 10, 10}), {5, 5, 5, 5}),
          "test_intersect_overlap");
    check(same(intersect({2, 3, 4, 5}, {0, 0, 100, 100}), {2, 3, 4, 5}),
          "test_intersect_contained");
    check(intersect({0, 0, 10, 10}, {20, 20, 5, 5}).empty(),
          "test_intersect_disjoint_is_empty");
    check(intersect({0, 0, 10, 10}, {10, 0, 10, 10}).empty(),
          "test_touching_edges_do_not_overlap");
    check(intersect({0, 0, 0, 0}, {0, 0, 10, 10}).empty(),
          "test_intersect_with_empty");

    check(same(union_of({0, 0, 10, 10}, {20, 20, 5, 5}), {0, 0, 25, 25}),
          "test_union_bounding_box");
    check(same(union_of({5, 5, 5, 5}, {0, 0, 20, 20}), {0, 0, 20, 20}),
          "test_union_contained");
    check(same(union_of({0, 0, 0, 0}, {3, 4, 5, 6}), {3, 4, 5, 6}),
          "test_union_empty_is_identity_left");
    check(same(union_of({3, 4, 5, 6}, {-1, 2, 0, 5}), {3, 4, 5, 6}),
          "test_union_empty_is_identity_right");
    check(union_of({0, 0, 0, 0}, {7, 7, -2, 3}).empty(),
          "test_union_of_two_empties_is_empty");

    return failed;
}
```

## Challenge: A Move-Only Handle {#raii-handle points=12}

Implement `UniqueHandle`, the RAII wrapper the platform backends use for
every OS resource. The handle is an `int` where `-1` means "none"; the
close function is supplied at construction (in the real backend it's a
function calling `xcb_destroy_window`, `close(2)`, and so on).

Required behavior:

- the destructor closes a valid handle exactly once; it never calls close
  for handle `-1` or when the close function is null;
- move construction and move assignment transfer ownership and leave the
  source empty;
- move assignment closes the destination's previous handle first;
- self-move-assignment leaves the object unchanged and closes nothing;
- `release()` returns the handle and empties the wrapper without closing;
- `reset(h, fn)` closes any current handle, then adopts `h` and `fn`.

### Starter

```cpp
#include <utility>

using CloseFn = void (*)(int handle);

// Move-only owner of a native handle. -1 means "no handle".
class UniqueHandle {
public:
    UniqueHandle() = default;
    UniqueHandle(int handle, CloseFn close) : handle_(handle), close_(close) {}

    ~UniqueHandle() {
        // TODO: close if we hold a valid handle and a close function
    }

    UniqueHandle(const UniqueHandle&) = delete;
    UniqueHandle& operator=(const UniqueHandle&) = delete;

    UniqueHandle(UniqueHandle&& other) noexcept {
        // TODO: steal other's handle; leave other empty
        (void)other;
    }

    UniqueHandle& operator=(UniqueHandle&& other) noexcept {
        // TODO: guard against self-move; close our handle; steal other's
        (void)other;
        return *this;
    }

    int get() const { return handle_; }
    explicit operator bool() const { return handle_ != -1; }

    // Give up ownership without closing; returns the handle.
    int release() {
        // TODO
        return -1;
    }

    // Close the current handle (if any), then adopt a new one.
    void reset(int handle = -1, CloseFn close = nullptr) {
        // TODO
        (void)handle;
        (void)close;
    }

private:
    int handle_ = -1;
    CloseFn close_ = nullptr;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <vector>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static std::vector<int> g_closed;
static void record_close(int h) { g_closed.push_back(h); }

int main() {
    {   // Destructor closes exactly once.
        g_closed.clear();
        {
            UniqueHandle h(42, record_close);
            check(h.get() == 42 && static_cast<bool>(h), "test_get_and_bool");
        }
        check(g_closed == std::vector<int>{42}, "test_dtor_closes_once");
    }

    {   // Default-constructed closes nothing.
        g_closed.clear();
        { UniqueHandle h; (void)h; }
        check(g_closed.empty(), "test_empty_dtor_closes_nothing");
    }

    {   // Move construction transfers ownership.
        g_closed.clear();
        {
            UniqueHandle a(7, record_close);
            UniqueHandle b(std::move(a));
            check(b.get() == 7 && a.get() == -1 && !a,
                  "test_move_ctor_transfers");
        }
        check(g_closed == std::vector<int>{7},
              "test_moved_from_not_double_closed");
    }

    {   // Move assignment closes the old handle first.
        g_closed.clear();
        {
            UniqueHandle a(1, record_close);
            UniqueHandle b(2, record_close);
            b = std::move(a);
            check(g_closed == std::vector<int>{2} && b.get() == 1 &&
                  a.get() == -1,
                  "test_move_assign_closes_old");
        }
        check(g_closed == (std::vector<int>{2, 1}),
              "test_move_assign_final_close");
    }

    {   // Self-move leaves the handle alive.
        g_closed.clear();
        {
            UniqueHandle a(9, record_close);
            UniqueHandle& alias = a;
            a = std::move(alias);
            check(g_closed.empty() && a.get() == 9, "test_self_move_safe");
        }
        check(g_closed == std::vector<int>{9},
              "test_self_move_still_closes_at_end");
    }

    {   // release() gives up ownership without closing.
        g_closed.clear();
        {
            UniqueHandle a(5, record_close);
            int raw = a.release();
            check(raw == 5 && a.get() == -1 && !a,
                  "test_release_returns_handle");
        }
        check(g_closed.empty(), "test_release_prevents_close");
    }

    {   // reset() closes the old handle and adopts the new.
        g_closed.clear();
        UniqueHandle a(3, record_close);
        a.reset(8, record_close);
        check(g_closed == std::vector<int>{3} && a.get() == 8,
              "test_reset_swaps_handles");
        a.reset();
        check(g_closed == (std::vector<int>{3, 8}) && a.get() == -1 && !a,
              "test_reset_to_empty");
    }

    return failed;
}
```
# Lesson: A Window on X11 {#a-window-on-x11}

Time to open a real window. We'll do Linux/X11 in full working code, then
walk the same ground on Windows and macOS so you can see that the three
APIs are one design wearing three costumes. (Wayland is Linux's successor
protocol; its concepts map closely enough to X11's that everything here
transfers, and X11 still runs everywhere via XWayland.)

### What X11 actually is

X is not a library — it is a **network protocol**, born in 1984 at MIT,
between your program (the *client*) and the *X server*, the process that
owns the screen, the mouse, and the keyboard. Everything you "do" to the
screen is a byte-serialized request written to a socket: `CreateWindow` is
request opcode 1, `MapWindow` opcode 8. Everything the user does arrives
back on the same socket as 32-byte event packets. That a window is "yours"
means only that you know its 32-bit ID and the server will route its
events to your socket. This design is why X could run applications on a
mainframe displaying on a terminal across the building — and why every X
call has latency in mind: requests are *buffered* and flushed in batches,
and most don't wait for a reply.

Two client libraries speak this protocol: **Xlib** (1985, hides the
asynchrony behind a synchronous-looking API) and **XCB** (2001, exposes the
protocol nearly 1:1 — you send a request, you *later* fetch the reply).
We'll use XCB: it's honest about what's on the wire, and that honesty is
the lesson.

### A complete window, annotated

This is a full program — about sixty lines — that opens a window, paints
it, and closes cleanly. Build it with `g++ main.cpp -lxcb` on any Linux
box with X headers (`apt install libxcb1-dev` / `pacman -S libxcb`). Read
every comment; each one is a concept the challenges will test.

```cpp
#include <xcb/xcb.h>
#include <cstdio>
#include <cstring>

int main() {
    // 1. Connect: open the socket to the X server ($DISPLAY tells us where).
    xcb_connection_t* conn = xcb_connect(nullptr, nullptr);
    if (xcb_connection_has_error(conn)) return 1;

    // The "screen" carries server facts: root window ID, depth, colors.
    const xcb_setup_t* setup = xcb_get_setup(conn);
    xcb_screen_t* screen = xcb_setup_roots_iterator(setup).data;

    // 2. Create: WE pick the window ID (from a client-side ID range the
    //    server granted at connect time — that's how creation needs no
    //    round trip). The window is a child of the root window.
    xcb_window_t win = xcb_generate_id(conn);
    uint32_t value_mask = XCB_CW_BACK_PIXEL | XCB_CW_EVENT_MASK;
    uint32_t values[2] = {
        screen->white_pixel,
        // The event mask is a SUBSCRIPTION: the server sends only what
        // you ask for. Forget EXPOSURE and you'll never be told to paint.
        XCB_EVENT_MASK_EXPOSURE | XCB_EVENT_MASK_STRUCTURE_NOTIFY |
        XCB_EVENT_MASK_KEY_PRESS | XCB_EVENT_MASK_BUTTON_PRESS |
        XCB_EVENT_MASK_BUTTON_RELEASE | XCB_EVENT_MASK_POINTER_MOTION,
    };
    xcb_create_window(conn, XCB_COPY_FROM_PARENT, win, screen->root,
                      0, 0, 800, 600, 0,            // x, y, w, h, border
                      XCB_WINDOW_CLASS_INPUT_OUTPUT,
                      screen->root_visual, value_mask, values);

    // 3. The close-button dance (explained below): tell the window manager
    //    we understand the WM_DELETE_WINDOW protocol.
    xcb_intern_atom_cookie_t c1 = xcb_intern_atom(conn, 0, 12, "WM_PROTOCOLS");
    xcb_intern_atom_cookie_t c2 = xcb_intern_atom(conn, 0, 16, "WM_DELETE_WINDOW");
    xcb_intern_atom_reply_t* r1 = xcb_intern_atom_reply(conn, c1, nullptr);
    xcb_intern_atom_reply_t* r2 = xcb_intern_atom_reply(conn, c2, nullptr);
    xcb_atom_t wm_protocols = r1->atom, wm_delete = r2->atom;
    free(r1); free(r2);
    xcb_change_property(conn, XCB_PROP_MODE_REPLACE, win, wm_protocols,
                        XCB_ATOM_ATOM, 32, 1, &wm_delete);

    // 4. Map: creating a window doesn't show it. Mapping asks for it to
    //    become viewable; the window manager may intervene first.
    xcb_map_window(conn, win);
    xcb_flush(conn);   // requests are buffered — actually send them!

    // 5. The event loop. xcb_wait_for_event blocks on the socket.
    bool running = true;
    while (running) {
        xcb_generic_event_t* ev = xcb_wait_for_event(conn);
        if (!ev) break;                       // connection died
        switch (ev->response_type & ~0x80) {  // high bit = "synthetic"
        case XCB_EXPOSE: {
            // A region became visible and its pixels are GONE. X does
            // not remember your window's contents; repaint or stay blank.
            auto* e = reinterpret_cast<xcb_expose_event_t*>(ev);
            std::printf("expose %dx%d at (%d,%d), %d more coming\n",
                        e->width, e->height, e->x, e->y, e->count);
            break;
        }
        case XCB_CONFIGURE_NOTIFY: {
            auto* e = reinterpret_cast<xcb_configure_notify_event_t*>(ev);
            std::printf("now %dx%d\n", e->width, e->height);
            break;
        }
        case XCB_KEY_PRESS: {
            auto* e = reinterpret_cast<xcb_key_press_event_t*>(ev);
            std::printf("keycode %d, modifier state %#x\n",
                        e->detail, e->state);
            break;
        }
        case XCB_CLIENT_MESSAGE: {
            auto* e = reinterpret_cast<xcb_client_message_event_t*>(ev);
            if (e->type == wm_protocols &&
                e->data.data32[0] == wm_delete)
                running = false;              // close button clicked
            break;
        }
        }
        free(ev);   // XCB events are malloc'd; in real code, wrap in RAII
    }

    xcb_disconnect(conn);
    return 0;
}
```

Five ideas in that listing deserve a closer look.

### Expose: the server remembers nothing

The deepest difference from "retained" UI frameworks: the X server does
not keep your window's pixels (historically, server memory was precious).
When your window is uncovered, resized, or first shown, you get an
`Expose` event naming the rectangle that needs painting, and if you don't
paint it, it stays garbage. Your editor is therefore, at bottom, a
function from document state to pixels, called every time the OS asks.
Win32 is identical in spirit: the `WM_PAINT` message with an "invalid
region". Cocoa too: `drawRect:` with a dirty rect. Every platform hands
you *damage* and expects you to repaint it — which is why lesson 12
builds a damage-tracking system instead of repainting the world per
keystroke.

Note `e->count`: expose events can arrive as a batch of rectangles, and
`count` tells you how many more follow. The classic optimization — union
the rects, repaint once when `count == 0` — is your `pump` coalescing
from lesson 1, in the wild.

### Atoms: interned strings

X needed extensible message types without central registration, so it
interns strings: `xcb_intern_atom("WM_DELETE_WINDOW")` returns a small
integer — the **atom** — that every client asking for the same string
gets back. Properties (arbitrary data attached to windows) are keyed by
atoms; client messages are typed by atoms; clipboard formats are atoms.
It's a string-to-int hashmap that lives in the server, and half of X's
"protocols" are just conventions about which atom-named properties to
set. You'll implement the interning contract in this lesson's second
challenge, and atoms return with a vengeance in the clipboard lesson.

### The window manager and WM_DELETE_WINDOW

Here's the part that surprises everyone: **the close button is not part
of X**. The title bar, the borders, the [x] — they're drawn by another
ordinary client, the *window manager*, which X grants the special right
to intercept map requests and re-parent your window inside a frame. So
how does clicking the [x] reach you? Convention, standardized in the
ICCCM: you set the `WM_PROTOCOLS` property on your window listing the
atom `WM_DELETE_WINDOW`, which means "don't just destroy me — send me a
message". The WM then delivers a `ClientMessage` of type `WM_PROTOCOLS`
whose first data word is `WM_DELETE_WINDOW`. If you *don't* opt in, the
WM calls `XKillClient` and your process dies mid-write. An editor with
unsaved changes cares deeply about the difference.

There's a second protocol worth knowing: `_NET_WM_PING`. Compositors use
it to detect hung apps — the WM sends a ping ClientMessage and you must
echo it back (re-addressed to the root window) promptly, or the desktop
grays out your window and offers "Force Quit". Both protocols are pure
message-routing logic, which makes them perfect headless challenge
material.

### Keycodes, keysyms, and state

`XCB_KEY_PRESS` gives you a **keycode** — a number for a physical key
position (row/column of the switch, roughly). Keycode 38 is the key
labeled A on a US board and Q on a French one. Turning keycodes into
meaning is a two-step lookup: the server's *keymap* maps (keycode,
modifier level) to a **keysym** — a stable symbolic constant like
`XK_a` (0x61), `XK_Return` (0xff0d), `XK_Left` (0xff51). Keysyms for
Latin-1 characters equal their character codes; function and navigation
keys live in a reserved 0xff00 page. The event's `state` field is a
bitmask of modifiers held *at the moment of the press*: `ShiftMask`
(bit 0), `LockMask` (bit 1, Caps), `ControlMask` (bit 2), `Mod1Mask`
(bit 3, almost always Alt).

Keep this distinction sharp — it becomes a whole design principle in the
final lesson: **key events are for shortcuts; committed text is for
typing**. Ctrl+S is a key event you match against keysym+state; the
letter "é" typed via a compose sequence or an IME is *text* that may
never correspond to any single key press.

### The same machine, twice more

**Win32.** You register a window *class* naming a callback, create a
window of that class, then pump messages:

```cpp
// Win32: the same event loop, spelled differently.
LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
    switch (msg) {
    case WM_PAINT:   /* BeginPaint gives the invalid rect; draw; EndPaint */
    case WM_KEYDOWN: /* virtual-key code in wp — the "keysym" step */
    case WM_CHAR:    /* translated TEXT arrives separately! */
    case WM_CLOSE:   /* the close button — you may veto */
    case WM_DESTROY: PostQuitMessage(0); return 0;
    }
    return DefWindowProc(hwnd, msg, wp, lp);   // defaults for the rest
}
// ... RegisterClassEx{ .lpfnWndProc = WndProc, ... }; CreateWindowEx(...);
while (GetMessage(&msg, nullptr, 0, 0) > 0) {
    TranslateMessage(&msg);   // keydown -> WM_CHAR text events
    DispatchMessage(&msg);    // calls WndProc
}
```

The differences are instructive. Where X pushes events through one socket
and you switch on a type field, Win32 *calls you back* per-window — but
notice `GetMessage`/`DispatchMessage`: the loop is still yours, the
callback is just where the switch lives. `DefWindowProc` is the genius
move: unhandled messages get OS-default behavior, which is why a minimal
Win32 window already moves, resizes, and closes. `TranslateMessage` is
the keysym step made explicit: raw `WM_KEYDOWN` goes in, cooked `WM_CHAR`
text comes out as a *separate message* — the same shortcut/text split as
X. And `WM_PAINT` is `Expose` with one refinement: it's not queued
per-occurrence but synthesized when the queue is empty and the invalid
region is non-empty — the OS coalesces paint events for you. For pixels,
the classic path is GDI (`StretchDIBits` to push a memory bitmap, as
we'll do in lesson 4); modern apps layer Direct2D/DirectWrite on top, but
the message machinery is unchanged since 1985.

**Cocoa.** macOS inverts control completely: `[NSApplication run]` *is*
the loop, and you never see it. You hand the framework objects and it
calls their methods:

```objc
// Cocoa: the loop belongs to AppKit; you supply delegates and views.
@interface EditorView : NSView @end
@implementation EditorView
- (void)drawRect:(NSRect)dirty { /* your Expose/WM_PAINT */ }
- (void)keyDown:(NSEvent*)ev {
    // Route to the text system: this is how keystrokes become TEXT,
    // IME included. insertText: arrives with the committed string.
    [self interpretKeyEvents:@[ ev ]];
}
- (void)mouseDown:(NSEvent*)ev { /* hit test, place caret */ }
@end
// AppDelegate's applicationShouldTerminate: is WM_DELETE_WINDOW's cousin:
// a chance to say "wait, unsaved changes" before quitting.
```

Why the `.mm` files? Cocoa's API *is* Objective-C: message sends,
`@interface`, blocks. **Objective-C++** lets one translation unit contain
both languages, so the convention is: your portable C++ core, plus a thin
`platform_cocoa.mm` whose Objective-C classes call into it. The seam from
lesson 2 is what makes this clean — `CocoaWindow : PlatformWindow` wraps
an `NSWindow*`, and nothing else in the program knows Objective-C exists.
One more Cocoa distinction: the window *does* keep its contents (layers
are retained and composited), so `drawRect:` fires far less often — but
the contract is the same: here's a dirty rect, fill it.

Same skeleton, three times: **subscribe → loop → translate native events
into your portable Event type → repaint damage**. The two challenges
below implement the "translate" step for X11's two trickiest cases; your
real backend calls exactly these functions from its event switch.

## Challenge: Translate Keys {#translate-keys points=12}

Implement `translate_key`, the function your `XCB_KEY_PRESS` handler calls
after looking up the keysym: it turns an X11 keysym plus modifier state
into the portable `KeyEvent` the editor core understands. (The Win32
backend feeds virtual-key codes through a twin of this function — the
portable side never knows.)

Rules:

- Keysyms in the printable Latin-1 ranges — `0x20..0x7e` and `0xa0..0xff`
  — become `Key::Text` with `ch` equal to the keysym value (X11
  deliberately made these keysyms equal their character codes; the server
  has already applied Shift via the keymap, so `A` arrives as keysym
  0x41).
- The navigation/editing keysyms in the table in the starter map to their
  named `Key` values, with `ch = 0`.
- Anything else becomes `Key::Unknown` with `ch = 0` (real editors ignore
  these).
- `shift`, `ctrl`, `alt` come from the state bitmask (`ShiftMask`,
  `ControlMask`, `Mod1Mask`). Other bits — Caps Lock, NumLock — must be
  ignored, not rejected: state `0x12` (Lock plus a NumLock-like bit, with
  none of the three masks above set) still decodes as an *unmodified* key
  — those extra bits are noise, not grounds for rejecting the event or
  spuriously setting `shift`/`ctrl`/`alt`.

### Starter

```cpp
// X11 modifier bits, as found in xcb_key_press_event_t::state.
constexpr unsigned kShiftMask   = 1u << 0;
constexpr unsigned kLockMask    = 1u << 1;  // Caps Lock — ignore
constexpr unsigned kControlMask = 1u << 2;
constexpr unsigned kMod1Mask    = 1u << 3;  // Alt on stock configs

// Keysyms, from X11/keysymdef.h.
constexpr unsigned kXK_BackSpace = 0xff08;
constexpr unsigned kXK_Tab       = 0xff09;
constexpr unsigned kXK_Return    = 0xff0d;
constexpr unsigned kXK_Escape    = 0xff1b;
constexpr unsigned kXK_Home      = 0xff50;
constexpr unsigned kXK_Left      = 0xff51;
constexpr unsigned kXK_Up        = 0xff52;
constexpr unsigned kXK_Right     = 0xff53;
constexpr unsigned kXK_Down      = 0xff54;
constexpr unsigned kXK_End       = 0xff57;
constexpr unsigned kXK_Delete    = 0xffff;

enum class Key {
    Text,       // a printable character; see KeyEvent::ch
    Backspace, Tab, Enter, Escape,
    Home, End, Left, Right, Up, Down, Delete,
    Unknown,
};

struct KeyEvent {
    Key key = Key::Unknown;
    unsigned ch = 0;          // character code when key == Key::Text
    bool shift = false;
    bool ctrl = false;
    bool alt = false;
};

KeyEvent translate_key(unsigned keysym, unsigned state) {
    // TODO
    (void)keysym;
    (void)state;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {
        KeyEvent e = translate_key(0x61, 0);  // 'a'
        check(e.key == Key::Text && e.ch == 0x61 && !e.shift && !e.ctrl &&
              !e.alt, "test_plain_letter");
    }
    {
        KeyEvent e = translate_key(0x41, kShiftMask);  // Shift+'A'
        check(e.key == Key::Text && e.ch == 0x41 && e.shift,
              "test_shifted_letter_keeps_keysym");
    }
    {
        KeyEvent e = translate_key(0x20, 0);  // space: lowest printable
        check(e.key == Key::Text && e.ch == 0x20, "test_space_is_text");
    }
    {
        KeyEvent e = translate_key(0x7e, 0);  // '~': highest ASCII printable
        check(e.key == Key::Text && e.ch == 0x7e, "test_tilde_is_text");
    }
    {
        KeyEvent e = translate_key(0x7f, 0);  // DEL slot: not printable
        check(e.key == Key::Unknown && e.ch == 0, "test_0x7f_not_text");
    }
    {
        KeyEvent e = translate_key(0xe9, 0);  // Latin-1 é
        check(e.key == Key::Text && e.ch == 0xe9, "test_latin1_is_text");
    }
    {
        KeyEvent e = translate_key(0x9f, 0);  // Latin-1 control gap
        check(e.key == Key::Unknown, "test_latin1_gap_not_text");
    }
    {
        KeyEvent e = translate_key(kXK_Return, 0);
        check(e.key == Key::Enter && e.ch == 0, "test_return_maps_to_enter");
    }
    {
        KeyEvent e = translate_key(kXK_BackSpace, 0);
        check(e.key == Key::Backspace, "test_backspace");
    }
    {
        KeyEvent e = translate_key(kXK_Left, kShiftMask);
        check(e.key == Key::Left && e.shift && !e.ctrl,
              "test_shift_left_for_selection");
    }
    {
        KeyEvent e = translate_key(0x73, kControlMask);  // Ctrl+S
        check(e.key == Key::Text && e.ch == 0x73 && e.ctrl && !e.shift,
              "test_ctrl_s_shortcut");
    }
    {
        KeyEvent e = translate_key(0x71, kControlMask | kMod1Mask);
        check(e.ctrl && e.alt && !e.shift, "test_ctrl_alt_combo");
    }
    {   // Caps Lock and NumLock-ish bits must be ignored, not fatal.
        KeyEvent e = translate_key(0x61, kLockMask | (1u << 4));
        check(e.key == Key::Text && e.ch == 0x61 && !e.shift && !e.ctrl &&
              !e.alt, "test_lock_bits_ignored");
    }
    {
        KeyEvent e = translate_key(0xffbe, 0);  // XK_F1
        check(e.key == Key::Unknown && e.ch == 0, "test_f1_unknown");
    }
    {
        KeyEvent e = translate_key(kXK_Delete, 0);
        check(e.key == Key::Delete, "test_delete_key");
    }
    {
        KeyEvent e = translate_key(kXK_Home, 0);
        KeyEvent f = translate_key(kXK_End, 0);
        KeyEvent g = translate_key(kXK_Up, 0);
        KeyEvent h = translate_key(kXK_Down, 0);
        KeyEvent i = translate_key(kXK_Right, 0);
        KeyEvent j = translate_key(kXK_Tab, 0);
        KeyEvent k = translate_key(kXK_Escape, 0);
        check(f.key == Key::End && e.key == Key::Home && g.key == Key::Up &&
              h.key == Key::Down && i.key == Key::Right &&
              j.key == Key::Tab && k.key == Key::Escape,
              "test_all_named_keys");
    }
    return failed;
}
```

## Challenge: Atoms and the Close Button {#wm-protocols points=12}

Two functions, straight out of the annotated program above.

First, model the server's atom table. `intern(table, name)` returns the
atom for `name`, creating it if needed — the same name must always yield
the same atom, and atom `0` is reserved to mean "None" (so your first
atom is 1). This is exactly the contract of `xcb_intern_atom`.

Second, `handle_client_message` — the routing logic your
`XCB_CLIENT_MESSAGE` case delegates to:

- If the message's `type` is not the `WM_PROTOCOLS` atom, it's not ours:
  return `WmAction::None`.
- If `data0` is the `WM_DELETE_WINDOW` atom: return `WmAction::Close`.
  (The caller decides whether to prompt about unsaved changes.)
- If `data0` is the `_NET_WM_PING` atom: return `WmAction::PingReply`
  with `reply` equal to the incoming message *except* `window` replaced
  by `root_window` — that's the echo the window manager is waiting for,
  timestamp and all.
- Any other protocol: `WmAction::None`.

`handle_client_message` must intern the three protocol names itself
(interning is idempotent, so calling it repeatedly is safe and cheap —
in the real backend you'd cache them, but the semantics are identical).

### Starter

```cpp
#include <string>
#include <string_view>
#include <vector>

// An atom is an interned string ID. 0 means "None"; real atoms start at 1.
using Atom = unsigned;

// The server's atom table, modeled as the list of interned names:
// table[i] is the name of atom i + 1.
Atom intern(std::vector<std::string>& table, std::string_view name) {
    // TODO: return the existing atom for name, or append and return the new one
    (void)table;
    (void)name;
    return 0;
}

struct ClientMessage {
    unsigned window = 0;  // the window the message was addressed to
    Atom type = 0;        // message category (an atom)
    Atom data0 = 0;       // first data word: for WM_PROTOCOLS, the protocol
    unsigned data1 = 0;   // e.g. the ping timestamp
    unsigned data2 = 0;
};

enum class WmAction { None, Close, PingReply };

struct WmResult {
    WmAction action = WmAction::None;
    ClientMessage reply{};  // meaningful only when action == PingReply
};

WmResult handle_client_message(std::vector<std::string>& atoms,
                               const ClientMessage& msg,
                               unsigned root_window) {
    // TODO: intern WM_PROTOCOLS, WM_DELETE_WINDOW, _NET_WM_PING; route.
    (void)atoms;
    (void)msg;
    (void)root_window;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // Interning basics.
        std::vector<std::string> t;
        Atom a = intern(t, "WM_PROTOCOLS");
        Atom b = intern(t, "WM_DELETE_WINDOW");
        Atom a2 = intern(t, "WM_PROTOCOLS");
        check(a != 0 && b != 0, "test_atoms_are_nonzero");
        check(a != b, "test_distinct_names_distinct_atoms");
        check(a == a2, "test_intern_is_idempotent");
        check(t.size() == 2, "test_no_duplicate_entries");
    }

    {   // Close request.
        std::vector<std::string> t;
        Atom proto = intern(t, "WM_PROTOCOLS");
        Atom del = intern(t, "WM_DELETE_WINDOW");
        ClientMessage m{700, proto, del, 0, 0};
        WmResult r = handle_client_message(t, m, 99);
        check(r.action == WmAction::Close, "test_delete_window_closes");
    }

    {   // Ping must be echoed to the root window.
        std::vector<std::string> t;
        Atom proto = intern(t, "WM_PROTOCOLS");
        Atom ping = intern(t, "_NET_WM_PING");
        ClientMessage m{700, proto, ping, 123456, 700};
        WmResult r = handle_client_message(t, m, 42);
        check(r.action == WmAction::PingReply, "test_ping_replies");
        check(r.reply.window == 42, "test_ping_readdressed_to_root");
        check(r.reply.type == proto && r.reply.data0 == ping &&
              r.reply.data1 == 123456 && r.reply.data2 == 700,
              "test_ping_payload_echoed");
    }

    {   // Unknown protocol under WM_PROTOCOLS: ignore.
        std::vector<std::string> t;
        Atom proto = intern(t, "WM_PROTOCOLS");
        Atom other = intern(t, "_NET_WM_SYNC_REQUEST");
        ClientMessage m{700, proto, other, 0, 0};
        check(handle_client_message(t, m, 42).action == WmAction::None,
              "test_unknown_protocol_ignored");
    }

    {   // Non-WM_PROTOCOLS message: not ours, even if data0 looks right.
        std::vector<std::string> t;
        Atom del = intern(t, "WM_DELETE_WINDOW");
        Atom unrelated = intern(t, "XdndEnter");
        ClientMessage m{700, unrelated, del, 0, 0};
        check(handle_client_message(t, m, 42).action == WmAction::None,
              "test_wrong_type_ignored");
    }

    {   // The handler interns names itself on a fresh table.
        std::vector<std::string> t;
        ClientMessage m{1, 0, 0, 0, 0};
        WmResult r = handle_client_message(t, m, 5);
        check(r.action == WmAction::None, "test_fresh_table_no_crash");
        Atom proto = intern(t, "WM_PROTOCOLS");
        Atom del = intern(t, "WM_DELETE_WINDOW");
        ClientMessage m2{1, proto, del, 0, 0};
        check(handle_client_message(t, m2, 5).action == WmAction::Close,
              "test_handler_interned_names_consistently");
    }

    return failed;
}
```
# Lesson: A Framebuffer of Your Own {#a-framebuffer-of-your-own}

When the Expose event arrives, you need pixels to show. Modern toolkits
route everything through the GPU; we're going to render in **software** —
computing every pixel on the CPU into a plain array — and there's nothing
apologetic about that choice. A text editor's frame is mostly blank space
and a few thousand small glyphs; software rendering handled this fine on
25 MHz machines, it handles 4K displays fine today, and it gives us
something priceless for this course: rendering that is *testable* — a
`std::vector` you can assert on — and identical on every platform. (Sublime
Text shipped for years on a software rasterizer; VS Code's terminal used
one; every 2D toolkit keeps a software fallback.)

### The pixel array

A framebuffer is a width, a height, and `w * h` 32-bit pixels in
**row-major** order: pixel (x, y) lives at index `y * w + x`. Rows are
contiguous; walking x is cache-friendly, walking y strides by `w`. The
32-bit value packs the color. We'll use the byte order X11, Windows, and
macOS all natively use on little-endian machines: `0xAARRGGBB` — blue in
the low byte. White is `0xFFFFFFFF`, a pleasant editor-background off-white
is `0xFFFAF8F0`, black text is `0xFF000000`.

```cpp
class Framebuffer {
public:
    Framebuffer(int w, int h) : w_(w), h_(h), px_(size_t(w) * h, 0) {}
    int width() const { return w_; }
    int height() const { return h_; }
    uint32_t at(int x, int y) const { return px_[size_t(y) * w_ + x]; }
    // drawing ops go here
private:
    int w_, h_;
    std::vector<uint32_t> px_;
};
```

`std::vector` rather than `new uint32_t[]` is not a style nicety: the
vector makes `Framebuffer` copyable, movable, and leak-proof with zero
special members written — rule of zero again. Resizing the window? Build a
new `Framebuffer` and move-assign it.

### Clipping is the whole game

Every drawing routine in a real renderer spends its first lines on the
same question: *which part of this shape actually lands inside the
buffer?* Draw a rect at x = −5 or a glyph half off the right edge, and the
naive loop writes outside the array — instant heap corruption (or in our
graded builds, an AddressSanitizer abort, which is the same bug caught
politely). The discipline:

- **Clip, then loop.** Intersect the requested rect with the buffer's
  bounds rect *before* touching memory, then loop over the clipped rect
  only. One `intersect` call — the one you wrote in lesson 2 — replaces
  four per-pixel `if`s and is dramatically faster.
- **Clip rects compose.** "Only draw inside the text area" (don't paint
  over the scrollbar) is just one more `intersect` against a caller-
  supplied clip rect. Renderers keep a *clip stack*; ours keeps one rect,
  which is all an editor needs.

### Scrolling without repainting

Here's the trick that made editors feel instant on slow hardware, and
still cuts your paint cost 20× today: when the user scrolls one line, the
new frame is 95% *the old frame, shifted*. So don't re-render — **blit**:
copy the surviving band to its new position within the same buffer, then
repaint only the newly revealed strip. (Lesson 12 computes exactly which
strip.)

Copying a rect *within one buffer* has a classic trap: if source and
destination overlap, the copy direction matters. Copy downward (dest below
source) iterating rows top-to-bottom and you'll read rows you've already
overwritten — the visual result is one source row smeared down the screen.
The fix is the `memmove` insight: iterate rows *bottom-up* when moving
down, *top-up* when moving up, and within a row let `std::memmove` (not
`memcpy` — overlap within the row is possible when shifting horizontally)
handle the bytes.

### Getting pixels onto glass

The framebuffer is portable; the last hop isn't. Each backend implements
`PlatformWindow::blit` in a few lines:

- **X11:** `xcb_put_image` ships the array to the server over the socket.
  That's a full copy per frame — fine at editor scale — and the reason the
  **MIT-SHM** extension exists: place the pixels in a POSIX shared-memory
  segment, and `xcb_shm_put_image` makes the server read them directly, no
  copy. Same array, faster hop; the SHM segment ID is precisely the kind
  of resource your `UniqueHandle` from lesson 2 wraps.
- **Win32:** in the `WM_PAINT` handler, `StretchDIBits(hdc, ...)` pushes a
  device-independent bitmap — your array with a small `BITMAPINFO` header
  saying "32-bit, top-down" — onto the window's device context.
- **Cocoa:** wrap the bytes in a `CGDataProvider` → `CGImageCreate` →
  draw it in `drawRect:`. (Or set it as the contents of a `CALayer`.)

Note what stayed portable: everything except one function per platform.
That's the seam doing its job.

## Challenge: Fill, Clipped {#fill-rect points=12}

Implement the workhorse: `fill_rect`, plus the variant that also respects
a caller-supplied clip rect. The starter includes the lesson-2 geometry
kit, solved — build on `intersect` rather than re-deriving edge cases.

Requirements:

- `fill_rect(r, color)`: set every pixel inside `r ∩ bounds` to `color`.
  Rects partially or fully outside the buffer must not touch out-of-bounds
  memory (the tests run under AddressSanitizer — any stray write fails the
  run).
- `fill_rect_clipped(r, clip, color)`: same, but only inside
  `r ∩ clip ∩ bounds`.
- Empty input rects paint nothing.

### Starter

```cpp
#include <algorithm>
#include <cstdint>
#include <vector>

struct Point {
    int x = 0;
    int y = 0;
};

struct Rect {
    int x = 0, y = 0, w = 0, h = 0;
    bool empty() const { return w <= 0 || h <= 0; }
};

inline Rect intersect(Rect a, Rect b) {
    if (a.empty() || b.empty())
        return {};
    int x0 = std::max(a.x, b.x), y0 = std::max(a.y, b.y);
    int x1 = std::min(a.x + a.w, b.x + b.w);
    int y1 = std::min(a.y + a.h, b.y + b.h);
    if (x1 <= x0 || y1 <= y0)
        return {};
    return {x0, y0, x1 - x0, y1 - y0};
}

class Framebuffer {
public:
    Framebuffer(int w, int h) : w_(w), h_(h), px_(size_t(w) * h, 0) {}

    int width() const { return w_; }
    int height() const { return h_; }
    uint32_t at(int x, int y) const { return px_[size_t(y) * w_ + x]; }
    Rect bounds() const { return {0, 0, w_, h_}; }

    void fill_rect(Rect r, uint32_t color) {
        // TODO: clip to bounds(), then loop rows/columns
        (void)r;
        (void)color;
    }

    void fill_rect_clipped(Rect r, Rect clip, uint32_t color) {
        // TODO: one more intersect, same loop
        (void)r;
        (void)clip;
        (void)color;
    }

private:
    int w_, h_;
    std::vector<uint32_t> px_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

// Count pixels holding `color`, and verify they lie exactly inside `r`.
static bool only_inside(const Framebuffer& fb, Rect r, uint32_t color) {
    for (int y = 0; y < fb.height(); ++y) {
        for (int x = 0; x < fb.width(); ++x) {
            bool in = x >= r.x && x < r.x + r.w && y >= r.y && y < r.y + r.h;
            if ((fb.at(x, y) == color) != in)
                return false;
        }
    }
    return true;
}

int main() {
    {   // Interior fill.
        Framebuffer fb(10, 8);
        fb.fill_rect({2, 3, 4, 2}, 0xFFAA0000u);
        check(only_inside(fb, {2, 3, 4, 2}, 0xFFAA0000u), "test_interior_fill");
    }

    {   // Full-buffer fill hits every pixel including edges.
        Framebuffer fb(6, 6);
        fb.fill_rect({0, 0, 6, 6}, 7u);
        check(only_inside(fb, {0, 0, 6, 6}, 7u), "test_full_fill");
    }

    {   // Negative origin: clipped, no crash.
        Framebuffer fb(10, 10);
        fb.fill_rect({-3, -4, 6, 7}, 9u);
        check(only_inside(fb, {0, 0, 3, 3}, 9u), "test_clip_top_left");
    }

    {   // Overhanging bottom-right corner.
        Framebuffer fb(10, 10);
        fb.fill_rect({8, 9, 100, 100}, 5u);
        check(only_inside(fb, {8, 9, 2, 1}, 5u), "test_clip_bottom_right");
    }

    {   // Entirely outside: nothing painted.
        Framebuffer fb(10, 10);
        fb.fill_rect({50, 50, 5, 5}, 3u);
        fb.fill_rect({-20, 0, 10, 10}, 3u);
        check(only_inside(fb, {0, 0, 0, 0}, 3u), "test_fully_outside");
    }

    {   // Empty rect paints nothing.
        Framebuffer fb(10, 10);
        fb.fill_rect({2, 2, 0, 5}, 4u);
        fb.fill_rect({2, 2, 5, -1}, 4u);
        check(only_inside(fb, {0, 0, 0, 0}, 4u), "test_empty_rect");
    }

    {   // Clip rect constrains the fill.
        Framebuffer fb(10, 10);
        fb.fill_rect_clipped({0, 0, 10, 10}, {3, 3, 4, 4}, 11u);
        check(only_inside(fb, {3, 3, 4, 4}, 11u), "test_clip_rect_applied");
    }

    {   // Clip rect itself may overhang the buffer.
        Framebuffer fb(10, 10);
        fb.fill_rect_clipped({0, 0, 10, 10}, {7, 7, 100, 100}, 13u);
        check(only_inside(fb, {7, 7, 3, 3}, 13u), "test_clip_rect_clipped_too");
    }

    {   // Disjoint rect and clip: nothing.
        Framebuffer fb(10, 10);
        fb.fill_rect_clipped({0, 0, 3, 3}, {5, 5, 3, 3}, 15u);
        check(only_inside(fb, {0, 0, 0, 0}, 15u), "test_disjoint_clip");
    }

    return failed;
}
```

## Challenge: The Scroll Blit {#scroll-blit points=15}

Implement `copy_rect`: copy the pixels of `src` so that `src`'s top-left
corner lands at `(dst_x, dst_y)` — *within the same buffer*, overlap
allowed. This is the routine your scroll handler calls before repainting
the revealed strip.

Semantics, in order:

1. Clip `src` against the buffer bounds. If clipping moves `src`'s origin
   (e.g. `src.x` was negative), the destination origin shifts by the same
   amount — clipping trims the *shape*, it doesn't slide the image.
2. Clip the destination rect (now `src.w × src.h` at the shifted origin)
   against the bounds, trimming the source correspondingly.
3. Copy whatever survives. Overlapping regions must copy correctly:
   choose row order (and use `std::memmove`, or equivalent care, within
   rows) so no pixel is read after being overwritten.
4. If nothing survives clipping, do nothing.

The starter's `Framebuffer` arrives with `fill_rect` working, and the
tests paint every pixel a unique value first — any smear, off-by-one, or
wrong-direction copy shows up as a mismatched pixel.

### Starter

```cpp
#include <algorithm>
#include <cstdint>
#include <cstring>
#include <vector>

struct Rect {
    int x = 0, y = 0, w = 0, h = 0;
    bool empty() const { return w <= 0 || h <= 0; }
};

inline Rect intersect(Rect a, Rect b) {
    if (a.empty() || b.empty())
        return {};
    int x0 = std::max(a.x, b.x), y0 = std::max(a.y, b.y);
    int x1 = std::min(a.x + a.w, b.x + b.w);
    int y1 = std::min(a.y + a.h, b.y + b.h);
    if (x1 <= x0 || y1 <= y0)
        return {};
    return {x0, y0, x1 - x0, y1 - y0};
}

class Framebuffer {
public:
    Framebuffer(int w, int h) : w_(w), h_(h), px_(size_t(w) * h, 0) {}

    int width() const { return w_; }
    int height() const { return h_; }
    uint32_t at(int x, int y) const { return px_[size_t(y) * w_ + x]; }
    Rect bounds() const { return {0, 0, w_, h_}; }

    void fill_rect(Rect r, uint32_t color) {
        Rect c = intersect(r, bounds());
        for (int y = c.y; y < c.y + c.h; ++y)
            for (int x = c.x; x < c.x + c.w; ++x)
                px_[size_t(y) * w_ + x] = color;
    }

    uint32_t* row(int y) { return px_.data() + size_t(y) * w_; }

private:
    int w_, h_;
    std::vector<uint32_t> px_;
};

// Copy src's pixels so src's top-left lands at (dst_x, dst_y).
// Same-buffer overlap must work; everything is clipped to the bounds.
void copy_rect(Framebuffer& fb, Rect src, int dst_x, int dst_y) {
    // TODO
    (void)fb;
    (void)src;
    (void)dst_x;
    (void)dst_y;
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

// Give every pixel a unique, position-derived value.
static void stamp(Framebuffer& fb) {
    for (int y = 0; y < fb.height(); ++y)
        for (int x = 0; x < fb.width(); ++x)
            fb.fill_rect({x, y, 1, 1}, uint32_t(1000 + y * 100 + x));
}

static uint32_t val(int x, int y) { return uint32_t(1000 + y * 100 + x); }

int main() {
    {   // Scroll up: rows 2..5 move to rows 0..3. No overlap issues upward.
        Framebuffer fb(8, 8);
        stamp(fb);
        copy_rect(fb, {0, 2, 8, 4}, 0, 0);
        bool ok = true;
        for (int y = 0; y < 4; ++y)
            for (int x = 0; x < 8; ++x)
                ok = ok && fb.at(x, y) == val(x, y + 2);
        for (int x = 0; x < 8; ++x)   // below the copy: untouched
            ok = ok && fb.at(x, 6) == val(x, 6);
        check(ok, "test_scroll_up");
    }

    {   // Overlapping downward copy: must iterate bottom-up.
        Framebuffer fb(8, 8);
        stamp(fb);
        copy_rect(fb, {0, 0, 8, 4}, 0, 2);
        bool ok = true;
        for (int y = 0; y < 4; ++y)
            for (int x = 0; x < 8; ++x)
                ok = ok && fb.at(x, y + 2) == val(x, y);
        check(ok, "test_overlap_down_no_smear");
    }

    {   // Overlapping rightward copy within rows: memmove territory.
        Framebuffer fb(8, 2);
        stamp(fb);
        copy_rect(fb, {0, 0, 6, 1}, 2, 0);
        bool ok = true;
        for (int x = 0; x < 6; ++x)
            ok = ok && fb.at(x + 2, 0) == val(x, 0);
        ok = ok && fb.at(0, 0) == val(0, 0) && fb.at(1, 0) == val(1, 0);
        check(ok, "test_overlap_right_no_smear");
    }

    {   // Overlapping leftward copy.
        Framebuffer fb(8, 1);
        stamp(fb);
        copy_rect(fb, {2, 0, 6, 1}, 0, 0);
        bool ok = true;
        for (int x = 0; x < 6; ++x)
            ok = ok && fb.at(x, 0) == val(x + 2, 0);
        check(ok, "test_overlap_left_no_smear");
    }

    {   // Source overhangs the buffer: clipped shape, shifted destination.
        Framebuffer fb(8, 8);
        stamp(fb);
        // src {6,6,4,4} clips to {6,6,2,2}; destination stays put at (0,0)
        // because clipping didn't move the origin.
        copy_rect(fb, {6, 6, 4, 4}, 0, 0);
        bool ok = fb.at(0, 0) == val(6, 6) && fb.at(1, 0) == val(7, 6) &&
                  fb.at(0, 1) == val(6, 7) && fb.at(1, 1) == val(7, 7) &&
                  fb.at(2, 0) == val(2, 0);   // beyond clipped copy: untouched
        check(ok, "test_src_clipped");
    }

    {   // Source origin clipped: destination shifts with it.
        Framebuffer fb(8, 8);
        stamp(fb);
        // src {-2,-2,4,4} clips to {0,0,2,2}, origin moved by (+2,+2), so
        // the copy lands at (4+2, 4+2) = (6,6).
        copy_rect(fb, {-2, -2, 4, 4}, 4, 4);
        bool ok = fb.at(6, 6) == val(0, 0) && fb.at(7, 7) == val(1, 1) &&
                  fb.at(4, 4) == val(4, 4);   // unshifted spot untouched
        check(ok, "test_negative_src_shifts_dst");
    }

    {   // Destination overhangs: partial copy, no out-of-bounds writes.
        Framebuffer fb(8, 8);
        stamp(fb);
        copy_rect(fb, {0, 0, 4, 4}, 6, 6);
        bool ok = fb.at(6, 6) == val(0, 0) && fb.at(7, 7) == val(1, 1) &&
                  fb.at(5, 5) == val(5, 5);
        check(ok, "test_dst_clipped");
    }

    {   // Fully clipped away: buffer unchanged.
        Framebuffer fb(4, 4);
        stamp(fb);
        copy_rect(fb, {0, 0, 4, 4}, 10, 10);
        copy_rect(fb, {10, 10, 4, 4}, 0, 0);
        bool ok = true;
        for (int y = 0; y < 4; ++y)
            for (int x = 0; x < 4; ++x)
                ok = ok && fb.at(x, y) == val(x, y);
        check(ok, "test_nothing_survives_clip");
    }

    return failed;
}
```
# Lesson: Drawing Text: Bitmap Fonts First {#drawing-text-bitmap-fonts}

An editor's rendering budget is 99% text, so this is where the framebuffer
starts earning its keep. Real font rendering is one of the deepest rabbit
holes in systems programming — outlines, hinting, subpixel antialiasing,
shaping — so we take the classic route: get a **bitmap font** working
end-to-end first, with the right *abstractions*, then survey how the real
stack slots into the same interfaces.

### The vocabulary: baseline, ascent, descent, advance

Text is not positioned by its top-left corner. Every professional text API
— FreeType, DirectWrite, CoreText, the lot — positions glyphs on the
**baseline**: the invisible line the letters sit on. Above it, a font
reserves the **ascent** (tall letters, capitals); below, the **descent**
(the tails of g, y, p). Per glyph, the **advance** says how far the pen
moves rightward for the next glyph — for proportional fonts it varies (i
narrow, W wide); for monospace it doesn't.

Why baseline-relative? Because mixed content must *align*. If you ever
draw two fonts on one line — or an emoji next to text — their baselines
must coincide, not their tops. Adopting the convention now costs one
subtraction (`top = baseline - ascent`) and saves a rewrite later. A
line of text in a box of height `line_height = ascent + descent +
line_gap` puts its baseline at `box_top + ascent`; that formula appears in
every paint function you'll write from here to the final challenge.

### The humble bitmap font

Our font is the time-honored terminal format: every printable ASCII glyph
is an 8×16 monochrome bitmap — 16 bytes per glyph, one byte per row, bit 7
the leftmost pixel. (This is exactly the format of the VGA text-mode fonts
and of X11's classic `misc-fixed` family; public-domain dumps abound, or
draw your own.) The whole ASCII range is 95 glyphs × 16 bytes = 1520 bytes
you embed straight into the executable as a `constexpr` array — no font
file, no loader, no I/O failure modes:

```cpp
// 'A' — one byte per row, MSB = leftmost pixel   bits, visualized:
constexpr uint8_t kGlyphA[16] = {
    0x00, 0x00,                     //  . . . . . . . .
    0x10,                           //  . . . # . . . .
    0x28,                           //  . . # . # . . .
    0x44,                           //  . # . . . # . .
    0x82, 0x82,                     //  # . . . . . # .   (x2)
    0xFE,                           //  # # # # # # # .
    0x82, 0x82, 0x82,               //  # . . . . . # .   (x3)
    0x00, 0x00, 0x00, 0x00, 0x00,   //  padding to the descent
};
```

Drawing a glyph is a masked blit: for each set bit, write the foreground
color; for each clear bit, *touch nothing* (the background was already
painted by `fill_rect` — this is what lets selection highlights show
through text). And because a glyph at the window edge is half outside the
buffer, the blit clips exactly like `fill_rect` did. For our font,
ascent = 12 and descent = 4: the baseline runs through glyph row 12.

### How the real stack fits the same holes

Swap the bitmap font out later and nothing above the font abstraction
changes. What goes in its place, on any modern OS, is a *pipeline*:

- **FreeType** (Linux; also inside Android, game engines) parses TrueType/
  OpenType files and rasterizes **outlines** — quadratic/cubic Bézier
  contours — into exactly the kind of small bitmaps we're blitting, one
  per glyph per size, which you cache. It also reports the real metrics:
  per-glyph advance, ascent/descent, and **kerning** — per-*pair* spacing
  adjustments, because "AV" set at plain advances looks like "A V" (the
  diagonals leave a hole; the kern pair pulls V leftward a few pixels).
- **HarfBuzz** answers a question we've quietly dodged: *which glyphs?*
  For English, char→glyph is a table lookup. For Arabic (letters change
  shape by position), Devanagari (characters reorder), or "fi" ligatures,
  a **shaping engine** converts a codepoint sequence into a glyph
  sequence with positions. That's why serious text APIs take whole runs,
  never one char at a time — an interface lesson our `draw_text` (which
  takes a string) respects, and a per-char `draw_glyph` wouldn't.
- **DirectWrite** (Windows) and **CoreText** (macOS) are those two layers
  fused into one platform service each — plus rasterization tuned for
  their compositor (ClearType subpixel AA; macOS's grayscale AA).

The seam design writes itself: the editor core measures text through a
`FontMetrics` value (ascent, descent, per-char advances, kern pairs) and
never asks *how* pixels get made. This lesson's second challenge builds
that measuring kit; the layout engine in lesson 9 consumes it unchanged
whether the numbers came from a `constexpr` table or from FreeType.

One habit to start now: **never position text by summing widths as you
render**. Measure with the same code you'll hit-test with. If measurement
and rendering ever disagree — one applies kerning, the other forgets — the
caret lands *between* pixels of a glyph and users file bugs that say "the
cursor is drunk". One function, one truth.

## Challenge: Blit a Glyph {#glyph-blit points=12}

Implement `draw_glyph` and `draw_text` for the 8×16 bitmap format. The
starter ships the geometry kit and a working `Framebuffer` from lesson 4.

Rules:

- A glyph is 16 bytes, one per row, bit 7 leftmost: pixel column `c` of
  row `r` is set iff `rows[r] & (0x80 >> c)`.
- `draw_glyph(fb, rows, x, baseline, fg)` puts the glyph's top-left at
  `(x, baseline - kAscent)`. Set bits are painted `fg`; clear bits leave
  the buffer untouched. Everything clips to the buffer (the tests run
  glyphs off all four edges under AddressSanitizer).
- `draw_text(fb, font, s, x, baseline, fg)` draws `s` left to right
  starting at pen position `x`, advancing `kGlyphW` per character.
  Characters in `' '..'~'` use `font[ch - 32]`; anything else draws
  nothing but still advances (the classic "tofu" slot would go here).
  Returns the final pen x.

### Starter

```cpp
#include <algorithm>
#include <cstdint>
#include <string_view>
#include <vector>

constexpr int kGlyphW = 8;
constexpr int kGlyphH = 16;
constexpr int kAscent = 12;   // baseline runs through glyph row 12

struct Rect {
    int x = 0, y = 0, w = 0, h = 0;
    bool empty() const { return w <= 0 || h <= 0; }
};

inline Rect intersect(Rect a, Rect b) {
    if (a.empty() || b.empty())
        return {};
    int x0 = std::max(a.x, b.x), y0 = std::max(a.y, b.y);
    int x1 = std::min(a.x + a.w, b.x + b.w);
    int y1 = std::min(a.y + a.h, b.y + b.h);
    if (x1 <= x0 || y1 <= y0)
        return {};
    return {x0, y0, x1 - x0, y1 - y0};
}

class Framebuffer {
public:
    Framebuffer(int w, int h) : w_(w), h_(h), px_(size_t(w) * h, 0) {}

    int width() const { return w_; }
    int height() const { return h_; }
    uint32_t at(int x, int y) const { return px_[size_t(y) * w_ + x]; }
    Rect bounds() const { return {0, 0, w_, h_}; }

    void set(int x, int y, uint32_t color) {
        if (x >= 0 && x < w_ && y >= 0 && y < h_)
            px_[size_t(y) * w_ + x] = color;
    }

private:
    int w_, h_;
    std::vector<uint32_t> px_;
};

// Draw one 8x16 glyph with its top-left at (x, baseline - kAscent).
// Set bits paint fg; clear bits are transparent. Clips to the buffer.
void draw_glyph(Framebuffer& fb, const uint8_t* rows, int x, int baseline,
                uint32_t fg) {
    // TODO
    (void)fb;
    (void)rows;
    (void)x;
    (void)baseline;
    (void)fg;
}

// Draw s starting at pen x; font[i] is the glyph for character 32 + i.
// Characters outside ' '..'~' advance without drawing. Returns final pen x.
int draw_text(Framebuffer& fb, const uint8_t (*font)[16], std::string_view s,
              int x, int baseline, uint32_t fg) {
    // TODO
    (void)fb;
    (void)font;
    (void)s;
    (void)baseline;
    (void)fg;
    return x;
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <cstring>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static int count_colored(const Framebuffer& fb, uint32_t fg) {
    int n = 0;
    for (int y = 0; y < fb.height(); ++y)
        for (int x = 0; x < fb.width(); ++x)
            if (fb.at(x, y) == fg)
                ++n;
    return n;
}

int main() {
    const uint32_t FG = 0xFF112233u;

    {   // Corner bits land exactly where the metrics say.
        uint8_t g[16] = {};
        g[0] = 0x80;   // top-left pixel of the glyph cell
        g[15] = 0x01;  // bottom-right pixel
        Framebuffer fb(32, 32);
        draw_glyph(fb, g, 10, 20, FG);
        // top-left = (10, 20 - 12) = (10, 8); bottom-right = (17, 23).
        check(fb.at(10, 8) == FG, "test_top_left_bit_position");
        check(fb.at(17, 23) == FG, "test_bottom_right_bit_position");
        check(count_colored(fb, FG) == 2, "test_only_set_bits_painted");
    }

    {   // Clear bits are transparent: existing pixels survive.
        uint8_t g[16] = {};
        g[3] = 0xAA;   // 10101010
        Framebuffer fb(16, 16);
        for (int x = 0; x < 16; ++x)
            fb.set(x, 3, 0xBBu);   // pre-paint the row the glyph row hits
        draw_glyph(fb, g, 0, 12, FG);   // top at y=0, so glyph row 3 -> y=3
        bool ok = true;
        for (int c = 0; c < 8; ++c) {
            uint32_t want = (c % 2 == 0) ? FG : 0xBBu;
            ok = ok && fb.at(c, 3) == want;
        }
        check(ok, "test_transparent_background");
    }

    {   // Clipping on all four sides, ASan verifies no stray writes.
        uint8_t solid[16];
        std::memset(solid, 0xFF, sizeof solid);
        Framebuffer fb(8, 8);
        draw_glyph(fb, solid, -5, 4, FG);        // off left + top
        draw_glyph(fb, solid, 5, 15, FG);        // off right + bottom
        draw_glyph(fb, solid, -100, -100, FG);   // fully outside
        // Left draw: cols -5..2 visible 0..2, rows -8..7 visible 0..7.
        check(fb.at(0, 0) == FG && fb.at(2, 7) == FG && fb.at(3, 0) != FG,
              "test_clipped_left_top");
        check(fb.at(5, 7) == FG && fb.at(4, 7) != FG,
              "test_clipped_right_bottom");
    }

    {   // draw_text: placement, advance, return value.
        static uint8_t font[95][16] = {};
        std::memset(font['A' - 32], 0xFF, 16);   // solid block
        font['B' - 32][0] = 0x81;                // two corner bits, row 0
        Framebuffer fb(64, 16);
        int pen = draw_text(fb, font, "AB", 0, 12, FG);
        check(pen == 16, "test_pen_advances_8_per_char");
        check(fb.at(0, 0) == FG && fb.at(7, 15) == FG,
              "test_first_glyph_at_origin");
        check(fb.at(8, 0) == FG && fb.at(15, 0) == FG && fb.at(9, 0) != FG,
              "test_second_glyph_at_x8");
    }

    {   // Non-printable characters advance silently.
        static uint8_t font[95][16] = {};
        std::memset(font['X' - 32], 0xFF, 16);
        Framebuffer fb(64, 16);
        int pen = draw_text(fb, font, "\x01X", 0, 12, FG);
        check(pen == 16, "test_unprintable_advances");
        check(fb.at(8, 0) == FG && fb.at(0, 0) != FG,
              "test_unprintable_draws_nothing");
        // Bytes >= 127 must not index the font table (ASan would catch it).
        Framebuffer fb2(64, 16);
        int pen2 = draw_text(fb2, font, "\x7f\xef", 0, 12, FG);
        check(pen2 == 16 && count_colored(fb2, FG) == 0,
              "test_high_bytes_safe");
    }

    {   // Empty string: no pixels, pen unchanged.
        static uint8_t font[95][16] = {};
        Framebuffer fb(16, 16);
        check(draw_text(fb, font, "", 5, 12, FG) == 5 &&
              count_colored(fb, FG) == 0,
              "test_empty_string");
    }

    return failed;
}
```

## Challenge: Measure Before You Draw {#measure-text points=12}

Build the measuring kit: the `FontMetrics` value type the layout engine
and hit-testing will consume. This one is proportional-font ready —
per-character advances plus kerning pairs — so that when FreeType replaces
the bitmap font, only the numbers change.

Semantics:

- `line_height(m)` = `ascent + descent + line_gap`.
- `advance_of(m, c)`: the advance for `c` from the table (`advances[c -
  32]` for `' '..'~'`); any other character measures as `'?'` does.
- `kern_between(m, l, r)`: the adjustment for the exact pair (l, r), or 0
  if the pair isn't listed.
- `measure(m, s)`: total advance of `s` — the sum of every character's
  advance plus the kern between each adjacent pair.
- `caret_xs(m, s)`: the n+1 caret positions in `s`: `caret_xs(s)[i]` is
  `measure` of the first `i` characters (so `[0]` is 0 and `[n]` is
  `measure(m, s)`). A kern pair contributes only once both its characters
  are inside the prefix.

### Starter

```cpp
#include <string_view>
#include <vector>

struct KernPair {
    char left;
    char right;
    int adjust;   // usually negative: pull right glyph leftward
};

struct FontMetrics {
    int ascent = 12;
    int descent = 4;
    int line_gap = 0;
    int advances[95] = {};   // ' ' (32) .. '~' (126)
    std::vector<KernPair> kerning;
};

int line_height(const FontMetrics& m) {
    // TODO
    (void)m;
    return 0;
}

int advance_of(const FontMetrics& m, char c) {
    // TODO: out-of-range characters measure as '?'
    (void)m;
    (void)c;
    return 0;
}

int kern_between(const FontMetrics& m, char l, char r) {
    // TODO
    (void)m;
    (void)l;
    (void)r;
    return 0;
}

int measure(const FontMetrics& m, std::string_view s) {
    // TODO
    (void)m;
    (void)s;
    return 0;
}

std::vector<int> caret_xs(const FontMetrics& m, std::string_view s) {
    // TODO: s.size() + 1 entries
    (void)m;
    (void)s;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static FontMetrics make_metrics() {
    FontMetrics m;
    m.ascent = 10;
    m.descent = 3;
    m.line_gap = 2;
    for (int i = 0; i < 95; ++i)
        m.advances[i] = 8;               // monospace baseline...
    m.advances['i' - 32] = 4;            // ...with a few proportional gaps
    m.advances['W' - 32] = 12;
    m.advances['?' - 32] = 9;
    m.kerning.push_back({'A', 'V', -2});
    m.kerning.push_back({'V', 'A', -3});
    return m;
}

int main() {
    FontMetrics m = make_metrics();

    check(line_height(m) == 15, "test_line_height");

    check(advance_of(m, 'x') == 8, "test_advance_default");
    check(advance_of(m, 'i') == 4, "test_advance_narrow");
    check(advance_of(m, 'W') == 12, "test_advance_wide");
    check(advance_of(m, '\n') == 9, "test_out_of_range_measures_as_question");
    check(advance_of(m, '\x7f') == 9, "test_del_measures_as_question");

    check(kern_between(m, 'A', 'V') == -2, "test_kern_pair");
    check(kern_between(m, 'V', 'A') == -3, "test_kern_pair_directional");
    check(kern_between(m, 'A', 'B') == 0, "test_no_kern_pair");

    check(measure(m, "") == 0, "test_measure_empty");
    check(measure(m, "Wii") == 12 + 4 + 4, "test_measure_sums_advances");
    check(measure(m, "AV") == 8 + 8 - 2, "test_measure_applies_kerning");
    check(measure(m, "AVA") == 8 + 8 + 8 - 2 - 3,
          "test_measure_kerns_every_pair");

    {
        std::vector<int> xs = caret_xs(m, "AVA");
        bool ok = xs.size() == 4 && xs[0] == 0 && xs[1] == 8 &&
                  xs[2] == 8 + 8 - 2 && xs[3] == 8 + 8 + 8 - 2 - 3;
        check(ok, "test_caret_positions");
    }
    {
        std::vector<int> xs = caret_xs(m, "");
        check(xs.size() == 1 && xs[0] == 0, "test_caret_positions_empty");
    }
    {   // The invariant hit-testing depends on: prefix consistency.
        std::vector<int> xs = caret_xs(m, "WiAV");
        bool ok = xs.size() == 5;
        if (ok)
            for (size_t i = 0; i <= 4; ++i)
                ok = ok && xs[i] == measure(m, std::string_view("WiAV").substr(0, i));
        check(ok, "test_caret_matches_measure_of_prefix");
    }

    return failed;
}
```
# Lesson: UTF-8 From Scratch {#utf-8-from-scratch}

Your editor's buffer will hold bytes. The user's text is *characters* —
more precisely Unicode **codepoints**, integers from U+0000 to U+10FFFF.
The bridge is UTF-8, and an editor cannot outsource this bridge: caret
movement, backspace, hit testing, and clipboard exchange all need to know
where one codepoint ends and the next begins. Get it wrong and pressing
Backspace after typing "é" deletes *half* of the é, leaving a mangled byte
that renders as garbage forever after. So we build the decoder ourselves,
to specification, and the tests hold you to the specification's sharp
edges.

### The encoding, in one table

UTF-8 (Thompson & Pike, 1992 — famously sketched on a diner placemat)
encodes each codepoint in 1–4 bytes. The first byte's high bits announce
the sequence length; continuation bytes always start `10`:

```
codepoint range      bytes  bit layout
U+0000  .. U+007F    1      0xxxxxxx
U+0080  .. U+07FF    2      110xxxxx 10xxxxxx
U+0800  .. U+FFFF    3      1110xxxx 10xxxxxx 10xxxxxx
U+10000 .. U+10FFFF  4      11110xxx 10xxxxxx 10xxxxxx 10xxxxxx
```

Decoding is: read the lead byte, learn the length, shift in the low six
bits of each continuation byte. `é` (U+00E9) is `0xC3 0xA9`:
`110_00011` + `10_101001` → `00011_101001` = 0xE9.

Three properties made UTF-8 conquer the world, and each one matters to
your editor directly:

- **ASCII is unchanged.** Every ASCII file is already valid UTF-8, and
  bytes < 0x80 never appear inside a multi-byte sequence. Your lesson-8
  line index can scan for `'\n'` bytes without decoding anything.
- **Self-synchronizing.** From *any* byte position you can find the
  nearest codepoint boundary by skipping backward over at most three
  `10xxxxxx` bytes. Caret movement never has to decode from the start of
  the file — this is the entire subject of the second challenge.
- **Byte-wise sort order equals codepoint order.** Not editor-critical,
  but it's why UTF-8 works in filenames and databases without special
  collation shims.

### The sharp edges

A decoder that only handles well-formed input is half a decoder. Editors
open arbitrary files: truncated downloads, Latin-1 mislabeled as UTF-8,
binary garbage. The Unicode standard is precise about what's *invalid*,
and two rules exist for security reasons, not tidiness:

- **Overlong encodings are forbidden.** The byte pair `0xC0 0xAF`
  mechanically decodes to 0x2F — `/` — but the two-byte form of a
  codepoint that fits in one byte is illegal. History's reason: pre-2001
  decoders that accepted overlongs let attackers smuggle `../` past
  path-validation code that only checked for the one-byte spelling. Your
  decoder must reject any sequence longer than its codepoint needs
  (that's `0xC0`/`0xC1` outright, `0xE0 0x80-0x9F`, `0xF0 0x80-0x8F`).
- **Surrogates (U+D800–U+DFFF) are forbidden in UTF-8.** They are UTF-16
  plumbing, not characters; three-byte sequences encoding them (`0xED
  0xA0-0xBF ...`) are invalid. Also invalid: anything above U+10FFFF
  (leads `0xF5`–`0xFF`), stray continuation bytes, and sequences cut off
  early.

What should an editor *do* with invalid bytes? The universal answer
(HTML5, Rust's `String::from_utf8_lossy`, every serious editor): replace
each offending byte with **U+FFFD �, the replacement character**, and
carry on. We adopt the simplest standard-sanctioned policy: an invalid or
truncated sequence consumes exactly **one byte** and yields U+FFFD; then
decoding resumes at the next byte. One byte, not "the whole broken
sequence" — restarting at the very next byte is what lets a single
corrupted byte in the middle of a CJK file cost one � instead of
desynchronizing the rest of the line.

A design note on types: the decoder returns `char32_t` (a codepoint is an
integer — C++'s dedicated type documents the intent), and takes
`std::string_view` — a non-owning pointer+length pair. All the text
plumbing from here on accepts `string_view`, so it works on a `std::string`,
a piece-table span, or a memory-mapped file without copying.

### Boundaries without decoding

The caret challenge is subtler than it looks. "Move left one character"
must work even when the text *isn't* valid UTF-8 — the user can place the
caret in a file full of garbage, and Backspace still has to delete
something sensible. The self-synchronization property gives the
algorithm: from byte index `i`, step back over continuation bytes (at
most 3 — anything more is malformed anyway), landing on a lead byte. Then
sanity-check: does that lead byte actually *claim* enough length to reach
past where you started? If yes, it's the boundary; if no, the bytes were
malformed, and the single byte before `i` is treated as its own
one-column unit — the � policy again, applied to navigation. The same
logic runs forward for "move right".

This byte-level view is deliberately humble: real cursor movement also
knows about combining marks (e + U+0301 = é as *two* codepoints), emoji
joined by zero-width joiners, and other grapheme-cluster rules — that's
another layer (with its own Unicode annex, UAX #29) built *on top of*
codepoint boundaries, and libraries like ICU provide it. Codepoint
boundaries are the load-bearing floor, and they're ours to get exactly
right.

## Challenge: A Strict Decoder {#utf8-decode points=15}

Implement `decode_utf8`, `encode_utf8`, and `decode_all`.

`decode_utf8(s, i)` decodes the sequence starting at byte `i` (the caller
guarantees `i < s.size()`), returning the codepoint and the number of
bytes consumed. On *any* invalid input — stray continuation byte, bad
lead, missing/malformed continuation, overlong form, surrogate, value
above U+10FFFF, or truncation at end of input — return `{0xFFFD, 1}`:
replace one byte, resume after it.

A clean implementation order: find the expected length from the lead
byte; verify every continuation byte matches `10xxxxxx` (and is present);
assemble the value; *then* reject it if it's a surrogate, above U+10FFFF,
or shorter-encodable (assembled value below the minimum for that length —
0x80 for 2 bytes, 0x800 for 3, 0x10000 for 4).

`encode_utf8(cp, out)` writes 1–4 bytes into `out` and returns the count
(the caller guarantees `cp` is a valid scalar value — you'll use this for
keyboard input, which is always valid). `decode_all` maps a whole string
through `decode_utf8`.

Mind `char` signedness: byte values ≥ 0x80 are negative as `char`. Route
everything through `unsigned char` before comparing.

### Starter

```cpp
#include <cstdint>
#include <string_view>
#include <vector>

struct Decoded {
    char32_t cp;   // the decoded codepoint, or 0xFFFD on error
    int len;       // bytes consumed (1..4; always 1 on error)
};

// Decode the sequence starting at byte i. Requires i < s.size().
Decoded decode_utf8(std::string_view s, size_t i) {
    // TODO
    (void)s;
    (void)i;
    return {0xFFFD, 1};
}

// Encode cp (a valid Unicode scalar value) into out; return byte count.
int encode_utf8(char32_t cp, char out[4]) {
    // TODO
    (void)cp;
    (void)out;
    return 0;
}

// Decode the whole string, one Decoded step at a time.
std::vector<char32_t> decode_all(std::string_view s) {
    // TODO
    (void)s;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static bool decodes_to(std::string_view s, char32_t cp, int len) {
    Decoded d = decode_utf8(s, 0);
    return d.cp == cp && d.len == len;
}

static bool is_error(std::string_view s) {
    Decoded d = decode_utf8(s, 0);
    return d.cp == 0xFFFD && d.len == 1;
}

int main() {
    // --- valid sequences at every length boundary ---
    check(decodes_to("A", U'A', 1), "test_ascii");
    check(decodes_to("\x7f", 0x7F, 1), "test_ascii_top");
    check(decodes_to("\xc2\x80", 0x80, 2), "test_two_byte_min");
    check(decodes_to("\xc3\xa9", 0xE9, 2), "test_e_acute");
    check(decodes_to("\xdf\xbf", 0x7FF, 2), "test_two_byte_max");
    check(decodes_to("\xe0\xa0\x80", 0x800, 3), "test_three_byte_min");
    check(decodes_to("\xe2\x82\xac", 0x20AC, 3), "test_euro_sign");
    check(decodes_to("\xef\xbf\xbf", 0xFFFF, 3), "test_three_byte_max");
    check(decodes_to("\xf0\x90\x80\x80", 0x10000, 4), "test_four_byte_min");
    check(decodes_to("\xf0\x9f\xa6\x86", 0x1F986, 4), "test_duck_emoji");
    check(decodes_to("\xf4\x8f\xbf\xbf", 0x10FFFF, 4), "test_unicode_max");

    // --- invalid: each consumes exactly one byte ---
    check(is_error("\x80"), "test_stray_continuation");
    check(is_error("\xbf"), "test_stray_continuation_high");
    check(is_error("\xc0\xaf"), "test_overlong_slash_rejected");
    check(is_error("\xc1\xbf"), "test_c1_rejected");
    check(is_error("\xe0\x80\xaf"), "test_overlong_three_byte");
    check(is_error("\xe0\x9f\xbf"), "test_overlong_three_byte_max");
    check(is_error("\xf0\x8f\xbf\xbf"), "test_overlong_four_byte");
    check(is_error("\xed\xa0\x80"), "test_surrogate_rejected");
    check(is_error("\xed\xbf\xbf"), "test_surrogate_high_rejected");
    check(is_error("\xf4\x90\x80\x80"), "test_above_max_rejected");
    check(is_error("\xf5\x80\x80\x80"), "test_f5_lead_rejected");
    check(is_error("\xff"), "test_ff_rejected");
    check(is_error("\xc3"), "test_truncated_two_byte");
    check(is_error("\xe2\x82"), "test_truncated_three_byte");
    check(is_error("\xc3\x28"), "test_bad_continuation");
    check(is_error("\xe2\x28\xa1"), "test_bad_continuation_middle");

    // Valid sequence AFTER a bad byte still decodes (resync).
    {
        std::vector<char32_t> v = decode_all("\x80\xc3\xa9");
        check(v.size() == 2 && v[0] == 0xFFFD && v[1] == 0xE9,
              "test_resync_after_error");
    }
    {
        std::vector<char32_t> v = decode_all("h\xc3\xa9!");
        check(v.size() == 3 && v[0] == U'h' && v[1] == 0xE9 && v[2] == U'!',
              "test_decode_all_mixed");
    }
    {   // Truncated at end of string: one FFFD per remaining byte.
        std::vector<char32_t> v = decode_all("A\xe2\x82");
        check(v.size() == 3 && v[0] == U'A' && v[1] == 0xFFFD &&
              v[2] == 0xFFFD,
              "test_truncation_at_eof");
    }
    check(decode_all("").empty(), "test_decode_all_empty");

    // --- encoding round-trips ---
    {
        char b[4];
        bool ok = true;
        const char32_t cps[] = {0x24,   0x7F,    0x80,    0x7FF,  0x800,
                                0xE9,   0x20AC,  0xFFFF,  0x10000, 0x1F986,
                                0x10FFFF};
        const int lens[] = {1, 1, 2, 2, 3, 2, 3, 3, 4, 4, 4};
        for (size_t i = 0; i < sizeof(cps) / sizeof(cps[0]); ++i) {
            int n = encode_utf8(cps[i], b);
            ok = ok && n == lens[i];
            Decoded d = decode_utf8(std::string_view(b, size_t(n)), 0);
            ok = ok && d.cp == cps[i] && d.len == n;
        }
        check(ok, "test_encode_decode_roundtrip");
    }
    {
        char b[4];
        int n = encode_utf8(0xE9, b);
        check(n == 2 && (unsigned char)b[0] == 0xC3 &&
              (unsigned char)b[1] == 0xA9,
              "test_encode_exact_bytes");
    }

    return failed;
}
```

## Challenge: Boundaries for the Caret {#utf8-boundaries points=12}

Implement the two functions caret movement is built on. They must behave
sensibly on *malformed* input too — one byte of garbage is one caret
position, per the � policy.

- `is_continuation(b)`: true iff `b` has the bit pattern `10xxxxxx`.
- `next_boundary(s, i)`: the first codepoint boundary strictly after `i`
  (caller guarantees `i < s.size()`). If `s[i]` is a lead byte declaring
  length L, the boundary is at `i + L` — but stop early at any byte that
  is not a continuation, and never run past the end of the string. If
  `s[i]` is itself a continuation byte (malformed here), the boundary is
  `i + 1`.
- `prev_boundary(s, i)`: the last boundary strictly before `i` (caller
  guarantees `0 < i <= s.size()`). Step back over at most 3 continuation
  bytes to find a lead candidate at position `j`. If `s[j]` is still a
  continuation byte (malformed run), return `i - 1`. Otherwise check the
  lead's declared length `L` (1 for ASCII and other non-lead values):
  if `j + L >= i` the sequence covers `i`, so return `j`; if it falls
  short (e.g. `"a"` followed by a stray continuation byte), the bytes
  between are orphans — return `i - 1`.

Declared lengths: `0xxxxxxx` → 1, `110xxxxx` → 2, `1110xxxx` → 3,
`11110xxx` → 4; treat anything else (`11111xxx`) as 1.

### Starter

```cpp
#include <cstdint>
#include <string_view>

bool is_continuation(unsigned char b) {
    // TODO
    (void)b;
    return false;
}

// First boundary strictly after i. Requires i < s.size().
size_t next_boundary(std::string_view s, size_t i) {
    // TODO
    (void)s;
    return i + 1;
}

// Last boundary strictly before i. Requires 0 < i <= s.size().
size_t prev_boundary(std::string_view s, size_t i) {
    // TODO
    (void)s;
    return i - 1;
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>
#include <vector>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    check(is_continuation(0x80) && is_continuation(0xBF),
          "test_continuation_range");
    check(!is_continuation(0x7F) && !is_continuation(0xC0) &&
          !is_continuation(0x41),
          "test_non_continuation");

    // "aé€𝄞" = 'a' (1) + U+00E9 (2) + U+20AC (3) + U+1D11E (4) = 10 bytes.
    std::string t = "a\xc3\xa9\xe2\x82\xac\xf0\x9d\x84\x9e";

    check(next_boundary(t, 0) == 1, "test_next_ascii");
    check(next_boundary(t, 1) == 3, "test_next_two_byte");
    check(next_boundary(t, 3) == 6, "test_next_three_byte");
    check(next_boundary(t, 6) == 10, "test_next_four_byte");

    check(prev_boundary(t, 10) == 6, "test_prev_four_byte");
    check(prev_boundary(t, 6) == 3, "test_prev_three_byte");
    check(prev_boundary(t, 3) == 1, "test_prev_two_byte");
    check(prev_boundary(t, 1) == 0, "test_prev_ascii");

    // Walking forward then backward visits the same boundaries.
    {
        std::vector<size_t> fwd;
        for (size_t i = 0; i < t.size(); i = next_boundary(t, i))
            fwd.push_back(i);
        std::vector<size_t> back;
        for (size_t i = t.size(); i > 0; i = prev_boundary(t, i))
            back.push_back(prev_boundary(t, i));
        bool ok = fwd.size() == 4 && back.size() == 4;
        for (size_t k = 0; ok && k < 4; ++k)
            ok = fwd[k] == back[3 - k];
        check(ok, "test_forward_backward_agree");
    }

    // From the middle of a sequence, prev finds the lead.
    check(prev_boundary(t, 2) == 1, "test_prev_from_mid_sequence");
    check(prev_boundary(t, 8) == 6, "test_prev_from_mid_four_byte");
    check(next_boundary(t, 7) == 8, "test_next_from_continuation_is_next_byte");

    // Malformed: stray continuation after ASCII — each byte its own unit.
    {
        std::string m = "a\xa9z";
        check(prev_boundary(m, 2) == 1, "test_orphan_continuation_own_unit");
        check(prev_boundary(m, 3) == 2, "test_after_orphan");
        check(next_boundary(m, 1) == 2, "test_next_orphan");
    }

    // Malformed: run of 5 continuations — prev never jumps more than 4.
    {
        std::string m = "\x80\x80\x80\x80\x80";
        check(prev_boundary(m, 5) == 4, "test_continuation_run_caps_at_one");
        check(next_boundary(m, 0) == 1, "test_next_in_continuation_run");
    }

    // Truncated lead at end of buffer: next_boundary stops at the end.
    {
        std::string m = "\xe2\x82";   // 3-byte lead, only 2 bytes present
        check(next_boundary(m, 0) == 2, "test_truncated_stops_at_end");
        check(prev_boundary(m, 2) == 0, "test_prev_truncated_covers");
    }

    // Lead whose continuation is cut short by a non-continuation byte.
    {
        std::string m = "\xc3(z";
        check(next_boundary(m, 0) == 1, "test_next_stops_at_non_continuation");
        check(prev_boundary(m, 1) == 0, "test_prev_bare_lead");
    }

    return failed;
}
```
# Lesson: The Piece Table {#the-piece-table}

Rendering is half an editor. The other half is the **document model**: the
data structure holding the text being edited. It has to absorb millions of
tiny edits — one keystroke each — anywhere in a file that might be 500 MB
of logs, while supporting undo, and never blocking the paint loop.

The naive model is a single `std::string`. Every insertion in the middle
shifts everything after it — O(n) per keystroke — and worse, every edit
*destroys information*: after `str.insert(...)` the old text is gone, so
undo needs you to have copied something somewhere first. Editors have
produced a whole zoo of better structures. The **gap buffer** keeps a hole
at the cursor so local typing is O(1) (Emacs; our sibling terminal-editor
course builds one, along with its heavier cousin the rope). This course
builds the structure used by Bravo — the first WYSIWYG editor, at Xerox
PARC — then Microsoft Word, AbiWord, and (in tree-refined form) VS Code:
the **piece table**. Charles Crowley's paper "Data Structures for Text
Sequences" surveys the field and comes down firmly in its favor; the VS
Code team's 2018 write-up of reboarding onto a piece tree is the modern
sequel, and both are in this course's extended reading.

### Two buffers, never edited

The piece table's move is to declare that text, once written, is
immutable. It keeps exactly two byte buffers:

- the **original buffer** — the file as loaded. Read-only, forever.
- the **add buffer** — every byte the user has ever typed, appended in
  arrival order. Append-only: nothing in it moves or dies either.

Neither buffer is the document. The document is a third thing: a list of
**pieces**, each saying "take `len` bytes from buffer B starting at offset
`start`". Read the pieces in order, concatenating their spans, and the
document appears:

```cpp
struct Piece {
    bool add;       // which buffer: false = original, true = add
    size_t start;   // offset into that buffer
    size_t len;     // bytes
};
```

Load "the quick fox": one piece, `{original, 0, 13}`. Type "brown " at
offset 10: the six bytes go to the *end of the add buffer* — wherever the
cursor is — and the piece list becomes:

```
{original, 0, 10}   "the quick "
{add,      0,  6}   "brown "
{original, 10, 3}   "fox"
```

Every edit is *span surgery*: an insertion splits at most one piece and
adds one; a deletion trims or removes pieces without touching a single
byte of text. The costs follow: insertion copies `s.size()` bytes into
the add buffer plus O(pieces) list work — never O(document). A 500 MB
file with one typo fixed is two buffers and three pieces.

Each piece is an arrow into a span of one immutable buffer; the emerald
arrow is the only piece that reads from the add buffer.

```d2
direction: right

pieces: "piece list (the document)" {
  shape: sql_table
  p0: "original · start 0 · len 10 · \"the quick \""
  p1: "add · start 0 · len 6 · \"brown \""
  p2: "original · start 10 · len 3 · \"fox\""
}

orig: "original buffer (read-only)" {
  shape: sql_table
  t: "\"the quick fox\""
}

add: "add buffer (append-only)" {
  shape: sql_table
  t: "\"brown \""
}

pieces.p0 -> orig.t
pieces.p1 -> add.t {style.stroke: "#34d399"; style.stroke-width: 2}
pieces.p2 -> orig.t
```

The design rewards you three more times downstream:

- **Undo is nearly free.** Since buffers never change, any prior document
  state is fully described by a prior *piece list* — a handful of structs.
  Snapshot the list (or just the changed slice of it), restore it later;
  the bytes are still there. Compare that with string-model undo, which
  must squirrel away every overwritten span.
- **Loading is instant.** "Read-only original buffer" fits `mmap` exactly:
  the OS pages the file in as pieces get read, and the editor shows a
  gigabyte file before having read most of it.
- **Sharing is safe.** A `std::string_view` into either buffer stays valid
  across edits (only the piece list changes) — which is why our reading
  APIs can hand out cheap views instead of copies.

The cost, honestly stated: the piece list grows by O(1) pieces per
noncontiguous edit, and *reading* offset `pos` means walking pieces to
find which one covers it — O(pieces). Long editing sessions on huge files
are why VS Code turned the list into a balanced tree with subtree-length
counts (O(log pieces) lookup). The flat `std::vector` version you'll
build is the same algorithm with a linear index; upgrading the container
later changes nothing about the surgery.

### The one optimization that matters: coalescing

Typing "hello" naively appends five pieces. But notice what typing looks
like in the table: each new char lands at the end of the add buffer, and
each insertion point is exactly the end of the piece created by the
previous keystroke. So before splitting, check: *is the insertion point
the end of an add-buffer piece whose span ends exactly where the add
buffer used to end?* If so, the new bytes continue that span — extend
`len`, done. One piece per typing burst instead of one per keystroke.
This check is what keeps the piece list short in practice, and the tests
count pieces to make sure you implemented it.

For deletion there's a symmetric subtlety: deleting a range that starts
mid-piece and ends mid-another means the first piece keeps its head, the
last keeps its tail, and everything between vanishes. Deleting from the
*middle* of one piece splits it in two — the table grows on delete, which
surprises people once and never again.

## Challenge: Insert {#piece-insert points=15}

Build the table and implement `insert`, including typing coalescing.

- The constructor wraps a non-empty original string in one piece (an
  empty original means an empty piece list).
- `size()` — total document length, in bytes; `piece_count()` exposes the
  list length so the tests can verify surgery and coalescing.
- `text()` — materialize the document by walking the pieces.
- `insert(pos, s)` — append `s` to the add buffer, then splice it in at
  document offset `pos` (`0 <= pos <= size()` guaranteed; empty `s` is a
  no-op that must not create pieces). Split a piece when `pos` falls
  inside one. **Coalesce** when `pos` is exactly the end of an add piece
  whose span ends at the pre-append end of the add buffer: extend that
  piece instead of creating a new one.

### Starter

```cpp
#include <cstddef>
#include <string>
#include <string_view>
#include <vector>

class PieceTable {
public:
    explicit PieceTable(std::string original) : original_(std::move(original)) {
        if (!original_.empty())
            pieces_.push_back({false, 0, original_.size()});
    }

    size_t size() const {
        // TODO: total length across pieces
        return 0;
    }

    size_t piece_count() const { return pieces_.size(); }

    std::string text() const {
        // TODO: concatenate every piece's span
        return {};
    }

    void insert(size_t pos, std::string_view s) {
        // TODO: append to add_, then splice/split/coalesce
        (void)pos;
        (void)s;
    }

private:
    struct Piece {
        bool add;       // false = original_, true = add_
        size_t start;   // offset into the source buffer
        size_t len;
    };

    const std::string& buf(const Piece& p) const {
        return p.add ? add_ : original_;
    }

    std::string original_;
    std::string add_;
    std::vector<Piece> pieces_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // Construction.
        PieceTable pt("hello");
        check(pt.size() == 5 && pt.text() == "hello" && pt.piece_count() == 1,
              "test_ctor_wraps_original");
        PieceTable empty("");
        check(empty.size() == 0 && empty.text() == "" &&
              empty.piece_count() == 0,
              "test_ctor_empty");
    }

    {   // Mid-piece insert splits: left + new + right.
        PieceTable pt("the quick fox");
        pt.insert(10, "brown ");
        check(pt.text() == "the quick brown fox", "test_mid_insert_text");
        check(pt.size() == 19, "test_mid_insert_size");
        check(pt.piece_count() == 3, "test_mid_insert_splits");
    }

    {   // Boundary inserts: start and end.
        PieceTable pt("core");
        pt.insert(0, ">> ");
        check(pt.text() == ">> core", "test_insert_at_start");
        pt.insert(pt.size(), " <<");
        check(pt.text() == ">> core <<", "test_insert_at_end");
    }

    {   // Typing coalesces into one add piece.
        PieceTable pt("");
        pt.insert(0, "h");
        pt.insert(1, "e");
        pt.insert(2, "y");
        check(pt.text() == "hey", "test_typing_text");
        check(pt.piece_count() == 1, "test_typing_coalesces");
    }

    {   // Coalescing at the tail of a document with an original.
        PieceTable pt("ab");
        pt.insert(2, "c");
        pt.insert(3, "d");
        pt.insert(4, "e");
        check(pt.text() == "abcde", "test_append_typing_text");
        check(pt.piece_count() == 2, "test_append_typing_coalesces");
    }

    {   // Moving the cursor breaks the coalescing run.
        PieceTable pt("");
        pt.insert(0, "world");
        pt.insert(0, "hello ");   // front: cannot extend the add piece
        check(pt.text() == "hello world", "test_front_insert_text");
        check(pt.piece_count() == 2, "test_front_insert_new_piece");
    }

    {   // Insert into the middle of an ADD piece splits it too.
        PieceTable pt("");
        pt.insert(0, "aaccc");
        pt.insert(2, "b");
        check(pt.text() == "aabccc", "test_split_add_piece");
        check(pt.piece_count() == 3, "test_split_add_piece_count");
    }

    {   // Empty insert: total no-op.
        PieceTable pt("xy");
        pt.insert(1, "");
        check(pt.text() == "xy" && pt.piece_count() == 1,
              "test_empty_insert_noop");
    }

    {   // Torture vs a reference string.
        PieceTable pt("0123456789");
        std::string ref = "0123456789";
        const char* words[] = {"AA", "b", "CCC", "dd", "E"};
        size_t positions[] = {3, 0, 11, 7, 1};
        for (int i = 0; i < 5; ++i) {
            pt.insert(positions[i], words[i]);
            ref.insert(positions[i], words[i]);
        }
        check(pt.text() == ref && pt.size() == ref.size(),
              "test_matches_reference_string");
    }

    return failed;
}
```

## Challenge: Delete and Read {#piece-delete points=15}

The starter arrives with the constructor and a working `insert` — this
challenge is `erase` and `substr`, the other half of the span surgery.

- `erase(pos, len)`: remove `len` bytes starting at `pos` (the range is
  guaranteed in-bounds; `len == 0` is a no-op). Pieces fully inside the
  range disappear; a piece straddling the start keeps its head; one
  straddling the end keeps its tail; a deletion strictly inside one piece
  splits it into head + tail. The buffers are never modified.
- `substr(pos, len)`: return the `len` bytes at `pos` without
  materializing the whole document — walk the pieces, skip to `pos`,
  copy only what's asked for. (`text()` is provided; `substr(0, size())`
  must equal it, and the paint loop will call `substr` for just the
  visible lines.)

### Starter

```cpp
#include <cstddef>
#include <string>
#include <string_view>
#include <vector>

class PieceTable {
public:
    explicit PieceTable(std::string original) : original_(std::move(original)) {
        if (!original_.empty())
            pieces_.push_back({false, 0, original_.size()});
    }

    size_t size() const {
        size_t n = 0;
        for (const Piece& p : pieces_)
            n += p.len;
        return n;
    }

    size_t piece_count() const { return pieces_.size(); }

    std::string text() const {
        std::string out;
        out.reserve(size());
        for (const Piece& p : pieces_)
            out.append(buf(p), p.start, p.len);
        return out;
    }

    void insert(size_t pos, std::string_view s) {
        if (s.empty())
            return;
        size_t add_start = add_.size();
        add_.append(s);

        size_t off = 0;   // document offset where the current piece starts
        for (size_t i = 0; i <= pieces_.size(); ++i) {
            // Boundary before piece i (or the very end of the document)?
            if (off == pos) {
                if (i > 0) {
                    Piece& prev = pieces_[i - 1];
                    if (prev.add && prev.start + prev.len == add_start) {
                        prev.len += s.size();   // coalesce a typing run
                        return;
                    }
                }
                pieces_.insert(pieces_.begin() + i, {true, add_start, s.size()});
                return;
            }
            if (i == pieces_.size())
                break;
            Piece& p = pieces_[i];
            if (pos < off + p.len) {   // strictly inside piece i: split
                Piece right{p.add, p.start + (pos - off), p.len - (pos - off)};
                p.len = pos - off;
                pieces_.insert(pieces_.begin() + i + 1,
                               {true, add_start, s.size()});
                pieces_.insert(pieces_.begin() + i + 2, right);
                return;
            }
            off += p.len;
        }
    }

    void erase(size_t pos, size_t len) {
        // TODO: trim, drop, or split pieces overlapping [pos, pos + len)
        (void)pos;
        (void)len;
    }

    std::string substr(size_t pos, size_t len) const {
        // TODO: copy only the requested span
        (void)pos;
        (void)len;
        return {};
    }

private:
    struct Piece {
        bool add;
        size_t start;
        size_t len;
    };

    const std::string& buf(const Piece& p) const {
        return p.add ? add_ : original_;
    }

    std::string original_;
    std::string add_;
    std::vector<Piece> pieces_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // Delete strictly inside one piece: split, buffers untouched.
        PieceTable pt("hello cruel world");
        pt.erase(5, 6);   // remove " cruel"
        check(pt.text() == "hello world", "test_erase_mid_piece_text");
        check(pt.size() == 11, "test_erase_mid_piece_size");
        check(pt.piece_count() == 2, "test_erase_splits_piece");
    }

    {   // Delete a prefix and a suffix.
        PieceTable pt("abcdef");
        pt.erase(0, 2);
        check(pt.text() == "cdef", "test_erase_prefix");
        pt.erase(2, 2);
        check(pt.text() == "cd", "test_erase_suffix");
    }

    {   // Delete spanning several pieces.
        PieceTable pt("aaa");
        pt.insert(3, "bbb");
        pt.insert(6, "ccc");   // may coalesce with bbb — that's fine
        pt.insert(0, "zz");    // "zzaaabbbccc"
        pt.erase(1, 8);        // "zzaaabbbccc" minus [1,9) = "z" + "cc"
        check(pt.text() == "zcc", "test_erase_across_pieces");
    }

    {   // Delete everything.
        PieceTable pt("wipe");
        pt.insert(4, " me");
        pt.erase(0, 7);
        check(pt.text() == "" && pt.size() == 0, "test_erase_all");
        check(pt.piece_count() == 0, "test_erase_all_drops_pieces");
        pt.insert(0, "again");
        check(pt.text() == "again", "test_insert_after_wipe");
    }

    {   // len == 0: no-op.
        PieceTable pt("solid");
        pt.erase(2, 0);
        check(pt.text() == "solid" && pt.piece_count() == 1,
              "test_erase_zero_noop");
    }

    {   // Exact piece boundaries: pieces drop, no splits.
        PieceTable pt("head");
        pt.insert(4, "TAIL");        // pieces: [head][TAIL]
        pt.erase(4, 4);              // remove exactly the add piece
        check(pt.text() == "head" && pt.piece_count() == 1,
              "test_erase_exact_piece");
    }

    {   // substr basics.
        PieceTable pt("the quick fox");
        pt.insert(10, "brown ");     // "the quick brown fox"
        check(pt.substr(0, 3) == "the", "test_substr_head");
        check(pt.substr(10, 5) == "brown", "test_substr_inside_add");
        check(pt.substr(8, 9) == "k brown f", "test_substr_across_pieces");
        check(pt.substr(0, pt.size()) == pt.text(), "test_substr_all");
        check(pt.substr(5, 0) == "", "test_substr_empty");
        check(pt.substr(pt.size(), 0) == "", "test_substr_at_end");
    }

    {   // Deterministic torture against std::string.
        PieceTable pt("Lorem ipsum dolor sit amet");
        std::string ref = "Lorem ipsum dolor sit amet";
        // A fixed little LCG so the sequence is reproducible.
        unsigned long long seed = 42;
        auto rnd = [&seed](size_t mod) {
            seed = seed * 6364136223846793005ULL + 1442695040888963407ULL;
            return size_t((seed >> 33) % (mod ? mod : 1));
        };
        const char* chunks[] = {"X", "yz", "@@@", "-", "0123"};
        for (int step = 0; step < 200; ++step) {
            if (ref.empty() || rnd(2) == 0) {
                size_t pos = rnd(ref.size() + 1);
                const char* c = chunks[rnd(5)];
                pt.insert(pos, c);
                ref.insert(pos, c);
            } else {
                size_t pos = rnd(ref.size());
                size_t len = rnd(ref.size() - pos + 1);
                pt.erase(pos, len);
                ref.erase(pos, len);
            }
            if (pt.text() != ref) {
                check(false, "test_torture_vs_reference");
                return failed;
            }
        }
        check(pt.substr(0, pt.size()) == ref, "test_torture_vs_reference");
    }

    return failed;
}
```
# Lesson: The Line Index {#the-line-index}

The piece table answers "what byte is at offset 12,345?" — but nothing an
editor *displays* is phrased in offsets. The screen shows lines; the
scrollbar is a fraction of lines; clicking needs "which line is under y";
damage tracking (lesson 12) wants "which rows does this edit dirty?".
Between the flat byte world and the line world sits one small, crucial
structure: the **line index** — a sorted array of the byte offsets where
each line begins.

The definition that makes every edge case fall out cleanly:

- Offset 0 is always a line start (even in an empty document — an empty
  document has one line, which is empty).
- Every `'\n'` at offset `i` *ends* a line, and offset `i + 1` starts the
  next one. Consequently `"a\nb"` has starts `[0, 2]`, and `"a\n"` has
  starts `[0, 2]` too — the second line exists and is empty. This matches
  what every editor shows: a file ending in a newline has one final empty
  line your caret can sit on.

Two lookups then run the whole show, both trivial over a sorted array:

- `line_of(offset)`: the greatest `L` with `starts[L] <= offset` — one
  `std::upper_bound`, minus one. Every offset in the document (including
  `size()`, where the end-of-document caret sits) belongs to exactly one
  line. Note the convention this implies: the offset *of* a `'\n'` still
  belongs to the line it terminates.
- `line_span(L)`: the line's content is `[starts[L], starts[L+1] - 1)`
  for interior lines (the `- 1` excludes the newline), and
  `[starts[L], text.size())` for the last one.

A pleasant consequence of UTF-8's design (lesson 6): since bytes below
0x80 never occur inside a multi-byte sequence, scanning raw *bytes* for
`0x0A` finds exactly the real newlines — the line index never needs to
decode anything. (Full Unicode also defines exotic breaks — U+2028 LINE
SEPARATOR and friends — which real editors mostly ignore for line
structure, and so do we. Windows CRLF? Treat the `'\r'` as an ordinary
byte of line content and strip it at file load/save time; normalizing at
the boundary keeps `'\n'`-only logic everywhere inside, which is exactly
what VS Code and friends do.)

### Keeping it current: shift, don't rebuild

Rebuilding the index is O(document) — fine on load, absurd per keystroke
on a big file (type "hello" and you've re-scanned 500 MB five times). But
look at what an edit actually does to the array:

- **Insert of `n` bytes at `pos`:** every start strictly greater than
  `pos` slides right by `n`. (A start *equal to* `pos` stays: inserting
  at the head of a line prepends to that line; its start doesn't move.)
  If the inserted text itself contains newlines, each `'\n'` at inserted
  index `k` mints a brand-new start at `pos + k + 1`, spliced in sorted
  position.
- **Erase of `len` bytes at `pos`:** starts inside `(pos, pos + len]`
  correspond to newlines that just vanished — remove them. Starts beyond
  `pos + len` slide left by `len`.

Both are O(lines after the edit point) array traffic and O(newlines in
the change) new entries — independent of document size. The tests keep
you honest with a torture loop comparing your incremental index against a
from-scratch rebuild after every random edit; if the two ever disagree,
you'll get the exact reproducing sequence for free (it's deterministic).

This "notify the index after each document edit" pattern — the piece
table doesn't know the index exists; the *editor* calls `on_insert`/
`on_erase` on both — is the shape all derived state takes in this
course: the layout cache (lesson 9) and damage list (lesson 12) hang off
the same notifications.

## Challenge: Line Starts {#line-starts points=12}

The read-only half: build the index from text, and the two lookups.

- `line_starts(text)`: offsets of every line start, per the definition
  above. Always non-empty; `[0]` for an empty text.
- `line_of(starts, offset)`: index of the line containing `offset`.
  Callers guarantee `offset <= text.size()`; remember `offset ==
  text.size()` belongs to the last line.
- `line_span(starts, text_size, line)`: half-open byte range of the
  line's content, newline excluded.

### Starter

```cpp
#include <algorithm>
#include <cstddef>
#include <string_view>
#include <vector>

struct LineSpan {
    size_t begin;
    size_t end;   // one past the last content byte; the '\n' is excluded
};

std::vector<size_t> line_starts(std::string_view text) {
    // TODO: 0, plus i + 1 for every '\n' at i
    (void)text;
    return {0};
}

size_t line_of(const std::vector<size_t>& starts, size_t offset) {
    // TODO: greatest L with starts[L] <= offset (std::upper_bound helps)
    (void)starts;
    (void)offset;
    return 0;
}

LineSpan line_span(const std::vector<size_t>& starts, size_t text_size,
                   size_t line) {
    // TODO: interior lines end at starts[line + 1] - 1; the last at text_size
    (void)starts;
    (void)text_size;
    (void)line;
    return {0, 0};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // Empty document: one empty line.
        auto s = line_starts("");
        check(s.size() == 1 && s[0] == 0, "test_empty_has_one_line");
        check(line_of(s, 0) == 0, "test_empty_line_of");
        LineSpan sp = line_span(s, 0, 0);
        check(sp.begin == 0 && sp.end == 0, "test_empty_span");
    }

    {   // No trailing newline.
        std::string t = "ab\ncd\nef";
        auto s = line_starts(t);
        check(s.size() == 3 && s[0] == 0 && s[1] == 3 && s[2] == 6,
              "test_three_lines");
        check(line_of(s, 0) == 0 && line_of(s, 2) == 0,
              "test_newline_belongs_to_its_line");
        check(line_of(s, 3) == 1 && line_of(s, 5) == 1,
              "test_second_line");
        check(line_of(s, 6) == 2 && line_of(s, 8) == 2,
              "test_offset_at_size_is_last_line");
        LineSpan sp0 = line_span(s, t.size(), 0);
        LineSpan sp1 = line_span(s, t.size(), 1);
        LineSpan sp2 = line_span(s, t.size(), 2);
        check(sp0.begin == 0 && sp0.end == 2, "test_span_excludes_newline");
        check(sp1.begin == 3 && sp1.end == 5, "test_span_interior");
        check(sp2.begin == 6 && sp2.end == 8, "test_span_last_line");
    }

    {   // Trailing newline: final empty line.
        std::string t = "x\n";
        auto s = line_starts(t);
        check(s.size() == 2 && s[1] == 2, "test_trailing_newline_empty_line");
        LineSpan sp = line_span(s, t.size(), 1);
        check(sp.begin == 2 && sp.end == 2, "test_trailing_empty_span");
        check(line_of(s, 2) == 1, "test_caret_on_final_empty_line");
    }

    {   // Consecutive newlines: empty interior lines.
        std::string t = "a\n\n\nb";
        auto s = line_starts(t);
        check(s.size() == 4, "test_blank_lines_counted");
        LineSpan sp = line_span(s, t.size(), 1);
        check(sp.begin == 2 && sp.end == 2, "test_blank_line_span");
        check(line_of(s, 2) == 1 && line_of(s, 3) == 2,
              "test_blank_line_of");
    }

    {   // Newline-only document.
        auto s = line_starts("\n");
        check(s.size() == 2 && s[0] == 0 && s[1] == 1, "test_lone_newline");
        check(line_of(s, 0) == 0 && line_of(s, 1) == 1,
              "test_lone_newline_line_of");
    }

    {   // A larger sanity walk: line_of agrees with spans everywhere.
        std::string t = "one\ntwo two\n\nfour\n";
        auto s = line_starts(t);
        bool ok = true;
        for (size_t off = 0; off <= t.size(); ++off) {
            size_t L = line_of(s, off);
            LineSpan sp = line_span(s, t.size(), L);
            // offset lies in [begin, end] (end == offset allowed: caret
            // just past the content, or on the '\n' itself for interior).
            ok = ok && off >= sp.begin && off <= sp.end + 1;
            ok = ok && L < s.size() && s[L] <= off;
        }
        check(ok, "test_line_of_consistent_with_spans");
    }

    return failed;
}
```

## Challenge: An Index That Keeps Up {#line-index-edit points=15}

The incremental half: a `LineIndex` class that is told about every edit
and shifts instead of rebuilding.

- `LineIndex(text)` builds the initial array (reuse your `line_starts`
  logic).
- `on_insert(pos, s)`: shift starts `> pos` right by `s.size()`; splice
  in a new start at `pos + k + 1` for each newline `s[k]`, keeping the
  array sorted.
- `on_erase(pos, len)`: drop starts in `(pos, pos + len]`; shift starts
  `> pos + len` left by `len`. (`len == 0` must be a no-op.)
- Lookups as before: `line_count`, `start_of(line)`, `line_of(offset)`.

The final test performs 300 scripted random edits, comparing your index
to a rebuilt-from-scratch one after each. Any divergence fails
immediately — and because the "random" sequence is a fixed LCG, a failure
reproduces identically every run, which is exactly how you'd debug it.

### Starter

```cpp
#include <algorithm>
#include <cstddef>
#include <string_view>
#include <vector>

class LineIndex {
public:
    explicit LineIndex(std::string_view text) {
        starts_.push_back(0);
        for (size_t i = 0; i < text.size(); ++i)
            if (text[i] == '\n')
                starts_.push_back(i + 1);
    }

    size_t line_count() const { return starts_.size(); }
    size_t start_of(size_t line) const { return starts_[line]; }

    size_t line_of(size_t offset) const {
        // TODO: greatest L with starts_[L] <= offset
        (void)offset;
        return 0;
    }

    void on_insert(size_t pos, std::string_view s) {
        // TODO: shift starts > pos; add a start after each '\n' in s
        (void)pos;
        (void)s;
    }

    void on_erase(size_t pos, size_t len) {
        // TODO: drop starts in (pos, pos + len]; shift the rest left
        (void)pos;
        (void)len;
    }

private:
    std::vector<size_t> starts_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>
#include <vector>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static bool same_as_rebuild(const LineIndex& idx, const std::string& text) {
    LineIndex fresh{text};
    if (idx.line_count() != fresh.line_count())
        return false;
    for (size_t i = 0; i < idx.line_count(); ++i)
        if (idx.start_of(i) != fresh.start_of(i))
            return false;
    return true;
}

int main() {
    {   // Plain text insert: shifts only.
        std::string t = "aa\nbb\ncc";
        LineIndex idx{t};
        t.insert(1, "XY");
        idx.on_insert(1, "XY");
        check(same_as_rebuild(idx, t), "test_insert_shifts");
        check(idx.line_of(0) == 0 && idx.line_of(5) == 1 && idx.line_of(9) == 2,
              "test_line_of_after_insert");
    }

    {   // Insert containing newlines: new entries appear, sorted.
        std::string t = "hello world";
        LineIndex idx{t};
        t.insert(5, "\nmid\n");
        idx.on_insert(5, "\nmid\n");
        check(idx.line_count() == 3, "test_insert_newlines_count");
        check(same_as_rebuild(idx, t), "test_insert_newlines_positions");
    }

    {   // Insert at a line start: the start doesn't move.
        std::string t = "a\nb";
        LineIndex idx{t};
        t.insert(2, "zz");
        idx.on_insert(2, "zz");
        check(idx.start_of(1) == 2 && same_as_rebuild(idx, t),
              "test_insert_at_line_start");
    }

    {   // Insert newline at position 0.
        std::string t = "abc";
        LineIndex idx{t};
        t.insert(0, "\n");
        idx.on_insert(0, "\n");
        check(idx.line_count() == 2 && idx.start_of(1) == 1 &&
              same_as_rebuild(idx, t),
              "test_insert_newline_at_zero");
    }

    {   // Erase without newlines: shifts only.
        std::string t = "aaa\nbbb\nccc";
        LineIndex idx{t};
        t.erase(5, 2);
        idx.on_erase(5, 2);
        check(same_as_rebuild(idx, t), "test_erase_shifts");
    }

    {   // Erase spanning a newline: the line disappears.
        std::string t = "aaa\nbbb";
        LineIndex idx{t};
        t.erase(2, 3);   // removes "a\nb"
        idx.on_erase(2, 3);
        check(idx.line_count() == 1 && same_as_rebuild(idx, t),
              "test_erase_swallows_line");
    }

    {   // Erase exactly one newline byte.
        std::string t = "x\ny";
        LineIndex idx{t};
        t.erase(1, 1);
        idx.on_erase(1, 1);
        check(idx.line_count() == 1 && same_as_rebuild(idx, t),
              "test_erase_newline_joins_lines");
    }

    {   // Zero-length erase: no-op.
        std::string t = "a\nb";
        LineIndex idx{t};
        idx.on_erase(1, 0);
        check(same_as_rebuild(idx, t), "test_erase_zero_noop");
    }

    {   // Torture: 300 scripted edits vs rebuild.
        std::string t = "seed\ntext\n";
        LineIndex idx{t};
        unsigned long long seed = 7;
        auto rnd = [&seed](size_t mod) {
            seed = seed * 6364136223846793005ULL + 1442695040888963407ULL;
            return size_t((seed >> 33) % (mod ? mod : 1));
        };
        const char* chunks[] = {"q", "\n", "ab\ncd", "\n\n", "xyz"};
        bool ok = true;
        for (int step = 0; ok && step < 300; ++step) {
            if (t.empty() || rnd(2) == 0) {
                size_t pos = rnd(t.size() + 1);
                const char* c = chunks[rnd(5)];
                t.insert(pos, c);
                idx.on_insert(pos, c);
            } else {
                size_t pos = rnd(t.size());
                size_t len = rnd(t.size() - pos + 1);
                t.erase(pos, len);
                idx.on_erase(pos, len);
            }
            ok = same_as_rebuild(idx, t);
        }
        check(ok, "test_torture_vs_rebuild");
    }

    return failed;
}
```
# Lesson: Layout: Greedy Word Wrap {#layout-word-wrap}

A document line and a screen line are different things, and the moment you
admit that, half the editor's remaining architecture falls into place. A
**logical line** is what the line index tracks: bytes between newlines,
possibly thousands of characters. A **visual row** is what fits across the
window. The mapping between them is the **layout engine**, and in a text
editor its core algorithm is *greedy word wrap*: pack characters into a
row until the next one won't fit, then break — preferably at a space.

Everything downstream speaks in the layout's output. Hit testing (lesson
10) turns a click's `y` into a row index; the caret is drawn at a row +
x-offset; scrolling (lesson 12) is measured in rows; damage is a band of
rows. So the output type is worth fixing carefully. A row is just a byte
range within its line:

```cpp
struct Row {
    size_t begin;   // byte offsets into the logical line's text
    size_t end;     // half-open, as always
};
```

No copied text — offsets into text owned by the document, the same
`string_view` discipline as everywhere else. Note what a `Row` doesn't
contain: no pixel positions. Given a row, x-positions come from the same
metrics used to wrap (`caret_xs` from lesson 5) — computed on demand,
never stored to go stale.

### The greedy algorithm, precisely

"Greedy" wrapping (as opposed to the global-optimizing Knuth–Plass
algorithm TeX uses for paragraphs — beautiful, but no editor wants line
breaks *changing above the cursor* as you type) is a single forward scan.
The subtleties are worth spelling out, because each is a bug I promise
you'd otherwise ship:

- **Track the last break opportunity.** As you scan, remember the
  position *just after* the most recent space in the current row. On
  overflow, break there if you have one — the space stays at the end of
  the upper row, where it invisibly "hangs" — otherwise you're inside one
  unbroken word longer than the window: **hard-break** right where you
  are, mid-word. (Try narrowing any editor around a long URL: mid-word
  breaks are correct behavior, not a cop-out.)
- **Overflow means strictly greater.** A row that exactly fills
  `max_width` is a fit, not an overflow. Get this backwards and every
  perfectly-full row wraps one character early.
- **Always make progress.** If the window is narrower than a single
  character, each character still gets a row of its own. The overflow
  check applies only when the row already has content (`i > start`) —
  otherwise you'd emit empty rows forever. This is the classic
  infinite-loop bug in wrap code; the tests include the killer case.
- **After a break, re-measure the carry-over.** The characters between
  the break point and your scan position move down to the new row; the
  new row's running width is *their* total, and the break-opportunity
  tracker resets to the last space among them. Then re-test the same
  character that overflowed — it may overflow the new row too.
- **The empty line still exists.** An empty logical line produces one
  empty row `{0, 0}` — it occupies vertical space and the caret can sit
  on it. Zero rows would make the line invisible and unclickable.

This challenge wraps printable-ASCII text (one byte = one column unit,
widths from the metrics table, anything else measuring as `'?'`); the
final challenge generalizes the same algorithm to UTF-8 codepoints using
your lesson-6 decoder. Kerning is deliberately ignored during wrapping —
a break destroys the pair anyway, and real shaping engines measure runs,
not pairs, at this stage.

### Cache it, invalidate it honestly

Wrapping is O(line length), and a keystroke changes *one* logical line —
yet a naive editor re-wraps the whole document per keystroke (and then
wonders why a 100k-line file types slowly). The cure is a **layout
cache**: line number → its rows, filled lazily as lines become visible,
so cold lines are never wrapped at all.

The hard part of any cache is invalidation, and text edits are a
particularly instructive case because line *numbers shift*. Say lines
[first, first + old_count) were edited and became new_count lines:

- Cached entries in the edited range are stale: **drop** them (they'll be
  re-wrapped lazily if ever visible again).
- Entries *below* the edit still hold perfectly good rows — but they're
  filed under old line numbers: **re-key** them by `new_count -
  old_count`. Dropping them instead would be *correct* but would re-wrap
  the whole visible tail after every Enter keypress — the cache would
  stop earning its rent exactly when files get big.
- Entries above the edit are untouched.

Two width-related notes that follow from the same honesty: when the
window resizes, *every* row boundary is suspect — the whole cache drops
(and this is fine: it refills lazily, visible lines first). And the cache
key deliberately does not include the width; the owner clears on width
change instead. One cache, one invalidation policy, no stale reads.

## Challenge: Wrap a Line {#word-wrap points=18}

Implement `wrap_line` per the algorithm above.

- `advances` points at 95 widths for `' '..'~'`; characters outside that
  range measure as `'?'` does. The input is printable ASCII (plus
  possibly stray bytes, which just measure as `'?'`).
- Break opportunities are *after each space* (`' '` only — no tabs here).
- On overflow (running width + next char's width **>** `max_width`, and
  the row is non-empty): break at the last opportunity after the row
  start if there is one, else before the current character. A space is
  not free: it is measured like any other character when scanned, so
  there is no "hanging space" exemption from the width budget — a run of
  spaces can itself overflow and wrap (see `test_spaces_wrap_too`).
- An empty line yields exactly one row, `{0, 0}`. All rows are
  contiguous: each begins where the previous ended, the first at 0, the
  last ending at `line.size()`.

### Starter

```cpp
#include <cstddef>
#include <string_view>
#include <vector>

struct Row {
    size_t begin;
    size_t end;
};

// Advance for c out of a ' '..'~' table; others measure as '?'.
inline int advance_for(const int* advances, char c) {
    unsigned char uc = static_cast<unsigned char>(c);
    if (uc < 32 || uc > 126)
        uc = '?';
    return advances[uc - 32];
}

// Greedy word wrap. See the lesson for the exact rules.
std::vector<Row> wrap_line(std::string_view line, const int* advances,
                           int max_width) {
    // TODO
    (void)line;
    (void)advances;
    (void)max_width;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

// All chars 10 wide except W (20) and i (4).
static void fill_advances(int* a) {
    for (int k = 0; k < 95; ++k)
        a[k] = 10;
    a['W' - 32] = 20;
    a['i' - 32] = 4;
}

static bool rows_are(const std::vector<Row>& rows,
                     std::initializer_list<Row> want) {
    if (rows.size() != want.size())
        return false;
    size_t i = 0;
    for (const Row& w : want) {
        if (rows[i].begin != w.begin || rows[i].end != w.end)
            return false;
        ++i;
    }
    return true;
}

// Rows must tile the line exactly.
static bool contiguous(const std::vector<Row>& rows, size_t n) {
    if (rows.empty())
        return false;
    size_t at = 0;
    for (const Row& r : rows) {
        if (r.begin != at || r.end < r.begin)
            return false;
        at = r.end;
    }
    return at == n;
}

int main() {
    int adv[95];
    fill_advances(adv);

    {   // Everything fits: one row.
        auto r = wrap_line("abc def", adv, 100);
        check(rows_are(r, {{0, 7}}), "test_fits_one_row");
    }

    {   // Empty line: one empty row.
        auto r = wrap_line("", adv, 100);
        check(rows_are(r, {{0, 0}}), "test_empty_line_one_row");
    }

    {   // Break at the space; space stays with the upper row.
        auto r = wrap_line("aaa bbb", adv, 40);
        check(rows_are(r, {{0, 4}, {4, 7}}), "test_break_at_space");
    }

    {   // Break at the LAST space in the row.
        auto r = wrap_line("a b c ddd", adv, 50);
        check(rows_are(r, {{0, 4}, {4, 9}}), "test_break_at_last_space");
    }

    {   // Long unbroken word: hard breaks mid-word.
        auto r = wrap_line("aaaaaa", adv, 25);
        check(rows_are(r, {{0, 2}, {2, 4}, {4, 6}}), "test_hard_break");
    }

    {   // Exact fit is not an overflow.
        auto r = wrap_line("ab", adv, 20);
        check(rows_are(r, {{0, 2}}), "test_exact_fit");
        auto r2 = wrap_line("abcd", adv, 20);
        check(rows_are(r2, {{0, 2}, {2, 4}}), "test_just_over");
    }

    {   // Window narrower than one character: one char per row, no hang.
        auto r = wrap_line("abc", adv, 5);
        check(rows_are(r, {{0, 1}, {1, 2}, {2, 3}}), "test_progress_guarantee");
    }

    {   // Wide characters count their real width.
        auto r = wrap_line("WW", adv, 30);
        check(rows_are(r, {{0, 1}, {1, 2}}), "test_wide_chars");
        auto r2 = wrap_line("Wii", adv, 28);
        check(rows_are(r2, {{0, 3}}), "test_mixed_widths_fit");
    }

    {   // Spaces overflow like anything else.
        auto r = wrap_line("aa      ", adv, 40);
        check(rows_are(r, {{0, 4}, {4, 8}}), "test_spaces_wrap_too");
    }

    {   // Carry-over remeasure: after a space break, the tail may still
        // overflow and must hard-break correctly.
        auto r = wrap_line("aa bbbbbb", adv, 40);
        // After "aa " (width 30), the next 'b' (idx 3) makes 40: exact
        // fit, not overflow, so it stays on row 0. The 'b' after THAT
        // (idx 4) would make 50: overflow, so break at the last
        // opportunity, the space at idx 3 -> row 0 = {0,3}.
        // Carry "b" (idx 3, width 10) down to row 1, no space in the
        // carry so the break-opportunity tracker resets; row 1 fills
        // to idx 7 (width 40) and the next 'b' would overflow with no
        // opportunity to break at -> hard-break at idx 7 -> row 1 =
        // {3,7}. The remaining "bb" fits in row 2 = {7,9}.
        check(rows_are(r, {{0, 3}, {3, 7}, {7, 9}}), "test_carry_over");
    }

    {   // Out-of-range bytes measure as '?' (10 here) — no crash, no skip.
        std::string s = "ab\x01\x02";
        auto r = wrap_line(s, adv, 20);
        check(rows_are(r, {{0, 2}, {2, 4}}), "test_stray_bytes_measured");
    }

    {   // Structural invariant on a longer sample.
        std::string s = "the quick brown fox jumps over the lazy dog";
        for (int w : {15, 40, 55, 80, 200}) {
            auto r = wrap_line(s, adv, w);
            if (!contiguous(r, s.size())) {
                check(false, "test_rows_tile_line");
                return failed;
            }
        }
        check(true, "test_rows_tile_line");
    }

    return failed;
}
```

## Challenge: The Layout Cache {#layout-cache points=12}

Implement `LayoutCache`: rows by line number, with shift-aware
invalidation.

- `put(line, rows)` stores (replacing any entry); `get(line)` returns a
  pointer to the stored rows or `nullptr` — pointer-or-null is the idiom
  for "maybe a big object" where `std::optional` would copy.
- `on_lines_edited(first, old_count, new_count)`: lines `[first, first +
  old_count)` were replaced by `new_count` lines. Drop cached entries in
  the edited range; re-key entries at `>= first + old_count` by
  `new_count - old_count`; leave entries before `first` alone.
- `clear()` for width changes; `cached_count()` so the tests can see
  evictions happen.

### Starter

```cpp
#include <cstddef>
#include <map>
#include <vector>

struct Row {
    size_t begin;
    size_t end;
};

class LayoutCache {
public:
    void put(size_t line, std::vector<Row> rows) {
        // TODO
        (void)line;
        (void)rows;
    }

    const std::vector<Row>* get(size_t line) const {
        // TODO: nullptr when absent
        (void)line;
        return nullptr;
    }

    void on_lines_edited(size_t first, size_t old_count, size_t new_count) {
        // TODO: drop [first, first + old_count); shift the tail
        (void)first;
        (void)old_count;
        (void)new_count;
    }

    void clear() {
        // TODO
    }

    size_t cached_count() const {
        // TODO
        return 0;
    }

private:
    std::map<size_t, std::vector<Row>> rows_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

// A recognizable per-line dummy layout.
static std::vector<Row> rows_for(size_t line) {
    return {{0, line + 1}};
}

static bool holds(const LayoutCache& c, size_t line, size_t orig_line) {
    const std::vector<Row>* r = c.get(line);
    return r && r->size() == 1 && (*r)[0].end == orig_line + 1;
}

int main() {
    {   // put/get basics.
        LayoutCache c;
        check(c.get(0) == nullptr && c.cached_count() == 0,
              "test_empty_cache");
        c.put(3, rows_for(3));
        check(holds(c, 3, 3) && c.get(2) == nullptr,
              "test_put_get");
        c.put(3, {{0, 99}});
        const std::vector<Row>* r = c.get(3);
        check(r && (*r)[0].end == 99, "test_put_replaces");
    }

    {   // Same-size edit: dirty lines drop, others stay put.
        LayoutCache c;
        for (size_t l = 0; l < 6; ++l)
            c.put(l, rows_for(l));
        c.on_lines_edited(2, 2, 2);   // lines 2,3 rewritten
        check(c.get(2) == nullptr && c.get(3) == nullptr,
              "test_edited_lines_dropped");
        check(holds(c, 0, 0) && holds(c, 1, 1), "test_lines_above_kept");
        check(holds(c, 4, 4) && holds(c, 5, 5),
              "test_lines_below_kept_same_key");
        check(c.cached_count() == 4, "test_count_after_drop");
    }

    {   // Deletion: tail shifts up.
        LayoutCache c;
        for (size_t l = 0; l < 6; ++l)
            c.put(l, rows_for(l));
        c.on_lines_edited(1, 3, 1);   // lines 1..3 became one line
        check(holds(c, 0, 0), "test_head_untouched");
        check(c.get(1) == nullptr, "test_new_merged_line_uncached");
        check(holds(c, 2, 4) && holds(c, 3, 5), "test_tail_shifted_up");
        check(c.get(4) == nullptr && c.get(5) == nullptr,
              "test_old_keys_gone");
    }

    {   // Insertion (Enter pressed): tail shifts down.
        LayoutCache c;
        for (size_t l = 0; l < 4; ++l)
            c.put(l, rows_for(l));
        c.on_lines_edited(1, 1, 3);   // line 1 became three lines
        check(holds(c, 0, 0), "test_insert_head_untouched");
        check(c.get(1) == nullptr && c.get(2) == nullptr &&
              c.get(3) == nullptr,
              "test_insert_dirty_range");
        check(holds(c, 4, 2) && holds(c, 5, 3), "test_tail_shifted_down");
    }

    {   // Edit at line 0, and an edit past all cached lines.
        LayoutCache c;
        c.put(0, rows_for(0));
        c.put(9, rows_for(9));
        c.on_lines_edited(0, 1, 2);
        check(c.get(0) == nullptr && holds(c, 10, 9),
              "test_edit_at_zero");
        c.on_lines_edited(20, 1, 1);
        check(holds(c, 10, 9), "test_edit_past_cache_noop");
    }

    {   // clear() empties everything.
        LayoutCache c;
        for (size_t l = 0; l < 5; ++l)
            c.put(l, rows_for(l));
        c.clear();
        check(c.cached_count() == 0 && c.get(2) == nullptr, "test_clear");
    }

    return failed;
}
```
# Lesson: Hit Testing and the Caret {#hit-testing-and-the-caret}

The user clicks at pixel (312, 148). Which byte of the document did they
mean? That question — **hit testing** — and its inverse — where on screen
does document offset 5,231 live? — are the two coordinate transforms an
editor runs constantly: every click, every drag tick, every caret blink,
every "scroll until the cursor is visible".

It pays to see the coordinate spaces as a pipeline with one honest
converter at each stage:

```
pixel (x, y)
  <->  visual row index      (y / line_height, within this lesson's line)
  <->  byte offset in row    (x vs the row's caret positions)
  <->  document offset       (row.begin + local offset; lesson 8's index
                              adds the logical-line base in the full app)
```

Each converter must be the *inverse* of its partner, built on the same
metrics. The moment hit testing measures text one way and painting
another, clicks land one character off and the bug report writes itself.
That's why `caret_x` (offset → pixel) and `index_at_x` (pixel → offset)
in this lesson share one advance table — and why lesson 5 nagged you to
measure and draw with the same code.

### The half-advance rule

Where does a click *inside* a character land the caret? Not "before the
character you hit" — users click sloppily, aiming at the gap between
letters. Every editor since forever uses the **half-advance rule**: a
click in the left half of a glyph puts the caret before it, in the right
half, after it. Equivalently: snap to the *nearest caret position*. The
tests pin the boundary exactly: with a 10px-wide glyph spanning x∈[0,10),
clicks at x≤4 go before, x≥5 after. Clicks left of the row snap to its
first offset; clicks beyond its last glyph snap to its end; clicks above
the first row snap into it, likewise below the last — clamping *toward
sensible text positions* rather than rejecting the event is what makes an
editor feel solid at its edges.

### Caret affinity: one offset, two homes

Wrap "aaa bbb" at width 40 (10px glyphs) and you get rows `{0,4}` and
`{4,7}`. Now: where is the caret when its offset is 4? It's the *end of
row 0* and the *start of row 1* — the same byte offset, two different
pixels. This ambiguity is called **caret affinity**, every wrapped-text
editor must resolve it, and both answers are correct at different times:

- Click at the ragged end of row 0 (or press End there): the caret should
  sit where you clicked — end of the upper row. **Upstream** affinity.
- Click at the left edge of row 1 (or press Home, or arrow-right onto
  it): start of the lower row. **Downstream** affinity.

So a document position for caret purposes is not a bare `size_t`; it's an
offset *plus* an affinity bit, and hit testing is what sets the bit:
clicking in a row that resolves to that row's `end` — when another row
continues the line — means upstream. (Try End vs Home around a wrap point
in any editor; you're watching this bit flip. Only wrap boundaries need
it: at a real `'\n'`, the offsets on either side differ, so there's no
ambiguity to resolve.)

```cpp
struct DocPos {
    size_t offset;
    bool upstream;   // at a wrap boundary: caret shows at the END of the
                     // earlier row rather than the start of the later one
};
```

### Up, Down, and the goal column

Vertical caret movement hides a classic piece of state. Put the caret at
column 40, press Down through a short row (10 wide), then Down again into
a long row: the caret should return to column 40, not stick at 10. So
Up/Down do **not** move to "the same column"; they move to the **goal x**
— the pixel x remembered from the last horizontal positioning. Arrow
up/down *reads* the goal (setting it from the current position only if
unset); clicking or typing *clears* it. Inside a target row, the goal x
resolves to an offset with the same nearest-caret rule as a mouse click —
`index_at_x` again, one function, third customer.

At the document's edges, vertical motion clamps: Up on the first row and
Down on the last leave the caret in place (TextEdit-style; some editors
jump to line start/end — either is defensible, ours is the simpler
contract, but the *goal x must still be recorded* so a later Down works).

## Challenge: From Pixels to Positions {#hit-test points=18}

Implement the three converters over the wrapped rows of one logical line
(the final challenge stacks multiple lines; nothing about the logic
changes).

- `caret_x(text, row, advances, offset)`: pixel x of the caret position
  `offset` within `row` — the summed advances of `text[row.begin ..
  offset)`. Callers guarantee `row.begin <= offset <= row.end`.
- `index_at_x(text, row, advances, x)`: the offset in `[row.begin,
  row.end]` whose caret position is nearest `x`, half-advance rule, ties
  to the right: a click at exactly `left + adv/2` (integer division)
  goes after the glyph. `x < 0` returns `row.begin`; `x` past the last
  glyph returns `row.end`.
- `hit_test(text, rows, advances, line_height, x, y)`: rows are the
  wrapped rows of the line, top to bottom, `rows[i]` covering pixels
  `y ∈ [i*line_height, (i+1)*line_height)`. Clamp `y` into the row range
  (negative → row 0, too large → last row), resolve `x` within that row,
  and set `upstream` exactly when the resolved offset equals the row's
  `end` and a later row exists.

Characters outside `' '..'~'` measure as `'?'`, as in lesson 9.

### Starter

```cpp
#include <cstddef>
#include <string_view>
#include <vector>

struct Row {
    size_t begin;
    size_t end;
};

struct DocPos {
    size_t offset;
    bool upstream;
};

inline int advance_for(const int* advances, char c) {
    unsigned char uc = static_cast<unsigned char>(c);
    if (uc < 32 || uc > 126)
        uc = '?';
    return advances[uc - 32];
}

int caret_x(std::string_view text, Row row, const int* advances,
            size_t offset) {
    // TODO: sum advances over [row.begin, offset)
    (void)text;
    (void)row;
    (void)advances;
    (void)offset;
    return 0;
}

size_t index_at_x(std::string_view text, Row row, const int* advances,
                  int x) {
    // TODO: nearest caret position, half-advance, ties right
    (void)text;
    (void)row;
    (void)advances;
    (void)x;
    return row.begin;
}

DocPos hit_test(std::string_view text, const std::vector<Row>& rows,
                const int* advances, int line_height, int x, int y) {
    // TODO: clamp y to a row, resolve x, set upstream at wrap boundaries
    (void)text;
    (void)rows;
    (void)advances;
    (void)line_height;
    (void)x;
    (void)y;
    return {0, false};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    int adv[95];
    for (int k = 0; k < 95; ++k)
        adv[k] = 10;
    adv['i' - 32] = 4;

    // "aaa bbb" wrapped at 40px: rows {0,4} and {4,7}.
    std::string t = "aaa bbb";
    std::vector<Row> rows = {{0, 4}, {4, 7}};
    const int LH = 16;

    // --- caret_x ---
    check(caret_x(t, rows[0], adv, 0) == 0, "test_caret_x_row_start");
    check(caret_x(t, rows[0], adv, 3) == 30, "test_caret_x_interior");
    check(caret_x(t, rows[0], adv, 4) == 40, "test_caret_x_row_end");
    check(caret_x(t, rows[1], adv, 4) == 0, "test_caret_x_second_row_zero");
    check(caret_x(t, rows[1], adv, 6) == 20, "test_caret_x_second_row");

    // --- index_at_x: half-advance, ties right ---
    check(index_at_x(t, rows[0], adv, 0) == 0, "test_x_zero");
    check(index_at_x(t, rows[0], adv, 4) == 0, "test_left_half_goes_before");
    check(index_at_x(t, rows[0], adv, 5) == 1, "test_midpoint_goes_after");
    check(index_at_x(t, rows[0], adv, 14) == 1, "test_second_char_left_half");
    check(index_at_x(t, rows[0], adv, -7) == 0, "test_negative_x_clamps");
    check(index_at_x(t, rows[0], adv, 999) == 4, "test_far_right_is_row_end");
    check(index_at_x(t, rows[1], adv, 999) == 7, "test_far_right_second_row");

    {   // Narrow glyphs move the midpoints.
        std::string ii = "iii";
        Row r{0, 3};
        check(index_at_x(ii, r, adv, 1) == 0, "test_narrow_left_half");
        check(index_at_x(ii, r, adv, 2) == 1, "test_narrow_midpoint_right");
        check(index_at_x(ii, r, adv, 9) == 2, "test_narrow_third");
    }

    // --- hit_test: rows stack vertically ---
    {
        DocPos p = hit_test(t, rows, adv, LH, 0, 0);
        check(p.offset == 0 && !p.upstream, "test_hit_origin");
    }
    {
        DocPos p = hit_test(t, rows, adv, LH, 21, 5);
        check(p.offset == 2 && !p.upstream, "test_hit_row0_interior");
    }
    {   // End of a wrapped row: SAME offset as start of next, upstream set.
        DocPos p = hit_test(t, rows, adv, LH, 200, 5);
        check(p.offset == 4 && p.upstream, "test_row_end_is_upstream");
    }
    {
        DocPos p = hit_test(t, rows, adv, LH, 0, LH + 3);
        check(p.offset == 4 && !p.upstream, "test_row1_start_is_downstream");
    }
    {   // Last row's end: no later row, no upstream flag.
        DocPos p = hit_test(t, rows, adv, LH, 200, LH + 3);
        check(p.offset == 7 && !p.upstream, "test_last_row_end_downstream");
    }
    {   // y clamping.
        DocPos above = hit_test(t, rows, adv, LH, 14, -50);
        DocPos below = hit_test(t, rows, adv, LH, 14, 900);
        check(above.offset == 1 && !above.upstream, "test_y_above_clamps");
        check(below.offset == 5 && !below.upstream, "test_y_below_clamps");
    }
    {   // Single-row line: end never upstream.
        std::vector<Row> one = {{0, 7}};
        DocPos p = hit_test(t, one, adv, LH, 999, 8);
        check(p.offset == 7 && !p.upstream, "test_single_row_no_upstream");
    }
    {   // Empty line: one empty row, always offset 0.
        std::vector<Row> er = {{0, 0}};
        DocPos p = hit_test("", er, adv, LH, 50, 5);
        check(p.offset == 0 && !p.upstream, "test_empty_row_hit");
    }

    return failed;
}
```

## Challenge: Up and Down with a Goal {#vertical-motion points=15}

Implement `row_of` — which visual row displays a given caret? — and
`move_vertical`, the Up/Down handler. The starter provides working
`caret_x` and `index_at_x` (the lesson's converters) to build on.

- `row_of(rows, offset, upstream)`: the index of the row that displays
  the caret. An offset strictly inside a row is unambiguous. At a shared
  boundary (`offset == rows[i].end == rows[i+1].begin`), `upstream`
  picks row `i`, otherwise row `i+1`. The line-end offset belongs to the
  last row.
- `move_vertical(text, rows, advances, c, dir)` with `dir` −1 (up) or +1
  (down): resolve the goal x (`c.goal_x`, or the caret's current pixel x
  if `goal_x < 0`), step to the adjacent row, resolve the goal within it
  (nearest caret position, as always), and set `upstream` exactly as
  `hit_test` would. At the top/bottom edge the caret stays put — but the
  returned caret must carry the resolved `goal_x` either way.

### Starter

```cpp
#include <cstddef>
#include <string_view>
#include <vector>

struct Row {
    size_t begin;
    size_t end;
};

struct Caret {
    size_t offset = 0;
    bool upstream = false;
    int goal_x = -1;   // -1 = no goal recorded yet
};

inline int advance_for(const int* advances, char c) {
    unsigned char uc = static_cast<unsigned char>(c);
    if (uc < 32 || uc > 126)
        uc = '?';
    return advances[uc - 32];
}

inline int caret_x(std::string_view text, Row row, const int* advances,
                   size_t offset) {
    int x = 0;
    for (size_t i = row.begin; i < offset; ++i)
        x += advance_for(advances, text[i]);
    return x;
}

inline size_t index_at_x(std::string_view text, Row row, const int* advances,
                         int x) {
    if (x < 0)
        return row.begin;
    int left = 0;
    for (size_t i = row.begin; i < row.end; ++i) {
        int adv = advance_for(advances, text[i]);
        if (x < left + adv / 2)
            return i;
        left += adv;
    }
    return row.end;
}

size_t row_of(const std::vector<Row>& rows, size_t offset, bool upstream) {
    // TODO
    (void)rows;
    (void)offset;
    (void)upstream;
    return 0;
}

Caret move_vertical(std::string_view text, const std::vector<Row>& rows,
                    const int* advances, Caret c, int dir) {
    // TODO
    (void)text;
    (void)rows;
    (void)advances;
    (void)dir;
    return c;
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    int adv[95];
    for (int k = 0; k < 95; ++k)
        adv[k] = 10;

    // "aaaa bbbb cccc" wrapped at 50px: {0,5} {5,10} {10,14}.
    std::string t = "aaaa bbbb cccc";
    std::vector<Row> rows = {{0, 5}, {5, 10}, {10, 14}};

    // --- row_of ---
    check(row_of(rows, 2, false) == 0, "test_row_of_interior");
    check(row_of(rows, 7, false) == 1, "test_row_of_second");
    check(row_of(rows, 5, true) == 0, "test_row_of_boundary_upstream");
    check(row_of(rows, 5, false) == 1, "test_row_of_boundary_downstream");
    check(row_of(rows, 14, false) == 2, "test_row_of_line_end");
    check(row_of(rows, 14, true) == 2, "test_row_of_line_end_upstream");

    // --- straight down and back up ---
    {
        Caret c{2, false, -1};
        c = move_vertical(t, rows, adv, c, +1);
        check(c.offset == 7 && !c.upstream && c.goal_x == 20,
              "test_down_sets_goal");
        c = move_vertical(t, rows, adv, c, +1);
        check(c.offset == 12 && c.goal_x == 20, "test_down_again");
        c = move_vertical(t, rows, adv, c, -1);
        check(c.offset == 7, "test_up_returns");
    }

    // --- clamping at the edges keeps the goal ---
    {
        Caret c{12, false, -1};
        c = move_vertical(t, rows, adv, c, +1);
        check(c.offset == 12 && !c.upstream && c.goal_x == 20,
              "test_down_at_bottom_stays");
        Caret d{1, false, -1};
        d = move_vertical(t, rows, adv, d, -1);
        check(d.offset == 1 && d.goal_x == 10, "test_up_at_top_stays");
    }

    // --- goal survives a short row ---
    {
        // "xxxxxxxx" / "yy" / "zzzzzzzz" as three rows of one line.
        std::string u = "xxxxxxxxyyzzzzzzzz";
        std::vector<Row> ur = {{0, 8}, {8, 10}, {10, 18}};
        Caret c{6, false, -1};   // x = 60
        c = move_vertical(u, ur, adv, c, +1);
        check(c.offset == 10 && c.upstream && c.goal_x == 60,
              "test_short_row_clamps_to_end");
        c = move_vertical(u, ur, adv, c, +1);
        check(c.offset == 16 && !c.upstream && c.goal_x == 60,
              "test_goal_survives_short_row");
        c = move_vertical(u, ur, adv, c, -1);
        c = move_vertical(u, ur, adv, c, -1);
        check(c.offset == 6 && c.goal_x == 60, "test_round_trip_via_goal");
    }

    // --- upstream caret starts from the upper row ---
    {
        Caret c{5, true, -1};    // shown at end of row 0, x = 50
        c = move_vertical(t, rows, adv, c, -1);
        check(c.offset == 5 && c.goal_x == 50,
              "test_upstream_up_from_row0_stays");
        Caret d{5, false, -1};   // shown at start of row 1, x = 0
        d = move_vertical(t, rows, adv, d, -1);
        check(d.offset == 0 && d.goal_x == 0,
              "test_downstream_up_from_row1_moves");
    }

    // --- moving down INTO a wrap boundary sets upstream ---
    {
        std::string u = "aabb";
        std::vector<Row> ur = {{0, 2}, {2, 4}};
        Caret c{2, true, 999};   // end of row 0, far-right goal
        c = move_vertical(u, ur, adv, c, +1);
        check(c.offset == 4 && !c.upstream && c.goal_x == 999,
              "test_down_to_last_row_end");
    }

    return failed;
}
```
# Lesson: Selection and the Mouse {#selection-and-the-mouse}

Selection looks like a range. It isn't — not quite. Drag from offset 20
leftward to offset 5, and the selected *range* is [5, 20), but the editor
must remember more: if you now press Shift+Right, the selection *shrinks
at the left edge* (to [6, 20)) rather than growing at the right. The
selection has a fixed end and a moving end.

So the model every serious editor uses is an **anchor** and a **focus**
(browsers use exactly these names in the DOM Selection API; you'll also
see "mark and point" in Emacs lineage):

- the **anchor** is where selection started — it never moves during a
  gesture;
- the **focus** is the caret — every extension gesture (drag,
  Shift+click, Shift+arrow) moves *only* the focus.

The pair is allowed to be "backwards" (`focus < anchor` after a leftward
drag) and *must not be normalized in place* — normalizing destroys the
information of which end moves next. Instead, expose normalization as a
read-only view: `begin()`/`end()` return the sorted pair for consumers
(rendering the highlight, deleting the range), while `anchor`/`focus`
keep gesture state. A collapsed selection (`anchor == focus`) *is* the
caret; there is no separate caret object. One state, two readings —
this little discipline eliminates a whole class of "caret and selection
disagree" bugs.

The paint rule follows mechanically: a byte at offset `o` is highlighted
iff `begin() <= o < end()` — half-open, like every range in this course,
so adjacent selections tile without overlap and an empty selection
highlights nothing.

### Edits move selections

Here's the wrinkle that separates toy editors from real ones: positions
are only meaningful *against a particular revision of the text*. When
text changes — by your own typing, or in lesson 14 by an undo — every
stored offset (both selection ends, bookmark positions, the scroll
anchor) must be **adjusted** through the edit:

- Insert of `n` bytes at `pos`: offsets after the insertion point shift
  right by `n`. Convention for the boundary: an offset exactly *at*
  `pos` also shifts — type at the caret and the caret rides ahead of the
  inserted text. (This is the natural typing behavior; sophisticated
  systems make the boundary policy explicit per marker — the "left/right
  gravity" you'll meet in any editor-internals discussion.)
- Erase of `[pos, pos+n)`: offsets before `pos` stay; offsets past the
  end shift left by `n`; offsets *inside* the erased range have lost
  their home — they clamp to `pos`.

Apply the adjustment to anchor and focus independently and the selection
survives edits made elsewhere in the document — exactly what you expect
when an undo happens while you have text selected.

### Clicks: one, two, three

Mouse gestures map onto the anchor/focus model with counted clicks —
these conventions are so old (they shipped with the original Macintosh)
that users' hands know them:

- **Single click**: collapse — anchor = focus = hit position. Drag moves
  focus, character by character.
- **Double click**: select the **word** under the cursor. What's a word?
  The pragmatic editor answer (matching vi's `w`, roughly): a maximal run
  of *word characters* (letters, digits, underscore), or a maximal run of
  *other punctuation*, or a run of whitespace — double-clicking a gap
  selects the gap. Classify each byte into one of those three classes;
  the word at a position is the maximal same-class run around it. At a
  boundary between runs, the convention is to look at the character to
  the *right* of the position (and at end-of-text, the one to the left).
- **Triple click**: the whole line — no challenge needed once you have
  lesson 8's `line_span`.

Double-click-and-*drag* selects by whole words: the selection is always
the union of the word under the press and the word under the pointer,
with anchor and focus arranged so the moving end tracks the mouse — drag
right and focus is the right edge; drag left and focus is the left edge.
(Word-drag granularity is also why the anchor is remembered as the whole
*anchor word*, not a point — release inside the original word and it
stays fully selected.)

ASCII classes suffice here; the classifier takes an `unsigned char` and
bytes ≥ 0x80 (UTF-8 continuation or lead bytes) count as word bytes, so
multi-byte letters clump together with their neighbors rather than
splitting words — crude but surprisingly serviceable, and the honest
version (Unicode word segmentation, UAX #29) plugs into the same two
functions later.

## Challenge: Anchor and Focus {#selection-model points=12}

Implement the selection value type and the offset-adjustment functions.

- `empty()`, `begin()`, `end()`: the normalized read-only view.
- `contains(o)`: is byte `o` highlighted? Half-open; empty selections
  contain nothing.
- `extend_to(sel, o)`: the Shift+click / drag primitive — move focus
  only.
- `adjust_for_insert(off, pos, n)`: offsets `>= pos` shift right by `n`.
- `adjust_for_erase(off, pos, n)`: before `pos` unchanged; `>= pos + n`
  shifts left; inside the range clamps to `pos`.
- `adjust_selection_*`: apply to both ends independently.

### Starter

```cpp
#include <algorithm>
#include <cstddef>

struct Selection {
    size_t anchor = 0;
    size_t focus = 0;

    bool empty() const {
        // TODO
        return true;
    }

    size_t begin() const {
        // TODO: min of the ends
        return 0;
    }

    size_t end() const {
        // TODO: max of the ends
        return 0;
    }

    bool contains(size_t o) const {
        // TODO: half-open, empty contains nothing
        (void)o;
        return false;
    }
};

Selection extend_to(Selection s, size_t o) {
    // TODO: move focus only
    (void)o;
    return s;
}

size_t adjust_for_insert(size_t off, size_t pos, size_t n) {
    // TODO
    (void)pos;
    (void)n;
    return off;
}

size_t adjust_for_erase(size_t off, size_t pos, size_t n) {
    // TODO
    (void)pos;
    (void)n;
    return off;
}

Selection adjust_selection_insert(Selection s, size_t pos, size_t n) {
    // TODO: both ends independently
    (void)pos;
    (void)n;
    return s;
}

Selection adjust_selection_erase(Selection s, size_t pos, size_t n) {
    // TODO
    (void)pos;
    (void)n;
    return s;
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // Collapsed selection is the caret.
        Selection s{5, 5};
        check(s.empty() && s.begin() == 5 && s.end() == 5,
              "test_collapsed");
        check(!s.contains(5) && !s.contains(4), "test_empty_contains_nothing");
    }

    {   // Backward selection normalizes in the view only.
        Selection s{20, 5};
        check(!s.empty() && s.begin() == 5 && s.end() == 20,
              "test_backward_normalized_view");
        check(s.anchor == 20 && s.focus == 5, "test_backward_keeps_ends");
        check(s.contains(5) && s.contains(19) && !s.contains(20) &&
              !s.contains(4),
              "test_contains_half_open");
    }

    {   // extend_to moves focus only; can shrink, cross, and re-extend.
        Selection s{10, 10};
        s = extend_to(s, 15);
        check(s.anchor == 10 && s.focus == 15, "test_extend_right");
        s = extend_to(s, 12);
        check(s.anchor == 10 && s.focus == 12, "test_extend_shrinks");
        s = extend_to(s, 3);
        check(s.anchor == 10 && s.focus == 3 && s.begin() == 3 &&
              s.end() == 10,
              "test_extend_crosses_anchor");
    }

    // --- insert adjustment ---
    check(adjust_for_insert(10, 20, 5) == 10, "test_insert_after_no_shift");
    check(adjust_for_insert(20, 10, 5) == 25, "test_insert_before_shifts");
    check(adjust_for_insert(10, 10, 5) == 15, "test_insert_at_caret_rides");
    check(adjust_for_insert(0, 0, 3) == 3, "test_insert_at_zero");

    // --- erase adjustment ---
    check(adjust_for_erase(5, 10, 4) == 5, "test_erase_after_no_shift");
    check(adjust_for_erase(20, 10, 4) == 16, "test_erase_before_shifts");
    check(adjust_for_erase(12, 10, 4) == 10, "test_erase_inside_clamps");
    check(adjust_for_erase(10, 10, 4) == 10, "test_erase_at_start_clamps");
    check(adjust_for_erase(14, 10, 4) == 10, "test_erase_at_end_boundary");

    {   // Whole-selection adjustment, ends handled independently.
        Selection s{4, 12};
        Selection t = adjust_selection_insert(s, 8, 3);
        check(t.anchor == 4 && t.focus == 15, "test_selection_insert_between");

        Selection u = adjust_selection_erase(Selection{4, 12}, 6, 10);
        check(u.anchor == 4 && u.focus == 6, "test_selection_erase_clamps_focus");

        Selection v = adjust_selection_erase(Selection{15, 8}, 0, 4);
        check(v.anchor == 11 && v.focus == 4, "test_backward_selection_adjusts");
    }

    return failed;
}
```

## Challenge: Double-Click Words {#word-select points=15}

Implement the word machinery: the three-class byte classifier, `word_at`,
and the word-granularity drag.

- `classify(b)`: `Word` for ASCII letters, digits, `'_'`, and any byte ≥
  0x80; `Space` for `' '`, `'\t'`, `'\n'`; `Punct` for everything else.
- `word_at(text, o)`: the maximal same-class run around position `o`.
  For `o` inside the text, use the class of `text[o]` (the character to
  the right of the caret position); for `o == text.size()` use the run
  ending at the text's end. Empty text: `{0, 0}`.
- `drag_words(text, press, current)`: the double-click-drag selection —
  the union of `word_at(press)` and `word_at(current)`, ends arranged so
  focus tracks the mouse: dragging at-or-past the press point puts the
  focus at the union's right edge; dragging before it, at the left edge.

### Starter

```cpp
#include <cstddef>
#include <string_view>

enum class CharClass { Word, Space, Punct };

struct Range {
    size_t begin;
    size_t end;
};

struct Selection {
    size_t anchor = 0;
    size_t focus = 0;
};

CharClass classify(unsigned char b) {
    // TODO
    (void)b;
    return CharClass::Punct;
}

Range word_at(std::string_view text, size_t o) {
    // TODO: maximal same-class run around o
    (void)text;
    (void)o;
    return {0, 0};
}

Selection drag_words(std::string_view text, size_t press, size_t current) {
    // TODO: union of the two words; focus on the moving side
    (void)text;
    (void)press;
    (void)current;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static bool range_is(Range r, size_t b, size_t e) {
    return r.begin == b && r.end == e;
}

int main() {
    check(classify('a') == CharClass::Word && classify('Z') == CharClass::Word,
          "test_letters_are_word");
    check(classify('7') == CharClass::Word && classify('_') == CharClass::Word,
          "test_digits_underscore_word");
    check(classify(' ') == CharClass::Space && classify('\t') == CharClass::Space &&
          classify('\n') == CharClass::Space,
          "test_whitespace");
    check(classify('.') == CharClass::Punct && classify('(') == CharClass::Punct &&
          classify('-') == CharClass::Punct,
          "test_punct");
    check(classify(0xC3) == CharClass::Word && classify(0xA9) == CharClass::Word,
          "test_utf8_bytes_are_word");

    //            0123456789012345678
    std::string t = "foo_bar42 baz(qux)";

    check(range_is(word_at(t, 0), 0, 9), "test_word_at_start");
    check(range_is(word_at(t, 4), 0, 9), "test_word_interior");
    check(range_is(word_at(t, 8), 0, 9), "test_word_last_char");
    check(range_is(word_at(t, 9), 9, 10), "test_boundary_uses_right_char");
    check(range_is(word_at(t, 10), 10, 13), "test_second_word");
    check(range_is(word_at(t, 13), 13, 14), "test_punct_run");
    check(range_is(word_at(t, 17), 17, 18), "test_closing_paren");
    check(range_is(word_at(t, 18), 17, 18), "test_end_of_text_uses_left");

    {   // Punctuation runs clump.
        std::string p = "a==>b";
        check(range_is(word_at(p, 1), 1, 4), "test_punct_run_clumps");
    }

    {   // UTF-8 bytes stay glued to their word.
        std::string u = "caf\xc3\xa9 au";
        check(range_is(word_at(u, 0), 0, 5), "test_utf8_word_glued");
        check(range_is(word_at(u, 4), 0, 5), "test_utf8_word_from_inside");
    }

    {   // Whitespace gap is its own "word".
        std::string g = "a   b";
        check(range_is(word_at(g, 2), 1, 4), "test_gap_selects_gap");
    }

    {
        check(range_is(word_at("", 0), 0, 0), "test_empty_text");
        check(range_is(word_at("xyz", 3), 0, 3), "test_at_size_takes_last_run");
    }

    {   // Drag right: focus at the right edge of the union.
        Selection s = drag_words(t, 2, 11);
        check(s.anchor == 0 && s.focus == 13, "test_drag_right");
    }
    {   // Drag left: focus at the left edge.
        Selection s = drag_words(t, 11, 2);
        check(s.anchor == 13 && s.focus == 0, "test_drag_left");
    }
    {   // Release inside the pressed word: whole word stays selected.
        Selection s = drag_words(t, 4, 6);
        check(s.anchor == 0 && s.focus == 9, "test_drag_within_word");
        Selection r = drag_words(t, 4, 4);
        check(r.anchor == 0 && r.focus == 9, "test_no_drag_selects_word");
    }
    {   // Crossing into punctuation and back over spaces.
        Selection s = drag_words(t, 15, 9);
        check(s.anchor == 17 && s.focus == 9, "test_drag_left_from_qux");
    }

    return failed;
}
```
# Lesson: Scrolling and Damage {#scrolling-and-damage}

Open a 200,000-line log file in your editor. The window shows 40 lines.
Any work proportional to 200,000 — wrapping every line, painting every
line, even *iterating* every line per frame — is work you'll feel as lag.
The discipline that keeps editors fast has one name: **do O(viewport)
work, not O(document) work**. This lesson builds its two halves: knowing
*which rows are on screen* (scrolling & virtualization), and knowing
*which pixels actually changed* (damage tracking).

### The scroll model

Scrolling is one integer: `scroll_y`, the document-space pixel row shown
at the top of the viewport. The content is `row_count * line_height`
pixels tall; the viewport shows `viewport_h` of them. Three small
functions define the whole system, and each has a classic off-by-one
lurking:

- **Clamping.** Valid scroll positions are `[0, max(0, content_h -
  viewport_h)]` — you can't scroll above the top, and the last line
  stops at the *bottom* of the window rather than scrolling up past it.
  (When content is shorter than the viewport, the only valid position is
  0 — that `max(0, ...)` is the line everyone forgets, and without it
  short files jitter.)
- **Visibility.** The rows intersecting the viewport run from `scroll_y /
  line_height` (integer division — a partially visible top row counts)
  through the row containing the viewport's last pixel: `ceil((scroll_y
  + viewport_h) / line_height)`, capped at the row count. Only rows in
  this half-open range get wrapped (via the lesson-9 cache) and painted.
  This range *is* the virtualization: nothing outside it is ever
  touched.
- **Reveal.** When typing or arrow keys move the caret off screen, the
  view follows — by the *minimum* amount. If the caret row's top is
  above the viewport, align tops; if its bottom is below, align bottoms;
  if it's visible, don't move at all (gratuitous scrolling on every
  keystroke is deeply disorienting). Compute bottom-alignment first,
  then top — in the degenerate case of a viewport shorter than one line,
  the row's *top* must win, so the top correction is applied last.

Where does the lesson-4 `copy_rect` fit? When `scroll_y` changes by Δ,
the surviving `viewport_h - |Δ|` pixels are already rendered — blit them
to their new position and only the revealed strip needs painting. The
strip is damage, which brings us to the second half.

### Damage: repaint what changed, nothing else

Between two frames, almost nothing changes: a character appears, the
caret moves, a selection extends. The naive editor repaints the window
anyway — and at 4K that's 8 million pixels of glyph blitting per
keystroke, most redrawing what was already there. The professional
pattern is a **damage list** (the same idea the X server exposes as
Expose rectangles, Win32 as the invalid region, Cocoa as
`setNeedsDisplayInRect:`): every state change *registers* the rectangle
it dirtied; at paint time, only pixels inside the accumulated damage get
recomputed, and only that region is pushed to the screen.

The interesting design question is what to do when damage rectangles pile
up. Keep every rect and you repaint overlapping areas repeatedly and blit
dozens of tiny regions; collapse everything to one bounding box and a
caret blink plus a status-bar update repaints the whole diagonal between
them. The workable middle ground — used, in fancier region-algebra form,
by every windowing system — is pairwise **coalescing with a no-waste
rule**: merge two rects when doing so costs nothing, i.e. when they
overlap, or when their union's area doesn't exceed the sum of theirs
(adjacent same-height rects merge into one clean strip; far-apart specks
stay separate). One subtlety your implementation must handle: merging two
rects can make the merged result newly mergeable with a *third* — after
each merge, rescan until nothing combines. The list stabilizes fast in
practice (an editor's damage is a handful of text bands), and `take()` —
return everything, clear the list — is the paint loop's entire interface.

Half-open rects earn their keep here one more time: "adjacent" is exactly
`a.x + a.w == b.x`, no fudge terms, and the area arithmetic in the merge
rule is exact.

## Challenge: The Visible Window {#visible-rows points=12}

Implement the scroll math. All heights are pixels; `rows` is the total
visual row count from layout.

- `content_height(rows, lh)` — total pixel height.
- `clamp_scroll(scroll_y, viewport_h, lh, rows)` — nearest valid scroll
  position.
- `visible_rows(scroll_y, viewport_h, lh, rows)` — the half-open range
  of row indices intersecting the viewport. `scroll_y` is already
  clamped; a non-positive `viewport_h` or zero `rows` yields an empty
  range. Partially visible rows count at both ends.
- `scroll_to_reveal(scroll_y, viewport_h, lh, row)` — the minimally
  adjusted scroll position making `row` fully visible (top-aligned when
  the viewport is shorter than one line, per the lesson).

### Starter

```cpp
#include <algorithm>
#include <cstddef>

struct RowRange {
    size_t first;
    size_t end;   // half-open; first == end means nothing visible
};

int content_height(size_t rows, int lh) {
    // TODO
    (void)rows;
    (void)lh;
    return 0;
}

int clamp_scroll(int scroll_y, int viewport_h, int lh, size_t rows) {
    // TODO: clamp into [0, max(0, content - viewport)]
    (void)viewport_h;
    (void)lh;
    (void)rows;
    return scroll_y;
}

RowRange visible_rows(int scroll_y, int viewport_h, int lh, size_t rows) {
    // TODO
    (void)scroll_y;
    (void)viewport_h;
    (void)lh;
    (void)rows;
    return {0, 0};
}

int scroll_to_reveal(int scroll_y, int viewport_h, int lh, size_t row) {
    // TODO: minimal adjustment; bottom correction, then top
    (void)viewport_h;
    (void)lh;
    (void)row;
    return scroll_y;
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    const int LH = 16;

    check(content_height(100, LH) == 1600, "test_content_height");
    check(content_height(0, LH) == 0, "test_content_height_empty");

    // --- clamping ---
    check(clamp_scroll(-50, 200, LH, 100) == 0, "test_clamp_negative");
    check(clamp_scroll(0, 200, LH, 100) == 0, "test_clamp_zero_ok");
    check(clamp_scroll(1400, 200, LH, 100) == 1400, "test_clamp_max_ok");
    check(clamp_scroll(9999, 200, LH, 100) == 1400, "test_clamp_past_end");
    check(clamp_scroll(50, 200, LH, 5) == 0, "test_clamp_short_content");

    // --- visibility ---
    {
        RowRange r = visible_rows(0, 200, LH, 100);
        // 200 / 16 = 12.5 rows: rows 0..12 inclusive are (partly) visible.
        check(r.first == 0 && r.end == 13, "test_visible_from_top");
    }
    {
        RowRange r = visible_rows(160, 160, LH, 100);
        // Exact alignment: rows 10..19, nothing partial.
        check(r.first == 10 && r.end == 20, "test_visible_exact");
    }
    {
        RowRange r = visible_rows(8, 16, LH, 100);
        // Straddles rows 0 and 1.
        check(r.first == 0 && r.end == 2, "test_partial_rows_count");
    }
    {
        RowRange r = visible_rows(1400, 200, LH, 100);
        // Bottom of the document: rows 87..99.
        check(r.first == 87 && r.end == 100, "test_visible_clamps_to_count");
    }
    {
        RowRange r = visible_rows(0, 200, LH, 3);
        check(r.first == 0 && r.end == 3, "test_short_content");
    }
    {
        RowRange r = visible_rows(0, 0, LH, 100);
        check(r.first == r.end, "test_zero_viewport_empty");
        RowRange r2 = visible_rows(0, 200, LH, 0);
        check(r2.first == r2.end, "test_no_rows_empty");
    }

    // --- reveal ---
    check(scroll_to_reveal(0, 160, LH, 5) == 0, "test_reveal_already_visible");
    check(scroll_to_reveal(0, 160, LH, 0) == 0, "test_reveal_first_row");
    // Row 20 (top 320, bottom 336) below a 0..160 viewport: bottom-align.
    check(scroll_to_reveal(0, 160, LH, 20) == 176, "test_reveal_below");
    // One row below the last visible: scrolls exactly one line.
    check(scroll_to_reveal(0, 160, LH, 10) == 16, "test_reveal_minimal_down");
    // Row above the viewport: top-align.
    check(scroll_to_reveal(320, 160, LH, 2) == 32, "test_reveal_above");
    // Boundary: top row partially cut (scroll 8): revealing row 0 top-aligns.
    check(scroll_to_reveal(8, 160, LH, 0) == 0, "test_reveal_partial_top");
    // Viewport shorter than a line: align the row's TOP.
    check(scroll_to_reveal(0, 10, LH, 3) == 48, "test_reveal_tiny_viewport");

    return failed;
}
```

## Challenge: The Damage List {#damage-rects points=15}

Implement `DamageList` with no-waste coalescing. The starter includes the
lesson-2 geometry kit, solved.

- `add(r)`: ignore empty rects. Otherwise merge `r` with any stored rect
  it's *mergeable* with — mergeable means they overlap, or the union's
  area is no larger than the sum of their areas (compute areas in 64-bit:
  `long long`). Merging removes the partner, replaces `r` with the
  union, and **rescans**, because the grown rect may now absorb others.
  When nothing merges, store `r`.
- `empty()`, `count()`: bookkeeping for the tests.
- `bounds()`: bounding box of everything stored (empty rect when empty)
  — what you'd hand to a single `xcb_put_image`.
- `take()`: return the list and clear it — called once per paint.

### Starter

```cpp
#include <algorithm>
#include <cstddef>
#include <vector>

struct Rect {
    int x = 0, y = 0, w = 0, h = 0;
    bool empty() const { return w <= 0 || h <= 0; }
};

inline Rect intersect(Rect a, Rect b) {
    if (a.empty() || b.empty())
        return {};
    int x0 = std::max(a.x, b.x), y0 = std::max(a.y, b.y);
    int x1 = std::min(a.x + a.w, b.x + b.w);
    int y1 = std::min(a.y + a.h, b.y + b.h);
    if (x1 <= x0 || y1 <= y0)
        return {};
    return {x0, y0, x1 - x0, y1 - y0};
}

inline Rect union_of(Rect a, Rect b) {
    if (a.empty())
        return b;
    if (b.empty())
        return a;
    int x0 = std::min(a.x, b.x), y0 = std::min(a.y, b.y);
    int x1 = std::max(a.x + a.w, b.x + b.w);
    int y1 = std::max(a.y + a.h, b.y + b.h);
    return {x0, y0, x1 - x0, y1 - y0};
}

inline long long area(Rect r) {
    return r.empty() ? 0 : static_cast<long long>(r.w) * r.h;
}

class DamageList {
public:
    void add(Rect r) {
        // TODO: merge-with-rescan, then store
        (void)r;
    }

    bool empty() const {
        // TODO
        return true;
    }

    size_t count() const {
        // TODO
        return 0;
    }

    Rect bounds() const {
        // TODO
        return {};
    }

    std::vector<Rect> take() {
        // TODO: hand back the rects, leave the list empty
        return {};
    }

private:
    std::vector<Rect> rects_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static bool same(Rect a, Rect b) {
    return a.x == b.x && a.y == b.y && a.w == b.w && a.h == b.h;
}

static bool has(const std::vector<Rect>& v, Rect r) {
    for (const Rect& e : v)
        if (same(e, r))
            return true;
    return false;
}

int main() {
    {   // Basics: add, take, clear.
        DamageList d;
        check(d.empty() && d.count() == 0 && d.bounds().empty(),
              "test_starts_empty");
        d.add({10, 10, 5, 5});
        check(!d.empty() && d.count() == 1, "test_add_one");
        std::vector<Rect> got = d.take();
        check(got.size() == 1 && same(got[0], {10, 10, 5, 5}),
              "test_take_returns_rect");
        check(d.empty() && d.count() == 0, "test_take_clears");
    }

    {   // Empty rects are ignored.
        DamageList d;
        d.add({5, 5, 0, 10});
        d.add({5, 5, 10, -2});
        check(d.empty(), "test_empty_rects_ignored");
    }

    {   // Far-apart rects stay separate (merging would waste area).
        DamageList d;
        d.add({0, 0, 10, 10});
        d.add({100, 100, 10, 10});
        check(d.count() == 2, "test_distant_stay_separate");
        check(same(d.bounds(), {0, 0, 110, 110}), "test_bounds_covers_all");
    }

    {   // Overlapping rects merge into the union.
        DamageList d;
        d.add({0, 0, 10, 10});
        d.add({5, 5, 10, 10});
        check(d.count() == 1, "test_overlap_merges");
        std::vector<Rect> got = d.take();
        check(got.size() == 1 && same(got[0], {0, 0, 15, 15}),
              "test_overlap_union");
    }

    {   // Adjacent same-height rects merge (no wasted area).
        DamageList d;
        d.add({0, 0, 10, 10});
        d.add({10, 0, 10, 10});
        std::vector<Rect> got = d.take();
        check(got.size() == 1 && same(got[0], {0, 0, 20, 10}),
              "test_adjacent_strips_merge");
    }

    {   // A contained rect disappears into its container.
        DamageList d;
        d.add({0, 0, 100, 100});
        d.add({20, 20, 10, 10});
        std::vector<Rect> got = d.take();
        check(got.size() == 1 && same(got[0], {0, 0, 100, 100}),
              "test_contained_absorbed");
    }

    {   // Chain reaction: a bridge rect fuses two separated ones.
        DamageList d;
        d.add({0, 0, 10, 10});
        d.add({30, 0, 10, 10});
        check(d.count() == 2, "test_pre_bridge_separate");
        d.add({10, 0, 20, 10});
        std::vector<Rect> got = d.take();
        check(got.size() == 1 && same(got[0], {0, 0, 40, 10}),
              "test_bridge_chain_merge");
    }

    {   // Typical editor frame: two text bands + caret sliver.
        DamageList d;
        d.add({0, 32, 640, 16});     // edited line's band
        d.add({0, 48, 640, 16});     // reflowed next line
        d.add({204, 32, 2, 16});     // caret, inside the first band
        std::vector<Rect> got = d.take();
        check(got.size() == 1 && same(got[0], {0, 32, 640, 32}),
              "test_editor_frame_coalesces");
    }

    {   // Separate specks: bounds is a box, take is still precise.
        DamageList d;
        d.add({0, 0, 2, 2});
        d.add({500, 300, 2, 2});
        check(same(d.bounds(), {0, 0, 502, 302}), "test_speck_bounds");
        std::vector<Rect> got = d.take();
        check(got.size() == 2 && has(got, {0, 0, 2, 2}) &&
              has(got, {500, 300, 2, 2}),
              "test_speck_list_precise");
    }

    return failed;
}
```
# Lesson: The Clipboard {#the-clipboard}

Copy and paste feels like the simplest feature in the editor. On Windows
it nearly is: `OpenClipboard`, `SetClipboardData(CF_UNICODETEXT, hglobal)`
— the OS keeps a copy in a central store, and your process can exit
without the data vanishing. macOS's `NSPasteboard` is the same central-
store idea with a richer type system (`writeObjects:`, UTIs for types).

X11 did something completely different, and famously so: **there is no
clipboard**. There is no central store at all. What X has is
**selections** — a protocol for *live negotiation between clients*,
standardized in the ICCCM. Understanding it will finally explain two
Linux folklore facts: why middle-click paste exists, and why your copied
text disappears when you close the app you copied it from.

### Selections: copy is a claim, paste is a conversation

When you "copy" in an X11 app, no data moves anywhere. The app simply
tells the server: *I own the selection named CLIPBOARD now* (that's
`xcb_set_selection_owner`; the name is an atom — lesson 3's interned
strings, back again). Whoever owned it before receives a
**SelectionClear** event: you've been dethroned; release your buffered
data. That's the entire copy operation — a claim, one previous owner
notified. (There are several selections co-existing: `CLIPBOARD` for
explicit Ctrl+C/Ctrl+V, `PRIMARY` for select-then-middle-click — the
same protocol, different atom, which is how those two paste channels
stay independent.)

Paste is where the data actually moves, and it's a four-step
conversation between the *requestor* (the app being pasted into) and the
*owner* (the app holding the copied data):

1. The requestor asks the server to deliver a **SelectionRequest** to
   the owner: "convert selection CLIPBOARD to *target* `UTF8_STRING`,
   and put the result in *property* P on my window W."
2. The owner receives the event, looks at the requested target, and —
   if it can — writes the converted bytes into property P on window W
   (properties are the server-side key-value storage from lesson 3, so
   the server briefly holds the data in transit).
3. The owner sends back a **SelectionNotify**: "done — look in property
   P." Refusal has a shape too: a SelectionNotify with property `None`
   (atom 0) means "I can't produce that target".
4. The requestor reads and deletes the property.

Before asking for text, well-behaved requestors first ask for the
special target **TARGETS**: "what formats can you offer?" The owner
answers with a list of atoms — `UTF8_STRING`, maybe `text/html`, maybe
`image/png` from a screenshot tool. That's the negotiation that lets
"paste" mean rich text between word processors but plain text into a
terminal. Your editor's owner side supports exactly two targets: TARGETS
itself, and `UTF8_STRING`.

Two consequences of this design, now explicable:

- **Data dies with its owner.** The owner *is* the store. Close the app
  and there is nobody left to answer SelectionRequests. (Clipboard-
  manager daemons fix this by watching for ownership changes and
  immediately requesting a copy for themselves — becoming a hidden
  second app that never exits.)
- **Nothing transfers until paste.** Copying a 100 MB selection costs
  nothing until someone asks for it — lazy, like the piece table.

One more protocol wrinkle you should recognize by name: server
properties have practical size limits, so for large transfers the owner
writes the property with type **INCR** instead of the requested target —
"this will arrive in chunks" — and the two clients then ping-pong
property writes and deletions until a zero-length write says done. We
model the *decision* (small enough to
send directly, or INCR?) without the chunk loop.

The seam stays honest through all of this: the editor core calls
`Platform::clipboard_write(text)` and `clipboard_read()`. The Win32
backend implements those in six lines; the X11 backend runs this whole
conversation — and the state machine at its heart is this lesson's first
challenge, pure logic, no server needed.

### The editing side: cut, copy, paste as document operations

Independent of transport, the three commands are selection-and-text
algebra with firm conventions users rely on:

- **Copy** with a non-empty selection stores its text and leaves the
  document untouched — copy never collapses the selection, so the
  highlight stays put and you can keep working with it (watch any
  editor). Copy with an *empty* selection is a no-op that must **not**
  clear the clipboard — nothing is more rage-inducing than losing a
  clipboard to a stray Ctrl+C.
- **Cut** = copy + delete selection + caret collapses where the text
  was.
- **Paste** replaces the selection (if any) with the clipboard, caret
  landing *after* the inserted text, selection collapsed. Pasting an
  empty clipboard is a no-op — it must not silently delete a selection.

## Challenge: Own the Selection {#selection-owner points=15}

Implement the owner side of the X11 selection protocol as a pure state
machine. (Your real backend feeds it `xcb_selection_request_event_t`s
and turns its answers into `xcb_send_event` + `xcb_change_property`
calls; every rule below is straight from the ICCCM.)

- `copy(text)`: become the owner and store the data.
- `on_clear()`: SelectionClear arrived — another client claimed the
  selection. Stop owning; release the stored data (`text()` becomes
  empty).
- `on_request(req, out_targets, out_data)` returns the SelectionNotify
  to send:
  - Not the owner (never copied, or cleared): refuse — property `0`,
    regardless of target.
  - `Target::Targets`: set `*out_targets = {Targets, Utf8String}` and
    succeed with the requested property echoed back.
  - `Target::Utf8String`: if the data is larger than `kIncrThreshold`
    bytes, succeed with `incr = true` and leave `out_data` alone (the
    chunk loop would follow); otherwise write the data to `*out_data`
    and succeed normally.
  - `Target::Other` (`text/html`, images... things we can't produce):
    refuse — property `0`.
  - Every notify carries the requestor's window ID back.

### Starter

```cpp
#include <cstddef>
#include <string>
#include <vector>

enum class Target { Targets, Utf8String, Other };

constexpr size_t kIncrThreshold = 16;   // tiny, to make tests readable

struct SelRequest {
    unsigned requestor;   // window to notify
    Target target;
    unsigned property;    // where the requestor wants the data (never 0)
};

struct SelNotify {
    unsigned requestor;
    unsigned property;    // 0 = refused
    bool incr = false;    // true = data will follow in INCR chunks
};

class ClipboardOwner {
public:
    void copy(std::string text) {
        // TODO
        (void)text;
    }

    void on_clear() {
        // TODO
    }

    bool owns() const {
        // TODO
        return false;
    }

    const std::string& text() const { return data_; }

    SelNotify on_request(const SelRequest& req,
                         std::vector<Target>* out_targets,
                         std::string* out_data) const {
        // TODO
        (void)out_targets;
        (void)out_data;
        return {req.requestor, 0, false};
    }

private:
    std::string data_;
    bool owning_ = false;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // Never copied: every request is refused.
        ClipboardOwner o;
        check(!o.owns(), "test_starts_unowned");
        std::vector<Target> t;
        std::string d;
        SelNotify n = o.on_request({77, Target::Utf8String, 5}, &t, &d);
        check(n.requestor == 77 && n.property == 0 && d.empty(),
              "test_unowned_refuses");
    }

    {   // The full paste conversation.
        ClipboardOwner o;
        o.copy("hello");
        check(o.owns() && o.text() == "hello", "test_copy_takes_ownership");

        std::vector<Target> t;
        std::string d;
        SelNotify n1 = o.on_request({42, Target::Targets, 9}, &t, &d);
        check(n1.requestor == 42 && n1.property == 9 && !n1.incr,
              "test_targets_succeeds");
        bool has_targets = false, has_utf8 = false;
        for (Target x : t) {
            if (x == Target::Targets)
                has_targets = true;
            if (x == Target::Utf8String)
                has_utf8 = true;
        }
        check(t.size() == 2 && has_targets && has_utf8,
              "test_targets_lists_both");

        SelNotify n2 = o.on_request({42, Target::Utf8String, 9}, &t, &d);
        check(n2.property == 9 && !n2.incr && d == "hello",
              "test_utf8_delivers_data");
    }

    {   // Unsupported target: refused, but ownership continues.
        ClipboardOwner o;
        o.copy("keep");
        std::vector<Target> t;
        std::string d;
        SelNotify n = o.on_request({7, Target::Other, 3}, &t, &d);
        check(n.property == 0 && d.empty(), "test_other_target_refused");
        check(o.owns() && o.text() == "keep",
              "test_refusal_keeps_ownership");
    }

    {   // Large data goes INCR; out_data stays untouched.
        ClipboardOwner o;
        o.copy("this string is definitely longer than sixteen bytes");
        std::vector<Target> t;
        std::string d;
        SelNotify n = o.on_request({8, Target::Utf8String, 4}, &t, &d);
        check(n.property == 4 && n.incr && d.empty(),
              "test_large_data_incr");
        // Exactly at the threshold: direct, not INCR.
        ClipboardOwner p;
        p.copy("0123456789abcdef");   // 16 bytes
        SelNotify m = p.on_request({8, Target::Utf8String, 4}, &t, &d);
        check(!m.incr && d == "0123456789abcdef",
              "test_threshold_is_exclusive");
    }

    {   // SelectionClear dethrones and releases the data.
        ClipboardOwner o;
        o.copy("mine");
        o.on_clear();
        check(!o.owns() && o.text().empty(), "test_clear_releases");
        std::vector<Target> t;
        std::string d;
        SelNotify n = o.on_request({3, Target::Utf8String, 2}, &t, &d);
        check(n.property == 0, "test_cleared_refuses");
        // Copying again re-establishes ownership.
        o.copy("back");
        SelNotify m = o.on_request({3, Target::Utf8String, 2}, &t, &d);
        check(m.property == 2 && d == "back", "test_recopy_owns_again");
    }

    return failed;
}
```

## Challenge: Cut, Copy, Paste {#clipboard-ops points=12}

Implement the three commands as pure functions over an editing state.
`sel_begin <= sel_end` is guaranteed (the normalized view from lesson
11).

- `do_copy`: non-empty selection → clipboard becomes the selected text,
  state untouched. Empty selection → *everything* untouched.
- `do_cut`: non-empty selection → clipboard becomes the selected text,
  the selection's bytes are removed, caret collapses to `sel_begin`.
  Empty → no-op.
- `do_paste`: non-empty clipboard → selection (possibly empty) is
  replaced by the clipboard text, caret collapses to just after it,
  clipboard unchanged. Empty clipboard → no-op.

### Starter

```cpp
#include <cstddef>
#include <string>

struct EditState {
    std::string text;
    size_t sel_begin = 0;
    size_t sel_end = 0;   // == sel_begin when the selection is empty
};

struct ClipResult {
    EditState state;
    std::string clipboard;
};

ClipResult do_copy(EditState s, std::string clipboard) {
    // TODO
    return {std::move(s), std::move(clipboard)};
}

ClipResult do_cut(EditState s, std::string clipboard) {
    // TODO
    return {std::move(s), std::move(clipboard)};
}

ClipResult do_paste(EditState s, std::string clipboard) {
    // TODO
    return {std::move(s), std::move(clipboard)};
}
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // Copy takes the selection, changes nothing else.
        ClipResult r = do_copy({"hello world", 6, 11}, "old");
        check(r.clipboard == "world", "test_copy_takes_selection");
        check(r.state.text == "hello world" && r.state.sel_begin == 6 &&
              r.state.sel_end == 11,
              "test_copy_leaves_state");
    }

    {   // Copy with empty selection must NOT clobber the clipboard.
        ClipResult r = do_copy({"hello", 2, 2}, "precious");
        check(r.clipboard == "precious" && r.state.text == "hello",
              "test_empty_copy_keeps_clipboard");
    }

    {   // Cut removes the text and collapses the caret.
        ClipResult r = do_cut({"hello world", 5, 11}, "");
        check(r.clipboard == " world", "test_cut_takes_selection");
        check(r.state.text == "hello" && r.state.sel_begin == 5 &&
              r.state.sel_end == 5,
              "test_cut_removes_and_collapses");
    }

    {   // Cut with empty selection: full no-op.
        ClipResult r = do_cut({"hello", 3, 3}, "keep");
        check(r.clipboard == "keep" && r.state.text == "hello" &&
              r.state.sel_begin == 3,
              "test_empty_cut_noop");
    }

    {   // Paste at a collapsed caret.
        ClipResult r = do_paste({"ab", 1, 1}, "XY");
        check(r.state.text == "aXYb", "test_paste_inserts");
        check(r.state.sel_begin == 3 && r.state.sel_end == 3,
              "test_paste_caret_after");
        check(r.clipboard == "XY", "test_paste_keeps_clipboard");
    }

    {   // Paste over a selection replaces it.
        ClipResult r = do_paste({"one two three", 4, 7}, "2");
        check(r.state.text == "one 2 three", "test_paste_replaces_selection");
        check(r.state.sel_begin == 5 && r.state.sel_end == 5,
              "test_paste_replacement_caret");
    }

    {   // Empty clipboard: paste is a no-op, selection survives.
        ClipResult r = do_paste({"danger zone", 0, 6}, "");
        check(r.state.text == "danger zone" && r.state.sel_begin == 0 &&
              r.state.sel_end == 6,
              "test_empty_paste_noop");
    }

    {   // Cut then paste round-trips.
        ClipResult cut = do_cut({"abcdef", 2, 4}, "");
        ClipResult back = do_paste(cut.state, cut.clipboard);
        check(back.state.text == "abcdef" && back.state.sel_begin == 4,
              "test_cut_paste_roundtrip");
    }

    {   // Paste at the very ends.
        ClipResult a = do_paste({"mid", 0, 0}, ">>");
        check(a.state.text == ">>mid" && a.state.sel_begin == 2,
              "test_paste_at_start");
        ClipResult b = do_paste({"mid", 3, 3}, "<<");
        check(b.state.text == "mid<<" && b.state.sel_begin == 5,
              "test_paste_at_end");
    }

    return failed;
}
```
# Lesson: Undo, Redo, and the Last Mile {#undo-redo-last-mile}

Undo is a contract with the user's trust: *nothing you do here is
irreversible*. Editors without dependable undo don't get used twice. The
mechanics are a solved problem with well-known shape — two stacks and an
inverse — but the *feel* of good undo lives in the details this lesson
pins down.

### Edits as values

The foundation is representing every text change as a value that carries
enough information to run **both directions**:

```cpp
struct EditOp {
    size_t pos;             // where
    std::string removed;    // what the edit deleted (empty for pure inserts)
    std::string inserted;   // what it added   (empty for pure deletions)
};
```

Typing `X` at 5 is `{5, "", "X"}`. Backspacing an `é` is `{4, "\xc3\xa9",
""}`. Pasting over a selection is `{pos, old_text, new_text}` — one op,
both facts. Applying an op replaces `removed` with `inserted` at `pos`;
the **inverse** just swaps the two strings, and applying the inverse
lands you byte-for-byte where you started. (Note the piece-table synergy
from lesson 7: the "removed" text is still sitting immutably in a buffer,
so real implementations can store spans instead of copies. We store
strings for clarity; the algebra is identical.)

This is the command pattern without ceremony — no `class ICommand`, no
virtual `Execute()`. A struct with three fields *is* the command, and
`inverse()` is three swaps. When a pattern reduces to a value type, let
it.

### Two stacks and one rule

The engine: an **undo stack** and a **redo stack**.

- A fresh edit is *recorded*: pushed onto undo. And here is the rule
  users depend on without knowing it: **recording clears the redo
  stack.** After undo–undo–type, the two undone futures are gone; Ctrl+Y
  must not resurrect them into your new timeline. (Editors that keep
  those branches — Vim's undo tree — are deliberately exotic.)
- **Undo** pops an op, pushes it onto redo, and hands back its *inverse*
  for the document to apply.
- **Redo** pops from redo, pushes back onto undo, and hands back the op
  itself. Undo/redo never record — they shuffle.

Where does the caret go? To the site of the change — after undo, to the
end of the restored text (`pos + removed.size()`); after redo, to the end
of the re-applied text. Restoring the *document* but leaving the caret
where it was strands the user staring at an unchanged screen wondering if
anything happened; every mainstream editor warps the caret (and scrolls
to reveal it — lesson 12's function, third customer).

### Grouping: undo at the speed of intention

Record one op per keystroke and Ctrl+Z peels off single letters — typing
a sentence takes thirty undos to remove. Users think in *runs*: "undo
what I just typed". So consecutive plain insertions **coalesce** into one
op, under conditions that all make sense once stated:

- both ops are pure insertions (`removed` empty — typing over a
  selection starts a fresh group, since that op also carries a
  deletion);
- the new insertion lands exactly at the end of the previous one
  (`pos == top.pos + top.inserted.size()`) — type, click elsewhere, type,
  and the position check alone splits the groups;
- and nothing else happened in between. The caller passes a `can_merge`
  flag for this: arrow keys, clicks, undo itself — anything that isn't
  uninterrupted typing — sets it false for the next record. Real editors
  also split on pauses and at word boundaries; those are policy tweaks
  on the same flag.

Merging means appending to the top op's `inserted` — the undo stack
doesn't grow. One Ctrl+Z, one burst of typing gone. That's the feel.

### The last mile

Four topics belong in your real build-out of the editor; none can be
graded headless, all deserve a map before you go:

- **High DPI.** A "pixel" in window coordinates may be 2×2 physical
  pixels. Each platform tells you the scale factor (`Xft.dpi` resource /
  `WM_DPICHANGED` / `backingScaleFactor`); render your framebuffer at
  physical size and multiply all font metrics by the scale — with a
  bitmap font, integer-scale the glyphs (8×16 → 16×32 looks crisp and
  period-correct; fractional scales are where you graduate to FreeType).
  The trap: mixing logical mouse coordinates with physical framebuffer
  coordinates — pick one space for the editor core (logical) and convert
  at the seam, in exactly one place.
- **File watching.** When the file changes on disk, offer to reload. On
  Linux that's `inotify` — a file descriptor you add to the same
  `poll()` as the X connection: the event loop gains a second input, not
  a thread. Watch the *directory*, not the file: most programs (and your
  own safe-save below) replace files by rename, which silently orphans a
  file-handle watch. Coalesce the burst of events a single save produces
  (your lesson-1 `pump` logic, fourth customer).
- **IME.** For Chinese, Japanese, Korean — and dead-key composition
  everywhere — text arrives through a *composition* dialogue
  (preedit-draw, commit), not as keystrokes: one more reason the core's
  text-entry API is `insert_text(string)` and never "handle key". Wire
  X11's XIM (or ibus), Windows' `WM_IME_*`, Cocoa's `NSTextInputClient`
  to the same two calls: draw preedit, commit text.
- **Saving safely.** Never truncate-and-write the user's file — a crash
  mid-write destroys both versions. Write to a temp file in the same
  directory, `fsync`, then `rename` over the target: POSIX rename is
  atomic, so the file is always either old or new, never half. (The
  fine print — preserving permissions/ownership, symlink targets,
  hardlink identity — is why "safe save" options exist in every serious
  editor's manual.)

## Challenge: Grouped Undo {#undo-groups points=15}

Implement the op algebra and the two-stack engine with coalescing.

- `apply_op(text, op)`: replace the `op.removed.size()` bytes at
  `op.pos` (guaranteed to equal `op.removed`) with `op.inserted`.
- `inverse(op)`: same position, strings swapped.
- `UndoStack::record(op, can_merge)`: clears redo. Merges into the top
  undo entry iff `can_merge`, the top exists, both ops are pure
  insertions, and `op.pos == top.pos + top.inserted.size()`; otherwise
  pushes.
- `undo()` / `redo()`: `std::nullopt` when their stack is empty;
  otherwise move the op across and return the op *to apply* (the inverse
  for undo, the original for redo).
- `undo_depth()` / `redo_depth()` for the tests.

### Starter

```cpp
#include <cstddef>
#include <optional>
#include <string>
#include <utility>
#include <vector>

struct EditOp {
    size_t pos = 0;
    std::string removed;
    std::string inserted;
};

std::string apply_op(std::string text, const EditOp& op) {
    // TODO
    return text;
}

EditOp inverse(const EditOp& op) {
    // TODO
    return op;
}

class UndoStack {
public:
    void record(EditOp op, bool can_merge) {
        // TODO
        (void)op;
        (void)can_merge;
    }

    std::optional<EditOp> undo() {
        // TODO
        return std::nullopt;
    }

    std::optional<EditOp> redo() {
        // TODO
        return std::nullopt;
    }

    size_t undo_depth() const {
        // TODO
        return 0;
    }

    size_t redo_depth() const {
        // TODO
        return 0;
    }

private:
    std::vector<EditOp> undo_;
    std::vector<EditOp> redo_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>
#include <string>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main() {
    {   // apply_op both directions.
        EditOp op{5, "", "X"};
        check(apply_op("hello world", op) == "helloX world", "test_apply_insert");
        EditOp del{0, "he", ""};
        check(apply_op("hello", del) == "llo", "test_apply_delete");
        EditOp rep{4, "two", "2"};
        check(apply_op("one two three", rep) == "one 2 three",
              "test_apply_replace");
        EditOp inv = inverse(rep);
        check(inv.pos == 4 && inv.removed == "2" && inv.inserted == "two",
              "test_inverse_swaps");
        check(apply_op(apply_op("one two three", rep), inverse(rep)) ==
                  "one two three",
              "test_inverse_roundtrip");
    }

    {   // Typing coalesces into one op.
        UndoStack u;
        std::string text = "";
        EditOp a{0, "", "h"};
        text = apply_op(text, a);
        u.record(a, true);
        EditOp b{1, "", "e"};
        text = apply_op(text, b);
        u.record(b, true);
        EditOp c{2, "", "y"};
        text = apply_op(text, c);
        u.record(c, true);
        check(text == "hey" && u.undo_depth() == 1, "test_typing_merges");

        std::optional<EditOp> op = u.undo();
        check(op.has_value(), "test_undo_returns_op");
        if (op)
            text = apply_op(text, *op);
        check(text == "", "test_undo_removes_whole_run");
        check(u.undo_depth() == 0 && u.redo_depth() == 1,
              "test_stacks_after_undo");
    }

    {   // can_merge == false splits groups.
        UndoStack u;
        u.record({0, "", "a"}, true);
        u.record({1, "", "b"}, false);   // e.g. the user clicked in between
        check(u.undo_depth() == 2, "test_merge_flag_respected");
    }

    {   // Non-adjacent insertions never merge.
        UndoStack u;
        u.record({0, "", "ab"}, true);
        u.record({5, "", "c"}, true);
        check(u.undo_depth() == 2, "test_non_adjacent_split");
    }

    {   // Ops with removals never merge (either side).
        UndoStack u;
        u.record({0, "", "ab"}, true);
        u.record({2, "xy", "z"}, true);    // replaced a selection
        check(u.undo_depth() == 2, "test_replacement_not_merged");
        u.record({3, "", "w"}, true);      // typing after it: new group too
        check(u.undo_depth() == 3, "test_after_replacement_new_group");
    }

    {   // Undo/redo round trip over a mixed history.
        UndoStack u;
        std::string text = "hello";
        EditOp e1{5, "", " world"};
        text = apply_op(text, e1);
        u.record(e1, true);
        EditOp e2{0, "h", "H"};
        text = apply_op(text, e2);
        u.record(e2, false);
        check(text == "Hello world", "test_history_applied");

        text = apply_op(text, *u.undo());
        check(text == "hello world", "test_undo_step_one");
        text = apply_op(text, *u.undo());
        check(text == "hello", "test_undo_step_two");
        check(!u.undo().has_value(), "test_undo_exhausted");

        text = apply_op(text, *u.redo());
        text = apply_op(text, *u.redo());
        check(text == "Hello world", "test_redo_replays");
        check(!u.redo().has_value(), "test_redo_exhausted");
        check(u.undo_depth() == 2, "test_ops_back_on_undo_stack");
    }

    {   // A new edit clears the redo stack.
        UndoStack u;
        std::string text = "";
        EditOp a{0, "", "a"};
        text = apply_op(text, a);
        u.record(a, true);
        text = apply_op(text, *u.undo());
        check(u.redo_depth() == 1, "test_redo_pending");
        EditOp b{0, "", "b"};
        text = apply_op(text, b);
        u.record(b, false);
        check(u.redo_depth() == 0, "test_record_clears_redo");
        check(!u.redo().has_value() && text == "b", "test_no_zombie_redo");
    }

    return failed;
}
```
# Final Challenge: The Editor Core {#editor-core points=75}

Time to assemble the machine. `EditorCore` is the 100%-portable heart of
the editor: document, layout, selection, editing, undo, damage — driven
entirely by synthetic events and queried entirely through values. In the
real application, the platform layer you studied in lessons 1–4 is a thin
shell around exactly this class: the X11 (or Win32, or Cocoa) event
switch translates native events into these method calls, and the paint
handler asks `take_damage()` what to repaint. Here, the test harness
plays the platform: it clicks, drags, types, and asserts.

Use the pieces you've built: the piece table is the intended document
store (a `std::string` will pass the tests — behavior is all that's
graded — but you'd be skipping the point), the line index and wrap
algorithm drive layout, the anchor/focus model drives selection, and the
lesson-14 stack drives undo. The starter provides, fully implemented: the
UTF-8 boundary functions (lesson 6), `word_at` and `classify` (lesson
11), and `Rect` with `union_of` (lesson 2). Everything else is yours.

The font is a fake fixed-width face: **every codepoint advances 8
pixels** (`kAdvance`), and rows are `line_height` pixels tall. This is
the "stub metrics" trick — with geometry this predictable, the tests can
compute expected pixel math by hand, while your code stays structured
exactly as if a real `FontMetrics` were plugged in.

The contract, in five parts. It is deliberately exhaustive — every rule
below is exercised by at least one test.

**Layout.** The document is logical lines split on `'\n'` (lesson 8
conventions: offset 0 always starts a line; a trailing newline yields a
final empty line). Each line wraps greedily at `wrap_width` pixels per
lesson 9's exact rules, generalized to codepoints: iterate UTF-8
boundaries, 8px per codepoint, break opportunities after `' '`, strictly-
greater overflow, hard-break inside long words, always make progress; an
empty line is one empty row. The document's rows are all lines' rows
concatenated, top to bottom; `row_count()` reports the total. Row `i`
covers pixels `y ∈ [i*line_height, (i+1)*line_height)`. **The displaying
row of an offset** (needed for damage) is the *last* row whose `begin <=
offset` — i.e. wrap boundaries display downstream, no affinity bit in
this final.

**Hit testing.** `click`/`double_click`/`drag` take pixel coordinates:
clamp `y` to a row (negative → first, beyond → last), then resolve `x`
within the row by the half-advance rule with ties right (`x < left + 4`
puts the caret before the codepoint); `x < 0` → row start, past the last
glyph → row end.

**Selection & movement.** Anchor/focus (lesson 11); `caret()` returns the
focus; `sel_begin()`/`sel_end()` the normalized ends. `click` collapses
both to the hit; `drag` moves only the focus; `double_click` selects
`word_at(hit)` (anchor at its begin, focus at its end). `left`/`right`
with `shift` move only the focus by one UTF-8 boundary (clamped at the
document ends); without `shift`, a non-empty selection *collapses* to its
begin/end (no extra movement), and an empty one moves one boundary.
`home`/`end_key` go to the start/end of the *logical* line containing the
focus (end = before the `'\n'`); with `shift` they move the focus only,
extending or shrinking the selection. Without `shift` they collapse both
anchor and focus to that line boundary outright — unlike plain
`left`/`right`, which spend their first press collapsing an existing
selection to its nearer edge, `home`/`end_key` always jump all the way to
the line start/end even if a selection was active.

**Editing & undo.** `insert_text(s)` (empty `s`: total no-op) replaces
the selection (possibly empty) with `s`; caret lands after `s`,
collapsed. `backspace` deletes the selection, or one codepoint before the
caret (no-op at offset 0); `key_delete` deletes the selection, or one
codepoint after (no-op at the end). Every text change records a lesson-14
`EditOp {pos, removed, inserted}`; recording clears redo. Consecutive
`insert_text` calls coalesce under exactly the lesson-14 rule (both pure
insertions, adjacent, nothing else in between — *any* other event,
including clicks and arrows, breaks the run). `undo()` applies the
inverse of the top op and collapses the caret to `op.pos +
op.removed.size()`; `redo()` re-applies and lands at `op.pos +
op.inserted.size()`; both are silent no-ops when their stack is empty.

**Damage.** `take_damage()` returns the bounding box of everything that
changed since the last call (an empty `Rect` if nothing did), then
resets. Damage accrues as full-width bands of rows, unioned together:

- *Selection change* (any event that alters the anchor/focus pair
  without changing text): the band from the lowest to the highest of
  the displaying rows of {old begin, old end, new begin, new end},
  inclusive, i.e. `y` from `min_row * line_height` through `(max_row +
  1) * line_height`. Events that leave anchor and focus unchanged add
  no damage.
- *Text change* (insert, backspace, delete, undo, redo): re-layout first,
  then compute damage against the *new* text — let `L` be the logical
  line containing the edit position as the document now reads (inserting
  or deleting a `'\n'` changes where line boundaries fall, so `L` must be
  found post-edit, not looked up in the old layout), and `first` the
  displaying row of `L`'s first offset. The band runs from
  `first * line_height` to `max(row_count_before, row_count_after) *
  line_height`. (Everything from the edited line to the bottom of the
  taller layout — reflow can move every row below the edit.)

### Starter

```cpp
#include <algorithm>
#include <cstddef>
#include <string>
#include <string_view>
#include <vector>

// ---------- provided: geometry (lesson 2) ----------

struct Rect {
    int x = 0, y = 0, w = 0, h = 0;
    bool empty() const { return w <= 0 || h <= 0; }
};

inline Rect union_of(Rect a, Rect b) {
    if (a.empty())
        return b;
    if (b.empty())
        return a;
    int x0 = std::min(a.x, b.x), y0 = std::min(a.y, b.y);
    int x1 = std::max(a.x + a.w, b.x + b.w);
    int y1 = std::max(a.y + a.h, b.y + b.h);
    return {x0, y0, x1 - x0, y1 - y0};
}

// ---------- provided: UTF-8 boundaries (lesson 6) ----------

inline bool is_continuation(unsigned char b) { return (b & 0xC0) == 0x80; }

inline int declared_len(unsigned char b) {
    if (b < 0x80)
        return 1;
    if ((b & 0xE0) == 0xC0)
        return 2;
    if ((b & 0xF0) == 0xE0)
        return 3;
    if ((b & 0xF8) == 0xF0)
        return 4;
    return 1;
}

inline size_t next_boundary(std::string_view s, size_t i) {
    unsigned char b = static_cast<unsigned char>(s[i]);
    if (is_continuation(b))
        return i + 1;
    size_t end = std::min(s.size(), i + size_t(declared_len(b)));
    size_t k = i + 1;
    while (k < end && is_continuation(static_cast<unsigned char>(s[k])))
        ++k;
    return k;
}

inline size_t prev_boundary(std::string_view s, size_t i) {
    size_t j = i - 1;
    int steps = 0;
    while (j > 0 && is_continuation(static_cast<unsigned char>(s[j])) &&
           steps < 3) {
        --j;
        ++steps;
    }
    unsigned char lead = static_cast<unsigned char>(s[j]);
    if (is_continuation(lead))
        return i - 1;
    if (j + size_t(declared_len(lead)) >= i)
        return j;
    return i - 1;
}

// ---------- provided: word boundaries (lesson 11) ----------

enum class CharClass { Word, Space, Punct };

struct Range {
    size_t begin;
    size_t end;
};

inline CharClass classify(unsigned char b) {
    if (b >= 0x80)
        return CharClass::Word;
    if ((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
        (b >= '0' && b <= '9') || b == '_')
        return CharClass::Word;
    if (b == ' ' || b == '\t' || b == '\n')
        return CharClass::Space;
    return CharClass::Punct;
}

inline Range word_at(std::string_view text, size_t o) {
    if (text.empty())
        return {0, 0};
    if (o >= text.size())
        o = text.size() - 1;
    CharClass cls = classify(static_cast<unsigned char>(text[o]));
    size_t b = o;
    while (b > 0 && classify(static_cast<unsigned char>(text[b - 1])) == cls)
        --b;
    size_t e = o + 1;
    while (e < text.size() &&
           classify(static_cast<unsigned char>(text[e])) == cls)
        ++e;
    return {b, e};
}

// ---------- yours from here ----------

constexpr int kAdvance = 8;   // every codepoint is 8px wide

class EditorCore {
public:
    EditorCore(std::string text, int wrap_width, int line_height)
        : text_(std::move(text)), wrap_w_(wrap_width), lh_(line_height) {}

    // --- queries ---
    std::string text() const { return text_; }

    size_t caret() const {
        // TODO: the focus end
        return 0;
    }

    size_t sel_begin() const {
        // TODO
        return 0;
    }

    size_t sel_end() const {
        // TODO
        return 0;
    }

    size_t row_count() const {
        // TODO: wrap every logical line, count the rows
        return 0;
    }

    Rect take_damage() {
        // TODO: return and reset the accumulated bounding box
        return {};
    }

    // --- events ---
    void insert_text(std::string_view s) { (void)s; /* TODO */ }
    void backspace() { /* TODO */ }
    void key_delete() { /* TODO */ }
    void left(bool shift) { (void)shift; /* TODO */ }
    void right(bool shift) { (void)shift; /* TODO */ }
    void home(bool shift) { (void)shift; /* TODO */ }
    void end_key(bool shift) { (void)shift; /* TODO */ }
    void click(int x, int y) { (void)x; (void)y; /* TODO */ }
    void double_click(int x, int y) { (void)x; (void)y; /* TODO */ }
    void drag(int x, int y) { (void)x; (void)y; /* TODO */ }
    void undo() { /* TODO */ }
    void redo() { /* TODO */ }

private:
    // Suggested shape (all yours to change):
    //  - std::vector<size_t> line_starts() const;
    //  - rows(): wrap each line per lesson 9, per codepoint, 8px each
    //  - row_of(rows, offset): last row with begin <= offset
    //  - hit(x, y): clamp y to a row, half-advance within it
    //  - move_to(anchor, focus): selection-change damage + assignment
    //  - edit_damage(pos, old_row_count)
    //  - record(EditOp, can_merge) + undo/redo stacks (lesson 14)

    std::string text_;
    int wrap_w_;
    int lh_;
};
```

### Tests

```cpp
#include "solution.cpp"

#include <cstdio>

static int failed;

static void check(bool ok, const char* name) {
    if (ok) {
        std::printf("--- PASS: %s\n", name);
    } else {
        std::printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static bool rect_is(Rect r, int x, int y, int w, int h) {
    return r.x == x && r.y == y && r.w == w && r.h == h;
}

int main() {
    // ---------- construction & layout ----------
    {
        EditorCore ed("hello world foo", 80, 16);
        check(ed.text() == "hello world foo", "test_ctor_text");
        // 10 columns: "hello worl|d" overflows at 'd', breaks after "hello ".
        check(ed.row_count() == 2, "test_wrap_two_rows");
        check(ed.caret() == 0 && ed.sel_begin() == 0 && ed.sel_end() == 0,
              "test_initial_caret");
        check(ed.take_damage().empty(), "test_no_initial_damage");
    }
    {
        EditorCore ed("a\n", 80, 16);
        check(ed.row_count() == 2, "test_trailing_newline_final_row");
        EditorCore e2("", 80, 16);
        check(e2.row_count() == 1, "test_empty_doc_one_row");
        EditorCore e3("aaa\nbbb\nccc", 800, 16);
        check(e3.row_count() == 3, "test_multiline_rows");
    }

    // ---------- clicking ----------
    {
        EditorCore ed("hello world foo", 80, 16);   // rows {0,6} {6,15}
        ed.click(3, 0);
        check(ed.caret() == 0, "test_click_left_half");
        check(ed.take_damage().empty(), "test_unmoved_click_no_damage");
        ed.click(5, 0);
        check(ed.caret() == 1, "test_click_past_midpoint_advances");
        check(rect_is(ed.take_damage(), 0, 0, 80, 16),
              "test_caret_move_damage_band");
        ed.click(16, 20);
        check(ed.caret() == 8, "test_click_second_row");
        check(rect_is(ed.take_damage(), 0, 0, 80, 32),
              "test_cross_row_damage");
        ed.click(50, 500);
        check(ed.caret() == 12, "test_click_y_clamps_to_last_row");
        ed.click(-9, -50);
        check(ed.caret() == 0, "test_click_negative_clamps");
    }
    {
        EditorCore ed("hi\n", 80, 16);
        ed.click(50, 25);   // second row: the empty final line
        check(ed.caret() == 3, "test_click_empty_final_line");
    }

    // ---------- typing, grouping, undo/redo ----------
    {
        EditorCore ed("", 80, 16);
        ed.insert_text("h");
        ed.insert_text("i");
        ed.insert_text("!");
        check(ed.text() == "hi!" && ed.caret() == 3, "test_typing");
        ed.undo();
        check(ed.text() == "" && ed.caret() == 0, "test_undo_whole_run");
        ed.redo();
        check(ed.text() == "hi!" && ed.caret() == 3, "test_redo_whole_run");
        ed.undo();
        ed.insert_text("x");
        ed.redo();   // must be a no-op: redo history was cleared
        check(ed.text() == "x", "test_new_edit_clears_redo");
        ed.undo();
        ed.undo();   // empty stack: silent no-op
        check(ed.text() == "" && ed.caret() == 0, "test_undo_empty_noop");
    }
    {   // Caret movement splits typing groups.
        EditorCore ed("", 80, 16);
        ed.insert_text("ab");
        ed.left(false);
        ed.insert_text("c");
        check(ed.text() == "acb" && ed.caret() == 2, "test_insert_mid");
        ed.undo();
        check(ed.text() == "ab" && ed.caret() == 1, "test_move_splits_group");
        ed.undo();
        check(ed.text() == "", "test_second_group_undone");
    }

    // ---------- selection: drag, replace, undo caret ----------
    {
        EditorCore ed("abcdef", 80, 16);
        ed.click(0, 0);
        ed.drag(24, 0);
        check(ed.sel_begin() == 0 && ed.sel_end() == 3 && ed.caret() == 3,
              "test_drag_selects");
        ed.insert_text("Z");
        check(ed.text() == "Zdef" && ed.caret() == 1 &&
                  ed.sel_begin() == ed.sel_end(),
              "test_typing_replaces_selection");
        ed.insert_text("!");
        ed.undo();
        check(ed.text() == "Zdef", "test_replacement_not_merged");
        ed.undo();
        check(ed.text() == "abcdef" && ed.caret() == 3,
              "test_undo_restores_replaced_text");
    }

    // ---------- shift-arrows ----------
    {
        EditorCore ed("abcdef", 80, 16);
        ed.click(17, 0);
        check(ed.caret() == 2, "test_click_for_arrows");
        ed.right(true);
        ed.right(true);
        check(ed.sel_begin() == 2 && ed.sel_end() == 4 && ed.caret() == 4,
              "test_shift_right_extends");
        ed.left(false);
        check(ed.caret() == 2 && ed.sel_begin() == ed.sel_end(),
              "test_left_collapses_to_begin");
        ed.right(false);
        check(ed.caret() == 3, "test_plain_right_moves");
        ed.left(true);
        check(ed.sel_begin() == 2 && ed.sel_end() == 3 && ed.caret() == 2,
              "test_shift_left_extends_back");
    }

    // ---------- UTF-8 aware editing ----------
    {
        EditorCore ed("caf\xc3\xa9!", 80, 16);   // "café!" — é is 2 bytes
        ed.end_key(false);
        check(ed.caret() == 6, "test_end_key");
        ed.left(false);
        ed.left(false);
        check(ed.caret() == 3, "test_left_steps_over_codepoint");
        ed.key_delete();
        check(ed.text() == "caf!" && ed.caret() == 3,
              "test_delete_whole_codepoint");
        ed.undo();
        check(ed.text() == "caf\xc3\xa9!" && ed.caret() == 5,
              "test_undo_delete_caret_after_restored");
        ed.backspace();
        check(ed.text() == "caf!" && ed.caret() == 3,
              "test_backspace_whole_codepoint");
    }

    // ---------- home/end on logical lines ----------
    {
        EditorCore ed("one two\nthree", 800, 16);
        ed.click(0, 20);
        check(ed.caret() == 8, "test_click_line_two");
        ed.end_key(true);
        check(ed.sel_begin() == 8 && ed.sel_end() == 13,
              "test_shift_end_selects_to_eol");
        ed.home(false);
        check(ed.caret() == 8 && ed.sel_begin() == ed.sel_end(),
              "test_home_collapses");
        ed.left(false);
        check(ed.caret() == 7, "test_left_crosses_newline");
        ed.end_key(false);
        check(ed.caret() == 7, "test_end_stops_before_newline");
    }

    // ---------- double-click ----------
    {
        EditorCore ed("foo bar_baz qux", 800, 16);
        ed.double_click(33, 0);   // inside "bar_baz"
        check(ed.sel_begin() == 4 && ed.sel_end() == 11 && ed.caret() == 11,
              "test_double_click_word");
        ed.double_click(25, 0);   // on the gap
        check(ed.sel_begin() == 3 && ed.sel_end() == 4,
              "test_double_click_gap");
    }

    // ---------- selection delete ----------
    {
        EditorCore ed("hello world", 800, 16);
        ed.click(0, 0);
        ed.drag(45, 0);   // "hello " selected
        check(ed.sel_begin() == 0 && ed.sel_end() == 6, "test_drag_six");
        ed.key_delete();
        check(ed.text() == "world" && ed.caret() == 0,
              "test_delete_selection");
        ed.undo();
        check(ed.text() == "hello world" && ed.caret() == 6,
              "test_undo_selection_delete");
    }

    // ---------- damage from edits ----------
    {
        EditorCore ed("aaa\nbbb\nccc", 800, 16);
        ed.click(0, 18);   // caret to start of line 1
        check(rect_is(ed.take_damage(), 0, 0, 800, 32),
              "test_click_damage");
        ed.insert_text("X");
        check(rect_is(ed.take_damage(), 0, 16, 800, 32),
              "test_edit_damages_line_to_bottom");
        ed.insert_text("\n");
        check(ed.row_count() == 4, "test_newline_adds_row");
        check(rect_is(ed.take_damage(), 0, 16, 800, 48),
              "test_newline_damage_extends");
        ed.backspace();
        check(ed.row_count() == 3, "test_join_removes_row");
        check(rect_is(ed.take_damage(), 0, 16, 800, 48),
              "test_join_damage_covers_old_bottom");
    }
    {   // Damage accumulates between takes.
        EditorCore ed("aaa\nbbb", 800, 16);
        ed.click(0, 18);
        ed.insert_text("Z");
        check(rect_is(ed.take_damage(), 0, 0, 800, 32),
              "test_damage_accumulates");
        check(ed.take_damage().empty(), "test_take_resets");
    }

    // ---------- reflow on edit ----------
    {
        EditorCore ed("aaaa bbbb cccc", 80, 16);
        check(ed.row_count() == 2, "test_reflow_initial");
        ed.end_key(false);
        ed.insert_text(" dddd eeee");
        check(ed.row_count() == 3, "test_edit_reflows");
        check(ed.caret() == 24, "test_caret_after_growth");
    }

    // ---------- edge no-ops ----------
    {
        EditorCore ed("", 80, 16);
        ed.backspace();
        ed.key_delete();
        ed.left(false);
        ed.right(false);
        check(ed.text() == "" && ed.caret() == 0, "test_edge_noops");
        check(ed.take_damage().empty(), "test_noops_no_damage");
        ed.undo();
        ed.redo();
        check(ed.take_damage().empty(), "test_empty_history_no_damage");
    }

    return failed;
}
```
