---
course: terminal-editor
title: Build a Terminal Text Editor
language: cpp
description: >
  Build a vi-like text editor from raw bytes up: put the terminal in raw
  mode (and restore it with RAII), decode escape sequences into a Key
  variant, paint flicker-free frames, then grow a real editor core — gap
  buffer, viewport, search, modal editing with motions and operators,
  undo with coalescing, and a syntax-highlighting state machine. Every
  challenge is a pure, testable piece of the editor you assemble.
duration_hours: 20
tags: [cpp, systems, terminal, editors]
extended_reading:
  - title: "VT100 User Guide, Chapter 3: Programmer Information"
    url: https://vt100.net/docs/vt100-ug/chapter3.html
  - title: "XTerm Control Sequences (the ctlseqs document)"
    url: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html
  - title: "termios(3) — Linux manual page"
    url: https://man7.org/linux/man-pages/man3/termios.3.html
  - title: "antirez's kilo — a text editor in ~1000 lines of C"
    url: https://github.com/antirez/kilo
  - title: "Build Your Own Text Editor (the kilo walkthrough)"
    url: https://viewsourcecode.org/snaptoken/kilo/
  - title: "Data Structures for Text Sequences (Crowley) — gap buffers, piece tables, and friends"
    url: https://www.cs.unm.edu/~crowley/papers/sds.pdf
  - title: "The Craft of Text Editing (Finseth) — the Emacs-lineage classic"
    url: https://www.finseth.com/craft/
---

# Lesson: The Terminal Is a File {#the-terminal-is-a-file}

Run a program in a terminal and three file descriptors are already open:
0 (stdin), 1 (stdout), 2 (stderr). Usually all three point at the same
kernel object — a **tty device**. `isatty(0)` asks whether fd 0 is one:

```cpp
#include <unistd.h>

if (!isatty(STDIN_FILENO)) {
    // stdin is a pipe or a file — an interactive editor can't run here.
}
```

The name is a fossil: **tty** = teletype, the electromechanical printing
terminals of the 1960s. The terminal "screen" your editor will draw on is,
as far as the kernel is concerned, a serial device you `write(2)` bytes to
and `read(2)` bytes from. There is no "move the cursor" system call, no
"key event" structure. Bytes in, bytes out. Everything a full-screen
program does — vim, htop, less — is built on exactly two tricks:

- **Output**: certain byte sequences, when written to the terminal, are
  interpreted as commands (move cursor, clear line, change color) instead
  of text. Lesson 3 is about those.
- **Input**: keys arrive as bytes, and you reconfigure the tty so they
  arrive *immediately* and *unmodified*. That is this lesson.

### Canonical mode: the line discipline is editing before you are

By default a tty is in **canonical mode**. The kernel's *line discipline*
— a layer between the device and your process — buffers input until the
user presses Enter, and implements its own primitive line editor:
backspace erases, Ctrl-U kills the line, and only the finished line is
delivered to `read()`. That's why a plain `std::getline` program feels
"line based": the kernel is doing the editing, not the C++ runtime.

The line discipline also *echoes* every typed character back to the
screen, translates carriage returns to newlines, and turns certain bytes
into **signals**: Ctrl-C sends SIGINT, Ctrl-Z sends SIGTSTP, Ctrl-\ sends
SIGQUIT. Useful defaults for a shell session; fatal for an editor. If the
kernel echoes keystrokes, every `j` the user types to move down gets
printed into your carefully painted screen. If Ctrl-C kills you, you can't
bind it. An editor needs the terminal in **raw mode**: every byte
delivered as typed, nothing echoed, nothing translated, no signals.

### termios, flag by flag

The tty's configuration lives in a `struct termios` (POSIX,
`<termios.h>`), read with `tcgetattr(fd, &t)` and written with
`tcsetattr(fd, TCSAFLUSH, &t)`. It has four flag words — input (`c_iflag`),
output (`c_oflag`), control (`c_cflag`), local (`c_lflag`) — plus an array
`c_cc` of control characters and thresholds. "Raw mode" is not a single
switch (well, non-portably there is `cfmakeraw`); it is a specific set of
flags to clear, and knowing *why* each one matters is knowing what the
terminal actually does for you:

- **`ECHO`** (local): kernel echoes input back to the display. Off — the
  editor decides what appears on screen.
- **`ICANON`** (local): canonical (line-buffered) mode. Off — `read()`
  returns bytes as they are typed, not lines.
- **`ISIG`** (local): Ctrl-C → SIGINT, Ctrl-Z → SIGTSTP. Off — those bytes
  (0x03, 0x1A) are delivered to you like any other key, so vi-style
  bindings can use them.
- **`IXON`** (input): software flow control — Ctrl-S freezes output until
  Ctrl-Q resumes it. A gift to 1970s serial links, a trap today (everyone
  has "frozen" a terminal with a stray Ctrl-S). Off, so Ctrl-S is just a
  byte.
- **`IEXTEN`** (local): extended input processing — on many systems Ctrl-V
  means "quote the next character" (and on macOS Ctrl-O is swallowed).
  Off.
- **`ICRNL`** (input): translate carriage return (0x0D, what the Enter key
  actually sends) into newline (0x0A). Off — you want to see the real
  byte, and to tell Enter (0x0D) apart from Ctrl-J (0x0A).
- **`OPOST`** (output): output post-processing, in practice `\n` →
  `\r\n` translation. Off — from now on, when you want the cursor at the
  start of the next line you write `"\r\n"` yourself. (Forget this and
  your output staircases across the screen.)
- **`BRKINT`, `INPCK`, `ISTRIP`** (input): break-condition SIGINT, parity
  checking, and stripping the 8th bit. All relics of real serial hardware;
  all off, both for tradition and because `ISTRIP` would mangle UTF-8.
- **`CS8`** (control): a two-bit field, *set* rather than cleared — 8-bit
  characters. Almost certainly already set.

Two entries of `c_cc` control when `read()` returns in non-canonical
mode, and they are worth internalizing because they define your event
loop's personality:

- **`VMIN`** — minimum bytes before `read()` may return.
- **`VTIME`** — read timeout in tenths of a second.

`VMIN=1, VTIME=0` makes `read()` block until at least one byte arrives —
a pure event-driven loop. `VMIN=0, VTIME=1` makes `read()` return after
at most 100 ms *even with no input* — a polling loop that gives you a
natural place to handle "no key pressed" work (like noticing the window
was resized, or timing out a lone ESC — a plot point in the next lesson).
We'll use `VMIN=0, VTIME=1`.

In real code, entering raw mode looks like this — and note that we
*modify a copy* of the original settings rather than building a termios
from zero, because the struct has fields we don't understand and must not
clobber:

```cpp
#include <termios.h>
#include <unistd.h>

termios orig;                       // saved so we can restore at exit
void enter_raw() {
    tcgetattr(STDIN_FILENO, &orig);
    termios raw = orig;             // copy, then surgically edit
    raw.c_iflag &= ~(BRKINT | ICRNL | INPCK | ISTRIP | IXON);
    raw.c_oflag &= ~(OPOST);
    raw.c_cflag |= CS8;
    raw.c_lflag &= ~(ECHO | ICANON | IEXTEN | ISIG);
    raw.c_cc[VMIN] = 0;
    raw.c_cc[VTIME] = 1;
    tcsetattr(STDIN_FILENO, TCSAFLUSH, &raw);
}
```

`TCSAFLUSH` applies the change after pending output drains and discards
unread input — so a half-typed line from before raw mode doesn't leak
into the editor as phantom keystrokes.

### Leaving the terminal the way you found it

Exit your editor without restoring the original termios and the user's
shell inherits raw mode: nothing echoes, Enter doesn't work, and they're
typing `reset` blind. Restoration must happen on *every* exit path —
normal quit, early return, exception. C++ has a dedicated idiom for
"must happen on every exit path": **RAII** (Resource Acquisition Is
Initialization). Acquire in a constructor, release in the destructor, and
let scope exit — however it happens — do the bookkeeping:

```cpp
class RawMode {
public:
    RawMode() {
        tcgetattr(STDIN_FILENO, &orig_);
        termios raw = orig_;
        make_raw(raw);                       // this lesson's challenge
        tcsetattr(STDIN_FILENO, TCSAFLUSH, &raw);
    }
    ~RawMode() { tcsetattr(STDIN_FILENO, TCSAFLUSH, &orig_); }

    RawMode(const RawMode&) = delete;             // copying makes no sense:
    RawMode& operator=(const RawMode&) = delete;  // who restores, and when?

private:
    termios orig_;
};

int main() {
    RawMode raw;          // terminal is raw from here on
    run_editor();         // may return early, may throw
}                         // ...restored here, no matter what
```

A raw-mode guard is the canonical RAII example for a reason: the resource
isn't memory, it's *terminal state*, and leaking it hurts a human
immediately. Two design points worth dwelling on:

- **Copying is deleted.** If two `RawMode` objects existed, the second's
  constructor would save an `orig_` that is *already raw* — restoring it
  would "restore" rawness. Deleting the copy operations makes the broken
  program not compile. This is the **rule of five** in action: since we
  wrote a destructor with side effects, we must decide what copy/move
  mean, and here the right answer is "they don't".
- The flip side is the **rule of zero**: classes that *don't* own a
  resource should define none of the five special members and let the
  compiler-generated ones work. Most classes you write in this course —
  positions, ranges, buffers built on `std::string` — follow the rule of
  zero. Only resource guards need the full ceremony.

### The exit paths RAII cannot catch

Destructors run on returns and exceptions — but not on `std::abort`, not
on `_exit`, and not when a **signal** kills the process. Two signals need
explicit handling in a real editor, and both are pure terminal lore worth
knowing:

- **`SIGTSTP` / `SIGCONT`** (Ctrl-Z suspend, `fg` resume): with `ISIG`
  off you won't get Ctrl-Z from the keyboard, but `kill -TSTP` can still
  arrive. The polite dance: on SIGTSTP, restore the original termios,
  then re-raise SIGTSTP with default disposition so the shell actually
  suspends you; on SIGCONT, re-enter raw mode and repaint the whole
  screen (the shell owned the display while you slept).
- **Crash safety**: register the restore with `atexit`, and keep signal
  handler paths async-signal-safe — `tcsetattr` and `write` are on the
  safe list; `std::cout` and `malloc` are not.

### The grader has no tty (and that's a feature)

Everything above is glue you'll assemble in your own editor binary and
verify by running it in a real terminal. The grader, though, runs your
code headless — no tty at all — which enforces a discipline this whole
course is built on: **separate the OS glue from the logic, and make the
logic pure.** The first challenge tests your raw-mode flag surgery
against a stand-in `Termios` struct with the same field names and (Linux)
flag values as the real one — swap `Termios` for `termios` and the
identical function body drops into your editor. The second distills the
RAII guard into a reusable, tty-free scope guard.

## Challenge: Raw Mode, Flag by Flag {#raw-mode-flags points=10}

Implement `make_raw`, which edits a `Termios` in place to configure raw
mode exactly as specified:

- **Clear** in `c_lflag`: `kECHO`, `kICANON`, `kISIG`, `kIEXTEN`.
- **Clear** in `c_iflag`: `kBRKINT`, `kICRNL`, `kINPCK`, `kISTRIP`,
  `kIXON`.
- **Clear** in `c_oflag`: `kOPOST`.
- **Set** in `c_cflag`: `kCS8` (set the whole two-bit field).
- Set `c_cc[kVMIN] = 0` and `c_cc[kVTIME] = 1`.

Every bit you were not told to touch must survive unchanged — the tests
plant unrelated bits in every flag word and check they're still there.
This mirrors the real API contract: `termios` carries settings you don't
own, so you edit, never overwrite.

### Starter

```cpp
#include <cstdint>
#include <cstddef>

// A stand-in for struct termios so the grader can run without a tty.
// Field names and flag values match Linux <termios.h>; in your editor,
// the same function body works on the real struct.
struct Termios {
    std::uint32_t c_iflag = 0;
    std::uint32_t c_oflag = 0;
    std::uint32_t c_cflag = 0;
    std::uint32_t c_lflag = 0;
    std::uint8_t  c_cc[32] = {};
};

// Input flags (c_iflag)
inline constexpr std::uint32_t kBRKINT = 0x0002;
inline constexpr std::uint32_t kINPCK  = 0x0010;
inline constexpr std::uint32_t kISTRIP = 0x0020;
inline constexpr std::uint32_t kICRNL  = 0x0100;
inline constexpr std::uint32_t kIXON   = 0x0400;
// Output flags (c_oflag)
inline constexpr std::uint32_t kOPOST  = 0x0001;
// Control flags (c_cflag)
inline constexpr std::uint32_t kCS8    = 0x0030;
// Local flags (c_lflag)
inline constexpr std::uint32_t kISIG   = 0x0001;
inline constexpr std::uint32_t kICANON = 0x0002;
inline constexpr std::uint32_t kECHO   = 0x0008;
inline constexpr std::uint32_t kIEXTEN = 0x8000;
// c_cc indexes
inline constexpr std::size_t kVTIME = 5;
inline constexpr std::size_t kVMIN  = 6;

void make_raw(Termios& t) {
    // TODO: clear the local/input/output flags listed in the prompt,
    // set kCS8 in c_cflag, and set VMIN=0, VTIME=1.
    (void)t;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    {
        Termios t;
        t.c_lflag = kECHO | kICANON | kISIG | kIEXTEN;
        make_raw(t);
        check(t.c_lflag == 0, "test_clears_all_four_local_flags");
    }
    {
        Termios t;
        t.c_iflag = kBRKINT | kICRNL | kINPCK | kISTRIP | kIXON;
        make_raw(t);
        check(t.c_iflag == 0, "test_clears_all_five_input_flags");
    }
    {
        Termios t;
        t.c_oflag = kOPOST;
        make_raw(t);
        check(t.c_oflag == 0, "test_clears_opost");
    }
    {
        Termios t;
        t.c_cflag = 0;
        make_raw(t);
        check((t.c_cflag & kCS8) == kCS8, "test_sets_cs8");
    }
    {
        Termios t;
        make_raw(t);
        check(t.c_cc[kVMIN] == 0 && t.c_cc[kVTIME] == 1,
              "test_vmin_0_vtime_1");
    }
    {
        // Bits we were NOT told to touch must survive.
        Termios t;
        t.c_iflag = 0x1000 | kIXON;   // 0x1000 is not ours to clear
        t.c_oflag = 0x0004 | kOPOST;  // ONLCR-ish bit stays
        t.c_lflag = 0x0100 | kECHO;   // e.g. ECHOCTL stays
        t.c_cflag = 0x0800;           // e.g. PARODD stays
        t.c_cc[0] = 3;                // VINTR untouched
        make_raw(t);
        check(t.c_iflag == 0x1000 && t.c_oflag == 0x0004 &&
              t.c_lflag == 0x0100 &&
              (t.c_cflag & 0x0800) == 0x0800 && t.c_cc[0] == 3,
              "test_preserves_unrelated_bits");
    }
    {
        // Calling twice must be idempotent.
        Termios t;
        t.c_lflag = kECHO | kICANON | kISIG | kIEXTEN | 0x0100;
        make_raw(t);
        make_raw(t);
        check(t.c_lflag == 0x0100 && t.c_cc[kVTIME] == 1,
              "test_idempotent");
    }
    return failed;
}
```

## Challenge: A Scope Guard {#scoped-guard points=10}

Implement `ScopedAction`: it stores a callable at construction and
invokes it exactly once when the guard dies — unless disarmed. This is
the lesson's `RawMode` with the terminal dependency injected, and it's
the shape of every "undo this on scope exit" problem you'll meet later
(leave the alternate screen, re-show the cursor, restore termios before
suspending).

Unlike `RawMode`, this guard is **movable**: a factory function like
`ScopedAction enter_raw_mode()` must be able to return the guard to its
caller, handing off responsibility for the cleanup. That makes it a
worked example of the rule of five: destructor, deleted copies, and move
operations that keep the invariant *the action runs exactly once*.

Semantics to implement:

- `ScopedAction(std::function<void()> f)` stores `f`; the destructor
  invokes it if the guard is still **armed**.
- `release()` disarms: after it, the destructor does nothing.
- **Move constructor**: transfers the action; the moved-from guard is
  disarmed (the action must not run twice).
- **Move assignment**: the target first runs its own pending action (it
  is being destroyed, morally), then takes over the source's; the source
  is disarmed. Self-assignment is a no-op.
- Copying is deleted — two owners of one cleanup is the bug this class
  exists to prevent.
- `armed()` reports whether the destructor would fire.

### Starter

```cpp
#include <functional>
#include <utility>

class ScopedAction {
public:
    ScopedAction() = default;
    explicit ScopedAction(std::function<void()> f) : action_(std::move(f)) {}

    ~ScopedAction() {
        // TODO: run action_ if armed.
    }

    ScopedAction(const ScopedAction&) = delete;
    ScopedAction& operator=(const ScopedAction&) = delete;

    ScopedAction(ScopedAction&& other) noexcept {
        // TODO: steal other's action; disarm other.
        (void)other;
    }

    ScopedAction& operator=(ScopedAction&& other) noexcept {
        // TODO: run own pending action, then take other's; disarm other.
        // Handle self-assignment.
        (void)other;
        return *this;
    }

    void release() {
        // TODO: disarm without running.
    }

    bool armed() const {
        return false; // TODO
    }

private:
    std::function<void()> action_; // empty == disarmed
};
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <stdexcept>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    {
        int runs = 0;
        {
            ScopedAction g([&] { runs++; });
            check(g.armed(), "test_armed_after_construction");
            check(runs == 0, "test_not_run_early");
        }
        check(runs == 1, "test_runs_once_on_scope_exit");
    }
    {
        int runs = 0;
        try {
            ScopedAction g([&] { runs++; });
            throw std::runtime_error("boom");
        } catch (const std::runtime_error&) {
        }
        check(runs == 1, "test_runs_during_stack_unwinding");
    }
    {
        int runs = 0;
        {
            ScopedAction g([&] { runs++; });
            g.release();
            check(!g.armed(), "test_release_disarms");
        }
        check(runs == 0, "test_released_guard_does_not_run");
    }
    {
        int runs = 0;
        {
            ScopedAction a([&] { runs++; });
            ScopedAction b(std::move(a));
            check(!a.armed(), "test_moved_from_is_disarmed");
            check(b.armed(), "test_move_target_is_armed");
        }
        check(runs == 1, "test_action_runs_once_after_move");
    }
    {
        int first = 0, second = 0;
        {
            ScopedAction a([&] { first++; });
            ScopedAction b([&] { second++; });
            a = std::move(b);
            check(first == 1,
                  "test_move_assign_runs_targets_pending_action");
            check(second == 0 && a.armed() && !b.armed(),
                  "test_move_assign_transfers_ownership");
        }
        check(second == 1, "test_transferred_action_runs_at_exit");
    }
    {
        int runs = 0;
        {
            ScopedAction a([&] { runs++; });
            ScopedAction* p = &a;   // launder self-move past -Wself-move
            a = std::move(*p);
            check(runs == 0 && a.armed(), "test_self_move_assign_is_noop");
        }
        check(runs == 1, "test_self_move_still_runs_once");
    }
    {
        ScopedAction g;
        check(!g.armed(), "test_default_constructed_is_disarmed");
    }
    return failed;
}
```

# Lesson: Bytes In — Decoding the Keyboard {#decoding-the-keyboard}

With the terminal raw, input is a byte stream and nothing more. The read
loop at the bottom of every terminal editor looks like this:

```cpp
char c;
ssize_t n = read(STDIN_FILENO, &c, 1);
// n == 1: got a byte.  n == 0: VTIME expired with no input (our
// VMIN=0/VTIME=1 config) — a "tick" you can use for housekeeping.
// n == -1 with errno == EINTR: a signal interrupted the read; retry.
```

What arrives in `c`? For the letter keys, exactly what you'd hope: `j` is
0x6A. The interesting keys are everything else, and their encodings are
archaeology you have to know to write a decoder.

### Control characters: the 5-bit connection

Hold Ctrl and press a letter, and the terminal sends the letter's ASCII
code **with bits 6 and 5 cleared**: Ctrl-A is 0x01, Ctrl-B is 0x02, …,
Ctrl-Z is 0x1A. That's not a lookup table, it's a circuit: on a teletype
the Ctrl key literally grounded two bit lines. It survives in ASCII's
layout — `'A'` is 0x41, and `0x41 & 0x1F == 0x01`. The same masking
explains some familiar aliases:

- **Ctrl-I is Tab** (0x09), **Ctrl-M is Enter** (0x0D, carriage return —
  with `ICRNL` off you finally see the real CR), **Ctrl-J is newline**
  (0x0A), **Ctrl-[ is Escape** (0x1B — `'[' & 0x1F`). Old-school vi users
  really do type Ctrl-[ instead of reaching for Esc.
- **Backspace** is its own mess: modern terminals send **DEL, 0x7F** for
  the Backspace key, while 0x08 (Ctrl-H, the ASCII "backspace" character)
  arrives if someone types Ctrl-H — historically the same editing key, so
  editors treat both as backspace.

### Escape sequences: why ESC [ ?

Arrow keys, Home, End, PageUp, Delete — none of these have an ASCII code.
When you press ↑, the terminal sends **three bytes**: `ESC [ A` (0x1B,
0x5B, 0x41). Why that shape? In the late 1970s DEC's VT100 adopted the
ANSI X3.64 standard for terminal control: commands are introduced by
ESC + `[` — the **CSI**, Control Sequence Introducer — followed by
optional numeric parameters and a final letter that names the command.
The VT100 was so successful that its sequences became the lingua franca;
"ANSI escape codes" and xterm's `ctlseqs` document descend directly from
it. Your terminal emulator in 2026 is, protocol-wise, imitating a 1978
DEC terminal. The same CSI grammar drives *output* (next lesson you'll
write `ESC [ 2 J` to clear the screen); on *input* the terminal uses it
to encode special keys:

```
ESC [ A / B / C / D      arrows up / down / right / left
ESC [ H   and  ESC [ F   Home and End (one common encoding)
ESC [ 1 ~  or  ESC [ 7 ~ Home (other terminals' encoding)
ESC [ 4 ~  or  ESC [ 8 ~ End
ESC [ 3 ~                Delete
ESC [ 5 ~ / ESC [ 6 ~    PageUp / PageDown
ESC O A ... ESC O F      arrows/Home/End again — SS3 form, sent by
                         terminals in "application keypad" mode
```

Yes: three different encodings for Home, from different terminal
lineages, all still in the wild. A robust decoder accepts all of them.
(The `~`-form numbers come from the VT220's function-key scheme; the
`ESC O` prefix is SS3, "single shift 3", from the VT100's application
keypad.) The `ctlseqs` document in the extended reading is the closest
thing to a complete map.

There's one genuinely nasty ambiguity: the user pressing the **Esc key**
sends a lone 0x1B — the same byte that *starts* every sequence. The only
way to tell "Esc" from "the first byte of ESC [ A" is timing: after an
ESC, if more bytes are already buffered (or arrive within a few
milliseconds), it's a sequence; if the stream goes quiet, it was the Esc
key. Our `VMIN=0, VTIME=1` setting gives exactly that: after reading an
ESC, one more `read()` either returns the next byte of a sequence or
times out (~100 ms) and returns 0 — verdict: lone Esc. This is why
Esc in terminal vim can feel hesitant over a laggy ssh connection: the
editor is waiting to see whether more bytes follow.

### A Key type: sum types over flag soup

The decoder's output should say *which key*, in one honest type. The C
tradition is an int with magic values (kilo uses `enum { ARROW_LEFT = 1000,
... }` above char range). Modern C++ has a better tool — the **sum
type**:

```cpp
using Key = std::variant<char, CtrlKey, SpecialKey>;
```

A `Key` is *exactly one of*: a printable character, a Ctrl-chord, or a
special key — and the compiler knows which. `std::holds_alternative<char>(k)`
asks; `std::get<char>(k)` extracts (throwing if you're wrong, unlike a
union silently misreading); and `std::visit` dispatches over all cases,
failing to compile if you forget one. `enum class` (rather than plain
`enum`) keeps `SpecialKey::Delete` scoped — no bare `Delete` leaking into
the global namespace, no accidental conversion to int. The `CtrlKey`
struct gets `bool operator==(const CtrlKey&) const = default` — C++20's
defaulted comparisons — because `std::variant`'s own `==` requires each
alternative to be comparable; one defaulted line and `Key{CtrlKey{'q'}}
== key` just works in tests and in your keymap.

Two challenges: first the single-byte classifier, then the full
escape-sequence state machine. Together they are `read_key()`, the
function your editor's main loop calls once per keystroke; here they're
fed byte strings so they can be tested without a terminal.

## Challenge: One Byte, One Key {#classify-byte points=10}

Implement `decode_byte`, mapping a single input byte to a `Key`:

- `0x1B` → `SpecialKey::Escape`; `0x0D` **and** `0x0A` →
  `SpecialKey::Enter` (CR is what Enter sends in raw mode; LF is Ctrl-J,
  which vi also treats as Enter); `0x09` → `SpecialKey::Tab`; `0x7F`
  **and** `0x08` → `SpecialKey::Backspace` (DEL from the Backspace key,
  Ctrl-H from tradition).
- Remaining bytes `0x01..0x1A` → `CtrlKey` with the lowercase letter:
  `0x01` → `{'a'}`, `0x1A` → `{'z'}`.
- Everything else — printable ASCII, and bytes ≥ 0x80 (UTF-8
  continuation bytes pass through untouched; the buffer stores raw
  bytes) — → `char` (cast the byte).

Note the parameter is `std::uint8_t`, not `char`: whether `char` is
signed is platform-dependent, and a signed 0xE9 is −23 — the same bug the
hashmap course meets, and the reason careful byte code says "byte" with
an unsigned type.

### Starter

```cpp
#include <cstdint>
#include <variant>

enum class SpecialKey {
    ArrowUp, ArrowDown, ArrowLeft, ArrowRight,
    Home, End, PageUp, PageDown,
    Delete, Backspace, Enter, Escape, Tab,
};

struct CtrlKey {
    char letter; // 'a'..'z'
    bool operator==(const CtrlKey&) const = default;
};

using Key = std::variant<char, CtrlKey, SpecialKey>;

Key decode_byte(std::uint8_t b) {
    // TODO: specials first, then Ctrl range, then plain char.
    (void)b;
    return Key{'\0'};
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    check(decode_byte('j') == Key{'j'}, "test_printable_letter");
    check(decode_byte(' ') == Key{' '}, "test_space_is_a_char");
    check(decode_byte('~') == Key{'~'}, "test_tilde_is_a_char");

    check(decode_byte(0x1B) == Key{SpecialKey::Escape}, "test_escape");
    check(decode_byte(0x0D) == Key{SpecialKey::Enter}, "test_cr_is_enter");
    check(decode_byte(0x0A) == Key{SpecialKey::Enter}, "test_lf_is_enter");
    check(decode_byte(0x09) == Key{SpecialKey::Tab}, "test_tab");
    check(decode_byte(0x7F) == Key{SpecialKey::Backspace},
          "test_del_is_backspace");
    check(decode_byte(0x08) == Key{SpecialKey::Backspace},
          "test_ctrl_h_is_backspace");

    check(decode_byte(0x01) == Key{CtrlKey{'a'}}, "test_ctrl_a");
    check(decode_byte(0x11) == Key{CtrlKey{'q'}}, "test_ctrl_q");
    check(decode_byte(0x1A) == Key{CtrlKey{'z'}}, "test_ctrl_z");
    check(decode_byte(0x13) == Key{CtrlKey{'s'}},
          "test_ctrl_s_reaches_us_with_ixon_off");

    check(decode_byte(0xE9) == Key{static_cast<char>(0xE9)},
          "test_high_byte_passes_through");
    check(decode_byte(0x00) == Key{'\0'}, "test_nul_is_a_char");
    check(decode_byte(0x1C) == Key{static_cast<char>(0x1C)},
          "test_0x1c_not_a_ctrl_letter");
    return failed;
}
```

## Challenge: The Escape-Sequence Decoder {#escape-decoder points=15}

Implement `decode_input`, which consumes a whole byte string (as read
from the tty) and produces the decoded keys in order. In your editor this
runs incrementally over the read buffer; given a complete capture it must
produce exactly the keys a terminal user typed.

The grammar, in the order you should check it:

- A byte other than `0x1B` decodes via `decode_byte` from the previous
  challenge (provided again in the starter).
- `0x1B` at the **end of input** → `SpecialKey::Escape` (the lone-Esc
  timeout case).
- `0x1B` followed by `[` — a CSI sequence. Collect the parameter bytes
  (everything up to, not including, the **final byte**, the first byte
  in the range `0x40..0x7E`). Then:
  - no parameters and final `A`/`B`/`C`/`D` → ArrowUp/Down/Right/Left
    (note: C is right, D is left);
  - no parameters and final `H` / `F` → Home / End;
  - all-digit parameters and final `~`: `1` or `7` → Home, `4` or `8` →
    End, `3` → Delete, `5` → PageUp, `6` → PageDown; any other number →
    produce nothing (an unrecognized key — swallow it, don't corrupt the
    stream);
  - anything else (a `;` in the params, an unknown final byte) → produce
    nothing for the whole sequence;
  - input ends before a final byte arrives → produce nothing (a real
    editor would wait for more bytes).
- `0x1B` followed by `O` — an SS3 sequence: one more byte, `A`–`D`, `H`,
  `F`, mapped exactly as CSI's letter finals; anything else (or end of
  input) → produce nothing.
- `0x1B` followed by any other byte → `SpecialKey::Escape`, then decode
  that byte normally (the user pressed Esc, then typed).

Swallowing unknown sequences whole is the important robustness property:
if a terminal sends `ESC [ 1 ; 5 C` (Ctrl-Right — the `;5` is a modifier
parameter) and you only ate the `ESC [ 1`, the stray `; 5 C` would type
"; 5 C" into the buffer. Real bug, real editors have shipped it.

### Starter

```cpp
#include <cstdint>
#include <string_view>
#include <variant>
#include <vector>

enum class SpecialKey {
    ArrowUp, ArrowDown, ArrowLeft, ArrowRight,
    Home, End, PageUp, PageDown,
    Delete, Backspace, Enter, Escape, Tab,
};

struct CtrlKey {
    char letter; // 'a'..'z'
    bool operator==(const CtrlKey&) const = default;
};

using Key = std::variant<char, CtrlKey, SpecialKey>;

// From the previous challenge, provided.
Key decode_byte(std::uint8_t b) {
    switch (b) {
        case 0x1B: return SpecialKey::Escape;
        case 0x0D: case 0x0A: return SpecialKey::Enter;
        case 0x09: return SpecialKey::Tab;
        case 0x7F: case 0x08: return SpecialKey::Backspace;
        default: break;
    }
    if (b >= 0x01 && b <= 0x1A)
        return CtrlKey{static_cast<char>('a' + b - 1)};
    return static_cast<char>(b);
}

std::vector<Key> decode_input(std::string_view bytes) {
    // TODO: walk the bytes; on 0x1B, branch on '[' (CSI), 'O' (SS3),
    // end-of-input (lone Esc), or anything else (Esc then that byte).
    (void)bytes;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

static bool eq(std::string_view in, const std::vector<Key>& want) {
    return decode_input(in) == want;
}

int main() {
    check(eq("jk", {Key{'j'}, Key{'k'}}), "test_plain_bytes");
    check(eq("", {}), "test_empty_input");

    check(eq("\x1b[A", {Key{SpecialKey::ArrowUp}}), "test_arrow_up");
    check(eq("\x1b[B", {Key{SpecialKey::ArrowDown}}), "test_arrow_down");
    check(eq("\x1b[C", {Key{SpecialKey::ArrowRight}}), "test_arrow_right");
    check(eq("\x1b[D", {Key{SpecialKey::ArrowLeft}}), "test_arrow_left");
    check(eq("\x1b[H", {Key{SpecialKey::Home}}), "test_home_letter_form");
    check(eq("\x1b[F", {Key{SpecialKey::End}}), "test_end_letter_form");

    check(eq("\x1b[1~", {Key{SpecialKey::Home}}), "test_home_tilde_1");
    check(eq("\x1b[7~", {Key{SpecialKey::Home}}), "test_home_tilde_7");
    check(eq("\x1b[4~", {Key{SpecialKey::End}}), "test_end_tilde_4");
    check(eq("\x1b[8~", {Key{SpecialKey::End}}), "test_end_tilde_8");
    check(eq("\x1b[3~", {Key{SpecialKey::Delete}}), "test_delete");
    check(eq("\x1b[5~", {Key{SpecialKey::PageUp}}), "test_page_up");
    check(eq("\x1b[6~", {Key{SpecialKey::PageDown}}), "test_page_down");

    check(eq("\x1bOA", {Key{SpecialKey::ArrowUp}}), "test_ss3_arrow_up");
    check(eq("\x1bOD", {Key{SpecialKey::ArrowLeft}}), "test_ss3_arrow_left");
    check(eq("\x1bOH", {Key{SpecialKey::Home}}), "test_ss3_home");
    check(eq("\x1bOF", {Key{SpecialKey::End}}), "test_ss3_end");

    check(eq("\x1b", {Key{SpecialKey::Escape}}), "test_lone_escape");
    check(eq("\x1bx", {Key{SpecialKey::Escape}, Key{'x'}}),
          "test_escape_then_typing");
    check(eq("\x1b\x1b", {Key{SpecialKey::Escape}, Key{SpecialKey::Escape}}),
          "test_double_escape");

    check(eq("\x1b[1;5C", {}), "test_modifier_sequence_swallowed_whole");
    check(eq("\x1b[2~", {}), "test_unknown_tilde_code_swallowed");
    check(eq("\x1b[Z", {}), "test_unknown_final_swallowed");
    check(eq("\x1b[", {}), "test_truncated_csi_dropped");
    check(eq("\x1b[5", {}), "test_truncated_params_dropped");
    check(eq("\x1bO", {}), "test_truncated_ss3_dropped");
    check(eq("\x1bOx", {}), "test_unknown_ss3_swallowed");

    check(eq("a\x1b[Ab", {Key{'a'}, Key{SpecialKey::ArrowUp}, Key{'b'}}),
          "test_sequence_embedded_in_typing");
    check(eq("\x1b[A\x1b[B\x1b[3~x",
             {Key{SpecialKey::ArrowUp}, Key{SpecialKey::ArrowDown},
              Key{SpecialKey::Delete}, Key{'x'}}),
          "test_back_to_back_sequences");
    check(eq("\x1b[1;5Cq", {Key{'q'}}),
          "test_stream_continues_after_swallowed_sequence");
    check(eq("\x11", {Key{CtrlKey{'q'}}}), "test_ctrl_byte_in_stream");
    return failed;
}
```

# Lesson: Bytes Out — Painting the Screen {#painting-the-screen}

Output is the same CSI grammar in the other direction: you `write()`
command sequences and the terminal executes them. The handful an editor
lives on:

```
ESC [ 2 J        erase the whole screen
ESC [ K          erase from the cursor to the end of the line
ESC [ H          cursor to row 1, column 1 (home)
ESC [ 12 ; 40 H  cursor to row 12, column 40  — 1-BASED, row;col
ESC [ 7 m        SGR (Select Graphic Rendition): inverted video (swap fg/bg)
ESC [ m          SGR: reset all attributes
ESC [ ? 25 l     hide the cursor        (l = reset a private mode)
ESC [ ? 25 h     show the cursor        (h = set a private mode)
```

Cursor addressing being **1-based** while every array in your program is
0-based is a permanent off-by-one hazard; we'll bury the `+1` in exactly
one function and never think about it again. The `?`-prefixed numbers are
*private modes* — extensions beyond ANSI X3.64, in a namespace DEC
reserved for itself, which is why they look different. Two more private
modes are worth knowing even though our tests don't cover them:
`ESC [ ? 1049 h` switches to the **alternate screen buffer** (vim's
trick: your shell scrollback is untouched underneath and reappears on
exit — pair the enter/leave with a `ScopedAction`), and `ESC [ ? 2004 h`
enables **bracketed paste**, making a paste arrive wrapped in
`ESC [ 200 ~` … `ESC [ 201 ~` markers so 500 pasted characters don't get
interpreted as 500 keystrokes (in vi, pasting text containing `j` would
otherwise *move the cursor*...).

### Flicker, and the one-write rule

The naive render loop clears the screen, then prints each line:

```cpp
write(fd, "\x1b[2J", 4);              // blank everything   <-- flicker!
for (auto& line : rows) write(fd, ...);
```

Between the clear and the last line, the terminal may refresh its own
display — the user sees a blank frame, i.e. flicker. Two fixes, both
standard practice:

- **Never clear the whole screen.** Redraw every line over the old
  content and erase only each line's tail with `ESC [ K`. Nothing is ever
  blank.
- **One `write()` per frame.** Build the entire frame — cursor-hide,
  home, every row, cursor-park, cursor-show — in a memory buffer, then
  hand the terminal one syscall's worth of bytes. Fewer syscalls, and no
  torn intermediate states. (kilo calls this the "append buffer" `abuf`;
  in C it's forty lines of realloc — in C++ it is `std::string` and
  `operator+=`. You already have a better abuf than kilo's.)

Hiding the cursor during the repaint (`?25l` … `?25h`) kills the last
artifact: the cursor visibly teleporting around the screen as lines are
drawn.

A note on types while we're building APIs that take text:
**`std::string_view`** is the right parameter type for "some characters I
will only read" — it's a pointer+length pair, so it accepts a
`std::string`, a literal, or a slice of either, without copying. The one
rule: a view doesn't own, so never *store* one beyond the call (the
classic dangling-view bug). Parameters yes, members no.

### The frame builder

`build_frame` below is your editor's whole render path minus the
`write()`. It takes the **already-visible** portion of the file (the
viewport lessons will produce it), the screen size, and where the cursor
should end up, and returns the exact byte sequence to send. Rows past the
end of the file render as `~` — vi's famous tilde column, which exists
precisely to distinguish "empty line in the file" from "beyond the end of
the file".

One more raw-mode consequence baked in: with `OPOST` off there is no
automatic `\n` → `\r\n`, so the frame must end every screen row (except
the last — writing a newline on the bottom row would scroll the terminal)
with an explicit `\r\n`.

## Challenge: One Frame, One Write {#frame-builder points=15}

Implement `build_frame(rows, screen_rows, screen_cols, cursor_row,
cursor_col)` returning the frame as a `std::string`, concatenated in
exactly this order:

- `"\x1b[?25l"` — hide the cursor, then `"\x1b[H"` — home.
- For each screen row `r` in `0 .. screen_rows-1`:
  - the text: `rows[r]` truncated to at most `screen_cols` characters if
    `r < rows.size()`, otherwise a single `"~"`;
  - `"\x1b[K"` — erase the stale tail of the old frame's line;
  - `"\r\n"` — unless this is the last screen row.
- `"\x1b[<row>;<col>H"` parking the cursor: `cursor_row`/`cursor_col`
  are **0-based screen coordinates**; the sequence wants them 1-based.
- `"\x1b[?25h"` — show the cursor again.

### Starter

```cpp
#include <string>
#include <string_view>
#include <vector>

std::string build_frame(const std::vector<std::string>& rows,
                        int screen_rows, int screen_cols,
                        int cursor_row, int cursor_col) {
    // TODO: hide cursor, home, draw each row (text or "~", then \x1b[K,
    // then \r\n except on the last row), park cursor 1-based, show it.
    (void)rows; (void)screen_rows; (void)screen_cols;
    (void)cursor_row; (void)cursor_col;
    return "";
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    {
        std::vector<std::string> rows = {"hello", "world"};
        std::string want =
            "\x1b[?25l\x1b[H"
            "hello\x1b[K\r\n"
            "world\x1b[K"
            "\x1b[1;1H\x1b[?25h";
        check(build_frame(rows, 2, 80, 0, 0) == want,
              "test_two_rows_exact_bytes");
    }
    {
        std::vector<std::string> rows = {"only"};
        std::string want =
            "\x1b[?25l\x1b[H"
            "only\x1b[K\r\n"
            "~\x1b[K\r\n"
            "~\x1b[K"
            "\x1b[1;1H\x1b[?25h";
        check(build_frame(rows, 3, 80, 0, 0) == want,
              "test_tildes_past_end_of_file");
    }
    {
        std::vector<std::string> rows = {"abcdefghij"};
        std::string got = build_frame(rows, 1, 4, 0, 0);
        check(got == "\x1b[?25l\x1b[Habcd\x1b[K\x1b[1;1H\x1b[?25h",
              "test_truncates_to_screen_cols");
    }
    {
        std::vector<std::string> rows = {"a", "b"};
        std::string got = build_frame(rows, 2, 80, 1, 3);
        check(got.find("\x1b[2;4H") != std::string::npos,
              "test_cursor_park_is_one_based");
        check(got.find("\x1b[?25h") == got.size() - 6,
              "test_show_cursor_is_last");
    }
    {
        std::vector<std::string> rows;
        std::string want =
            "\x1b[?25l\x1b[H"
            "~\x1b[K\r\n"
            "~\x1b[K"
            "\x1b[1;1H\x1b[?25h";
        check(build_frame(rows, 2, 80, 0, 0) == want,
              "test_empty_file_all_tildes");
    }
    {
        std::vector<std::string> rows = {""};
        std::string got = build_frame(rows, 2, 80, 0, 0);
        check(got ==
                  "\x1b[?25l\x1b[H"
                  "\x1b[K\r\n"
                  "~\x1b[K"
                  "\x1b[1;1H\x1b[?25h",
              "test_empty_line_is_not_a_tilde");
    }
    {
        // No \r\n after the last row: writing one would scroll the
        // terminal and shear the whole frame upward.
        std::vector<std::string> rows = {"x", "y", "z"};
        std::string got = build_frame(rows, 3, 80, 2, 0);
        check(got.find("z\x1b[K\r\n") == std::string::npos,
              "test_no_newline_after_last_row");
    }
    {
        std::vector<std::string> rows = {"row"};
        std::string got = build_frame(rows, 1, 80, 0, 79);
        check(got.find("\x1b[1;80H") != std::string::npos,
              "test_cursor_park_wide_column");
    }
    return failed;
}
```

## Challenge: The Status Bar {#status-bar points=10}

Every vi descendant reserves a line for status: filename, a dirty
marker, where you are in the file. Rendering one is a fixed-width
formatting problem — the bar must be **exactly** the screen width, no
matter how long the filename is, because it's drawn in inverted video
(`ESC [ 7 m`) and a bar one column short leaves a normal-video hole; one
column long and it wraps, shearing the frame.

Implement `status_bar(filename, dirty, current_line, total_lines,
width)`:

- Left part: the filename, or `"[No Name]"` if it's empty; then `" [+]"`
  appended if `dirty` (unsaved changes).
- Right part: `"<current_line>/<total_lines>"` (both already 1-based).
- Compose `left + spaces + right` padded to exactly `width` characters,
  with at least one space between the parts. If the left part is too
  long, truncate it (keep its prefix) so that the space and right part
  still fit exactly. If even the right part alone exceeds `width`, use
  its first `width` characters.
- Wrap the padded content in `"\x1b[7m"` … `"\x1b[m"`.

The returned string is what your render loop appends to the frame right
after the text rows.

### Starter

```cpp
#include <string>
#include <string_view>

std::string status_bar(std::string_view filename, bool dirty,
                       int current_line, int total_lines, int width) {
    // TODO: build left ("[No Name]" fallback, " [+]" if dirty) and
    // right ("cur/total"), pad or truncate to exactly `width` chars,
    // wrap in \x1b[7m ... \x1b[m.
    (void)filename; (void)dirty; (void)current_line;
    (void)total_lines; (void)width;
    return "";
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

// Strip the required SGR wrapper; returns the content or "" if the
// wrapper is missing/malformed.
static std::string content(const std::string& bar) {
    const std::string pre = "\x1b[7m", post = "\x1b[m";
    if (bar.size() < pre.size() + post.size()) return "";
    if (bar.compare(0, pre.size(), pre) != 0) return "";
    if (bar.compare(bar.size() - post.size(), post.size(), post) != 0)
        return "";
    return bar.substr(pre.size(), bar.size() - pre.size() - post.size());
}

int main() {
    {
        std::string c = content(status_bar("notes.txt", false, 3, 10, 30));
        check(c.size() == 30, "test_exact_width");
        check(c.rfind("notes.txt", 0) == 0, "test_filename_on_left");
        check(c.size() >= 4 && c.substr(c.size() - 4) == "3/10",
              "test_position_on_right");
        check(c.find("  ") != std::string::npos, "test_padded_with_spaces");
    }
    {
        std::string c = content(status_bar("a.c", true, 1, 1, 20));
        check(c.rfind("a.c [+]", 0) == 0, "test_dirty_marker");
        check(c.size() == 20, "test_dirty_still_exact_width");
    }
    {
        std::string c = content(status_bar("a.c", false, 1, 1, 20));
        check(c.find("[+]") == std::string::npos,
              "test_clean_has_no_marker");
    }
    {
        std::string c = content(status_bar("", false, 5, 9, 30));
        check(c.rfind("[No Name]", 0) == 0, "test_no_name_fallback");
    }
    {
        // Long filename: truncated so right part still fits exactly.
        std::string c = content(
            status_bar("a_very_long_file_name_indeed.cpp", false, 12, 340,
                       20));
        check(c.size() == 20, "test_long_name_exact_width");
        check(c.size() >= 6 && c.substr(c.size() - 6) == "12/340",
              "test_long_name_keeps_right_part");
        check(c.size() >= 7 && c[c.size() - 7] == ' ',
              "test_long_name_separator_space");
        check(c.rfind("a_very_long_f", 0) == 0,
              "test_long_name_prefix_kept");
    }
    {
        std::string bar = status_bar("x", false, 1, 2, 10);
        check(bar.size() >= 7 && bar.rfind("\x1b[7m", 0) == 0 &&
                  bar.substr(bar.size() - 3) == "\x1b[m",
              "test_sgr_wrapper");
    }
    {
        // Degenerate width: right part alone doesn't fit.
        std::string c = content(status_bar("f", false, 100, 2000, 4));
        check(c == "100/", "test_tiny_width_truncates_right");
    }
    return failed;
}
```

# Lesson: How Big Is the Screen? {#window-size-and-signals}

`build_frame` needs `screen_rows` and `screen_cols`. The terminal knows;
the question is how to ask. There are two answers, and a robust editor
implements both.

### The ioctl, and the escape-sequence fallback

The kernel tracks each tty's dimensions and hands them over through an
ioctl:

```cpp
#include <sys/ioctl.h>

winsize ws;
if (ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws) == 0 && ws.ws_col > 0) {
    rows = ws.ws_row;
    cols = ws.ws_col;
}
```

`TIOCGWINSZ` — *TIOCtl Get WINdow SiZe*. It can fail (some serial links,
some odd environments report 0×0), and the traditional fallback is a
lovely hack built from two escape sequences: send `ESC [ 999 ; 999 H`
(cursor addressing clamps at the screen edge, so the cursor lands at the
true bottom-right), then send `ESC [ 6 n` — **DSR, Device Status Report**,
argument 6: "report the active cursor position". The terminal *writes
back onto your stdin* a **Cursor Position Report**:

```
ESC [ 24 ; 80 R        ← the terminal typed this into your input
```

Parse the two numbers and you have the screen size. Note what just
happened: the input stream your key decoder reads can also contain
*replies from the terminal*. (That's also how bracketed-paste markers
arrive, and why decoders that choke on unexpected CSI input are a
liability — your Lesson 2 decoder already swallows unknown sequences.)

### SIGWINCH: the size changes under you

When the user drags the terminal window, every process in its foreground
process group receives **SIGWINCH** (WINdow CHange — one of the few
signals that's an information broadcast, not a demand). The handler
discipline from Lesson 1 applies doubly here: a signal handler can fire
between *any two instructions*, so it must only do things that are
async-signal-safe. The standard pattern is a flag:

```cpp
volatile sig_atomic_t g_resized = 0;
void on_winch(int) { g_resized = 1; }   // the entire handler
...
// in the main loop, between keystrokes:
if (g_resized) { g_resized = 0; requery_size(); clamp_and_redraw(); }
```

`volatile sig_atomic_t` is the only type the C and C++ standards bless
for this. Conveniently, a pending `read()` returns −1/`EINTR` when a
signal arrives (install the handler *without* `SA_RESTART`), so a resize
also wakes your input loop immediately instead of waiting for the next
keystroke. After re-querying the size, the editor must **clamp**: the
cursor and scroll offsets that were valid at 80×24 may be far outside a
40×12 window — that clamping function is exactly the viewport logic you
build in a later lesson.

The graded piece of this lesson is the parser for the cursor position
report, which is subtler than it looks: it reads *attacker-adjacent*
input (any program can write bytes to your terminal), so it must reject
garbage rather than parse it optimistically. `std::optional` is the
right return type — "a position, or nothing" — making the failure case
impossible to ignore silently, unlike an `int*` out-param or a magic
−1×−1.

## Challenge: Parse the Cursor Position Report {#cursor-report points=10}

Implement `parse_cursor_report(s)` returning `std::optional<std::pair<int,
int>>` — `{rows, cols}` — for a **complete, exact** report:

- The string must be exactly `ESC [ <digits> ; <digits> R` — nothing
  before, nothing after.
- Both numbers must be 1–4 digits (no terminal is 10,000 columns wide;
  the cap also sidesteps integer overflow on hostile input) and ≥ 1.
- Anything else — missing prefix, missing `;`, empty digits, trailing
  bytes, a lowercase `r` — returns `std::nullopt`.

### Starter

```cpp
#include <optional>
#include <string_view>
#include <utility>

std::optional<std::pair<int, int>> parse_cursor_report(std::string_view s) {
    // TODO: match \x1b [ digits ; digits R exactly; 1-4 digits each,
    // both values >= 1; otherwise nullopt.
    (void)s;
    return std::nullopt;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    {
        auto r = parse_cursor_report("\x1b[24;80R");
        check(r && r->first == 24 && r->second == 80, "test_classic_24x80");
    }
    {
        auto r = parse_cursor_report("\x1b[1;1R");
        check(r && r->first == 1 && r->second == 1, "test_minimum_1x1");
    }
    {
        auto r = parse_cursor_report("\x1b[9999;9999R");
        check(r && r->first == 9999 && r->second == 9999,
              "test_four_digit_max");
    }
    check(!parse_cursor_report(""), "test_empty");
    check(!parse_cursor_report("\x1b[24;80"), "test_missing_final_r");
    check(!parse_cursor_report("\x1b[24;80r"), "test_lowercase_r_rejected");
    check(!parse_cursor_report("[24;80R"), "test_missing_escape");
    check(!parse_cursor_report("\x1b[2480R"), "test_missing_semicolon");
    check(!parse_cursor_report("\x1b[;80R"), "test_empty_rows");
    check(!parse_cursor_report("\x1b[24;R"), "test_empty_cols");
    check(!parse_cursor_report("\x1b[24;80Rx"), "test_trailing_garbage");
    check(!parse_cursor_report("x\x1b[24;80R"), "test_leading_garbage");
    check(!parse_cursor_report("\x1b[12345;80R"), "test_five_digits_rejected");
    check(!parse_cursor_report("\x1b[0;80R"), "test_zero_rows_rejected");
    check(!parse_cursor_report("\x1b[24;0R"), "test_zero_cols_rejected");
    check(!parse_cursor_report("\x1b[2a;80R"), "test_nondigit_rejected");
    check(!parse_cursor_report("\x1b[-1;80R"), "test_negative_rejected");
    return failed;
}
```

# Lesson: The Text Buffer {#the-text-buffer}

Enough plumbing; time for the data structure at the heart of the editor.
How should a file being edited live in memory? This question has a
fifty-year literature (Crowley's *Data Structures for Text Sequences*,
in the extended reading, is the best survey), and every serious answer
is a different point on the same tradeoff curve:

- **One flat `std::string`.** Reading and saving are trivial; rendering
  needs a scan to find line starts; and inserting a character at
  position *i* moves every byte after *i*. For a 10 MB file with the
  cursor at the top, that's 10 MB of `memmove` *per keystroke*. Fine for
  a config-file editor, embarrassing beyond that.
- **A vector of lines** (`std::vector<std::string>`). What `vi`, kilo,
  and a large fraction of real editors use. Inserting a character moves
  only the tail of *one line* (~40 bytes, not megabytes); inserting or
  deleting a whole line shifts only the vector's pointers-to-lines, not
  the text. Rendering is natural — the viewport asks for lines *r₁..r₂*
  — and per-line syntax highlighting falls out for free. Weaknesses:
  a pathological single-line file (a minified 5 MB `bundle.js`) degrades
  to the flat-string case, and line joins/splits churn allocations.
- **A gap buffer.** The Emacs answer; next lesson, in full.
- **A rope** — a balanced tree of string chunks: everything is O(log n),
  10 GB files open instantly, and the implementation is 10× the code and
  every simple question ("what's at line 12?") becomes a tree walk. The
  choice of xi and helix.
- **A piece table** — the file is never modified; edits are a list of
  descriptors pointing into the original bytes plus an append-only "add"
  buffer. Undo is nearly free (the old pieces still exist), which is why
  VS Code and, going back further, Word use it.

The honest engineering call for a terminal editor aimed at source files:
**vector of lines**, and that's what our editor core uses. The costs it
doesn't handle (giant single lines) are real but rare; the costs it
avoids (complexity, cache-hostile tree walks for the common case) are
paid on every keystroke. Data structure choice is about *which operation
you make cheap*, and an editor's hot operations are: insert/delete a
char near the cursor, split/join a line, and read a screenful of
consecutive lines.

### Positions, and the operations that edit

A position in the buffer is a row/column pair — a tiny value type,
rule-of-zero, with C++20's `= default` comparison:

```cpp
struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};
```

`col` counts **characters into the line's string** — the *chars index*,
`cx` in kilo's terminology. (How that differs from the *screen* column
once tabs enter the picture is the whole next-next lesson.) The buffer's
editing API is three operations, and each returns the cursor position
the editor should move to — pinning down, in the type signature, a
question every editor must answer ("after Enter, where is the cursor?"):

- `insert_char(p, c)` → cursor after the inserted char: `{row, col+1}`.
- `insert_newline(p)` — split the line at `p`: the current line keeps
  `[0, col)`, a new line below receives `[col, end)`. Cursor: start of
  the new line. Pressing Enter at the end of a line inserts an empty
  line below; at column 0 it pushes the whole line down.
- `backspace(p)` — delete the character *before* `p`. At `col == 0` the
  line **joins**: the current line's text is appended to the previous
  line, and the cursor lands at the join seam. Backspace at `{0, 0}`
  does nothing. This join is why Backspace at the start of a line pulls
  it up — one operation, two behaviors, both fall out of "delete the
  boundary before the cursor".

One invariant makes every downstream component simpler: **the buffer
always contains at least one line** (possibly empty). "Empty buffer" is
`{""}`, never `{}` — so there is always a line for the cursor to sit on,
and no code ever checks "is there a line 0?".

## Challenge: A Vector-of-Lines Buffer {#line-buffer points=20}

Implement `TextBuffer` per the operations above. Details the tests pin
down:

- The default constructor yields one empty line; constructing from an
  empty vector must also normalize to `{""}`.
- `line(row)` returns the line's text **by value**; out-of-range rows
  return `""`. (Returning `const std::string&` would be faster — and
  would dangle the moment a caller kept the reference across an edit
  that reallocates the vector. Return-by-value is the value-semantics
  default; optimize only the proven hot path.)
- Positions passed in are trusted to be valid: `row < line_count()`,
  `col <= line(row).size()`. (The editor core maintains that invariant;
  the buffer doesn't re-police it.)
- `to_string()` joins lines with `'\n'` — `{"a", "b"}` → `"a\nb"`, and
  `{""}` → `""`.

### Starter

```cpp
#include <cstddef>
#include <string>
#include <utility>
#include <vector>

struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};

class TextBuffer {
public:
    TextBuffer() : lines_{""} {}
    explicit TextBuffer(std::vector<std::string> lines)
        : lines_(std::move(lines)) {
        if (lines_.empty()) lines_.push_back("");
    }

    std::size_t line_count() const { return lines_.size(); }

    std::string line(std::size_t row) const {
        // TODO: the row's text, or "" when out of range.
        (void)row;
        return "";
    }

    Pos insert_char(Pos p, char c) {
        // TODO: insert c at p; return the position after it.
        (void)c;
        return p;
    }

    Pos insert_newline(Pos p) {
        // TODO: split line p.row at p.col; return start of the new line.
        return p;
    }

    Pos backspace(Pos p) {
        // TODO: delete the char before p; join lines at col 0;
        // no-op at {0,0}. Return the new cursor position.
        return p;
    }

    std::string to_string() const {
        // TODO: join with '\n'.
        return "";
    }

private:
    std::vector<std::string> lines_;
};
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    {
        TextBuffer b;
        check(b.line_count() == 1 && b.line(0) == "",
              "test_default_is_one_empty_line");
        check(TextBuffer(std::vector<std::string>{}).line_count() == 1,
              "test_empty_vector_normalized");
    }
    {
        TextBuffer b;
        Pos p;
        for (char c : std::string("hi"))
            p = b.insert_char(p, c);
        check(b.line(0) == "hi" && p == Pos{0, 2}, "test_typing_appends");
    }
    {
        TextBuffer b(std::vector<std::string>{"hllo"});
        Pos p = b.insert_char({0, 1}, 'e');
        check(b.line(0) == "hello" && p == Pos{0, 2},
              "test_insert_mid_line");
    }
    {
        TextBuffer b(std::vector<std::string>{"hello world"});
        Pos p = b.insert_newline({0, 5});
        check(b.line_count() == 2 && b.line(0) == "hello" &&
                  b.line(1) == " world" && p == Pos{1, 0},
              "test_enter_splits_line");
    }
    {
        TextBuffer b(std::vector<std::string>{"abc"});
        Pos p = b.insert_newline({0, 3});
        check(b.line_count() == 2 && b.line(0) == "abc" &&
                  b.line(1) == "" && p == Pos{1, 0},
              "test_enter_at_eol_opens_empty_line");
    }
    {
        TextBuffer b(std::vector<std::string>{"abc"});
        Pos p = b.insert_newline({0, 0});
        check(b.line_count() == 2 && b.line(0) == "" && b.line(1) == "abc" &&
                  p == Pos{1, 0},
              "test_enter_at_col0_pushes_line_down");
    }
    {
        TextBuffer b(std::vector<std::string>{"abc"});
        Pos p = b.backspace({0, 2});
        check(b.line(0) == "ac" && p == Pos{0, 1},
              "test_backspace_mid_line");
    }
    {
        TextBuffer b(std::vector<std::string>{"hello", "world"});
        Pos p = b.backspace({1, 0});
        check(b.line_count() == 1 && b.line(0) == "helloworld" &&
                  p == Pos{0, 5},
              "test_backspace_at_col0_joins_lines");
    }
    {
        TextBuffer b(std::vector<std::string>{"", "x"});
        Pos p = b.backspace({1, 0});
        check(b.line_count() == 1 && b.line(0) == "x" && p == Pos{0, 0},
              "test_join_into_empty_line");
    }
    {
        TextBuffer b(std::vector<std::string>{"abc"});
        Pos p = b.backspace({0, 0});
        check(b.line_count() == 1 && b.line(0) == "abc" && p == Pos{0, 0},
              "test_backspace_at_origin_is_noop");
    }
    {
        TextBuffer b(std::vector<std::string>{"a", "b", "c"});
        check(b.to_string() == "a\nb\nc", "test_to_string_joins");
        check(TextBuffer().to_string() == "", "test_to_string_empty");
    }
    {
        TextBuffer b(std::vector<std::string>{"ab"});
        check(b.line(5) == "", "test_out_of_range_line_is_empty");
    }
    {
        // Round-trip a small editing session: type, split, join back.
        TextBuffer b;
        Pos p;
        for (char c : std::string("one two"))
            p = b.insert_char(p, c);
        p = b.insert_newline({0, 3});           // "one" / " two"
        p = b.backspace(p);                     // join back
        check(b.to_string() == "one two" && p == Pos{0, 3},
              "test_split_then_join_roundtrip");
    }
    return failed;
}
```

# Lesson: The Gap Buffer {#the-gap-buffer}

Before committing to vector-of-lines forever, build the classic
alternative well enough to respect it. The **gap buffer** is how Emacs
has stored text since the 1970s, and it's founded on one empirical
observation: *edits cluster*. You type a character at position 100, the
next lands at 101, then 102. A flat string pays a full `memmove` for
each. The gap buffer prepays **one** move, then rides the cluster for
free.

The idea: store the text in one array with a **hole** — the gap — at the
cursor:

```
        "the qick fox"  with the cursor after "q", gap size 4:
        the q[____]ick fox
              ^gap_start           text[gap_start..gap_end) is dead space
```

- **Insert at the cursor**: write into the gap, `gap_start++`. O(1).
- **Delete before the cursor**: `gap_start--`. O(1). Delete after:
  `gap_end++`. O(1). Nothing is moved; the dead zone just swallows the
  character. (This should remind you of the append buffer trick:
  reserve space once, then cheap operations amortize against it.)
- **Move the cursor** to position *p*: slide the gap there by
  `memmove`-ing the characters between the old and new gap position —
  cost proportional to the *distance moved*, not the file size. A
  cursor that moves one line pays ~80 bytes; jumping from top to bottom
  of a 10 MB file pays 10 MB, once, and then edits are O(1) again.
- **Gap exhausted** (`gap_start == gap_end`): reallocate bigger, like
  `std::vector` growth — doubling gives amortized O(1) inserts.

The "logical" text is the array minus the gap, so reading position *i*
must skip it:

```cpp
char at(size_t i) {                     // logical index -> physical
    return i < gap_start ? buf[i] : buf[i + gap_size()];
}
```

That translation is the gap buffer's tax: every read pays a branch, and
substring extraction must be gap-aware. It's also why "vector of lines
where each line is a gap buffer" — plausibly the best of both — is
rarely worth it: the constant factors eat the win for ordinary line
lengths.

Complexity summary, honestly stated: for a buffer of n characters and an
edit at distance d from the previous edit — flat string: O(n) per edit,
always. Gap buffer: O(d) to seek + amortized O(1) per edit; the
pathological case is *alternating* edits at opposite ends (O(n) each).
Vector of lines: O(line length) per edit regardless of distance. Ropes
and piece tables: O(log n), for 10× the code. Emacs bets edits cluster
(they do), vi bets lines are short (they are). Both bets are 50 years
old and still paying out.

## Challenge: Build the Gap Buffer {#gap-buffer points=25}

Implement `GapBuffer` with logical-index semantics — from the outside it
behaves like a `std::string`; the gap is invisible except through
`gap_start()`, which the tests use to verify you're really moving a gap
and not `memmove`-ing the world:

- `GapBuffer(std::string_view initial)`: logical content = `initial`,
  and the gap sits **at the end**, with nonzero capacity (so
  `gap_start() == size()` after construction).
- `size()`, `at(i)` (`i < size()` guaranteed by callers), `to_string()`.
- `insert(pos, c)`: after it, `gap_start() == pos + 1` — the gap was
  moved to `pos`, the character written, the gap front advanced.
- `erase(pos, n)`: remove `n` logical chars starting at `pos` (callers
  guarantee `pos + n <= size()`) by **growing the gap over them**;
  after it, `gap_start() == pos`. Deletion never moves text — the tests
  can't see that directly, but `gap_start()` landing exactly at `pos`
  means you absorbed, not shifted.
- When the gap is empty at insert time, grow capacity (any growth
  policy; doubling is traditional).

Store the text in a `std::string` or `std::vector<char>` — raw `new[]`
buys nothing here but a rule-of-five obligation you'd have to meet
yourself. (Composition over allocation: the rule of zero means the
default special members are already correct.)

### Starter

```cpp
#include <cstddef>
#include <string>
#include <string_view>

class GapBuffer {
public:
    explicit GapBuffer(std::string_view initial = "") {
        // TODO: content + a gap at the end (nonzero initial capacity).
        (void)initial;
    }

    std::size_t size() const {
        return 0; // TODO: logical size (excludes the gap)
    }

    char at(std::size_t i) const {
        // TODO: logical index -> physical index, skipping the gap.
        (void)i;
        return '\0';
    }

    std::size_t gap_start() const {
        return 0; // TODO
    }

    void insert(std::size_t pos, char c) {
        // TODO: move gap to pos (grow first if empty), write c,
        // advance gap_start.
        (void)pos; (void)c;
    }

    void erase(std::size_t pos, std::size_t n) {
        // TODO: move gap to pos, then extend its end over n chars.
        (void)pos; (void)n;
    }

    std::string to_string() const {
        return ""; // TODO: the logical content, gap skipped
    }

private:
    std::string buf_;        // physical storage, including the gap
    std::size_t gap_start_ = 0;
    std::size_t gap_end_ = 0; // gap is [gap_start_, gap_end_)
};
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    {
        GapBuffer g("hello");
        check(g.size() == 5, "test_initial_size");
        check(g.to_string() == "hello", "test_initial_content");
        check(g.gap_start() == 5, "test_gap_starts_at_end");
        check(g.at(0) == 'h' && g.at(4) == 'o', "test_at_before_gap");
    }
    {
        GapBuffer g;
        check(g.size() == 0 && g.to_string() == "", "test_empty_buffer");
        g.insert(0, 'a');
        check(g.to_string() == "a" && g.size() == 1,
              "test_insert_into_empty");
    }
    {
        GapBuffer g("the ick fox");
        g.insert(4, 'q');
        check(g.to_string() == "the qick fox", "test_insert_mid");
        check(g.gap_start() == 5, "test_gap_follows_insert");
        g.insert(5, 'u');
        check(g.to_string() == "the quick fox", "test_clustered_insert");
        check(g.gap_start() == 6, "test_gap_rides_the_cluster");
    }
    {
        GapBuffer g("abc");
        g.insert(0, 'x');
        check(g.to_string() == "xabc", "test_insert_at_front");
        check(g.gap_start() == 1, "test_gap_moved_to_front");
        check(g.at(0) == 'x' && g.at(1) == 'a' && g.at(3) == 'c',
              "test_at_spans_the_gap");
    }
    {
        GapBuffer g("abcdef");
        g.erase(1, 3);
        check(g.to_string() == "aef" && g.size() == 3, "test_erase_middle");
        check(g.gap_start() == 1, "test_gap_lands_at_erase_point");
        g.insert(1, 'X');
        check(g.to_string() == "aXef", "test_insert_after_erase");
    }
    {
        GapBuffer g("abc");
        g.erase(0, 3);
        check(g.size() == 0 && g.to_string() == "", "test_erase_all");
        g.insert(0, 'z');
        check(g.to_string() == "z", "test_reuse_after_erase_all");
    }
    {
        GapBuffer g("ab");
        g.erase(2, 0);
        check(g.to_string() == "ab" && g.gap_start() == 2,
              "test_erase_zero_at_end");
    }
    {
        // Force growth: many inserts at the same cluster.
        GapBuffer g;
        for (int i = 0; i < 1000; i++)
            g.insert(static_cast<std::size_t>(i), 'a' + (i % 26));
        check(g.size() == 1000, "test_growth_size");
        bool ok = true;
        for (int i = 0; i < 1000; i++)
            ok = ok && g.at(static_cast<std::size_t>(i)) == 'a' + (i % 26);
        check(ok, "test_growth_content");
    }
    {
        // Ping-pong: alternate front/back edits (the worst case) —
        // correctness must survive even when performance suffers.
        GapBuffer g("0123456789");
        g.insert(0, 'F');
        g.insert(g.size(), 'B');
        g.insert(1, 'f');
        g.insert(g.size() - 1, 'b');
        check(g.to_string() == "Ff0123456789bB", "test_ping_pong_edits");
    }
    {
        // Erase spanning a previous gap position.
        GapBuffer g("hello world");
        g.insert(5, ',');            // gap now at 6
        g.erase(4, 4);               // spans the gap seam
        check(g.to_string() == "hellorld", "test_erase_across_gap_seam");
        check(g.gap_start() == 4, "test_gap_after_spanning_erase");
    }
    return failed;
}
```

# Lesson: Cursor Math and the Viewport {#cursor-math-and-the-viewport}

Here is a bug every first-time editor author ships: open a file with a
tab in it, press the right-arrow key, and watch the cursor jump eight
columns while your position bookkeeping says it moved one. The buffer
and the screen *disagree about geometry*, and the disagreement has a
name in kilo's source that we'll adopt: **cx** versus **rx**.

- **cx** — the *chars index*: how many characters into the line's
  string the cursor is. This is what `TextBuffer` operations take: byte
  positions in `std::string`s.
- **rx** — the *render index*: which screen column the cursor occupies.
  This is what `build_frame`'s cursor-park parameter needs.

For a line of plain ASCII they're equal. One character breaks the
equality: **Tab** (0x09). A tab is *one character* in the buffer but
renders as *one to eight columns* on screen, because a tab doesn't mean
"eight spaces" — it means **advance to the next tab stop**, the next
column that's a multiple of the tab width. Typewriter semantics,
faithfully preserved by every terminal since. Given tab stops every 8:

```
buffer:   \t  x  \t  y            cx: 0   1   2   3
screen:   ········x·······y       rx: 0-7 8   9-15 16
                                       ^ first tab: 8 cols (cursor was at 0)
                                             ^ second tab: 7 cols (was at 9)
```

The same `\t` costs 8 columns or 7 columns depending on where it
starts. So there's no per-character lookup table — converting cx to rx
requires *replaying the line from column zero*:

```
rx = 0
for each of the first cx characters:
    if it's a tab: rx += (tabstop - 1) - (rx % tabstop)   # to the stop...
    rx += 1                                               # ...inclusive
```

The `(tabstop - 1) - (rx % tabstop)` idiom: how far to the character
*before* the next stop, and then the shared `rx += 1` steps onto the
stop itself. Every editor and terminal implements exactly this rule, so
your rendering agrees with what the terminal does when it prints the
tab.

The inverse — **rx to cx** — is needed the moment the user clicks a
screen column, or (sooner, for us) presses `j` and the editor must
figure out which *character* of the next line sits under the unchanged
*screen* column. There's no closed form; you replay the line, watching
for the render position to pass the target. Two conversions, one
replay-loop each — pure functions, perfect for headless tests, and
consumed directly by the second half of this lesson.

### The viewport

A 100,000-line file, a 40-row window. The editor shows a **viewport**: a
contiguous band of lines starting at `row_off`, and (for long lines) a
horizontal band of columns starting at `col_off`. Rendering row `r` of
the screen means drawing buffer line `row_off + r`, sliced to columns
`[col_off, col_off + screen_cols)` — that slice is exactly what you feed
`build_frame`, and the cursor parks at screen position `(cur_row -
row_off, cur_rx - col_off)`.

The design question is the *policy*: when does the viewport move? The
vi answer — and the one that feels right in every editor since — is:
**the cursor moves; the viewport follows just enough to keep it
visible.** Never more. Scrolling by whole pages when the cursor walks
off the edge (some 1980s editors did) disorients; scrolling by exactly
one line feels like the window is glued to the cursor. The rule, per
axis:

- Cursor above the window (`cur_row < row_off`): snap the top edge to
  it — `row_off = cur_row`.
- Cursor below the last visible row (`cur_row >= row_off +
  screen_rows`): snap the *bottom* edge to it — `row_off = cur_row -
  screen_rows + 1`.
- Otherwise: don't touch it. (This case matters as much as the other
  two — a viewport that recenters on every keystroke is unusable.)

The horizontal axis is identical with `col_off`/`cur_rx`/`screen_cols` —
and note it's **rx**, not cx, that the horizontal comparison uses: the
viewport is a window over *screen columns*, and `cx_to_rx` is what turns the cursor's buffer position into the
coordinate this policy clamps. Wire them in the wrong order (clamp on
cx, render with rx) and files with tabs scroll horizontally at the
wrong moment — by an amount that depends on how many tabs are left of
the cursor, a lovely class of bug to debug at 40 columns.

The scroll function runs *every frame*, between "the keypress moved the
cursor" and "build the frame" — which also makes it the resize handler:
when SIGWINCH delivers a new, smaller `screen_rows`, the same clamp
walks the viewport back to keep the cursor on screen. One pure
function, two jobs.

## Challenge: Tab-Stop Arithmetic {#render-column points=15}

Implement both conversions:

- `cx_to_rx(line, cx, tabstop)`: the render column of character index
  `cx` (0 ≤ cx ≤ line.size(); cx == size means "just past the last
  char"). Replay the first `cx` characters with the tab-stop rule.
- `rx_to_cx(line, rx, tabstop)`: the character index whose render
  column *covers or first exceeds* `rx` — replay the whole line, and
  return the first `cx` whose running render column goes **strictly
  above** `rx`; if the line ends first (the target column is past the
  end of the line), return `line.size()`. A consequence the tests pin:
  any rx inside a tab's span maps to the tab character itself, so a
  cursor can never land "inside" a tab.

`tabstop` is always ≥ 1. Non-tab characters are one column each (we
render plain ASCII; the multi-column story for CJK characters and
emoji is a rabbit hole — `wcwidth()` — that we acknowledge and step
around).

### Starter

```cpp
#include <cstddef>
#include <string_view>

std::size_t cx_to_rx(std::string_view line, std::size_t cx,
                     std::size_t tabstop) {
    // TODO: replay the first cx chars; tabs advance to the next stop.
    (void)line; (void)cx; (void)tabstop;
    return 0;
}

std::size_t rx_to_cx(std::string_view line, std::size_t rx,
                     std::size_t tabstop) {
    // TODO: replay the line; return the first cx whose render column
    // exceeds rx, or line.size() if rx is past the end.
    (void)line; (void)rx; (void)tabstop;
    return 0;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    check(cx_to_rx("hello", 3, 8) == 3, "test_ascii_identity");
    check(cx_to_rx("hello", 0, 8) == 0, "test_zero_is_zero");
    check(cx_to_rx("hello", 5, 8) == 5, "test_cx_at_end");

    check(cx_to_rx("\tx", 1, 8) == 8, "test_tab_at_col0_is_8_wide");
    check(cx_to_rx("\tx", 2, 8) == 9, "test_char_after_tab");
    check(cx_to_rx("a\tx", 2, 8) == 8, "test_tab_at_col1_is_7_wide");
    check(cx_to_rx("abcdefg\tx", 8, 8) == 8,
          "test_tab_at_col7_is_1_wide");
    check(cx_to_rx("\t\tx", 2, 8) == 16, "test_two_tabs");
    check(cx_to_rx("a\tb\tc", 5, 8) == 17, "test_mixed_line_full");

    check(cx_to_rx("\tx", 1, 4) == 4, "test_tabstop_4");
    check(cx_to_rx("ab\tx", 3, 4) == 4, "test_tabstop_4_partial");
    check(cx_to_rx("\t", 1, 1) == 1, "test_tabstop_1_degenerate");

    check(rx_to_cx("hello", 3, 8) == 3, "test_inverse_ascii");
    check(rx_to_cx("hello", 0, 8) == 0, "test_inverse_zero");
    check(rx_to_cx("hello", 99, 8) == 5, "test_inverse_past_end");
    check(rx_to_cx("", 5, 8) == 0, "test_inverse_empty_line");

    check(rx_to_cx("\tx", 0, 8) == 0, "test_inside_tab_maps_to_tab");
    check(rx_to_cx("\tx", 7, 8) == 0, "test_last_tab_column_is_tab");
    check(rx_to_cx("\tx", 8, 8) == 1, "test_first_col_after_tab");
    check(rx_to_cx("a\tb", 4, 8) == 1, "test_inside_offset_tab");
    check(rx_to_cx("a\tb", 8, 8) == 2, "test_after_offset_tab");

    // Round-trip: cx -> rx -> cx is identity for every valid cx.
    {
        std::string_view line = "a\tbc\t\td";
        bool ok = true;
        for (std::size_t cx = 0; cx <= line.size(); cx++)
            ok = ok && rx_to_cx(line, cx_to_rx(line, cx, 8), 8) == cx;
        check(ok, "test_roundtrip_identity");
    }
    return failed;
}
```

## Challenge: Scroll to Follow the Cursor {#viewport-scroll points=15}

Implement `scroll`, applying the follow-the-cursor policy to both axes
and returning the adjusted viewport. Inputs the tests rely on:
`cur_row`, `cur_rx` are the cursor's buffer row and *render* column;
`screen_rows`, `screen_cols` are ≥ 1; all values are non-negative ints;
offsets in the input viewport may be arbitrarily stale (a resize may
have shrunk the screen since they were computed) but never negative.

### Starter

```cpp
struct Viewport {
    int row_off = 0;
    int col_off = 0;
    bool operator==(const Viewport&) const = default;
};

Viewport scroll(Viewport vp, int cur_row, int cur_rx,
                int screen_rows, int screen_cols) {
    // TODO: per axis — snap the near edge if the cursor is outside;
    // leave the offset alone if it's visible.
    (void)cur_row; (void)cur_rx; (void)screen_rows; (void)screen_cols;
    return vp;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    check(scroll({0, 0}, 5, 5, 24, 80) == Viewport{0, 0},
          "test_visible_cursor_no_scroll");
    check(scroll({10, 0}, 10, 0, 24, 80) == Viewport{10, 0},
          "test_cursor_at_top_edge_stays");
    check(scroll({10, 0}, 33, 0, 24, 80) == Viewport{10, 0},
          "test_cursor_at_bottom_edge_stays");

    check(scroll({10, 0}, 9, 0, 24, 80) == Viewport{9, 0},
          "test_scroll_up_snaps_top");
    check(scroll({10, 0}, 0, 0, 24, 80) == Viewport{0, 0},
          "test_jump_to_top");
    check(scroll({10, 0}, 34, 0, 24, 80) == Viewport{11, 0},
          "test_scroll_down_one");
    check(scroll({0, 0}, 100, 0, 24, 80) == Viewport{77, 0},
          "test_jump_far_down_bottom_aligned");

    check(scroll({0, 10}, 0, 9, 24, 80) == Viewport{0, 9},
          "test_scroll_left_snaps");
    check(scroll({0, 0}, 0, 80, 24, 80) == Viewport{0, 1},
          "test_scroll_right_one");
    check(scroll({0, 0}, 0, 200, 24, 80) == Viewport{0, 121},
          "test_jump_far_right");
    check(scroll({0, 40}, 0, 45, 24, 80) == Viewport{0, 40},
          "test_horizontal_visible_stays");

    check(scroll({5, 7}, 40, 100, 24, 80) == Viewport{17, 21},
          "test_both_axes_at_once");

    // Resize shrink: offsets were fine at 24 rows, screen is now 10.
    check(scroll({10, 0}, 25, 0, 10, 80) == Viewport{16, 0},
          "test_resize_shrink_reclamps");
    check(scroll({0, 0}, 0, 0, 1, 1) == Viewport{0, 0},
          "test_1x1_screen_origin");
    check(scroll({0, 0}, 7, 3, 1, 1) == Viewport{7, 3},
          "test_1x1_screen_tracks_cursor_exactly");
    return failed;
}
```

# Lesson: Files — Open, Save, Don't Lose Data {#files-and-saving}

An editor's one sacred duty: **never lose the user's work.** Loading is
easy; saving is where the sacred duty meets the operating system.

### Loading, and the line-ending question

Read the whole file (an editor's working set is the whole file anyway),
then split into the line vector. Splitting meets the oldest portability
scar in computing: Unix ends lines with `\n` (LF); Windows with `\r\n`
(CR LF) — a literal carriage-return-then-line-feed, teletype choreography
that outlived the teletype by half a century. Open a Windows file naively
and every line grows a trailing `\r` that renders as nothing, breaks
end-of-line motions, and pollutes every diff if you save.

The grown-up policy (vim's, roughly): **detect** the convention from the
file's first line break, **normalize** in memory (lines never contain
line terminators), and **restore** the convention on save. Two more
facts to preserve: whether the file ended with a final newline (POSIX
says a text file's last line ends with one, and build tools care —
editors that silently add or drop it create one-line diffs), and — for
the buffer invariant — an empty file still becomes `{""}`.

While we're at file edges: what if *stdin isn't the terminal* at all —
`git diff | vim -`? The editor reads the *file* from stdin, then opens
**`/dev/tty`** — a magic path that always names the process's
controlling terminal — to get a keyboard back. That's the tool for
"I need the terminal even though my fds are redirected"; the `isatty(0)`
check from Lesson 1 is how you notice you're in that situation.

### Saving: the rename trick

The naive save — open the file with `O_TRUNC`, write the buffer — has a
window of doom: after the truncate, before the write completes, the file
on disk is *empty or partial*. Crash there (power, OOM-kill, a full
disk mid-write) and the user's file is gone. The fix is one of the great
POSIX idioms, **write-temp-then-rename**:

```cpp
// 1. write the full contents to a temp file ON THE SAME DIRECTORY
int fd = open(".notes.txt.tmp", O_WRONLY | O_CREAT | O_EXCL, 0644);
write(fd, data, len);      // (in a loop; write can be partial)
fsync(fd);                 // 2. force it to stable storage
close(fd);
rename(".notes.txt.tmp", "notes.txt");   // 3. atomic replace
```

`rename(2)` is **atomic**: any observer — and any crash — sees either
the old complete file or the new complete file, never a mixture, never
an absence. The `fsync` before it matters just as much: without it the
rename can hit disk before the data does, and a crash leaves you a
perfectly renamed empty file. Same-directory matters too — `rename`
can't cross filesystems (`EXDEV`), which is why the temp file lives next
to the target, not in `/tmp`. (The costs: it breaks hard links and needs
a re-`chown`/`chmod` for exotic permissions — vim exposes this whole
tradeoff as the `backupcopy` option. No free lunch, but the default is
clear.)

The **dirty flag** drives the UX around all this: set on every buffer
edit, cleared on save, consulted by quit ("unsaved changes! press Ctrl-Q
again to discard"), displayed as the `[+]` your status bar already
renders. It's a one-bit summary of "does the buffer differ from disk" —
cheap because it can only go stale in the safe direction (an edit
followed by its exact inverse still reads dirty; annoying, never
dangerous).

The syscalls are your editor's job; the graded core is the codec — the
detect/normalize/restore logic, plus round-trip fidelity, which is where
line-ending bugs actually live.

## Challenge: The Line Codec {#line-codec points=15}

Implement the load/save text transforms:

- `load_text(text)` → `LoadResult{lines, crlf, trailing_newline}`:
  - Split on `\n`; a `\r` immediately before a `\n` is not part of the
    line's content.
  - `crlf` is true iff the **first** line break in the text is `\r\n`
    (that convention is then assumed for the whole file, vim-style).
    No line breaks → false.
  - `trailing_newline` is true iff the text ends with a line break;
    that final break does *not* produce an extra empty line.
  - Empty text → `{{""}, false, false}` (the buffer invariant).
  - A `\r` *not* followed by `\n` is ordinary content (classic-Mac
    files are 25 years dead; we keep the byte rather than guess).
- `save_text(lines, crlf, trailing_newline)`: join with `"\r\n"` or
  `"\n"`, append a final break iff `trailing_newline`. Must be the
  exact inverse: `save_text` of a `load_text` reproduces the original
  bytes for any input without stray `\r`s.

### Starter

```cpp
#include <string>
#include <string_view>
#include <vector>

struct LoadResult {
    std::vector<std::string> lines;
    bool crlf = false;
    bool trailing_newline = false;
};

LoadResult load_text(std::string_view text) {
    // TODO: split on \n (dropping a preceding \r), detect the first
    // break's convention, note the trailing break, normalize "" to {""}.
    (void)text;
    return {};
}

std::string save_text(const std::vector<std::string>& lines, bool crlf,
                      bool trailing_newline) {
    // TODO: join with the right break; final break iff trailing_newline.
    (void)lines; (void)crlf; (void)trailing_newline;
    return "";
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

using SV = std::vector<std::string>;

int main() {
    {
        LoadResult r = load_text("");
        check(r.lines == SV{""} && !r.crlf && !r.trailing_newline,
              "test_empty_file_is_one_empty_line");
    }
    {
        LoadResult r = load_text("a\nb\n");
        check(r.lines == SV{"a", "b"}, "test_unix_lines");
        check(!r.crlf, "test_unix_not_crlf");
        check(r.trailing_newline, "test_unix_trailing");
    }
    {
        LoadResult r = load_text("a\nb");
        check(r.lines == SV{"a", "b"} && !r.trailing_newline,
              "test_no_trailing_newline");
    }
    {
        LoadResult r = load_text("a\r\nb\r\n");
        check(r.lines == SV{"a", "b"}, "test_crlf_lines_stripped");
        check(r.crlf, "test_crlf_detected");
        check(r.trailing_newline, "test_crlf_trailing");
    }
    {
        LoadResult r = load_text("\n");
        check(r.lines == SV{""} && r.trailing_newline && !r.crlf,
              "test_single_newline");
    }
    {
        LoadResult r = load_text("\n\n");
        check(r.lines == SV{"", ""} && r.trailing_newline,
              "test_blank_lines_kept");
    }
    {
        LoadResult r = load_text("a\rb");
        check(r.lines == SV{"a\rb"}, "test_lone_cr_is_content");
    }
    {
        // Mixed file: first break decides the convention.
        LoadResult r = load_text("a\r\nb\nc");
        check(r.crlf, "test_first_break_wins");
        check(r.lines == SV{"a", "b", "c"}, "test_mixed_still_splits");
    }
    {
        LoadResult r = load_text("x");
        check(r.lines == SV{"x"} && !r.crlf && !r.trailing_newline,
              "test_single_line_no_breaks");
    }

    check(save_text({"a", "b"}, false, true) == "a\nb\n",
          "test_save_unix");
    check(save_text({"a", "b"}, true, true) == "a\r\nb\r\n",
          "test_save_crlf");
    check(save_text({"a", "b"}, false, false) == "a\nb",
          "test_save_no_trailing");
    check(save_text({""}, false, false) == "", "test_save_empty_buffer");
    check(save_text({""}, false, true) == "\n",
          "test_save_one_empty_line_with_break");

    {
        // Round-trip fidelity, both directions.
        const char* files[] = {"", "a\nb\n", "a\r\nb\r\nc\r\n", "one",
                               "x\ry\n", "\n\n\n", "a\r\nb\r\n\r\n"};
        bool ok = true;
        for (const char* f : files) {
            LoadResult r = load_text(f);
            ok = ok &&
                 save_text(r.lines, r.crlf, r.trailing_newline) == f;
        }
        check(ok, "test_roundtrip_bytes");
    }
    return failed;
}
```

# Lesson: Incremental Search {#incremental-search}

Every editor needs `/pattern`. The modern refinement — vim's `incsearch`,
now everyone's default — is **incremental** search: the view jumps to the
first match after every keystroke of the pattern, before you press
Enter. The UX loop lives in your editor (a mini input loop reading into
the query string, rendering each frame with the candidate match
highlighted, Esc restoring the pre-search cursor — save it before you
start!). The engine underneath is one pure function, and that's the
graded part:

```
find the next match for `needle`, starting from the cursor,
in this direction, wrapping around the ends of the file
```

Design decisions worth making explicit, because each is a behavior users
have 45 years of muscle memory about:

- **Strictly after.** Searching forward from a cursor *on* a match must
  find the *next* one — otherwise pressing `n` (repeat search) pins you
  in place forever. So the scan starts one position after the cursor
  (one before, going backward).
- **Wraparound.** Hitting the end of the file continues from the top
  (vi flashes "search hit BOTTOM, continuing at TOP"). The cursor
  position itself is the last candidate checked — so if the file
  contains exactly one match and you're standing on it, `n` finds *it*
  again (having gone all the way around), not "no match".
- **A match is a starting position.** Matches don't span lines (needle
  `"ab"` never matches across `"...a"` / `"b..."` — true to vi, where
  patterns are line-oriented), and overlapping matches count: `"aa"`
  occurs twice in `"aaa"`, at columns 0 and 1.

The workhorse inside is `std::string::find(needle, pos)` and its mirror
`rfind` — but the interesting code is the *scan order*: a forward search
from `(row, col)` must visit, in order: the rest of that row, the rows
below, then (wrapping) the rows above, and finally the beginning of the
starting row up to and including the cursor. Backward is the exact
mirror. Get the boundary arithmetic right at the seams — last column of
a row, the wrap point, the final partial row — and the tests below walk
every seam.

`std::optional<Pos>` is again the honest return type: "no match" is a
normal outcome (the user typed a needle that isn't there — the editor
shows "pattern not found" and stays put), not an exception, not a
sentinel `Pos{-1,-1}` that someone forgets to check.

## Challenge: Search with Wraparound {#search-wrap points=20}

Implement `search(lines, needle, from, forward)`:

- Returns the position of the first character of the nearest match
  **strictly after** `from` (forward) or **strictly before** it
  (backward), scanning with wraparound through the entire file; the
  match starting exactly at `from`, if any, is found *last* (after a
  full loop).
- "After" and "before" order positions by `(row, col)`, matches ordered
  by their starting position; on a backward scan the nearest match is
  the one with the *greatest* starting position less than `from`.
- Empty needle → `std::nullopt` (vi treats bare `/` as "repeat last
  search"; with no history there's nothing to do). No match anywhere →
  `std::nullopt`.
- `from` is a valid cursor position; matches may start at any column
  `c` with `c + needle.size() <= line.size()`.

This is the function your editor calls on every keystroke of the
incremental query (always from the *saved* pre-search cursor, so the
candidate match doesn't run away as you type) and on every `n`/`N`
(from the current cursor, direction flipped for `N`).

### Starter

```cpp
#include <cstddef>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};

std::optional<Pos> search(const std::vector<std::string>& lines,
                          std::string_view needle, Pos from,
                          bool forward) {
    // TODO: scan rows in wrap order; within the starting row respect
    // the strictly-after/strictly-before boundary; the position `from`
    // itself is checked last.
    (void)lines; (void)needle; (void)from; (void)forward;
    return std::nullopt;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

static bool at(std::optional<Pos> r, std::size_t row, std::size_t col) {
    return r && *r == Pos{row, col};
}

int main() {
    std::vector<std::string> text = {
        "the cat sat",     // 0
        "on the mat",      // 1
        "category cat",    // 2
        "",                // 3
        "the end",         // 4
    };

    check(at(search(text, "cat", {0, 0}, true), 0, 4),
          "test_forward_same_line");
    check(at(search(text, "cat", {0, 4}, true), 2, 0),
          "test_forward_skips_current_match");
    check(at(search(text, "cat", {2, 0}, true), 2, 9),
          "test_forward_two_matches_same_line");
    check(at(search(text, "cat", {2, 9}, true), 0, 4),
          "test_forward_wraps_to_top");
    check(at(search(text, "the", {4, 0}, true), 0, 0),
          "test_forward_wrap_from_last_row");

    check(at(search(text, "cat", {2, 9}, false), 2, 0),
          "test_backward_same_line");
    check(at(search(text, "cat", {2, 0}, false), 0, 4),
          "test_backward_previous_match");
    check(at(search(text, "cat", {0, 4}, false), 2, 9),
          "test_backward_wraps_to_bottom");
    check(at(search(text, "the", {0, 0}, false), 4, 0),
          "test_backward_from_origin_wraps");

    {
        std::vector<std::string> one = {"only one needle here"};
        check(at(search(one, "needle", {0, 9}, true), 0, 9),
              "test_unique_match_found_after_full_wrap");
        check(at(search(one, "needle", {0, 9}, false), 0, 9),
              "test_unique_match_backward_full_wrap");
    }
    {
        std::vector<std::string> aaa = {"aaa"};
        check(at(search(aaa, "aa", {0, 0}, true), 0, 1),
              "test_overlapping_matches");
    }

    check(!search(text, "zebra", {0, 0}, true), "test_no_match");
    check(!search(text, "", {0, 0}, true), "test_empty_needle");
    check(!search(text, "cat sat on", {0, 0}, true),
          "test_no_cross_line_match");

    {
        // Match at the very end of a line.
        std::vector<std::string> t = {"xxab", "cd"};
        check(at(search(t, "ab", {0, 0}, true), 0, 2),
              "test_match_at_line_end");
        check(at(search(t, "cd", {0, 3}, true), 1, 0),
              "test_match_fills_whole_line");
    }
    {
        // Needle longer than some lines must not read out of bounds.
        std::vector<std::string> t = {"x", "longer line x", "y"};
        check(at(search(t, "longer", {0, 0}, true), 1, 0),
              "test_needle_longer_than_short_lines");
    }
    {
        std::vector<std::string> empty_buf = {""};
        check(!search(empty_buf, "a", {0, 0}, true),
              "test_empty_buffer");
    }
    return failed;
}
```

# Lesson: Modal Editing — Modes {#modal-editing-modes}

Now the vi heart of the course. Everything so far — raw input, painted
frames, a buffer — could become a notepad: keys insert themselves,
arrows move. vi's founding idea is stranger and, once learned,
unshakeable: **the same key means different things depending on mode.**
In *normal* mode the keyboard is a command console — `x` deletes, `w`
hops a word, nothing inserts. In *insert* mode it's a typewriter. In
*command-line* mode (after `:`) you're typing an instruction to be
executed on Enter.

The lineage explains the shape. Bill Joy grew vi (1976) out of the line
editor `ex`, itself descended from `ed` — on printing terminals, where
"editing" *was* a command language (`ed` still answers your typos with
a lone `?`). When video terminals arrived, Joy put a live viewport on
top of ex; the command-language soul stayed. The hardware left
fingerprints too: Joy's terminal was a Lear Siegler **ADM-3A**, whose
keyboard had arrows printed on H, J, K, L — that's why those keys move
the cursor — and whose Esc key sat where Tab does today, an easy pinky
reach. Modern keyboards moved Esc; the mode-switch key stayed, and
generations of vi users remap Caps Lock to chase it. `:wq`, hjkl, the
`~` — none of it is arbitrary; all of it is 1976 preserved in muscle
memory.

Why does modality survive? Because it turns editing into a
**composable language**. Next lesson: motions as nouns. The one after:
operators as verbs, and `d w` — "delete a word" — as a sentence. None of
that grammar works if every printable key is busy meaning itself, which
is exactly what insert mode is *for*: a mode where keys mean themselves,
entered deliberately, left with Esc.

### The mode machine

Mechanically, modes are a small state machine that sits *in front of*
everything you've built: each decoded `Key` is dispatched first on the
current mode. The transitions:

- **Normal → Insert**: `i` (insert here), `I` (insert at first
  non-blank), `a` (append after cursor), `A` (append at end of line),
  `o` (open a line below), `O` (open above). Six doors into the same
  mode, differing only in where they put the cursor first — the cursor
  moves are the editor core's job (final challenge); the *transition* is
  the machine's.
- **Insert → Normal**: Esc. The only door out. (vi's deep bet: you
  spend most of your time in normal mode, so *leaving* insert must be
  one keystroke, always the same one.)
- **Normal → Command**: `:` opens the command line at the bottom of the
  screen. Keys now build up a string, shown as you type. **Enter**
  executes it; **Esc** abandons it; **Backspace** erases — and
  backspacing past the start *cancels back to normal mode*, a small
  authentic vi behavior the tests check.

`std::visit` earns its keep here: dispatching a `Key` variant means
handling each alternative, and the **overloaded-lambdas idiom** is the
standard C++ pattern for it:

```cpp
template <class... Ts> struct overloaded : Ts... { using Ts::operator()...; };

std::visit(overloaded{
    [&](char c)        { /* printable key  */ },
    [&](CtrlKey k)     { /* control chord  */ },
    [&](SpecialKey k)  { /* Esc, Enter ... */ },
}, key);
```

Forget one alternative and it *doesn't compile* — the sum-type payoff:
the compiler enforces that every kind of key has a decided meaning in
every mode. An `if`/`else` chain over `holds_alternative` compiles fine
with a case missing; the visit does not.

The machine below returns the executed command string and leaves
*interpreting* it to a parser — the second challenge, where
`std::variant` appears on the *output* side: a parsed command is
one-of-several shapes, exactly what a variant is for.

## Challenge: The Mode Machine {#mode-machine points=15}

Implement `ModeMachine`, dispatching keys per the transitions above.

- `mode()` starts as `Mode::Normal`.
- `feed(key)` processes one key and returns
  `std::optional<std::string>`: engaged exactly when a command line was
  submitted (Enter in command mode), carrying its text (without the
  `:`). All other feeds return `std::nullopt`.
- **Normal**: `i I a A o O` → Insert. `:` → Command with empty pending
  text. Every other key: stays normal (your editor core will interpret
  them; the machine ignores them).
- **Insert**: `SpecialKey::Escape` → Normal; everything else stays.
- **Command**: printable `char` → append to the pending text.
  `SpecialKey::Enter` → submit: return the text, clear it, back to
  Normal. `SpecialKey::Escape` → abandon: clear, back to Normal, return
  nullopt. `SpecialKey::Backspace` → erase the last pending char; if
  the pending text is already empty, cancel back to Normal. Other keys:
  ignored.
- `pending()` returns the command line being typed (empty outside
  command mode) — your render loop draws it in the message line.

### Starter

```cpp
#include <optional>
#include <string>
#include <variant>

enum class SpecialKey {
    ArrowUp, ArrowDown, ArrowLeft, ArrowRight,
    Home, End, PageUp, PageDown,
    Delete, Backspace, Enter, Escape, Tab,
};

struct CtrlKey {
    char letter;
    bool operator==(const CtrlKey&) const = default;
};

using Key = std::variant<char, CtrlKey, SpecialKey>;

enum class Mode { Normal, Insert, Command };

class ModeMachine {
public:
    Mode mode() const { return mode_; }

    std::string pending() const { return pending_; }

    std::optional<std::string> feed(const Key& key) {
        // TODO: dispatch on mode_, then on the key (std::visit or
        // holds_alternative/get — visit catches missing cases).
        (void)key;
        return std::nullopt;
    }

private:
    Mode mode_ = Mode::Normal;
    std::string pending_;
};
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

static void type(ModeMachine& m, const std::string& s) {
    for (char c : s) m.feed(Key{c});
}

int main() {
    {
        ModeMachine m;
        check(m.mode() == Mode::Normal, "test_starts_in_normal");
        m.feed(Key{'i'});
        check(m.mode() == Mode::Insert, "test_i_enters_insert");
        m.feed(Key{'x'});
        check(m.mode() == Mode::Insert, "test_typing_stays_insert");
        m.feed(Key{SpecialKey::Escape});
        check(m.mode() == Mode::Normal, "test_escape_leaves_insert");
    }
    {
        bool ok = true;
        for (char door : std::string("iIaAoO")) {
            ModeMachine m;
            m.feed(Key{door});
            ok = ok && m.mode() == Mode::Insert;
        }
        check(ok, "test_all_six_insert_doors");
    }
    {
        ModeMachine m;
        m.feed(Key{'x'});
        m.feed(Key{'j'});
        check(m.mode() == Mode::Normal, "test_other_keys_stay_normal");
    }
    {
        ModeMachine m;
        m.feed(Key{':'});
        check(m.mode() == Mode::Command, "test_colon_enters_command");
        check(m.pending() == "", "test_command_starts_empty");
        type(m, "wq");
        check(m.pending() == "wq", "test_pending_accumulates");
        auto r = m.feed(Key{SpecialKey::Enter});
        check(r && *r == "wq", "test_enter_submits_command");
        check(m.mode() == Mode::Normal, "test_submit_returns_to_normal");
        check(m.pending() == "", "test_submit_clears_pending");
    }
    {
        ModeMachine m;
        m.feed(Key{':'});
        type(m, "q!");
        m.feed(Key{SpecialKey::Escape});
        check(m.mode() == Mode::Normal, "test_escape_abandons_command");
        m.feed(Key{':'});
        auto r = m.feed(Key{SpecialKey::Enter});
        check(r && *r == "", "test_abandoned_text_not_resubmitted");
    }
    {
        ModeMachine m;
        m.feed(Key{':'});
        type(m, "wx");
        m.feed(Key{SpecialKey::Backspace});
        check(m.pending() == "w", "test_backspace_erases");
        m.feed(Key{SpecialKey::Backspace});
        check(m.pending() == "" && m.mode() == Mode::Command,
              "test_backspace_to_empty_stays");
        m.feed(Key{SpecialKey::Backspace});
        check(m.mode() == Mode::Normal,
              "test_backspace_past_start_cancels");
    }
    {
        ModeMachine m;
        m.feed(Key{'i'});
        auto r = m.feed(Key{':'});
        check(m.mode() == Mode::Insert && !r,
              "test_colon_in_insert_is_just_a_char");
    }
    {
        ModeMachine m;
        m.feed(Key{':'});
        m.feed(Key{SpecialKey::ArrowUp});
        check(m.mode() == Mode::Command && m.pending() == "",
              "test_command_ignores_special_keys");
        auto r = m.feed(Key{CtrlKey{'c'}});
        check(!r && m.pending() == "", "test_command_ignores_ctrl_keys");
    }
    {
        ModeMachine m;
        auto r = m.feed(Key{SpecialKey::Escape});
        check(m.mode() == Mode::Normal && !r,
              "test_escape_in_normal_is_noop");
    }
    return failed;
}
```

## Challenge: Parse the Command Line {#ex-parse points=10}

The mode machine hands you `"wq"` or `"w notes.txt"` or `"42"`. Now
interpret it. The output is a textbook sum type: a command is *one of*
write / quit / write-and-quit / go-to-line, each carrying different
data — `std::variant` on the return side, wrapped in `std::optional`
because the input might be gibberish.

Implement `parse_ex(input)` (input arrives *without* the leading `:`):

- `"w"` → `WriteCmd{""}` (write to the current file). `"w <name>"` →
  `WriteCmd{name}` — name is everything after the first space (may
  itself contain spaces; filenames are like that).
- `"q"` → `QuitCmd{false}`; `"q!"` → `QuitCmd{true}` (force: discard
  changes).
- `"wq"` or `"x"` → `WriteQuitCmd{}`.
- A string of digits → `GotoCmd{n}` with `n ≥ 1` (`:42` jumps to line
  42; `:0` is invalid in our dialect — reject it).
- Anything else — empty string, unknown words, `"w"` with trailing junk
  like `"wfoo"`, digits with a suffix — → `std::nullopt`; the editor
  shows "not an editor command".

### Starter

```cpp
#include <cstddef>
#include <optional>
#include <string>
#include <string_view>
#include <variant>

struct WriteCmd {
    std::string filename; // empty = current file
    bool operator==(const WriteCmd&) const = default;
};
struct QuitCmd {
    bool force = false;
    bool operator==(const QuitCmd&) const = default;
};
struct WriteQuitCmd {
    bool operator==(const WriteQuitCmd&) const = default;
};
struct GotoCmd {
    std::size_t line = 1; // 1-based
    bool operator==(const GotoCmd&) const = default;
};

using ExCommand = std::variant<WriteCmd, QuitCmd, WriteQuitCmd, GotoCmd>;

std::optional<ExCommand> parse_ex(std::string_view input) {
    // TODO: exact matches first (q, q!, wq, x), then "w [name]",
    // then all-digits; otherwise nullopt.
    (void)input;
    return std::nullopt;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    {
        auto r = parse_ex("w");
        check(r && *r == ExCommand{WriteCmd{""}}, "test_write");
    }
    {
        auto r = parse_ex("w notes.txt");
        check(r && *r == ExCommand{WriteCmd{"notes.txt"}},
              "test_write_with_filename");
    }
    {
        auto r = parse_ex("w my file.txt");
        check(r && *r == ExCommand{WriteCmd{"my file.txt"}},
              "test_filename_may_contain_spaces");
    }
    {
        auto r = parse_ex("q");
        check(r && *r == ExCommand{QuitCmd{false}}, "test_quit");
    }
    {
        auto r = parse_ex("q!");
        check(r && *r == ExCommand{QuitCmd{true}}, "test_quit_force");
    }
    check(parse_ex("wq") && *parse_ex("wq") == ExCommand{WriteQuitCmd{}},
          "test_write_quit");
    check(parse_ex("x") && *parse_ex("x") == ExCommand{WriteQuitCmd{}},
          "test_x_is_write_quit");
    {
        auto r = parse_ex("42");
        check(r && *r == ExCommand{GotoCmd{42}}, "test_goto_line");
    }
    {
        auto r = parse_ex("1");
        check(r && *r == ExCommand{GotoCmd{1}}, "test_goto_first_line");
    }
    check(!parse_ex(""), "test_empty_rejected");
    check(!parse_ex("0"), "test_goto_zero_rejected");
    check(!parse_ex("zz"), "test_unknown_rejected");
    check(!parse_ex("wfoo"), "test_w_needs_space_before_name");
    check(!parse_ex("q !"), "test_q_bang_no_space");
    check(!parse_ex("12x"), "test_digits_with_suffix_rejected");
    check(!parse_ex("wq!"), "test_wq_bang_not_in_dialect");
    check(!parse_ex(" w"), "test_leading_space_rejected");
    return failed;
}
```

# Lesson: Motions — the Nouns {#motions}

In vi's grammar, a **motion** is a noun phrase: a place in the text,
named relative to the cursor. `w` — the next word. `$` — the end of the
line. `G` — the last line. Pressed alone, a motion *moves the cursor
there*. The deeper payoff comes next lesson, when the same nouns become
the objects of verbs (`d w`: delete *to* the next word) — which is
exactly why motions must be **pure functions** `(buffer, position,
count) → position`, with no side effects: the operator machinery will
call them to *measure* text without moving anything.

One invariant governs everything: in normal mode the cursor sits **on**
a character, never past the last one. Valid columns are `0 ..
max(0, len-1)` (an empty line parks the cursor at column 0 — the one
place it sits on nothing). Insert mode relaxes this — the cursor may sit
at `len`, "after everything" — which is why `a` (append) at the end of a
line can insert where normal mode won't stand. Every motion ends by
re-imposing the clamp.

The core motions, and the details that make them feel right:

- **`h` `l`** — left/right, clamped to the line. No line wrapping:
  authentic vi `l` at line end just stops. (vim's `whichwrap` option
  exists precisely because people argue about this.)
- **`j` `k`** — down/up, and here's the classic subtlety: moving from a
  long line to a short one must **clamp the column** to the short
  line's end. vim actually remembers the "goal column" so j-j through a
  short line pops back out to the original column; we make the honest
  simplification of clamping without memory (the final challenge keeps
  it too, and upgrading is a great post-course exercise).
- **`0`** — column zero, unconditionally. **`^`** — the first
  *non-blank* (indentation-aware "start of line"; on an all-blank line
  there's nothing non-blank, so it clamps to the last column). The
  difference matters in real code: `0` on `"    return x;"` reaches
  column 0, `^` reaches the `r`.
- **`$`** — the last character. With a count, `2$` means "end of the
  *next* line" (count minus one rows down, then end) — a real vi
  refinement the tests cover.
- **`G`** — with no count, the last line; with a count, *that line*
  (1-based: `42G` = line 42, clamped to the file). **`gg`** — the same
  with the default flipped: no count means line one. (Real vi moves to
  the first non-blank after `G`; we keep the column, clamped —
  modern-editor behavior, and one less moving part.)
- **Counts**: `5j` is `j` five times; `0`, `^`, `$` treat counts their
  own way (`0` and `^` ignore them). A count of 0 in our API means "no
  count given" — vi cannot even express a count of zero, since `0` is
  itself a motion; our implementation honors that piece of grammar
  trivia directly in the representation.

### Word motions: the fiddliest fifty lines in vi

`w`, `b`, `e` — forward word, back word, end of word — are where "word"
needs a definition, and vi's is precise. Characters divide into three
**classes**: *word* characters (letters, digits, underscore — C
identifier characters, not a coincidence), *punctuation* (every other
non-blank), and *whitespace*. A **word** is a maximal run of a single
non-blank class. In `foo_bar+=42`:

```
foo_bar   +=   42        three words: word-class, punct-class, word-class
```

So `w` from `f` lands on `+`; `w` again lands on `4`. Punctuation runs
are words *of their own class* — that's what makes `w` useful in code,
hopping `->`, `::`, `+=` as single units. (vi also has `W`/`B`/`E` —
WORD motions, whitespace-delimited only, one class instead of two — an
easy extension once small-word logic works.)

The rules that complete the spec:

- **`w`**: skip the rest of the current word (if on one), then skip
  blanks, land on the first character of the next word. **`e`**: move
  at least one character, skip blanks, land on the *last* character of
  the current/next word. **`b`**: mirror of `e`, backward: land on the
  *first* character of the current/previous word.
- **Lines end, words end.** A word never spans a line break: the break
  acts as whitespace between the last word of one line and the first of
  the next. All three motions cross lines freely.
- **Empty lines are words** — for `w` and `b`, which stop on them; `e`
  skips them entirely. (Try it in vim: `w` through a paragraph break
  pauses on the blank line, `e` sails past.) This asymmetry is
  authentic, ancient, and pinned by the tests.
- **At the edges**: a motion that cannot move (e.g. `w` at the last
  word's last character... actually anywhere in the file's final
  stretch) returns its input position — the caller sees "no movement"
  rather than an error.

Implementation advice: model the line break as a *virtual whitespace
character* sitting one column past each line's last character (except
the final line). The starter's `next_pos`/`prev_pos` helpers implement
exactly that iteration; with them, each motion is a short loop over
character classes, and all the line-crossing edge cases dissolve into
"the slot is whitespace". Getting these three functions right is the
single most re-used work in the course: the operator lesson *and* the
final challenge both build directly on them.

## Challenge: The Motion Engine {#motion-engine points=15}

Implement `apply_motion(lines, p, motion, count)` for the line motions:
`h l j k 0 ^ $ G g` (where `'g'` denotes `gg`), with the semantics and
clamping described above. `count == 0` means no count given. The
returned position always satisfies the normal-mode clamp; the input
position is always valid.

### Starter

```cpp
#include <cstddef>
#include <string>
#include <vector>

struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};

Pos apply_motion(const std::vector<std::string>& lines, Pos p,
                 char motion, int count) {
    // TODO: reps = count ? count : 1; per-motion logic; end with the
    // normal-mode clamp (col <= max(0, len-1)).
    (void)lines; (void)motion; (void)count;
    return p;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    std::vector<std::string> ls = {
        "  hello world",    // 0  (len 13, maxcol 12)
        "",                 // 1
        "third line here",  // 2  (len 15, maxcol 14)
        "last",             // 3  (len 4, maxcol 3)
    };

    check(apply_motion(ls, {0, 0}, 'l', 0) == Pos{0, 1}, "test_l");
    check(apply_motion(ls, {0, 0}, 'l', 5) == Pos{0, 5}, "test_l_count");
    check(apply_motion(ls, {0, 10}, 'l', 99) == Pos{0, 12},
          "test_l_clamps_at_line_end");
    check(apply_motion(ls, {1, 0}, 'l', 1) == Pos{1, 0},
          "test_l_on_empty_line");

    check(apply_motion(ls, {0, 5}, 'h', 0) == Pos{0, 4}, "test_h");
    check(apply_motion(ls, {0, 5}, 'h', 2) == Pos{0, 3}, "test_h_count");
    check(apply_motion(ls, {0, 5}, 'h', 99) == Pos{0, 0},
          "test_h_clamps_at_zero");

    check(apply_motion(ls, {0, 12}, 'j', 0) == Pos{1, 0},
          "test_j_clamps_col_on_short_line");
    check(apply_motion(ls, {0, 5}, 'j', 2) == Pos{2, 5}, "test_j_count");
    check(apply_motion(ls, {0, 5}, 'j', 99) == Pos{3, 3},
          "test_j_clamps_at_last_row_and_col");
    check(apply_motion(ls, {2, 14}, 'k', 99) == Pos{0, 12},
          "test_k_clamps_at_first_row_and_col");
    check(apply_motion(ls, {3, 0}, 'k', 1) == Pos{2, 0}, "test_k");

    check(apply_motion(ls, {2, 10}, '0', 0) == Pos{2, 0}, "test_0");
    check(apply_motion(ls, {2, 10}, '0', 7) == Pos{2, 0},
          "test_0_ignores_count");

    check(apply_motion(ls, {0, 12}, '^', 0) == Pos{0, 2},
          "test_caret_first_nonblank");
    check(apply_motion(ls, {2, 10}, '^', 0) == Pos{2, 0},
          "test_caret_no_indent");
    check(apply_motion(ls, {1, 0}, '^', 0) == Pos{1, 0},
          "test_caret_empty_line");
    {
        std::vector<std::string> blank = {"    "};
        check(apply_motion(blank, {0, 1}, '^', 0) == Pos{0, 3},
              "test_caret_all_blank_clamps_to_end");
    }

    check(apply_motion(ls, {2, 3}, '$', 0) == Pos{2, 14}, "test_dollar");
    check(apply_motion(ls, {0, 0}, '$', 2) == Pos{1, 0},
          "test_2dollar_is_end_of_next_line");
    check(apply_motion(ls, {0, 0}, '$', 99) == Pos{3, 3},
          "test_dollar_count_clamps_rows");
    check(apply_motion(ls, {1, 0}, '$', 0) == Pos{1, 0},
          "test_dollar_empty_line");

    check(apply_motion(ls, {0, 5}, 'G', 0) == Pos{3, 3},
          "test_G_default_last_line");
    check(apply_motion(ls, {0, 5}, 'G', 3) == Pos{2, 5},
          "test_G_count_is_line_number");
    check(apply_motion(ls, {0, 5}, 'G', 99) == Pos{3, 3},
          "test_G_count_clamped");
    check(apply_motion(ls, {3, 2}, 'g', 0) == Pos{0, 2},
          "test_gg_default_first_line");
    check(apply_motion(ls, {3, 2}, 'g', 2) == Pos{1, 0},
          "test_gg_count_is_line_number");
    return failed;
}
```

## Challenge: Word Motions {#word-motions points=20}

Implement `word_forward` (`w`), `word_back` (`b`), and `word_end` (`e`)
per the class rules above — one application each (counts are the
caller's loop). The starter provides the character classifier and the
virtual-newline position iterators; your job is the three class-run
loops. Inputs are valid normal-mode positions; outputs must be too.

### Starter

```cpp
#include <cstddef>
#include <optional>
#include <string>
#include <vector>

struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};

using Lines = std::vector<std::string>;

// Character classes: 0 = whitespace, 1 = word (alnum + '_'), 2 = punct.
inline int cls(char c) {
    if (c == ' ' || c == '\t') return 0;
    if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
        (c >= '0' && c <= '9') || c == '_')
        return 1;
    return 2;
}

// Class at p, where col == line length is the virtual newline slot
// (class 0). Only non-final lines have a slot (see next_pos).
inline int class_at(const Lines& ls, Pos p) {
    if (p.col >= ls[p.row].size()) return 0;
    return cls(ls[p.row][p.col]);
}

inline bool line_empty(const Lines& ls, Pos p) {
    return ls[p.row].empty();
}

// Steps forward one position. Non-final lines include the newline slot
// at col == size(); the final line ends at its last character.
inline std::optional<Pos> next_pos(const Lines& ls, Pos p) {
    if (p.row + 1 == ls.size()) {
        if (p.col + 1 < ls[p.row].size()) return Pos{p.row, p.col + 1};
        return std::nullopt;
    }
    if (p.col + 1 <= ls[p.row].size()) return Pos{p.row, p.col + 1};
    return Pos{p.row + 1, 0};
}

// Steps backward one position, landing on the previous line's last
// character (or col 0 if it is empty).
inline std::optional<Pos> prev_pos(const Lines& ls, Pos p) {
    if (p.col > 0) return Pos{p.row, p.col - 1};
    if (p.row == 0) return std::nullopt;
    std::size_t r = p.row - 1;
    return Pos{r, ls[r].empty() ? 0 : ls[r].size() - 1};
}

Pos word_forward(const Lines& ls, Pos p) {
    // TODO (w): skip the rest of the current word (unless on blank /
    // empty line — then step once); skip whitespace; stop on empty
    // lines; return p when the buffer runs out.
    (void)ls;
    return p;
}

Pos word_back(const Lines& ls, Pos p) {
    // TODO (b): step back once; stop on empty line; skip whitespace
    // backward; run back to the first char of that class run.
    (void)ls;
    return p;
}

Pos word_end(const Lines& ls, Pos p) {
    // TODO (e): step forward once; skip whitespace AND empty lines;
    // run forward to the last char of that class run.
    (void)ls;
    return p;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

int main() {
    Lines ls = {
        "int main(void) {",  // 0
        "",                  // 1
        "  foo_bar += 42;",  // 2
        "}",                 // 3
    };

    check(word_forward(ls, {0, 0}) == Pos{0, 4}, "test_w_word_to_word");
    check(word_forward(ls, {0, 4}) == Pos{0, 8}, "test_w_word_to_punct");
    check(word_forward(ls, {0, 8}) == Pos{0, 9}, "test_w_punct_to_word");
    check(word_forward(ls, {0, 13}) == Pos{0, 15},
          "test_w_skips_blank_between_puncts");
    check(word_forward(ls, {0, 15}) == Pos{1, 0},
          "test_w_stops_on_empty_line");
    check(word_forward(ls, {1, 0}) == Pos{2, 2},
          "test_w_from_empty_line");
    check(word_forward(ls, {2, 2}) == Pos{2, 10},
          "test_w_underscore_is_word_char");
    check(word_forward(ls, {2, 10}) == Pos{2, 13},
          "test_w_punct_run_is_one_word");
    check(word_forward(ls, {2, 15}) == Pos{3, 0},
          "test_w_crosses_line_break");
    check(word_forward(ls, {3, 0}) == Pos{3, 0},
          "test_w_at_buffer_end_stays");
    check(word_forward(ls, {0, 1}) == Pos{0, 4},
          "test_w_from_mid_word");

    check(word_back(ls, {0, 4}) == Pos{0, 0}, "test_b_to_previous_word");
    check(word_back(ls, {0, 9}) == Pos{0, 8}, "test_b_word_to_punct");
    check(word_back(ls, {0, 6}) == Pos{0, 4}, "test_b_mid_word_to_start");
    check(word_back(ls, {2, 2}) == Pos{1, 0},
          "test_b_stops_on_empty_line");
    check(word_back(ls, {1, 0}) == Pos{0, 15},
          "test_b_from_empty_line");
    check(word_back(ls, {3, 0}) == Pos{2, 15},
          "test_b_crosses_line_break");
    check(word_back(ls, {0, 0}) == Pos{0, 0},
          "test_b_at_buffer_start_stays");
    check(word_back(ls, {2, 6}) == Pos{2, 2},
          "test_b_underscore_run");

    check(word_end(ls, {0, 0}) == Pos{0, 2}, "test_e_end_of_word");
    check(word_end(ls, {0, 2}) == Pos{0, 7}, "test_e_from_word_end");
    check(word_end(ls, {2, 6}) == Pos{2, 8}, "test_e_mid_word");
    check(word_end(ls, {2, 8}) == Pos{2, 11},
          "test_e_lands_on_punct_run_end");
    check(word_end(ls, {0, 15}) == Pos{2, 8},
          "test_e_skips_empty_line");
    check(word_end(ls, {3, 0}) == Pos{3, 0},
          "test_e_at_buffer_end_stays");
    check(word_end(ls, {0, 12}) == Pos{0, 13},
          "test_e_word_then_punct_boundary");

    {
        Lines trail = {"ab "};
        check(word_forward(trail, {0, 1}) == Pos{0, 2},
              "test_w_trailing_blank_is_last_stop");
    }
    {
        Lines two = {"foo", "bar"};
        check(word_forward(two, {0, 2}) == Pos{1, 0},
              "test_w_newline_separates_words");
        check(word_end(two, {0, 2}) == Pos{1, 2},
              "test_e_crosses_to_next_word_end");
        check(word_back(two, {1, 0}) == Pos{0, 0},
              "test_b_crosses_to_word_start");
    }
    return failed;
}
```

# Lesson: Operators — the Verbs {#operators}

Now the sentence. In vi, `d` alone does nothing — it *waits*. The next
motion tells it what to act on: `dw` deletes to the next word, `d$` to
the end of the line, `dG` to the end of the file, `d2j` this line and
two below. One verb, every noun you know, and every noun you learn
later multiplies through the grammar for free. This is the design idea
that made vi immortal — commands aren't memorized as monoliths;
they're *composed* — and the reason our motions were pure functions:
`d` needs to ask "where would `w` go?" without moving anything.

Three verbs share all the machinery: **`d`** (delete), **`c`** (change:
delete, then enter insert mode in the hole), **`y`** (yank: copy,
change nothing). Plus the doubled forms — `dd`, `cc`, `yy` — meaning
"this whole line": vi's convention for "the operator applied to itself"
(a verb with no noun acts on lines).

### Ranges: charwise vs linewise, exclusive vs inclusive

Resolving `operator + motion` produces a **range**, and vi's ranges
come in two natures the range type must capture:

- **Charwise**: from one character position to another. `dw`, `d$`,
  `dh`. The subtle bit is vi's exclusive/inclusive split, straight from
  the POSIX vi spec: motions that go *to a place* (`w`, `b`, `0`, `h`,
  `l`) are **exclusive** — the character at the destination survives;
  motions that go *onto a thing* (`e`, `$` — the end of a word, the
  last character) are **inclusive** — the destination character dies
  too. `dw` from `f` in `foo bar` leaves `bar` (the `b` survives); `de`
  leaves ` bar` (the `o` dies). We normalize both into one canonical
  form: **start inclusive, end exclusive** — inclusive motions just add
  one to the end. Half-open ranges, same as STL iterators, same
  reasons: length is `end - start`, empty is `start == end`, no ±1
  guesswork downstream.
- **Linewise**: whole lines, regardless of columns. `dd`, and any
  operator with an up/down motion (`dj` deletes *two* full lines —
  yours and the one below; `dG`, `dgg` likewise whole-line spans). vi's
  rule: vertical motions make operators linewise. In the range this is
  a flag plus a row interval; columns are meaningless (we zero them).

Counts multiply through the grammar: `2dw` and `d2w` and `2d3w` are all
legal; the counts *multiply* (`2d3w` = delete six words) — the
resolver takes the product and applies the motion that many times.
For `G`/`gg` the count is a line number instead (grammar quirk,
faithfully copied: `d3G` deletes from here through line 3).

An operator whose range turns out **empty** — `dh` at column 0, `d$` on
an empty line — does nothing at all: `std::optional<Range>` again, and
the editor treats `nullopt` as "beep".

One vi special case is too load-bearing to skip: `dw` on the last word
of a line. Plain grammar says "delete to the next word's start" — on
the next line! — which would eat the line break. vi's actual rule
(vim documents it as: *an exclusive motion ending in column 1 retreats
to the end of the previous line*): when the `w` target lands on a later
row, the range's end **retreats** to the end of the row just before the
target. `dw` on the last word deletes to end of line; the newline
survives. A second edge: `w` with the buffer running out mid-word
(there *is* no next word start) — then the operator extends to the end
of the line, so `dw` on the file's final word still deletes it. Both
are in the challenge spec and tests.

### Applying the range

The second half is mechanical but detail-rich: given a resolved range,
**extract** the doomed text (that's the yank — vi always yanks what it
deletes, which is why `p` after `dd` is "move line"), **remove** it for
`d`/`c`, splice the seam for cross-line charwise ranges, and place the
cursor per the verb: `d` clamps to normal mode's rule on the resulting
line; `c` leaves the cursor *at the hole, unclamped* — insert mode is
about to begin, where sitting at `len` is legal; `y` doesn't edit at
all, cursor to the range start (yank-moves-to-start is why `yG` seems
to "jump": authentic). Linewise `c` (`cc`) has its own vi flavor: the
lines are deleted but an empty line is left open at the spot, cursor on
it — change means "make me type the replacement".

Deleting every line linewise leaves `{""}` — the buffer invariant from
the buffer lesson, honored by every code path that can empty the file.

## Challenge: Resolve Operator + Motion {#operator-range points=20}

Implement `resolve(lines, cur, op, motion, count)` →
`std::optional<Range>`:

- `motion == op` (`dd`/`cc`/`yy`): linewise, rows `cur.row` through
  `cur.row + reps - 1`, clamped to the file.
- `j` / `k`: linewise, `cur.row` through `cur.row ± reps`, clamped,
  normalized so `start.row <= end.row`.
- `G`: linewise, `cur.row` through line `count` (1-based; no count =
  last line), normalized. `g` (gg): same with no-count default line 1.
- Charwise, exclusive (`w`, `b`, `0`, `h`): apply the motion `reps`
  times (helpers provided); range between origin and destination,
  normalized to start < end, end exclusive. **`w` fixups**, in order:
  (a) target on a *later row* → retreat: end = `{target.row - 1,
  len(target.row - 1)}` (the column-1 retreat rule from the lesson);
  (b) target == cur, or target is not at a word start (the buffer ran
  out mid-word: its previous character exists, isn't blank, and has
  the same class) → extend: end = `{cur.row, len(cur.row)}`.
- `l` computes its own end: `{cur.row, min(cur.col + reps, len)}` —
  operator motions may stand one past the end of the line, which is
  exactly why `dl` deletes the character under the cursor.
- Charwise, inclusive (`e`, `$`): destination character is *included*
  — add one to the end column. `$` takes the count-minus-one-rows-down
  rule from the motion engine; on an empty target line → `nullopt`.
  For `e`, a target that could not move still takes the character
  under the cursor (`de` at the very end of the buffer eats it).
- Empty range → `std::nullopt` (charwise; a linewise range of one row
  is never empty). Linewise ranges: zero the cols.
- `count == 0` = no count; the caller already multiplied `2d3w` into
  `count = 6`.

The starter provides working `word_forward`/`word_back`/`word_end` and
`apply_motion` (your previous two challenges — here given, so this
challenge is purely about range algebra).

### Starter

```cpp
#include <algorithm>
#include <cstddef>
#include <optional>
#include <string>
#include <vector>

struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};

using Lines = std::vector<std::string>;

// ---- provided: solutions to the two motion challenges ----------------

inline int cls(char c) {
    if (c == ' ' || c == '\t') return 0;
    if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
        (c >= '0' && c <= '9') || c == '_')
        return 1;
    return 2;
}

inline int class_at(const Lines& ls, Pos p) {
    if (p.col >= ls[p.row].size()) return 0;
    return cls(ls[p.row][p.col]);
}

inline std::optional<Pos> next_pos(const Lines& ls, Pos p) {
    if (p.row + 1 == ls.size()) {
        if (p.col + 1 < ls[p.row].size()) return Pos{p.row, p.col + 1};
        return std::nullopt;
    }
    if (p.col + 1 <= ls[p.row].size()) return Pos{p.row, p.col + 1};
    return Pos{p.row + 1, 0};
}

inline std::optional<Pos> prev_pos(const Lines& ls, Pos p) {
    if (p.col > 0) return Pos{p.row, p.col - 1};
    if (p.row == 0) return std::nullopt;
    std::size_t r = p.row - 1;
    return Pos{r, ls[r].empty() ? 0 : ls[r].size() - 1};
}

inline Pos word_forward(const Lines& ls, Pos p) {
    int cl = class_at(ls, p);
    if (cl != 0 && !ls[p.row].empty()) {
        while (true) {
            auto q = next_pos(ls, p);
            if (!q) return p;
            p = *q;
            if (ls[p.row].empty()) return p;
            if (class_at(ls, p) != cl) break;
        }
    } else {
        auto q = next_pos(ls, p);
        if (!q) return p;
        p = *q;
        if (ls[p.row].empty()) return p;
    }
    while (class_at(ls, p) == 0) {
        auto q = next_pos(ls, p);
        if (!q) return p;
        p = *q;
        if (ls[p.row].empty()) return p;
    }
    return p;
}

inline Pos word_back(const Lines& ls, Pos p) {
    auto q = prev_pos(ls, p);
    if (!q) return p;
    p = *q;
    if (ls[p.row].empty()) return p;
    while (class_at(ls, p) == 0) {
        q = prev_pos(ls, p);
        if (!q) return p;
        p = *q;
        if (ls[p.row].empty()) return p;
    }
    int cl = class_at(ls, p);
    while (true) {
        q = prev_pos(ls, p);
        if (!q || ls[q->row].empty() || class_at(ls, *q) != cl) return p;
        p = *q;
    }
}

inline Pos word_end(const Lines& ls, Pos p) {
    auto q = next_pos(ls, p);
    if (!q) return p;
    p = *q;
    while (ls[p.row].empty() || class_at(ls, p) == 0) {
        q = next_pos(ls, p);
        if (!q) return p;
        p = *q;
    }
    int cl = class_at(ls, p);
    while (true) {
        q = next_pos(ls, p);
        if (!q || ls[q->row].empty() || class_at(ls, *q) != cl) return p;
        p = *q;
    }
}

inline Pos apply_motion(const Lines& lines, Pos p, char motion,
                        int count) {
    std::size_t reps = count > 0 ? static_cast<std::size_t>(count) : 1;
    std::size_t nrows = lines.size();
    auto maxcol = [&](std::size_t r) {
        return lines[r].empty() ? 0 : lines[r].size() - 1;
    };
    switch (motion) {
        case 'h': p.col = p.col >= reps ? p.col - reps : 0; break;
        case 'l': p.col = std::min(p.col + reps, maxcol(p.row)); break;
        case 'j':
            p.row = std::min(p.row + reps, nrows - 1);
            p.col = std::min(p.col, maxcol(p.row));
            break;
        case 'k':
            p.row = p.row >= reps ? p.row - reps : 0;
            p.col = std::min(p.col, maxcol(p.row));
            break;
        case '0': p.col = 0; break;
        case '^': {
            const std::string& L = lines[p.row];
            std::size_t i = 0;
            while (i < L.size() && (L[i] == ' ' || L[i] == '\t')) i++;
            p.col = i < L.size() ? i : maxcol(p.row);
            break;
        }
        case '$':
            p.row = std::min(p.row + reps - 1, nrows - 1);
            p.col = maxcol(p.row);
            break;
        case 'G':
            p.row = count > 0 ? std::min<std::size_t>(
                                    static_cast<std::size_t>(count),
                                    nrows) - 1
                              : nrows - 1;
            p.col = std::min(p.col, maxcol(p.row));
            break;
        case 'g':
            p.row = count > 0 ? std::min<std::size_t>(
                                    static_cast<std::size_t>(count),
                                    nrows) - 1
                              : 0;
            p.col = std::min(p.col, maxcol(p.row));
            break;
        default: break;
    }
    return p;
}

// ---- your work --------------------------------------------------------

struct Range {
    Pos start;               // charwise: inclusive
    Pos end;                 // charwise: exclusive; linewise: cols are 0
    bool linewise = false;
    bool operator==(const Range&) const = default;
};

std::optional<Range> resolve(const Lines& lines, Pos cur, char op,
                             char motion, int count) {
    // TODO: doubled op and j/k/G/g -> linewise row spans (normalized,
    // cols zeroed); w/b/0/h/l -> exclusive charwise; e/$ -> inclusive
    // (+1 col); empty -> nullopt.
    (void)lines; (void)cur; (void)op; (void)motion; (void)count;
    return std::nullopt;
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

static bool is(std::optional<Range> r, Range want) {
    return r && *r == want;
}

int main() {
    Lines ls = {
        "foo bar baz",  // 0
        "second",       // 1
        "third",        // 2
        "fourth",       // 3
    };

    check(is(resolve(ls, {1, 3}, 'd', 'd', 0),
             {{1, 0}, {1, 0}, true}),
          "test_dd_one_line");
    check(is(resolve(ls, {1, 3}, 'd', 'd', 2),
             {{1, 0}, {2, 0}, true}),
          "test_2dd_two_lines");
    check(is(resolve(ls, {2, 0}, 'y', 'y', 99),
             {{2, 0}, {3, 0}, true}),
          "test_yy_count_clamped");

    check(is(resolve(ls, {0, 2}, 'd', 'j', 0),
             {{0, 0}, {1, 0}, true}),
          "test_dj_two_lines_linewise");
    check(is(resolve(ls, {2, 4}, 'd', 'k', 0),
             {{1, 0}, {2, 0}, true}),
          "test_dk_normalizes_upward");
    check(is(resolve(ls, {1, 0}, 'd', 'G', 0),
             {{1, 0}, {3, 0}, true}),
          "test_dG_to_end_linewise");
    check(is(resolve(ls, {2, 3}, 'd', 'g', 0),
             {{0, 0}, {2, 0}, true}),
          "test_dgg_to_top");
    check(is(resolve(ls, {3, 0}, 'd', 'G', 2),
             {{1, 0}, {3, 0}, true}),
          "test_d2G_count_is_line_number");

    check(is(resolve(ls, {0, 0}, 'd', 'w', 0),
             {{0, 0}, {0, 4}, false}),
          "test_dw_exclusive");
    check(is(resolve(ls, {0, 0}, 'd', 'w', 2),
             {{0, 0}, {0, 8}, false}),
          "test_d2w_two_words");
    check(is(resolve(ls, {0, 8}, 'd', 'w', 0),
             {{0, 8}, {0, 11}, false}),
          "test_dw_last_word_stops_at_eol");
    check(is(resolve(ls, {0, 0}, 'd', 'e', 0),
             {{0, 0}, {0, 3}, false}),
          "test_de_inclusive");
    check(is(resolve(ls, {0, 4}, 'd', 'b', 0),
             {{0, 0}, {0, 4}, false}),
          "test_db_backward_normalized");
    check(is(resolve(ls, {0, 4}, 'd', '$', 0),
             {{0, 4}, {0, 11}, false}),
          "test_d_dollar_to_eol");
    check(is(resolve(ls, {0, 4}, 'd', '0', 0),
             {{0, 0}, {0, 4}, false}),
          "test_d0_to_line_start");
    check(is(resolve(ls, {0, 5}, 'd', 'h', 2),
             {{0, 3}, {0, 5}, false}),
          "test_d2h_left");
    check(is(resolve(ls, {0, 5}, 'd', 'l', 0),
             {{0, 5}, {0, 6}, false}),
          "test_dl_deletes_under_cursor");
    check(is(resolve(ls, {0, 10}, 'd', 'l', 5),
             {{0, 10}, {0, 11}, false}),
          "test_dl_clamps_at_eol");

    check(is(resolve(ls, {0, 10}, 'd', 'w', 0),
             {{0, 10}, {0, 11}, false}),
          "test_dw_at_last_char_of_line");
    {
        Lines last = {"one two"};
        check(is(resolve(last, {0, 4}, 'd', 'w', 0),
                 {{0, 4}, {0, 7}, false}),
              "test_dw_cannot_move_extends_to_eol");
    }

    check(!resolve(ls, {0, 0}, 'd', 'h', 0), "test_dh_at_col0_empty");
    check(!resolve(ls, {0, 0}, 'd', '0', 0), "test_d0_at_col0_empty");
    {
        Lines e = {""};
        check(!resolve(e, {0, 0}, 'd', '$', 0),
              "test_d_dollar_empty_line");
        check(!resolve(e, {0, 0}, 'd', 'l', 0), "test_dl_empty_line");
        check(is(resolve(e, {0, 0}, 'd', 'd', 0),
                 {{0, 0}, {0, 0}, true}),
              "test_dd_empty_line_still_linewise");
    }
    {
        // 2w from "baz" targets line 2 col 0; the retreat rule pulls
        // the end back to the end of line 1 -> "baz\nsecond" dies.
        check(is(resolve(ls, {0, 8}, 'd', 'w', 2),
                 {{0, 8}, {1, 6}, false}),
              "test_d2w_retreats_to_prev_line_end");
    }
    check(is(resolve(ls, {3, 5}, 'd', 'e', 0),
             {{3, 5}, {3, 6}, false}),
          "test_de_at_buffer_end_takes_char");
    return failed;
}
```

## Challenge: Apply the Operator {#apply-operator points=15}

Implement `apply_op(lines, op, r)` — execute a resolved `Range` and
return the `EditResult`:

- `yanked`: the extracted text, one vector entry per range row —
  linewise: full copies of each row; charwise same-row: the one
  segment; charwise multi-row: first row's tail, middle rows whole,
  last row's head (text before `end.col`).
- `'y'`: buffer untouched; cursor = range start (for linewise:
  `{start.row, 0}`).
- `'d'` charwise: delete `[start, end)`; a multi-row range splices
  first-head + last-tail into one line. Cursor: `start`, with the col
  clamped to the resulting line's normal-mode max.
- `'d'` linewise: remove the rows; `{""}` if that empties the buffer.
  Cursor: `{min(start.row, new_last_row), 0}`.
- `'c'` charwise: like `d` but the cursor col is *not* clamped
  (insert mode follows), and `enter_insert` is true.
- `'c'` linewise: the rows collapse to one **empty line** at
  `start.row`; cursor there, `enter_insert` true.
- `linewise` in the result mirrors the range (your editor uses it to
  decide how `p` will paste later).

Ranges arrive well-formed (from `resolve`): non-empty, normalized,
in-bounds.

### Starter

```cpp
#include <cstddef>
#include <string>
#include <vector>

struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};

struct Range {
    Pos start;
    Pos end;
    bool linewise = false;
    bool operator==(const Range&) const = default;
};

struct EditResult {
    std::vector<std::string> lines;
    Pos cursor;
    std::vector<std::string> yanked;
    bool linewise = false;
    bool enter_insert = false;
};

EditResult apply_op(std::vector<std::string> lines, char op,
                    const Range& r) {
    // TODO: extract yanked text; for d/c remove it (splice multi-row
    // charwise; keep {""} invariant for linewise); cursor per verb.
    (void)op; (void)r;
    return {std::move(lines), {}, {}, false, false};
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

using SV = std::vector<std::string>;

int main() {
    SV ls = {"foo bar baz", "second", "third", "fourth"};

    {
        EditResult r = apply_op(ls, 'd', {{0, 0}, {0, 4}, false});
        check(r.lines == SV{"bar baz", "second", "third", "fourth"},
              "test_dw_deletes_word");
        check(r.cursor == Pos{0, 0}, "test_dw_cursor_at_start");
        check(r.yanked == SV{"foo "}, "test_dw_yanks_deleted_text");
        check(!r.enter_insert && !r.linewise, "test_dw_flags");
    }
    {
        EditResult r = apply_op(ls, 'd', {{0, 4}, {0, 11}, false});
        check(r.lines[0] == "foo " && r.cursor == Pos{0, 3},
              "test_d_dollar_clamps_cursor");
    }
    {
        EditResult r = apply_op(ls, 'd', {{1, 0}, {2, 0}, true});
        check(r.lines == SV{"foo bar baz", "fourth"},
              "test_dd_linewise_removes_rows");
        check(r.cursor == Pos{1, 0}, "test_dd_cursor_row_kept");
        check(r.yanked == SV{"second", "third"},
              "test_dd_yanks_whole_lines");
        check(r.linewise, "test_dd_result_linewise");
    }
    {
        EditResult r = apply_op(ls, 'd', {{3, 0}, {3, 0}, true});
        check(r.lines == SV{"foo bar baz", "second", "third"},
              "test_dd_last_line");
        check(r.cursor == Pos{2, 0}, "test_dd_last_line_cursor_clamps");
    }
    {
        SV one = {"only"};
        EditResult r = apply_op(one, 'd', {{0, 0}, {0, 0}, true});
        check(r.lines == SV{""}, "test_dd_only_line_leaves_empty");
        check(r.cursor == Pos{0, 0}, "test_dd_only_line_cursor");
        check(r.yanked == SV{"only"}, "test_dd_only_line_yank");
    }
    {
        // Charwise across rows: "bar baz\nsec|ond" -> splice.
        EditResult r = apply_op(ls, 'd', {{0, 4}, {1, 3}, false});
        check(r.lines == SV{"foo ond", "third", "fourth"},
              "test_charwise_multirow_splices");
        check(r.yanked == SV{"bar baz", "sec"},
              "test_charwise_multirow_yank");
        check(r.cursor == Pos{0, 4}, "test_charwise_multirow_cursor");
    }
    {
        EditResult r = apply_op(ls, 'y', {{1, 0}, {2, 0}, true});
        check(r.lines == ls, "test_yank_changes_nothing");
        check(r.yanked == SV{"second", "third"}, "test_yank_lines");
        check(r.cursor == Pos{1, 0}, "test_yank_cursor_to_start");
        check(r.linewise && !r.enter_insert, "test_yank_flags");
    }
    {
        EditResult r = apply_op(ls, 'y', {{0, 4}, {0, 8}, false});
        check(r.lines == ls && r.yanked == SV{"bar "},
              "test_yank_charwise");
    }
    {
        EditResult r = apply_op(ls, 'c', {{0, 4}, {0, 11}, false});
        check(r.lines[0] == "foo ", "test_c_deletes_like_d");
        check(r.cursor == Pos{0, 4},
              "test_c_cursor_unclamped_for_insert");
        check(r.enter_insert, "test_c_enters_insert");
    }
    {
        EditResult r = apply_op(ls, 'c', {{1, 0}, {2, 0}, true});
        check(r.lines == SV{"foo bar baz", "", "fourth"},
              "test_cc_leaves_empty_line_open");
        check(r.cursor == Pos{1, 0} && r.enter_insert,
              "test_cc_cursor_on_open_line");
        check(r.yanked == SV{"second", "third"}, "test_cc_yank");
    }
    {
        // Deleting the entire only-line charwise leaves an empty line;
        // cursor clamps to col 0.
        SV one = {"xyz"};
        EditResult r = apply_op(one, 'd', {{0, 0}, {0, 3}, false});
        check(r.lines == SV{""} && r.cursor == Pos{0, 0},
              "test_charwise_delete_whole_line_content");
    }
    return failed;
}
```

# Lesson: Undo and Redo {#undo-and-redo}

An editor without undo is a threat, not a tool. Two classic designs:

- **Snapshots** (the memento pattern): before each change, save the
  whole buffer; undo = restore. Trivially correct — undo can never
  disagree with what happened, because it *is* what happened — and
  memory-hungry in proportion to buffer size, not edit size. For
  source-file-sized buffers, honestly fine, and the final challenge
  uses it for exactly that reason.
- **Inverse operations** (the command pattern): record each edit as a
  small object that knows how to reverse itself. An insert's inverse is
  "erase those characters"; an erase's inverse is "put this text back"
  — note that the erase record must therefore *carry the erased text*;
  the operation alone isn't invertible without it. Memory scales with
  the edits, which is why every serious editor lives here. The cost is
  a new invariant to defend: the records must replay against exactly
  the document states they were recorded against, in exactly reverse
  order. One position off and undo corrupts the file it was supposed
  to protect.

This lesson builds the second kind. Two stacks: **undo** holds done
things; **redo** holds undone things. `undo` pops, reverses, pushes to
redo. `redo` pops, re-applies, pushes back. And the rule everyone knows
from using editors without knowing they know it: **a fresh edit clears
the redo stack** — history is a line, not a tree, and editing after an
undo abandons the future you undid. (vim actually keeps the abandoned
branches — `:help undo-tree` — one of its deepest features, built on
exactly the representation you're about to write.)

### Granularity: coalescing

The subtle design decision is not *how* to undo but **how much**. Type
`hello` — five insert operations. Should undo peel back `o`, then `l`,
then `l`...? Every real editor says no: one undo removes the word. vi's
rule is that the whole insert-mode session — from `i` to Esc — is one
undo unit. The mechanism is **coalescing**: when a new insert arrives,
check whether it *extends* the unit on top of the undo stack, and if
so, merge instead of push. Extends means:

- the top unit is an insert (you can't merge typing into a deletion),
- nothing has broken the run since (leaving insert mode calls
  `break_run()` — that's the i-to-Esc boundary made explicit; cursor
  motions in a real editor would too),
- the new text lands **exactly at the end** of the top unit's text
  (`pos == top.pos + top.text.size()` — type in the middle of a word
  after moving the cursor and positions won't line up, correctly
  forcing a new unit),
- and the new text isn't a newline — we cut units at line breaks, so
  undoing a paragraph of typing goes line by line rather than
  vaporizing all of it. (vim keeps whole sessions; our dialect is
  line-grained. Both are defensible; ours makes the tests nicer and
  the behavior less destructive.)

Deletions could coalesce too (backspace-backspace-backspace as one
unit — note the positions *decrease*), but that's symmetric bookkeeping
you can add later; here, each erase is its own unit.

The document in this challenge is a plain `std::string` with byte
positions — undo logic is completely independent of the 2-D row/column
structure, and testing it in 1-D removes every distraction. In your
editor, the same class runs against the flattened buffer, or against
`Pos`-keyed edits; the algebra is identical.

## Challenge: The Undo History {#undo-history points=20}

Implement `UndoHistory` against `std::string` documents:

- `record_insert(pos, text)` — the editor just inserted `text` at
  `pos`. Coalesce per the four conditions above; otherwise push a new
  unit. Clears redo.
- `record_erase(pos, text)` — the editor just removed `text` from
  `pos`. Always a new unit. Clears redo.
- `break_run()` — end the current coalescing run (Esc pressed). No
  other effect; harmless when the stack is empty.
- `undo(doc)` — reverse the top unit against `doc` (erase what the
  unit inserted / re-insert what it erased), move it to the redo
  stack, return `true`. Empty stack: return `false`, touch nothing.
- `redo(doc)` — re-apply the most recently undone unit, move it back
  to the undo stack, return `true`; `false` if no redo available.
- `can_undo()` / `can_redo()`.

The tests drive a consistent document alongside the history — exactly
the discipline your editor must keep: **record precisely what you did,
when you did it.**

### Starter

```cpp
#include <cstddef>
#include <string>
#include <string_view>
#include <vector>

class UndoHistory {
public:
    void record_insert(std::size_t pos, std::string_view text) {
        // TODO: coalesce into the top insert unit when contiguous,
        // unbroken, and text != "\n"; else push. Clear redo.
        (void)pos; (void)text;
    }

    void record_erase(std::size_t pos, std::string_view text) {
        // TODO: always a new unit. Clear redo.
        (void)pos; (void)text;
    }

    void break_run() {
        // TODO: stop the current coalescing run.
    }

    bool undo(std::string& doc) {
        // TODO: reverse the top unit, move it to redo.
        (void)doc;
        return false;
    }

    bool redo(std::string& doc) {
        // TODO: re-apply the top redo unit, move it back to undo.
        (void)doc;
        return false;
    }

    bool can_undo() const { return false; } // TODO
    bool can_redo() const { return false; } // TODO

private:
    struct Edit {
        std::size_t pos = 0;
        bool insert = true;
        std::string text;
    };
    std::vector<Edit> undo_;
    std::vector<Edit> redo_;
    bool run_open_ = false; // top of undo_ may still coalesce
};
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

// Applies an insert to both the doc and the history, like an editor.
static void ins(UndoHistory& h, std::string& doc, std::size_t pos,
                std::string_view text) {
    doc.insert(pos, text);
    h.record_insert(pos, text);
}

static void del(UndoHistory& h, std::string& doc, std::size_t pos,
                std::size_t n) {
    std::string removed = doc.substr(pos, n);
    doc.erase(pos, n);
    h.record_erase(pos, removed);
}

int main() {
    {
        UndoHistory h;
        std::string doc;
        check(!h.can_undo() && !h.can_redo(), "test_starts_empty");
        check(!h.undo(doc) && !h.redo(doc),
              "test_undo_redo_on_empty_return_false");
    }
    {
        // Typing "abc" one char at a time is ONE undo unit.
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "a");
        ins(h, doc, 1, "b");
        ins(h, doc, 2, "c");
        check(doc == "abc", "test_doc_after_typing");
        check(h.undo(doc), "test_undo_returns_true");
        check(doc == "", "test_insert_run_coalesced");
        check(!h.can_undo(), "test_single_unit_consumed");
    }
    {
        // break_run splits units (the i ... Esc ... i boundary).
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "ab");
        h.break_run();
        ins(h, doc, 2, "cd");
        check(doc == "abcd", "test_doc_two_sessions");
        h.undo(doc);
        check(doc == "ab", "test_undo_peels_last_session");
        h.undo(doc);
        check(doc == "", "test_undo_peels_first_session");
    }
    {
        // Non-contiguous inserts don't coalesce: the second insert lands
        // BEFORE the first, so the end-of-run position doesn't line up.
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "a");
        ins(h, doc, 0, "b");     // inserted BEFORE previous text
        check(doc == "ba", "test_doc_noncontiguous");
        h.undo(doc);
        check(doc == "a", "test_noncontiguous_separate_units");
        h.undo(doc);
        check(doc == "", "test_noncontiguous_second_undo");
    }
    {
        // Newline starts a new unit: undo is line-grained.
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "a");
        ins(h, doc, 1, "\n");
        ins(h, doc, 2, "b");
        check(doc == "a\nb", "test_doc_with_newline");
        h.undo(doc);
        check(doc == "a\n", "test_undo_stops_at_newline");
        h.undo(doc);
        check(doc == "a", "test_newline_was_own_unit");
        h.undo(doc);
        check(doc == "", "test_first_line_unit");
    }
    {
        // Erase is always its own unit, and undo restores the text.
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "hello");
        del(h, doc, 1, 3);
        check(doc == "ho", "test_doc_after_erase");
        h.undo(doc);
        check(doc == "hello", "test_undo_erase_restores_text");
        h.undo(doc);
        check(doc == "", "test_undo_back_to_empty");
    }
    {
        // Insert after erase must not coalesce with the pre-erase run.
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "ab");
        del(h, doc, 1, 1);
        ins(h, doc, 1, "c");
        check(doc == "ac", "test_doc_ins_del_ins");
        h.undo(doc);
        check(doc == "a", "test_insert_after_erase_own_unit");
        h.undo(doc);
        check(doc == "ab", "test_erase_unit_reversed");
    }
    {
        // Redo round-trip, and redo invalidation on fresh edit.
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "one");
        h.break_run();
        ins(h, doc, 3, "two");
        h.undo(doc);
        check(doc == "one" && h.can_redo(), "test_redo_available");
        check(h.redo(doc) && doc == "onetwo", "test_redo_reapplies");
        h.undo(doc);
        h.undo(doc);
        check(doc == "", "test_double_undo");
        check(h.redo(doc) && doc == "one", "test_redo_in_order");
        check(h.redo(doc) && doc == "onetwo", "test_redo_second");
        check(!h.redo(doc), "test_redo_exhausted");

        h.undo(doc);
        ins(h, doc, 3, "X");
        check(!h.can_redo(), "test_new_edit_clears_redo");
        check(doc == "oneX", "test_doc_after_branch");
        h.undo(doc);
        check(doc == "one", "test_undo_after_branch");
    }
    {
        // A unit undone and redone can be undone again.
        UndoHistory h;
        std::string doc;
        ins(h, doc, 0, "zz");
        h.undo(doc);
        h.redo(doc);
        check(h.undo(doc) && doc == "", "test_undo_redo_undo");
    }
    return failed;
}
```

# Lesson: Syntax Highlighting {#syntax-highlighting}

The last subsystem before assembly. Syntax highlighting sounds like
"parse the language" and would be a terrible idea if it were: real
parsers demand valid programs, and an editor's buffer is *invalid
almost always* — you're typing, half the file is mid-keystroke. What
editors actually run (until you get to tree-sitter-class machinery) is
a **lexer-shaped state machine**: classify every character into a
handful of buckets — keyword, string, comment, number, normal — with
just enough state to get the annoying cases right.

Per *character*, because that's what rendering needs: the render loop
walks the line emitting SGR color codes (`ESC [ 3x m` — the same SGR
family as your status bar's inverted video) whenever the class changes
from one char to the next; a `std::vector<Hl>` parallel to the line is
exactly the right output shape. (`Hl` is an `enum class`, and the color
mapping lives elsewhere — classification and presentation separated,
so the tests can check classification without caring about colors.)

The interesting design decision comes from a nasty fact: `/* ... */`
**comments cross lines**. Whether line 500 starts inside a comment
depends, in principle, on every line above it. Rescanning the file per
keystroke is O(file) per frame; the editor answer is beautiful:
per-line highlighting with **one bit of carried state**. Each line's
highlight function takes "did we start inside a comment?" and returns
"did we end inside one?" — line *n*'s output feeding line *n+1*'s
input, like carry propagation in an adder. Your editor caches per-line
results and, after an edit to line *n*, re-highlights from *n*
downward only while the carry-out *changes* — type `/*` at the top of
the file and the repaint cascades; fix a typo inside a line and
re-highlighting stops after that one line. (Strings, in our dialect as
in C's grammar, do *not* cross lines — an unterminated string dies at
the newline, so the string flag is line-local and doesn't join the
carry.)

The classification rules, in precedence order — comment state beats
string state beats everything, because inside `/* */` a quote is just
punctuation, and inside `"..."` a `//` is just two slashes:

- Carrying a comment: chars are Comment until `*/` (both chars
  Comment), then back to normal scanning.
- In a string: chars are String; backslash escapes the next char (both
  String — `"a\"b"` doesn't end at the escaped quote); the closing
  `"` is String; end of line terminates the string without carry.
- `//` opens a comment to end of line; `/*` opens the carrying kind;
  `"` opens a string.
- **Numbers**: a digit run is Number if it *starts* after a separator
  (or at line start); a `.` between digits stays Number (`3.14`). A
  digit glued to a word (`x1`, `return42`) is Normal — highlighting
  `1` in `x1` as a number is the kind of half-right that's worse than
  wrong.
- **Keywords**: matched as whole words — separator (or line edge) on
  both sides. `int` in `printf` must not light up.
- A **separator** for these purposes: any char that's not a letter,
  digit, or underscore. (The C-identifier class again — the same
  definition as the word-motion classes, not by coincidence: both are
  asking "where do tokens end?")

## Challenge: The Highlight State Machine {#highlight-line points=20}

Implement `highlight_line(line, keywords, starts_in_comment)` →
`HlLine{spans, ends_in_comment}` per the rules above. `spans` has
exactly one entry per character. An empty line passes the carry
through untouched.

This function is your render loop's per-line preprocessor: cache the
results, feed each carry-out forward, and re-highlight below an edit
only while carry-outs change.

### Starter

```cpp
#include <cstddef>
#include <string>
#include <string_view>
#include <vector>

enum class Hl { Normal, Keyword, String, Comment, Number };

struct HlLine {
    std::vector<Hl> spans;       // one per character of the line
    bool ends_in_comment = false; // carry-out for the next line
};

HlLine highlight_line(std::string_view line,
                      const std::vector<std::string>& keywords,
                      bool starts_in_comment) {
    // TODO: single pass; comment carry > string > comment openers >
    // string opener > number > keyword. Track prev-separator.
    (void)line; (void)keywords; (void)starts_in_comment;
    return {};
}
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

static const std::vector<std::string> kw = {"int", "return", "if", "for"};

// Compact span signature: one letter per char.
// N=Normal K=Keyword S=String C=Comment 0=Number
static std::string sig(const HlLine& h) {
    std::string s;
    for (Hl x : h.spans) {
        switch (x) {
            case Hl::Normal:  s += 'N'; break;
            case Hl::Keyword: s += 'K'; break;
            case Hl::String:  s += 'S'; break;
            case Hl::Comment: s += 'C'; break;
            case Hl::Number:  s += '0'; break;
        }
    }
    return s;
}

int main() {
    {
        HlLine h = highlight_line("int x = 42;", kw, false);
        check(sig(h) == "KKKNNNNN00N", "test_keyword_and_number");
        check(!h.ends_in_comment, "test_plain_line_no_carry");
    }
    {
        HlLine h = highlight_line("// all comment", kw, false);
        check(sig(h) == "CCCCCCCCCCCCCC", "test_line_comment");
        check(!h.ends_in_comment, "test_line_comment_no_carry");
    }
    {
        HlLine h = highlight_line("int a; // c", kw, false);
        check(sig(h) == "KKKNNNNCCCC", "test_trailing_line_comment");
    }
    {
        HlLine h = highlight_line("/* c */ int", kw, false);
        check(sig(h) == "CCCCCCCNKKK", "test_block_comment_closed");
        check(!h.ends_in_comment, "test_closed_block_no_carry");
    }
    {
        HlLine h = highlight_line("int x; /* open", kw, false);
        check(sig(h) == "KKKNNNNCCCCCCC", "test_block_comment_opens");
        check(h.ends_in_comment, "test_open_block_carries");
    }
    {
        HlLine h = highlight_line("still in */ if x", kw, true);
        check(sig(h) == "CCCCCCCCCCCNKKNN", "test_carry_in_then_close");
        check(!h.ends_in_comment, "test_carry_cleared_after_close");
    }
    {
        HlLine h = highlight_line("never closes", kw, true);
        check(sig(h) == "CCCCCCCCCCCC", "test_carry_whole_line");
        check(h.ends_in_comment, "test_carry_propagates");
    }
    {
        HlLine h = highlight_line("", kw, true);
        check(h.spans.empty() && h.ends_in_comment,
              "test_empty_line_passes_carry");
        HlLine h2 = highlight_line("", kw, false);
        check(h2.spans.empty() && !h2.ends_in_comment,
              "test_empty_line_no_carry");
    }
    {
        HlLine h = highlight_line("x = \"str\";", kw, false);
        check(sig(h) == "NNNNSSSSSN", "test_string_with_quotes");
    }
    {
        HlLine h = highlight_line("\"a\\\"b\"c", kw, false);
        check(sig(h) == "SSSSSSN", "test_escaped_quote_stays_inside");
    }
    {
        HlLine h = highlight_line("\"// not a comment\"", kw, false);
        check(sig(h) == "SSSSSSSSSSSSSSSSSS",
              "test_slashes_inside_string");
    }
    {
        HlLine h = highlight_line("/* \"quote\" */x", kw, false);
        check(sig(h) == "CCCCCCCCCCCCCN", "test_quote_inside_comment");
    }
    {
        HlLine h = highlight_line("\"unterminated", kw, false);
        check(sig(h) == "SSSSSSSSSSSSS", "test_unterminated_string");
        check(!h.ends_in_comment, "test_strings_do_not_carry");
    }
    {
        HlLine h = highlight_line("3.14 + x9", kw, false);
        check(sig(h) == "0000NNNNN", "test_float_number_x9_not");
    }
    {
        HlLine h = highlight_line("return42", kw, false);
        check(sig(h) == "NNNNNNNN", "test_glued_keyword_number_normal");
    }
    {
        HlLine h = highlight_line("if(x)return y;", kw, false);
        check(sig(h) == "KKNNNKKKKKKNNN",
              "test_keywords_at_separator_edges");
    }
    {
        HlLine h = highlight_line("printf", kw, false);
        check(sig(h) == "NNNNNN", "test_keyword_inside_word_normal");
    }
    {
        HlLine h = highlight_line("int/*c*/if", kw, false);
        check(sig(h) == "KKKCCCCCKK",
              "test_comment_is_a_separator_for_keywords");
    }
    return failed;
}
```

# Final Challenge: The Headless Editor {#editor-core points=75}

Everything converges. You have a buffer, a mode machine, motions,
operators, and an undo design; now assemble them into an `Editor` — the
object that, in your real program, sits between `decode_input` and
`build_frame`:

```
read() -> decode_input() -> editor.feed(key)  ...  render(editor) -> write()
```

Here it runs headless: tests construct an `Editor`, feed it a script of
keystrokes, and assert on the three things that define an editor's
observable state — **buffer content, cursor position, mode**. If this
class is correct, wrapping it in the terminal glue from Lessons 1–4
produces a working vi. (That's the course's endgame, and it's yours to
run: `RawMode` guard, read/decode/feed/render loop, done.)

Keys arrive as single chars via `feed(char)` (`feed(string_view)` just
loops): `0x1B` is Esc, `'\r'` or `'\n'` is Enter, `0x7F` or `0x08` is
Backspace, `0x12` is Ctrl-R. Everything else is a literal key.

**Normal mode** — the command grammar, built from your earlier pieces
(all provided in the starter):

- **Counts**: digits `1`–`9` accumulate a count; `0` is a digit only
  if a count has started (otherwise it's the motion). A count before
  *and* after an operator multiply (`2d3w` = 6 words). For `G`/`gg`
  the combined count is a line number. Esc clears any pending
  count/operator; so does an unrecognized key.
- **Motions** (cursor moves, count-aware): `h l j k 0 ^ $ w b e G`
  and `gg` (a `g` waits for a second `g`; `g` + anything else aborts
  cleanly — that second key is *discarded*, the same as an unrecognized
  key clearing a pending count, not executed as a command of its own).
  Semantics exactly as in your motion challenges — including the
  no-goal-column `j`/`k` clamp.
- **Operators**: `d` and `c`, with any motion above (via `resolve` +
  `apply_op`, provided), plus doubled `dd`/`cc`. A `nullopt` range
  (e.g. `dh` at column 0) is a no-op — and must not create an undo
  entry. `c` ends in insert mode. (Our dialect: `cw` follows the
  regular grammar, unlike real vi's `cw`-acts-like-`ce` special case —
  a documented divergence, and a good upgrade exercise later.)
- **`x`**: delete `count` characters at the cursor (clamped to the
  line end); no-op on an empty line; cursor re-clamped after.
- **Insert doors**: `i` (here), `I` (first non-blank), `a` (after
  cursor — col may become `len`, legal in insert mode), `A` (end of
  line), `o` / `O` (open a line below / above, cursor on it).
- **`u`** undo, **Ctrl-R** redo.

**Insert mode**: printable ASCII (`0x20`–`0x7E`) inserts at the cursor;
Enter splits the line; Backspace deletes left, joining lines at column
0 (no-op at the very start of the buffer); Esc returns to normal mode,
moving the cursor one left (not below 0) and re-imposing the
normal-mode clamp. Other bytes are ignored.

**Undo** — snapshot-based (`{lines, cursor}`), per the design lesson:
simple, correct, and honest about its memory cost at this file size.
The unit rules:

- Every successful `x`, `d`, `c`, `o`, `O` pushes one snapshot (taken
  *before* the change) — one command, one unit.
- An insert session (door key through Esc) is **one unit**: snapshot
  the state at the door press, but commit it **lazily** — only when
  the first actual edit of the session happens. A session with no
  edits (`i` then Esc) must leave no trace in the history. For `c`,
  `o`, `O` the snapshot pushed by the command itself covers the whole
  following session (they already changed the buffer).
- `u` restores the top snapshot — buffer *and* cursor (the cursor
  returns to where the change began; test-pinned). Ctrl-R re-applies.
  Any new undoable change clears the redo stack. `u` with an empty
  history is a quiet no-op.

Work through the dispatch methodically — normal mode is a `switch`
over one char, with four little pieces of pending state (count,
operator count, operator, the `g` flag) threaded through it. That
state *is* the mode machine's normal-mode half, now for real.

### Starter

```cpp
#include <algorithm>
#include <cstddef>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

struct Pos {
    std::size_t row = 0;
    std::size_t col = 0;
    bool operator==(const Pos&) const = default;
};

enum class Mode { Normal, Insert };

using Lines = std::vector<std::string>;

// ---- provided: the motion/operator stack from earlier lessons --------

inline int cls(char c) {
    if (c == ' ' || c == '\t') return 0;
    if ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
        (c >= '0' && c <= '9') || c == '_')
        return 1;
    return 2;
}

inline int class_at(const Lines& ls, Pos p) {
    if (p.col >= ls[p.row].size()) return 0;
    return cls(ls[p.row][p.col]);
}

inline std::optional<Pos> next_pos(const Lines& ls, Pos p) {
    if (p.row + 1 == ls.size()) {
        if (p.col + 1 < ls[p.row].size()) return Pos{p.row, p.col + 1};
        return std::nullopt;
    }
    if (p.col + 1 <= ls[p.row].size()) return Pos{p.row, p.col + 1};
    return Pos{p.row + 1, 0};
}

inline std::optional<Pos> prev_pos(const Lines& ls, Pos p) {
    if (p.col > 0) return Pos{p.row, p.col - 1};
    if (p.row == 0) return std::nullopt;
    std::size_t r = p.row - 1;
    return Pos{r, ls[r].empty() ? 0 : ls[r].size() - 1};
}

inline Pos word_forward(const Lines& ls, Pos p) {
    int cl = class_at(ls, p);
    if (cl != 0 && !ls[p.row].empty()) {
        while (true) {
            auto q = next_pos(ls, p);
            if (!q) return p;
            p = *q;
            if (ls[p.row].empty()) return p;
            if (class_at(ls, p) != cl) break;
        }
    } else {
        auto q = next_pos(ls, p);
        if (!q) return p;
        p = *q;
        if (ls[p.row].empty()) return p;
    }
    while (class_at(ls, p) == 0) {
        auto q = next_pos(ls, p);
        if (!q) return p;
        p = *q;
        if (ls[p.row].empty()) return p;
    }
    return p;
}

inline Pos word_back(const Lines& ls, Pos p) {
    auto q = prev_pos(ls, p);
    if (!q) return p;
    p = *q;
    if (ls[p.row].empty()) return p;
    while (class_at(ls, p) == 0) {
        q = prev_pos(ls, p);
        if (!q) return p;
        p = *q;
        if (ls[p.row].empty()) return p;
    }
    int cl = class_at(ls, p);
    while (true) {
        q = prev_pos(ls, p);
        if (!q || ls[q->row].empty() || class_at(ls, *q) != cl) return p;
        p = *q;
    }
}

inline Pos word_end(const Lines& ls, Pos p) {
    auto q = next_pos(ls, p);
    if (!q) return p;
    p = *q;
    while (ls[p.row].empty() || class_at(ls, p) == 0) {
        q = next_pos(ls, p);
        if (!q) return p;
        p = *q;
    }
    int cl = class_at(ls, p);
    while (true) {
        q = next_pos(ls, p);
        if (!q || ls[q->row].empty() || class_at(ls, *q) != cl) return p;
        p = *q;
    }
}

inline Pos apply_motion(const Lines& lines, Pos p, char motion,
                        int count) {
    std::size_t reps = count > 0 ? static_cast<std::size_t>(count) : 1;
    std::size_t nrows = lines.size();
    auto maxcol = [&](std::size_t r) {
        return lines[r].empty() ? 0 : lines[r].size() - 1;
    };
    switch (motion) {
        case 'h': p.col = p.col >= reps ? p.col - reps : 0; break;
        case 'l': p.col = std::min(p.col + reps, maxcol(p.row)); break;
        case 'j':
            p.row = std::min(p.row + reps, nrows - 1);
            p.col = std::min(p.col, maxcol(p.row));
            break;
        case 'k':
            p.row = p.row >= reps ? p.row - reps : 0;
            p.col = std::min(p.col, maxcol(p.row));
            break;
        case '0': p.col = 0; break;
        case '^': {
            const std::string& L = lines[p.row];
            std::size_t i = 0;
            while (i < L.size() && (L[i] == ' ' || L[i] == '\t')) i++;
            p.col = i < L.size() ? i : maxcol(p.row);
            break;
        }
        case '$':
            p.row = std::min(p.row + reps - 1, nrows - 1);
            p.col = maxcol(p.row);
            break;
        case 'G':
            p.row = count > 0 ? std::min<std::size_t>(
                                    static_cast<std::size_t>(count),
                                    nrows) - 1
                              : nrows - 1;
            p.col = std::min(p.col, maxcol(p.row));
            break;
        case 'g':
            p.row = count > 0 ? std::min<std::size_t>(
                                    static_cast<std::size_t>(count),
                                    nrows) - 1
                              : 0;
            p.col = std::min(p.col, maxcol(p.row));
            break;
        default: break;
    }
    return p;
}

struct Range {
    Pos start;
    Pos end;
    bool linewise = false;
    bool operator==(const Range&) const = default;
};

namespace detail {

inline bool pos_less(Pos a, Pos b) {
    return a.row < b.row || (a.row == b.row && a.col < b.col);
}

inline bool is_word_start(const Lines& ls, Pos p) {
    if (ls[p.row].empty()) return true;
    int c = class_at(ls, p);
    if (c == 0) return false;
    if (p.col == 0) return true;
    return cls(ls[p.row][p.col - 1]) != c;
}

} // namespace detail

inline std::optional<Range> resolve(const Lines& lines, Pos cur, char op,
                                    char motion, int count) {
    std::size_t reps = count > 0 ? static_cast<std::size_t>(count) : 1;
    std::size_t n = lines.size();

    if (motion == op || motion == 'j' || motion == 'k' || motion == 'G' ||
        motion == 'g') {
        std::size_t r1 = cur.row, r2 = cur.row;
        if (motion == op) {
            r2 = std::min(cur.row + reps - 1, n - 1);
        } else if (motion == 'j') {
            r2 = std::min(cur.row + reps, n - 1);
        } else if (motion == 'k') {
            r1 = cur.row >= reps ? cur.row - reps : 0;
        } else {
            std::size_t dflt = motion == 'G' ? n - 1 : 0;
            std::size_t t =
                count > 0
                    ? std::min<std::size_t>(static_cast<std::size_t>(count),
                                            n) - 1
                    : dflt;
            r1 = std::min(cur.row, t);
            r2 = std::max(cur.row, t);
        }
        return Range{{r1, 0}, {r2, 0}, true};
    }

    Pos start = cur, end = cur;
    switch (motion) {
        case 'w': {
            Pos t = cur;
            for (std::size_t i = 0; i < reps; i++)
                t = word_forward(lines, t);
            if (t.row > cur.row) {
                end = {t.row - 1, lines[t.row - 1].size()};
            } else if (t == cur || !detail::is_word_start(lines, t)) {
                end = {cur.row, lines[cur.row].size()};
            } else {
                end = t;
            }
            break;
        }
        case 'b': {
            Pos t = cur;
            for (std::size_t i = 0; i < reps; i++) t = word_back(lines, t);
            start = t;
            break;
        }
        case 'e': {
            Pos t = cur;
            for (std::size_t i = 0; i < reps; i++) t = word_end(lines, t);
            end = lines[t.row].empty() ? t : Pos{t.row, t.col + 1};
            break;
        }
        case '$': {
            Pos t = apply_motion(lines, cur, '$', count);
            if (lines[t.row].empty()) return std::nullopt;
            end = {t.row, t.col + 1};
            break;
        }
        case '0':
            start = {cur.row, 0};
            break;
        case 'h':
            start = {cur.row, cur.col >= reps ? cur.col - reps : 0};
            break;
        case 'l':
            end = {cur.row,
                   std::min(cur.col + reps, lines[cur.row].size())};
            break;
        default:
            return std::nullopt;
    }

    if (detail::pos_less(end, start)) std::swap(start, end);
    if (start == end) return std::nullopt;
    return Range{start, end, false};
}

struct EditResult {
    Lines lines;
    Pos cursor;
    std::vector<std::string> yanked;
    bool linewise = false;
    bool enter_insert = false;
};

inline EditResult apply_op(Lines lines, char op, const Range& r) {
    EditResult res;
    res.linewise = r.linewise;

    if (r.linewise) {
        for (std::size_t i = r.start.row; i <= r.end.row; i++)
            res.yanked.push_back(lines[i]);
        if (op == 'y') {
            res.cursor = {r.start.row, 0};
            res.lines = std::move(lines);
            return res;
        }
        lines.erase(
            lines.begin() + static_cast<std::ptrdiff_t>(r.start.row),
            lines.begin() + static_cast<std::ptrdiff_t>(r.end.row) + 1);
        if (op == 'c') {
            lines.insert(
                lines.begin() + static_cast<std::ptrdiff_t>(r.start.row),
                "");
            res.cursor = {r.start.row, 0};
            res.enter_insert = true;
        } else {
            if (lines.empty()) lines.push_back("");
            res.cursor = {std::min(r.start.row, lines.size() - 1), 0};
        }
        res.lines = std::move(lines);
        return res;
    }

    if (r.start.row == r.end.row) {
        res.yanked.push_back(
            lines[r.start.row].substr(r.start.col, r.end.col - r.start.col));
    } else {
        res.yanked.push_back(lines[r.start.row].substr(r.start.col));
        for (std::size_t i = r.start.row + 1; i < r.end.row; i++)
            res.yanked.push_back(lines[i]);
        res.yanked.push_back(lines[r.end.row].substr(0, r.end.col));
    }

    if (op == 'y') {
        res.cursor = r.start;
        res.lines = std::move(lines);
        return res;
    }

    if (r.start.row == r.end.row) {
        lines[r.start.row].erase(r.start.col, r.end.col - r.start.col);
    } else {
        lines[r.start.row] = lines[r.start.row].substr(0, r.start.col) +
                             lines[r.end.row].substr(r.end.col);
        lines.erase(
            lines.begin() + static_cast<std::ptrdiff_t>(r.start.row) + 1,
            lines.begin() + static_cast<std::ptrdiff_t>(r.end.row) + 1);
    }

    res.cursor = r.start;
    if (op == 'c') {
        res.enter_insert = true;
    } else {
        std::size_t len = lines[r.start.row].size();
        std::size_t maxc = len ? len - 1 : 0;
        res.cursor.col = std::min(res.cursor.col, maxc);
    }
    res.lines = std::move(lines);
    return res;
}

// ---- your work: the editor core ---------------------------------------

class Editor {
public:
    Editor() : lines_{""} {}
    explicit Editor(Lines lines) : lines_(std::move(lines)) {
        if (lines_.empty()) lines_.push_back("");
    }

    void feed(char c) {
        // TODO: dispatch on mode_; normal mode threads count_/opcount_/
        // op_/g_ through a switch; insert mode edits the buffer.
        (void)c;
    }

    void feed(std::string_view keys) {
        for (char c : keys) feed(c);
    }

    Mode mode() const { return mode_; }
    Pos cursor() const { return cur_; }
    const Lines& lines() const { return lines_; }

    std::string text() const {
        std::string out;
        for (std::size_t i = 0; i < lines_.size(); i++) {
            if (i) out += '\n';
            out += lines_[i];
        }
        return out;
    }

private:
    struct Snap {
        Lines lines;
        Pos cur;
    };

    // Suggested helpers (implement as you see fit):
    //   clear_pending, combined_count, push_undo,
    //   commit_pending_snapshot, do_undo, do_redo,
    //   motion_target, operate, do_x, feed_normal, feed_insert.

    Lines lines_;
    Pos cur_;
    Mode mode_ = Mode::Normal;

    int count_ = 0;    // count typed since the operator (or overall)
    int opcount_ = 0;  // count typed before the operator
    char op_ = 0;      // pending operator: 'd', 'c', or 0
    bool g_ = false;   // saw 'g', waiting for the second one

    std::vector<Snap> undo_;
    std::vector<Snap> redo_;
    std::optional<Snap> pending_; // uncommitted insert-session snapshot
};
```

### Tests

```cpp
#include "solution.cpp"
#include <cstdio>
#include <string>
#include <vector>

static int failed = 0;
static void check(bool ok, const char* name) {
    std::printf("--- %s: %s\n", ok ? "PASS" : "FAIL", name);
    if (!ok) failed++;
}

// NOTE: "\x1b" is kept in its own literal ("...\x1b" "u") wherever a
// hex-digit character follows — C++ hex escapes are greedy.

static const Lines A = {"foo bar baz", "second line", "", "last"};

int main() {
    {
        Editor ed;
        check(ed.mode() == Mode::Normal && ed.cursor() == Pos{0, 0} &&
                  ed.lines() == Lines{""},
              "test_default_state");
        check(Editor(Lines{}).lines() == Lines{""},
              "test_empty_vector_normalized");
        check(Editor(Lines{"a", "b"}).text() == "a\nb", "test_text_joins");
    }
    {
        Editor ed;
        ed.feed("ihello");
        check(ed.mode() == Mode::Insert, "test_i_enters_insert");
        check(ed.text() == "hello" && ed.cursor() == Pos{0, 5},
              "test_typing_inserts");
        ed.feed("\x1b");
        check(ed.mode() == Mode::Normal && ed.cursor() == Pos{0, 4},
              "test_esc_backs_cursor_left");
    }
    {
        Editor ed(A);
        ed.feed("j");
        check(ed.cursor() == Pos{1, 0}, "test_j");
        ed.feed("3l");
        check(ed.cursor() == Pos{1, 3}, "test_count_l");
        ed.feed("k");
        check(ed.cursor() == Pos{0, 3}, "test_k");
        ed.feed("h");
        check(ed.cursor() == Pos{0, 2}, "test_h");
        ed.feed("$");
        check(ed.cursor() == Pos{0, 10}, "test_dollar");
        ed.feed("j");
        check(ed.cursor() == Pos{1, 10}, "test_j_keeps_col_when_it_fits");
        ed.feed("j");
        check(ed.cursor() == Pos{2, 0}, "test_j_clamps_on_empty_line");
        ed.feed("0");
        check(ed.cursor() == Pos{2, 0}, "test_0_motion");
    }
    {
        Editor ed(A);
        ed.feed("G");
        check(ed.cursor() == Pos{3, 0}, "test_G_last_line");
        ed.feed("2G");
        check(ed.cursor() == Pos{1, 0}, "test_count_G");
        ed.feed("99G");
        check(ed.cursor() == Pos{3, 0}, "test_G_clamped");
        ed.feed("gg");
        check(ed.cursor() == Pos{0, 0}, "test_gg");
        ed.feed("3gg");
        check(ed.cursor() == Pos{2, 0}, "test_count_gg");
    }
    {
        Editor ed(Lines{"  hi"});
        ed.feed("$");
        check(ed.cursor() == Pos{0, 3}, "test_dollar_short");
        ed.feed("^");
        check(ed.cursor() == Pos{0, 2}, "test_caret_first_nonblank");
    }
    {
        Editor ed(A);
        ed.feed("2w");
        check(ed.cursor() == Pos{0, 8}, "test_count_w");
        ed.feed("w");
        check(ed.cursor() == Pos{1, 0}, "test_w_crosses_lines");
        ed.feed("b");
        check(ed.cursor() == Pos{0, 8}, "test_b_back_across_lines");
        ed.feed("e");
        check(ed.cursor() == Pos{0, 10}, "test_e_word_end");
    }
    {
        Editor ed(A);
        ed.feed("x");
        check(ed.lines()[0] == "oo bar baz" && ed.cursor() == Pos{0, 0},
              "test_x");
        ed.feed("3x");
        check(ed.lines()[0] == "bar baz", "test_count_x");
    }
    {
        Editor ed(Lines{"abc"});
        ed.feed("l9x");
        check(ed.lines()[0] == "a" && ed.cursor() == Pos{0, 0},
              "test_x_clamps_to_eol_and_reclamps_cursor");
    }
    {
        Editor ed(Lines{""});
        ed.feed("x");
        ed.feed("u");
        check(ed.text() == "" && ed.cursor() == Pos{0, 0},
              "test_x_on_empty_line_is_silent_noop");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("aX");
        check(ed.text() == "aXb" && ed.cursor() == Pos{0, 2} &&
                  ed.mode() == Mode::Insert,
              "test_a_appends_after_cursor");
        ed.feed("\x1b");
        check(ed.cursor() == Pos{0, 1}, "test_esc_after_a");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("AX");
        check(ed.text() == "abX" && ed.cursor() == Pos{0, 3},
              "test_A_appends_at_eol");
    }
    {
        Editor ed(Lines{"  hi"});
        ed.feed("lll");
        ed.feed("IX");
        check(ed.text() == "  Xhi" && ed.cursor() == Pos{0, 3},
              "test_I_inserts_at_first_nonblank");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("oX");
        check(ed.lines() == Lines{"ab", "X"} &&
                  ed.cursor() == Pos{1, 1} && ed.mode() == Mode::Insert,
              "test_o_opens_below");
        ed.feed("\x1b");
        check(ed.cursor() == Pos{1, 0}, "test_esc_after_o");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("OY\x1b");
        check(ed.lines() == Lines{"Y", "ab"} && ed.cursor() == Pos{0, 0},
              "test_O_opens_above");
    }
    {
        Editor ed(Lines{"hello"});
        ed.feed("3li");
        ed.feed("\r");
        check(ed.lines() == Lines{"hel", "lo"} &&
                  ed.cursor() == Pos{1, 0},
              "test_enter_splits_line");
        ed.feed("\x7f");
        check(ed.lines() == Lines{"hello"} && ed.cursor() == Pos{0, 3},
              "test_backspace_joins_lines");
        ed.feed("p");
        check(ed.text() == "helplo" && ed.cursor() == Pos{0, 4},
              "test_insert_continues_after_join");
    }
    {
        Editor ed;
        ed.feed("i\x7f");
        check(ed.text() == "" && ed.mode() == Mode::Insert,
              "test_backspace_at_origin_noop");
    }
    {
        Editor ed(A);
        ed.feed("dw");
        check(ed.lines()[0] == "bar baz" && ed.cursor() == Pos{0, 0},
              "test_dw");
    }
    {
        Editor ed(A);
        ed.feed("de");
        check(ed.lines()[0] == " bar baz", "test_de_inclusive");
    }
    {
        Editor ed(A);
        ed.feed("3l");
        ed.feed("d$");
        check(ed.lines()[0] == "foo" && ed.cursor() == Pos{0, 2},
              "test_d_dollar_clamps_cursor");
    }
    {
        Editor ed(A);
        ed.feed("dd");
        check(ed.lines() == Lines{"second line", "", "last"} &&
                  ed.cursor() == Pos{0, 0},
              "test_dd");
    }
    {
        Editor ed(A);
        ed.feed("2dd");
        check(ed.lines() == Lines{"", "last"}, "test_2dd");
    }
    {
        Editor ed(Lines{"a", "b"});
        ed.feed("jdd");
        check(ed.lines() == Lines{"a"} && ed.cursor() == Pos{0, 0},
              "test_dd_last_line_clamps_cursor");
    }
    {
        Editor ed(A);
        ed.feed("dj");
        check(ed.lines() == Lines{"", "last"} &&
                  ed.cursor() == Pos{0, 0},
              "test_dj_linewise_pair");
    }
    {
        Editor ed(A);
        ed.feed("jdG");
        check(ed.lines() == Lines{"foo bar baz"} &&
                  ed.cursor() == Pos{0, 0},
              "test_dG_deletes_to_end");
    }
    {
        Editor ed(A);
        ed.feed("jdgg");
        check(ed.lines() == Lines{"", "last"},
              "test_dgg_deletes_to_top");
    }
    {
        Editor ed1(A), ed2(A);
        ed1.feed("d2w");
        ed2.feed("2dw");
        check(ed1.lines()[0] == "baz", "test_d2w");
        check(ed2.lines()[0] == "baz", "test_2dw_counts_multiply");
    }
    {
        Editor ed(A);
        ed.feed("2ldh");
        check(ed.lines()[0] == "fo bar baz" && ed.cursor() == Pos{0, 1},
              "test_dh");
        ed.feed("d0");
        check(ed.lines()[0] == "o bar baz" && ed.cursor() == Pos{0, 0},
              "test_d0");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("dl");
        check(ed.lines()[0] == "b", "test_dl_deletes_under_cursor");
    }
    {
        Editor ed(A);
        ed.feed("dh"); // empty range at col 0: no-op, no undo entry
        ed.feed("u");
        check(ed.lines() == A, "test_failed_operator_leaves_no_undo");
    }
    {
        Editor ed(A);
        ed.feed("cwX");
        check(ed.lines()[0] == "Xbar baz" &&
                  ed.cursor() == Pos{0, 1} && ed.mode() == Mode::Insert,
              "test_cw_regular_grammar_dialect");
    }
    {
        Editor ed(A);
        ed.feed("jccnew");
        check(ed.lines() == Lines{"foo bar baz", "new", "", "last"} &&
                  ed.cursor() == Pos{1, 3} && ed.mode() == Mode::Insert,
              "test_cc_change_line");
    }
    {
        Editor ed(Lines{"foo bar"});
        ed.feed("4lc$X");
        check(ed.text() == "foo X" && ed.mode() == Mode::Insert,
              "test_c_dollar_unclamped_insert_point");
    }
    {
        Editor ed;
        ed.feed("ihello\x1b");
        ed.feed("u");
        check(ed.text() == "" && ed.cursor() == Pos{0, 0},
              "test_insert_session_is_one_undo_unit");
        ed.feed("\x12");
        check(ed.text() == "hello" && ed.cursor() == Pos{0, 4},
              "test_redo_restores_session");
    }
    {
        Editor ed;
        ed.feed("iab\x1b");
        ed.feed("acd\x1b");
        check(ed.text() == "abcd", "test_two_sessions_text");
        ed.feed("u");
        check(ed.text() == "ab" && ed.cursor() == Pos{0, 1},
              "test_undo_peels_last_session_only");
        ed.feed("u");
        check(ed.text() == "", "test_undo_peels_first_session");
    }
    {
        Editor ed(A);
        ed.feed("wx");
        check(ed.lines()[0] == "foo ar baz", "test_x_after_motion");
        ed.feed("u");
        check(ed.lines()[0] == "foo bar baz" &&
                  ed.cursor() == Pos{0, 4},
              "test_undo_x_restores_cursor");
    }
    {
        Editor ed(A);
        ed.feed("jdd");
        ed.feed("u");
        check(ed.lines() == A && ed.cursor() == Pos{1, 0},
              "test_undo_dd_restores_cursor");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("x");
        ed.feed("i\x1b"); // insert session with no edits
        ed.feed("u");
        check(ed.text() == "ab",
              "test_empty_insert_session_leaves_no_undo_unit");
    }
    {
        Editor ed;
        ed.feed("ione\x1b");
        ed.feed("u");
        ed.feed("itwo\x1b");
        ed.feed("\x12");
        check(ed.text() == "two", "test_new_edit_clears_redo");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("oX\x1b");
        ed.feed("u");
        check(ed.lines() == Lines{"ab"} && ed.cursor() == Pos{0, 0},
              "test_undo_o_removes_line_and_typing");
    }
    {
        Editor ed(Lines{"ab"});
        ed.feed("li");
        ed.feed("X\rY\x1b");
        check(ed.lines() == Lines{"aX", "Yb"} &&
                  ed.cursor() == Pos{1, 0},
              "test_session_with_split");
        ed.feed("u");
        check(ed.lines() == Lines{"ab"} && ed.cursor() == Pos{0, 1},
              "test_undo_session_with_split_is_one_unit");
    }
    {
        Editor ed(A);
        ed.feed("2");
        ed.feed("\x1b");
        ed.feed("l");
        check(ed.cursor() == Pos{0, 1}, "test_esc_cancels_count");
    }
    {
        Editor ed(A);
        ed.feed("d");
        ed.feed("\x1b");
        ed.feed("w");
        check(ed.lines() == A && ed.cursor() == Pos{0, 4},
              "test_esc_cancels_pending_operator");
    }
    {
        Editor ed(A);
        ed.feed("2q"); // unknown key clears pending count
        ed.feed("l");
        check(ed.cursor() == Pos{0, 1}, "test_unknown_key_clears_count");
    }
    {
        Editor ed(A);
        ed.feed("gx"); // g followed by non-g aborts cleanly
        ed.feed("l");
        check(ed.cursor() == Pos{0, 1} && ed.lines() == A,
              "test_g_then_other_aborts");
    }
    return failed;
}
```
