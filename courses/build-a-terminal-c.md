---
course: build-a-terminal
title: Build a Terminal Emulator in C
language: c
description: >
  Terminals aren't magic — they're programs speaking Unix I/O, POSIX APIs,
  and ANSI escape codes. Build one from scratch: master file descriptors,
  ttys, and pseudoterminals; put a real device into raw mode; parse escape
  sequences with a state machine; decode keyboards and UTF-8; model the
  screen as a grid of cells and render it with minimal writes; then build
  a text editor on top — line buffers, viewports, atomic saves, search,
  and an event loop — and assemble it into a working editor you can
  actually live in.
duration_hours: 32
tags: [systems-programming, c, unix, io, terminals]
extended_reading:
  - title: "The TTY demystified — the best single article on the tty layer"
    url: http://www.linusakesson.net/programming/tty/
  - title: "XTerm Control Sequences (ctlseqs) — the de-facto escape code reference"
    url: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html
  - title: "A parser for DEC's ANSI-compatible video terminals"
    url: https://vt100.net/emu/dec_ansi_parser
  - title: POSIX termios(3) man page
    url: https://man7.org/linux/man-pages/man3/termios.3.html
  - title: "Build Your Own Text Editor (kilo, walked through step by step)"
    url: https://viewsourcecode.org/snaptoken/kilo/
  - title: "UTF-8 history — Ken Thompson's placemat design, as told by Rob Pike"
    url: https://www.cl.cam.ac.uk/~mgk25/ucs/utf-8-history.txt
  - title: "ECMA-48 — the actual standard behind 'ANSI escape codes'"
    url: https://ecma-international.org/publications-and-standards/standards/ecma-48/
  - title: "stty(1) — inspect and modify terminal settings from the shell"
    url: https://man7.org/linux/man-pages/man1/stty.1.html
---

# Lesson: Unix I/O and File Descriptors {#unix-io}

A terminal emulator is, at bottom, a program that moves bytes: bytes in
from a keyboard, bytes out to a screen, bytes to and from files. Before
we touch a single escape code, we need to be precise about how Unix moves
bytes — because every bug you will hit later (garbled screens, hung
reads, half-written files) is really a misunderstanding of this layer.

## File descriptors are just integers

When a process opens a file, the kernel gives it back a small integer —
a **file descriptor** (fd). The integer indexes into a per-process table
the kernel keeps; the table entry points at an open file description
(offset, flags) which points at the actual thing: a file on disk, a
pipe, a socket, or — the case we care about — a terminal device.

Every process starts life with three of them already open:

- **0 — stdin**: where input comes from
- **1 — stdout**: where normal output goes
- **2 — stderr**: where errors go, deliberately separate so they survive
  when stdout is redirected into a file

`unistd.h` gives them names: `STDIN_FILENO`, `STDOUT_FILENO`,
`STDERR_FILENO`. The two primitive operations are:

```c
#include <unistd.h>

ssize_t nread   = read(fd, buf, count);   /* read up to count bytes  */
ssize_t nwritten = write(fd, buf, count); /* write up to count bytes */
```

The word **up to** is doing a lot of work in those comments, and it is
the single most important fact in this lesson. We'll come back to it.

The beautiful part of the design is that `read` and `write` don't care
what's on the other end. The same two calls work whether fd 0 is your
keyboard, a pipe from another program, or a file. That's why

```
$ ./myprog            # stdin is a terminal
$ ./myprog < in.txt   # stdin is a file
$ cat in.txt | ./myprog  # stdin is a pipe
```

all run the same code. Your terminal emulator will exploit this
constantly — and its test suite will too, feeding it pipes and
pseudoterminals where a human would be typing.

## Syscalls vs. stdio: why we use write(), not printf()

You've used `printf` and `fgets` since your first C program. Those are
**stdio** — a *user-space buffering library* wrapped around `read` and
`write`. When you `printf("hi")`, nothing reaches the kernel yet: the
bytes go into a buffer inside your process, and stdio flushes that
buffer later — when it's full, when you print a newline (if stdout is a
terminal), or when the process exits.

That buffering is a performance win for ordinary programs and a
correctness hazard for a terminal program:

- We will disable output processing (`OPOST`, next lessons), at which
  point `\n` no longer flushes and no longer means what stdio thinks it
  means.
- We need byte-exact control over *what* goes to the terminal and
  *when*. An escape sequence that gets flushed in two halves at the
  wrong moment paints garbage.
- Mixing stdio and raw `write` on the same fd interleaves
  unpredictably, because half your output is sitting in a buffer the
  kernel has never seen.

So the rule for this course: **talk to the terminal with `read(2)` and
`write(2)` directly.** We'll still use `snprintf` — but only to format
bytes *into our own buffers*, which we then hand to `write` ourselves.

## Short reads and short writes

Here is the contract, precisely:

- `read(fd, buf, n)` returns the number of bytes actually read, which
  may be **anything from 1 to n** when data is available. It returns
  **0 only at end-of-file** (the other end of the pipe closed; the file
  ran out). It returns **-1 on error**, with the reason in `errno`.
- `write(fd, buf, n)` returns the number of bytes actually written —
  again possibly **less than n**. -1 on error.

A short read is not an error and not rare. Read from a terminal and
you get whatever the user has typed so far, not the 512 bytes you asked
for. Read from a pipe and you get whatever the writer has written.
Even reads from regular files can return short at (say) an EOF
boundary. Any code that assumes `read(fd, buf, n)` fills the buffer is
wrong code that happens to pass its first test.

Short *writes* are rarer — writes to pipes and terminals usually
either block until complete or fail — but they happen exactly when you
can least afford them: the pipe is nearly full, or a signal arrives
mid-write (see below). Production code loops:

```c
/* Keep calling write() until all n bytes are gone. */
size_t off = 0;
while (off < n) {
    ssize_t w = write(fd, buf + off, n - off);
    if (w < 0) { /* error handling here */ }
    off += (size_t)w;
}
```

## EINTR: the signal in the middle

Unix delivers **signals** (Ctrl+C's SIGINT, window-resize's SIGWINCH,
timers' SIGALRM) asynchronously. If a signal arrives while your process
is blocked inside `read` or `write`, and the handler was installed
without the `SA_RESTART` flag, the syscall gives up and returns -1 with
`errno == EINTR` — "interrupted, nothing wrong, try again."

This matters enormously for a terminal program because we *want* to be
interrupted: when the user resizes the window, SIGWINCH must be able to
break us out of a blocked `read` so we can repaint at the new size. The
price is that every `read`/`write` in the program must treat EINTR as
"retry", not "fail":

```c
ssize_t r;
do {
    r = read(fd, buf, n);
} while (r < 0 && errno == EINTR);
```

You will write `write_all` and `read_full` exactly once, in the next
challenge, and then use them for the rest of the course.

## Watching it happen

Two tools make this layer visible, and you should actually run them:

```
$ strace -e trace=read,write ./yourprog
```

prints every `read`/`write` syscall your program makes — arguments,
buffers, return values. Watch a `printf`-based program make one big
`write` at exit, then watch a raw-`write` program make exactly the
calls you wrote. And:

```
$ ls -l /proc/self/fd/
lrwx------ 0 -> /dev/pts/3
lrwx------ 1 -> /dev/pts/3
lrwx------ 2 -> /dev/pts/3
```

shows where a process's fds actually point — here, all three at the
same terminal device, `/dev/pts/3`. That device file is the subject of
the next lesson.

## How this course works

Each challenge gives you a `solution.c` starter with TODOs. The grader
compiles your file together with a hidden-in-plain-sight test program
(shown in each challenge) as:

```
cc -std=c17 -Wall -O1 -o test_bin solution.c test_solution.c
```

Two consequences you should internalize now:

- **Your `solution.c` must not define `main()`** — the test file owns
  `main`. Where a challenge is fun to drive by hand (raw mode! the
  editor!) the starter includes a demo `main` fenced behind
  `#ifdef DEMO`; build it locally with
  `cc -std=c17 -Wall -DDEMO solution.c -o demo && ./demo`.
- The grader has **no interactive terminal** — tests run headless. So
  the course is engineered the way real terminal code is engineered:
  logic lives in pure functions over buffers and structs, and where a
  real device is genuinely needed, the tests conjure one with
  `posix_openpt()`. You'll build that muscle too.

## Challenge: Inspect the Terminal {#inspect-tty points=10}

Not every fd is a terminal, and programs change behavior based on the
difference: `ls` prints columns to a tty but one-name-per-line into a
pipe; `grep` colors matches only on a tty. Two calls answer the
question:

- `isatty(fd)` → 1 if fd refers to a terminal device, 0 otherwise
  (with `errno` set to `ENOTTY` for non-terminals).
- `ttyname(fd)` → the pathname of that device (e.g. `/dev/pts/3`), or
  `NULL` if fd isn't a terminal.

Write `describe_fd()`, which fills a caller-supplied buffer with a
one-line human-readable description of an fd:

- If the fd is a terminal: `tty <name>` (e.g. `tty /dev/pts/3`).
- If the fd is a terminal but the name can't be determined:
  `tty (name unknown)`.
- Otherwise: `not a tty`.

Return 1 if the fd was a terminal, 0 if not. Use `snprintf` to build
the string — never assume the caller's buffer is big enough.

The tests exercise your function against a pipe (not a tty) and
against a real pseudoterminal device the test creates itself — proof
that "is this a terminal?" is a property of the *device behind the
fd*, not of how the program was launched.

### Starter

```c
#include <unistd.h>
#include <stdio.h>
#include <string.h>

/* Describe what fd points at.
 * Writes one of:
 *   "tty <device path>"    e.g. "tty /dev/pts/3"
 *   "tty (name unknown)"   isatty() said yes but ttyname() failed
 *   "not a tty"
 * into out (always NUL-terminated, truncated to cap if needed).
 * Returns 1 if fd is a terminal, 0 otherwise. */
int describe_fd(int fd, char *out, size_t cap) {
    /* TODO: call isatty(fd); if 0, write "not a tty" and return 0 */
    /* TODO: call ttyname(fd); if NULL, write "tty (name unknown)" */
    /* TODO: otherwise snprintf "tty %s" with the name */
    /* TODO: return 1 for the terminal cases */
    (void)fd;
    snprintf(out, cap, "not a tty");
    return 0;
}

#ifdef DEMO
int main(void) {
    char line[128];
    for (int fd = 0; fd <= 2; fd++) {
        describe_fd(fd, line, sizeof(line));
        printf("fd %d: %s\n", fd, line);
    }
    /* Try:  ./demo   |   ./demo < /dev/null   |   ./demo | cat  */
    return 0;
}
#endif
```

### Tests

```c
#define _XOPEN_SOURCE 600
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>

int describe_fd(int fd, char *out, size_t cap);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main(void) {
    alarm(10); /* watchdog: no test here should block */
    char out[256];

    /* A pipe is not a terminal. */
    int pfd[2];
    if (pipe(pfd) != 0) { printf("--- FAIL: pipe_setup\n"); return 1; }
    memset(out, 'x', sizeof(out));
    int r = describe_fd(pfd[0], out, sizeof(out));
    check(r == 0, "test_pipe_returns_zero");
    check(strcmp(out, "not a tty") == 0, "test_pipe_description");
    close(pfd[0]);
    close(pfd[1]);

    /* A pseudoterminal slave IS a terminal, no keyboard required. */
    int master = posix_openpt(O_RDWR | O_NOCTTY);
    check(master >= 0, "test_openpt");
    if (master >= 0) {
        grantpt(master);
        unlockpt(master);
        const char *sname = ptsname(master);
        check(sname != NULL, "test_ptsname");
        int slave = open(sname, O_RDWR | O_NOCTTY);
        check(slave >= 0, "test_open_slave");
        if (slave >= 0) {
            memset(out, 'x', sizeof(out));
            r = describe_fd(slave, out, sizeof(out));
            check(r == 1, "test_pty_returns_one");
            check(strncmp(out, "tty ", 4) == 0, "test_pty_prefix");
            check(strstr(out, "/dev/") != NULL, "test_pty_device_path");
            close(slave);
        }
        close(master);
    }

    /* Truncation: a tiny buffer must still be NUL-terminated. */
    int pfd2[2];
    pipe(pfd2);
    char tiny[4];
    memset(tiny, 'x', sizeof(tiny));
    describe_fd(pfd2[0], tiny, sizeof(tiny));
    check(tiny[3] == '\0' || memchr(tiny, '\0', 4) != NULL,
          "test_truncation_nul_terminated");
    close(pfd2[0]);
    close(pfd2[1]);

    return failed;
}
```

## Challenge: Reliable Reads and Writes {#write-all points=15}

Time to bake the short-read/short-write/EINTR rules into two helpers
you'll use for the rest of the course:

- `write_all(fd, buf, n)` — keep calling `write` until all `n` bytes
  are written. Retry on `EINTR`. Return `n` on success, -1 on any
  real error.
- `read_full(fd, buf, n)` — keep calling `read` until `n` bytes have
  arrived **or end-of-file**. Retry on `EINTR`. Return the number of
  bytes actually read (which is less than `n` only at EOF), or -1 on
  a real error.

The tests are adversarial in exactly the ways the real world is:

1. A child process reads your 256 KiB `write_all` through a pipe in
   awkward 777-byte sips, so the pipe backs up and your write cannot
   complete in one call.
2. A `SIGALRM` handler installed *without* `SA_RESTART` fires while
   your `write_all` is blocked on a full pipe — so `write` returns
   `EINTR` mid-transfer and your loop must carry on.
3. A child drips 64 KiB to your `read_full` in tiny bursts, so single
   `read` calls return short over and over.
4. The writer closes early, and `read_full` must return the short
   byte count rather than hanging or erroring.

### Starter

```c
#include <unistd.h>
#include <errno.h>
#include <stddef.h>
#include <sys/types.h>

/* Write all n bytes of buf to fd.
 * Returns (ssize_t)n on success, -1 on error (errno preserved).
 * Must survive short writes and EINTR. */
ssize_t write_all(int fd, const void *buf, size_t n) {
    /* TODO: loop until n bytes written                       */
    /* TODO: on w < 0 && errno == EINTR: retry                */
    /* TODO: on any other error: return -1                    */
    /* HINT: cast buf to const char * to do pointer arithmetic */
    (void)fd; (void)buf;
    return (ssize_t)n;
}

/* Read exactly n bytes from fd into buf, unless EOF comes first.
 * Returns the number of bytes read (== n unless EOF), -1 on error.
 * Must survive short reads and EINTR. */
ssize_t read_full(int fd, void *buf, size_t n) {
    /* TODO: loop; read() returning 0 means EOF -> stop        */
    /* TODO: retry on EINTR                                    */
    (void)fd; (void)buf; (void)n;
    return 0;
}
```

### Tests

```c
#define _POSIX_C_SOURCE 200809L
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <errno.h>
#include <signal.h>
#include <sys/wait.h>
#include <sys/types.h>

ssize_t write_all(int fd, const void *buf, size_t n);
ssize_t read_full(int fd, void *buf, size_t n);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void on_alarm(int sig) { (void)sig; /* just interrupt syscalls */ }

/* Fill buf with a deterministic pattern so corruption is detectable. */
static void pattern(unsigned char *buf, size_t n, unsigned seed) {
    for (size_t i = 0; i < n; i++)
        buf[i] = (unsigned char)((i * 131 + seed) & 0xff);
}

static int pattern_ok(const unsigned char *buf, size_t n, unsigned seed) {
    for (size_t i = 0; i < n; i++)
        if (buf[i] != (unsigned char)((i * 131 + seed) & 0xff)) return 0;
    return 1;
}

int main(void) {
    alarm(30); /* whole-suite watchdog */
    signal(SIGPIPE, SIG_IGN);

    /* Install a non-restarting SIGALRM handler: syscalls WILL see EINTR. */
    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_handler = on_alarm;
    sa.sa_flags = 0; /* no SA_RESTART, on purpose */
    sigaction(SIGALRM, &sa, NULL);

    /* --- write_all pushes 256 KiB through a slow reader --- */
    {
        enum { N = 256 * 1024 };
        unsigned char *out = malloc(N);
        pattern(out, N, 7);

        int pfd[2];
        pipe(pfd);
        pid_t pid = fork();
        if (pid == 0) {
            /* child: slow sipping reader, verifies the pattern */
            close(pfd[1]);
            unsigned char sip[777];
            size_t total = 0;
            int ok = 1;
            ssize_t r;
            while ((r = read(pfd[0], sip, sizeof(sip))) != 0) {
                if (r < 0) { if (errno == EINTR) continue; ok = 0; break; }
                for (ssize_t i = 0; i < r; i++)
                    if (sip[i] != (unsigned char)(((total + (size_t)i) * 131 + 7) & 0xff))
                        ok = 0;
                total += (size_t)r;
            }
            _exit(ok && total == N ? 0 : 1);
        }
        close(pfd[0]);
        alarm(1); /* fires mid-transfer: write() gets EINTR */
        ssize_t w = write_all(pfd[1], out, N);
        alarm(30);
        close(pfd[1]);
        int st;
        waitpid(pid, &st, 0);
        check(w == (ssize_t)N, "test_write_all_returns_n");
        check(WIFEXITED(st) && WEXITSTATUS(st) == 0,
              "test_write_all_bytes_intact");
        free(out);
    }

    /* --- read_full assembles 64 KiB from a dripping writer --- */
    {
        enum { N = 64 * 1024 };
        int pfd[2];
        pipe(pfd);
        pid_t pid = fork();
        if (pid == 0) {
            close(pfd[0]);
            unsigned char *out = malloc(N);
            pattern(out, N, 3);
            /* drip in 511-byte chunks */
            for (size_t off = 0; off < N; off += 511) {
                size_t k = N - off < 511 ? N - off : 511;
                size_t done = 0;
                while (done < k) {
                    ssize_t w = write(pfd[1], out + off + done, k - done);
                    if (w < 0) { if (errno == EINTR) continue; _exit(1); }
                    done += (size_t)w;
                }
            }
            close(pfd[1]);
            _exit(0);
        }
        close(pfd[1]);
        unsigned char *in = malloc(N);
        memset(in, 0, N);
        ssize_t r = read_full(pfd[0], in, N);
        close(pfd[0]);
        int st;
        waitpid(pid, &st, 0);
        check(r == (ssize_t)N, "test_read_full_returns_n");
        check(pattern_ok(in, N, 3), "test_read_full_bytes_intact");
        free(in);
    }

    /* --- read_full stops short at EOF, without error --- */
    {
        int pfd[2];
        pipe(pfd);
        const char *msg = "only twenty bytes!!!";
        size_t done = 0;
        while (done < 20) {
            ssize_t w = write(pfd[1], msg + done, 20 - done);
            if (w < 0) { if (errno == EINTR) continue; break; }
            done += (size_t)w;
        }
        close(pfd[1]); /* EOF after 20 bytes */
        char buf[100];
        memset(buf, 0, sizeof(buf));
        ssize_t r = read_full(pfd[0], buf, 100);
        check(r == 20, "test_read_full_short_at_eof");
        check(memcmp(buf, msg, 20) == 0, "test_read_full_eof_content");
        close(pfd[0]);
    }

    /* --- write_all propagates real errors --- */
    {
        char c = 'x';
        ssize_t w = write_all(-1, &c, 1); /* EBADF: not retryable */
        check(w == -1, "test_write_all_reports_error");
    }

    return failed;
}
```
# Lesson: TTYs, PTYs, and the Line Discipline {#tty-device}

Type `tty` in your shell:

```
$ tty
/dev/pts/3
```

That path is a **terminal device** — a file that isn't storage but a
conversation. To understand what your terminal emulator actually *is*,
you need the (surprisingly physical) history of that file.

## From teletypes to /dev/tty

"TTY" is short for **teletypewriter**: an electromechanical typewriter
from the early 1900s that sent each keystroke down a wire and printed
whatever came back. When Unix was born at Bell Labs in 1969, teletypes
were how you talked to it — the Model 33 ASR printed 10 characters per
second onto paper. Unix modeled each one as a device file: `/dev/tty1`,
`/dev/tty2`, … Write bytes to the file, they print on that desk's
paper; read from it, you get what that person typed.

Physical teletypes died, but the *model* was too useful to kill.
Video terminals (the DEC VT100, 1978 — remember that name) replaced
paper with a CRT and added something new: they interpreted special
byte sequences to move a cursor around the screen instead of only
appending at the bottom. Then terminals stopped being hardware at all
and became *programs* — and the model still didn't change. Your
`/dev/pts/3` behaves, as far as any program can tell, like a serial
cable with a 1978 DEC terminal on the other end.

## The line discipline: the kernel's helpful middleman

Here's the crucial, non-obvious part. When you run `cat` and type at
it, your keystrokes do **not** go straight to `cat`. Between the
device and the process, the kernel runs a layer called the **line
discipline**, and by default it is doing a surprising amount of work:

- **Line buffering.** Bytes you type accumulate in a kernel buffer.
  `read()` in `cat` doesn't return until you press Enter. That's why
  it's called *canonical* (line-at-a-time) mode.
- **Line editing.** Backspace works *in the kernel*. So do Ctrl+W
  (erase word) and Ctrl+U (erase line). `cat` never sees the typo or
  the backspace — the kernel edits the buffer before delivering it.
- **Echo.** The kernel copies each keystroke back to the display.
  Programs don't print your typing; the kernel does.
- **Signals.** Ctrl+C doesn't send a byte to `cat` at all — the line
  discipline swallows it and sends `SIGINT` to the foreground process
  group. Ctrl+Z sends `SIGTSTP`. These are keyboard shortcuts
  implemented in the kernel.
- **Translation.** Enter arrives as carriage return (`\r`, 0x0D) from
  the terminal, and the kernel hands your program a newline (`\n`,
  0x0A). On output, `\n` becomes `\r\n` so the cursor both drops a
  line *and* returns to column 0 — two distinct motions, another
  teletype fossil.
- **Flow control.** Ctrl+S freezes output, Ctrl+Q resumes it — useful
  when your terminal printed at 10 chars/sec, mostly a trap today.

This kernel service is why *most* programs never think about
terminals: they read lines from fd 0 and write lines to fd 1, and the
line discipline makes it pleasant. A full-screen program — vim, less,
htop, *your terminal-to-be* — needs the opposite: every keystroke
immediately, no echo, no editing, no surprise translations. Turning
all of this off is called **raw mode**, and it's the next lesson.

You can inspect the line discipline's current configuration with
`stty -a`, which prints the exact flags you'll soon be flipping with
`tcsetattr()`:

```
$ stty -a
speed 38400 baud; rows 40; columns 132;
intr = ^C; susp = ^Z; erase = ^?; werase = ^W; ...
icanon icrnl ixon opost onlcr echo echoe ...
```

Every one of those lowercase words is a flag in `struct termios`.

## Pseudoterminals: the trick that makes terminal emulators possible

A real serial port has hardware on the far end. But xterm, tmux, ssh,
and your GUI terminal have no hardware — so where does `/dev/pts/3`
come from?

A **pseudoterminal** (pty) is a pair of connected virtual devices the
kernel manufactures on demand:

- The **master** side (an fd from opening `/dev/ptmx`) is held by the
  terminal emulator.
- The **slave** side (`/dev/pts/N`) is what the shell and its children
  get as stdin/stdout/stderr. To them it is a terminal in every
  observable way — `isatty()` says yes, `tcsetattr()` works, Ctrl+C
  raises SIGINT.

The two sides are cross-connected through the line discipline:

```
  keyboard/screen side                      program side

  ┌──────────────────┐   write   ┌─────────────────┐   read    ┌───────┐
  │ terminal emulator│ ────────► │ line discipline │ ────────► │ shell │
  │  (master fd)     │ ◄──────── │   (the kernel)  │ ◄──────── │ (pts) │
  └──────────────────┘   read    └─────────────────┘   write   └───────┘
```

Everything the emulator writes to the master comes out of the slave's
`read()` as "keyboard input" — after line-discipline processing. And
everything programs write to the slave arrives at the master's
`read()` as "screen output" for the emulator to draw. When you press
`k` in a shell running under xterm: xterm writes `k` to the master →
the line discipline echoes it back and buffers it → the shell reads
it from the slave. Three processes, two fds, one kernel layer.

This is also how `ssh` works (the remote sshd allocates a pty and
runs your shell on its slave), how `tmux` keeps sessions alive after
you disconnect (tmux holds the masters; your disconnection kills only
the client drawing them), and how `script`, `expect`, and CI systems
fool programs into thinking a human is present.

The modern API for conjuring a pair is small:

```c
#define _XOPEN_SOURCE 600
#include <stdlib.h>
#include <fcntl.h>

int m = posix_openpt(O_RDWR | O_NOCTTY); /* open /dev/ptmx        */
grantpt(m);                              /* fix slave permissions  */
unlockpt(m);                             /* allow slave to open    */
char *name = ptsname(m);                 /* "/dev/pts/N"          */
int s = open(name, O_RDWR | O_NOCTTY);
```

(`O_NOCTTY` says "don't make this my controlling terminal" — we want a
device to experiment on, not to adopt. Controlling terminals, sessions,
and job control are a deep topic; the extended-reading TTY article is
the best tour.)

For this course the pty is our laboratory: tests can't assume a human
at a keyboard, but they can *manufacture a real terminal device*,
apply real termios settings to it, and push real bytes through the
real line discipline. Which is exactly what you'll do now.

## Challenge: Open a Pseudoterminal {#open-pty points=15}

Implement `pty_open_pair()`: create a master/slave pty pair and hand
both fds back to the caller. Then prove the plumbing works both ways.

Details that will bite if skipped:

- Open the master with `O_RDWR | O_NOCTTY`.
- `grantpt()` and `unlockpt()` must both succeed **before** the slave
  is opened — `unlockpt` is the kernel's safety interlock, and
  opening the slave first fails with `EIO`.
- Close the master fd on any failure path; don't leak it.

The tests then verify the classic pty behaviors through your pair:
the slave is a tty (the master, on Linux, is too — but its *name* is
the slave's); a line written to the master emerges from the slave's
`read()` (input direction, through canonical buffering); and bytes
written to the slave emerge from the master (output direction).

### Starter

```c
#define _XOPEN_SOURCE 600
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>

/* Create a pseudoterminal pair.
 * On success: *master_out and *slave_out hold open fds, returns 0.
 * On failure: returns -1 and leaks nothing. */
int pty_open_pair(int *master_out, int *slave_out) {
    /* TODO: posix_openpt(O_RDWR | O_NOCTTY)      -> master fd  */
    /* TODO: grantpt(master), unlockpt(master)    (check both!) */
    /* TODO: ptsname(master)                      -> slave path */
    /* TODO: open(path, O_RDWR | O_NOCTTY)        -> slave fd   */
    /* TODO: on any failure: close what's open, return -1       */
    (void)master_out; (void)slave_out;
    return -1;
}

#ifdef DEMO
#include <stdio.h>
#include <string.h>
int main(void) {
    int m, s;
    if (pty_open_pair(&m, &s) != 0) { perror("pty_open_pair"); return 1; }
    printf("master fd %d, slave fd %d (%s)\n", m, s, ttyname(s));
    write(m, "hello through the kernel\n", 25);
    char buf[64];
    ssize_t n = read(s, buf, sizeof(buf) - 1);
    buf[n > 0 ? n : 0] = '\0';
    printf("slave read %zd bytes: %s", n, buf);
    return 0;
}
#endif
```

### Tests

```c
#define _XOPEN_SOURCE 600
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <poll.h>

int pty_open_pair(int *master_out, int *slave_out);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

/* read with a poll() guard so a broken solution can't hang the suite */
static ssize_t read_timeout(int fd, void *buf, size_t n, int ms) {
    struct pollfd p = { .fd = fd, .events = POLLIN };
    if (poll(&p, 1, ms) <= 0) return -1;
    return read(fd, buf, n);
}

/* portable memmem-lite */
static int contains(const char *hay, size_t hlen, const char *needle, size_t nlen) {
    if (nlen > hlen) return 0;
    for (size_t i = 0; i + nlen <= hlen; i++)
        if (memcmp(hay + i, needle, nlen) == 0) return 1;
    return 0;
}

int main(void) {
    alarm(15);
    int m = -1, s = -1;

    int rc = pty_open_pair(&m, &s);
    check(rc == 0, "test_open_pair_succeeds");
    if (rc != 0) return failed + 1;

    check(m >= 0 && s >= 0, "test_fds_valid");
    check(m != s, "test_fds_distinct");
    check(isatty(s) == 1, "test_slave_is_a_tty");

    const char *sname = ttyname(s);
    check(sname != NULL && strstr(sname, "/dev/") == sname,
          "test_slave_has_device_name");

    /* Input direction: master -> line discipline -> slave.
     * Canonical mode is on by default, so the line is delivered
     * only once the newline arrives — but it IS delivered. */
    {
        const char *line = "ping\n";
        ssize_t w = write(m, line, 5);
        check(w == 5, "test_master_write");
        char buf[64];
        ssize_t r = read_timeout(s, buf, sizeof(buf), 2000);
        check(r == 5 && memcmp(buf, "ping\n", 5) == 0,
              "test_slave_reads_master_line");
    }

    /* Output direction: slave -> master. No newline involved, so no
     * canonical buffering and no output translation to worry about. */
    {
        ssize_t w = write(s, "pong", 4);
        check(w == 4, "test_slave_write");
        char buf[64];
        ssize_t r = read_timeout(m, buf, sizeof(buf), 2000);
        /* the master may first see the echo of "ping\r\n" from the
         * input test above; scan until "pong" shows up */
        int found = (r > 0 && contains(buf, (size_t)r, "pong", 4));
        for (int tries = 0; !found && tries < 4; tries++) {
            r = read_timeout(m, buf, sizeof(buf), 1000);
            found = (r > 0 && contains(buf, (size_t)r, "pong", 4));
        }
        check(found, "test_master_reads_slave_bytes");
    }

    close(m);
    close(s);
    return failed;
}
```
# Lesson: Terminal Settings with termios {#termios-intro}

Everything the line discipline does — echo, buffering, signals,
translation — is configurable, per terminal device, through one struct
and two functions declared in `termios.h`:

```c
#include <termios.h>

struct termios t;
tcgetattr(fd, &t);              /* read the device's current settings  */
t.c_lflag &= ~ECHO;             /* flip some bits                      */
tcsetattr(fd, TCSAFLUSH, &t);   /* write them back                     */
```

This is *the* API. `stty` is a thin wrapper over it; so is everything
vim or ssh does to a terminal. Note that both calls take an **fd**: the
settings belong to the *device*, not to your process. Change them and
they stay changed for everyone using that device until something
changes them back — which is why "restore on exit" is a hard
requirement, not a courtesy.

## The struct, field by field

```c
struct termios {
    tcflag_t c_iflag;    /* input processing:  what happens to arriving bytes */
    tcflag_t c_oflag;    /* output processing: what happens to departing bytes */
    tcflag_t c_cflag;    /* hardware-ish: baud, character size, parity */
    tcflag_t c_lflag;    /* "local": the line discipline's personality */
    cc_t     c_cc[NCCS]; /* control characters: which byte means what */
};
```

Each `tcflag_t` is a bitmask; you flip flags with the usual idioms:

```c
t.c_lflag &= ~(ECHO | ICANON);   /* clear (disable) */
t.c_cflag |= CS8;                /* set (enable)    */
```

### c_lflag — the big personality switches

| Flag     | When set (the default)                                | Raw mode |
|----------|-------------------------------------------------------|----------|
| `ICANON` | line buffering + kernel line editing (Backspace, ^U)  | clear    |
| `ECHO`   | kernel echoes input back to the display               | clear    |
| `ISIG`   | ^C→SIGINT, ^Z→SIGTSTP, ^\→SIGQUIT                     | clear    |
| `IEXTEN` | extended input processing (^V literal-next, ^O)       | clear    |

`ICANON` and `ECHO` are the headliners. Clearing `ISIG` means Ctrl+C
becomes just a byte (0x03) delivered to you — your program decides what
"interrupt" means. Clearing `IEXTEN` closes the odd loopholes.

### c_iflag — input translation

| Flag     | When set                                              | Raw mode |
|----------|-------------------------------------------------------|----------|
| `IXON`   | ^S/^Q pause and resume output (software flow control) | clear    |
| `ICRNL`  | translate incoming `\r` (Enter) into `\n`             | clear    |
| `BRKINT` | serial "break" sends SIGINT                           | clear    |
| `INPCK`  | parity checking (serial-line era)                     | clear    |
| `ISTRIP` | strip input bytes to 7 bits                           | clear    |

With `ICRNL` cleared you'll see Enter as it truly arrives: `\r`, byte
0x0D. Your key decoder (a few lessons from now) must know that.
`BRKINT`, `INPCK`, `ISTRIP` are fossils of real serial hardware —
clearing them is tradition and costs nothing.

### c_oflag — output translation

| Flag    | When set                                        | Raw mode |
|---------|-------------------------------------------------|----------|
| `OPOST` | enable all output processing                    | clear    |
| `ONLCR` | (under OPOST) translate outgoing `\n` to `\r\n` | —        |

Clearing `OPOST` turns off *all* output massaging — most visibly
`ONLCR`. From then on `\n` moves the cursor **down one row without
returning to column 0**, staircasing your text like

```
first line
          second line
                     third line
```

That's not a bug; it's two motions you're now responsible for
distinguishing. Full-screen programs don't mind — they position the
cursor explicitly anyway.

### c_cflag and c_cc

`c_cflag` matters to us only for `CS8`: 8-bit characters, please (yet
another serial fossil — 7-bit terminals were real). In `c_cc`, the
control-character array, two "characters" aren't characters at all but
read-timing knobs — and they're important enough to get their own
section.

## VMIN and VTIME: how much, how long

With `ICANON` cleared, when does `read()` return? Two bytes in `c_cc`
decide:

- **`VMIN`** — the minimum number of bytes before `read` may return.
- **`VTIME`** — a timeout in **tenths of a second**.

The four quadrants:

| VMIN | VTIME | `read()` behavior |
|------|-------|-------------------|
| 0    | 0     | never blocks: returns whatever is there, possibly 0 |
| >0   | 0     | blocks until VMIN bytes exist (classic blocking read) |
| 0    | >0    | returns on first byte OR after VTIME expires with 0 |
| >0   | >0    | inter-byte timer: first byte starts the clock |

`VMIN=1, VTIME=0` is the sane default for interactive programs: block
until there's *something*, deliver it immediately. `VMIN=0, VTIME=1`
(return within 0.1 s no matter what) is the poor-man's event loop —
the kilo editor uses it. We'll use `VMIN=1` and get our timeouts a
better way, with `poll()`, in the next lesson.

## Applying settings: the third argument

`tcsetattr(fd, when, &t)` — the `when` matters:

- **`TCSANOW`** — apply immediately, even mid-output.
- **`TCSADRAIN`** — wait for pending output to finish first. Use when
  changing output flags so queued text isn't garbled.
- **`TCSAFLUSH`** — drain output *and discard pending unread input*.
  The right choice when entering/leaving raw mode: keystrokes typed
  before the switch die instead of leaking into the new regime.

One more trap, straight from the man page: `tcsetattr` returns success
if *any* requested change succeeded. Paranoid code calls `tcgetattr`
afterwards and compares. (Our tests do exactly that to your code.)

## Raw mode, assembled

Putting the whole lesson in one function — this is `cfmakeraw(3)`
reimplemented by hand, and it's the next challenge:

```c
raw.c_iflag &= ~(BRKINT | ICRNL | INPCK | ISTRIP | IXON);
raw.c_oflag &= ~OPOST;
raw.c_cflag |= CS8;
raw.c_lflag &= ~(ECHO | ICANON | IEXTEN | ISIG);
raw.c_cc[VMIN]  = 1;
raw.c_cc[VTIME] = 0;
```

And the discipline that goes with it, which every serious tool
follows: **save the original struct before touching anything, restore
it on every exit path** — normal exit, error, fatal signal. A program
that dies in raw mode leaves the shell deaf and mute (`reset` fixes
it, but users shouldn't need to know that incantation).

## Challenge: Enable Raw Mode {#raw-mode points=15}

Three functions:

- `termios_make_raw(struct termios *t)` — mutate a settings struct
  into the raw configuration above. Pure: no fd, no syscalls, no
  side effects. (Testable to the last bit, and reusable on any fd.)
- `raw_mode_enable(int fd, struct termios *saved)` — snapshot the
  device's current settings into `*saved`, then apply the raw
  configuration to it with `TCSAFLUSH`. Return 0, or -1 on error.
- `raw_mode_restore(int fd, const struct termios *saved)` — put the
  snapshot back (again `TCSAFLUSH`). Return 0 or -1.

The tests run against a real pseudoterminal: they check every flag
bit before and after, and then do the behavioral proof — in raw mode
a single keystroke byte (no newline!) written to the master must be
immediately readable from the slave.

### Starter

```c
#define _XOPEN_SOURCE 600
#include <termios.h>
#include <unistd.h>

/* Turn *t into a raw-mode configuration (pure function).
 * Clear: ECHO, ICANON, IEXTEN, ISIG   (c_lflag)
 *        BRKINT, ICRNL, INPCK, ISTRIP, IXON   (c_iflag)
 *        OPOST                        (c_oflag)
 * Set:   CS8                          (c_cflag)
 * Reads: VMIN = 1, VTIME = 0          (c_cc) */
void termios_make_raw(struct termios *t) {
    /* TODO */
    (void)t;
}

/* Save fd's current settings into *saved, then switch fd to raw mode.
 * Use TCSAFLUSH. Return 0 on success, -1 on any failure. */
int raw_mode_enable(int fd, struct termios *saved) {
    /* TODO: tcgetattr -> *saved                        */
    /* TODO: copy, termios_make_raw, tcsetattr TCSAFLUSH */
    (void)fd; (void)saved;
    return -1;
}

/* Restore previously saved settings. Return 0 on success, -1 on failure. */
int raw_mode_restore(int fd, const struct termios *saved) {
    /* TODO */
    (void)fd; (void)saved;
    return -1;
}

#ifdef DEMO
#include <stdio.h>
#include <stdlib.h>
static struct termios g_saved;
static void cleanup(void) { raw_mode_restore(STDIN_FILENO, &g_saved); }
int main(void) {
    if (raw_mode_enable(STDIN_FILENO, &g_saved) != 0) {
        perror("raw_mode_enable (is stdin a terminal?)");
        return 1;
    }
    atexit(cleanup);
    const char *msg = "raw mode: press keys to see their bytes, q to quit\r\n";
    write(STDOUT_FILENO, msg, 52);
    unsigned char c;
    while (read(STDIN_FILENO, &c, 1) == 1 && c != 'q') {
        char line[32];
        int n = snprintf(line, sizeof(line), "byte 0x%02x\r\n", c);
        write(STDOUT_FILENO, line, (size_t)n);
    }
    return 0;
}
#endif
```

### Tests

```c
#define _XOPEN_SOURCE 600
#include <termios.h>
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <poll.h>

void termios_make_raw(struct termios *t);
int raw_mode_enable(int fd, struct termios *saved);
int raw_mode_restore(int fd, const struct termios *saved);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static int open_pty(int *m, int *s) {
    *m = posix_openpt(O_RDWR | O_NOCTTY);
    if (*m < 0) return -1;
    if (grantpt(*m) != 0 || unlockpt(*m) != 0) { close(*m); return -1; }
    const char *name = ptsname(*m);
    if (!name) { close(*m); return -1; }
    *s = open(name, O_RDWR | O_NOCTTY);
    if (*s < 0) { close(*m); return -1; }
    return 0;
}

int main(void) {
    alarm(15);

    /* --- the pure function, bit by bit --- */
    {
        struct termios t;
        memset(&t, 0xff, sizeof(t)); /* start with everything set */
        termios_make_raw(&t);
        check((t.c_lflag & ICANON) == 0, "test_icanon_cleared");
        check((t.c_lflag & ECHO)   == 0, "test_echo_cleared");
        check((t.c_lflag & ISIG)   == 0, "test_isig_cleared");
        check((t.c_lflag & IEXTEN) == 0, "test_iexten_cleared");
        check((t.c_iflag & IXON)   == 0, "test_ixon_cleared");
        check((t.c_iflag & ICRNL)  == 0, "test_icrnl_cleared");
        check((t.c_iflag & BRKINT) == 0, "test_brkint_cleared");
        check((t.c_oflag & OPOST)  == 0, "test_opost_cleared");
        check((t.c_cflag & CS8)    == CS8, "test_cs8_set");
        check(t.c_cc[VMIN] == 1,  "test_vmin_1");
        check(t.c_cc[VTIME] == 0, "test_vtime_0");
    }

    /* --- against a real device --- */
    int m, s;
    if (open_pty(&m, &s) != 0) {
        printf("--- FAIL: pty_setup\n");
        return failed + 1;
    }

    struct termios before, saved, active;
    tcgetattr(s, &before);
    check((before.c_lflag & ICANON) != 0, "test_pty_starts_canonical");

    int rc = raw_mode_enable(s, &saved);
    check(rc == 0, "test_enable_returns_zero");

    /* saved must be the ORIGINAL settings, not the raw ones */
    check(saved.c_lflag == before.c_lflag && saved.c_iflag == before.c_iflag,
          "test_saved_is_original");

    tcgetattr(s, &active);
    check((active.c_lflag & (ICANON | ECHO)) == 0, "test_device_is_raw");
    check((active.c_oflag & OPOST) == 0, "test_device_opost_off");
    check(active.c_cc[VMIN] == 1 && active.c_cc[VTIME] == 0,
          "test_device_vmin_vtime");

    /* behavioral proof: one byte, no newline, readable immediately */
    {
        write(m, "x", 1);
        struct pollfd p = { .fd = s, .events = POLLIN };
        int ready = poll(&p, 1, 2000);
        check(ready == 1, "test_raw_byte_ready_without_newline");
        unsigned char c = 0;
        if (ready == 1) read(s, &c, 1);
        check(c == 'x', "test_raw_byte_value");
    }

    /* echo must be off: nothing should bounce back to the master */
    {
        write(m, "y", 1);
        struct pollfd pin = { .fd = s, .events = POLLIN };
        if (poll(&pin, 1, 500) == 1) {
            unsigned char c;
            read(s, &c, 1); /* consume it on the slave side */
        }
        struct pollfd p = { .fd = m, .events = POLLIN };
        check(poll(&p, 1, 200) == 0, "test_no_echo_in_raw_mode");
    }

    rc = raw_mode_restore(s, &saved);
    check(rc == 0, "test_restore_returns_zero");
    tcgetattr(s, &active);
    check((active.c_lflag & ICANON) == (before.c_lflag & ICANON) &&
          (active.c_lflag & ECHO)   == (before.c_lflag & ECHO),
          "test_restore_puts_flags_back");

    close(m);
    close(s);
    return failed;
}
```

## Challenge: Read Timeouts with VMIN and VTIME {#vmin-vtime points=15}

Before we abandon VMIN/VTIME for `poll()`, prove you can drive them —
plenty of real code (including kilo) does its event timing this way,
and understanding the quadrant table beats memorizing it.

Implement `term_set_read_timing(fd, vmin, vtime)`: fetch the device's
current termios, set `c_cc[VMIN]` and `c_cc[VTIME]`, apply with
`TCSANOW`. The tests put a pty slave into raw mode, then:

1. `VMIN=0, VTIME=0` with no data → `read` returns 0 immediately
   (the polling read).
2. `VMIN=0, VTIME=3` with no data → `read` blocks ~0.3 s, then
   returns 0. The test asserts the elapsed time is at least 0.15 s
   (it really waited) and under 3 s (it didn't hang).
3. `VMIN=0, VTIME=5` with data already waiting → `read` returns it
   immediately, well before the timeout.

### Starter

```c
#define _XOPEN_SOURCE 600
#include <termios.h>
#include <unistd.h>

/* Set the read-timing knobs on fd's termios (leave everything else).
 * vmin:  minimum bytes before read() may return
 * vtime: timeout in tenths of a second
 * Apply with TCSANOW. Return 0 on success, -1 on failure. */
int term_set_read_timing(int fd, int vmin, int vtime) {
    /* TODO: tcgetattr, set c_cc[VMIN] and c_cc[VTIME], tcsetattr */
    (void)fd; (void)vmin; (void)vtime;
    return -1;
}
```

### Tests

```c
#define _XOPEN_SOURCE 600
#define _POSIX_C_SOURCE 200809L
#include <termios.h>
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <time.h>

int term_set_read_timing(int fd, int vmin, int vtime);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static double now_s(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec / 1e9;
}

int main(void) {
    alarm(20);

    int m = posix_openpt(O_RDWR | O_NOCTTY);
    if (m < 0 || grantpt(m) != 0 || unlockpt(m) != 0) {
        printf("--- FAIL: pty_setup\n");
        return 1;
    }
    int s = open(ptsname(m), O_RDWR | O_NOCTTY);
    if (s < 0) { printf("--- FAIL: pty_slave\n"); return 1; }

    /* raw-ish mode so canonical buffering doesn't interfere */
    struct termios t;
    tcgetattr(s, &t);
    t.c_lflag &= ~(tcflag_t)(ICANON | ECHO);
    tcsetattr(s, TCSANOW, &t);

    /* quadrant 1: VMIN=0 VTIME=0 -> instant empty read */
    {
        int rc = term_set_read_timing(s, 0, 0);
        check(rc == 0, "test_set_timing_returns_zero");
        struct termios got;
        tcgetattr(s, &got);
        check(got.c_cc[VMIN] == 0 && got.c_cc[VTIME] == 0,
              "test_knobs_applied");
        /* with the knobs unset, the reads below would block forever */
        if (rc != 0 || got.c_cc[VMIN] != 0) {
            printf("--- FAIL: knobs_not_applied_bailing_out\n");
            return failed + 1;
        }
        double t0 = now_s();
        unsigned char c;
        ssize_t r = read(s, &c, 1);
        double dt = now_s() - t0;
        check(r == 0, "test_poll_read_returns_zero");
        check(dt < 0.5, "test_poll_read_is_immediate");
    }

    /* quadrant 3: VMIN=0 VTIME=3 -> ~0.3s wait, then empty */
    {
        term_set_read_timing(s, 0, 3);
        double t0 = now_s();
        unsigned char c;
        ssize_t r = read(s, &c, 1);
        double dt = now_s() - t0;
        check(r == 0, "test_vtime_read_returns_zero");
        check(dt >= 0.15, "test_vtime_actually_waited");
        check(dt < 3.0, "test_vtime_did_not_hang");
    }

    /* data waiting beats the timer */
    {
        term_set_read_timing(s, 0, 5);
        write(m, "z", 1);
        double t0 = now_s();
        unsigned char c = 0;
        ssize_t r = read(s, &c, 1);
        double dt = now_s() - t0;
        check(r == 1 && c == 'z', "test_data_returned");
        check(dt < 0.4, "test_data_beats_timer");
    }

    close(m);
    close(s);
    return failed;
}
```
# Lesson: Waiting for Input — poll and select {#event-io}

An interactive program spends nearly all of its life doing nothing,
and doing nothing well is a genuine engineering problem. Your editor's
main loop wants to say: *"wake me when the user types, but no later
than 100 ms from now, because I might also have a resize to handle or
a status message to expire."* A plain blocking `read` can't express
"no later than"; a `VMIN=0` polling read can, but only by burning CPU
in a spin loop or by committing the whole program to VTIME's
tenth-of-a-second granularity on one fd.

The general answer — the one every event loop from vim to nginx to
your terminal emulator is built on — is **readiness notification**:
ask the kernel to block *for you*, on a *set* of fds, with a deadline.

## poll()

```c
#include <poll.h>

struct pollfd {
    int   fd;       /* which descriptor                   */
    short events;   /* what you care about (POLLIN, ...)  */
    short revents;  /* what actually happened (kernel fills) */
};

int poll(struct pollfd *fds, nfds_t nfds, int timeout_ms);
```

You hand `poll` an array of fds and what you're waiting for
(`POLLIN` — readable; `POLLOUT` — writable without blocking), plus a
timeout in milliseconds (`-1` = forever, `0` = just check). It returns:

- **> 0**: that many entries have nonzero `revents`. Check each.
- **0**: the timeout expired; nothing happened.
- **-1**: error — and yes, `EINTR` again: a signal (SIGWINCH!) woke
  the process. For us that's a feature, but the caller must decide
  whether to retry, and with how much of the deadline left.

The crucial semantic: `poll` tells you a read **won't block**, not how
much data there is. The contract is *"one `read` will return
something"* — maybe 1 byte. Readiness, then short read, then loop:
the two lessons compose.

`POLLHUP` (hang-up: the other end closed) and `POLLERR` can appear in
`revents` unrequested. Treat them as "readable" — the `read` will
return 0 or an error, which is exactly the news you need.

## select(), for the record

`select(2)` is `poll`'s older sibling: same idea, clunkier interface
(fd bitmasks you rebuild every call, a `struct timeval` the kernel may
scribble on, and a hard `FD_SETSIZE` ceiling of 1024 fds). You'll read
it in older code — kilo's tutorial era used it — but there is no
reason to write new `select` code. For thousands of fds there's
`epoll` (Linux) / `kqueue` (BSD); a terminal watching one or two fds
needs none of that.

## Deadlines that survive EINTR

There's a subtle bug lurking in the obvious retry loop:

```c
while (poll(&p, 1, timeout_ms) < 0 && errno == EINTR)
    ;  /* BUG: restarts the FULL timeout after every signal */
```

If signals arrive steadily (a user leaning on the resize handle), the
deadline recedes forever. Correct code computes the deadline **once**,
against a monotonic clock, and re-arms `poll` with whatever remains:

```c
struct timespec ts;
clock_gettime(CLOCK_MONOTONIC, &ts);
long deadline_ms = ts.tv_sec * 1000 + ts.tv_nsec / 1000000 + timeout_ms;
/* after EINTR: remaining = deadline_ms - now_ms; if <= 0, timed out */
```

`CLOCK_MONOTONIC`, not `time()` or `CLOCK_REALTIME`: wall clocks jump
(NTP, DST, a sysadmin); the monotonic clock only marches forward.

## Challenge: Poll with a Deadline {#poll-timeout points=15}

Two functions that will sit at the heart of your event loop:

- `wait_readable(fd, timeout_ms)` — block until fd is readable or the
  deadline passes. Return 1 (readable), 0 (timeout), -1 (error).
  `timeout_ms < 0` means wait forever. **EINTR must not restart the
  full timeout** — recompute the remainder from a monotonic clock.
- `read_byte_timeout(fd, out, timeout_ms)` — the composition: wait,
  then read exactly one byte. Return 1 (got a byte), 0 (timeout),
  -1 (error or EOF).

Tests: data already waiting returns immediately; a writer that shows
up 150 ms in is caught within the deadline; an empty fd times out in
roughly the right amount of time (not instantly, not forever).

### Starter

```c
#define _POSIX_C_SOURCE 200809L
#include <poll.h>
#include <unistd.h>
#include <errno.h>
#include <time.h>

static long now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000L + ts.tv_nsec / 1000000L;
}

/* Block until fd is readable or timeout_ms elapses.
 * timeout_ms < 0 = wait forever.
 * Returns 1 = readable, 0 = timed out, -1 = error.
 * On EINTR, resume waiting with the REMAINING time. */
int wait_readable(int fd, int timeout_ms) {
    /* TODO: compute the deadline once (if timeout_ms >= 0)     */
    /* TODO: loop: poll(POLLIN); on EINTR recompute remainder   */
    /* TODO: map poll's 1/0/-1 to ours; POLLHUP counts as ready */
    (void)fd; (void)timeout_ms; (void)now_ms;
    return -1;
}

/* Wait for a byte, then read it.
 * Returns 1 = byte stored in *out, 0 = timeout, -1 = error or EOF. */
int read_byte_timeout(int fd, unsigned char *out, int timeout_ms) {
    /* TODO: wait_readable, then read(fd, out, 1)  */
    /* TODO: read returning 0 (EOF) -> return -1   */
    (void)fd; (void)out; (void)timeout_ms;
    return -1;
}
```

### Tests

```c
#define _POSIX_C_SOURCE 200809L
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <signal.h>
#include <time.h>
#include <sys/wait.h>

int wait_readable(int fd, int timeout_ms);
int read_byte_timeout(int fd, unsigned char *out, int timeout_ms);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static double now_s(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec / 1e9;
}

static void sleep_ms(int ms) {
    struct timespec ts = { ms / 1000, (ms % 1000) * 1000000L };
    nanosleep(&ts, NULL);
}

int main(void) {
    alarm(20);

    /* data already waiting -> immediate 1 */
    {
        int pfd[2];
        pipe(pfd);
        write(pfd[1], "a", 1);
        double t0 = now_s();
        int r = wait_readable(pfd[0], 2000);
        double dt = now_s() - t0;
        check(r == 1, "test_ready_returns_one");
        check(dt < 0.5, "test_ready_is_immediate");

        unsigned char c = 0;
        r = read_byte_timeout(pfd[0], &c, 1000);
        check(r == 1 && c == 'a', "test_read_byte_value");
        close(pfd[0]);
        close(pfd[1]);
    }

    /* nothing arrives -> 0, after roughly the requested wait */
    {
        int pfd[2];
        pipe(pfd);
        double t0 = now_s();
        int r = wait_readable(pfd[0], 300);
        double dt = now_s() - t0;
        check(r == 0, "test_timeout_returns_zero");
        check(dt >= 0.2, "test_timeout_waited");
        check(dt < 5.0, "test_timeout_bounded");
        close(pfd[0]);
        close(pfd[1]);
    }

    /* a late writer is caught before the deadline */
    {
        int pfd[2];
        pipe(pfd);
        pid_t pid = fork();
        if (pid == 0) {
            close(pfd[0]);
            sleep_ms(150);
            write(pfd[1], "b", 1);
            _exit(0);
        }
        close(pfd[1]);
        double t0 = now_s();
        unsigned char c = 0;
        int r = read_byte_timeout(pfd[0], &c, 3000);
        double dt = now_s() - t0;
        check(r == 1 && c == 'b', "test_late_writer_caught");
        check(dt >= 0.05 && dt < 3.0, "test_late_writer_timing");
        int st;
        waitpid(pid, &st, 0);
        close(pfd[0]);
    }

    /* EOF is not a timeout and not a byte: -1 */
    {
        int pfd[2];
        pipe(pfd);
        close(pfd[1]); /* immediate EOF */
        unsigned char c;
        int r = read_byte_timeout(pfd[0], &c, 1000);
        check(r == -1, "test_eof_is_error");
        close(pfd[0]);
    }

    return failed;
}
```
# Lesson: ANSI Escape Sequences — the Wire Protocol {#ansi-basics}

Raw mode gave us a byte pipe with no kernel meddling. Now: what do the
bytes *mean*? How does `htop` paint bars in the middle of the screen,
how does vim turn a line yellow, how does anything ever un-print?

The answer is an in-band protocol: special byte sequences, mixed right
into the output stream, that the terminal interprets as commands
instead of printing. There is no second channel — no ioctl for "move
the cursor", no side API for "make it red". The screen is programmed
entirely through the same fd the text flows through. This design is
why output can be piped, logged, replayed, and sent over ssh without
anyone in the middle understanding it — and why `cat`-ing a binary
file can wreck your terminal (you just fed it random commands).

## A short history you actually need

Every terminal vendor of the 1970s invented its own control codes,
and portable software drowned in the differences (the `termcap`
database, and later `terminfo`, exist to catalogue that chaos — more
below). ANSI standardized a common language in **ANSI X3.64** (1979),
which grew into **ECMA-48** / ISO 6429. DEC's **VT100** (1978) was the
hit implementation, so the family is called "ANSI escape codes" and
"VT100 sequences" interchangeably. Everything since — xterm, Linux
console, tmux, kitty, iTerm2, Windows Terminal — is a VT100 descendant
with extensions. Implement the VT100 core and you speak to fifty years
of software.

## The byte grammar

Codes 0x00–0x1F are the **C0 control characters** — the originals,
each one a tiny command:

| Byte | Name | Meaning |
|------|------|---------|
| 0x07 | BEL  | ring the bell |
| 0x08 | BS   | cursor left one column (doesn't erase!) |
| 0x09 | HT   | tab: cursor to next tab stop (every 8 by default) |
| 0x0A | LF   | cursor down one row |
| 0x0D | CR   | cursor to column 1 |
| 0x1B | ESC  | *the* escape: the next bytes are a command |

Multi-byte commands start with **ESC** (0x1B, `"\x1b"`, sometimes
written `^[`). ESC followed by most single characters is a simple
command (`ESC 7` save cursor, `ESC 8` restore, `ESC c` full reset).
But the workhorse is ESC followed by `[` — the **Control Sequence
Introducer (CSI)** — which begins a parameterized command:

```
CSI  =  ESC [
full sequence  =  ESC [ <parameters> <intermediates> <final byte>
```

- **Parameters** (bytes 0x30–0x3F): decimal numbers separated by `;`,
  e.g. `12;40`. Empty slots are allowed and mean "default":
  `ESC[;5H` has an empty first parameter. A leading `?` marks a
  **private** parameter space (DEC's extensions live there).
- **Intermediates** (0x20–0x2F): rare; you can ignore them for years.
- **Final byte** (0x40–0x7E): one character that names the command.
  The final byte is what dispatches — `H` is "cursor position"
  whether it has zero, one, or two parameters.

This grammar is fixed and machine-readable *without understanding the
command*, which is what makes a clean parser possible (two lessons
from now): you always know where a sequence ends.

## The sequences that matter

Cursor movement:

```
ESC [ <r> ; <c> H    CUP   cursor to row r, column c (1-BASED! both default 1)
ESC [ <n> A          CUU   up n rows        (n defaults to 1)
ESC [ <n> B          CUD   down n
ESC [ <n> C          CUF   right n
ESC [ <n> D          CUB   left n
```

Row and column are **1-based**: `ESC[1;1H` (or just `ESC[H`) is the
top-left corner. Fifty years of off-by-one bugs live in that fact —
your C code counts from 0, the wire counts from 1, and the conversion
belongs in exactly one place in your program.

Erasing (these do not move the cursor):

```
ESC [ 0 J    ED    erase cursor -> end of screen   (0 = default)
ESC [ 1 J          erase start of screen -> cursor
ESC [ 2 J          erase entire screen
ESC [ 0 K    EL    erase cursor -> end of line     (0 = default)
ESC [ 1 K          erase start of line -> cursor
ESC [ 2 K          erase entire line
```

`ESC[K` after redrawing a line's content is the cheap way to clear
stale tail characters — you'll use it in the renderer, it beats
clearing the whole screen and repainting.

DEC private modes — set with final `h`, reset with `l`:

```
ESC [ ? 25 h / l     show / hide the cursor
ESC [ ? 1049 h / l   enter / leave the ALTERNATE SCREEN
ESC [ ? 2004 h / l   bracketed paste on / off
```

Hiding the cursor while repainting kills the ghostly flicker of a
cursor teleporting through a redraw. The **alternate screen** is the
trick behind vim and less feeling like "apps": a second framebuffer
with no scrollback; enter it on startup, leave on exit, and the
user's shell history reappears untouched.

Finally, `ESC ] ...` (note `]`, not `[`) starts an **OSC** — Operating
System Command — string, terminated by BEL or `ESC \`. OSC 0 sets the
window title: `ESC ] 0 ; my title BEL`. Your parser must at least
*skip* these correctly, or one title-setting program will desync your
whole stream.

## Colors and attributes: SGR

The final byte `m` — **Select Graphic Rendition** — takes a *list* of
attribute codes and is by far the most-sent sequence in practice:

| Code | Effect |
|------|--------|
| 0 | reset everything to default |
| 1 | bold |
| 4 | underline |
| 7 | reverse video (swap fg/bg — your status bar) |
| 30–37 | foreground: black, red, green, yellow, blue, magenta, cyan, white |
| 39 | default foreground |
| 40–47 / 49 | background versions of the same |
| 90–97 | bright foreground variants |

Codes chain in one sequence: `ESC[1;33;44m` = bold yellow on blue.
And SGR is **stateful**: it sets the *current brush*, which applies to
everything printed until changed. Forget `ESC[0m` and your prompt
inherits your last color — a bug you have certainly already seen in
the wild.

Two modern extensions squeeze bigger palettes through the same door:

```
ESC [ 38 ; 5 ; <n> m         foreground from a 256-color palette
ESC [ 48 ; 5 ; <n> m         background, same palette
ESC [ 38 ; 2 ; <r> ; <g> ; <b> m    24-bit "truecolor" foreground
ESC [ 48 ; 2 ; <r> ; <g> ; <b> m    truecolor background
```

Note what `38;5;208` implies for parsers: parameters are no longer
independent — `5` and `208` are *arguments of* `38`. Keep that in
mind when you write yours.

## Where do sequences come from in practice? (terminfo)

Not every terminal supports every sequence, and historically they
disagreed wildly. The `terminfo` database (query it with `infocmp`;
the `$TERM` env var picks the entry) maps capability names to the
bytes each terminal wants, and libraries like ncurses read it so
programs don't hardcode. We *will* hardcode the VT100/xterm core —
every terminal you'll meet this decade speaks it — but you should
know why `TERM=dumb make` and ncurses exist, and what breaks when
`$TERM` lies.

Try the protocol by hand right now — no code needed:

```
$ printf '\x1b[2J\x1b[10;20HHello\x1b[0m'
$ printf '\x1b[1;31mred and bold\x1b[0m plain\n'
$ printf '\x1b[?25l'; sleep 2; printf '\x1b[?25h'   # cursor vanishes
```

## Challenge: Sequence Builders {#cursor-move points=15}

Escape sequences are just formatted strings, and we want them **in
buffers, not written straight to fd 1** — the renderer will batch
everything into one `write`. So every builder here takes a
destination buffer and returns how many bytes it wrote:

```c
int n = ansi_cursor_move(buf, sizeof(buf), 5, 10);
/* buf now holds "\x1b[5;10H", n == 7 */
```

Contract for **all** builders: write the sequence into `dst`, return
its length in bytes; if it wouldn't fit in `cap` (including the NUL
that `snprintf` writes), write nothing useful and return -1. Let
`snprintf`'s return value do the heavy lifting — it returns the
length the output *would have* (excluding the NUL), so
`n < 0 || n >= (int)cap` is exactly the failure test.

Build: cursor move / up / down, screen clear, line clear, cursor
hide/show, and alternate-screen enter/leave.

### Starter

```c
#include <stdio.h>
#include <stddef.h>

/* Every builder: writes the escape sequence into dst, returns its
 * length in bytes, or -1 if it doesn't fit in cap. */

/* ESC[<row>;<col>H  — cursor to (row, col), both 1-based */
int ansi_cursor_move(char *dst, size_t cap, int row, int col) {
    int n = snprintf(dst, cap, "\x1b[%d;%dH", row, col);
    return (n < 0 || (size_t)n >= cap) ? -1 : n;
}

/* ESC[<n>A — cursor up n rows */
int ansi_cursor_up(char *dst, size_t cap, int n) {
    /* TODO */
    (void)dst; (void)cap; (void)n;
    return -1;
}

/* ESC[<n>B — cursor down n rows */
int ansi_cursor_down(char *dst, size_t cap, int n) {
    /* TODO */
    (void)dst; (void)cap; (void)n;
    return -1;
}

/* ESC[2J — erase entire screen */
int ansi_clear_screen(char *dst, size_t cap) {
    /* TODO */
    (void)dst; (void)cap;
    return -1;
}

/* ESC[K — erase from cursor to end of line */
int ansi_clear_line(char *dst, size_t cap) {
    /* TODO */
    (void)dst; (void)cap;
    return -1;
}

/* ESC[?25l — hide cursor */
int ansi_cursor_hide(char *dst, size_t cap) {
    /* TODO */
    (void)dst; (void)cap;
    return -1;
}

/* ESC[?25h — show cursor */
int ansi_cursor_show(char *dst, size_t cap) {
    /* TODO */
    (void)dst; (void)cap;
    return -1;
}

/* ESC[?1049h — switch to the alternate screen */
int ansi_alt_screen_enter(char *dst, size_t cap) {
    /* TODO */
    (void)dst; (void)cap;
    return -1;
}

/* ESC[?1049l — return to the main screen */
int ansi_alt_screen_leave(char *dst, size_t cap) {
    /* TODO */
    (void)dst; (void)cap;
    return -1;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

int ansi_cursor_move(char *dst, size_t cap, int row, int col);
int ansi_cursor_up(char *dst, size_t cap, int n);
int ansi_cursor_down(char *dst, size_t cap, int n);
int ansi_clear_screen(char *dst, size_t cap);
int ansi_clear_line(char *dst, size_t cap);
int ansi_cursor_hide(char *dst, size_t cap);
int ansi_cursor_show(char *dst, size_t cap);
int ansi_alt_screen_enter(char *dst, size_t cap);
int ansi_alt_screen_leave(char *dst, size_t cap);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void expect(int got_len, const char *buf, const char *want,
                   const char *name) {
    int want_len = (int)strlen(want);
    check(got_len == want_len && memcmp(buf, want, (size_t)want_len) == 0,
          name);
}

int main(void) {
    char buf[64];

    expect(ansi_cursor_move(buf, sizeof(buf), 5, 10), buf,
           "\x1b[5;10H", "test_cursor_move");
    expect(ansi_cursor_move(buf, sizeof(buf), 1, 1), buf,
           "\x1b[1;1H", "test_cursor_move_home");
    expect(ansi_cursor_move(buf, sizeof(buf), 120, 300), buf,
           "\x1b[120;300H", "test_cursor_move_big");

    expect(ansi_cursor_up(buf, sizeof(buf), 3), buf,
           "\x1b[3A", "test_cursor_up");
    expect(ansi_cursor_down(buf, sizeof(buf), 7), buf,
           "\x1b[7B", "test_cursor_down");

    expect(ansi_clear_screen(buf, sizeof(buf)), buf,
           "\x1b[2J", "test_clear_screen");
    expect(ansi_clear_line(buf, sizeof(buf)), buf,
           "\x1b[K", "test_clear_line");

    expect(ansi_cursor_hide(buf, sizeof(buf)), buf,
           "\x1b[?25l", "test_cursor_hide");
    expect(ansi_cursor_show(buf, sizeof(buf)), buf,
           "\x1b[?25h", "test_cursor_show");

    expect(ansi_alt_screen_enter(buf, sizeof(buf)), buf,
           "\x1b[?1049h", "test_alt_screen_enter");
    expect(ansi_alt_screen_leave(buf, sizeof(buf)), buf,
           "\x1b[?1049l", "test_alt_screen_leave");

    /* too-small buffers must report failure, not truncate silently */
    char tiny[4];
    check(ansi_cursor_move(tiny, sizeof(tiny), 10, 20) == -1,
          "test_move_reports_overflow");
    check(ansi_cursor_hide(tiny, sizeof(tiny)) == -1,
          "test_hide_reports_overflow");
    check(ansi_clear_screen(tiny, 1) == -1,
          "test_clear_reports_overflow");

    return failed;
}
```

## Challenge: Colors and Styles — SGR {#sgr-styles points=20}

Model a text style as data, then compile it to the wire format:

```c
enum color_mode { COLOR_DEFAULT, COLOR_BASIC, COLOR_256, COLOR_RGB };

struct color {
    enum color_mode mode;
    unsigned char idx;      /* BASIC: 0-7, C256: 0-255 */
    unsigned char r, g, b;  /* RGB */
};

struct style {
    struct color fg, bg;
    unsigned bold : 1, underline : 1, reverse : 1;
};
```

`sgr_encode(dst, cap, &style)` must emit **one combined sequence**
that fully establishes the style from an unknown state. The reliable
recipe (and the required output format):

1. Start from reset: begin the parameter list with `0`.
2. Append `1` if bold, `4` if underline, `7` if reverse — in that
   order.
3. Append the foreground: nothing for `COLOR_DEFAULT` (reset already
   chose it), `3<idx>` for basic, `38;5;<idx>` for 256-color,
   `38;2;<r>;<g>;<b>` for RGB.
4. Same for background with `4<idx>` / `48;5;…` / `48;2;…`.
5. Final byte `m`.

So bold red on default is `\x1b[0;1;31m`; plain default-on-default is
`\x1b[0m`. Also provide `sgr_reset` (emit exactly `\x1b[0m`) and
`style_eq` — the renderer will soon need "is the brush already
right?" to avoid spamming SGR before every character.

Buffer contract: same as the previous challenge (length or -1).

### Starter

```c
#include <stdio.h>
#include <string.h>
#include <stddef.h>

enum color_mode { COLOR_DEFAULT, COLOR_BASIC, COLOR_256, COLOR_RGB };

struct color {
    enum color_mode mode;
    unsigned char idx;
    unsigned char r, g, b;
};

struct style {
    struct color fg, bg;
    unsigned bold : 1, underline : 1, reverse : 1;
};

/* Emit "\x1b[0m". Return length or -1 if it doesn't fit. */
int sgr_reset(char *dst, size_t cap) {
    /* TODO */
    (void)dst; (void)cap;
    return -1;
}

/* Emit one combined SGR establishing *st from scratch:
 *   \x1b[0 [;1] [;4] [;7] [;fg-params] [;bg-params] m
 * Return length or -1 if it doesn't fit in cap.
 * HINT: build the parameter list into dst incrementally with
 * snprintf(dst + len, cap - len, ...), checking each step. */
int sgr_encode(char *dst, size_t cap, const struct style *st) {
    /* TODO: "\x1b[0"                                     */
    /* TODO: ";1" ";4" ";7" for bold/underline/reverse    */
    /* TODO: fg: BASIC ";3%d"  C256 ";38;5;%d"  RGB ";38;2;%d;%d;%d" */
    /* TODO: bg: BASIC ";4%d"  C256 ";48;5;%d"  RGB ";48;2;%d;%d;%d" */
    /* TODO: "m"                                          */
    (void)dst; (void)cap; (void)st;
    return -1;
}

/* Two colors equal? Two styles equal? (field-by-field; only the
 * fields the mode uses count) */
int color_eq(const struct color *a, const struct color *b) {
    /* TODO */
    (void)a; (void)b;
    return 0;
}

int style_eq(const struct style *a, const struct style *b) {
    /* TODO */
    (void)a; (void)b;
    return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

enum color_mode { COLOR_DEFAULT, COLOR_BASIC, COLOR_256, COLOR_RGB };

struct color {
    enum color_mode mode;
    unsigned char idx;
    unsigned char r, g, b;
};

struct style {
    struct color fg, bg;
    unsigned bold : 1, underline : 1, reverse : 1;
};

int sgr_reset(char *dst, size_t cap);
int sgr_encode(char *dst, size_t cap, const struct style *st);
int color_eq(const struct color *a, const struct color *b);
int style_eq(const struct style *a, const struct style *b);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void expect(int got_len, const char *buf, const char *want,
                   const char *name) {
    int want_len = (int)strlen(want);
    check(got_len == want_len && memcmp(buf, want, (size_t)want_len) == 0,
          name);
}

int main(void) {
    char buf[128];
    struct style st;

    expect(sgr_reset(buf, sizeof(buf)), buf, "\x1b[0m", "test_reset");

    /* everything default */
    memset(&st, 0, sizeof(st));
    expect(sgr_encode(buf, sizeof(buf), &st), buf, "\x1b[0m",
           "test_plain_style");

    /* bold red on default */
    memset(&st, 0, sizeof(st));
    st.bold = 1;
    st.fg.mode = COLOR_BASIC;
    st.fg.idx = 1;
    expect(sgr_encode(buf, sizeof(buf), &st), buf, "\x1b[0;1;31m",
           "test_bold_red");

    /* reverse video only (the status bar) */
    memset(&st, 0, sizeof(st));
    st.reverse = 1;
    expect(sgr_encode(buf, sizeof(buf), &st), buf, "\x1b[0;7m",
           "test_reverse");

    /* underline + basic yellow on basic blue */
    memset(&st, 0, sizeof(st));
    st.underline = 1;
    st.fg.mode = COLOR_BASIC; st.fg.idx = 3;
    st.bg.mode = COLOR_BASIC; st.bg.idx = 4;
    expect(sgr_encode(buf, sizeof(buf), &st), buf, "\x1b[0;4;33;44m",
           "test_underline_yellow_on_blue");

    /* 256-color foreground */
    memset(&st, 0, sizeof(st));
    st.fg.mode = COLOR_256; st.fg.idx = 208;
    expect(sgr_encode(buf, sizeof(buf), &st), buf, "\x1b[0;38;5;208m",
           "test_256_fg");

    /* truecolor both, bold */
    memset(&st, 0, sizeof(st));
    st.bold = 1;
    st.fg.mode = COLOR_RGB; st.fg.r = 250; st.fg.g = 128; st.fg.b = 10;
    st.bg.mode = COLOR_RGB; st.bg.r = 10;  st.bg.g = 20;  st.bg.b = 30;
    expect(sgr_encode(buf, sizeof(buf), &st), buf,
           "\x1b[0;1;38;2;250;128;10;48;2;10;20;30m", "test_rgb_both");

    /* equality */
    struct style a, b;
    memset(&a, 0, sizeof(a));
    memset(&b, 0, sizeof(b));
    check(style_eq(&a, &b) == 1, "test_eq_plain");
    a.bold = 1;
    check(style_eq(&a, &b) == 0, "test_neq_bold");
    b.bold = 1;
    a.fg.mode = COLOR_256; a.fg.idx = 100;
    b.fg.mode = COLOR_256; b.fg.idx = 100;
    check(style_eq(&a, &b) == 1, "test_eq_256");
    b.fg.idx = 101;
    check(style_eq(&a, &b) == 0, "test_neq_256_idx");

    /* overflow reporting */
    char tiny[6];
    memset(&st, 0, sizeof(st));
    st.fg.mode = COLOR_RGB; st.fg.r = 255; st.fg.g = 255; st.fg.b = 255;
    check(sgr_encode(tiny, sizeof(tiny), &st) == -1,
          "test_encode_reports_overflow");

    return failed;
}
```
# Lesson: Parsing the Protocol — a VT State Machine {#csi-parser}

So far you've been *speaking* the protocol. A terminal emulator's
defining job is the opposite: **listening**. Everything the programs
on the pty slave print — shells, compilers, vim itself — arrives at
your master fd as one undifferentiated byte stream, escape sequences
mixed into text, and you must decide, byte by byte: is this a
character to draw, or part of a command?

## Why this must be a state machine

The tempting approach — "when I see ESC, read ahead until the
sequence looks complete" — collapses on contact with reality, for one
deep reason: **the stream has no message boundaries.** `read()` hands
you whatever chunk happened to be in the kernel buffer. A single
`ESC[38;5;208m` may arrive as `ESC[3` in one read and `8;5;208m` in
the next; or a thousand sequences may arrive glued together in one
64 KiB read. Your parser can never assume "the rest is here" and can
never afford to block waiting for it — there may be a screenful of
printable text after the split point.

The clean solution is a **pushdown-free state machine**: an object
that eats exactly one byte at a time, remembers where it is between
bytes, and emits an *event* whenever a unit completes:

```c
struct vt_parser p;
vt_parser_init(&p);

struct vt_event ev;
for (each byte b of whatever read() returned)
    if (vt_feed(&p, b, &ev))
        handle(&ev);       /* print a char, run a command, ... */
```

Because all context lives in the struct, chunk boundaries simply
don't matter: feed it `ESC [ 3` and it sits in the CSI state holding
a half-built parameter; feed `8 ; 5 ...` tomorrow and it carries on.
This is the same architecture as every production emulator; xterm,
st, and Alacritty differ in table encoding, not in shape. (The
canonical description, reverse-engineered from real DEC hardware, is
Paul Flo Williams' state diagram at vt100.net — linked in extended
reading. Ours is a faithful subset.)

## The states

For the VT100 core plus OSC-skipping, five states suffice:

```
GROUND ──ESC──► ESC_SEEN ──'['──► CSI ──final byte──► GROUND (emit CSI event)
   │                │
   │                ├─']'──► OSC ──BEL or ESC \──► GROUND (no event)
   │                │
   │                ├─'(' or ')'──► CHARSET ──any byte──► GROUND (ignore)
   │                │
   │                └─other──► GROUND (emit simple-ESC event)
   │
   ├── printable byte ──► GROUND (emit PRINT event)
   └── C0 control    ──► GROUND (emit CTRL event)
```

State by state:

- **GROUND** — the resting state. Bytes ≥ 0x20 (and, for now, bytes
  ≥ 0x80 — that's UTF-8, next lesson's problem) are **PRINT** events.
  Bytes below 0x20 are C0 controls: ESC (0x1B) changes state; the
  rest (`\n`, `\r`, `\b`, `\t`, BEL…) are **CTRL** events for the
  screen layer to interpret.
- **ESC_SEEN** — one byte of lookahead decides everything: `[` opens
  a control sequence, `]` opens an OSC string, `(` / `)` are the old
  charset-designation sequences (consume one more byte, ignore — you
  still see `ESC ( B` in the wild), anything else is a complete
  two-byte command (`ESC 7`, `ESC 8`, `ESC c`) — emit it.
- **CSI** — the parameter grinder, detailed below.
- **OSC** — swallow bytes (a window title, say) until the terminator:
  BEL, or ESC `\` (which costs a tiny sub-state: an ESC inside OSC
  might be the start of the terminator). Emit nothing. Skipping
  *correctly* matters: mishandle one OSC and every byte after it is
  interpreted in the wrong state — the classic "my terminal went
  insane" failure.
- **CHARSET** — consume exactly one byte, return to GROUND.

## Grinding CSI parameters

Inside CSI, each byte is one of four things:

- **`0`–`9`** — a digit of the current parameter:
  `cur = cur * 10 + (b - '0')`.
- **`;`** — parameter separator: append `cur` to the list, reset the
  accumulator. An empty slot (`ESC[;5H`) appends **0** — by
  convention 0 and "absent" both mean "use the default", so storing
  0 loses nothing.
- **`?`** (and its neighbors `<` `=` `>`, bytes 0x3C–0x3F) — mark the
  sequence *private*. Real parsers keep which byte; a flag is enough
  for us. Intermediates 0x20–0x2F may be silently ignored.
- **final byte 0x40–0x7E** — flush the pending parameter (if the
  slot was started — digits seen or a `;` consumed earlier), emit
  one **CSI event** carrying the final byte, the parameter list, and
  the private flag, and return to GROUND.

Two disciplines keep this robust against hostile streams (remember:
`cat /dev/urandom` is a legal input!):

- **Bound the parameter list.** ECMA-48 says implementations may
  limit parameters; 16 slots is generous. Extra parameters are
  *parsed but dropped* — never written past the array.
- **Bound the values.** `ESC[99999999999999H` must not overflow
  `int`. Clamp each parameter at some sane ceiling (we use 65535)
  while accumulating.

Note what the parser does **not** do: it never interprets. It doesn't
know that `H` moves cursors or that `2J` clears screens — it only
knows the *grammar*. Meaning is the screen layer's job, two lessons
away. This separation is what makes both halves testable in
isolation.

## Challenge: A VT Parser {#vt-parser points=35}

Build it. The event and parser types are fixed by the starter; the
behavior contract:

- `vt_parser_init(&p)` — start in GROUND with an empty accumulator.
- `vt_feed(&p, byte, &ev)` — consume one byte. Return **1** if `ev`
  was filled with a completed event, **0** if the byte was absorbed.

Events:

| type | fields used | emitted for |
|-----------|------------------|-------------|
| `VT_PRINT`| `ch` | printable bytes ≥ 0x20 except 0x7F, and any byte ≥ 0x80, in GROUND |
| `VT_CTRL` | `ch` | C0 bytes in GROUND other than ESC (0x7F: absorb silently) |
| `VT_CSI` | `final`, `params[]`, `nparams`, `priv` | completed control sequences |
| `VT_ESC` | `final` | two-byte ESC commands (`ESC 7` → final `'7'`) |

Parameter rules exactly as in the lesson: empty slots become 0; a
sequence with *no* parameter bytes at all (`ESC[H`) has `nparams == 0`;
at most 16 parameters, extras dropped; values clamped to 65535; bytes
0x3C–0x3F set `priv`; intermediates 0x20–0x2F ignored; OSC and
charset sequences absorbed silently.

The tests feed sequences whole, split at every possible boundary, and
interleaved with text — plus a light fuzz: 4 KiB of pseudo-random
bytes must neither crash you nor leave you wedged (after feeding a
BEL-terminated OSC and a fresh `ESC[m`, the parser must be back in
business).

### Starter

```c
#include <string.h>

#define VT_MAX_PARAMS 16
#define VT_PARAM_MAX  65535

enum vt_event_type { VT_PRINT, VT_CTRL, VT_CSI, VT_ESC };

struct vt_event {
    enum vt_event_type type;
    unsigned char ch;               /* VT_PRINT, VT_CTRL          */
    unsigned char final;            /* VT_CSI, VT_ESC             */
    int params[VT_MAX_PARAMS];      /* VT_CSI                     */
    int nparams;                    /* VT_CSI                     */
    int priv;                       /* VT_CSI: saw ? < = or >     */
};

enum vt_state {
    VT_GROUND,
    VT_ESC_SEEN,
    VT_CSI_PARAM,
    VT_OSC_STRING,
    VT_OSC_ESC,      /* saw ESC inside an OSC: is a '\' next?      */
    VT_CHARSET,      /* after ESC ( or ESC ): eat one byte         */
};

struct vt_parser {
    enum vt_state state;
    int params[VT_MAX_PARAMS];
    int nparams;
    int cur;          /* parameter accumulator                     */
    int slot_started; /* digits seen or ';' consumed in this seq?  */
    int priv;
};

void vt_parser_init(struct vt_parser *p) {
    memset(p, 0, sizeof(*p));
    p->state = VT_GROUND;
}

/* Append the accumulator to the parameter list (bounded). */
static void push_param(struct vt_parser *p) {
    if (p->nparams < VT_MAX_PARAMS)
        p->params[p->nparams++] = p->cur;
    p->cur = 0;
}

/* Feed one byte. Returns 1 if *ev was filled, 0 otherwise. */
int vt_feed(struct vt_parser *p, unsigned char b, struct vt_event *ev) {
    switch (p->state) {
    case VT_GROUND:
        /* TODO: ESC -> VT_ESC_SEEN (reset nparams/cur/slot_started/priv) */
        /* TODO: byte >= 0x20 && != 0x7f, or >= 0x80 -> emit VT_PRINT     */
        /* TODO: other C0 bytes -> emit VT_CTRL; absorb 0x7f              */
        break;

    case VT_ESC_SEEN:
        /* TODO: '[' -> VT_CSI_PARAM;  ']' -> VT_OSC_STRING               */
        /* TODO: '(' or ')' -> VT_CHARSET                                 */
        /* TODO: anything else -> emit VT_ESC{final=b}, back to GROUND    */
        break;

    case VT_CSI_PARAM:
        /* TODO: '0'..'9' -> accumulate (clamp at VT_PARAM_MAX),          */
        /*       slot_started = 1                                         */
        /* TODO: ';' -> push_param, slot_started = 1                      */
        /* TODO: 0x3c..0x3f -> priv = 1                                   */
        /* TODO: 0x20..0x2f -> ignore (intermediates)                     */
        /* TODO: 0x40..0x7e -> flush pending param if slot_started,       */
        /*       emit VT_CSI (copy params, nparams, priv), -> GROUND      */
        /* TODO: anything else -> abort sequence, -> GROUND               */
        break;

    case VT_OSC_STRING:
        /* TODO: BEL (0x07) -> GROUND; ESC -> VT_OSC_ESC; else absorb     */
        break;

    case VT_OSC_ESC:
        /* TODO: '\\' -> GROUND (that was the ST terminator);             */
        /*       else -> back to VT_OSC_STRING                            */
        break;

    case VT_CHARSET:
        /* TODO: absorb one byte, -> GROUND                               */
        break;
    }
    (void)b; (void)ev; (void)push_param;
    return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

#define VT_MAX_PARAMS 16

enum vt_event_type { VT_PRINT, VT_CTRL, VT_CSI, VT_ESC };

struct vt_event {
    enum vt_event_type type;
    unsigned char ch;
    unsigned char final;
    int params[VT_MAX_PARAMS];
    int nparams;
    int priv;
};

/* Must match the starter's definition exactly (separate translation
 * units each carry their own copy of the type). */
enum vt_state {
    VT_GROUND, VT_ESC_SEEN, VT_CSI_PARAM,
    VT_OSC_STRING, VT_OSC_ESC, VT_CHARSET,
};

struct vt_parser {
    enum vt_state state;
    int params[VT_MAX_PARAMS];
    int nparams;
    int cur;
    int slot_started;
    int priv;
};

void vt_parser_init(struct vt_parser *p);
int vt_feed(struct vt_parser *p, unsigned char b, struct vt_event *ev);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

/* Feed a byte string; collect up to max events. Returns event count. */
static int feed_len(struct vt_parser *p, const char *s, size_t len,
                    struct vt_event *evs, int max) {
    int n = 0;
    for (size_t i = 0; i < len; i++) {
        struct vt_event ev;
        if (vt_feed(p, (unsigned char)s[i], &ev) && n < max)
            evs[n++] = ev;
    }
    return n;
}

/* None of our test strings contain NUL, so strlen is safe. */
static int feed_str(struct vt_parser *p, const char *s,
                    struct vt_event *evs, int max) {
    return feed_len(p, s, strlen(s), evs, max);
}

int main(void) {
    struct vt_parser storage;
    struct vt_parser *p = &storage;
    struct vt_event evs[64];
    int n;

    /* plain text -> PRINT events */
    vt_parser_init(p);
    n = feed_str(p, "hi", evs, 64);
    check(n == 2 && evs[0].type == VT_PRINT && evs[0].ch == 'h' &&
          evs[1].type == VT_PRINT && evs[1].ch == 'i',
          "test_plain_text");

    /* C0 controls -> CTRL events */
    vt_parser_init(p);
    n = feed_str(p, "\r\n\t", evs, 64);
    check(n == 3 && evs[0].type == VT_CTRL && evs[0].ch == '\r' &&
          evs[1].type == VT_CTRL && evs[1].ch == '\n' &&
          evs[2].type == VT_CTRL && evs[2].ch == '\t',
          "test_c0_controls");

    /* ESC[2J */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[2J", evs, 64);
    check(n == 1 && evs[0].type == VT_CSI && evs[0].final == 'J' &&
          evs[0].nparams == 1 && evs[0].params[0] == 2 && !evs[0].priv,
          "test_csi_2J");

    /* ESC[H — no parameters at all */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[H", evs, 64);
    check(n == 1 && evs[0].type == VT_CSI && evs[0].final == 'H' &&
          evs[0].nparams == 0,
          "test_csi_no_params");

    /* ESC[12;40H — two parameters */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[12;40H", evs, 64);
    check(n == 1 && evs[0].final == 'H' && evs[0].nparams == 2 &&
          evs[0].params[0] == 12 && evs[0].params[1] == 40,
          "test_csi_two_params");

    /* ESC[;5H — empty first slot becomes 0 */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[;5H", evs, 64);
    check(n == 1 && evs[0].nparams == 2 &&
          evs[0].params[0] == 0 && evs[0].params[1] == 5,
          "test_csi_empty_slot");

    /* ESC[?25l — private mode */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[?25l", evs, 64);
    check(n == 1 && evs[0].final == 'l' && evs[0].priv == 1 &&
          evs[0].nparams == 1 && evs[0].params[0] == 25,
          "test_csi_private");

    /* SGR with a long parameter list */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[1;31;48;5;22m", evs, 64);
    check(n == 1 && evs[0].final == 'm' && evs[0].nparams == 5 &&
          evs[0].params[0] == 1 && evs[0].params[1] == 31 &&
          evs[0].params[2] == 48 && evs[0].params[3] == 5 &&
          evs[0].params[4] == 22,
          "test_sgr_param_list");

    /* the same sequence split at EVERY boundary must parse the same */
    {
        const char *seq = "\x1b[38;5;208m";
        size_t len = strlen(seq);
        int all_ok = 1;
        for (size_t cut = 1; cut < len; cut++) {
            vt_parser_init(p);
            struct vt_event got[8];
            int k = feed_len(p, seq, cut, got, 8);
            k += feed_len(p, seq + cut, len - cut, got + k, 8 - k);
            if (!(k == 1 && got[0].type == VT_CSI && got[0].final == 'm' &&
                  got[0].nparams == 3 && got[0].params[0] == 38 &&
                  got[0].params[1] == 5 && got[0].params[2] == 208))
                all_ok = 0;
        }
        check(all_ok, "test_split_at_every_boundary");
    }

    /* text around a sequence */
    vt_parser_init(p);
    n = feed_str(p, "a\x1b[3Cb", evs, 64);
    check(n == 3 && evs[0].type == VT_PRINT && evs[0].ch == 'a' &&
          evs[1].type == VT_CSI && evs[1].final == 'C' &&
          evs[1].params[0] == 3 &&
          evs[2].type == VT_PRINT && evs[2].ch == 'b',
          "test_text_around_csi");

    /* simple ESC commands */
    vt_parser_init(p);
    n = feed_str(p, "\x1b""7x\x1b""8", evs, 64);
    check(n == 3 && evs[0].type == VT_ESC && evs[0].final == '7' &&
          evs[1].type == VT_PRINT && evs[1].ch == 'x' &&
          evs[2].type == VT_ESC && evs[2].final == '8',
          "test_simple_esc");

    /* charset designation is absorbed */
    vt_parser_init(p);
    n = feed_str(p, "\x1b(Bok", evs, 64);
    check(n == 2 && evs[0].type == VT_PRINT && evs[0].ch == 'o' &&
          evs[1].type == VT_PRINT && evs[1].ch == 'k',
          "test_charset_absorbed");

    /* OSC terminated by BEL is skipped silently */
    vt_parser_init(p);
    n = feed_str(p, "\x1b]0;window title\aA", evs, 64);
    check(n == 1 && evs[0].type == VT_PRINT && evs[0].ch == 'A',
          "test_osc_bel_skipped");

    /* OSC terminated by ESC \ (ST) is skipped silently */
    vt_parser_init(p);
    n = feed_str(p, "\x1b]2;t\x1b\\B", evs, 64);
    check(n == 1 && evs[0].type == VT_PRINT && evs[0].ch == 'B',
          "test_osc_st_skipped");

    /* parameter count is bounded: 20 params, only 16 kept */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[1;2;3;4;5;6;7;8;9;10;11;12;13;14;15;16;17;18;19;20m",
                 evs, 64);
    check(n == 1 && evs[0].nparams == VT_MAX_PARAMS &&
          evs[0].params[15] == 16,
          "test_param_count_bounded");

    /* parameter values are clamped, not overflowed */
    vt_parser_init(p);
    n = feed_str(p, "\x1b[99999999999999999H", evs, 64);
    check(n == 1 && evs[0].params[0] == 65535,
          "test_param_value_clamped");

    /* fuzz: garbage must not wedge the parser */
    {
        vt_parser_init(p);
        unsigned x = 12345;
        for (int i = 0; i < 4096; i++) {
            x = x * 1103515245 + 12345;
            struct vt_event ev;
            vt_feed(p, (unsigned char)(x >> 16), &ev);
        }
        /* force back to a known state: end any OSC, complete a CSI */
        struct vt_event ev;
        vt_feed(p, 0x07, &ev);      /* BEL: terminates OSC if in one */
        feed_str(p, "\x1b[m", evs, 64);
        vt_parser_init(p);
        n = feed_str(p, "\x1b[5;6H", evs, 64);
        check(n == 1 && evs[0].final == 'H' && evs[0].params[0] == 5 &&
              evs[0].params[1] == 6,
              "test_survives_fuzz");
    }

    return failed;
}
```
# Lesson: Decoding the Keyboard {#parse-input}

The output direction (previous lesson) had a tidy standard. The input
direction is messier, because what your program reads in raw mode is
whatever bytes *the terminal emulator decided to send* for each key —
a dialect frozen by history, with real variation between terminals.
Time to learn it cold, because your editor's first job every loop
iteration is turning these bytes back into intent.

## One byte: printable keys and the Ctrl story

Letters, digits, punctuation, space: one byte each, exactly the ASCII
you expect (multi-byte UTF-8 for non-ASCII — next lesson).

**Ctrl+letter** is where the teletype heritage shows. Terminals send
Ctrl+A through Ctrl+Z as bytes 0x01–0x1A: the letter's ASCII value
**with bits 6 and 5 stripped** — `'A' & 0x1F == 0x01`. The Ctrl key
was literally a "zero the high bits" key on teletypes. This mapping
has consequences you must design around:

- **Ctrl+M is 0x0D — the same byte as Enter.** You cannot tell them
  apart. (`'M' & 0x1F == 13 == '\r'`.)
- **Ctrl+I is 0x09 — Tab.** Same collision.
- **Ctrl+[ is 0x1B — Escape itself.** That's why vim users remap Caps
  Lock: Ctrl+[ *is* ESC at the byte level.
- In raw mode (ICRNL off), **Enter arrives as `\r` (0x0D)**, not
  `\n`. Programs that forget this wait forever for a newline that
  never comes.

And the strangest resident of the one-byte world: **Backspace sends
0x7F (DEL)**, not 0x08 (BS), on essentially every modern terminal.
Byte 0x08 is what **Ctrl+H** sends. The reasons are pure archaeology
(DEL was the "rub out a punch-tape mistake" character; the VT100
shipped its Backspace key sending DEL), and the upshot is a rule:
treat **both** 0x7F and 0x08 as Backspace and nobody gets hurt.

## Many bytes: the escape sequences

Keys that had no ASCII seat at the table send short escape sequences
— the same CSI grammar you've been writing, now arriving as input:

```
Up      ESC [ A          Home    ESC [ H   or  ESC [ 1 ~   or  ESC O H
Down    ESC [ B          End     ESC [ F   or  ESC [ 4 ~   or  ESC O F
Right   ESC [ C          Delete  ESC [ 3 ~
Left    ESC [ D          PgUp    ESC [ 5 ~
                         PgDn    ESC [ 6 ~
```

Notice Home and End each have *three* spellings — CSI-letter,
CSI-number-tilde, and `ESC O` letter (the VT100's "application mode",
called SS3). Which one you receive depends on the terminal and its
mode. A robust decoder simply accepts all of them; that's not
sloppiness, that's the actual job.

Modifier keys extend the grammar with a parameter: the encoding is
`1 + bitmask` where Shift=1, Alt=2, Ctrl=4. So Ctrl+Right arrives as
`ESC [ 1 ; 5 C` (5 = 1 + Ctrl's 4) and Shift+Alt+Up as
`ESC [ 1 ; 4 A` (4 = 1 + 1 + 2). Same shape for tilde keys:
Ctrl+Delete is `ESC [ 3 ; 5 ~`.

## The ESC ambiguity — the one genuinely hard part

The user presses the **Escape key**: you read byte 0x1B, alone.
The user presses **Up**: you read 0x1B, then `[`, then `A` — but
possibly split across `read()` calls, with 0x1B arriving alone in
the first one!

At the moment 0x1B lands in your buffer, "Escape was pressed" and
"an arrow key's first byte arrived" are **indistinguishable**. No
amount of cleverness fixes this; the information simply isn't there
yet. Every terminal program resolves it the same way: **wait a few
milliseconds**. If more bytes follow immediately, it was a sequence
(machines are fast); if silence follows, it was the Escape key
(humans are slow). That's what vim's `ttimeoutlen` option tunes, and
it's why Escape feels ever-so-slightly laggy in some tools.

Related: terminals encode **Alt+x** by prefixing the key with ESC —
Alt+f sends `ESC f`. Your decoder gets that for free once it treats
"ESC followed by a non-sequence byte" as Alt+byte.

This shapes the decoder's *interface*. Rather than reading the fd
itself (unmockable, untestable), the decoder is a pure function over
a byte buffer:

```c
size_t key_decode(const unsigned char *buf, size_t len,
                  struct key_event *out);
```

Return how many bytes you consumed; return **0** to mean "I can't
decide yet — bring more bytes (or a timeout)". The event loop owns
the fd, the timing, and the buffer; the decoder owns the grammar.
The tests can then feed any byte pattern, including pathological
splits, without a terminal in sight. (Two modern extensions worth
knowing exist — the kitty keyboard protocol, which fixes the
ambiguity properly, and bracketed paste, which brackets pasted text
in `ESC[200~`/`ESC[201~` so a pasted `:wq!` can't execute — both are
opt-in CSI modes and out of our scope.)

## Challenge: Decode Key Events {#parse-keys points=25}

Implement `key_decode` with this contract:

- Returns the number of bytes consumed for one complete key event
  stored in `*out`, or **0** if `buf` holds an incomplete sequence
  (starts with ESC but needs more bytes to classify).
- `len == 0` → return 0.
- **One-byte keys:** printable bytes (0x20–0x7E) and bytes ≥ 0x80 →
  `KEY_CHAR` with `value` = the byte. `\r` → `KEY_ENTER`. `\t` →
  `KEY_CHAR` value `'\t'`. 0x7F **and** 0x08 → `KEY_BACKSPACE`.
  Remaining bytes 0x01–0x1A → `KEY_CTRL` with `value` = the letter
  (`'A'` for 0x01 … `'Z'` for 0x1A; so 0x03 → `'C'`). Byte 0x00 →
  `KEY_UNKNOWN`, consume 1.
- **ESC sequences** (`buf[0] == 0x1B`):
  - `len == 1` → return 0 (can't decide — the caller's timeout will
    decide it was the Escape key and synthesize `KEY_ESCAPE`).
  - `ESC [ A/B/C/D` → arrow keys. `ESC [ H` / `ESC [ F` → Home/End.
  - `ESC [ <digits> ~` → 1/7=Home, 4/8=End, 3=Delete, 5=PageUp,
    6=PageDown; other numbers → `KEY_UNKNOWN` (consume the whole
    sequence!).
  - Modifier form `ESC [ 1 ; <m> <letter>` and
    `ESC [ <digits> ; <m> ~`: decode the key as above and set
    `mods` = `m - 1` (bit 0 Shift, bit 1 Alt, bit 2 Ctrl).
  - Incomplete CSI (e.g. `ESC [` alone, `ESC [ 5` with no final
    byte yet) → return 0.
  - `ESC O H/F/P/Q/R/S` → Home, End, F1–F4 (map F1–F4 to
    `KEY_UNKNOWN` — we don't use them — but consume 3 bytes).
  - `ESC <other byte>` → `KEY_ALT` with `value` = that byte,
    consume 2.
- Unrecognized-but-complete CSI sequences must be consumed in full
  and reported as `KEY_UNKNOWN` — a decoder that consumes the wrong
  number of bytes poisons every key after it.

### Starter

```c
#include <stddef.h>

enum key_type {
    KEY_CHAR,        /* value = the byte                       */
    KEY_CTRL,        /* value = 'A'..'Z'                       */
    KEY_ALT,         /* value = the byte after ESC             */
    KEY_ENTER,
    KEY_BACKSPACE,
    KEY_ESCAPE,      /* synthesized by the CALLER on timeout   */
    KEY_ARROW_UP,
    KEY_ARROW_DOWN,
    KEY_ARROW_LEFT,
    KEY_ARROW_RIGHT,
    KEY_HOME,
    KEY_END,
    KEY_PAGE_UP,
    KEY_PAGE_DOWN,
    KEY_DELETE,
    KEY_UNKNOWN,
};

#define KEY_MOD_SHIFT 1
#define KEY_MOD_ALT   2
#define KEY_MOD_CTRL  4

struct key_event {
    enum key_type type;
    int value;       /* KEY_CHAR / KEY_CTRL / KEY_ALT           */
    int mods;        /* bitmask of KEY_MOD_*                    */
};

/* Decode one key event from buf[0..len).
 * Returns bytes consumed (>= 1) with *out filled,
 * or 0 if the buffer starts an incomplete escape sequence
 * (or len == 0). */
size_t key_decode(const unsigned char *buf, size_t len,
                  struct key_event *out) {
    /* TODO: len == 0 -> 0                                          */
    /* TODO: single-byte cases (see contract; handle before ESC?    */
    /*       no — 0x1b IS the escape prefix, handle it as such)     */
    /* TODO: ESC alone -> 0                                         */
    /* TODO: ESC [ ... : scan digits/semicolons, find the final     */
    /*       byte; if none in the buffer yet -> 0                   */
    /* TODO: ESC O X : SS3 forms                                    */
    /* TODO: ESC other -> KEY_ALT                                   */
    (void)buf; (void)len; (void)out;
    return 0;
}

#ifdef DEMO
/* cc -std=c17 -Wall -DDEMO solution.c -o demo && ./demo
 * (needs a real terminal; press q to quit)                          */
#define _XOPEN_SOURCE 600
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <termios.h>
#include <stdlib.h>
static struct termios saved;
static void restore(void) { tcsetattr(0, TCSAFLUSH, &saved); }
int main(void) {
    tcgetattr(0, &saved);
    atexit(restore);
    struct termios raw = saved;
    raw.c_lflag &= ~(unsigned)(ICANON | ECHO);
    tcsetattr(0, TCSAFLUSH, &raw);

    static const char *names[] = {
        "CHAR","CTRL","ALT","ENTER","BACKSPACE","ESCAPE","UP","DOWN",
        "LEFT","RIGHT","HOME","END","PGUP","PGDN","DELETE","UNKNOWN",
    };
    unsigned char buf[32];
    size_t have = 0;
    for (;;) {
        ssize_t r = read(0, buf + have, sizeof(buf) - have);
        if (r <= 0) break;
        have += (size_t)r;
        size_t off = 0;
        for (;;) {
            struct key_event ev;
            size_t used = key_decode(buf + off, have - off, &ev);
            if (used == 0) break;
            printf("%s value=%d mods=%d\r\n", names[ev.type], ev.value, ev.mods);
            if (ev.type == KEY_CHAR && ev.value == 'q') return 0;
            off += used;
        }
        memmove(buf, buf + off, have - off);
        have -= off;
    }
    return 0;
}
#endif
```

### Tests

```c
#include <stdio.h>
#include <string.h>

enum key_type {
    KEY_CHAR, KEY_CTRL, KEY_ALT, KEY_ENTER, KEY_BACKSPACE, KEY_ESCAPE,
    KEY_ARROW_UP, KEY_ARROW_DOWN, KEY_ARROW_LEFT, KEY_ARROW_RIGHT,
    KEY_HOME, KEY_END, KEY_PAGE_UP, KEY_PAGE_DOWN, KEY_DELETE,
    KEY_UNKNOWN,
};

#define KEY_MOD_SHIFT 1
#define KEY_MOD_ALT   2
#define KEY_MOD_CTRL  4

struct key_event {
    enum key_type type;
    int value;
    int mods;
};

size_t key_decode(const unsigned char *buf, size_t len,
                  struct key_event *out);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

/* decode a C string, expect one event using all bytes */
static struct key_event decode1(const char *s, size_t *used) {
    struct key_event ev = { KEY_UNKNOWN, -999, -999 };
    *used = key_decode((const unsigned char *)s, strlen(s), &ev);
    return ev;
}

int main(void) {
    size_t used;
    struct key_event ev;

    /* printable characters */
    ev = decode1("a", &used);
    check(used == 1 && ev.type == KEY_CHAR && ev.value == 'a',
          "test_plain_char");
    ev = decode1(" ", &used);
    check(used == 1 && ev.type == KEY_CHAR && ev.value == ' ',
          "test_space");

    /* enter is \r in raw mode */
    ev = decode1("\r", &used);
    check(used == 1 && ev.type == KEY_ENTER, "test_enter_cr");

    /* tab stays a character */
    ev = decode1("\t", &used);
    check(used == 1 && ev.type == KEY_CHAR && ev.value == '\t',
          "test_tab_is_char");

    /* both backspace bytes */
    ev = decode1("\x7f", &used);
    check(used == 1 && ev.type == KEY_BACKSPACE, "test_backspace_del");
    ev = decode1("\x08", &used);
    check(used == 1 && ev.type == KEY_BACKSPACE, "test_backspace_bs");

    /* ctrl keys map to letters */
    ev = decode1("\x03", &used);
    check(used == 1 && ev.type == KEY_CTRL && ev.value == 'C',
          "test_ctrl_c");
    ev = decode1("\x13", &used);
    check(used == 1 && ev.type == KEY_CTRL && ev.value == 'S',
          "test_ctrl_s");
    ev = decode1("\x11", &used);
    check(used == 1 && ev.type == KEY_CTRL && ev.value == 'Q',
          "test_ctrl_q");

    /* arrows */
    ev = decode1("\x1b[A", &used);
    check(used == 3 && ev.type == KEY_ARROW_UP, "test_arrow_up");
    ev = decode1("\x1b[B", &used);
    check(used == 3 && ev.type == KEY_ARROW_DOWN, "test_arrow_down");
    ev = decode1("\x1b[C", &used);
    check(used == 3 && ev.type == KEY_ARROW_RIGHT, "test_arrow_right");
    ev = decode1("\x1b[D", &used);
    check(used == 3 && ev.type == KEY_ARROW_LEFT, "test_arrow_left");

    /* home/end, all three spellings */
    ev = decode1("\x1b[H", &used);
    check(used == 3 && ev.type == KEY_HOME, "test_home_csi_letter");
    ev = decode1("\x1b[F", &used);
    check(used == 3 && ev.type == KEY_END, "test_end_csi_letter");
    ev = decode1("\x1b[1~", &used);
    check(used == 4 && ev.type == KEY_HOME, "test_home_tilde");
    ev = decode1("\x1b[4~", &used);
    check(used == 4 && ev.type == KEY_END, "test_end_tilde");
    ev = decode1("\x1bOH", &used);
    check(used == 3 && ev.type == KEY_HOME, "test_home_ss3");
    ev = decode1("\x1bOF", &used);
    check(used == 3 && ev.type == KEY_END, "test_end_ss3");

    /* delete and paging */
    ev = decode1("\x1b[3~", &used);
    check(used == 4 && ev.type == KEY_DELETE, "test_delete");
    ev = decode1("\x1b[5~", &used);
    check(used == 4 && ev.type == KEY_PAGE_UP, "test_page_up");
    ev = decode1("\x1b[6~", &used);
    check(used == 4 && ev.type == KEY_PAGE_DOWN, "test_page_down");

    /* modifiers */
    ev = decode1("\x1b[1;5C", &used);
    check(used == 6 && ev.type == KEY_ARROW_RIGHT &&
          ev.mods == KEY_MOD_CTRL,
          "test_ctrl_right");
    ev = decode1("\x1b[1;2A", &used);
    check(used == 6 && ev.type == KEY_ARROW_UP &&
          ev.mods == KEY_MOD_SHIFT,
          "test_shift_up");
    ev = decode1("\x1b[3;5~", &used);
    check(used == 6 && ev.type == KEY_DELETE && ev.mods == KEY_MOD_CTRL,
          "test_ctrl_delete");

    /* alt-prefixed keys */
    ev = decode1("\x1b""f", &used);
    check(used == 2 && ev.type == KEY_ALT && ev.value == 'f',
          "test_alt_f");

    /* incomplete sequences ask for more bytes */
    {
        struct key_event e2;
        check(key_decode((const unsigned char *)"\x1b", 1, &e2) == 0,
              "test_lone_esc_incomplete");
        check(key_decode((const unsigned char *)"\x1b[", 2, &e2) == 0,
              "test_esc_bracket_incomplete");
        check(key_decode((const unsigned char *)"\x1b[5", 3, &e2) == 0,
              "test_esc_partial_param_incomplete");
        check(key_decode(NULL, 0, &e2) == 0, "test_empty_buffer");
    }

    /* unknown-but-complete sequences consume fully */
    ev = decode1("\x1b[99~", &used);
    check(used == 5 && ev.type == KEY_UNKNOWN, "test_unknown_tilde_key");

    /* a stream of events decodes in order */
    {
        const unsigned char stream[] = "a\x1b[Cb\x7f";
        size_t off = 0, n = sizeof(stream) - 1;
        struct key_event e2;
        size_t u = key_decode(stream + off, n - off, &e2);
        int ok = (u == 1 && e2.type == KEY_CHAR && e2.value == 'a');
        off += u;
        u = key_decode(stream + off, n - off, &e2);
        ok = ok && (u == 3 && e2.type == KEY_ARROW_RIGHT);
        off += u;
        u = key_decode(stream + off, n - off, &e2);
        ok = ok && (u == 1 && e2.type == KEY_CHAR && e2.value == 'b');
        off += u;
        u = key_decode(stream + off, n - off, &e2);
        ok = ok && (u == 1 && e2.type == KEY_BACKSPACE);
        check(ok, "test_stream_of_events");
    }

    /* high bytes pass through as chars (UTF-8 handled elsewhere) */
    {
        const unsigned char hi[] = { 0xC3 };
        struct key_event e2;
        size_t u = key_decode(hi, 1, &e2);
        check(u == 1 && e2.type == KEY_CHAR && e2.value == 0xC3,
              "test_high_byte_passthrough");
    }

    return failed;
}
```
# Lesson: Text Is Not Bytes — UTF-8 {#utf8}

Everything so far pretended one byte = one character. For 1970s
terminals that was true; for yours it can't be — type `é`, `→`, or
`日` into any modern shell and multiple bytes flow. Modern terminals
are **UTF-8 native**: it's what programs write to them and what
keyboards send from them. A terminal that mishandles it doesn't get
to call itself one.

## The design (worth knowing the story)

In 1992, needing an encoding for Plan 9, Ken Thompson sketched UTF-8
on a New Jersey diner placemat (Rob Pike's telling of it is in the
extended reading, and it's a great read). The constraints: encode all
of Unicode; keep ASCII files valid unchanged; never produce a 0x00
or other ASCII byte inside a multi-byte character (so C strings,
`/`-separated paths, and every existing Unix tool keep working); and
make it possible to find character boundaries from anywhere in a
stream. The result:

| Code point range | Bytes | Pattern |
|------------------|-------|---------|
| U+0000 – U+007F | 1 | `0xxxxxxx` |
| U+0080 – U+07FF | 2 | `110xxxxx 10xxxxxx` |
| U+0800 – U+FFFF | 3 | `1110xxxx 10xxxxxx 10xxxxxx` |
| U+10000 – U+10FFFF | 4 | `11110xxx 10xxxxxx 10xxxxxx 10xxxxxx` |

The payload bits (`x`) carry the code point, big-end first. Read the
patterns as a self-describing header: the number of leading 1-bits
in the first byte *is* the sequence length, and every continuation
byte is unmistakable (`10xxxxxx`, values 0x80–0xBF). Consequences:

- **ASCII is already UTF-8.** Every file you've ever compiled: valid.
- **Self-synchronizing.** Land at a random byte and you can find the
  next character start without backing up: skip continuation bytes.
- **No aliasing with ASCII.** A multi-byte character contains no
  bytes < 0x80 — `strchr(s, '/')` can't match inside one.

Worked example — `é` is U+00E9, binary `000 1110 1001` (11 bits →
needs the 2-byte form's 11 payload slots):

```
110 00011   10 101001
    ^^^^^      ^^^^^^
    00011      101001   →  0xC3 0xA9
```

## Decoding, and the ways bytes go wrong

A decoder collects a leading byte, then the right number of
continuation bytes, and reassembles:

```c
cp = (lead & payload_mask);
for each continuation byte b:  cp = (cp << 6) | (b & 0x3F);
```

But terminals eat *arbitrary* byte streams, so the error cases are
not optional:

- **A stray continuation byte** (0x80–0xBF with no leading byte).
- **A leading byte followed by a non-continuation** — the sequence
  was truncated by whoever produced it.
- **Overlong encodings**: `0xC0 0x80` decodes by-the-rules to U+0000
  — the 2-byte form of a 1-byte value. Forbidden by the spec, and
  historically a real security hole (encode `/` as `0xC0 0xAF` and
  sail past a path check that only looked for byte 0x2F). Reject:
  2-byte forms below U+0080, 3-byte below U+0800, 4-byte below
  U+10000.
- **UTF-16 surrogates** (U+D800–U+DFFF) and values past U+10FFFF:
  not characters; reject.

The universal convention for all of these — what every terminal,
browser, and editor does — is to emit **U+FFFD REPLACEMENT
CHARACTER (�)**, consume a minimal prefix (we'll consume exactly one
byte), and carry on. Never stall, never crash, never let one bad
byte eat good ones after it.

There's one more case that is *not* an error: the buffer simply ends
mid-character because `read()` split it. Same answer as the VT
parser and the key decoder: return "need more bytes" and let the
caller re-present the tail later. (Sensing the pattern? Incremental
interfaces over byte buffers, all the way down. They compose:
GROUND-state print bytes flow from the VT parser into the UTF-8
decoder, whose code points land in screen cells.)

One honest caveat before the challenge: code point ≠ column. `日`
occupies two terminal columns; combining marks occupy zero; that's
the `wcwidth()` problem, and real emulators carry Unicode tables for
it. Our editor sticks to one-column characters, but know the dragon
is there.

## Challenge: A UTF-8 Decoder {#utf8-decode points=20}

Implement:

```c
size_t utf8_decode(const unsigned char *buf, size_t len, uint32_t *cp);
```

- Decode the character starting at `buf[0]`; store the code point in
  `*cp`; return the number of bytes consumed (1–4).
- If the buffer ends mid-character (valid prefix, not enough bytes):
  return **0** — need more input.
- On invalid input (stray continuation, bad follow byte, overlong,
  surrogate, > U+10FFFF, or an invalid leading byte like 0xFE):
  store **0xFFFD** and return **1** — consume the offending byte
  only, so decoding can resynchronize.

Also implement `utf8_encode(cp, out)` — the reverse, needed when the
editor saves multi-byte text — returning the byte length (and
encoding invalid code points as U+FFFD).

### Starter

```c
#include <stdint.h>
#include <stddef.h>

#define UTF8_REPLACEMENT 0xFFFDu

/* Decode one character from buf[0..len).
 * Returns bytes consumed (1-4) with *cp set,
 * 0 if buf holds a valid but incomplete prefix (need more bytes).
 * Invalid bytes: *cp = UTF8_REPLACEMENT, return 1. */
size_t utf8_decode(const unsigned char *buf, size_t len, uint32_t *cp) {
    /* TODO: len == 0 -> 0                                            */
    /* TODO: buf[0] < 0x80 -> ASCII, consume 1                        */
    /* TODO: leading byte -> expected length + payload bits:          */
    /*       0xC0..0xDF -> 2 bytes, payload = b & 0x1F                */
    /*       0xE0..0xEF -> 3 bytes, payload = b & 0x0F                */
    /*       0xF0..0xF4 -> 4 bytes, payload = b & 0x07                */
    /*       anything else (0x80..0xBF, 0xF5..0xFF) -> invalid        */
    /* TODO: if len < expected -> check available continuation bytes  */
    /*       are valid so far; if yes return 0, if no -> invalid      */
    /* TODO: each continuation must match 10xxxxxx; shift in 6 bits   */
    /* TODO: reject overlongs (min cp per length: 0x80, 0x800,        */
    /*       0x10000), surrogates 0xD800-0xDFFF, cp > 0x10FFFF        */
    (void)buf; (void)len; (void)cp;
    return 0;
}

/* Encode cp into out (at least 4 bytes). Returns length 1-4.
 * Invalid cp (surrogate or > 0x10FFFF) encodes U+FFFD. */
size_t utf8_encode(uint32_t cp, unsigned char *out) {
    /* TODO: the table above, run backwards */
    (void)cp; (void)out;
    return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdint.h>

#define UTF8_REPLACEMENT 0xFFFDu

size_t utf8_decode(const unsigned char *buf, size_t len, uint32_t *cp);
size_t utf8_encode(uint32_t cp, unsigned char *out);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main(void) {
    uint32_t cp;
    size_t n;

    /* ASCII */
    n = utf8_decode((const unsigned char *)"A", 1, &cp);
    check(n == 1 && cp == 'A', "test_ascii");

    /* 2-byte: é = U+00E9 = C3 A9 */
    {
        const unsigned char b[] = { 0xC3, 0xA9 };
        n = utf8_decode(b, 2, &cp);
        check(n == 2 && cp == 0xE9, "test_two_byte");
    }

    /* 3-byte: € = U+20AC = E2 82 AC */
    {
        const unsigned char b[] = { 0xE2, 0x82, 0xAC };
        n = utf8_decode(b, 3, &cp);
        check(n == 3 && cp == 0x20AC, "test_three_byte");
    }

    /* 4-byte: 😀 = U+1F600 = F0 9F 98 80 */
    {
        const unsigned char b[] = { 0xF0, 0x9F, 0x98, 0x80 };
        n = utf8_decode(b, 4, &cp);
        check(n == 4 && cp == 0x1F600, "test_four_byte");
    }

    /* incomplete prefixes ask for more */
    {
        const unsigned char b[] = { 0xE2, 0x82 };
        check(utf8_decode(b, 1, &cp) == 0, "test_incomplete_1_of_3");
        check(utf8_decode(b, 2, &cp) == 0, "test_incomplete_2_of_3");
        const unsigned char f[] = { 0xF0, 0x9F, 0x98 };
        check(utf8_decode(f, 3, &cp) == 0, "test_incomplete_3_of_4");
    }

    /* stray continuation byte -> U+FFFD, consume 1 */
    {
        const unsigned char b[] = { 0x80, 'x' };
        n = utf8_decode(b, 2, &cp);
        check(n == 1 && cp == UTF8_REPLACEMENT, "test_stray_continuation");
    }

    /* leading byte followed by garbage -> U+FFFD, consume 1, resync */
    {
        const unsigned char b[] = { 0xC3, 'x' };
        n = utf8_decode(b, 2, &cp);
        check(n == 1 && cp == UTF8_REPLACEMENT, "test_bad_continuation");
        n = utf8_decode(b + 1, 1, &cp);
        check(n == 1 && cp == 'x', "test_resync_after_error");
    }

    /* overlong: C0 80 would decode to U+0000 -> reject */
    {
        const unsigned char b[] = { 0xC0, 0x80 };
        n = utf8_decode(b, 2, &cp);
        check(n == 1 && cp == UTF8_REPLACEMENT, "test_overlong_two_byte");
    }
    {
        /* E0 80 AF would be an overlong U+002F */
        const unsigned char b[] = { 0xE0, 0x80, 0xAF };
        n = utf8_decode(b, 3, &cp);
        check(n == 1 && cp == UTF8_REPLACEMENT, "test_overlong_three_byte");
    }

    /* surrogate half: ED A0 80 = U+D800 -> reject */
    {
        const unsigned char b[] = { 0xED, 0xA0, 0x80 };
        n = utf8_decode(b, 3, &cp);
        check(n == 1 && cp == UTF8_REPLACEMENT, "test_surrogate_rejected");
    }

    /* invalid leading bytes */
    {
        const unsigned char b1[] = { 0xFE };
        n = utf8_decode(b1, 1, &cp);
        check(n == 1 && cp == UTF8_REPLACEMENT, "test_invalid_lead_fe");
        const unsigned char b2[] = { 0xFF };
        n = utf8_decode(b2, 1, &cp);
        check(n == 1 && cp == UTF8_REPLACEMENT, "test_invalid_lead_ff");
    }

    /* a mixed string decodes cleanly end to end */
    {
        /* "aé€😀!" */
        const unsigned char s[] = { 'a', 0xC3, 0xA9, 0xE2, 0x82, 0xAC,
                                    0xF0, 0x9F, 0x98, 0x80, '!' };
        uint32_t want[] = { 'a', 0xE9, 0x20AC, 0x1F600, '!' };
        size_t off = 0;
        int idx = 0, ok = 1;
        while (off < sizeof(s)) {
            n = utf8_decode(s + off, sizeof(s) - off, &cp);
            if (n == 0) { ok = 0; break; }
            if (idx >= 5 || cp != want[idx]) { ok = 0; break; }
            off += n;
            idx++;
        }
        check(ok && idx == 5, "test_mixed_string");
    }

    /* encode: the reverse direction */
    {
        unsigned char out[4];
        check(utf8_encode('A', out) == 1 && out[0] == 'A',
              "test_encode_ascii");
        n = utf8_encode(0xE9, out);
        check(n == 2 && out[0] == 0xC3 && out[1] == 0xA9,
              "test_encode_two_byte");
        n = utf8_encode(0x20AC, out);
        check(n == 3 && out[0] == 0xE2 && out[1] == 0x82 && out[2] == 0xAC,
              "test_encode_three_byte");
        n = utf8_encode(0x1F600, out);
        check(n == 4 && out[0] == 0xF0 && out[1] == 0x9F &&
              out[2] == 0x98 && out[3] == 0x80,
              "test_encode_four_byte");
        /* invalid code points encode the replacement char */
        n = utf8_encode(0xD800, out);
        check(n == 3 && out[0] == 0xEF && out[1] == 0xBF && out[2] == 0xBD,
              "test_encode_surrogate_becomes_fffd");
    }

    /* roundtrip everything interesting */
    {
        uint32_t cps[] = { 0x7F, 0x80, 0x7FF, 0x800, 0xFFFF,
                           0x10000, 0x10FFFF };
        int ok = 1;
        for (size_t i = 0; i < sizeof(cps) / sizeof(cps[0]); i++) {
            unsigned char out[4];
            size_t e = utf8_encode(cps[i], out);
            uint32_t back;
            size_t d = utf8_decode(out, e, &back);
            if (d != e || back != cps[i]) ok = 0;
        }
        check(ok, "test_roundtrip_boundaries");
    }

    return failed;
}
```
# Lesson: The Screen Model {#screen-buffer}

Between "bytes arrive" and "pixels change" every terminal keeps an
in-memory model of the display. So does every full-screen *program*
talking to a terminal — vim doesn't re-send your whole file on each
keystroke; it maintains what the screen *should* look like and sends
the difference. Both directions of this course now converge on the
same data structure: **a grid of cells**.

## Why not just write() as you go?

Because of what you'd be writing *to*. Three separate costs punish
scattered small writes:

1. **Syscalls aren't free.** A `write()` is a user→kernel→user round
   trip — over a thousand times the cost of a memory store. Painting
   a 50×200 screen cell-by-cell is 10,000 syscalls to draw one frame
   you could send in *one*.
2. **Flicker.** The terminal renders whenever it likes — including
   after your "clear screen" but before your "draw contents". The
   user sees a blank flash. Batch the clear and the redraw into one
   write and there is no in-between state to glimpse.
3. **Tearing over distance.** Over ssh, each write can become a
   packet. Half a frame per packet means the user literally watches
   your UI assemble.

So the architecture — the same one in kilo, vim, and ncurses — is:

```
mutate cells in memory  →  serialize to ONE byte buffer  →  ONE write()
```

Nothing touches the fd until the frame is complete.

## The cell

A terminal cell is a glyph plus its styling — the "brush" that was
active when it was painted:

```c
struct cell {
    char ch;              /* the glyph                              */
    unsigned char fg;     /* 0-7 = basic colors, 9 = default        */
    unsigned char bg;     /*                 same                   */
    unsigned char attrs;  /* CELL_BOLD | CELL_UNDERLINE | CELL_REVERSE */
};
```

The grid is `rows * cols` of these. Resist the 2D-array temptation —
`struct cell grid[ROWS][COLS]` hardcodes the size at compile time,
and terminals resize at runtime. One `malloc(rows * cols *
sizeof(struct cell))` and the classic flattening

```c
cell = &s->cells[row * s->cols + col];
```

gives you a grid of any size, reallocatable on SIGWINCH. (That
`row * cols + col` line will appear in your dreams. It should: get
it backwards — `col * rows + row` — and everything *almost* works,
which is worse than not working.)

Alongside the grid, the screen keeps a **cursor** (where the next
glyph lands) and the **current brush** (what style it lands with).
SGR's statefulness, which looked like a wire-protocol quirk two
lessons ago, turns out to be this: the brush is state *in the screen
model*, and the wire just mutates it.

## The append buffer

The serialize step needs a growable byte buffer to accumulate the
frame — cells, cursor moves, SGR changes — before the single write.
C won't hand you one; you'll build it (kilo calls its version
`abuf`, dynamic-array veterans will recognize the pattern):

```c
struct abuf {
    char  *b;    /* heap storage           */
    size_t len;  /* bytes used             */
    size_t cap;  /* bytes allocated        */
};
```

The one interesting decision is the growth policy. Growing to
*exactly* the needed size makes N appends cost O(N²) total (each
realloc copies everything so far). Growing **geometrically** —
doubling — makes the total O(N): each byte is copied at most a
constant number of times, amortized. This single idea is why
`std::vector`, Go slices, and Python lists are fast; today you get
to own it in nine lines.

## Challenge: An Append Buffer {#append-buf points=15}

Implement `abuf`: `ab_init`, `ab_append` (arbitrary bytes),
`ab_append_str` (convenience for C strings), `ab_free`. Rules:

- Doubling growth (start at 64 when empty), so the amortized-O(1)
  property actually holds. Grow *at least* to fit; `cap` must never
  be less than `len`.
- `ab_append` returns 0 on success, -1 if `realloc` fails — and on
  failure the buffer must be *unchanged and still valid* (hint:
  `realloc`'s return value goes in a temporary; assigning it
  directly to `ab->b` leaks the old block and corrupts the struct
  on failure).
- `ab_free` releases storage and resets to a valid empty buffer
  (safe to reuse, safe to double-free).
- Appended bytes may include NULs — track `len`, never `strlen`.

### Starter

```c
#include <stdlib.h>
#include <string.h>

struct abuf {
    char *b;
    size_t len;
    size_t cap;
};

void ab_init(struct abuf *ab) {
    ab->b = NULL;
    ab->len = 0;
    ab->cap = 0;
}

/* Append n bytes. 0 on success, -1 on allocation failure
 * (buffer unchanged). Growth: double cap (min 64) until it fits. */
int ab_append(struct abuf *ab, const char *bytes, size_t n) {
    /* TODO: if len + n > cap: compute new cap, realloc via a temp */
    /* TODO: memcpy at b + len, bump len                           */
    (void)ab; (void)bytes; (void)n;
    return -1;
}

int ab_append_str(struct abuf *ab, const char *s) {
    return ab_append(ab, s, strlen(s));
}

void ab_free(struct abuf *ab) {
    /* TODO: free storage, reset to a valid empty state */
    (void)ab;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

struct abuf {
    char *b;
    size_t len;
    size_t cap;
};

void ab_init(struct abuf *ab);
int ab_append(struct abuf *ab, const char *bytes, size_t n);
int ab_append_str(struct abuf *ab, const char *s);
void ab_free(struct abuf *ab);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main(void) {
    struct abuf ab;

    /* basics */
    ab_init(&ab);
    check(ab.len == 0, "test_init_empty");
    check(ab_append_str(&ab, "hello") == 0, "test_append_returns_ok");
    check(ab.len == 5 && memcmp(ab.b, "hello", 5) == 0, "test_append_str");
    ab_append_str(&ab, ", world");
    check(ab.len == 12 && memcmp(ab.b, "hello, world", 12) == 0,
          "test_append_concatenates");
    check(ab.cap >= ab.len, "test_cap_covers_len");
    ab_free(&ab);
    check(ab.len == 0 && ab.cap == 0 && ab.b == NULL,
          "test_free_resets");
    ab_free(&ab); /* double free must be safe */
    check(1, "test_double_free_safe");

    /* NUL bytes are data, not terminators */
    ab_init(&ab);
    ab_append(&ab, "a\0b", 3);
    ab_append(&ab, "\0", 1);
    check(ab.len == 4 && ab.b[0] == 'a' && ab.b[1] == '\0' &&
          ab.b[2] == 'b' && ab.b[3] == '\0',
          "test_nul_bytes_preserved");
    ab_free(&ab);

    /* many appends: contents intact, growth amortized */
    ab_init(&ab);
    for (int i = 0; i < 10000; i++)
        ab_append_str(&ab, "abc");
    check(ab.len == 30000, "test_many_appends_len");
    int ok = ab.b != NULL && ab.len == 30000;
    for (int i = 0; ok && i < 10000; i++)
        if (memcmp(ab.b + i * 3, "abc", 3) != 0) ok = 0;
    check(ok, "test_many_appends_content");
    /* doubling growth keeps cap within a small factor of len */
    check(ab.cap >= ab.len && ab.cap <= ab.len * 4 + 64,
          "test_growth_geometric");
    ab_free(&ab);

    /* one huge append */
    ab_init(&ab);
    char *big = malloc(1 << 20);
    memset(big, 'z', 1 << 20);
    check(ab_append(&ab, big, 1 << 20) == 0 && ab.len == (1 << 20) &&
          ab.b[0] == 'z' && ab.b[(1 << 20) - 1] == 'z',
          "test_single_big_append");
    free(big);
    ab_free(&ab);

    /* reusable after free */
    ab_init(&ab);
    ab_append_str(&ab, "x");
    ab_free(&ab);
    check(ab_append_str(&ab, "again") == 0 && ab.len == 5 &&
          memcmp(ab.b, "again", 5) == 0,
          "test_reuse_after_free");
    ab_free(&ab);

    return failed;
}
```

## Challenge: The Cell Grid {#screen-struct points=20}

Build the screen: a heap-allocated grid of cells with a cursor and a
brush. Operations:

- `screen_init(s, rows, cols)` — allocate; every cell a space with
  default colors (`fg = 9, bg = 9, attrs = 0`); cursor at (0,0);
  brush = defaults. Return 0, or -1 on allocation failure.
- `screen_free(s)` — release; safe to call twice.
- `screen_cell(s, row, col)` — pointer to a cell (NULL if out of
  bounds — make the bounds check the *one* place it exists).
- `screen_set_brush(s, fg, bg, attrs)` — set the current brush.
- `screen_put(s, ch)` — stamp `ch` **with the current brush** at the
  cursor, then advance: right one; wrap to column 0 of the next row
  at the right edge; on wrapping past the last row, stay on the
  last row (scrolling arrives in a later lesson).
- `screen_set_cursor(s, row, col)` — absolute move, clamped into
  bounds.
- `screen_move_cursor(s, dr, dc)` — relative move, clamped.
- `screen_newline(s)` — cursor to column 0 of the next row (clamped
  at the bottom).
- `screen_backspace(s)` — if the cursor is past column 0: move left
  and blank that cell (space, default style). At column 0: no-op.
- `screen_clear(s)` — every cell back to the init state; cursor and
  brush **unchanged** (that's `ESC[2J` semantics — remember, ED
  doesn't move the cursor).

### Starter

```c
#include <stdlib.h>
#include <string.h>

#define CELL_BOLD      1u
#define CELL_UNDERLINE 2u
#define CELL_REVERSE   4u
#define COLOR_DEFAULT_IDX 9

struct cell {
    char ch;
    unsigned char fg;
    unsigned char bg;
    unsigned char attrs;
};

struct screen {
    int rows, cols;
    struct cell *cells;         /* rows * cols, row-major          */
    int cursor_row, cursor_col; /* 0-based                          */
    unsigned char brush_fg, brush_bg, brush_attrs;
};

/* 0 on success, -1 on allocation failure. */
int screen_init(struct screen *s, int rows, int cols) {
    /* TODO: store dims, malloc rows*cols cells, fill with blanks   */
    /* TODO: cursor (0,0); brush = default/default/0                */
    (void)s; (void)rows; (void)cols;
    return -1;
}

void screen_free(struct screen *s) {
    /* TODO: free cells, NULL the pointer (double-free safe) */
    (void)s;
}

struct cell *screen_cell(struct screen *s, int row, int col) {
    /* TODO: bounds check -> NULL; else &cells[row * cols + col] */
    (void)s; (void)row; (void)col;
    return NULL;
}

void screen_set_brush(struct screen *s, unsigned char fg,
                      unsigned char bg, unsigned char attrs) {
    /* TODO */
    (void)s; (void)fg; (void)bg; (void)attrs;
}

void screen_put(struct screen *s, char ch) {
    /* TODO: stamp ch + brush at cursor              */
    /* TODO: advance; wrap at right edge; clamp at bottom row */
    (void)s; (void)ch;
}

void screen_set_cursor(struct screen *s, int row, int col) {
    /* TODO: clamp into [0, rows) x [0, cols) */
    (void)s; (void)row; (void)col;
}

void screen_move_cursor(struct screen *s, int dr, int dc) {
    /* TODO: relative + clamp */
    (void)s; (void)dr; (void)dc;
}

void screen_newline(struct screen *s) {
    /* TODO: col 0, row+1 clamped */
    (void)s;
}

void screen_backspace(struct screen *s) {
    /* TODO: col > 0: move left, blank that cell; col == 0: nothing */
    (void)s;
}

void screen_clear(struct screen *s) {
    /* TODO: all cells blank+default; cursor and brush unchanged */
    (void)s;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

#define CELL_BOLD      1u
#define CELL_UNDERLINE 2u
#define CELL_REVERSE   4u
#define COLOR_DEFAULT_IDX 9

struct cell {
    char ch;
    unsigned char fg;
    unsigned char bg;
    unsigned char attrs;
};

struct screen {
    int rows, cols;
    struct cell *cells;
    int cursor_row, cursor_col;
    unsigned char brush_fg, brush_bg, brush_attrs;
};

int screen_init(struct screen *s, int rows, int cols);
void screen_free(struct screen *s);
struct cell *screen_cell(struct screen *s, int row, int col);
void screen_set_brush(struct screen *s, unsigned char fg,
                      unsigned char bg, unsigned char attrs);
void screen_put(struct screen *s, char ch);
void screen_set_cursor(struct screen *s, int row, int col);
void screen_move_cursor(struct screen *s, int dr, int dc);
void screen_newline(struct screen *s);
void screen_backspace(struct screen *s);
void screen_clear(struct screen *s);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

/* copy row r's characters into out as a NUL-terminated string */
static void row_str(struct screen *s, int r, char *out) {
    for (int c = 0; c < s->cols; c++)
        out[c] = screen_cell(s, r, c)->ch;
    out[s->cols] = '\0';
}

int main(void) {
    struct screen s;

    /* init: blank cells, default style, origin cursor */
    check(screen_init(&s, 5, 10) == 0, "test_init_ok");
    check(s.rows == 5 && s.cols == 10, "test_dims");
    check(s.cursor_row == 0 && s.cursor_col == 0, "test_cursor_origin");
    int blank = 1;
    for (int r = 0; r < 5; r++)
        for (int c = 0; c < 10; c++) {
            struct cell *cl = screen_cell(&s, r, c);
            if (!cl || cl->ch != ' ' || cl->fg != COLOR_DEFAULT_IDX ||
                cl->bg != COLOR_DEFAULT_IDX || cl->attrs != 0)
                blank = 0;
        }
    check(blank, "test_init_blank_cells");

    /* bounds checking lives in screen_cell */
    check(screen_cell(&s, -1, 0) == NULL && screen_cell(&s, 0, -1) == NULL &&
          screen_cell(&s, 5, 0) == NULL && screen_cell(&s, 0, 10) == NULL,
          "test_cell_bounds");

    /* if the grid isn't usable, stop before dereferencing it */
    if (screen_cell(&s, 0, 0) == NULL) {
        printf("--- FAIL: grid_unusable_bailing_out\n");
        return failed + 1;
    }

    /* put advances and stamps the brush */
    screen_put(&s, 'A');
    check(screen_cell(&s, 0, 0)->ch == 'A' && s.cursor_col == 1,
          "test_put_advances");
    screen_set_brush(&s, 1, COLOR_DEFAULT_IDX, CELL_BOLD);
    screen_put(&s, 'B');
    {
        struct cell *cl = screen_cell(&s, 0, 1);
        check(cl->ch == 'B' && cl->fg == 1 && cl->attrs == CELL_BOLD,
              "test_put_uses_brush");
    }

    /* wrap at the right edge */
    screen_set_cursor(&s, 1, 9);
    screen_put(&s, 'X');
    check(s.cursor_row == 2 && s.cursor_col == 0, "test_put_wraps");
    check(screen_cell(&s, 1, 9)->ch == 'X', "test_put_wrote_before_wrap");

    /* clamp at the bottom-right corner */
    screen_set_cursor(&s, 4, 9);
    screen_put(&s, 'Y');
    check(s.cursor_row == 4, "test_put_clamps_at_bottom");

    /* absolute + relative movement clamp */
    screen_set_cursor(&s, 100, 100);
    check(s.cursor_row == 4 && s.cursor_col == 9, "test_set_cursor_clamps");
    screen_set_cursor(&s, 2, 3);
    screen_move_cursor(&s, 1, 2);
    check(s.cursor_row == 3 && s.cursor_col == 5, "test_move_cursor");
    screen_move_cursor(&s, -100, -100);
    check(s.cursor_row == 0 && s.cursor_col == 0, "test_move_cursor_clamps");

    /* newline */
    screen_set_cursor(&s, 1, 7);
    screen_newline(&s);
    check(s.cursor_row == 2 && s.cursor_col == 0, "test_newline");
    screen_set_cursor(&s, 4, 3);
    screen_newline(&s);
    check(s.cursor_row == 4 && s.cursor_col == 0, "test_newline_clamps");

    /* backspace erases with default style */
    screen_free(&s);
    screen_init(&s, 3, 8);
    screen_set_brush(&s, 2, 4, CELL_REVERSE);
    screen_put(&s, 'H');
    screen_put(&s, 'I');
    screen_backspace(&s);
    {
        struct cell *cl = screen_cell(&s, 0, 1);
        check(s.cursor_col == 1 && cl->ch == ' ' &&
              cl->fg == COLOR_DEFAULT_IDX && cl->attrs == 0,
              "test_backspace_blanks");
    }
    screen_set_cursor(&s, 1, 0);
    screen_backspace(&s);
    check(s.cursor_row == 1 && s.cursor_col == 0,
          "test_backspace_at_col0_noop");

    /* clear resets cells but not cursor/brush */
    screen_set_cursor(&s, 1, 4);
    screen_set_brush(&s, 3, COLOR_DEFAULT_IDX, 0);
    screen_clear(&s);
    check(screen_cell(&s, 0, 0)->ch == ' ', "test_clear_blanks");
    check(s.cursor_row == 1 && s.cursor_col == 4,
          "test_clear_keeps_cursor");
    check(s.brush_fg == 3, "test_clear_keeps_brush");

    /* a sentence renders where expected */
    screen_clear(&s);
    screen_set_cursor(&s, 2, 0);
    const char *msg = "hi there";
    for (const char *p = msg; *p; p++)
        screen_put(&s, *p);
    char row[16];
    row_str(&s, 2, row);
    check(strcmp(row, "hi there") == 0, "test_sentence");

    screen_free(&s);
    screen_free(&s); /* double free must be safe */
    check(1, "test_double_free_safe");

    return failed;
}
```
# Lesson: Interpreting the Stream — Terminal Semantics {#interpret}

You have a parser that turns bytes into events, and a screen that
mutates cells. This lesson is the join: **what each event means**.
This is where your program stops being a collection of parts and
becomes a terminal emulator — the component that could sit on a pty
master, watch a real program's output, and know what the screen
should look like.

Precision matters here more than anywhere else in the course. Every
program that draws to a terminal is betting on these exact semantics;
be one column off in tab handling and `ls -l` output leans like a
tower in Pisa.

## Control characters (the CTRL events)

- **CR `\r`** — cursor to column 0. Nothing else. Doesn't erase.
- **LF `\n`** — cursor down one row, **column unchanged**. This is
  the one everyone gets wrong, because the canonical-mode tty driver
  spent your whole life quietly turning `\n` into `\r\n` (that's
  `ONLCR`, which raw mode disabled). At the bottom row, LF scrolls —
  but scrolling is the next lesson; for now LF clamps at the bottom.
- **BS `\b`** — cursor left one, clamped at column 0. Doesn't erase!
  Programs erase by printing `\b \b` — left, space over it, left
  again. (This is also how progress bars rewrite themselves; that,
  and `\r` + reprint.)
- **TAB `\t`** — cursor right to the next tab stop: the next column
  that's a multiple of 8, clamped to the last column. Columns
  0→8→16→…. A tab at column 8 goes to 16, not nowhere.
- **BEL `\a`** — ring the bell. We ignore it with a clear
  conscience.

## CSI commands

The dispatch is on the final byte; the parameters were pre-chewed by
your parser. Remember the two defaulting rules: a missing parameter
is 0, and for the movement/positioning commands **both 0 and absent
mean the default**, which is 1.

- **CUP — `H` (and its twin `f`)**: cursor to (row, col), 1-based on
  the wire, so subtract 1 for your 0-based grid. Out-of-range values
  clamp to the edges (`ESC[999;999H` is the standard "go to
  bottom-right corner" idiom — real programs rely on the clamp!).
- **CUU/CUD/CUF/CUB — `A`/`B`/`C`/`D`**: relative moves of max(1, p)
  rows/columns, clamped at the edges.
- **ED — `J`**: erase in display. p=0: cursor to end of screen
  (inclusive); p=1: start of screen to cursor (inclusive); p=2:
  everything. **The cursor does not move** — programs invariably
  follow `ESC[2J` with `ESC[H`, and if your emulator moves the
  cursor on its own, doubly-moved cursors paint chaos. Erased cells
  become blanks with default style.
- **EL — `K`**: erase in line, same three modes, same "cursor stays
  put", same blank-with-default result.
- **SGR — `m`**: mutate the brush. Walk the parameter list (an empty
  list acts like a single 0):
  - `0` reset brush to defaults, `1` bold on, `4` underline on,
    `7` reverse on.
  - `30`–`37` fg = p−30; `39` fg = default. `40`–`47` bg = p−40;
    `49` bg = default.
  - `38` / `48` are the extended-color introducers. We won't *store*
    256/truecolor in our one-byte cells, but you must **skip their
    arguments correctly** — `38;5;n` consumes two extra parameters,
    `38;2;r;g;b` consumes four — or you'll misread whatever follows
    them in the same sequence. (`ESC[38;5;208;1m` ends with a
    perfectly good bold you'd otherwise eat.)
  - Anything else: ignore, move on. Unknown SGR codes arrive
    constantly from the wild; a terminal that trips on them is a
    toy.
- **Private modes (`priv` flag set)**: `?25h`/`?25l` show/hide
  cursor — track it in a flag; the renderer will care. Everything
  else private: ignore politely.
- **Unknown finals**: ignore. Same reasoning as unknown SGR.

## Simple ESC commands

`ESC 7` saves the cursor position (and, in real terminals, the
brush); `ESC 8` restores it. One saved slot, overwritten by each
`ESC 7`. Full-screen programs bracket temporary excursions with
these. `ESC c` is "reset to initial state" — clear everything, home
the cursor, default brush. Others: ignore.

## The shape of the join

```c
void term_apply(struct term *t, const struct vt_event *ev);
```

One function, one switch on `ev->type`, with a nested switch on
`ev->final` for CSI. All the grid mechanics you already built; this
layer is pure *policy*. Keep it boring: every branch a few lines,
every default explicit. Boring code is what correctness looks like
at this altitude.

(A design footnote worth absorbing: notice we do *not* wire the
parser to the screen with callbacks or function pointers. Events are
plain values; the caller feeds bytes to one object and hands the
results to another. In C, dumb data flowing between dumb components
beats architecture every time.)

## Challenge: Applying Events to the Grid {#term-feed points=35}

The starter provides the `term` struct (a cell grid with cursor,
brush, saved-cursor slot, and a cursor-visibility flag) plus its
init/free/accessor — deliberately minimal so this challenge stands
alone; substituting your own screen code afterwards is encouraged.
The event struct is exactly your VT parser's.

You implement `term_apply` with the lesson's semantics:

- `VT_PRINT`: stamp the glyph with the brush at the cursor; advance
  with wrap-at-right-edge; clamp at the bottom row (row stays, the
  wrap still returns the cursor to column 0).
- `VT_CTRL`: `\r`, `\n`, `\b`, `\t` (tab stops every 8), ignore the
  rest.
- `VT_CSI`: `H f A B C D J K m`, private 25 h/l, per the lesson.
- `VT_ESC`: `7` save cursor, `8` restore (restoring with nothing
  saved: no-op), `c` full reset; ignore others.

The tests drive `term_apply` with hand-built event sequences and
check the resulting grid — including the classic traps: LF keeping
its column, ED leaving the cursor alone, `ESC[999;999H` clamping,
`38;5;208` not eating a following bold, and `ESC[m` acting as reset.

### Starter

```c
#include <stdlib.h>
#include <string.h>

#define VT_MAX_PARAMS 16

enum vt_event_type { VT_PRINT, VT_CTRL, VT_CSI, VT_ESC };

struct vt_event {
    enum vt_event_type type;
    unsigned char ch;
    unsigned char final;
    int params[VT_MAX_PARAMS];
    int nparams;
    int priv;
};

#define CELL_BOLD      1u
#define CELL_UNDERLINE 2u
#define CELL_REVERSE   4u
#define COLOR_DEFAULT_IDX 9

struct cell {
    char ch;
    unsigned char fg;
    unsigned char bg;
    unsigned char attrs;
};

struct term {
    int rows, cols;
    struct cell *cells;
    int cursor_row, cursor_col;
    unsigned char brush_fg, brush_bg, brush_attrs;
    int saved_row, saved_col;   /* ESC 7 / ESC 8; -1 = nothing saved */
    int cursor_visible;         /* ?25h / ?25l */
};

/* --- provided --- */

static void blank_cell(struct cell *c) {
    c->ch = ' ';
    c->fg = COLOR_DEFAULT_IDX;
    c->bg = COLOR_DEFAULT_IDX;
    c->attrs = 0;
}

int term_init(struct term *t, int rows, int cols) {
    memset(t, 0, sizeof(*t));
    t->rows = rows;
    t->cols = cols;
    t->cells = malloc((size_t)rows * (size_t)cols * sizeof(struct cell));
    if (!t->cells) return -1;
    for (int i = 0; i < rows * cols; i++)
        blank_cell(&t->cells[i]);
    t->brush_fg = t->brush_bg = COLOR_DEFAULT_IDX;
    t->saved_row = t->saved_col = -1;
    t->cursor_visible = 1;
    return 0;
}

void term_free(struct term *t) {
    free(t->cells);
    t->cells = NULL;
}

struct cell *term_cell(struct term *t, int row, int col) {
    if (row < 0 || row >= t->rows || col < 0 || col >= t->cols)
        return NULL;
    return &t->cells[row * t->cols + col];
}

/* --- yours --- */

/* CSI positioning params: 0 or missing both mean 1. */
static int param_or(const struct vt_event *ev, int idx, int dflt) {
    if (idx >= ev->nparams || ev->params[idx] == 0) return dflt;
    return ev->params[idx];
}

void term_apply(struct term *t, const struct vt_event *ev) {
    switch (ev->type) {
    case VT_PRINT:
        /* TODO: stamp + advance (wrap right edge, clamp bottom) */
        break;
    case VT_CTRL:
        /* TODO: \r \n \b \t; ignore others */
        break;
    case VT_CSI:
        if (ev->priv) {
            /* TODO: params[0]==25: 'h' show / 'l' hide cursor */
            break;
        }
        switch (ev->final) {
        /* TODO: 'H' and 'f': CUP (1-based! clamp!)            */
        /* TODO: 'A','B','C','D': relative moves, max(1,p)     */
        /* TODO: 'J': ED 0/1/2 — cursor does NOT move          */
        /* TODO: 'K': EL 0/1/2 — cursor does NOT move          */
        /* TODO: 'm': SGR — walk params; empty list = reset;   */
        /*       skip 38/48 argument groups correctly          */
        default:
            break;
        }
        break;
    case VT_ESC:
        /* TODO: '7' save cursor, '8' restore, 'c' full reset */
        break;
    }
    (void)param_or;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdarg.h>

#define VT_MAX_PARAMS 16

enum vt_event_type { VT_PRINT, VT_CTRL, VT_CSI, VT_ESC };

struct vt_event {
    enum vt_event_type type;
    unsigned char ch;
    unsigned char final;
    int params[VT_MAX_PARAMS];
    int nparams;
    int priv;
};

#define CELL_BOLD      1u
#define CELL_UNDERLINE 2u
#define CELL_REVERSE   4u
#define COLOR_DEFAULT_IDX 9

struct cell {
    char ch;
    unsigned char fg;
    unsigned char bg;
    unsigned char attrs;
};

struct term {
    int rows, cols;
    struct cell *cells;
    int cursor_row, cursor_col;
    unsigned char brush_fg, brush_bg, brush_attrs;
    int saved_row, saved_col;
    int cursor_visible;
};

int term_init(struct term *t, int rows, int cols);
void term_free(struct term *t);
struct cell *term_cell(struct term *t, int row, int col);
void term_apply(struct term *t, const struct vt_event *ev);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

/* --- event constructors --- */

static void print_str(struct term *t, const char *s) {
    for (; *s; s++) {
        struct vt_event ev = { .type = VT_PRINT, .ch = (unsigned char)*s };
        term_apply(t, &ev);
    }
}

static void ctrl(struct term *t, char c) {
    struct vt_event ev = { .type = VT_CTRL, .ch = (unsigned char)c };
    term_apply(t, &ev);
}

static void csi(struct term *t, unsigned char final, int priv,
                int nparams, ...) {
    struct vt_event ev = { .type = VT_CSI, .final = final,
                           .nparams = nparams, .priv = priv };
    va_list ap;
    va_start(ap, nparams);
    for (int i = 0; i < nparams && i < VT_MAX_PARAMS; i++)
        ev.params[i] = va_arg(ap, int);
    va_end(ap);
    term_apply(t, &ev);
}

static void esc(struct term *t, unsigned char final) {
    struct vt_event ev = { .type = VT_ESC, .final = final };
    term_apply(t, &ev);
}

static void row_str(struct term *t, int r, char *out) {
    for (int c = 0; c < t->cols; c++)
        out[c] = term_cell(t, r, c)->ch;
    out[t->cols] = '\0';
}

int main(void) {
    struct term t;
    char row[64];
    term_init(&t, 6, 12);

    /* printing advances and wraps */
    print_str(&t, "hello");
    row_str(&t, 0, row);
    check(strncmp(row, "hello", 5) == 0 && t.cursor_col == 5,
          "test_print_basic");

    /* CR returns to column 0 without erasing */
    ctrl(&t, '\r');
    check(t.cursor_row == 0 && t.cursor_col == 0, "test_cr");
    row_str(&t, 0, row);
    check(strncmp(row, "hello", 5) == 0, "test_cr_does_not_erase");

    /* LF keeps the column! */
    csi(&t, 'H', 0, 2, 1, 4);           /* to row 0, col 3 */
    ctrl(&t, '\n');
    check(t.cursor_row == 1 && t.cursor_col == 3, "test_lf_keeps_column");

    /* BS moves left, doesn't erase */
    ctrl(&t, '\b');
    check(t.cursor_col == 2, "test_bs_moves_left");

    /* TAB to the next multiple of 8 */
    csi(&t, 'H', 0, 2, 3, 1);           /* row 2, col 0 */
    ctrl(&t, '\t');
    check(t.cursor_col == 8, "test_tab_from_0");
    ctrl(&t, '\t');
    check(t.cursor_col == 11, "test_tab_clamps_at_edge"); /* cols=12 */
    csi(&t, 'H', 0, 2, 3, 3);           /* col 2 */
    ctrl(&t, '\t');
    check(t.cursor_col == 8, "test_tab_from_middle");

    /* CUP: 1-based, defaults, clamping */
    csi(&t, 'H', 0, 2, 4, 7);
    check(t.cursor_row == 3 && t.cursor_col == 6, "test_cup");
    csi(&t, 'H', 0, 0);
    check(t.cursor_row == 0 && t.cursor_col == 0, "test_cup_defaults_home");
    csi(&t, 'H', 0, 2, 999, 999);
    check(t.cursor_row == 5 && t.cursor_col == 11, "test_cup_clamps");
    csi(&t, 'f', 0, 2, 2, 2);
    check(t.cursor_row == 1 && t.cursor_col == 1, "test_cup_f_twin");

    /* relative moves: default 1, explicit n, clamped */
    csi(&t, 'H', 0, 2, 3, 5);
    csi(&t, 'A', 0, 0);
    check(t.cursor_row == 1, "test_cuu_default_1");
    csi(&t, 'B', 0, 1, 3);
    check(t.cursor_row == 4, "test_cud_n");
    csi(&t, 'C', 0, 1, 99);
    check(t.cursor_col == 11, "test_cuf_clamps");
    csi(&t, 'D', 0, 1, 2);
    check(t.cursor_col == 9, "test_cub_n");

    /* ED 2: clears everything, cursor stays */
    csi(&t, 'H', 0, 2, 3, 4);
    csi(&t, 'J', 0, 1, 2);
    check(t.cursor_row == 2 && t.cursor_col == 3, "test_ed2_keeps_cursor");
    int all_blank = 1;
    for (int r = 0; r < t.rows; r++)
        for (int c = 0; c < t.cols; c++)
            if (term_cell(&t, r, c)->ch != ' ') all_blank = 0;
    check(all_blank, "test_ed2_clears_all");

    /* ED 0 / ED 1 split the screen at the cursor (inclusive) */
    term_free(&t);
    term_init(&t, 3, 4);
    csi(&t, 'H', 0, 0);
    print_str(&t, "aaaabbbbcccc");        /* fills all 3 rows */
    csi(&t, 'H', 0, 2, 2, 2);             /* row 1, col 1 */
    csi(&t, 'J', 0, 1, 0);                /* erase cursor -> end */
    row_str(&t, 0, row);
    check(strcmp(row, "aaaa") == 0, "test_ed0_keeps_before");
    row_str(&t, 1, row);
    check(strcmp(row, "b   ") == 0, "test_ed0_erases_from_cursor");
    row_str(&t, 2, row);
    check(strcmp(row, "    ") == 0, "test_ed0_erases_below");

    term_free(&t);
    term_init(&t, 3, 4);
    print_str(&t, "aaaabbbbcccc");
    csi(&t, 'H', 0, 2, 2, 2);
    csi(&t, 'J', 0, 1, 1);                /* erase start -> cursor */
    row_str(&t, 0, row);
    check(strcmp(row, "    ") == 0, "test_ed1_erases_above");
    row_str(&t, 1, row);
    check(strcmp(row, "  bb") == 0, "test_ed1_erases_to_cursor");
    row_str(&t, 2, row);
    check(strcmp(row, "cccc") == 0, "test_ed1_keeps_after");

    /* EL variants */
    term_free(&t);
    term_init(&t, 2, 6);
    print_str(&t, "abcdef");
    csi(&t, 'H', 0, 2, 1, 3);             /* row 0, col 2 */
    csi(&t, 'K', 0, 0);                   /* default 0: to end */
    row_str(&t, 0, row);
    check(strcmp(row, "ab    ") == 0, "test_el0");
    check(t.cursor_col == 2, "test_el_keeps_cursor");

    term_free(&t);
    term_init(&t, 2, 6);
    print_str(&t, "abcdef");
    csi(&t, 'H', 0, 2, 1, 3);
    csi(&t, 'K', 0, 1, 1);
    row_str(&t, 0, row);
    check(strcmp(row, "   def") == 0, "test_el1_inclusive");

    term_free(&t);
    term_init(&t, 2, 6);
    print_str(&t, "abcdef");
    csi(&t, 'H', 0, 2, 1, 3);
    csi(&t, 'K', 0, 1, 2);
    row_str(&t, 0, row);
    check(strcmp(row, "      ") == 0, "test_el2_whole_line");

    /* SGR drives the brush */
    term_free(&t);
    term_init(&t, 2, 10);
    csi(&t, 'm', 0, 2, 1, 31);            /* bold red */
    print_str(&t, "x");
    {
        struct cell *c = term_cell(&t, 0, 0);
        check(c->fg == 1 && (c->attrs & CELL_BOLD), "test_sgr_bold_red");
    }
    csi(&t, 'm', 0, 1, 0);                /* reset */
    print_str(&t, "y");
    {
        struct cell *c = term_cell(&t, 0, 1);
        check(c->fg == COLOR_DEFAULT_IDX && c->attrs == 0,
              "test_sgr_reset");
    }
    csi(&t, 'm', 0, 0);                   /* EMPTY list = reset too */
    csi(&t, 'm', 0, 2, 7, 44);            /* reverse, blue bg */
    print_str(&t, "z");
    {
        struct cell *c = term_cell(&t, 0, 2);
        check((c->attrs & CELL_REVERSE) && c->bg == 4,
              "test_sgr_reverse_bg");
    }
    csi(&t, 'm', 0, 0);
    check(t.brush_attrs == 0 && t.brush_bg == COLOR_DEFAULT_IDX,
          "test_sgr_empty_is_reset");

    /* 38;5;208 must not eat the following bold */
    csi(&t, 'm', 0, 4, 38, 5, 208, 1);
    print_str(&t, "w");
    {
        struct cell *c = term_cell(&t, 0, 3);
        check((c->attrs & CELL_BOLD) != 0, "test_sgr_skips_extended_color");
    }
    /* 38;2;r;g;b likewise */
    csi(&t, 'm', 0, 1, 0);
    csi(&t, 'm', 0, 6, 38, 2, 10, 20, 30, 4);
    check((t.brush_attrs & CELL_UNDERLINE) != 0,
          "test_sgr_skips_truecolor");

    /* unknown SGR codes are ignored, list continues */
    csi(&t, 'm', 0, 1, 0);
    csi(&t, 'm', 0, 2, 53, 1);            /* 53 = overline, unsupported */
    check((t.brush_attrs & CELL_BOLD) != 0, "test_sgr_unknown_ignored");

    /* cursor visibility */
    check(t.cursor_visible == 1, "test_cursor_starts_visible");
    csi(&t, 'l', 1, 1, 25);
    check(t.cursor_visible == 0, "test_hide_cursor");
    csi(&t, 'h', 1, 1, 25);
    check(t.cursor_visible == 1, "test_show_cursor");

    /* save / restore cursor */
    csi(&t, 'H', 0, 2, 2, 5);
    esc(&t, '7');
    csi(&t, 'H', 0, 2, 1, 1);
    esc(&t, '8');
    check(t.cursor_row == 1 && t.cursor_col == 4, "test_save_restore");

    /* unknown CSI finals are ignored without damage */
    csi(&t, 'S', 0, 1, 3);
    csi(&t, 'q', 1, 1, 12);
    check(t.cursor_row == 1 && t.cursor_col == 4, "test_unknown_csi_ignored");

    /* full reset */
    print_str(&t, "junk");
    csi(&t, 'm', 0, 2, 1, 31);
    esc(&t, 'c');
    check(t.cursor_row == 0 && t.cursor_col == 0 &&
          t.brush_fg == COLOR_DEFAULT_IDX && t.brush_attrs == 0 &&
          term_cell(&t, 1, 4)->ch == ' ',
          "test_full_reset");

    term_free(&t);
    return failed;
}
```
# Lesson: Scrolling and Scrollback {#scrolling-region}

Print a line at the bottom of a full terminal and everything glides
up one row. It's so familiar it doesn't look like a feature — but
someone has to implement it, and in your emulator that someone is
you.

## What scrolling actually is

When a linefeed happens on the **last row**, the terminal doesn't
move the cursor down (there's no down); it moves the *content* up:

1. Row 0 disappears (hold that thought).
2. Rows 1..N−1 shift up by one.
3. The bottom row becomes blank.
4. The cursor stays exactly where it was — bottom row, same column.

With a flat `rows × cols` grid, the shift is one honest `memmove`:

```c
memmove(&cells[0],                    /* dst: row 0        */
        &cells[cols],                 /* src: row 1        */
        (size_t)(rows - 1) * cols * sizeof(cell));
/* then blank the last row */
```

`memmove`, not `memcpy` — the regions overlap, and overlapping
`memcpy` is undefined behavior that works right up until the day the
optimizer vectorizes it differently. (Real emulators dodge the copy
entirely by keeping a *ring of row pointers* and just rotating the
"which row is row 0" index. A worthwhile optimization exactly when
profiling says so — `cat` on a large file scrolls thousands of times
per second — and a distraction before that. We'll do the memmove.)

## Scrollback: where row 0 goes to live

That disappearing top row is the terminal's other beloved feature:
**scrollback** — the history you reach with Shift+PgUp. The screen
grid stays exactly `rows × cols`; scrolled-off lines move to a
separate buffer that only ever grows at one end and gets trimmed at
the other. First-in, first-out, bounded: a **ring buffer**.

Fix a capacity of `cap` lines and keep three numbers alongside the
storage:

```
head — the slot the NEXT line will be written into
len  — how many lines are currently stored (saturates at cap)
```

Writing line after line: slot `head`, then `head = (head+1) % cap`,
and `len` climbs until it hits `cap` and stays there — at which
point each new line silently lands on the slot of the oldest one.
That's the whole eviction policy. No shifting, no allocation per
line, O(1) always.

Reading is where rings earn their reputation for off-by-one bugs, so
derive it once, carefully. "The n-th most recent line" (n = 0 is the
newest) was written `n+1` writes before the next write, i.e. at slot

```
(head - 1 - n + cap) % cap        for n in [0, len)
```

The `+ cap` guards C's `%` against negative operands (in C, `-1 % 5`
is `-1`, not `4` — a fact that has ruined many evenings).

One more design note: notice the screen grid and the scrollback
never share memory. When a real emulator shows scrollback, it
*renders* from history while the grid keeps living underneath —
which is also why full-screen apps switch to the **alternate
screen** (`ESC[?1049h`): the alt screen has *no* scrollback, so vim
scrolling a file doesn't shred your shell history. Two grids, one
history buffer, sharp boundaries.

## Challenge: Scroll with Scrollback {#scrollback points=25}

A character-grid terminal (styles omitted to keep the focus on
motion) with three operations:

- `tg_scroll_up(t)` — push the top row into the scrollback ring
  (evicting the oldest line if full), shift the grid up, blank the
  bottom row. Cursor unchanged.
- `tg_linefeed(t)` — LF semantics on a scrolling screen: if the
  cursor is above the last row, move down one (column unchanged);
  on the last row, `tg_scroll_up` (cursor stays put, column
  unchanged).
- `tg_scrollback_get(t, n, out)` — copy the n-th most recent
  scrolled line (n = 0 newest) into `out` as a NUL-terminated
  string of exactly `cols` characters. Return 0, or -1 if `n` is
  out of range.

The starter provides init/free and a put-string helper; the tests
scroll far past capacity and read history back in exact order.

### Starter

```c
#include <stdlib.h>
#include <string.h>

struct termgrid {
    int rows, cols;
    char *cells;        /* rows * cols, row-major                  */
    int cursor_row, cursor_col;

    char *sb;           /* scrollback: sb_cap lines of cols chars  */
    int sb_cap;         /* capacity in lines                       */
    int sb_len;         /* lines stored (saturates at sb_cap)      */
    int sb_head;        /* slot the NEXT line goes into            */
};

/* --- provided --- */

int tg_init(struct termgrid *t, int rows, int cols, int sb_cap) {
    memset(t, 0, sizeof(*t));
    t->rows = rows;
    t->cols = cols;
    t->sb_cap = sb_cap;
    t->cells = malloc((size_t)rows * (size_t)cols);
    t->sb = malloc((size_t)sb_cap * (size_t)cols);
    if (!t->cells || !t->sb) {
        free(t->cells);
        free(t->sb);
        return -1;
    }
    memset(t->cells, ' ', (size_t)rows * (size_t)cols);
    return 0;
}

void tg_free(struct termgrid *t) {
    free(t->cells);
    free(t->sb);
    t->cells = NULL;
    t->sb = NULL;
}

/* write s at the cursor's row starting at column 0 (test helper) */
void tg_set_row(struct termgrid *t, int row, const char *s) {
    size_t n = strlen(s);
    if (n > (size_t)t->cols) n = (size_t)t->cols;
    memset(&t->cells[row * t->cols], ' ', (size_t)t->cols);
    memcpy(&t->cells[row * t->cols], s, n);
}

/* --- yours --- */

/* Push row 0 into the scrollback ring, shift the grid up one row,
 * blank the bottom row. The cursor does not move. */
void tg_scroll_up(struct termgrid *t) {
    /* TODO: copy row 0 into sb slot sb_head; advance sb_head       */
    /*       (mod sb_cap); saturate sb_len at sb_cap                */
    /* TODO: memmove rows 1..rows-1 up by one row                   */
    /* TODO: memset the last row to spaces                          */
    (void)t;
}

/* LF on a scrolling screen: down one row, or scroll at the bottom.
 * The column never changes. */
void tg_linefeed(struct termgrid *t) {
    /* TODO */
    (void)t;
}

/* Fetch the n-th most recent scrolled-off line (n = 0 newest) into
 * out (cols chars + NUL). Returns 0, or -1 if n out of range. */
int tg_scrollback_get(const struct termgrid *t, int n, char *out) {
    /* TODO: range check against sb_len                             */
    /* TODO: slot = (sb_head - 1 - n + sb_cap) % sb_cap             */
    (void)t; (void)n; (void)out;
    return -1;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

struct termgrid {
    int rows, cols;
    char *cells;
    int cursor_row, cursor_col;
    char *sb;
    int sb_cap;
    int sb_len;
    int sb_head;
};

int tg_init(struct termgrid *t, int rows, int cols, int sb_cap);
void tg_free(struct termgrid *t);
void tg_set_row(struct termgrid *t, int row, const char *s);
void tg_scroll_up(struct termgrid *t);
void tg_linefeed(struct termgrid *t);
int tg_scrollback_get(const struct termgrid *t, int n, char *out);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void grid_row(struct termgrid *t, int r, char *out) {
    memcpy(out, &t->cells[r * t->cols], (size_t)t->cols);
    out[t->cols] = '\0';
}

int main(void) {
    struct termgrid t;
    char buf[64];

    check(tg_init(&t, 3, 8, 4) == 0, "test_init");

    /* linefeed above the bottom just moves down, column intact */
    t.cursor_row = 0;
    t.cursor_col = 5;
    tg_linefeed(&t);
    check(t.cursor_row == 1 && t.cursor_col == 5,
          "test_lf_moves_down_keeps_col");

    /* scroll: content shifts, top row lands in scrollback */
    tg_set_row(&t, 0, "line-A");
    tg_set_row(&t, 1, "line-B");
    tg_set_row(&t, 2, "line-C");
    t.cursor_row = 2;
    t.cursor_col = 3;
    tg_linefeed(&t);                       /* at bottom: scrolls */
    check(t.cursor_row == 2 && t.cursor_col == 3,
          "test_lf_at_bottom_cursor_stays");
    grid_row(&t, 0, buf);
    check(strncmp(buf, "line-B", 6) == 0, "test_scroll_shifts_up");
    grid_row(&t, 1, buf);
    check(strncmp(buf, "line-C", 6) == 0, "test_scroll_shifts_up_2");
    grid_row(&t, 2, buf);
    check(strcmp(buf, "        ") == 0, "test_scroll_blank_bottom");

    check(t.sb_len == 1, "test_sb_has_one_line");
    check(tg_scrollback_get(&t, 0, buf) == 0 &&
          strncmp(buf, "line-A", 6) == 0,
          "test_sb_holds_top_row");
    check(buf[8] == '\0', "test_sb_line_nul_terminated");

    /* out-of-range reads are refused */
    check(tg_scrollback_get(&t, 1, buf) == -1, "test_sb_range_check");
    check(tg_scrollback_get(&t, -1, buf) == -1, "test_sb_negative_check");

    /* scroll several more: newest-first ordering */
    tg_set_row(&t, 0, "second");
    tg_scroll_up(&t);
    tg_set_row(&t, 0, "third");
    tg_scroll_up(&t);
    check(t.sb_len == 3, "test_sb_grows");
    tg_scrollback_get(&t, 0, buf);
    check(strncmp(buf, "third", 5) == 0, "test_sb_newest_first");
    tg_scrollback_get(&t, 2, buf);
    check(strncmp(buf, "line-A", 6) == 0, "test_sb_oldest_last");

    /* overflow the ring (cap 4): oldest lines evicted */
    tg_set_row(&t, 0, "fourth");
    tg_scroll_up(&t);
    tg_set_row(&t, 0, "fifth");
    tg_scroll_up(&t);
    tg_set_row(&t, 0, "sixth");
    tg_scroll_up(&t);
    check(t.sb_len == 4, "test_sb_saturates_at_cap");
    tg_scrollback_get(&t, 0, buf);
    check(strncmp(buf, "sixth", 5) == 0, "test_sb_newest_after_wrap");
    tg_scrollback_get(&t, 3, buf);
    check(strncmp(buf, "third", 5) == 0, "test_sb_oldest_after_wrap");
    check(tg_scrollback_get(&t, 4, buf) == -1, "test_sb_evicted_gone");

    /* a long pour: 100 linefeeds from the bottom row */
    tg_free(&t);
    tg_init(&t, 2, 8, 10);
    t.cursor_row = 1;
    for (int i = 0; i < 100; i++) {
        char line[16];
        snprintf(line, sizeof(line), "n-%d", i);
        tg_set_row(&t, 0, line);
        tg_linefeed(&t);
    }
    check(t.sb_len == 10, "test_pour_saturated");
    tg_scrollback_get(&t, 0, buf);
    check(strncmp(buf, "n-99", 4) == 0, "test_pour_newest");
    tg_scrollback_get(&t, 9, buf);
    check(strncmp(buf, "n-90", 4) == 0, "test_pour_tenth");

    tg_free(&t);
    return failed;
}
```
# Lesson: Rendering — Damage and Diffs {#render-diff}

You can now maintain a model of what the screen *should* show. The
renderer's job is to make the physical terminal agree with the model
— and the interesting question is how much you have to send to get
there.

## The cost of the naive frame

The blunt approach re-sends everything:

```
hide cursor · home · for each row: chars + \r\n · position cursor · show cursor
```

For a 50×200 window that's ~10 KB per frame. Hold a key with
auto-repeat at 30 Hz and you're pushing 300 KB/s — through a pty,
maybe through tmux, maybe over an ssh link to another continent. On
the LAN you won't notice. At 200 ms round-trip with a congested
uplink, "hold the down arrow" turns into a slideshow. And it's all
waste: between two frames of a text editor, typically *one row*
changed.

The fix has been reinvented by every serious screen program since
the 1980s (curses made it famous): **keep the previous frame, diff
against it, send only the damage.**

```
prev frame (what the terminal shows)
next frame (what the model says)
        │
        ▼
for each row that differs:
    position cursor at first difference
    re-send the changed span
```

Now a keystroke that changes one row costs ~10 bytes: one CUP, a few
characters. That's a 1000× reduction, achieved with a `memcmp` and a
loop — the single highest-leverage optimization in the terminal
world.

## Diffing well, without diffing perfectly

How fine-grained should the diff be? The spectrum:

- **Frame-level**: anything changed → redraw all. (The naive plan.)
- **Row-level**: `memcmp` each row pair; changed rows are redrawn
  whole. Simple, catches the common cases (one line edited, status
  bar ticking).
- **Span-level**: within a changed row, find the first and last
  differing columns and send just that span. One CUP + the span.
- **Run-level**: multiple separate dirty spans per row, each with
  its own CUP. Optimal output, fiddlier loop — and now positioning
  sequences (~8 bytes each) compete with just re-sending the clean
  gap between two nearby spans. True minimality is a cost model, not
  a diff.

Production emulators mix heuristics ("if the row is mostly dirty,
send it all") because *close enough is genuinely fine*: the payoff
plateau is reached at span-level. That's what you'll build: per row,
first-diff to last-diff, one CUP, one span.

Two disciplines make a diff renderer correct rather than just fast:

- **The prev frame must be the truth.** After emitting the diff, the
  new frame *becomes* prev — and nothing else may write to the
  terminal behind the renderer's back, or the model and reality
  diverge and stale cells haunt the screen until the next full
  redraw. (That's why full-screen apps repaint everything on
  Ctrl+L and on resize: it's the "I no longer trust prev" reset.)
- **Positioning must be absolute.** Tempting shortcut: "the cursor
  is already on row 4 col 10 after that span, the next span starts
  at col 15, just print the gap". Real emulators do track that —
  and every bug where the optimizer's idea of the cursor drifts
  from the terminal's produces smeared garbage. Start with one
  absolute CUP per span; optimize only against measurements.

A last habit of the pros, worth knowing: bracket the whole frame
with hide-cursor / show-cursor (the flicker fix from the screen
lesson), and if you ever chase visible tearing on slow links, look
up "synchronized output" (`ESC[?2026h`) — the modern extension that
lets a program mark frame boundaries explicitly.

## Challenge: Diff Two Frames {#frame-diff points=25}

`render_diff(prev, next, out)`: append to `out` (your abuf,
implementation provided) the byte sequence that transforms a
terminal showing `prev` into one showing `next`:

- For each row where the frames differ: exactly **one absolute CUP**
  (`\x1b[<row+1>;<col+1>H`, 1-based!) targeting the first differing
  column, followed by the row's characters from the first through
  the last differing column.
- Rows with no differences contribute **zero bytes**.
- Equal frames produce an **empty** buffer.

The tests replay your output through a reference terminal
interpreter seeded with `prev` and require it to end up exactly
`next` (correctness), then meter your byte counts (efficiency): an
unchanged frame emits nothing, a one-cell change stays under 16
bytes, and a one-row change doesn't touch other rows.

### Starter

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

/* --- provided: the append buffer from earlier --- */

struct abuf {
    char *b;
    size_t len;
    size_t cap;
};

void ab_init(struct abuf *ab) { ab->b = NULL; ab->len = 0; ab->cap = 0; }

int ab_append(struct abuf *ab, const char *bytes, size_t n) {
    if (ab->len + n > ab->cap) {
        size_t cap = ab->cap ? ab->cap : 64;
        while (cap < ab->len + n) cap *= 2;
        char *nb = realloc(ab->b, cap);
        if (!nb) return -1;
        ab->b = nb;
        ab->cap = cap;
    }
    memcpy(ab->b + ab->len, bytes, n);
    ab->len += n;
    return 0;
}

void ab_free(struct abuf *ab) { free(ab->b); ab_init(ab); }

/* --- provided: a frame is a bare character grid --- */

struct frame {
    int rows, cols;
    char *cells;                  /* rows * cols, row-major */
};

int frame_init(struct frame *f, int rows, int cols) {
    f->rows = rows;
    f->cols = cols;
    f->cells = malloc((size_t)rows * (size_t)cols);
    if (!f->cells) return -1;
    memset(f->cells, ' ', (size_t)rows * (size_t)cols);
    return 0;
}

void frame_free(struct frame *f) { free(f->cells); f->cells = NULL; }

char *frame_row(const struct frame *f, int r) {
    return &f->cells[r * f->cols];
}

/* --- yours --- */

/* Append to out the bytes that turn a terminal showing prev into
 * one showing next. Per changed row: ONE absolute CUP to the first
 * differing column, then the chars through the last differing
 * column. Unchanged rows emit nothing.
 * Returns 0, or -1 on allocation failure. */
int render_diff(const struct frame *prev, const struct frame *next,
                struct abuf *out) {
    /* TODO: for each row: memcmp fast-path; if different, scan for  */
    /*       first and last differing columns                        */
    /* TODO: snprintf the CUP (1-based!), ab_append it, then append  */
    /*       next's chars [first..last]                              */
    (void)prev; (void)next; (void)out;
    return -1;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

struct abuf {
    char *b;
    size_t len;
    size_t cap;
};

void ab_init(struct abuf *ab);
int ab_append(struct abuf *ab, const char *bytes, size_t n);
void ab_free(struct abuf *ab);

struct frame {
    int rows, cols;
    char *cells;
};

int frame_init(struct frame *f, int rows, int cols);
void frame_free(struct frame *f);
char *frame_row(const struct frame *f, int r);

int render_diff(const struct frame *prev, const struct frame *next,
                struct abuf *out);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void set_row(struct frame *f, int r, const char *s) {
    size_t n = strlen(s);
    if (n > (size_t)f->cols) n = (size_t)f->cols;
    memset(frame_row(f, r), ' ', (size_t)f->cols);
    memcpy(frame_row(f, r), s, n);
}

/* Reference terminal: seed with a frame, replay bytes. Accepts ONLY
 * absolute CUP sequences and printable characters; anything else is
 * a protocol violation and fails the replay. Returns 0 on success. */
static int replay(struct frame *screen, const char *bytes, size_t len) {
    int row = 0, col = 0;
    size_t i = 0;
    while (i < len) {
        unsigned char b = (unsigned char)bytes[i];
        if (b == 0x1b) {
            if (i + 1 >= len || bytes[i + 1] != '[') return -1;
            i += 2;
            int r = 0, c = 0;
            if (i >= len || bytes[i] < '0' || bytes[i] > '9') return -1;
            while (i < len && bytes[i] >= '0' && bytes[i] <= '9')
                r = r * 10 + (bytes[i++] - '0');
            if (i >= len || bytes[i] != ';') return -1;
            i++;
            if (i >= len || bytes[i] < '0' || bytes[i] > '9') return -1;
            while (i < len && bytes[i] >= '0' && bytes[i] <= '9')
                c = c * 10 + (bytes[i++] - '0');
            if (i >= len || bytes[i] != 'H') return -1;
            i++;
            row = r - 1;
            col = c - 1;
            if (row < 0 || row >= screen->rows ||
                col < 0 || col >= screen->cols)
                return -1;
        } else if (b >= 0x20 && b != 0x7f) {
            if (row >= screen->rows || col >= screen->cols) return -1;
            frame_row(screen, row)[col] = (char)b;
            col++;
            if (col == screen->cols) { col = 0; if (row < screen->rows - 1) row++; }
            i++;
        } else {
            return -1; /* control bytes other than CUP: not allowed */
        }
    }
    return 0;
}

/* run a diff, replay it, require the result to equal next; returns
 * the diff's byte length (or -1 on any failure) */
static long diff_and_verify(struct frame *prev, struct frame *next,
                            const char *name) {
    struct abuf out;
    ab_init(&out);
    if (render_diff(prev, next, &out) != 0) {
        printf("--- FAIL: %s (render_diff errored)\n", name);
        failed++;
        ab_free(&out);
        return -1;
    }
    /* replay onto a copy of prev */
    struct frame sim;
    frame_init(&sim, prev->rows, prev->cols);
    memcpy(sim.cells, prev->cells, (size_t)prev->rows * prev->cols);
    int rc = replay(&sim, out.b, out.len);
    int same = rc == 0 &&
        memcmp(sim.cells, next->cells,
               (size_t)next->rows * next->cols) == 0;
    if (!same) {
        printf("--- FAIL: %s (replay mismatch)\n", name);
        failed++;
    } else {
        printf("--- PASS: %s\n", name);
    }
    long n = (long)out.len;
    frame_free(&sim);
    ab_free(&out);
    return same ? n : -1;
}

int main(void) {
    struct frame prev, next;
    frame_init(&prev, 10, 40);
    frame_init(&next, 10, 40);

    /* identical frames: zero bytes */
    set_row(&prev, 0, "hello world");
    set_row(&next, 0, "hello world");
    long n = diff_and_verify(&prev, &next, "test_identical_replay");
    check(n == 0, "test_identical_emits_nothing");

    /* one cell changes: correct and tiny */
    set_row(&next, 0, "hellu world");
    n = diff_and_verify(&prev, &next, "test_one_cell_replay");
    check(n > 0 && n <= 16, "test_one_cell_is_small");

    /* one row changes in the middle */
    memcpy(prev.cells, next.cells, 10 * 40);
    set_row(&prev, 4, "aaaaaaaaaa");
    set_row(&next, 4, "aaabbbaaaa");
    n = diff_and_verify(&prev, &next, "test_span_replay");
    /* span is cols 3..5: one CUP (<=10 bytes) + 3 chars */
    check(n > 0 && n <= 14, "test_span_is_tight");

    /* several changed rows, each handled independently */
    memcpy(prev.cells, next.cells, 10 * 40);
    set_row(&next, 1, "first change");
    set_row(&next, 7, "second change");
    set_row(&next, 9, "third");
    n = diff_and_verify(&prev, &next, "test_multi_row_replay");
    check(n > 0 && n <= 3 * (10 + 40), "test_multi_row_bounded");

    /* changes at the extreme corners */
    memcpy(prev.cells, next.cells, 10 * 40);
    frame_row(&next, 0)[0] = '!';
    frame_row(&next, 9)[39] = '?';
    diff_and_verify(&prev, &next, "test_corners_replay");

    /* full-frame change: bounded by rows * (CUP + cols) */
    for (int r = 0; r < 10; r++) {
        char line[41];
        memset(line, 'x', 40);
        line[40] = '\0';
        set_row(&next, r, line);
    }
    memset(prev.cells, ' ', 10 * 40);
    n = diff_and_verify(&prev, &next, "test_full_frame_replay");
    check(n > 0 && n <= 10 * (12 + 40), "test_full_frame_bounded");

    /* whole-buffer growth torture: 200 random-ish frames all replay */
    {
        unsigned x = 99;
        int ok = 1;
        for (int iter = 0; iter < 200 && ok; iter++) {
            memcpy(prev.cells, next.cells, 10 * 40);
            /* mutate a handful of cells */
            for (int k = 0; k < 5; k++) {
                x = x * 1103515245 + 12345;
                int r = (int)((x >> 16) % 10);
                x = x * 1103515245 + 12345;
                int c = (int)((x >> 16) % 40);
                x = x * 1103515245 + 12345;
                frame_row(&next, r)[c] = (char)('A' + ((x >> 16) % 26));
            }
            struct abuf out;
            ab_init(&out);
            render_diff(&prev, &next, &out);
            struct frame sim;
            frame_init(&sim, 10, 40);
            memcpy(sim.cells, prev.cells, 10 * 40);
            if (replay(&sim, out.b, out.len) != 0 ||
                memcmp(sim.cells, next.cells, 10 * 40) != 0)
                ok = 0;
            frame_free(&sim);
            ab_free(&out);
        }
        check(ok, "test_fuzz_frames_replay");
    }

    frame_free(&prev);
    frame_free(&next);
    return failed;
}
```
# Lesson: Storing Text — Buffers of Lines {#text-widget}

The emulator half of the course is done: you can put a device in raw
mode, decode what comes in, and control what goes out. The remaining
lessons build the thing that *lives inside* the terminal: a text
editor. And an editor's first decision — the one everything else
leans on — is how to store the text being edited.

## The design space (worth ten minutes of your life)

**One flat buffer.** The file as a single `char*`. Reading is
trivial; inserting a character at position k means shifting
everything after k. Type at the top of a 10 MB file and every
keystroke is a 10 MB `memmove`. Fine for a config-file editor,
embarrassing beyond that.

**A gap buffer.** One flat buffer with a hole *at the cursor*:
insertions fill the hole (O(1)); moving the cursor moves the hole.
Elegant, cache-friendly, and famously what Emacs uses. Weakness:
edits far from the gap pay to relocate it, and multi-cursor /
multi-view scenarios fight over where the hole lives.

**A rope / piece table.** Trees of text chunks; edits touch O(log n)
nodes. This is what heavyweights use (VS Code: piece table; xi:
rope) because it stays fast at gigabyte scale and makes undo nearly
free (old pieces *are* the history). The price is real complexity —
balancing, iterators, invariants.

**An array of lines.** The file as `struct line*` — each line its
own heap string. Inserting a character shifts one *line's* bytes,
not the file's. Inserting a line shifts an array of pointers (8
bytes each), not text. Newline handling becomes structural instead
of textual. This is what vi's family and kilo use, it matches how an
editor *renders* (by lines!), and its worst case — one pathological
million-character line — is a case real editors handle badly too.

We take the array of lines. It's the sweet spot of
simplicity-to-capability, and its failure modes are honest.

```c
struct line {
    char *text;   /* heap-allocated, NUL-terminated       */
    int   len;    /* strlen(text) — cached, kept in sync  */
};

struct editor {
    struct line *lines;   /* growable array                */
    int nlines;
    int cap;              /* allocated slots               */
    /* cursor, viewport, file state ... (coming lessons)  */
};
```

We keep lines NUL-terminated *and* cache `len`. The NUL keeps every
`str*` function and `%s` format usable; the cached length avoids
`strlen` in every loop. The cost is one invariant to maintain:
**whoever edits `text` fixes `len`.** Centralize edits in a few
functions and the invariant holds itself.

## The four structural edits

Everything an editor does to text decomposes into four operations.
(Notice each is "memmove + bookkeeping" — the craft is in the
bookkeeping.)

**Insert a char into a line** at column c: grow the allocation by
one, shift the tail right (`len - c + 1` bytes — the `+1` drags the
NUL along), drop the char in, bump `len`.

**Delete a char from a line** at column c: shift the tail left,
shrink `len`. (Shrinking the allocation is optional; nobody does.)

**Split a line** at column c — the Enter key: the text right of c
becomes a brand-new line inserted below; the current line truncates
to c. Inserting into the middle of the *lines array* is the same
shift-right dance one level up, on `struct line` values.

**Join two lines** — Backspace at column 0, or Delete at
end-of-line: append line n+1's text onto line n, then remove slot
n+1 from the array (shift-left) and free the removed line's text —
this is the classic use-after-free / double-free hazard of the whole
structure. Join is where editor memory bugs go to be born; write it
once, carefully, and test it hard.

Delete-vs-Backspace, precisely, because every editor user's fingers
know the difference even if they've never said it aloud:

- **Delete** removes the character *at* the cursor; the cursor does
  not move. At end of line, it joins the *next* line up into this
  one.
- **Backspace** removes the character *before* the cursor; the
  cursor moves left. At column 0, it joins this line onto the
  *previous* one — and the cursor lands at the join seam (the old
  end of the previous line).

## Growth and the amortized array, reprised

The lines array grows like your abuf: double `cap` when full,
`realloc` through a temporary. One subtlety unique to this struct:
`realloc` may *move* the array, so never cache a `struct line*`
pointer across an insertion. (The bug this footnote prevents costs
an afternoon. Cheap at the price.)

Run the numbers to see why doubling matters here too: loading a
100,000-line file line-by-line with grow-by-one does ~5 billion
bytes of copying; with doubling, ~1.6 million. That's the difference
between "instant" and "why is my editor frozen".

## Challenge: The Line Buffer {#editor-widget points=30}

Build the text store. The struct (including fields future lessons
will use — initialize them to zero now) and the function set are
fixed; all storage is heap-allocated and freed by `editor_free`.

Semantics to honor exactly:

- A fresh editor holds **one empty line** — a file with zero lines
  doesn't exist in editor-land (open an empty file in vim: one `~`
  line, cursor on it).
- `editor_insert_char` inserts at (cursor_row, cursor_col) and
  advances the cursor. A `'\n'` delegates to `editor_newline`.
- `editor_newline` splits at the cursor; cursor to column 0 of the
  new line.
- `editor_delete_char` / `editor_backspace`: the Delete/Backspace
  semantics from the lesson, joins included.
- `editor_get_line` returns `""` (never NULL) out of range;
  `editor_line_len` returns 0 out of range.
- Everything must stay consistent under long random edit sequences
  — the tests hammer split/join cycles specifically.

### Starter

```c
#include <stdlib.h>
#include <string.h>

struct line {
    char *text;   /* heap, NUL-terminated */
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    /* used by later lessons — keep zeroed for now */
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

/* --- provided: allocation helpers --- */

/* a fresh heap copy of s[0..n) with a NUL */
static char *dup_n(const char *s, int n) {
    char *p = malloc((size_t)n + 1);
    if (!p) return NULL;
    memcpy(p, s, (size_t)n);
    p[n] = '\0';
    return p;
}

/* make room for one more line slot at index at (shifts tail right);
 * returns 0/-1 */
static int lines_make_gap(struct editor *e, int at) {
    if (e->nlines == e->cap) {
        int cap = e->cap ? e->cap * 2 : 8;
        struct line *nl = realloc(e->lines, (size_t)cap * sizeof(*nl));
        if (!nl) return -1;
        e->lines = nl;
        e->cap = cap;
    }
    memmove(&e->lines[at + 1], &e->lines[at],
            (size_t)(e->nlines - at) * sizeof(struct line));
    e->nlines++;
    return 0;
}

/* --- yours --- */

/* One empty line, cursor at (0,0), everything else zeroed.
 * Returns 0, or -1 on allocation failure. */
int editor_init(struct editor *e) {
    /* TODO: memset, then lines_make_gap(e, 0) + dup_n("", 0) */
    (void)e; (void)dup_n; (void)lines_make_gap;
    return -1;
}

void editor_free(struct editor *e) {
    /* TODO: free every line's text, the array, re-zero the struct */
    (void)e;
}

const char *editor_get_line(const struct editor *e, int row) {
    /* TODO: "" when out of range */
    (void)e; (void)row;
    return "";
}

int editor_line_len(const struct editor *e, int row) {
    /* TODO */
    (void)e; (void)row;
    return 0;
}

/* Split the cursor's line at the cursor; cursor -> (row+1, 0). */
void editor_newline(struct editor *e) {
    /* TODO: new line below gets the tail; current line truncates   */
    /*       (shrink len, write the NUL)                            */
    (void)e;
}

/* Insert c at the cursor, advance. '\n' -> editor_newline. */
void editor_insert_char(struct editor *e, char c) {
    /* TODO: grow the line's allocation by 1, memmove the tail      */
    /*       (including the NUL), place c, len++, cursor_col++      */
    (void)e; (void)c;
}

/* Delete AT the cursor (cursor stays). At end of line: join the
 * next line up into this one. On the last line's end: no-op. */
void editor_delete_char(struct editor *e) {
    /* TODO: mind the free() when joining                           */
    (void)e;
}

/* Delete BEFORE the cursor (cursor moves left). At column 0: join
 * this line onto the previous; cursor lands at the seam. On (0,0):
 * no-op. */
void editor_backspace(struct editor *e) {
    /* TODO */
    (void)e;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

int editor_init(struct editor *e);
void editor_free(struct editor *e);
const char *editor_get_line(const struct editor *e, int row);
int editor_line_len(const struct editor *e, int row);
void editor_newline(struct editor *e);
void editor_insert_char(struct editor *e, char c);
void editor_delete_char(struct editor *e);
void editor_backspace(struct editor *e);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void type_str(struct editor *e, const char *s) {
    for (; *s; s++)
        editor_insert_char(e, *s);
}

int main(void) {
    struct editor e;

    /* fresh editor: one empty line */
    check(editor_init(&e) == 0, "test_init_ok");
    check(e.nlines == 1 && e.cursor_row == 0 && e.cursor_col == 0,
          "test_init_one_empty_line");
    check(strcmp(editor_get_line(&e, 0), "") == 0 &&
          editor_line_len(&e, 0) == 0,
          "test_init_line_empty");

    /* out of range accessors are safe */
    check(strcmp(editor_get_line(&e, 5), "") == 0 &&
          editor_get_line(&e, -1) != NULL &&
          editor_line_len(&e, 99) == 0,
          "test_out_of_range_safe");

    /* typing */
    type_str(&e, "hello");
    check(strcmp(editor_get_line(&e, 0), "hello") == 0 &&
          e.cursor_col == 5 && editor_line_len(&e, 0) == 5,
          "test_typing");

    /* insert in the middle */
    e.cursor_col = 2;
    editor_insert_char(&e, 'X');
    check(strcmp(editor_get_line(&e, 0), "heXllo") == 0 &&
          e.cursor_col == 3,
          "test_insert_middle");

    /* newline splits */
    editor_newline(&e);
    check(e.nlines == 2 && e.cursor_row == 1 && e.cursor_col == 0,
          "test_newline_cursor");
    check(strcmp(editor_get_line(&e, 0), "heX") == 0 &&
          strcmp(editor_get_line(&e, 1), "llo") == 0,
          "test_newline_split");

    /* '\n' through insert_char behaves the same */
    e.cursor_col = 3;
    editor_insert_char(&e, '\n');
    check(e.nlines == 3 && strcmp(editor_get_line(&e, 1), "llo") == 0 &&
          strcmp(editor_get_line(&e, 2), "") == 0,
          "test_insert_newline_delegates");

    /* backspace at column 0 joins to the previous line, cursor at seam */
    editor_backspace(&e);
    check(e.nlines == 2 && e.cursor_row == 1 && e.cursor_col == 3 &&
          strcmp(editor_get_line(&e, 1), "llo") == 0,
          "test_backspace_joins");

    /* backspace mid-line */
    e.cursor_col = 2;
    editor_backspace(&e);
    check(strcmp(editor_get_line(&e, 1), "lo") == 0 && e.cursor_col == 1,
          "test_backspace_midline");

    /* backspace at (0,0) is a no-op */
    e.cursor_row = 0;
    e.cursor_col = 0;
    editor_backspace(&e);
    check(e.nlines == 2 && strcmp(editor_get_line(&e, 0), "heX") == 0,
          "test_backspace_origin_noop");

    /* delete at cursor, cursor stays */
    e.cursor_col = 1;
    editor_delete_char(&e);
    check(strcmp(editor_get_line(&e, 0), "hX") == 0 && e.cursor_col == 1,
          "test_delete_at_cursor");

    /* delete at end of line joins the NEXT line up */
    e.cursor_col = 2;
    editor_delete_char(&e);
    check(e.nlines == 1 && strcmp(editor_get_line(&e, 0), "hXlo") == 0 &&
          e.cursor_row == 0 && e.cursor_col == 2,
          "test_delete_joins_next");

    /* delete at the very end of the buffer: no-op */
    e.cursor_col = editor_line_len(&e, 0);
    editor_delete_char(&e);
    check(e.nlines == 1 && strcmp(editor_get_line(&e, 0), "hXlo") == 0,
          "test_delete_at_buffer_end_noop");

    editor_free(&e);

    /* build a multi-line document and take it apart again */
    editor_init(&e);
    type_str(&e, "one\ntwo\nthree");
    check(e.nlines == 3 &&
          strcmp(editor_get_line(&e, 0), "one") == 0 &&
          strcmp(editor_get_line(&e, 1), "two") == 0 &&
          strcmp(editor_get_line(&e, 2), "three") == 0,
          "test_multiline_build");
    /* join everything back into one line with backspaces at col 0 */
    e.cursor_row = 2; e.cursor_col = 0;
    editor_backspace(&e);
    e.cursor_row = 1; e.cursor_col = 0;
    editor_backspace(&e);
    check(e.nlines == 1 &&
          strcmp(editor_get_line(&e, 0), "onetwothree") == 0,
          "test_joins_reassemble");
    editor_free(&e);

    /* split/join hammering: 500 rounds must stay consistent */
    {
        editor_init(&e);
        type_str(&e, "abcdefghij");
        int ok = 1;
        for (int i = 0; i < 500; i++) {
            e.cursor_row = 0;
            e.cursor_col = 5;
            editor_newline(&e);          /* split           */
            if (e.nlines != 2) ok = 0;
            editor_backspace(&e);        /* join right back */
            if (e.nlines != 1 || e.cursor_col != 5) ok = 0;
            if (strcmp(editor_get_line(&e, 0), "abcdefghij") != 0) ok = 0;
            if (!ok) break;
        }
        check(ok, "test_split_join_hammer");
        editor_free(&e);
    }

    /* many lines force the array to grow */
    {
        editor_init(&e);
        for (int i = 0; i < 2000; i++) {
            editor_insert_char(&e, 'a' + (char)(i % 26));
            editor_newline(&e);
        }
        check(e.nlines == 2001, "test_grows_to_2001_lines");
        check(editor_get_line(&e, 0)[0] == 'a' &&
              editor_get_line(&e, 25)[0] == 'z' &&
              editor_get_line(&e, 26)[0] == 'a',
              "test_growth_content_intact");
        editor_free(&e);
        check(e.lines == NULL && e.nlines == 0, "test_free_resets");
        editor_free(&e); /* double free safe */
        check(1, "test_double_free_safe");
    }

    return failed;
}
```
# Lesson: Cursor Movement and the Goal Column {#cursor-rules}

Cursor movement sounds like `row += dr; col += dc`. Open vim, move
around for ten seconds, and count the rules that little formula
misses. Movement is *policy*, refined over fifty years into
conventions your fingers already know — which means getting them
wrong is instantly, viscerally noticeable.

## The clamping rules

**Where may the cursor be?** On line `r`, valid columns run from 0
to `len(r)` — **inclusive**. One past the last character is a legal
and essential position: it's where you stand to append to a line.
(Rendering-wise the cursor sits on the space after the text.) But
`len + 5` is not legal, and here's the classic case that generates
it: cursor at column 40 of a long line, press Down onto a 10-column
line. Keep the raw column and the cursor floats in the void; every
editor instead **snaps the column** to the shorter line's length.

**Vertical motion** clamps the row to `[0, nlines-1]`, then snaps
the column as above.

**Horizontal motion wraps.** Left at column 0 moves to the *end of
the previous line*; Right at end-of-line moves to *column 0 of the
next line*. (At the very start/end of the buffer: no-op.) This is
how arrow keys traverse a file as one continuous string of text
instead of trapping you inside a line.

**Home/End** go to column 0 / column `len`. **Page Up/Down** move by
a screenful of rows — the viewport height, which is why the editor
struct carries `screen_rows` — clamping at the ends like any
vertical move.

## The goal column — the rule everyone feels, nobody names

Snap-to-length has a follow-up problem. Cursor at column 40, press
Down through these lines:

```
a very long line of text with the cursor out here somewhere
short
another very long line of text
```

Snapping alone puts you at column 5 on `short` — fine — but then at
column 5 on the long line below. You *feel* this bug instantly:
"I was at column 40; passing a short line shouldn't reset my lane."

Editors therefore remember a **goal column** (Emacs calls it exactly
that): the column you *want*, as distinct from the column you've
got.

- **Vertical moves** never change the goal; they set the actual
  column to `min(goal, len(new line))`.
- **Horizontal moves and edits** set the goal to wherever they put
  the cursor.

With the goal at 40: Down onto `short` shows column 5, Down again
restores column 40. Two fields, three lines of discipline, and
vertical motion feels *right*.

## Where does this logic live?

Not in the key handler. The dispatch layer (a later lesson) will map
`KEY_ARROW_UP` to `editor_move(e, MOVE_UP)` — one call, no logic.
All policy concentrates in one function with one switch, where every
rule is visible next to its siblings and testable without a
keyboard. The alternative — clamps sprinkled through the
key handler, the mouse handler, the search-jump code — is how
editors end up with the cursor in the void when you arrive at a line
by a path nobody tested.

## Challenge: Movement Rules {#goal-column points=20}

Implement `editor_move(e, m)` for the eight motions, with all of the
lesson's policy: inclusive end-of-line positions, vertical snap,
goal-column memory, horizontal wrap, page moves by `screen_rows`,
and total clamping (no motion may ever leave the cursor out of
bounds, no matter the sequence).

Contract details the tests pin down:

- `MOVE_LEFT` at (0,0) and `MOVE_RIGHT` at the last position of the
  last line: no-ops.
- `MOVE_UP` on row 0 and `MOVE_DOWN` on the last row: no vertical
  motion, and the column also stays put (vim behavior).
- Horizontal motions (LEFT, RIGHT, HOME, END) set `goal_col` to the
  resulting column; vertical motions (UP, DOWN, PAGE_*) leave
  `goal_col` alone and set `cursor_col = min(goal_col, line len)`.

### Starter

```c
#include <stdlib.h>
#include <string.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

/* --- provided: a minimal core from the previous challenge --- */

int editor_init(struct editor *e) {
    memset(e, 0, sizeof(*e));
    e->lines = malloc(8 * sizeof(struct line));
    if (!e->lines) return -1;
    e->cap = 8;
    e->lines[0].text = calloc(1, 1);
    if (!e->lines[0].text) return -1;
    e->lines[0].len = 0;
    e->nlines = 1;
    e->screen_rows = 24;
    e->screen_cols = 80;
    return 0;
}

void editor_free(struct editor *e) {
    for (int i = 0; i < e->nlines; i++)
        free(e->lines[i].text);
    free(e->lines);
    memset(e, 0, sizeof(*e));
}

int editor_line_len(const struct editor *e, int row) {
    if (row < 0 || row >= e->nlines) return 0;
    return e->lines[row].len;
}

/* append a line to the document (test scaffolding) */
int editor_append_line(struct editor *e, const char *s) {
    if (e->nlines == e->cap) {
        struct line *nl = realloc(e->lines,
                                  (size_t)e->cap * 2 * sizeof(*nl));
        if (!nl) return -1;
        e->lines = nl;
        e->cap *= 2;
    }
    int n = (int)strlen(s);
    char *copy = malloc((size_t)n + 1);
    if (!copy) return -1;
    memcpy(copy, s, (size_t)n + 1);
    /* an empty fresh document's single empty line gets replaced */
    if (e->nlines == 1 && e->lines[0].len == 0) {
        free(e->lines[0].text);
        e->lines[0].text = copy;
        e->lines[0].len = n;
    } else {
        e->lines[e->nlines].text = copy;
        e->lines[e->nlines].len = n;
        e->nlines++;
    }
    return 0;
}

/* --- yours --- */

enum editor_move_dir {
    MOVE_UP, MOVE_DOWN, MOVE_LEFT, MOVE_RIGHT,
    MOVE_HOME, MOVE_END, MOVE_PAGE_UP, MOVE_PAGE_DOWN,
};

void editor_move(struct editor *e, enum editor_move_dir m) {
    /* TODO: UP/DOWN: clamp row (no-op at the edge, column frozen), */
    /*       then cursor_col = min(goal_col, new line's len)        */
    /* TODO: PAGE_UP/PAGE_DOWN: same but +- screen_rows, clamped    */
    /* TODO: LEFT: col-1, or wrap to end of previous line           */
    /* TODO: RIGHT: col+1 (up to len INCLUSIVE), or wrap to (row+1, 0) */
    /* TODO: HOME/END: column 0 / len                               */
    /* TODO: horizontal motions update goal_col; vertical don't     */
    (void)e; (void)m;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

enum editor_move_dir {
    MOVE_UP, MOVE_DOWN, MOVE_LEFT, MOVE_RIGHT,
    MOVE_HOME, MOVE_END, MOVE_PAGE_UP, MOVE_PAGE_DOWN,
};

int editor_init(struct editor *e);
void editor_free(struct editor *e);
int editor_line_len(const struct editor *e, int row);
int editor_append_line(struct editor *e, const char *s);
void editor_move(struct editor *e, enum editor_move_dir m);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void at(struct editor *e, int row, int col) {
    e->cursor_row = row;
    e->cursor_col = col;
    e->goal_col = col;
}

int main(void) {
    struct editor e;
    editor_init(&e);
    editor_append_line(&e, "a very long line of text 0123456789");  /* len 35 */
    editor_append_line(&e, "short");                                 /* len 5  */
    editor_append_line(&e, "another long line 0123456789012345");    /* len 34 */
    editor_append_line(&e, "");                                      /* len 0  */
    editor_append_line(&e, "last line here");                        /* len 14 */
    e.screen_rows = 3;

    /* basic vertical move */
    at(&e, 0, 2);
    editor_move(&e, MOVE_DOWN);
    check(e.cursor_row == 1 && e.cursor_col == 2, "test_down_basic");
    editor_move(&e, MOVE_UP);
    check(e.cursor_row == 0 && e.cursor_col == 2, "test_up_basic");

    /* vertical snap to shorter line */
    at(&e, 0, 30);
    editor_move(&e, MOVE_DOWN);
    check(e.cursor_row == 1 && e.cursor_col == 5, "test_down_snaps");

    /* ...and the goal column restores on the next long line */
    editor_move(&e, MOVE_DOWN);
    check(e.cursor_row == 2 && e.cursor_col == 30, "test_goal_restores");

    /* through an empty line, still remembered */
    editor_move(&e, MOVE_DOWN);
    check(e.cursor_row == 3 && e.cursor_col == 0, "test_empty_line_col0");
    editor_move(&e, MOVE_DOWN);
    check(e.cursor_row == 4 && e.cursor_col == 14, "test_goal_after_empty");

    /* horizontal motion rewrites the goal */
    at(&e, 0, 30);
    editor_move(&e, MOVE_LEFT);
    check(e.cursor_col == 29 && e.goal_col == 29, "test_left_sets_goal");
    editor_move(&e, MOVE_DOWN);
    editor_move(&e, MOVE_DOWN);
    check(e.cursor_row == 2 && e.cursor_col == 29, "test_new_goal_used");

    /* end-of-line is a legal position (len, inclusive) */
    at(&e, 1, 5);          /* end of "short" */
    editor_move(&e, MOVE_RIGHT);
    check(e.cursor_row == 2 && e.cursor_col == 0, "test_right_wraps");

    /* left at column 0 wraps to the previous line's end */
    at(&e, 2, 0);
    editor_move(&e, MOVE_LEFT);
    check(e.cursor_row == 1 && e.cursor_col == 5, "test_left_wraps");

    /* buffer edges are hard stops */
    at(&e, 0, 0);
    editor_move(&e, MOVE_LEFT);
    check(e.cursor_row == 0 && e.cursor_col == 0, "test_left_at_origin");
    editor_move(&e, MOVE_UP);
    check(e.cursor_row == 0 && e.cursor_col == 0, "test_up_at_top");
    at(&e, 4, 14);
    editor_move(&e, MOVE_RIGHT);
    check(e.cursor_row == 4 && e.cursor_col == 14, "test_right_at_end");
    editor_move(&e, MOVE_DOWN);
    check(e.cursor_row == 4, "test_down_at_bottom");

    /* home / end */
    at(&e, 0, 17);
    editor_move(&e, MOVE_HOME);
    check(e.cursor_col == 0 && e.goal_col == 0, "test_home");
    editor_move(&e, MOVE_END);
    check(e.cursor_col == 35 && e.goal_col == 35, "test_end");

    /* page moves by screen_rows (3), clamped */
    at(&e, 0, 1);
    editor_move(&e, MOVE_PAGE_DOWN);
    check(e.cursor_row == 3, "test_page_down");
    editor_move(&e, MOVE_PAGE_DOWN);
    check(e.cursor_row == 4, "test_page_down_clamps");
    editor_move(&e, MOVE_PAGE_UP);
    check(e.cursor_row == 1, "test_page_up");
    editor_move(&e, MOVE_PAGE_UP);
    check(e.cursor_row == 0, "test_page_up_clamps");

    /* page moves respect the goal column */
    at(&e, 0, 30);
    editor_move(&e, MOVE_PAGE_DOWN);       /* to row 3, empty line */
    check(e.cursor_col == 0, "test_page_snap");
    editor_move(&e, MOVE_PAGE_UP);         /* back to row 0 */
    check(e.cursor_col == 30, "test_page_goal_restores");

    /* random walks never leave the valid region */
    {
        unsigned x = 7;
        int ok = 1;
        for (int i = 0; i < 5000; i++) {
            x = x * 1103515245 + 12345;
            editor_move(&e, (enum editor_move_dir)((x >> 16) % 8));
            if (e.cursor_row < 0 || e.cursor_row >= e.nlines) { ok = 0; break; }
            if (e.cursor_col < 0 ||
                e.cursor_col > editor_line_len(&e, e.cursor_row)) { ok = 0; break; }
        }
        check(ok, "test_random_walk_stays_in_bounds");
    }

    editor_free(&e);
    return failed;
}
```
# Lesson: The Viewport {#viewport}

Your terminal shows 24 rows; your file has 10,000 lines. The editor's
window onto the file is the **viewport**, and its entire state is two
numbers:

```c
int rowoff;   /* index of the first VISIBLE file row    */
int coloff;   /* index of the first VISIBLE file column */
```

File row `r` appears on screen row `r - rowoff` (when `0 <= r -
rowoff < screen_rows`); file column `c` appears on screen column
`c - coloff`. That translation — file coordinates minus offset —
is the whole theory. Everything else is deciding when the offsets
change.

## Scroll-to-fit: the invariant

The editor never scrolls *speculatively*; it scrolls to maintain one
invariant: **the cursor is always visible.** Derive the vertical
rule from the inequality itself. Visibility means:

```
rowoff <= cursor_row < rowoff + screen_rows
```

Two ways to violate it, two one-line repairs:

- Cursor above the window (`cursor_row < rowoff`) — you pressed Up
  at the top edge: slide the window up to put the cursor on the
  first row: `rowoff = cursor_row`.
- Cursor below the window (`cursor_row >= rowoff + screen_rows`) —
  Down at the bottom edge: slide the window down to put the cursor
  on the *last* row: `rowoff = cursor_row - screen_rows + 1`.

Check the fencepost in that second line with a concrete case (always
check fenceposts with concrete cases): 10 screen rows, cursor lands
on file row 10 — one past visible rows 0–9. New `rowoff = 10 - 10 +
1 = 1`, visible rows 1–10, cursor on the last screen row. Off by one
in either direction and you'll either scroll a row early or leave
the cursor hanging one row off-screen.

The horizontal axis is the *same two lines* with `cursor_col`,
`coloff`, `screen_cols` — long lines scroll sideways as the cursor
pushes past the edges. (This is how nano and kilo handle long lines;
vim wraps them by default instead. Wrapping is a much deeper change
— one file line becomes several screen rows and the row translation
stops being subtraction — which is exactly why we chose sideways
scrolling.)

Where does this run? Once per frame, *after* input handling and
*before* rendering — one `editor_scroll(e)` that enforces the
invariant no matter who moved the cursor or how (keys, search jump,
file load). Nobody else touches the offsets; one writer, one
invariant, no drift.

## Tabs, or: the cursor is not where the bytes say

There's a second coordinate subtlety hiding in rendering: the
**byte** column and the **screen** column disagree the moment a line
contains a tab. `\tx` is two bytes, but on an 8-column tab display
the `x` sits at screen column 8, not 1.

So an editor tracks two notions (kilo calls them `cx` and `rx`):

- `cursor_col` — index into the line's *bytes*. Editing operates
  here.
- render column — where that byte lands on *screen*. The terminal
  cursor gets positioned here, and horizontal scrolling must be
  computed here (it's visual overflow, not byte overflow).

The conversion walks the line's prefix, expanding each tab to the
next multiple of the tab width:

```c
rx = 0;
for each byte before cursor_col:
    if tab: rx += TABSTOP - (rx % TABSTOP);   /* jump to next stop */
    else:   rx += 1;
```

Note `rx % TABSTOP`, not a fixed 8: a tab consumes *up to* 8
columns, less if text precedes it — `ab\t` puts the next glyph at
column 8, not 10. The reverse mapping (screen column → byte column,
needed when a mouse click or a search highlight gives you a visual
position) runs the same walk until it meets or passes the target.

## Challenge: Scroll to Keep the Cursor Visible {#scrolling points=20}

Implement `editor_scroll(e)`: enforce both axes of the invariant,
exactly as derived — including never letting the offsets go
negative, and treating the cursor's *render* column (provided
helper) as the thing that must be horizontally visible. The tests
walk a cursor around a large document and check the offsets at every
boundary fencepost.

### Starter

```c
#include <stdlib.h>
#include <string.h>

#define TABSTOP 8

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

/* --- provided --- */

int editor_init(struct editor *e) {
    memset(e, 0, sizeof(*e));
    e->lines = malloc(8 * sizeof(struct line));
    if (!e->lines) return -1;
    e->cap = 8;
    e->lines[0].text = calloc(1, 1);
    if (!e->lines[0].text) return -1;
    e->lines[0].len = 0;
    e->nlines = 1;
    e->screen_rows = 24;
    e->screen_cols = 80;
    return 0;
}

void editor_free(struct editor *e) {
    for (int i = 0; i < e->nlines; i++)
        free(e->lines[i].text);
    free(e->lines);
    memset(e, 0, sizeof(*e));
}

int editor_append_line(struct editor *e, const char *s) {
    if (e->nlines == e->cap) {
        struct line *nl = realloc(e->lines,
                                  (size_t)e->cap * 2 * sizeof(*nl));
        if (!nl) return -1;
        e->lines = nl;
        e->cap *= 2;
    }
    int n = (int)strlen(s);
    char *copy = malloc((size_t)n + 1);
    if (!copy) return -1;
    memcpy(copy, s, (size_t)n + 1);
    if (e->nlines == 1 && e->lines[0].len == 0) {
        free(e->lines[0].text);
        e->lines[0].text = copy;
        e->lines[0].len = n;
    } else {
        e->lines[e->nlines].text = copy;
        e->lines[e->nlines].len = n;
        e->nlines++;
    }
    return 0;
}

/* byte column -> render column (you build this in the next
 * challenge; a working version is provided here so the two
 * challenges are independent) */
int editor_cx_to_rx(const struct line *ln, int cx) {
    int rx = 0;
    for (int i = 0; i < cx && i < ln->len; i++) {
        if (ln->text[i] == '\t')
            rx += TABSTOP - (rx % TABSTOP);
        else
            rx++;
    }
    return rx;
}

/* --- yours --- */

/* Enforce: cursor visible on both axes.
 *   rowoff <= cursor_row < rowoff + screen_rows
 *   coloff <= rx         < coloff + screen_cols
 * where rx = editor_cx_to_rx(current line, cursor_col).
 * Offsets never go negative. */
void editor_scroll(struct editor *e) {
    /* TODO: vertical: the two repairs from the lesson  */
    /* TODO: horizontal: same shape, using rx           */
    (void)e;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

#define TABSTOP 8

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

int editor_init(struct editor *e);
void editor_free(struct editor *e);
int editor_append_line(struct editor *e, const char *s);
int editor_cx_to_rx(const struct line *ln, int cx);
void editor_scroll(struct editor *e);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main(void) {
    struct editor e;
    editor_init(&e);
    /* 100 lines; line i is i+1 copies of 'x' (line 99 is 100 wide) */
    for (int i = 0; i < 100; i++) {
        char buf[128];
        memset(buf, 'x', (size_t)i + 1);
        buf[i + 1] = '\0';
        editor_append_line(&e, buf);
    }
    e.screen_rows = 10;
    e.screen_cols = 20;

    /* within the window: nothing moves */
    e.cursor_row = 5; e.cursor_col = 0;
    editor_scroll(&e);
    check(e.rowoff == 0 && e.coloff == 0, "test_no_scroll_needed");

    /* last visible row (9) is still inside */
    e.cursor_row = 9;
    editor_scroll(&e);
    check(e.rowoff == 0, "test_boundary_no_scroll");

    /* one past: scroll by exactly one */
    e.cursor_row = 10;
    editor_scroll(&e);
    check(e.rowoff == 1, "test_scroll_down_one");

    /* far jump down: cursor lands on the LAST screen row */
    e.cursor_row = 50;
    editor_scroll(&e);
    check(e.rowoff == 41, "test_jump_down");

    /* moving back up inside the window: offsets stay */
    e.cursor_row = 45;
    editor_scroll(&e);
    check(e.rowoff == 41, "test_up_within_window");

    /* above the window: window slides to put cursor on top row */
    e.cursor_row = 30;
    editor_scroll(&e);
    check(e.rowoff == 30, "test_scroll_up");

    /* back to the top */
    e.cursor_row = 0;
    editor_scroll(&e);
    check(e.rowoff == 0, "test_back_to_top");

    /* horizontal: cursor at col 25 of a long line (screen is 20) */
    e.cursor_row = 50;                    /* len 51 */
    e.cursor_col = 25;
    editor_scroll(&e);
    check(e.coloff == 25 - 20 + 1, "test_scroll_right");

    /* back left inside the window */
    e.cursor_col = 10;
    editor_scroll(&e);
    check(e.coloff == 6, "test_left_within_window");
    e.cursor_col = 3;
    editor_scroll(&e);
    check(e.coloff == 3, "test_scroll_left");
    e.cursor_col = 0;
    editor_scroll(&e);
    check(e.coloff == 0, "test_left_edge");

    /* horizontal scrolling is computed on RENDER columns */
    editor_free(&e);
    editor_init(&e);
    editor_append_line(&e, "\t\t\tabcdef");  /* rx of col 3 is 24 */
    e.screen_rows = 10;
    e.screen_cols = 20;
    e.cursor_row = 0;
    e.cursor_col = 3;                     /* after three tabs */
    editor_scroll(&e);
    check(e.coloff == 24 - 20 + 1, "test_scroll_uses_render_col");

    editor_free(&e);
    return failed;
}
```

## Challenge: Tabs and Render Columns {#render-cols points=15}

The two coordinate conversions, this time yours:

- `editor_cx_to_rx(line, cx)` — byte column → screen column, tabs
  expanding to the next multiple of `TABSTOP` (8).
- `editor_rx_to_cx(line, rx)` — screen column → byte column: walk
  until the render position *meets or passes* `rx`; if `rx` lies
  past the end of the line, return the line length.

### Starter

```c
#include <string.h>

#define TABSTOP 8

struct line {
    char *text;   /* not necessarily NUL-terminated here — use len */
    int   len;
};

/* byte column cx -> render (screen) column */
int editor_cx_to_rx(const struct line *ln, int cx) {
    /* TODO: walk bytes 0..cx-1, tabs jump to the next multiple of
     *       TABSTOP: rx += TABSTOP - (rx % TABSTOP) */
    (void)ln; (void)cx;
    return 0;
}

/* render column rx -> byte column */
int editor_rx_to_cx(const struct line *ln, int rx) {
    /* TODO: walk forward accumulating render width; return the
     *       first byte index whose render position >= rx; if the
     *       whole line is narrower than rx, return ln->len */
    (void)ln; (void)rx;
    return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

#define TABSTOP 8

struct line {
    char *text;
    int   len;
};

int editor_cx_to_rx(const struct line *ln, int cx);
int editor_rx_to_cx(const struct line *ln, int rx);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static struct line mk(const char *s) {
    struct line ln = { (char *)s, (int)strlen(s) };
    return ln;
}

int main(void) {
    /* no tabs: identity */
    struct line ln = mk("hello world");
    check(editor_cx_to_rx(&ln, 0) == 0, "test_plain_zero");
    check(editor_cx_to_rx(&ln, 5) == 5, "test_plain_identity");
    check(editor_rx_to_cx(&ln, 5) == 5, "test_plain_reverse");

    /* leading tab */
    ln = mk("\tabc");
    check(editor_cx_to_rx(&ln, 1) == 8, "test_tab_expands");
    check(editor_cx_to_rx(&ln, 2) == 9, "test_after_tab");
    check(editor_cx_to_rx(&ln, 4) == 11, "test_tab_line_end");

    /* tab after text jumps to the NEXT stop, not +8 */
    ln = mk("ab\tcd");
    check(editor_cx_to_rx(&ln, 2) == 2, "test_before_mid_tab");
    check(editor_cx_to_rx(&ln, 3) == 8, "test_mid_tab_next_stop");
    check(editor_cx_to_rx(&ln, 5) == 10, "test_after_mid_tab");

    /* tab exactly at a stop consumes a full TABSTOP */
    ln = mk("12345678\tx");
    check(editor_cx_to_rx(&ln, 8) == 8, "test_at_stop_before");
    check(editor_cx_to_rx(&ln, 9) == 16, "test_at_stop_full_jump");

    /* consecutive tabs */
    ln = mk("\t\t\tabcdef");
    check(editor_cx_to_rx(&ln, 3) == 24, "test_three_tabs");
    check(editor_cx_to_rx(&ln, 9) == 30, "test_three_tabs_text");

    /* reverse mapping */
    ln = mk("ab\tcd");
    check(editor_rx_to_cx(&ln, 0) == 0, "test_rx0");
    check(editor_rx_to_cx(&ln, 2) == 2, "test_rx_at_tab");
    /* screen columns 3..7 are inside the tab; landing anywhere in
     * it resolves to the tab's byte... or the next byte once the
     * accumulated width reaches the target */
    check(editor_rx_to_cx(&ln, 8) == 3, "test_rx_past_tab");
    check(editor_rx_to_cx(&ln, 9) == 4, "test_rx_after_tab");
    /* beyond the line: clamp to len */
    check(editor_rx_to_cx(&ln, 100) == 5, "test_rx_clamps");

    /* cx_to_rx never reads past len even if cx is too big */
    ln = mk("abc");
    check(editor_cx_to_rx(&ln, 50) == 3, "test_cx_clamps");

    /* roundtrip on a gnarly line */
    ln = mk("\ta\tbb\tccc\t");
    int ok = 1;
    for (int cx = 0; cx <= ln.len; cx++) {
        int rx = editor_cx_to_rx(&ln, cx);
        if (editor_rx_to_cx(&ln, rx) != cx) ok = 0;
    }
    check(ok, "test_roundtrip");

    return failed;
}
```
# Lesson: Loading and Saving Files {#file-io}

An editor that can't open and save files is a very elaborate toy.
This lesson is short on new syscalls — you know `open`, `read`,
`write` — and long on the judgment calls, because file I/O is where
an editor can *destroy the user's data*, and the difference between
"editor" and "data shredder" is a handful of decisions made
correctly.

## Loading: bytes → lines

Loading is a decode step: the file is a flat byte string; your
buffer is an array of lines. Split on `'\n'`, with three
conventions:

- **The trailing newline belongs to the format, not the text.** A
  well-formed Unix text file *ends* with `\n` ("a\nb\n" is the
  two-line file). Naively splitting on every `\n` yields a phantom
  third empty line; strip the terminator instead of storing it.
- **Tolerate a missing final newline.** "a\nb" is technically
  malformed but everywhere; load it as two lines. (Then fix it on
  save — see below.)
- **Tolerate CRLF.** Files that crossed a Windows machine end lines
  with `\r\n`; strip the `\r` too, or every line of the file wears
  an invisible last character that makes end-of-line navigation
  feel haunted.

A file the OS says doesn't exist (`ENOENT`) is not an error for an
editor: `vim newfile.txt` opens an empty buffer and creates the file
on first save. Any *other* open failure (`EACCES`, `EISDIR`…) is a
real error to report.

## Saving: the decision that matters

Serializing is trivial — every line, `'\n'` after each, done
(quietly repairing any missing final newline). The question is *how
the bytes reach the disk*. The obvious way:

```c
fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
write(fd, everything, len);
```

Read it as a timeline and see the trap: `O_TRUNC` destroys the old
contents **at open**, before one byte of the new contents is
written. Crash between those steps — power loss, OOM-kill, a bug in
your own editor — and the user's file is now zero bytes. Their old
data is gone *and* their new data is gone. Editors have shipped
this; users remember.

The professional pattern is **write-temp-then-rename**:

```c
fd = open("file.txt.tmp", O_WRONLY | O_CREAT | O_TRUNC, 0644);
write_all(fd, everything, len);   /* your helper from lesson 1 */
close(fd);
rename("file.txt.tmp", "file.txt");
```

The load-bearing fact is that POSIX `rename(2)` is **atomic**: at
every instant, `file.txt` is either entirely the old file or
entirely the new one. There is no moment when it's empty or half
written. A crash before the rename leaves the old file untouched
(plus a stray `.tmp` to clean up); after, the new file is complete.
This one idiom protects more user data than any amount of testing.

(Going further — `fsync` before the rename to survive power loss
with certainty, preserving ownership/permissions of the original,
keeping the temp file on the same filesystem so rename stays atomic
— is real and matters for production editors; the shape stays
exactly this.)

One more piece of editor state rides along with saving: the
**dirty flag** — set by every edit, cleared by a successful save,
consulted by "quit without saving?". Wire it now: `editor_open` and
`editor_save` both end with `e->dirty = 0`.

## Challenge: Load and Save {#load-save points=25}

- `editor_open(e, path)` — load `path` into an initialized editor,
  replacing its contents. Missing file: succeed with one empty
  line. Strip `\n` terminators and any preceding `\r`. Copy `path`
  into `e->filename`. Clear `dirty`. Return 0; -1 on real errors
  (in which case the editor must still be in a valid state).
- `editor_save(e, path)` — write all lines, `'\n'` after each,
  via a temp file (`"<path>.tmp"`) renamed over the target. Update
  `e->filename`, clear `dirty`. Return the number of bytes written,
  -1 on error.

The tests round-trip files through a real filesystem, cover the
trailing-newline and CRLF conventions, verify the temp file doesn't
linger, and — the tell for `O_TRUNC`-style saving — check that
saving over an existing file replaces it completely.

### Starter

```c
#define _POSIX_C_SOURCE 200809L
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <errno.h>
#include <unistd.h>
#include <fcntl.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

/* --- provided --- */

int editor_init(struct editor *e) {
    memset(e, 0, sizeof(*e));
    e->lines = malloc(8 * sizeof(struct line));
    if (!e->lines) return -1;
    e->cap = 8;
    e->lines[0].text = calloc(1, 1);
    if (!e->lines[0].text) return -1;
    e->lines[0].len = 0;
    e->nlines = 1;
    e->screen_rows = 24;
    e->screen_cols = 80;
    return 0;
}

void editor_free(struct editor *e) {
    for (int i = 0; i < e->nlines; i++)
        free(e->lines[i].text);
    free(e->lines);
    memset(e, 0, sizeof(*e));
}

const char *editor_get_line(const struct editor *e, int row) {
    if (row < 0 || row >= e->nlines) return "";
    return e->lines[row].text;
}

int editor_append_line(struct editor *e, const char *s, int n) {
    if (e->nlines == e->cap) {
        struct line *nl = realloc(e->lines,
                                  (size_t)e->cap * 2 * sizeof(*nl));
        if (!nl) return -1;
        e->lines = nl;
        e->cap *= 2;
    }
    char *copy = malloc((size_t)n + 1);
    if (!copy) return -1;
    memcpy(copy, s, (size_t)n);
    copy[n] = '\0';
    if (e->nlines == 1 && e->lines[0].len == 0 && e->lines[0].text[0] == '\0') {
        free(e->lines[0].text);
        e->lines[0].text = copy;
        e->lines[0].len = n;
    } else {
        e->lines[e->nlines].text = copy;
        e->lines[e->nlines].len = n;
        e->nlines++;
    }
    return 0;
}

ssize_t write_all(int fd, const void *buf, size_t n) {
    const char *p = buf;
    size_t off = 0;
    while (off < n) {
        ssize_t w = write(fd, p + off, n - off);
        if (w < 0) {
            if (errno == EINTR) continue;
            return -1;
        }
        off += (size_t)w;
    }
    return (ssize_t)n;
}

/* --- yours --- */

/* Load path into e (which is already initialized; replace its
 * contents — hint: editor_free + editor_init is the easy reset).
 * ENOENT: succeed, empty buffer. Split on '\n', dropping the
 * terminator and any '\r' before it. Copy path into e->filename.
 * dirty = 0. Return 0, or -1 on real errors. */
int editor_open(struct editor *e, const char *path) {
    /* TODO: open O_RDONLY; ENOENT -> just set filename, return 0   */
    /* TODO: read the whole file (loop read into a growing buffer,  */
    /*       or fstat for the size)                                 */
    /* TODO: walk the buffer splitting lines; watch the last line   */
    /*       with no trailing '\n'                                  */
    (void)e; (void)path;
    return -1;
}

/* Save all lines to path, '\n' after each, ATOMICALLY:
 * write "<path>.tmp", then rename over path. Update filename,
 * clear dirty. Return total bytes written, or -1 on error (and
 * try not to leave the .tmp behind). */
long editor_save(struct editor *e, const char *path) {
    /* TODO: snprintf the temp path (reject paths too long to fit)  */
    /* TODO: open(tmp, O_WRONLY|O_CREAT|O_TRUNC, 0644)              */
    /* TODO: write_all each line + "\n"; close; rename              */
    /* TODO: on any failure: close, unlink(tmp), return -1          */
    (void)e; (void)path;
    return -1;
}
```

### Tests

```c
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

int editor_init(struct editor *e);
void editor_free(struct editor *e);
const char *editor_get_line(const struct editor *e, int row);
int editor_append_line(struct editor *e, const char *s, int n);
int editor_open(struct editor *e, const char *path);
long editor_save(struct editor *e, const char *path);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void write_file(const char *path, const char *bytes, size_t n) {
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    write(fd, bytes, n);
    close(fd);
}

static long read_file(const char *path, char *out, size_t cap) {
    int fd = open(path, O_RDONLY);
    if (fd < 0) return -1;
    long total = 0;
    ssize_t r;
    while ((r = read(fd, out + total, cap - (size_t)total)) > 0)
        total += r;
    close(fd);
    out[total] = '\0';
    return total;
}

int main(void) {
    alarm(20);
    struct editor e;
    char buf[4096];

    /* well-formed file: trailing newline is format, not content */
    write_file("t_load1.txt", "alpha\nbeta\ngamma\n", 17);
    editor_init(&e);
    check(editor_open(&e, "t_load1.txt") == 0, "test_open_ok");
    check(e.nlines == 3, "test_open_line_count");
    check(strcmp(editor_get_line(&e, 0), "alpha") == 0 &&
          strcmp(editor_get_line(&e, 1), "beta") == 0 &&
          strcmp(editor_get_line(&e, 2), "gamma") == 0,
          "test_open_content");
    check(e.dirty == 0, "test_open_clears_dirty");
    check(strcmp(e.filename, "t_load1.txt") == 0, "test_open_sets_filename");
    editor_free(&e);

    /* missing trailing newline still loads both lines */
    write_file("t_load2.txt", "one\ntwo", 7);
    editor_init(&e);
    editor_open(&e, "t_load2.txt");
    check(e.nlines == 2 && strcmp(editor_get_line(&e, 1), "two") == 0,
          "test_open_no_trailing_newline");
    editor_free(&e);

    /* CRLF is stripped */
    write_file("t_load3.txt", "win\r\ndows\r\n", 11);
    editor_init(&e);
    editor_open(&e, "t_load3.txt");
    check(e.nlines == 2 &&
          strcmp(editor_get_line(&e, 0), "win") == 0 &&
          strcmp(editor_get_line(&e, 1), "dows") == 0,
          "test_open_strips_cr");
    editor_free(&e);

    /* empty file: one empty line */
    write_file("t_load4.txt", "", 0);
    editor_init(&e);
    editor_open(&e, "t_load4.txt");
    check(e.nlines == 1 && strcmp(editor_get_line(&e, 0), "") == 0,
          "test_open_empty_file");
    editor_free(&e);

    /* empty lines inside the file survive */
    write_file("t_load5.txt", "a\n\nb\n", 5);
    editor_init(&e);
    editor_open(&e, "t_load5.txt");
    check(e.nlines == 3 && strcmp(editor_get_line(&e, 1), "") == 0,
          "test_open_blank_lines");
    editor_free(&e);

    /* missing file: succeed with an empty buffer */
    unlink("t_missing.txt");
    editor_init(&e);
    check(editor_open(&e, "t_missing.txt") == 0, "test_open_missing_ok");
    check(e.nlines == 1, "test_open_missing_empty");
    editor_free(&e);

    /* save: exact bytes, trailing newline appears */
    editor_init(&e);
    editor_append_line(&e, "first", 5);
    editor_append_line(&e, "second", 6);
    e.dirty = 1;
    long n = editor_save(&e, "t_save1.txt");
    check(n == 13, "test_save_returns_bytes");   /* "first\nsecond\n" */
    check(e.dirty == 0, "test_save_clears_dirty");
    long got = read_file("t_save1.txt", buf, sizeof(buf) - 1);
    check(got == 13 && strcmp(buf, "first\nsecond\n") == 0,
          "test_save_content");
    /* the temp file must not linger */
    check(access("t_save1.txt.tmp", F_OK) != 0, "test_save_no_tmp_left");
    editor_free(&e);

    /* save over an existing longer file: fully replaced */
    write_file("t_save2.txt", "OLD OLD OLD OLD OLD OLD OLD\n", 28);
    editor_init(&e);
    editor_append_line(&e, "new", 3);
    editor_save(&e, "t_save2.txt");
    got = read_file("t_save2.txt", buf, sizeof(buf) - 1);
    check(got == 4 && strcmp(buf, "new\n") == 0,
          "test_save_replaces_completely");
    editor_free(&e);

    /* full roundtrip: open what we saved */
    editor_init(&e);
    editor_append_line(&e, "line with\ttab", 13);
    editor_append_line(&e, "", 0);
    editor_append_line(&e, "end", 3);
    editor_save(&e, "t_save3.txt");
    editor_free(&e);
    editor_init(&e);
    editor_open(&e, "t_save3.txt");
    check(e.nlines == 3 &&
          strcmp(editor_get_line(&e, 0), "line with\ttab") == 0 &&
          strcmp(editor_get_line(&e, 1), "") == 0 &&
          strcmp(editor_get_line(&e, 2), "end") == 0,
          "test_roundtrip");
    editor_free(&e);

    /* a bigger file: 1000 lines */
    editor_init(&e);
    for (int i = 0; i < 1000; i++) {
        char line[64];
        int len = snprintf(line, sizeof(line), "line number %d", i);
        editor_append_line(&e, line, len);
    }
    editor_save(&e, "t_save4.txt");
    editor_free(&e);
    editor_init(&e);
    editor_open(&e, "t_save4.txt");
    check(e.nlines == 1000 &&
          strcmp(editor_get_line(&e, 999), "line number 999") == 0,
          "test_thousand_lines");
    editor_free(&e);

    /* saving into an unwritable directory reports failure */
    editor_init(&e);
    editor_append_line(&e, "x", 1);
    check(editor_save(&e, "/no_such_dir_xyz/f.txt") == -1,
          "test_save_error_reported");
    editor_free(&e);

    return failed;
}
```
# Lesson: The Status Bar and Messages {#status-line}

Every serious full-screen tool reserves a strip of screen for
telling you where you are: vim's status line, nano's shortcut bars,
less's prompt. It's the difference between editing a file and
editing *blind*. Ours takes the bottom row of the terminal: the
text viewport gets `terminal_rows - 1` rows, the status bar gets
the last one. (That subtraction is why the editor struct's
`screen_rows` is the *viewport* height, not the terminal height —
decide once, at init, who owns which rows.)

## Inverted video

The bar must read as chrome, not content. The classic trick costs
five bytes: **reverse video** — SGR 7 swaps foreground and
background, so the bar renders as a solid contrasting stripe:

```
\x1b[7m  ...status text padded to full width...  \x1b[m
```

Two details make it look right rather than almost-right:

- **Pad to exactly the screen width.** Reverse video colors only the
  cells you actually print. Print a 30-character status on an
  80-column terminal and you get a 30-character stripe and 50 cells
  of abandoned background. The bar text must be built to *precisely*
  `screen_cols` characters — which is the actual programming problem
  here.
- **Reset afterwards** (`\x1b[m`), or the next thing drawn inherits
  the inversion. You knew that; it's still the #1 status-bar bug.

## Left, right, and the squeeze

The conventional layout puts identity left and position right:

```
report.txt [+]                                    Ln 128, Col 43
└─ filename, dirty marker                         1-based, always ─┘
```

Building it is a fixed-width layout problem in miniature:

1. Compose the left segment: filename (or `[No Name]` for a fresh
   buffer), plus a ` [+]` dirty marker when unsaved changes exist.
   (1-based Ln/Col on the right, by the way — users count from 1,
   programs from 0; the status bar is user territory.)
2. Compose the right segment.
3. If both fit in `width`, print left, `width - left - right`
   spaces, right.
4. If they don't fit, something must yield: **truncate the left
   segment** (long filenames lose their tails; a position display
   truncated to `Ln 12` is misinformation), keeping one space
   between the segments. In the pathological case where even the
   right segment alone doesn't fit, truncate it to `width` — never
   write more than `width`.

This "compose segments, then resolve the squeeze" shape recurs in
every TUI you'll ever write (tab bars, prompts, tmux's status)
— worth doing carefully once.

## Challenge: Render the Status Bar {#status-line-impl points=20}

Implement `format_status_bar(e, out, width)`: fill `out` with
**exactly `width` characters** (plus a terminating NUL) per the
lesson's layout rules. No escape sequences here — the renderer adds
the `\x1b[7m` wrapper; this function is pure fixed-width text
layout, and the tests measure it to the character.

### Starter

```c
#include <stdio.h>
#include <string.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

/* Fill out[0..width) + NUL:
 *   LEFT  = filename or "[No Name]", then " [+]" if dirty
 *   RIGHT = "Ln <row+1>, Col <col+1>"
 *   between them: spaces to reach exactly width chars
 * Squeeze: truncate LEFT (keep 1 space before RIGHT); if RIGHT
 * alone can't fit, truncate RIGHT at width. */
void format_status_bar(const struct editor *e, char *out, int width) {
    /* TODO: build left into a temp (snprintf), right into another  */
    /* TODO: the three cases: fits / squeeze left / squeeze right   */
    (void)e; (void)width;
    out[0] = '\0';
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

void format_status_bar(const struct editor *e, char *out, int width);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

int main(void) {
    struct editor e;
    char out[256];
    memset(&e, 0, sizeof(e));

    /* the standard case */
    strcpy(e.filename, "notes.txt");
    e.cursor_row = 4;
    e.cursor_col = 10;
    e.dirty = 0;
    format_status_bar(&e, out, 40);
    check((int)strlen(out) == 40, "test_exact_width");
    check(strncmp(out, "notes.txt", 9) == 0, "test_left_filename");
    check(strstr(out, "Ln 5, Col 11") != NULL, "test_right_position");
    /* right segment is flush against the right edge */
    check(strcmp(out + 40 - (int)strlen("Ln 5, Col 11"),
                 "Ln 5, Col 11") == 0,
          "test_right_flush");
    /* the middle is spaces */
    check(out[9] == ' ' && out[20] == ' ', "test_middle_padded");

    /* dirty marker */
    e.dirty = 1;
    format_status_bar(&e, out, 40);
    check(strncmp(out, "notes.txt [+]", 13) == 0, "test_dirty_marker");
    check((int)strlen(out) == 40, "test_dirty_still_exact");

    /* no filename */
    e.filename[0] = '\0';
    e.dirty = 0;
    format_status_bar(&e, out, 40);
    check(strncmp(out, "[No Name]", 9) == 0, "test_no_name");

    /* 1-based positions, larger numbers */
    strcpy(e.filename, "a.c");
    e.cursor_row = 99;
    e.cursor_col = 0;
    format_status_bar(&e, out, 30);
    check(strstr(out, "Ln 100, Col 1") != NULL, "test_one_based");
    check((int)strlen(out) == 30, "test_width_30");

    /* the squeeze: long filename yields, position survives intact */
    strcpy(e.filename,
           "a_ridiculously_long_filename_that_cannot_possibly_fit.txt");
    e.cursor_row = 9;
    e.cursor_col = 9;
    format_status_bar(&e, out, 30);
    check((int)strlen(out) == 30, "test_squeeze_exact_width");
    check(strstr(out, "Ln 10, Col 10") != NULL,
          "test_squeeze_right_survives");
    check(strcmp(out + 30 - (int)strlen("Ln 10, Col 10"),
                 "Ln 10, Col 10") == 0,
          "test_squeeze_right_flush");
    check(strncmp(out, "a_ridic", 7) == 0, "test_squeeze_left_prefix");
    /* one space between truncated left and right */
    check(out[30 - (int)strlen("Ln 10, Col 10") - 1] == ' ',
          "test_squeeze_separator_space");

    /* pathological width: right truncates, never overflows */
    format_status_bar(&e, out, 8);
    check((int)strlen(out) == 8, "test_tiny_width");
    check(strncmp(out, "Ln 10, C", 8) == 0, "test_tiny_right_truncated");

    return failed;
}
```

# Lesson: Search {#search}

A file you can't search is a file you can only scroll. The good news
after the last few lessons: search is *easy* — plain C string work,
no escape codes, no state machines. The design still deserves five
minutes, because search UX has sharp conventions.

## Find-next semantics

The operation that matters is not "find" but **find next**: given
where I am, where is the following match? Repeat-invocations then
walk match to match. Two conventions define it:

- **Start searching *at* a given position** (inclusive), and let
  the *caller* pass "cursor + one column" for repeat-search — else
  pressing find-next while standing on a match finds the same match
  forever. Keeping the +1 out of the search function keeps the
  function honest and the policy visible at the call site.
- **Wrap around.** Hitting the last match then searching again
  jumps back to the first (vim flashes "search hit BOTTOM,
  continuing at TOP"). Implement by scanning position → end of
  buffer, then start of buffer → position.

Within a line, `strstr(3)` does the byte work — find the first
occurrence of a needle in a haystack. The only wrinkle: starting
mid-line means searching the line's *suffix* (`strstr(line + col,
q)`), then reporting the match's column relative to the whole line
(add the offset back — a classic pointer-arithmetic fencepost).

Real editors layer niceties on this core — case folding,
highlight-all, incremental search that jumps as you type, regex.
All of them sit on exactly this function; incremental search, for
instance, is just re-running find-next from the search's *origin*
on every keystroke of the query.

## Challenge: Find in Buffer {#find points=20}

Implement:

```c
int editor_find_next(const struct editor *e, const char *query,
                     int from_row, int from_col,
                     int *match_row, int *match_col);
```

- Search forward from (`from_row`, `from_col`) inclusive, wrapping
  past the end of the buffer, over **all** lines — including the
  part of the starting line *before* `from_col*` on the wrapped
  pass (a match just left of the cursor must be reachable).
- On a hit: store its position, return 1. No match anywhere (or
  empty/NULL query, or an out-of-range start): return 0.
- Matches are byte-exact and case-sensitive.

### Starter

```c
#include <string.h>
#include <stdlib.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

/* Find the next occurrence of query at or after (from_row,
 * from_col), wrapping around the buffer. 1 = found (position in
 * *match_row / *match_col), 0 = no match / bad arguments. */
int editor_find_next(const struct editor *e, const char *query,
                     int from_row, int from_col,
                     int *match_row, int *match_col) {
    /* TODO: guard empty query and out-of-range start               */
    /* TODO: scan nlines+1 line-visits starting at from_row; on the */
    /*       first visit search from from_col, afterwards from 0    */
    /*       (the +1 revisits the start line for its early columns) */
    /* TODO: strstr on the line suffix; column = hit - line start   */
    (void)e; (void)query; (void)from_row; (void)from_col;
    (void)match_row; (void)match_col;
    return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

int editor_find_next(const struct editor *e, const char *query,
                     int from_row, int from_col,
                     int *match_row, int *match_col);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static struct editor make_doc(const char **lines, int n) {
    struct editor e;
    memset(&e, 0, sizeof(e));
    e.lines = malloc((size_t)n * sizeof(struct line));
    e.cap = n;
    e.nlines = n;
    for (int i = 0; i < n; i++) {
        size_t len = strlen(lines[i]);
        e.lines[i].text = malloc(len + 1);
        memcpy(e.lines[i].text, lines[i], len + 1);
        e.lines[i].len = (int)len;
    }
    return e;
}

static void free_doc(struct editor *e) {
    for (int i = 0; i < e->nlines; i++)
        free(e->lines[i].text);
    free(e->lines);
}

int main(void) {
    const char *doc[] = {
        "the quick brown fox",       /* 0 */
        "jumps over the lazy dog",   /* 1 */
        "the end",                   /* 2 */
        "",                          /* 3 */
        "fox fox fox",               /* 4 */
    };
    struct editor e = make_doc(doc, 5);
    int r, c;

    /* simple forward find */
    check(editor_find_next(&e, "quick", 0, 0, &r, &c) == 1 &&
          r == 0 && c == 4,
          "test_basic_find");

    /* inclusive start: a match AT the start position is found */
    check(editor_find_next(&e, "quick", 0, 4, &r, &c) == 1 &&
          r == 0 && c == 4,
          "test_inclusive_start");

    /* starting one past it finds the next occurrence elsewhere */
    check(editor_find_next(&e, "the", 0, 1, &r, &c) == 1 &&
          r == 1 && c == 11,
          "test_find_next_line");

    /* multiple matches within one line: nearest one after from_col */
    check(editor_find_next(&e, "fox", 4, 1, &r, &c) == 1 &&
          r == 4 && c == 4,
          "test_same_line_second_match");
    check(editor_find_next(&e, "fox", 4, 5, &r, &c) == 1 &&
          r == 4 && c == 8,
          "test_same_line_third_match");

    /* wrap-around: from past the last match, back to the top */
    check(editor_find_next(&e, "quick", 2, 0, &r, &c) == 1 &&
          r == 0 && c == 4,
          "test_wraps_around");

    /* wrap reaches the start line's earlier columns */
    check(editor_find_next(&e, "the", 2, 5, &r, &c) == 1 &&
          r == 0 && c == 0,
          "test_wrap_full_circle");
    check(editor_find_next(&e, "end", 2, 5, &r, &c) == 1 &&
          r == 2 && c == 4,
          "test_wrap_revisits_start_line");

    /* absent needle */
    check(editor_find_next(&e, "zebra", 0, 0, &r, &c) == 0,
          "test_no_match");

    /* empty and NULL queries match nothing */
    check(editor_find_next(&e, "", 0, 0, &r, &c) == 0,
          "test_empty_query");
    check(editor_find_next(&e, NULL, 0, 0, &r, &c) == 0,
          "test_null_query");

    /* needle spanning lines does not match (we search per line) */
    check(editor_find_next(&e, "fox\njumps", 0, 0, &r, &c) == 0,
          "test_no_cross_line_match");

    /* out-of-range start */
    check(editor_find_next(&e, "the", 99, 0, &r, &c) == 0,
          "test_bad_start_row");

    /* empty lines are skipped without crashing */
    check(editor_find_next(&e, "fox", 3, 0, &r, &c) == 1 &&
          r == 4 && c == 0,
          "test_search_past_empty_line");

    /* walking all matches of "the" with the caller's +1 policy */
    {
        int rows[8], cols[8], count = 0;
        int cr = 0, cc = 0;
        while (count < 8 &&
               editor_find_next(&e, "the", cr, cc, &r, &c) == 1) {
            /* stop once we've looped back to the first match */
            if (count > 0 && r == rows[0] && c == cols[0]) break;
            rows[count] = r;
            cols[count] = c;
            count++;
            cr = r;
            cc = c + 1;   /* the caller-side +1 */
        }
        check(count == 3 &&
              rows[0] == 0 && cols[0] == 0 &&
              rows[1] == 1 && cols[1] == 11 &&
              rows[2] == 2 && cols[2] == 0,
              "test_walk_all_matches");
    }

    free_doc(&e);
    return failed;
}
```
# Lesson: The Event Loop, Signals, and Cleanup {#main-loop}

Every piece is on the bench. The event loop is the engine block they
bolt onto — and it's small, because you've already built everything
hard about it:

```c
enable raw mode; enter the alternate screen;
while (running) {
    editor_scroll(e);                     /* enforce the invariant  */
    editor_render(e, &frame);             /* model -> one buffer    */
    write_all(STDOUT_FILENO, frame...);   /* -> one write           */
    wait for input (poll, with a timeout);
    read; decode key events; dispatch each;
}
leave the alternate screen; restore termios;
```

Render, wait, dispatch, repeat. vim, less, htop, tmux: this loop.
Three topics deserve real attention before you wire it: interruptions,
crashes, and where the *logic* should live.

## SIGWINCH and the rules of signal handlers

When the user drags the terminal's corner, the kernel sends your
process **SIGWINCH** (window change). You must re-query the size
(`ioctl(fd, TIOCGWINSZ, &winsize)`), resize the editor's viewport,
and repaint. The temptation is to do all that *in the signal
handler*. Don't — a signal handler interrupts your program at an
arbitrary instruction. Mid-`malloc`, mid-`printf`, mid anything. Call
anything non-reentrant from the handler and you corrupt whatever the
interrupted code was in the middle of; POSIX blesses only a short
list of "async-signal-safe" functions (`write` is on it; `malloc`,
`printf`, and friends are not).

The professional pattern makes the handler trivial and moves the
work to the loop:

```c
static volatile sig_atomic_t g_resized = 0;
static void on_winch(int sig) { (void)sig; g_resized = 1; }

/* in the loop, at the top: */
if (g_resized) { g_resized = 0; requery_size(e); /* repaint */ }
```

`volatile sig_atomic_t` is the one type C guarantees is safe to
write from a handler and read from normal code. And notice the loop
is *already* shaped to notice the flag promptly: the signal
interrupts the blocked `poll` (that EINTR you handled properly in
`wait_readable` — this is why), the loop comes around, sees the
flag, repaints at the new size.

## Dying well

Your program owns the user's terminal *and* their unsaved text; it
must exit cleanly even when things go wrong.

- **Normal exits**: leave the alternate screen, show the cursor,
  restore termios. Registering the restore with `atexit()` gives
  every `exit()` path the cleanup for free.
- **Errors**: the classic `die()` helper — restore terminal *first*,
  then `perror` + `exit(1)`, so the error message prints onto a sane
  screen instead of into raw-mode garbage.
- **Ctrl+C**: raw mode disabled ISIG, so it's just a byte (0x03) —
  your dispatch decides what it means. (Ours ignores it; Ctrl+Q is
  quit.)
- Even on `SIGSEGV`, a crash-handler that `write()`s the
  restore-terminal escape sequence before dying spares the user a
  wrecked shell (remember: `write` is signal-safe; this is the same
  trick as the resize flag, one rung more desperate).

## Dispatch is policy; keep it pure

The last design decision, and the one this challenge grades: what
happens when a key arrives? Resist writing it inline in the loop.
Make it a function —

```c
enum ed_action { ED_CONTINUE, ED_QUIT, ED_SAVE, ED_FIND };
enum ed_action editor_handle_key(struct editor *e, struct key_event k);
```

— that mutates the editor and *reports what the loop should do*,
touching no I/O itself. Saving involves a filesystem; quitting
involves the loop's control flow; so the function returns intent
(`ED_SAVE`, `ED_QUIT`) and the loop executes it. The payoff is the
one you've collected throughout the course: the entire keyboard
personality of the editor becomes a value-in, value-out function the
tests (and you) can drive without a terminal.

One piece of dispatch state earns its keep: **quit confirmation**.
Ctrl+Q with unsaved changes shouldn't kill the buffer on the spot;
the convention (kilo's, among others) is press-again-to-confirm:
first Ctrl+Q on a dirty buffer arms `quit_pending` and continues;
a second consecutive Ctrl+Q quits; *any other key* disarms it. It's
a two-state state machine — after the VT parser, barely worth the
name — but it's the difference between an editor and a data-loss
device.

## Challenge: Key Dispatch {#main-loop-impl points=30}

The starter provides a working editing core (line buffer, movement,
all from your earlier challenges — reference versions included so
this challenge stands alone). You write `editor_handle_key`:

| key | effect | returns |
|-----|--------|---------|
| printable `KEY_CHAR` (incl. tab) | insert, `dirty = 1` | `ED_CONTINUE` |
| `KEY_ENTER` | split line, `dirty = 1` | `ED_CONTINUE` |
| `KEY_BACKSPACE` / `KEY_DELETE` | the usual, `dirty = 1` | `ED_CONTINUE` |
| arrows / Home / End / PgUp / PgDn | `editor_move` | `ED_CONTINUE` |
| Ctrl+S | — | `ED_SAVE` |
| Ctrl+F | — | `ED_FIND` |
| Ctrl+Q | quit-confirm logic below | `ED_QUIT` / `ED_CONTINUE` |
| anything else | ignored | `ED_CONTINUE` |

Quit-confirm: Ctrl+Q returns `ED_QUIT` immediately if the buffer is
clean **or** `quit_pending` is set; otherwise it sets `quit_pending
= 1` and returns `ED_CONTINUE`. Every key *other than* Ctrl+Q
resets `quit_pending` to 0.

### Starter

```c
#include <stdlib.h>
#include <string.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

enum key_type {
    KEY_CHAR, KEY_CTRL, KEY_ALT, KEY_ENTER, KEY_BACKSPACE, KEY_ESCAPE,
    KEY_ARROW_UP, KEY_ARROW_DOWN, KEY_ARROW_LEFT, KEY_ARROW_RIGHT,
    KEY_HOME, KEY_END, KEY_PAGE_UP, KEY_PAGE_DOWN, KEY_DELETE,
    KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;
    int mods;
};

enum editor_move_dir {
    MOVE_UP, MOVE_DOWN, MOVE_LEFT, MOVE_RIGHT,
    MOVE_HOME, MOVE_END, MOVE_PAGE_UP, MOVE_PAGE_DOWN,
};

enum ed_action { ED_CONTINUE, ED_QUIT, ED_SAVE, ED_FIND };

/* --- provided: the editing core (reference versions of your
 *     earlier challenges; substituting your own is encouraged) --- */

int editor_init(struct editor *e) {
    memset(e, 0, sizeof(*e));
    e->lines = malloc(8 * sizeof(struct line));
    if (!e->lines) return -1;
    e->cap = 8;
    e->lines[0].text = calloc(1, 1);
    if (!e->lines[0].text) return -1;
    e->lines[0].len = 0;
    e->nlines = 1;
    e->screen_rows = 24;
    e->screen_cols = 80;
    return 0;
}

void editor_free(struct editor *e) {
    for (int i = 0; i < e->nlines; i++)
        free(e->lines[i].text);
    free(e->lines);
    memset(e, 0, sizeof(*e));
}

const char *editor_get_line(const struct editor *e, int row) {
    if (row < 0 || row >= e->nlines) return "";
    return e->lines[row].text;
}

int editor_line_len(const struct editor *e, int row) {
    if (row < 0 || row >= e->nlines) return 0;
    return e->lines[row].len;
}

static int lines_make_gap(struct editor *e, int at) {
    if (e->nlines == e->cap) {
        struct line *nl = realloc(e->lines,
                                  (size_t)e->cap * 2 * sizeof(*nl));
        if (!nl) return -1;
        e->lines = nl;
        e->cap *= 2;
    }
    memmove(&e->lines[at + 1], &e->lines[at],
            (size_t)(e->nlines - at) * sizeof(struct line));
    e->nlines++;
    return 0;
}

void editor_insert_char(struct editor *e, char c) {
    struct line *ln = &e->lines[e->cursor_row];
    char *nt = realloc(ln->text, (size_t)ln->len + 2);
    if (!nt) return;
    ln->text = nt;
    memmove(ln->text + e->cursor_col + 1, ln->text + e->cursor_col,
            (size_t)(ln->len - e->cursor_col) + 1);
    ln->text[e->cursor_col] = c;
    ln->len++;
    e->cursor_col++;
    e->goal_col = e->cursor_col;
}

void editor_newline(struct editor *e) {
    struct line *ln = &e->lines[e->cursor_row];
    int tail = ln->len - e->cursor_col;
    char *rest = malloc((size_t)tail + 1);
    if (!rest || lines_make_gap(e, e->cursor_row + 1) != 0) {
        free(rest);
        return;
    }
    ln = &e->lines[e->cursor_row]; /* realloc may have moved lines */
    memcpy(rest, ln->text + e->cursor_col, (size_t)tail + 1);
    e->lines[e->cursor_row + 1].text = rest;
    e->lines[e->cursor_row + 1].len = tail;
    ln->len = e->cursor_col;
    ln->text[ln->len] = '\0';
    e->cursor_row++;
    e->cursor_col = 0;
    e->goal_col = 0;
}

static void join_line_up(struct editor *e, int row) {
    /* append row+1's text onto row, remove row+1 */
    struct line *a = &e->lines[row], *b = &e->lines[row + 1];
    char *nt = realloc(a->text, (size_t)a->len + (size_t)b->len + 1);
    if (!nt) return;
    a->text = nt;
    memcpy(a->text + a->len, b->text, (size_t)b->len + 1);
    a->len += b->len;
    free(b->text);
    memmove(&e->lines[row + 1], &e->lines[row + 2],
            (size_t)(e->nlines - row - 2) * sizeof(struct line));
    e->nlines--;
}

void editor_backspace(struct editor *e) {
    if (e->cursor_col > 0) {
        struct line *ln = &e->lines[e->cursor_row];
        memmove(ln->text + e->cursor_col - 1, ln->text + e->cursor_col,
                (size_t)(ln->len - e->cursor_col) + 1);
        ln->len--;
        e->cursor_col--;
    } else if (e->cursor_row > 0) {
        int seam = e->lines[e->cursor_row - 1].len;
        join_line_up(e, e->cursor_row - 1);
        e->cursor_row--;
        e->cursor_col = seam;
    }
    e->goal_col = e->cursor_col;
}

void editor_delete_char(struct editor *e) {
    struct line *ln = &e->lines[e->cursor_row];
    if (e->cursor_col < ln->len) {
        memmove(ln->text + e->cursor_col, ln->text + e->cursor_col + 1,
                (size_t)(ln->len - e->cursor_col));
        ln->len--;
    } else if (e->cursor_row < e->nlines - 1) {
        join_line_up(e, e->cursor_row);
    }
    e->goal_col = e->cursor_col;
}

void editor_move(struct editor *e, enum editor_move_dir m) {
    int len = editor_line_len(e, e->cursor_row);
    switch (m) {
    case MOVE_UP:
        if (e->cursor_row > 0) {
            e->cursor_row--;
            len = editor_line_len(e, e->cursor_row);
            e->cursor_col = e->goal_col < len ? e->goal_col : len;
        }
        break;
    case MOVE_DOWN:
        if (e->cursor_row < e->nlines - 1) {
            e->cursor_row++;
            len = editor_line_len(e, e->cursor_row);
            e->cursor_col = e->goal_col < len ? e->goal_col : len;
        }
        break;
    case MOVE_PAGE_UP:
    case MOVE_PAGE_DOWN: {
        int dr = m == MOVE_PAGE_UP ? -e->screen_rows : e->screen_rows;
        e->cursor_row += dr;
        if (e->cursor_row < 0) e->cursor_row = 0;
        if (e->cursor_row > e->nlines - 1) e->cursor_row = e->nlines - 1;
        len = editor_line_len(e, e->cursor_row);
        e->cursor_col = e->goal_col < len ? e->goal_col : len;
        break;
    }
    case MOVE_LEFT:
        if (e->cursor_col > 0) {
            e->cursor_col--;
        } else if (e->cursor_row > 0) {
            e->cursor_row--;
            e->cursor_col = editor_line_len(e, e->cursor_row);
        }
        e->goal_col = e->cursor_col;
        break;
    case MOVE_RIGHT:
        if (e->cursor_col < len) {
            e->cursor_col++;
        } else if (e->cursor_row < e->nlines - 1) {
            e->cursor_row++;
            e->cursor_col = 0;
        }
        e->goal_col = e->cursor_col;
        break;
    case MOVE_HOME:
        e->cursor_col = 0;
        e->goal_col = 0;
        break;
    case MOVE_END:
        e->cursor_col = len;
        e->goal_col = len;
        break;
    }
}

/* --- yours --- */

enum ed_action editor_handle_key(struct editor *e, struct key_event k) {
    /* TODO: the dispatch table from the challenge description       */
    /* TODO: quit_pending: armed by Ctrl+Q on a dirty buffer,        */
    /*       disarmed by every other key                             */
    (void)e; (void)k;
    return ED_CONTINUE;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

enum key_type {
    KEY_CHAR, KEY_CTRL, KEY_ALT, KEY_ENTER, KEY_BACKSPACE, KEY_ESCAPE,
    KEY_ARROW_UP, KEY_ARROW_DOWN, KEY_ARROW_LEFT, KEY_ARROW_RIGHT,
    KEY_HOME, KEY_END, KEY_PAGE_UP, KEY_PAGE_DOWN, KEY_DELETE,
    KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;
    int mods;
};

enum ed_action { ED_CONTINUE, ED_QUIT, ED_SAVE, ED_FIND };

int editor_init(struct editor *e);
void editor_free(struct editor *e);
const char *editor_get_line(const struct editor *e, int row);
enum ed_action editor_handle_key(struct editor *e, struct key_event k);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static enum ed_action press(struct editor *e, enum key_type t, int v) {
    struct key_event k = { t, v, 0 };
    return editor_handle_key(e, k);
}

int main(void) {
    struct editor e;
    editor_init(&e);

    /* typing inserts and dirties */
    check(press(&e, KEY_CHAR, 'h') == ED_CONTINUE, "test_char_continues");
    press(&e, KEY_CHAR, 'i');
    check(strcmp(editor_get_line(&e, 0), "hi") == 0, "test_chars_insert");
    check(e.dirty == 1, "test_typing_dirties");
    check(e.cursor_col == 2, "test_cursor_advanced");

    /* enter splits */
    press(&e, KEY_ENTER, 0);
    check(e.nlines == 2 && e.cursor_row == 1, "test_enter_splits");
    check(e.dirty == 1, "test_enter_dirties");

    /* arrows move without dirtying a saved buffer */
    e.dirty = 0;
    press(&e, KEY_ARROW_UP, 0);
    check(e.cursor_row == 0, "test_arrow_up");
    press(&e, KEY_ARROW_RIGHT, 0);
    check(e.cursor_col == 1, "test_arrow_right");
    press(&e, KEY_END, 0);
    check(e.cursor_col == 2, "test_end_key");
    press(&e, KEY_HOME, 0);
    check(e.cursor_col == 0, "test_home_key");
    check(e.dirty == 0, "test_movement_does_not_dirty");

    /* backspace / delete edit and dirty */
    press(&e, KEY_ARROW_RIGHT, 0);
    press(&e, KEY_BACKSPACE, 0);
    check(strcmp(editor_get_line(&e, 0), "i") == 0, "test_backspace");
    check(e.dirty == 1, "test_backspace_dirties");
    press(&e, KEY_HOME, 0);
    press(&e, KEY_DELETE, 0);
    check(strcmp(editor_get_line(&e, 0), "") == 0, "test_delete");

    /* ctrl+s asks the loop to save */
    check(press(&e, KEY_CTRL, 'S') == ED_SAVE, "test_ctrl_s_saves");

    /* ctrl+f asks the loop to find */
    check(press(&e, KEY_CTRL, 'F') == ED_FIND, "test_ctrl_f_finds");

    /* ctrl+q on a CLEAN buffer quits at once */
    e.dirty = 0;
    e.quit_pending = 0;
    check(press(&e, KEY_CTRL, 'Q') == ED_QUIT, "test_clean_quit");

    /* ctrl+q on a DIRTY buffer needs confirmation */
    e.dirty = 1;
    e.quit_pending = 0;
    check(press(&e, KEY_CTRL, 'Q') == ED_CONTINUE, "test_dirty_quit_armed");
    check(e.quit_pending == 1, "test_quit_pending_set");
    check(press(&e, KEY_CTRL, 'Q') == ED_QUIT, "test_dirty_quit_confirmed");

    /* any other key disarms the confirmation */
    e.dirty = 1;
    e.quit_pending = 0;
    press(&e, KEY_CTRL, 'Q');
    check(e.quit_pending == 1, "test_armed_again");
    press(&e, KEY_CHAR, 'x');
    check(e.quit_pending == 0, "test_typing_disarms");
    check(press(&e, KEY_CTRL, 'Q') == ED_CONTINUE,
          "test_quit_needs_rearming");
    press(&e, KEY_ARROW_LEFT, 0);
    check(e.quit_pending == 0, "test_movement_disarms");

    /* unknown keys are ignored gracefully */
    e.dirty = 0;
    check(press(&e, KEY_CTRL, 'G') == ED_CONTINUE, "test_unknown_ctrl");
    check(press(&e, KEY_UNKNOWN, 0) == ED_CONTINUE, "test_unknown_key");
    check(press(&e, KEY_ESCAPE, 0) == ED_CONTINUE, "test_escape_ignored");
    check(e.dirty == 0, "test_ignored_keys_do_not_dirty");

    /* a realistic little session */
    editor_free(&e);
    editor_init(&e);
    const char *text = "hello";
    for (const char *p = text; *p; p++)
        press(&e, KEY_CHAR, *p);
    press(&e, KEY_ENTER, 0);
    for (const char *p = "world"; *p; p++)
        press(&e, KEY_CHAR, *p);
    press(&e, KEY_HOME, 0);
    press(&e, KEY_ARROW_UP, 0);
    press(&e, KEY_END, 0);
    press(&e, KEY_CHAR, '!');
    check(strcmp(editor_get_line(&e, 0), "hello!") == 0 &&
          strcmp(editor_get_line(&e, 1), "world") == 0,
          "test_session");

    editor_free(&e);
    return failed;
}
```
# Final Challenge: duck — a Terminal Text Editor {#final-editor points=100}

Assembly time. Every layer you've built converges into one program —
call it `duck` — that opens a file in a terminal, edits it, and
saves it back. The layering you'll wire together, bottom to top:

```
             ┌────────────────────────────────────────────┐
   output ◄──┤ editor_render     frame -> escape sequences │
             │   editor_scroll   viewport invariant        │
             │   cx_to_rx        tabs -> screen columns    │
             │   format_status_bar                         │
             ├────────────────────────────────────────────┤
    state    │ struct editor     lines, cursor, dirty      │
             │   insert/delete/newline/move/open/save      │
             ├────────────────────────────────────────────┤
    input ──►│ editor_feed_input bytes -> keys -> actions  │
             │   key_decode      escape-sequence grammar   │
             │   editor_handle_key                dispatch │
             └────────────────────────────────────────────┘
```

The starter provides the editing core and file I/O (reference
versions of your earlier work — swap in your own; they're yours
now) plus the abuf. You bring three of your solutions and write two
integration functions:

**Paste in** (from your previous challenges): `key_decode`
(parse-keys), `editor_handle_key` (main-loop-impl),
`format_status_bar` (status-line-impl).

**Write new:**

`editor_render(e, ab)` — serialize one complete frame into the
abuf, exactly this recipe:

1. Call `editor_scroll(e)` (also yours to write — the two-axis
   invariant from the scrolling challenge, using render columns).
2. `\x1b[?25l\x1b[H` — hide cursor, home.
3. For each of the `screen_rows` text rows: the visible slice of
   file row `rowoff + i` — render columns `[coloff, coloff +
   screen_cols)` with tabs expanded to `TABSTOP`-column stops — or
   a single `~` for rows past the end of the file; then `\x1b[K`
   and `\r\n`.
4. The status bar: `\x1b[7m`, then `format_status_bar(...)` at
   exactly `screen_cols` wide, then `\x1b[m`.
5. Park the terminal cursor on the editor cursor:
   `\x1b[<cursor_row - rowoff + 1>;<rx - coloff + 1>H` (1-based;
   `rx` from `cx_to_rx`).
6. `\x1b[?25h` — show cursor.

`editor_feed_input(e, buf, len)` — drain a chunk of raw input:
loop `key_decode` over the buffer; for each event, dispatch through
`editor_handle_key`; execute the actions (`ED_SAVE` →
`editor_save(e, e->filename)`; `ED_FIND` → ignore here; `ED_QUIT` →
stop processing and return `ED_QUIT`). When `key_decode` returns 0
(incomplete trailing sequence) or the buffer runs out, return
`ED_CONTINUE`. (A real loop buffers the incomplete tail and applies
the ESC timeout — the DEMO main does; the graded function may
discard it.)

The tests run your editor headless through a full scripted session:
load a file from disk, page and arrow around it (checking the
rendered frames: viewport contents, `~` filler, status bar, cursor
parking), type into it, save with Ctrl+S, verify the bytes that
landed on disk, and quit with the dirty-buffer confirmation dance.
If the previous challenges pass, most of your work here is careful
plumbing — which is the point: **systems are assembled from parts
that were designed to be assembled.**

When it's green, run the real thing:

```
cc -std=c17 -Wall -DDEMO solution.c -o duck
./duck some_file.txt
```

Raw mode, alternate screen, arrows, editing, Ctrl+S, Ctrl+Q. A
terminal program you built from `read(2)` up. Take the moment —
then go read kilo's source and see how close you landed, and the
extended-reading list for where to go next (windows! syntax
highlighting! a pty of your own with a shell inside!).

### Starter

```c
#define _XOPEN_SOURCE 600
#define _POSIX_C_SOURCE 200809L
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <errno.h>
#include <unistd.h>
#include <fcntl.h>

#define TABSTOP 8

/* ============================ types ============================ */

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;   /* TEXT viewport (status bar extra) */
    int dirty;
    int quit_pending;
    char filename[256];
};

enum key_type {
    KEY_CHAR, KEY_CTRL, KEY_ALT, KEY_ENTER, KEY_BACKSPACE, KEY_ESCAPE,
    KEY_ARROW_UP, KEY_ARROW_DOWN, KEY_ARROW_LEFT, KEY_ARROW_RIGHT,
    KEY_HOME, KEY_END, KEY_PAGE_UP, KEY_PAGE_DOWN, KEY_DELETE,
    KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;
    int mods;
};

enum editor_move_dir {
    MOVE_UP, MOVE_DOWN, MOVE_LEFT, MOVE_RIGHT,
    MOVE_HOME, MOVE_END, MOVE_PAGE_UP, MOVE_PAGE_DOWN,
};

enum ed_action { ED_CONTINUE, ED_QUIT, ED_SAVE, ED_FIND };

/* ==================== provided: append buffer ================== */

struct abuf {
    char *b;
    size_t len;
    size_t cap;
};

void ab_init(struct abuf *ab) { ab->b = NULL; ab->len = 0; ab->cap = 0; }

int ab_append(struct abuf *ab, const char *bytes, size_t n) {
    if (ab->len + n > ab->cap) {
        size_t cap = ab->cap ? ab->cap : 64;
        while (cap < ab->len + n) cap *= 2;
        char *nb = realloc(ab->b, cap);
        if (!nb) return -1;
        ab->b = nb;
        ab->cap = cap;
    }
    memcpy(ab->b + ab->len, bytes, n);
    ab->len += n;
    return 0;
}

int ab_append_str(struct abuf *ab, const char *s) {
    return ab_append(ab, s, strlen(s));
}

void ab_free(struct abuf *ab) { free(ab->b); ab_init(ab); }

/* =============== provided: editing core + file I/O ============= */

int editor_init(struct editor *e) {
    memset(e, 0, sizeof(*e));
    e->lines = malloc(8 * sizeof(struct line));
    if (!e->lines) return -1;
    e->cap = 8;
    e->lines[0].text = calloc(1, 1);
    if (!e->lines[0].text) return -1;
    e->lines[0].len = 0;
    e->nlines = 1;
    e->screen_rows = 23;
    e->screen_cols = 80;
    return 0;
}

void editor_free(struct editor *e) {
    for (int i = 0; i < e->nlines; i++)
        free(e->lines[i].text);
    free(e->lines);
    memset(e, 0, sizeof(*e));
}

const char *editor_get_line(const struct editor *e, int row) {
    if (row < 0 || row >= e->nlines) return "";
    return e->lines[row].text;
}

int editor_line_len(const struct editor *e, int row) {
    if (row < 0 || row >= e->nlines) return 0;
    return e->lines[row].len;
}

static int lines_make_gap(struct editor *e, int at) {
    if (e->nlines == e->cap) {
        struct line *nl = realloc(e->lines,
                                  (size_t)e->cap * 2 * sizeof(*nl));
        if (!nl) return -1;
        e->lines = nl;
        e->cap *= 2;
    }
    memmove(&e->lines[at + 1], &e->lines[at],
            (size_t)(e->nlines - at) * sizeof(struct line));
    e->nlines++;
    return 0;
}

static int append_raw_line(struct editor *e, const char *s, int n) {
    char *copy = malloc((size_t)n + 1);
    if (!copy) return -1;
    memcpy(copy, s, (size_t)n);
    copy[n] = '\0';
    if (e->nlines == 1 && e->lines[0].len == 0 &&
        e->lines[0].text[0] == '\0' && e->dirty == 0) {
        free(e->lines[0].text);
        e->lines[0].text = copy;
        e->lines[0].len = n;
        e->dirty = 1; /* mark slot used; cleared again by open/save */
        return 0;
    }
    if (lines_make_gap(e, e->nlines) != 0) {
        free(copy);
        return -1;
    }
    e->lines[e->nlines - 1].text = copy;
    e->lines[e->nlines - 1].len = n;
    return 0;
}

void editor_insert_char(struct editor *e, char c) {
    struct line *ln = &e->lines[e->cursor_row];
    char *nt = realloc(ln->text, (size_t)ln->len + 2);
    if (!nt) return;
    ln->text = nt;
    memmove(ln->text + e->cursor_col + 1, ln->text + e->cursor_col,
            (size_t)(ln->len - e->cursor_col) + 1);
    ln->text[e->cursor_col] = c;
    ln->len++;
    e->cursor_col++;
    e->goal_col = e->cursor_col;
}

void editor_newline(struct editor *e) {
    struct line *ln = &e->lines[e->cursor_row];
    int tail = ln->len - e->cursor_col;
    char *rest = malloc((size_t)tail + 1);
    if (!rest || lines_make_gap(e, e->cursor_row + 1) != 0) {
        free(rest);
        return;
    }
    ln = &e->lines[e->cursor_row];
    memcpy(rest, ln->text + e->cursor_col, (size_t)tail + 1);
    e->lines[e->cursor_row + 1].text = rest;
    e->lines[e->cursor_row + 1].len = tail;
    ln->len = e->cursor_col;
    ln->text[ln->len] = '\0';
    e->cursor_row++;
    e->cursor_col = 0;
    e->goal_col = 0;
}

static void join_line_up(struct editor *e, int row) {
    struct line *a = &e->lines[row], *b = &e->lines[row + 1];
    char *nt = realloc(a->text, (size_t)a->len + (size_t)b->len + 1);
    if (!nt) return;
    a->text = nt;
    memcpy(a->text + a->len, b->text, (size_t)b->len + 1);
    a->len += b->len;
    free(b->text);
    memmove(&e->lines[row + 1], &e->lines[row + 2],
            (size_t)(e->nlines - row - 2) * sizeof(struct line));
    e->nlines--;
}

void editor_backspace(struct editor *e) {
    if (e->cursor_col > 0) {
        struct line *ln = &e->lines[e->cursor_row];
        memmove(ln->text + e->cursor_col - 1, ln->text + e->cursor_col,
                (size_t)(ln->len - e->cursor_col) + 1);
        ln->len--;
        e->cursor_col--;
    } else if (e->cursor_row > 0) {
        int seam = e->lines[e->cursor_row - 1].len;
        join_line_up(e, e->cursor_row - 1);
        e->cursor_row--;
        e->cursor_col = seam;
    }
    e->goal_col = e->cursor_col;
}

void editor_delete_char(struct editor *e) {
    struct line *ln = &e->lines[e->cursor_row];
    if (e->cursor_col < ln->len) {
        memmove(ln->text + e->cursor_col, ln->text + e->cursor_col + 1,
                (size_t)(ln->len - e->cursor_col));
        ln->len--;
    } else if (e->cursor_row < e->nlines - 1) {
        join_line_up(e, e->cursor_row);
    }
    e->goal_col = e->cursor_col;
}

void editor_move(struct editor *e, enum editor_move_dir m) {
    int len = editor_line_len(e, e->cursor_row);
    switch (m) {
    case MOVE_UP:
        if (e->cursor_row > 0) {
            e->cursor_row--;
            len = editor_line_len(e, e->cursor_row);
            e->cursor_col = e->goal_col < len ? e->goal_col : len;
        }
        break;
    case MOVE_DOWN:
        if (e->cursor_row < e->nlines - 1) {
            e->cursor_row++;
            len = editor_line_len(e, e->cursor_row);
            e->cursor_col = e->goal_col < len ? e->goal_col : len;
        }
        break;
    case MOVE_PAGE_UP:
    case MOVE_PAGE_DOWN: {
        int dr = m == MOVE_PAGE_UP ? -e->screen_rows : e->screen_rows;
        e->cursor_row += dr;
        if (e->cursor_row < 0) e->cursor_row = 0;
        if (e->cursor_row > e->nlines - 1) e->cursor_row = e->nlines - 1;
        len = editor_line_len(e, e->cursor_row);
        e->cursor_col = e->goal_col < len ? e->goal_col : len;
        break;
    }
    case MOVE_LEFT:
        if (e->cursor_col > 0) {
            e->cursor_col--;
        } else if (e->cursor_row > 0) {
            e->cursor_row--;
            e->cursor_col = editor_line_len(e, e->cursor_row);
        }
        e->goal_col = e->cursor_col;
        break;
    case MOVE_RIGHT:
        if (e->cursor_col < len) {
            e->cursor_col++;
        } else if (e->cursor_row < e->nlines - 1) {
            e->cursor_row++;
            e->cursor_col = 0;
        }
        e->goal_col = e->cursor_col;
        break;
    case MOVE_HOME:
        e->cursor_col = 0;
        e->goal_col = 0;
        break;
    case MOVE_END:
        e->cursor_col = len;
        e->goal_col = len;
        break;
    }
}

static ssize_t write_all(int fd, const void *buf, size_t n) {
    const char *p = buf;
    size_t off = 0;
    while (off < n) {
        ssize_t w = write(fd, p + off, n - off);
        if (w < 0) {
            if (errno == EINTR) continue;
            return -1;
        }
        off += (size_t)w;
    }
    return (ssize_t)n;
}

int editor_open(struct editor *e, const char *path) {
    snprintf(e->filename, sizeof(e->filename), "%s", path);
    int fd = open(path, O_RDONLY);
    if (fd < 0) {
        if (errno == ENOENT) { e->dirty = 0; return 0; }
        return -1;
    }
    struct abuf all;
    ab_init(&all);
    char chunk[4096];
    ssize_t r;
    while ((r = read(fd, chunk, sizeof(chunk))) != 0) {
        if (r < 0) {
            if (errno == EINTR) continue;
            close(fd);
            ab_free(&all);
            return -1;
        }
        ab_append(&all, chunk, (size_t)r);
    }
    close(fd);
    size_t start = 0;
    for (size_t i = 0; i < all.len; i++) {
        if (all.b[i] == '\n') {
            size_t end = i;
            if (end > start && all.b[end - 1] == '\r') end--;
            append_raw_line(e, all.b + start, (int)(end - start));
            start = i + 1;
        }
    }
    if (start < all.len)
        append_raw_line(e, all.b + start, (int)(all.len - start));
    ab_free(&all);
    e->dirty = 0;
    return 0;
}

long editor_save(struct editor *e, const char *path) {
    char tmp[sizeof(e->filename) + 8];
    if (snprintf(tmp, sizeof(tmp), "%s.tmp", path) >= (int)sizeof(tmp))
        return -1;
    int fd = open(tmp, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    if (fd < 0) return -1;
    long total = 0;
    for (int i = 0; i < e->nlines; i++) {
        if (write_all(fd, e->lines[i].text, (size_t)e->lines[i].len) < 0 ||
            write_all(fd, "\n", 1) < 0) {
            close(fd);
            unlink(tmp);
            return -1;
        }
        total += e->lines[i].len + 1;
    }
    if (close(fd) != 0 || rename(tmp, path) != 0) {
        unlink(tmp);
        return -1;
    }
    snprintf(e->filename, sizeof(e->filename), "%s", path);
    e->dirty = 0;
    return total;
}

/* ====================== yours: paste + write ==================== */

/* PASTE your solution from the parse-keys challenge. */
size_t key_decode(const unsigned char *buf, size_t len,
                  struct key_event *out) {
    /* TODO */
    (void)buf; (void)len; (void)out;
    return 0;
}

/* PASTE your solution from the render-cols challenge. */
int editor_cx_to_rx(const struct line *ln, int cx) {
    /* TODO */
    (void)ln; (void)cx;
    return 0;
}

/* PASTE your solution from the status-line-impl challenge. */
void format_status_bar(const struct editor *e, char *out, int width) {
    /* TODO */
    (void)e; (void)width;
    out[0] = '\0';
}

/* PASTE your solution from the main-loop-impl challenge. */
enum ed_action editor_handle_key(struct editor *e, struct key_event k) {
    /* TODO */
    (void)e; (void)k;
    return ED_CONTINUE;
}

/* PASTE your solution from the scrolling challenge (rx-aware). */
void editor_scroll(struct editor *e) {
    /* TODO */
    (void)e;
}

/* WRITE: serialize one frame (the 6-step recipe in the prompt). */
void editor_render(struct editor *e, struct abuf *ab) {
    /* TODO: editor_scroll(e)                                        */
    /* TODO: "\x1b[?25l\x1b[H"                                       */
    /* TODO: screen_rows text rows: visible slice w/ tabs expanded,  */
    /*       or "~"; then "\x1b[K\r\n" each                          */
    /* TODO: "\x1b[7m" + status (exactly screen_cols wide) + "\x1b[m"*/
    /* TODO: cursor CUP (1-based, viewport-relative, rx-based)       */
    /* TODO: "\x1b[?25h"                                             */
    (void)e; (void)ab;
}

/* WRITE: drain an input chunk through decode -> dispatch -> action. */
enum ed_action editor_feed_input(struct editor *e,
                                 const unsigned char *buf, size_t len) {
    /* TODO: loop key_decode; 0 consumed -> done (ED_CONTINUE)       */
    /* TODO: ED_SAVE -> editor_save(e, e->filename) and continue     */
    /* TODO: ED_QUIT -> return ED_QUIT immediately                   */
    (void)e; (void)buf; (void)len;
    return ED_CONTINUE;
}

/* ===================== the real thing (demo) ==================== */
#ifdef DEMO
#include <termios.h>
#include <signal.h>
#include <sys/ioctl.h>
#include <poll.h>

static struct termios g_saved;
static void term_restore(void) {
    write(STDOUT_FILENO, "\x1b[?1049l\x1b[?25h", 14);
    tcsetattr(STDIN_FILENO, TCSAFLUSH, &g_saved);
}
static volatile sig_atomic_t g_resized = 0;
static void on_winch(int sig) { (void)sig; g_resized = 1; }

static void query_size(struct editor *e) {
    struct winsize ws;
    if (ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws) == 0 && ws.ws_row > 1) {
        e->screen_rows = ws.ws_row - 1;   /* status bar rent */
        e->screen_cols = ws.ws_col;
    }
}

int main(int argc, char *argv[]) {
    struct editor e;
    if (editor_init(&e) != 0) return 1;
    if (argc > 1 && editor_open(&e, argv[1]) != 0) {
        perror(argv[1]);
        return 1;
    }

    if (tcgetattr(STDIN_FILENO, &g_saved) != 0) {
        fprintf(stderr, "not a terminal\n");
        return 1;
    }
    struct termios raw = g_saved;
    raw.c_iflag &= ~(tcflag_t)(BRKINT | ICRNL | INPCK | ISTRIP | IXON);
    raw.c_oflag &= ~(tcflag_t)OPOST;
    raw.c_cflag |= (tcflag_t)CS8;
    raw.c_lflag &= ~(tcflag_t)(ECHO | ICANON | IEXTEN | ISIG);
    raw.c_cc[VMIN] = 1;
    raw.c_cc[VTIME] = 0;
    tcsetattr(STDIN_FILENO, TCSAFLUSH, &raw);
    atexit(term_restore);
    signal(SIGWINCH, on_winch);
    write(STDOUT_FILENO, "\x1b[?1049h", 8);
    query_size(&e);

    unsigned char inbuf[64];
    size_t have = 0;
    struct abuf frame;
    ab_init(&frame);
    for (;;) {
        if (g_resized) { g_resized = 0; query_size(&e); }

        frame.len = 0;
        editor_render(&e, &frame);
        write_all(STDOUT_FILENO, frame.b, frame.len);

        struct pollfd p = { .fd = STDIN_FILENO, .events = POLLIN };
        int pr = poll(&p, 1, have > 0 ? 25 : -1); /* ESC timeout */
        if (pr < 0 && errno != EINTR) break;
        if (pr > 0) {
            ssize_t r = read(STDIN_FILENO, inbuf + have,
                             sizeof(inbuf) - have);
            if (r <= 0) continue;
            have += (size_t)r;
        } else if (pr == 0 && have > 0 && inbuf[0] == 0x1b) {
            /* lone ESC resolved by timeout: treat as Escape key */
            struct key_event ev = { KEY_ESCAPE, 0, 0 };
            editor_handle_key(&e, ev);
            memmove(inbuf, inbuf + 1, --have);
        }
        size_t off = 0;
        int quit = 0;
        while (off < have) {
            struct key_event ev;
            size_t used = key_decode(inbuf + off, have - off, &ev);
            if (used == 0) break;
            enum ed_action a = editor_handle_key(&e, ev);
            if (a == ED_SAVE && e.filename[0])
                editor_save(&e, e.filename);
            if (a == ED_QUIT) { quit = 1; break; }
            off += used;
        }
        memmove(inbuf, inbuf + off, have - off);
        have -= off;
        if (quit) break;
    }
    ab_free(&frame);
    editor_free(&e);
    return 0;
}
#endif
```

### Tests

```c
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>

#define TABSTOP 8

struct line {
    char *text;
    int   len;
};

struct editor {
    struct line *lines;
    int nlines;
    int cap;
    int cursor_row, cursor_col;
    int goal_col;
    int rowoff, coloff;
    int screen_rows, screen_cols;
    int dirty;
    int quit_pending;
    char filename[256];
};

enum key_type {
    KEY_CHAR, KEY_CTRL, KEY_ALT, KEY_ENTER, KEY_BACKSPACE, KEY_ESCAPE,
    KEY_ARROW_UP, KEY_ARROW_DOWN, KEY_ARROW_LEFT, KEY_ARROW_RIGHT,
    KEY_HOME, KEY_END, KEY_PAGE_UP, KEY_PAGE_DOWN, KEY_DELETE,
    KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;
    int mods;
};

enum ed_action { ED_CONTINUE, ED_QUIT, ED_SAVE, ED_FIND };

struct abuf {
    char *b;
    size_t len;
    size_t cap;
};

void ab_init(struct abuf *ab);
void ab_free(struct abuf *ab);

int editor_init(struct editor *e);
void editor_free(struct editor *e);
const char *editor_get_line(const struct editor *e, int row);
int editor_open(struct editor *e, const char *path);
long editor_save(struct editor *e, const char *path);
void editor_render(struct editor *e, struct abuf *ab);
enum ed_action editor_feed_input(struct editor *e,
                                 const unsigned char *buf, size_t len);

static int failed = 0;

static void check(int ok, const char *name) {
    if (ok) {
        printf("--- PASS: %s\n", name);
    } else {
        printf("--- FAIL: %s\n", name);
        failed++;
    }
}

static void write_file(const char *path, const char *bytes) {
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    write(fd, bytes, strlen(bytes));
    close(fd);
}

static long read_file(const char *path, char *out, size_t cap) {
    int fd = open(path, O_RDONLY);
    if (fd < 0) return -1;
    long total = 0;
    ssize_t r;
    while ((r = read(fd, out + total, cap - (size_t)total)) > 0)
        total += r;
    close(fd);
    out[total] = '\0';
    return total;
}

/* does the frame contain this byte string? */
static int frame_has(const struct abuf *ab, const char *needle) {
    size_t nl = strlen(needle);
    if (nl > ab->len) return 0;
    for (size_t i = 0; i + nl <= ab->len; i++)
        if (memcmp(ab->b + i, needle, nl) == 0) return 1;
    return 0;
}

static enum ed_action feed_str(struct editor *e, const char *s) {
    return editor_feed_input(e, (const unsigned char *)s, strlen(s));
}

int main(void) {
    alarm(30);
    struct editor e;
    struct abuf frame;
    char buf[8192];

    write_file("duck_test.txt",
               "alpha line\n"
               "bravo line\n"
               "charlie line\n"
               "delta line\n"
               "echo line\n"
               "foxtrot line\n"
               "golf line\n"
               "hotel line\n");

    editor_init(&e);
    check(editor_open(&e, "duck_test.txt") == 0, "test_open");
    check(e.nlines == 8, "test_loaded_lines");
    e.screen_rows = 4;
    e.screen_cols = 30;

    /* --- first frame --- */
    ab_init(&frame);
    editor_render(&e, &frame);
    check(frame.len > 0, "test_render_emits");
    check(frame.len >= 9 &&
          memcmp(frame.b, "\x1b[?25l\x1b[H", 9) == 0,
          "test_render_starts_hide_home");
    check(frame_has(&frame, "alpha line"), "test_render_first_line");
    check(frame_has(&frame, "delta line"), "test_render_fourth_line");
    check(!frame_has(&frame, "echo line"),
          "test_render_clips_to_viewport");
    check(frame_has(&frame, "\x1b[K"), "test_render_clears_lines");
    check(frame_has(&frame, "\x1b[7m"), "test_render_status_reverse");
    check(frame_has(&frame, "duck_test.txt"), "test_render_status_name");
    check(frame_has(&frame, "Ln 1, Col 1"), "test_render_status_position");
    check(frame_has(&frame, "\x1b[1;1H"), "test_render_cursor_parked");
    check(frame.len >= 6 &&
          memcmp(frame.b + frame.len - 6, "\x1b[?25h", 6) == 0,
          "test_render_ends_show_cursor");
    ab_free(&frame);

    /* --- arrows move the cursor; the frame follows --- */
    check(feed_str(&e, "\x1b[B\x1b[B\x1b[C\x1b[C\x1b[C") == ED_CONTINUE,
          "test_feed_arrows");
    check(e.cursor_row == 2 && e.cursor_col == 3, "test_arrow_position");
    ab_init(&frame);
    editor_render(&e, &frame);
    check(frame_has(&frame, "Ln 3, Col 4"), "test_status_tracks_cursor");
    check(frame_has(&frame, "\x1b[3;4H"), "test_cursor_parks_at_position");
    ab_free(&frame);

    /* --- scrolling: walk below the viewport --- */
    feed_str(&e, "\x1b[B\x1b[B\x1b[B\x1b[B");   /* to row 6 */
    ab_init(&frame);
    editor_render(&e, &frame);
    check(e.rowoff == 3, "test_scrolled_offset");
    check(frame_has(&frame, "golf line") &&
          !frame_has(&frame, "alpha line"),
          "test_scrolled_frame");
    ab_free(&frame);

    /* --- past the end of the file: tilde rows --- */
    editor_free(&e);
    editor_init(&e);
    editor_open(&e, "duck_test.txt");
    e.screen_rows = 12;
    e.screen_cols = 30;
    ab_init(&frame);
    editor_render(&e, &frame);
    check(frame_has(&frame, "~"), "test_tilde_rows");
    ab_free(&frame);

    /* --- editing through the input pipeline --- */
    feed_str(&e, "\x1b[4~");               /* End (tilde form) */
    check(e.cursor_col == 10, "test_end_key_via_bytes");
    feed_str(&e, " EDITED");
    check(strcmp(editor_get_line(&e, 0), "alpha line EDITED") == 0,
          "test_typed_text");
    check(e.dirty == 1, "test_editing_dirties");
    ab_init(&frame);
    editor_render(&e, &frame);
    check(frame_has(&frame, "alpha line EDITED"), "test_edit_rendered");
    check(frame_has(&frame, "[+]"), "test_dirty_marker_rendered");
    ab_free(&frame);

    /* --- Ctrl+S persists to disk --- */
    check(feed_str(&e, "\x13") == ED_CONTINUE, "test_save_continues");
    check(e.dirty == 0, "test_save_clears_dirty");
    long n = read_file("duck_test.txt", buf, sizeof(buf) - 1);
    check(n > 0 && strncmp(buf, "alpha line EDITED\nbravo line\n", 29) == 0,
          "test_save_hit_disk");

    /* --- quit dance --- */
    check(feed_str(&e, "\x11") == ED_QUIT, "test_clean_quit");
    feed_str(&e, "xxx");                    /* dirty it again */
    check(e.dirty == 1, "test_dirty_again");
    check(feed_str(&e, "\x11") == ED_CONTINUE, "test_dirty_quit_armed");
    check(feed_str(&e, "\x11") == ED_QUIT, "test_dirty_quit_confirmed");

    /* --- quit stops processing mid-buffer --- */
    editor_free(&e);
    editor_init(&e);
    strcpy(e.filename, "duck_unused.txt");
    check(feed_str(&e, "\x11" "zzz") == ED_QUIT,
          "test_quit_stops_processing");
    check(strcmp(editor_get_line(&e, 0), "") == 0,
          "test_no_typing_after_quit");

    /* --- tabs render expanded --- */
    editor_free(&e);
    write_file("duck_tabs.txt", "\tx\n");
    editor_init(&e);
    editor_open(&e, "duck_tabs.txt");
    e.screen_rows = 4;
    e.screen_cols = 30;
    ab_init(&frame);
    editor_render(&e, &frame);
    check(frame_has(&frame, "        x"), "test_tab_expanded_in_frame");
    check(!frame_has(&frame, "\tx"), "test_no_raw_tab_in_frame");
    ab_free(&frame);

    /* --- a whole scripted session, end to end --- */
    editor_free(&e);
    unlink("duck_session.txt");
    editor_init(&e);
    editor_open(&e, "duck_session.txt");   /* new file */
    e.screen_rows = 4;
    e.screen_cols = 40;
    feed_str(&e, "first line\r");
    feed_str(&e, "second line");
    feed_str(&e, "\x1b[H");                 /* Home */
    feed_str(&e, ">> ");
    check(feed_str(&e, "\x13") == ED_CONTINUE, "test_session_save");
    n = read_file("duck_session.txt", buf, sizeof(buf) - 1);
    check(n > 0 && strcmp(buf, "first line\n>> second line\n") == 0,
          "test_session_file_content");
    check(feed_str(&e, "\x11") == ED_QUIT, "test_session_quit");
    editor_free(&e);

    return failed;
}
```
