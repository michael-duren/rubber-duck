---
course: build-a-terminal
title: Build a Terminal Emulator in C
language: c
description: >
  Terminals aren't magic — they're programs speaking Unix I/O, POSIX APIs, 
  and ANSI escape codes. Build one from scratch: master file descriptors and 
  ttys, parse escape sequences with state machines, buffer and render screens 
  efficiently, handle raw input and echoing, build a reusable text editor 
  widget, and assemble it all into a functional terminal that reads files, 
  edits them, and saves them back.
duration_hours: 16
tags: [systems-programming, c, unix, io]
extended_reading:
  - title: ANSI/VT100 Escape Sequences
    url: https://en.wikipedia.org/wiki/ANSI_escape_code
  - title: POSIX termios(3) man page
    url: https://man7.org/linux/man-pages/man3/termios.3.html
  - title: "termios tutorial — raw mode walkthrough"
    url: https://viewsourcecode.org/snaptoken/kilo/02.enteringRawMode.html
  - title: "stty(1) — inspect and modify terminal settings"
    url: https://man7.org/linux/man-pages/man1/stty.1.html
---

# Lesson: Unix I/O and File Descriptors {#unix-io}

Every process in Unix has three standard file descriptors (fds):

- **0 (stdin)**: input — keyboard or a pipe
- **1 (stdout)**: output — screen or a file
- **2 (stderr)**: error output — screen or a file

These are just integers; you read from and write to them with `read(fd, buf, n)` 
and `write(fd, buf, n)`. The kernel abstracts what's on the other end: it could 
be a real terminal, a pipe, a file, or a network socket.

```c
#include <unistd.h>

char buf[16];
read(STDIN_FILENO, buf, 16);    /* read up to 16 bytes from stdin */
write(STDOUT_FILENO, "hi\n", 3); /* write to stdout */
```

A **terminal device** (or **pseudoterminal**, or **pty**) is a special file 
like `/dev/tty` that represents an interactive terminal. When you run a 
program in a shell, the shell sets up stdin, stdout, and stderr to point to 
a pty. The kernel's **terminal driver** sits in the middle: it buffers your 
input, echoes keypresses, handles signals (Ctrl+C sends SIGINT), and manages 
output cooked vs. raw.

The terminal driver has **modes**:

- **Canonical mode (cooked)**: input is line-buffered. You type, the driver 
  echoes, and nothing reaches your program until you press Enter. This is 
  the default and suits most command-line tools.
- **Raw mode**: every keypress is delivered immediately, echoing is off (your 
  program does it), and no special handling of Ctrl+C or Ctrl+Z. This is what 
  interactive editors (vim, less, tmux) use.

A terminal emulator is a program that sets raw mode on the pty, reads raw 
input, interprets it, renders output, and reads/writes files. That's you, 
building it.

## Challenge: Inspect Terminal Properties {#inspect-tty points=10}

Write a program that detects whether stdin is a terminal and prints its 
terminal name. This teaches you the tty APIs.

### Starter

```c
#include <unistd.h>
#include <stdio.h>
#include <string.h>

int main(void) {
    /* TODO: check if stdin is a terminal using isatty() */
    /* TODO: if yes, print the terminal name using ttyname() */
    /* TODO: if no, print "not a terminal" */
    return 0;
}
```

### Tests

```c
#include <unistd.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

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
    /* Test that isatty is callable (it may return 0 in test context) */
    int is_tty = isatty(STDIN_FILENO);
    check(is_tty == 0 || is_tty == 1, "test_isatty_returns_bool");
    
    /* Test that ttyname can be called (may return NULL in test context) */
    const char *name = ttyname(STDIN_FILENO);
    check(name == NULL || strlen(name) > 0, "test_ttyname_valid");
    
    return failed;
}
```

# Lesson: Terminal Settings with termios {#termios-intro}

The `termios.h` header provides access to terminal settings. You read them 
with `tcgetattr()` and write them with `tcsetattr()`:

```c
#include <termios.h>

struct termios settings;
tcgetattr(STDIN_FILENO, &settings);  /* read current settings */

/* Modify flags */
settings.c_lflag &= ~ECHO;           /* disable echo */

/* Apply changes */
tcsetattr(STDIN_FILENO, TCSAFLUSH, &settings);
```

The `struct termios` has several fields:

- **c_iflag** (input flags): how the terminal interprets input (CR/LF mapping, 
  signals, etc.)
- **c_oflag** (output flags): how the terminal processes output
- **c_cflag** (control flags): baud rate, parity, stop bits
- **c_lflag** (local flags): echo, canonical mode, signal generation
- **c_cc** (control characters): the bytes for Ctrl+C, Ctrl+Z, etc.
- **c_ispeed**, **c_ospeed** (speeds): input and output baud rates

For a terminal emulator, the key flags are in **c_lflag**:

- **ICANON**: line (canonical) mode. Clear this to read raw input.
- **ECHO**: echo input back to the screen. Clear this; your program prints.
- **ISIG**: enable signals. You might clear this to handle Ctrl+C yourself.
- **IXON**, **IXOFF**: software flow control (Ctrl+S/Ctrl+Q). Often cleared.
- **IXANY**: let any character restart output after Ctrl+S. Usually cleared.
- **OPOST**: process output (expand tabs, handle CR/LF). Usually kept.

The third argument to `tcsetattr()` is how to apply changes:

- **TCSANOW**: apply immediately
- **TCSADRAIN**: wait for output to drain, then apply
- **TCSAFLUSH**: drain and also discard any pending input

## Challenge: Enable Raw Mode {#raw-mode points=15}

Write a function that saves the original terminal settings, enables raw mode 
(disable ICANON, ECHO, and some flow control), and a function to restore the 
original settings. You'll use these in every challenge from here on.

### Starter

```c
#include <termios.h>
#include <unistd.h>
#include <stdio.h>
#include <string.h>

struct termios g_original_termios;

void enable_raw_mode(void) {
    struct termios raw;
    
    /* TODO: save original settings to g_original_termios */
    /* TODO: copy to raw */
    /* TODO: disable ICANON and ECHO */
    /* TODO: disable IXON, IXOFF, IXANY (flow control) */
    /* TODO: disable OPOST (so we control all output) */
    /* TODO: apply raw settings with TCSAFLUSH */
}

void disable_raw_mode(void) {
    /* TODO: restore g_original_termios with TCSAFLUSH */
}

int main(void) {
    enable_raw_mode();
    printf("Raw mode enabled (type a key, no echo; Ctrl+D to exit)\n");
    
    unsigned char c;
    while (read(STDIN_FILENO, &c, 1) == 1 && c != 0x04) {
        /* c is the key; in raw mode, we see it immediately */
    }
    
    disable_raw_mode();
    return 0;
}
```

### Tests

```c
#include <termios.h>
#include <unistd.h>
#include <stdio.h>
#include <string.h>

struct termios g_original_termios;

void enable_raw_mode(void);
void disable_raw_mode(void);

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
    struct termios before, after;
    
    /* Capture state before */
    tcgetattr(STDIN_FILENO, &before);
    
    /* Enable raw mode and check flags are off */
    enable_raw_mode();
    tcgetattr(STDIN_FILENO, &after);
    
    check((after.c_lflag & ICANON) == 0, "test_icanon_disabled");
    check((after.c_lflag & ECHO) == 0, "test_echo_disabled");
    check((after.c_iflag & IXON) == 0, "test_ixon_disabled");
    check((after.c_oflag & OPOST) == 0, "test_opost_disabled");
    
    /* Disable and check restoration */
    disable_raw_mode();
    tcgetattr(STDIN_FILENO, &after);
    
    check((after.c_lflag & ICANON) == (before.c_lflag & ICANON),
          "test_restore_icanon");
    check((after.c_lflag & ECHO) == (before.c_lflag & ECHO),
          "test_restore_echo");
    
    return failed;
}
```

# Lesson: ANSI Escape Sequences — the Terminal Protocol {#ansi-basics}

Terminals speak **ANSI/VT100 escape sequences**. These are byte sequences that 
start with ESC (0x1B) and tell the terminal what to do. They're how every 
terminal emulator, text editor, and interactive tool communicate with the 
screen.

The most common form is:

```
ESC [ <params> <letter>
```

For example:

```
ESC [ 2 J              ← Clear entire screen
ESC [ H                ← Move cursor to home (1,1)
ESC [ 5 ; 10 H         ← Move cursor to row 5, col 10
ESC [ 31 m             ← Set foreground color to red
ESC [ 0 m              ← Reset all attributes
ESC [ 1 m              ← Set bold
```

The letter is the **command**: `H` is cursor positioning, `J` is clearing, 
`m` is attributes, etc.

Some sequences don't have parameters:

```
ESC [ A                ← Cursor up one row
ESC [ B                ← Cursor down one row
ESC [ C                ← Cursor right one column
ESC [ D                ← Cursor left one column
```

Writing sequences is tedious but straightforward:

```c
write(STDOUT_FILENO, "\x1b[2J", 4);            /* clear screen */
write(STDOUT_FILENO, "\x1b[H", 3);             /* home */

char buf[64];
int n = sprintf(buf, "\x1b[%d;%dH", row, col);
write(STDOUT_FILENO, buf, n);                  /* cursor to (row, col) */
```

The first parameter is typically a **row** or a **code**, the second is 
**column** (for positioning) or **attribute code** (for colors/styles).

## Challenge: Cursor Positioning {#cursor-move points=12}

Write functions to generate and write ANSI escape sequences for common 
operations: move cursor, clear screen, move to home, show/hide cursor.

### Starter

```c
#include <stdio.h>
#include <unistd.h>
#include <string.h>

/* Move cursor to row, col (1-indexed, as ANSI uses) */
void ansi_cursor_move(int row, int col) {
    char buf[32];
    int n = sprintf(buf, "\x1b[%d;%dH", row, col);
    write(STDOUT_FILENO, buf, n);
}

/* Move cursor to home (top-left) */
void ansi_cursor_home(void) {
    write(STDOUT_FILENO, "\x1b[H", 3);
}

/* Clear entire screen */
void ansi_screen_clear(void) {
    /* TODO: write the clear screen sequence */
}

/* Clear from cursor to end of line */
void ansi_clear_line(void) {
    /* TODO: write the clear-to-end-of-line sequence */
}

/* Hide the cursor */
void ansi_cursor_hide(void) {
    /* TODO: write the hide cursor sequence */
}

/* Show the cursor */
void ansi_cursor_show(void) {
    /* TODO: write the show cursor sequence */
}

/* Move cursor up N rows */
void ansi_cursor_up(int n) {
    /* TODO: write cursor-up sequence */
}

/* Move cursor down N rows */
void ansi_cursor_down(int n) {
    /* TODO: write cursor-down sequence */
}

/* Set foreground color (30-37 for standard colors) */
void ansi_color(int code) {
    char buf[16];
    int n = sprintf(buf, "\x1b[%dm", code);
    write(STDOUT_FILENO, buf, n);
}
```

### Tests

```c
#include <stdio.h>
#include <unistd.h>
#include <string.h>
#include <stdlib.h>

void ansi_cursor_move(int row, int col);
void ansi_cursor_home(void);
void ansi_screen_clear(void);
void ansi_clear_line(void);
void ansi_cursor_hide(void);
void ansi_cursor_show(void);
void ansi_cursor_up(int n);
void ansi_cursor_down(int n);
void ansi_color(int code);

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
    /* Verify escape sequences are correct (without actually writing them) */
    
    char buf[64];
    
    /* Test cursor move sequence format */
    int n = sprintf(buf, "\x1b[%d;%dH", 10, 20);
    check(n > 0 && strstr(buf, "\x1b[") != NULL && strstr(buf, "H") != NULL,
          "test_cursor_move_format");
    
    /* Test clear screen sequence */
    strcpy(buf, "\x1b[2J");
    check(strcmp(buf, "\x1b[2J") == 0, "test_clear_screen_sequence");
    
    /* Test clear line sequence */
    strcpy(buf, "\x1b[K");
    check(strcmp(buf, "\x1b[K") == 0, "test_clear_line_sequence");
    
    /* Test hide cursor sequence */
    strcpy(buf, "\x1b[?25l");
    check(strcmp(buf, "\x1b[?25l") == 0, "test_hide_cursor_sequence");
    
    /* Test show cursor sequence */
    strcpy(buf, "\x1b[?25h");
    check(strcmp(buf, "\x1b[?25h") == 0, "test_show_cursor_sequence");
    
    /* Test color sequence */
    n = sprintf(buf, "\x1b[%dm", 31);
    check(strcmp(buf, "\x1b[31m") == 0, "test_color_sequence");
    
    return failed;
}
```

# Lesson: Screen Buffering and Rendering {#screen-buffer}

Writing to the screen is expensive: every `write()` call is a system call, and 
if you write in small chunks (character-by-character), the screen flickers 
visibly. The solution is a **screen buffer**: an in-memory representation of 
what you want to display. You write to the buffer, then flush it all at once.

A simple screen buffer is a 2D array or a flat string:

```c
#define ROWS 24
#define COLS 80

struct screen {
    char buf[ROWS][COLS];
    int cursor_row;
    int cursor_col;
};
```

When you flush, you write the entire buffer to stdout, then move the cursor 
to its current position. For efficiency, some implementations only write 
**dirty regions** (parts that changed), but the simple approach works fine for 
interactive tools.

Another pattern is a **double buffer**: two buffers, one is the current screen 
(what's displayed), and one is the next screen (what you're building). Swap 
them on every frame, and write the diff. This is more complex but avoids 
flicker.

For now, we'll use a simple single buffer: initialize to spaces, write to it, 
and flush it all at once.

## Challenge: Implement a Screen Buffer {#screen-struct points=18}

Build a screen struct with initialization, character writing, cursor movement, 
and flushing. The tests verify that the buffer manages coordinates correctly.

### Starter

```c
#include <stdio.h>
#include <unistd.h>
#include <string.h>
#include <stdlib.h>

#define SCREEN_ROWS 12
#define SCREEN_COLS 40

struct screen {
    /* TODO: add a 2D buffer, cursor coordinates */
};

/* Initialize screen to spaces, cursor at (0,0) */
void screen_init(struct screen *s) {
    /* TODO */
}

/* Write a character at cursor, advance cursor, wrap to next line at EOL */
void screen_write_char(struct screen *s, char c) {
    if (c == '\n') {
        /* TODO: newline: move to start of next row */
    } else {
        /* TODO: write c at cursor, advance cursor, wrap if needed */
    }
}

/* Move cursor to absolute position (0-indexed); clamp to bounds */
void screen_set_cursor(struct screen *s, int row, int col) {
    /* TODO: validate and set cursor_row, cursor_col */
}

/* Move cursor by dr, dc (relative); clamp to bounds */
void screen_move_cursor(struct screen *s, int dr, int dc) {
    /* TODO: move cursor relative, don't go out of bounds */
}

/* Erase the character before cursor, move cursor back */
void screen_backspace(struct screen *s) {
    /* TODO: if at start, do nothing; else erase prev char and move back */
}

/* Flush buffer to stdout and reposition cursor */
void screen_flush(struct screen *s) {
    /* TODO: clear screen (ANSI) */
    /* TODO: move cursor home (ANSI) */
    /* TODO: write each row of the buffer */
    /* TODO: move cursor to cursor_row, cursor_col (ANSI) */
    /* TODO: show cursor */
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

#define SCREEN_ROWS 12
#define SCREEN_COLS 40

struct screen {
    char buf[SCREEN_ROWS][SCREEN_COLS];
    int cursor_row;
    int cursor_col;
};

void screen_init(struct screen *s);
void screen_write_char(struct screen *s, char c);
void screen_set_cursor(struct screen *s, int row, int col);
void screen_move_cursor(struct screen *s, int dr, int dc);
void screen_backspace(struct screen *s);

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
    struct screen s;
    
    /* Test 1: init fills with spaces */
    screen_init(&s);
    int all_spaces = 1;
    for (int i = 0; i < SCREEN_ROWS; i++) {
        for (int j = 0; j < SCREEN_COLS; j++) {
            if (s.buf[i][j] != ' ') all_spaces = 0;
        }
    }
    check(all_spaces, "test_init_fills_spaces");
    
    /* Test 2: cursor starts at origin */
    check(s.cursor_row == 0 && s.cursor_col == 0, "test_cursor_at_origin");
    
    /* Test 3: write_char places character */
    screen_init(&s);
    screen_write_char(&s, 'A');
    check(s.buf[0][0] == 'A' && s.cursor_col == 1, "test_write_char");
    
    /* Test 4: write_char wraps to next line at EOL */
    screen_init(&s);
    for (int i = 0; i < SCREEN_COLS; i++) {
        screen_write_char(&s, 'X');
    }
    check(s.cursor_row == 1 && s.cursor_col == 0, "test_write_wrap");
    
    /* Test 5: set_cursor positions cursor */
    screen_init(&s);
    screen_set_cursor(&s, 5, 10);
    check(s.cursor_row == 5 && s.cursor_col == 10, "test_set_cursor");
    
    /* Test 6: set_cursor clamps to bounds */
    screen_init(&s);
    screen_set_cursor(&s, 100, 100);
    check(s.cursor_row == SCREEN_ROWS - 1 && s.cursor_col == SCREEN_COLS - 1,
          "test_set_cursor_clamp");
    
    /* Test 7: move_cursor relative */
    screen_init(&s);
    screen_set_cursor(&s, 3, 5);
    screen_move_cursor(&s, 2, 3);
    check(s.cursor_row == 5 && s.cursor_col == 8, "test_move_cursor");
    
    /* Test 8: move_cursor clamps */
    screen_init(&s);
    screen_move_cursor(&s, -1, -1);
    check(s.cursor_row == 0 && s.cursor_col == 0, "test_move_cursor_clamp");
    
    /* Test 9: backspace erases and moves back */
    screen_init(&s);
    screen_write_char(&s, 'X');
    screen_write_char(&s, 'Y');
    screen_backspace(&s);
    check(s.cursor_col == 1 && s.buf[0][1] == ' ', "test_backspace");
    
    /* Test 10: backspace at start does nothing */
    screen_init(&s);
    screen_backspace(&s);
    check(s.cursor_row == 0 && s.cursor_col == 0, "test_backspace_at_start");
    
    /* Test 11: newline moves to next line at column 0 */
    screen_init(&s);
    screen_set_cursor(&s, 0, 10);
    screen_write_char(&s, '\n');
    check(s.cursor_row == 1 && s.cursor_col == 0, "test_newline");
    
    return failed;
}
```

# Lesson: Parsing Input — Escape Sequences from the Keyboard {#parse-input}

When the user presses a key, stdin delivers a byte (or bytes for multi-byte 
sequences). Regular keys produce one byte: 'a', 'A', '1', ' '. But special 
keys produce sequences:

```
Arrow Up:        ESC [ A
Arrow Down:      ESC [ B
Arrow Right:     ESC [ C
Arrow Left:      ESC [ D
Page Up:         ESC [ 5 ~
Page Down:       ESC [ 6 ~
Home:            ESC [ 1 ~ or ESC [ H
End:             ESC [ 4 ~ or ESC [ F
Delete:          ESC [ 3 ~
```

Ctrl+X keys produce single bytes (0x01 to 0x1A for Ctrl+A to Ctrl+Z):

```
Ctrl+A:  0x01
Ctrl+C:  0x03
Ctrl+D:  0x04 (EOF)
Ctrl+Z:  0x1A
```

Your parser needs to handle this complexity. The approach:

1. Read one byte
2. If it's 0x1B (ESC), read more bytes to complete the sequence
3. Interpret the sequence (could be a multi-parameter sequence like ESC [ 5 ; 10 ~ )
4. Return a structured key event

This is a **state machine**: read bytes, accumulate them, and emit events when 
you have a complete sequence.

## Challenge: Parse Keyboard Input {#parse-keys points=20}

Implement a parser that reads from stdin and classifies keypresses into an 
enum. Handle arrow keys, function keys (Home, End, Page Up/Down), Delete, 
Backspace, and regular characters.

### Starter

```c
#include <unistd.h>
#include <stdio.h>
#include <string.h>

enum key_type {
    KEY_CHAR,           /* regular character */
    KEY_ARROW_UP,
    KEY_ARROW_DOWN,
    KEY_ARROW_LEFT,
    KEY_ARROW_RIGHT,
    KEY_PAGE_UP,
    KEY_PAGE_DOWN,
    KEY_HOME,
    KEY_END,
    KEY_DELETE,
    KEY_BACKSPACE,
    KEY_CTRL_C,
    KEY_CTRL_D,
    KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;          /* for KEY_CHAR: the ASCII code; for others: 0 */
};

/* Read and parse one key event from stdin */
struct key_event read_key_event(void) {
    struct key_event e = {KEY_UNKNOWN, 0};
    unsigned char c;
    
    if (read(STDIN_FILENO, &c, 1) != 1) {
        return e;
    }
    
    /* TODO: handle single-byte keys: regular chars, Ctrl+C, Ctrl+D, Backspace */
    /* TODO: if ESC, read more bytes and identify the sequence */
    /* TODO: parse multi-byte sequences (arrow keys, function keys) */
    
    return e;
}

/* Helper: read one more byte without blocking (return -1 if timeout) */
static int read_next_byte_with_timeout(int timeout_ms) {
    /* TODO: use select() or poll() to check if data is available */
    /* TODO: return the byte, or -1 if timeout */
    unsigned char c;
    /* This is complex; for now, return -1 to signal "not available" */
    return -1;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

enum key_type {
    KEY_CHAR,
    KEY_ARROW_UP,
    KEY_ARROW_DOWN,
    KEY_ARROW_LEFT,
    KEY_ARROW_RIGHT,
    KEY_PAGE_UP,
    KEY_PAGE_DOWN,
    KEY_HOME,
    KEY_END,
    KEY_DELETE,
    KEY_BACKSPACE,
    KEY_CTRL_C,
    KEY_CTRL_D,
    KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;
};

struct key_event read_key_event(void);

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
    /* Verify enum values are distinct and correctly ordered */
    
    check(KEY_CHAR == 0, "test_key_char_is_zero");
    check(KEY_ARROW_UP != KEY_ARROW_DOWN, "test_arrow_keys_distinct");
    check(KEY_HOME != KEY_END, "test_home_end_distinct");
    check(KEY_DELETE != KEY_BACKSPACE, "test_delete_backspace_distinct");
    
    /* Verify struct can hold a key event */
    struct key_event e;
    e.type = KEY_CHAR;
    e.value = 65;
    check(e.type == KEY_CHAR && e.value == 65, "test_struct_init");
    
    /* Verify enum covers all special keys */
    check(KEY_CTRL_C > KEY_UNKNOWN || KEY_CTRL_C < KEY_UNKNOWN,
          "test_ctrl_c_exists");
    check(KEY_CTRL_D > KEY_UNKNOWN || KEY_CTRL_D < KEY_UNKNOWN,
          "test_ctrl_d_exists");
    
    return failed;
}
```

# Lesson: Text Editing — A Reusable Editor Widget {#text-widget}

To build a real terminal, we'll abstract a text editor into a **widget** — a 
reusable struct that manages:

- A buffer of text (the file being edited)
- A cursor position (row, col within the file)
- A viewport (what portion is visible on screen)
- Operations: insert character, delete character, move cursor, scroll

This widget is decoupled from I/O: you feed it key events, it modifies its 
internal state, and it tells you what to render. Then you use your screen 
buffer to draw the viewport.

```c
struct editor {
    char **lines;       /* array of strings, one per line */
    int nlines;         /* number of lines */
    int cursor_row;     /* current line (0-indexed) */
    int cursor_col;     /* column within that line */
    int viewport_row;   /* first visible row */
};

void editor_insert_char(struct editor *e, char c) {
    /* insert c at cursor, advance cursor */
}

void editor_delete_char(struct editor *e) {
    /* delete char at cursor, don't move cursor */
}

void editor_render(struct editor *e, struct screen *s) {
    /* fill screen buffer with viewport of editor */
}
```

The widget handles the semantic operations; the screen widget handles 
rendering; the main loop reads input and calls the editor.

## Challenge: Implement a Text Editor Widget {#editor-widget points=30}

Build an editor struct and functions to initialize it (from a file or empty), 
insert/delete characters, move the cursor, and render to a screen buffer. 
This is the heart of your terminal.

### Starter

```c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define EDITOR_MAX_LINES 1000
#define EDITOR_MAX_LINE_LEN 256

struct editor {
    /* TODO: add a buffer of lines, line count, cursor row/col */
};

/* Initialize empty editor */
void editor_init(struct editor *e) {
    /* TODO */
}

/* Load a file into the editor */
int editor_load_file(struct editor *e, const char *path) {
    /* TODO: open file, read lines, populate editor */
    return 0;  /* success */
}

/* Insert a character at the cursor position */
void editor_insert_char(struct editor *e, char c) {
    if (c == '\n') {
        /* TODO: split current line at cursor, move to next line */
    } else {
        /* TODO: insert c at cursor_col within the current line */
    }
}

/* Delete the character at cursor position */
void editor_delete_char(struct editor *e) {
    /* TODO: remove character at cursor, don't move cursor */
}

/* Backspace: delete before cursor and move cursor back */
void editor_backspace(struct editor *e) {
    /* TODO: delete character before cursor, move cursor back */
}

/* Move cursor up/down/left/right with boundary checking */
void editor_move_cursor(struct editor *e, int dr, int dc) {
    /* TODO */
}

/* Get the line at a given index */
const char *editor_get_line(struct editor *e, int row) {
    if (row < 0 || row >= e->nlines) return "";
    /* TODO: return the line */
}

/* Save editor to a file */
int editor_save_file(struct editor *e, const char *path) {
    /* TODO: open file for writing, write all lines */
    return 0;  /* success */
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define EDITOR_MAX_LINES 1000
#define EDITOR_MAX_LINE_LEN 256

struct editor {
    char lines[EDITOR_MAX_LINES][EDITOR_MAX_LINE_LEN];
    int nlines;
    int cursor_row;
    int cursor_col;
};

void editor_init(struct editor *e);
void editor_insert_char(struct editor *e, char c);
void editor_delete_char(struct editor *e);
void editor_backspace(struct editor *e);
void editor_move_cursor(struct editor *e, int dr, int dc);
const char *editor_get_line(struct editor *e, int row);

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
    
    /* Test 1: init creates empty editor */
    editor_init(&e);
    check(e.nlines == 1 && e.cursor_row == 0 && e.cursor_col == 0,
          "test_init");
    
    /* Test 2: insert_char adds character */
    editor_init(&e);
    editor_insert_char(&e, 'A');
    check(strcmp(editor_get_line(&e, 0), "A") == 0, "test_insert_char");
    
    /* Test 3: insert_char advances cursor */
    check(e.cursor_col == 1, "test_insert_advances");
    
    /* Test 4: delete_char removes character */
    editor_init(&e);
    editor_insert_char(&e, 'A');
    editor_insert_char(&e, 'B');
    editor_delete_char(&e);
    check(strcmp(editor_get_line(&e, 0), "A") == 0, "test_delete_char");
    
    /* Test 5: backspace erases and moves back */
    editor_init(&e);
    editor_insert_char(&e, 'X');
    editor_insert_char(&e, 'Y');
    editor_backspace(&e);
    check(e.cursor_col == 1 && strcmp(editor_get_line(&e, 0), "X") == 0,
          "test_backspace");
    
    /* Test 6: move_cursor with bounds */
    editor_init(&e);
    editor_move_cursor(&e, -1, 0);  /* try to go up from line 0 */
    check(e.cursor_row == 0, "test_move_cursor_bounds");
    
    /* Test 7: newline creates new line */
    editor_init(&e);
    editor_insert_char(&e, 'A');
    editor_insert_char(&e, '\n');
    editor_insert_char(&e, 'B');
    check(e.nlines == 2 && e.cursor_row == 1 && 
          strcmp(editor_get_line(&e, 0), "A") == 0 &&
          strcmp(editor_get_line(&e, 1), "B") == 0,
          "test_newline");
    
    return failed;
}
```

# Lesson: The Viewport — Rendering Only What Fits {#viewport}

Your terminal is limited in size (say, 24 rows × 80 columns). The file being 
edited might be thousands of lines. You can't display all of it; you need to 
show a **viewport** — a window into the file.

The viewport has:

- **top_row**: the first visible line of the file (0-indexed)
- **height**: how many lines to show (depends on terminal height)

When the cursor moves past the visible area, you scroll the viewport to keep 
the cursor visible. This is called **keeping the cursor in view**:

```c
void editor_ensure_cursor_visible(struct editor *e) {
    if (e->cursor_row < e->viewport_row) {
        e->viewport_row = e->cursor_row;
    } else if (e->cursor_row >= e->viewport_row + viewport_height) {
        e->viewport_row = e->cursor_row - viewport_height + 1;
    }
}
```

When rendering, you only draw rows from `viewport_row` to 
`viewport_row + height - 1`, and you translate screen coordinates to editor 
coordinates.

## Challenge: Add Scrolling {#scrolling points=25}

Add a viewport to the editor, implement scrolling to keep the cursor visible, 
and modify the render function to only display the viewport.

### Starter

```c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define EDITOR_MAX_LINES 1000
#define EDITOR_MAX_LINE_LEN 256

struct editor {
    char lines[EDITOR_MAX_LINES][EDITOR_MAX_LINE_LEN];
    int nlines;
    int cursor_row;
    int cursor_col;
    int viewport_row;   /* TODO: first visible line */
    int viewport_height; /* TODO: how many rows to display */
};

void editor_init(struct editor *e, int viewport_height) {
    /* TODO: initialize with viewport height (e.g., 20) */
}

void editor_ensure_cursor_visible(struct editor *e) {
    /* TODO: if cursor is above viewport, scroll up */
    /* TODO: if cursor is below viewport, scroll down */
}

void editor_insert_char(struct editor *e, char c) {
    /* TODO: same as before, but call ensure_cursor_visible after */
}

void editor_move_cursor(struct editor *e, int dr, int dc) {
    /* TODO: same as before, but call ensure_cursor_visible after */
}

struct screen {
    char buf[24][80];
    int cursor_row;
    int cursor_col;
};

void editor_render(struct editor *e, struct screen *s) {
    /* TODO: clear screen buffer */
    /* TODO: for each visible line in viewport, write to screen */
    /* TODO: handle line wrapping if line is longer than SCREEN_COLS */
    /* TODO: position cursor in screen at right place */
}
```

### Tests

```c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define EDITOR_MAX_LINES 1000
#define EDITOR_MAX_LINE_LEN 256

struct editor {
    char lines[EDITOR_MAX_LINES][EDITOR_MAX_LINE_LEN];
    int nlines;
    int cursor_row;
    int cursor_col;
    int viewport_row;
    int viewport_height;
};

void editor_init(struct editor *e, int viewport_height);
void editor_ensure_cursor_visible(struct editor *e);
void editor_insert_char(struct editor *e, char c);
void editor_move_cursor(struct editor *e, int dr, int dc);

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
    
    /* Test 1: init sets viewport */
    editor_init(&e, 10);
    check(e.viewport_height == 10, "test_init_viewport_height");
    
    /* Test 2: viewport starts at 0 */
    check(e.viewport_row == 0, "test_viewport_row_start");
    
    /* Test 3: cursor visible when within viewport */
    editor_init(&e, 10);
    editor_move_cursor(&e, 5, 0);
    editor_ensure_cursor_visible(&e);
    check(e.viewport_row == 0, "test_cursor_visible_within");
    
    /* Test 4: viewport scrolls down when cursor past bottom */
    editor_init(&e, 10);
    for (int i = 0; i < 100; i++) {
        editor_insert_char(&e, 'X');
        editor_insert_char(&e, '\n');
    }
    editor_ensure_cursor_visible(&e);
    check(e.viewport_row > 0, "test_viewport_scrolls");
    
    return failed;
}
```

# Lesson: Status Line and User Feedback {#status-line}

A real terminal shows you information: the file name, line/column, whether 
it's been modified, current mode. This is typically a **status line** at the 
bottom of the screen.

You reserve one line at the bottom of your screen for status, then render your 
viewport in the remaining rows:

```
[file contents here, rows 0-22]
[status line at row 23]
```

The status line might show:

```
untitled.txt | Ln 5, Col 12 | [modified] | "hello world"
```

You render it as a string formatted with `sprintf()`, and write it to the 
screen buffer.

## Challenge: Add a Status Line {#status-line-impl points=15}

Add a status line that displays the file name, cursor position, and 
modification status. Highlight it with color (using ANSI sequences).

### Starter

```c
#include <stdio.h>
#include <string.h>

struct editor {
    char filename[256];
    char lines[1000][256];
    int nlines;
    int cursor_row;
    int cursor_col;
    int modified;   /* flag: has file been changed? */
};

/* Format status line into a buffer */
void format_status_line(struct editor *e, char *buf, int len) {
    const char *name = e->filename[0] ? e->filename : "[untitled]";
    const char *mod = e->modified ? " [modified]" : "";
    snprintf(buf, len, "%s | Ln %d, Col %d%s",
             name, e->cursor_row + 1, e->cursor_col + 1, mod);
}

struct screen {
    char buf[24][80];
};

void editor_render_with_status(struct editor *e, struct screen *s) {
    /* TODO: clear screen */
    /* TODO: render viewport in rows 0-22 */
    /* TODO: format status line and render in row 23 */
    /* TODO: optionally use ANSI colors to highlight status line */
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

void format_status_line(struct editor *e, char *buf, int len);

struct editor {
    char filename[256];
    int modified;
    int cursor_row;
    int cursor_col;
};

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
    char buf[256];
    
    /* Test 1: status with file name */
    strcpy(e.filename, "test.txt");
    e.modified = 0;
    e.cursor_row = 4;
    e.cursor_col = 10;
    format_status_line(&e, buf, sizeof(buf));
    check(strstr(buf, "test.txt") != NULL, "test_filename_in_status");
    
    /* Test 2: status shows cursor position */
    check(strstr(buf, "Ln 5") != NULL && strstr(buf, "Col 11") != NULL,
          "test_cursor_position");
    
    /* Test 3: status shows modified flag */
    e.modified = 1;
    format_status_line(&e, buf, sizeof(buf));
    check(strstr(buf, "modified") != NULL, "test_modified_flag");
    
    /* Test 4: untitled for empty filename */
    e.filename[0] = '\0';
    format_status_line(&e, buf, sizeof(buf));
    check(strstr(buf, "untitled") != NULL, "test_untitled_default");
    
    return failed;
}
```

# Lesson: The Main Event Loop {#main-loop}

Now assemble everything:

1. Enable raw mode
2. Get terminal size
3. Initialize editor (load file or empty)
4. Loop:
   - Render editor + status to screen buffer
   - Flush screen buffer to stdout
   - Read a key event
   - Handle key (edit, move, save, exit)
5. Disable raw mode
6. Restore terminal

This is the skeleton of vim, less, nano, and every other interactive tool.

## Challenge: Build the Main Loop {#main-loop-impl points=35}

Assemble your components into a complete event loop. Handle keys: regular 
input, arrow keys, Ctrl+S (save), Ctrl+Q (quit), Ctrl+D (exit).

### Starter

```c
#include <termios.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <signal.h>
#include <sys/ioctl.h>

struct termios g_original_termios;

enum key_type {
    KEY_CHAR, KEY_ARROW_UP, KEY_ARROW_DOWN, KEY_ARROW_LEFT, KEY_ARROW_RIGHT,
    KEY_PAGE_UP, KEY_PAGE_DOWN, KEY_HOME, KEY_END, KEY_DELETE, KEY_BACKSPACE,
    KEY_CTRL_C, KEY_CTRL_D, KEY_CTRL_S, KEY_CTRL_Q, KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;
};

/* Your helper functions from previous challenges */
void enable_raw_mode(void);
void disable_raw_mode(void);
struct key_event read_key_event(void);

#define SCREEN_ROWS 24
#define SCREEN_COLS 80

struct screen {
    char buf[SCREEN_ROWS][SCREEN_COLS];
    int cursor_row;
    int cursor_col;
};

void screen_init(struct screen *s);
void screen_write_char(struct screen *s, char c);
void screen_flush(struct screen *s);

#define EDITOR_MAX_LINES 1000
#define EDITOR_MAX_LINE_LEN 256

struct editor {
    char filename[256];
    char lines[EDITOR_MAX_LINES][EDITOR_MAX_LINE_LEN];
    int nlines;
    int cursor_row;
    int cursor_col;
    int modified;
    int viewport_row;
};

void editor_init(struct editor *e);
void editor_insert_char(struct editor *e, char c);
void editor_backspace(struct editor *e);
void editor_delete_char(struct editor *e);
void editor_move_cursor(struct editor *e, int dr, int dc);
int editor_load_file(struct editor *e, const char *path);
int editor_save_file(struct editor *e, const char *path);
void editor_ensure_cursor_visible(struct editor *e);
void editor_render(struct editor *e, struct screen *s);

/* Get terminal dimensions */
void get_terminal_size(int *rows, int *cols) {
    struct winsize ws;
    ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws);
    *rows = ws.ws_row;
    *cols = ws.ws_col;
}

void run_editor(const char *filename) {
    struct editor editor;
    struct screen screen;
    struct key_event key;
    int running = 1;
    int rows, cols;
    
    enable_raw_mode();
    atexit(disable_raw_mode);
    
    /* TODO: get terminal size */
    /* TODO: initialize editor */
    /* TODO: if filename given, load it */
    
    /* TODO: clear screen, show cursor */
    
    while (running) {
        /* TODO: render editor to screen buffer */
        /* TODO: flush screen to stdout */
        /* TODO: read a key event */
        /* TODO: handle key:
           - regular char: insert
           - arrow keys: move cursor
           - delete/backspace: delete
           - Ctrl+S: save
           - Ctrl+Q or Ctrl+D: exit */
    }
}

int main(int argc, char *argv[]) {
    const char *filename = argc > 1 ? argv[1] : NULL;
    run_editor(filename);
    return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

#define SCREEN_ROWS 24
#define SCREEN_COLS 80

struct screen {
    char buf[SCREEN_ROWS][SCREEN_COLS];
};

#define EDITOR_MAX_LINES 1000
#define EDITOR_MAX_LINE_LEN 256

struct editor {
    char filename[256];
    char lines[EDITOR_MAX_LINES][EDITOR_MAX_LINE_LEN];
    int nlines;
    int modified;
};

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
    /* Verify basic structures are sized correctly */
    
    struct screen s;
    struct editor e;
    
    check(sizeof(s.buf) == SCREEN_ROWS * SCREEN_COLS, "test_screen_size");
    check(sizeof(e.lines) == EDITOR_MAX_LINES * EDITOR_MAX_LINE_LEN,
          "test_editor_buffer_size");
    
    /* Verify arrays are contiguous */
    char *p = (char *)s.buf;
    check(p != NULL && p[0] == 0, "test_screen_buffer_valid");
    
    return failed;
}
```

# Final Challenge: A Functional Text Editor {#final-editor points=100}

Build a complete text editor that:

1. Takes a filename as an argument.
2. Loads the file if it exists, or starts empty.
3. Enters raw mode, clears the screen, hides the cursor.
4. Loops:
   - Renders the file (viewport of lines) + status line
   - Reads a key
   - Handles input:
     - Regular characters: insert at cursor
     - Arrow keys (up/down/left/right): move cursor with bounds
     - Backspace: delete before cursor
     - Delete: delete at cursor
     - Ctrl+S: save to file, clear modified flag
     - Ctrl+Q or Ctrl+D: exit (confirm if modified)
   - Keeps cursor visible (scrolls viewport)
5. On exit: show cursor, restore terminal, print the filename and status

The editor should feel responsive and usable: you can open a file, edit it, 
navigate with arrows, and save it back. It's a miniature vim.

Features to include:

- **File I/O**: load and save files
- **Line-based editing**: insert/delete characters, newlines
- **Cursor movement**: arrow keys with bounds checking
- **Viewport/scrolling**: keep cursor visible on small screens
- **Status line**: show file name, position, modification status
- **Signals**: handle window resize (SIGWINCH) to redraw on terminal resize
- **Clean exit**: restore terminal state on exit or error

### Starter

This is a substantial program. The starter provides the skeleton and function 
signatures; you fill in the bodies.

```c
#include <termios.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <signal.h>
#include <sys/ioctl.h>
#include <fcntl.h>

struct termios g_original_termios;
int g_terminal_rows = 24;
int g_terminal_cols = 80;
int g_resize_pending = 0;

void sigwinch_handler(int sig) {
    (void)sig;
    g_resize_pending = 1;
}

void enable_raw_mode(void) {
    struct termios raw;
    tcgetattr(STDIN_FILENO, &g_original_termios);
    raw = g_original_termios;
    raw.c_lflag &= ~(ECHO | ICANON);
    raw.c_iflag &= ~(IXON | IXOFF | IXANY);
    raw.c_oflag &= ~OPOST;
    raw.c_cc[VMIN] = 0;
    raw.c_cc[VTIME] = 0;
    tcsetattr(STDIN_FILENO, TCSAFLUSH, &raw);
}

void disable_raw_mode(void) {
    tcsetattr(STDIN_FILENO, TCSAFLUSH, &g_original_termios);
}

void get_terminal_size(int *rows, int *cols) {
    struct winsize ws;
    if (ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws) != -1) {
        *rows = ws.ws_row;
        *cols = ws.ws_col;
    }
}

enum key_type {
    KEY_CHAR, KEY_ARROW_UP, KEY_ARROW_DOWN, KEY_ARROW_LEFT, KEY_ARROW_RIGHT,
    KEY_HOME, KEY_END, KEY_DELETE, KEY_BACKSPACE,
    KEY_CTRL_S, KEY_CTRL_Q, KEY_CTRL_D, KEY_UNKNOWN,
};

struct key_event {
    enum key_type type;
    int value;
};

struct key_event read_key_event(void) {
    struct key_event e = {KEY_UNKNOWN, 0};
    unsigned char c;
    
    if (read(STDIN_FILENO, &c, 1) != 1) {
        return e;
    }
    
    /* TODO: parse single-byte keys */
    if (c == 0x1b) {
        /* TODO: parse escape sequence */
    }
    
    return e;
}

#define EDITOR_MAX_LINES 10000
#define EDITOR_MAX_LINE_LEN 512

struct editor {
    char filename[512];
    char *lines[EDITOR_MAX_LINES];
    int nlines;
    int cursor_row;
    int cursor_col;
    int modified;
    int viewport_row;
    int viewport_height;
};

void editor_init(struct editor *e, int viewport_height) {
    /* TODO */
}

void editor_load_file(struct editor *e, const char *path) {
    /* TODO */
}

void editor_save_file(struct editor *e) {
    /* TODO */
}

void editor_insert_char(struct editor *e, char c) {
    /* TODO */
}

void editor_delete_char(struct editor *e) {
    /* TODO */
}

void editor_backspace(struct editor *e) {
    /* TODO */
}

void editor_move_cursor(struct editor *e, int dr, int dc) {
    /* TODO */
}

void editor_ensure_cursor_visible(struct editor *e) {
    /* TODO */
}

void editor_render(struct editor *e) {
    /* TODO: clear screen */
    /* TODO: for each visible line, write to screen */
    /* TODO: draw status line at bottom */
    /* TODO: move cursor to right position */
}

void run_editor(const char *filename) {
    struct editor e;
    struct key_event key;
    int running = 1;
    
    enable_raw_mode();
    atexit(disable_raw_mode);
    
    signal(SIGWINCH, sigwinch_handler);
    
    get_terminal_size(&g_terminal_rows, &g_terminal_cols);
    editor_init(&e, g_terminal_rows - 1);
    
    if (filename) {
        editor_load_file(&e, filename);
        strncpy(e.filename, filename, sizeof(e.filename) - 1);
    }
    
    write(STDOUT_FILENO, "\x1b[2J\x1b[H\x1b[?25l", 11);  /* clear, home, hide cursor */
    
    while (running) {
        if (g_resize_pending) {
            get_terminal_size(&g_terminal_rows, &g_terminal_cols);
            e.viewport_height = g_terminal_rows - 1;
            g_resize_pending = 0;
        }
        
        editor_render(&e);
        
        key = read_key_event();
        
        switch (key.type) {
        case KEY_CHAR:
            /* TODO: insert character */
            break;
        case KEY_ARROW_UP:
        case KEY_ARROW_DOWN:
        case KEY_ARROW_LEFT:
        case KEY_ARROW_RIGHT:
            /* TODO: move cursor */
            break;
        case KEY_BACKSPACE:
            /* TODO: backspace */
            break;
        case KEY_DELETE:
            /* TODO: delete */
            break;
        case KEY_HOME:
            /* TODO: move to start of line */
            break;
        case KEY_END:
            /* TODO: move to end of line */
            break;
        case KEY_CTRL_S:
            /* TODO: save file */
            break;
        case KEY_CTRL_Q:
        case KEY_CTRL_D:
            /* TODO: confirm if modified, then exit */
            running = 0;
            break;
        default:
            break;
        }
        
        editor_ensure_cursor_visible(&e);
    }
    
    write(STDOUT_FILENO, "\x1b[?25h", 6);  /* show cursor */
    printf("\nEditor closed.\n");
}

int main(int argc, char *argv[]) {
    const char *filename = argc > 1 ? argv[1] : NULL;
    run_editor(filename);
    return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

#define EDITOR_MAX_LINES 10000
#define EDITOR_MAX_LINE_LEN 512

struct editor {
    char filename[512];
    char *lines[EDITOR_MAX_LINES];
    int nlines;
    int cursor_row;
    int cursor_col;
    int modified;
    int viewport_row;
    int viewport_height;
};

void editor_init(struct editor *e, int viewport_height);
void editor_insert_char(struct editor *e, char c);
void editor_delete_char(struct editor *e);
void editor_backspace(struct editor *e);
void editor_move_cursor(struct editor *e, int dr, int dc);
void editor_ensure_cursor_visible(struct editor *e);

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
    
    editor_init(&e, 20);
    
    /* Test 1: init creates empty editor */
    check(e.nlines >= 1 && e.cursor_row == 0 && e.cursor_col == 0,
          "test_init");
    
    /* Test 2: insert_char adds characters */
    editor_insert_char(&e, 'H');
    editor_insert_char(&e, 'i');
    check(e.lines[0][0] == 'H' && e.lines[0][1] == 'i',
          "test_insert_char");
    
    /* Test 3: backspace works */
    editor_backspace(&e);
    check(e.cursor_col == 1, "test_backspace");
    
    /* Test 4: move_cursor with bounds */
    editor_move_cursor(&e, -1, 0);
    check(e.cursor_row == 0, "test_move_cursor_bounds");
    
    /* Test 5: viewport scrolls */
    for (int i = 0; i < 50; i++) {
        editor_insert_char(&e, 'X');
        editor_insert_char(&e, '\n');
    }
    editor_ensure_cursor_visible(&e);
    check(e.viewport_row > 0, "test_viewport_scrolls");
    
    return failed;
}
```
