---
course: build-a-database
title: Build a Database in C
language: c
description: >
  Learn how SQLite works from the ground up. Build a single-table database
  with real file persistence, crash-safe atomic saves, indexing, transactions
  with rollback, thread-safe concurrent access, and a SQL-like command parser.
  Along the way you'll learn why databases are built the way they are: why
  fsync exists, why writes are journaled, why B-Trees beat hash tables on
  disk, and why every mutation path needs a lock.
duration_hours: 20
tags: [databases, c, systems, data-structures, algorithms, concurrency]
extended_reading:
  - title: "SQLite Architecture Documentation"
    url: https://www.sqlite.org/arch.html
  - title: "Atomic Commit in SQLite"
    url: https://www.sqlite.org/atomiccommit.html
  - title: "How To Corrupt An SQLite Database File"
    url: https://www.sqlite.org/howtocorrupt.html
  - title: "Write-Ahead Logging in SQLite"
    url: https://www.sqlite.org/wal.html
  - title: "B-Tree Indexes in SQLite"
    url: https://www.sqlite.org/btreeindex.html
  - title: "ACID Transactions"
    url: https://en.wikipedia.org/wiki/ACID
  - title: "POSIX Threads Programming (LLNL guide)"
    url: https://hpc-tutorials.llnl.gov/posix/
  - title: "fsync(2) — Linux manual page"
    url: https://www.man7.org/linux/man-pages/man2/fsync.2.html
---

# Lesson: Tables and Row Storage {#tables-and-rows}

A database table is a set of rows. In memory, each row is a struct — an ordered
collection of typed values. A table is an array of rows, with a fixed schema
(column names and types).

Real databases store rows in **pages** on disk (SQLite uses 4 KB pages), but
we'll start simpler: one flat file per table, with rows serialized end-to-end.
The challenge is not the disk format — it's managing a growing array without
knowing how many rows you'll need.

## Why an Array? (And Not a Linked List)

The obvious alternative to a growing array is a linked list: every insert
mallocs one node, no reallocation ever, O(1) append. Databases almost never do
this, and the reason is the **memory hierarchy**.

Your CPU doesn't read memory one byte at a time — it reads **cache lines**
(64 bytes on x86). When you touch `rows[0]`, the CPU pulls in the surrounding
bytes for free, and its prefetcher notices the sequential pattern and starts
loading `rows[1]`, `rows[2]`, ... before you ask. Scanning a contiguous array
runs at close to memory bandwidth. A linked list defeats all of this: each
node lives at an unpredictable address, so every `node->next` is a potential
**cache miss** — a stall of ~100ns while the CPU waits for DRAM. A full-table
scan over a linked list can easily be 10–50× slower than the same scan over
an array, for identical big-O.

This exact logic scales up one level in the hierarchy: what cache lines are to
RAM, **pages** are to disk. A disk read fetches 4 KB whether you wanted 4 bytes
or all of them, so databases pack rows contiguously into pages for the same
reason we pack them contiguously into an array. Learn the pattern once here and
you'll recognize it everywhere in storage engines.

## Why Track Capacity Separately From Count?

You've seen two strategies for a growing collection:

- **Preallocate and return an error** when full (simple, wastes space, and the
  first user with row n+1 files a bug report).
- **Dynamic reallocation** (malloc a bigger array, copy old data, free old space).

Databases use the second. The key design decision is that the table tracks
**capacity** (how much space is allocated) separately from **count** (how much
is used). A table with 3 rows might have capacity for 8, so the next 5 inserts
are just a struct copy — no allocator call, no copying the whole table. The
allocator is only involved on the rare insert that crosses the capacity
boundary. This idea — *reserve more than you need so the common case is cheap* —
shows up all over systems code: file systems preallocate extents, Postgres
leaves free space in pages for updates, Go slices and C++ vectors work exactly
like this.

## Growth Strategy: Why Double?

When you run out of space, how much space do you allocate next? Allocating just
`count + 1` means every insert triggers a reallocation — and each reallocation
copies every existing row, so inserting n rows costs 1 + 2 + 3 + ... + n copies:
O(n²) total. For a million rows that's half a trillion copies. Doubling the
capacity (`new_capacity = capacity * 2`) means you reallocate only log₂(n)
times, making the total cost O(n) — linear, which is optimal. This is the
**amortized analysis** of dynamic arrays: individual inserts are occasionally
expensive, but the *average* cost per insert is constant.

The math: if you start with capacity 1 and double each time, after k reallocations
you have capacity 2^k. To store n items, you need 2^k ≥ n, so k = log₂(n). Each
reallocation i costs 2^i work (copying 2^i items). Total:

```
∑(2^i for i = 0 to log₂(n)) = 2^(log₂(n)+1) - 1 = 2n - 1 = O(n)
```

Any constant factor works (1.5×, 1.75×), but the tradeoff is real: a larger
factor means fewer reallocations but more wasted memory (2× can leave the array
half empty), a smaller factor is thriftier with memory but reallocates more
often. Some allocators prefer 1.5× because the freed old blocks can eventually
be reused for a future growth — with 2×, the sum of all previous allocations is
always slightly smaller than the next one, so old space never fits. For this
course, 2× is fine.

## Why Fixed-Size Rows (For Now)

Our schema is hardcoded and every row is exactly the same size. That's a real
simplification — text columns in real databases are variable-length — but it
buys us two properties that make everything else in this course tractable:

1. **Row i lives at a computable address**: `rows + i * sizeof(Person)`. No
   per-row bookkeeping, no offset table.
2. **Serialization is trivial**: the in-memory array *is* the disk format
   (lesson 2).

Real engines pay significant complexity for variable-length rows: SQLite
encodes each row as a varint-prefixed record and keeps a cell pointer array in
every page; Postgres has a line-pointer array plus TOAST for oversized values.
The concepts you build here — capacity management, serialization, indexing —
carry over unchanged; only the byte-level encoding gets harder.

## Schema and Row Types

For this course, we'll use a fixed schema: a row is always a struct with the
same columns. (Real databases read schema from a catalog table — in SQLite,
`sqlite_schema` is itself an ordinary table that stores the `CREATE TABLE`
statements; we hardcode ours for simplicity.) Let's define a `Person` row: an
auto-increment ID, a name, and an age.

```c
#include <stdint.h>

typedef struct {
    uint32_t id;        /* auto-increment primary key */
    char name[256];     /* NUL-terminated string */
    uint32_t age;       /* unsigned integer */
} Person;
```

Note the fixed-width types: `uint32_t`, not `int` or `long`. Databases care
about exact sizes because these bytes will eventually hit disk (lesson 2), and
`long` is 4 bytes on some platforms and 8 on others. Also note the layout:
4 + 256 + 4 = 264 bytes, and since the largest field alignment is 4, the
compiler inserts **no padding**. That's deliberate — padding bytes are
uninitialized garbage, and in lesson 2 we'll write these structs to disk
verbatim. If you ever reorder fields or add a `uint8_t`, check the layout again.

A table holds a growing array of these:

```c
typedef struct {
    Person *rows;       /* dynamically allocated array */
    uint32_t count;     /* number of rows in use */
    uint32_t capacity;  /* how many rows we have space for */
    uint32_t next_id;   /* the ID to assign to the next insert */
} Table;
```

`next_id` is a monotonic counter: it only ever goes up, even when rows are
deleted. We'll see why ID reuse is dangerous in the update/delete lesson.

When you insert a row, you:
1. Check if `count >= capacity`. If so, realloc to a bigger capacity (e.g.,
   `capacity * 2`, or 16 if capacity was 0).
2. Set `rows[count].id = next_id++` and copy in the name and age.
3. Increment `count`.

One C-specific trap in step 2: copy the name with a bounded copy
(`strncpy` up to 255 bytes) and explicitly NUL-terminate. `strncpy` does *not*
write a terminator when the source fills the buffer — forgetting this is a
classic buffer over-read waiting to happen, and databases are exactly the kind
of long-lived process where it eventually does.

## Challenge: Create and Insert {#insert-rows points=15}

Implement a table with an insert operation. Start with an empty table, insert
multiple rows, and verify they are stored correctly with proper ID assignment
and automatic capacity growth.

### Starter

```c
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

/* Create an empty table. Caller owns the returned pointer. */
Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    return t;
}

/* Insert a row. Returns the row's ID, or 0 on error. */
uint32_t table_insert(Table *t, const char *name, uint32_t age) {
    if (!t) return 0;
    /* TODO: if count == capacity, realloc to double the capacity (or 16 if 0).
       Copy name (up to 255 bytes, NUL-terminate). Assign an ID from next_id.
       Increment next_id and count. Return the assigned ID. */
    (void)name; (void)age;
    return 0;
}

/* Free all resources. */
void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t);
    }
}

/* Return the row with the given ID, or NULL if not found. */
Person *table_find_by_id(Table *t, uint32_t id) {
    if (!t) return NULL;
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) return &t->rows[i];
    }
    return NULL;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

Table *table_new(void);
uint32_t table_insert(Table *t, const char *name, uint32_t age);
void table_free(Table *t);
Person *table_find_by_id(Table *t, uint32_t id);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void assert_str_eq(const char *test, const char *got, const char *want) {
    if (strcmp(got, want) == 0) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got '%s', want '%s')\n", test, got, want);
        failed++;
    }
}

int main(void) {
    Table *t = table_new();

    /* Insert three rows and verify IDs are assigned sequentially. */
    uint32_t id1 = table_insert(t, "Alice", 30);
    uint32_t id2 = table_insert(t, "Bob", 25);
    uint32_t id3 = table_insert(t, "Charlie", 35);

    assert_eq("first_id", id1, 1);
    assert_eq("second_id", id2, 2);
    assert_eq("third_id", id3, 3);
    assert_eq("count", t->count, 3);

    /* Verify data in inserted rows */
    if (t->count >= 2) {
        assert_str_eq("first_name", t->rows[0].name, "Alice");
        assert_eq("first_age", t->rows[0].age, 30);
        assert_str_eq("second_name", t->rows[1].name, "Bob");
        assert_eq("second_age", t->rows[1].age, 25);
    }

    /* Test capacity growth: insert many rows to trigger reallocation */
    for (int i = 0; i < 20; i++) {
        char name[32];
        snprintf(name, sizeof(name), "User%d", i);
        table_insert(t, name, 20 + i);
    }

    assert_eq("count_after_many", t->count, 23);
    assert_eq("capacity_grew", t->capacity > 0, 1);

    /* Test find_by_id */
    Person *p = table_find_by_id(t, 1);
    assert_eq("find_id_1_found", p != NULL, 1);
    if (p) assert_str_eq("find_id_1_name", p->name, "Alice");

    /* ID should not wrap around */
    assert_eq("next_id_advanced", t->next_id, 24);

    table_free(t);

    return failed > 0 ? 1 : 0;
}
```

# Lesson: File Persistence and Serialization {#persistence}

Storing a table in memory is convenient during a session, but you lose
everything when the program exits. Databases persist data to disk. The simplest
format is **direct serialization**: write the row array to a file as raw bytes.

SQLite uses a B-Tree structure with variable-sized pages; we'll use a simpler
approach: a header with metadata, followed by all rows packed end-to-end.

```
[Header: magic (4 bytes) | version (1 byte) | count (uint32) | next_id (uint32)]
[Row 1: id | name (256 bytes) | age]
[Row 2: id | name (256 bytes) | age]
...
```

## Why a Header at All?

A file on disk is just bytes — it carries no type information. Six months from
now, something *will* hand your load function the wrong file: a truncated copy,
a file from an older build, a JPEG some script renamed to `.db`. The header is
how your code notices before it does damage.

- The **magic number** (e.g., 0xCAFEBABE) is a cheap sanity check: if the first
  four bytes don't match, this is not our file, stop immediately. Nearly every
  binary format starts this way — `%PDF`, `\x7fELF`, PNG's 8-byte signature.
  SQLite files begin with the 16-byte string `"SQLite format 3\0"`.
- The **version byte** buys you the right to change the format later. If you
  add a column in version 2, version-1 code sees `version == 2` and bails out
  gracefully instead of interpreting new-format bytes with old-format offsets —
  which wouldn't error, it would silently load garbage. Rejecting loudly beats
  corrupting quietly, every time.
- **count** tells the loader how much to read, and doubles as a corruption
  check: a header claiming 1000 rows in a file with space for 3 means the file
  was truncated.

The general principle: **make the file self-describing**. Your in-memory struct
can change every compile; the bytes you've written to users' disks are forever.

## Struct Padding: The Silent Format-Breaker

We're going to serialize with `fwrite(t->rows, sizeof(Person), count, f)` —
writing the structs exactly as they sit in memory. That only works because
`Person` has no padding: 4 + 256 + 4 bytes, all 4-byte-aligned. The compiler is
free to insert invisible padding bytes between fields (and after the last one)
to satisfy alignment, and those bytes are **uninitialized garbage** that would
go straight into your file. Worse, padding differs between compilers and
architectures, so a file written on one machine might not parse on another.

This is why serious formats don't dump structs: they serialize **field by
field** into an explicitly laid-out buffer (SQLite's record format, Protocol
Buffers, etc.). We accept the struct-dump shortcut because our layout is
padding-free and we control both writer and reader — but if you ever add a
`uint8_t active` field to `Person`, three padding bytes appear and your file
format has silently changed. When in doubt: `_Static_assert(sizeof(Person) ==
264, "Person layout changed — bump DB_VERSION");`

## Endianness Gotcha

When you write `uint32_t count = 42` with `fwrite`, the bytes on disk depend on
your CPU's **endianness**: little-endian (x86, ARM) writes `2A 00 00 00`, while
big-endian (PowerPC, network byte order) writes `00 00 00 2A`. If you write on
little-endian and read on big-endian, the value is backwards — 42 becomes
704,643,072.

Real databases pick one order and stick to it: SQLite stores all multi-byte
integers big-endian regardless of the host CPU, converting on every read and
write, which is why an SQLite file copied between any two machines just works.
For this course, we'll assume all reads and writes happen on the same machine,
so you can use native byte order and `fwrite` directly. The magic number gives
you partial protection anyway: read on a wrong-endian machine, 0xCAFEBABE comes
back as 0xBEBAFECA and the load is rejected.

## Serialization Strategy

To load: read the header to learn how many rows exist, allocate space for them,
then read that many rows into the array. To save: write the header, then all
rows. This is O(n) in table size for each operation — every save rewrites the
entire file even if one row changed.

Why is that a problem at scale? A 10 GB table where you update one 264-byte row
would rewrite 10 GB. Real databases fix this with **page-oriented storage**:
the file is an array of fixed-size pages, each page holds some rows, and a
write only touches the pages containing changed rows. That's also *why* B-Trees
(later lesson) are the universal database structure — they're designed so every
operation touches O(log n) pages. Combined with **write-ahead logging** (append
changes to a log, apply them to pages lazily), a one-row update costs a few KB
of I/O regardless of table size. We'll keep whole-file rewrites — correct, just
not incremental — and make them *crash-safe* in the next lesson.

One more error-handling discipline this challenge forces on you: **every
`fread`/`fwrite` return value must be checked**. Disks fill up, files get
truncated. A load that ignores a short read returns a table full of
uninitialized memory — the worst kind of bug, because it *usually* works.

## Challenge: Load and Save {#save-load points=20}

Implement `table_save` to write a table to disk with a header, and `table_load`
to read it back. Add validation: reject files with the wrong magic number or
version, and fail cleanly (no leaks, no half-built tables) on short reads.

### Starter

```c
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

#define DB_MAGIC 0xCAFEBABE
#define DB_VERSION 1

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    return t;
}

uint32_t table_insert(Table *t, const char *name, uint32_t age) {
    if (!t) return 0;
    if (t->count >= t->capacity) {
        uint32_t new_cap = t->capacity == 0 ? 16 : t->capacity * 2;
        Person *new_rows = realloc(t->rows, new_cap * sizeof(Person));
        if (!new_rows) return 0;
        t->rows = new_rows;
        t->capacity = new_cap;
    }
    strncpy(t->rows[t->count].name, name, sizeof(t->rows[t->count].name) - 1);
    t->rows[t->count].name[sizeof(t->rows[t->count].name) - 1] = '\0';
    t->rows[t->count].age = age;
    t->rows[t->count].id = t->next_id++;
    return t->rows[t->count++].id;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t);
    }
}

Person *table_find_by_id(Table *t, uint32_t id) {
    if (!t) return NULL;
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) return &t->rows[i];
    }
    return NULL;
}

/* Write a table to disk with header. Return 0 on success, -1 on error. */
int table_save(Table *t, const char *path) {
    if (!t || !path) return -1;

    FILE *f = fopen(path, "wb");
    if (!f) return -1;

    /* TODO: write magic (uint32), version (uint8), count (uint32), next_id (uint32).
       Then write all rows. Check every fwrite's return value; on failure,
       close the file and return -1. Close file. */
    (void)f;
    return -1;
}

/* Read a table from disk. Return a new table, or NULL on error. */
Table *table_load(const char *path) {
    if (!path) return NULL;

    FILE *f = fopen(path, "rb");
    if (!f) return NULL;

    /* TODO: read magic, version, count, next_id. Validate magic and version.
       Allocate space for count rows. Read them. Return the table.
       On any error, close file, free partial table, return NULL. */
    (void)f;
    return NULL;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

#define DB_MAGIC 0xCAFEBABE
#define DB_VERSION 1

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

Table *table_new(void);
uint32_t table_insert(Table *t, const char *name, uint32_t age);
void table_free(Table *t);
Person *table_find_by_id(Table *t, uint32_t id);
int table_save(Table *t, const char *path);
Table *table_load(const char *path);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void assert_str_eq(const char *test, const char *got, const char *want) {
    if (strcmp(got, want) == 0) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got '%s', want '%s')\n", test, got, want);
        failed++;
    }
}

int main(void) {
    Table *t = table_new();

    table_insert(t, "Alice", 30);
    table_insert(t, "Bob", 25);
    table_insert(t, "Charlie", 35);

    /* Save and verify return value. */
    int save_result = table_save(t, "/tmp/test.db");
    assert_eq("save_returns_0", save_result, 0);

    /* Load and verify structure. */
    Table *loaded = table_load("/tmp/test.db");
    assert_eq("loaded_not_null", loaded != NULL, 1);

    if (loaded) {
        assert_eq("loaded_count", loaded->count, 3);
        assert_eq("loaded_next_id", loaded->next_id, 4);

        /* Verify row data integrity. */
        assert_eq("loaded_row0_id", loaded->rows[0].id, 1);
        assert_str_eq("loaded_row0_name", loaded->rows[0].name, "Alice");
        assert_eq("loaded_row0_age", loaded->rows[0].age, 30);

        assert_eq("loaded_row1_id", loaded->rows[1].id, 2);
        assert_str_eq("loaded_row1_name", loaded->rows[1].name, "Bob");
        assert_eq("loaded_row1_age", loaded->rows[1].age, 25);

        assert_eq("loaded_row2_id", loaded->rows[2].id, 3);
        assert_str_eq("loaded_row2_name", loaded->rows[2].name, "Charlie");
        assert_eq("loaded_row2_age", loaded->rows[2].age, 35);

        table_free(loaded);
    }

    /* Test round-trip with more data. */
    Table *t2 = table_new();
    for (int i = 0; i < 10; i++) {
        char name[32];
        snprintf(name, sizeof(name), "User%d", i);
        table_insert(t2, name, 20 + i);
    }

    assert_eq("save_large", table_save(t2, "/tmp/test2.db"), 0);
    Table *loaded2 = table_load("/tmp/test2.db");
    assert_eq("loaded2_not_null", loaded2 != NULL, 1);
    if (loaded2) {
        assert_eq("loaded_large_count", loaded2->count, 10);
        assert_eq("loaded_large_next_id", loaded2->next_id, 11);

        Person *p = table_find_by_id(loaded2, 5);
        assert_eq("loaded_large_find", p != NULL, 1);
        if (p) assert_str_eq("loaded_large_name", p->name, "User4");

        table_free(loaded2);
    }

    /* A file with the wrong magic number must be rejected. */
    FILE *bad = fopen("/tmp/bad.db", "wb");
    if (bad) {
        uint32_t wrong = 0xDEADBEEF;
        fwrite(&wrong, sizeof(wrong), 1, bad);
        fclose(bad);
    }
    assert_eq("wrong_magic_rejected", table_load("/tmp/bad.db") == NULL, 1);

    /* A truncated file (valid header, missing rows) must be rejected. */
    if (table_save(t2, "/tmp/trunc.db") == 0) {
        FILE *f = fopen("/tmp/trunc.db", "rb");
        FILE *g = fopen("/tmp/trunc2.db", "wb");
        if (f && g) {
            char buf[64];
            size_t n = fread(buf, 1, sizeof(buf), f); /* header + partial row */
            fwrite(buf, 1, n, g);
        }
        if (f) fclose(f);
        if (g) fclose(g);
        assert_eq("truncated_rejected", table_load("/tmp/trunc2.db") == NULL, 1);
    }

    table_free(t2);
    table_free(t);

    return failed > 0 ? 1 : 0;
}
```

# Lesson: Durability — What "Saved" Actually Means {#durability}

Your `table_save` returns 0 and you trust the data is on disk. Here's the
uncomfortable truth: **it probably isn't yet.** If the machine loses power one
second after `table_save` returns, there's a good chance your file is gone,
empty, or half-written. This lesson is about the D in ACID — **durability** —
and it's where a toy database starts becoming a real one.

## The Write Path: Four Layers of Lies

When you call `fwrite`, the bytes travel through a stack of buffers, and each
layer reports success as soon as it has handed off to the next:

```
your buffer
   → stdio buffer        (in your process; flushed by fflush/fclose)
   → kernel page cache   (in RAM; flushed by the OS "eventually", or by fsync)
   → drive write cache   (RAM on the disk itself; flushed by a cache-flush command)
   → the actual medium   (platter / NAND — the only layer that survives power loss)
```

- `fwrite` usually copies into the **stdio buffer** and returns. The kernel
  hasn't even seen the data.
- `fflush` (or `fclose`) pushes it to the **kernel page cache**. Now it
  survives your process crashing — but the page cache is RAM. The kernel
  writes dirty pages back on its own schedule, typically within ~30 seconds.
  Power loss in that window: data gone, `fwrite` and `fclose` both reported
  success.
- `fsync(fd)` tells the kernel: *block me until this file's data is on stable
  storage*, including telling the drive to flush its own cache. This is the
  only call in the stack that means "durable".

Why the layering? **Performance.** RAM is ~1000× faster than storage, and
batching many small writes into few large ones is the single biggest I/O
optimization there is. The page cache makes 99% of programs faster and is the
right default. Databases are the 1%: when a database says "committed", that's
a promise about power loss, so databases call `fsync` at every commit point —
and pay for it. An fsync costs ~milliseconds on an SSD (~tens on spinning
rust), which is why real databases *group commit*: batch several transactions'
writes, fsync once, then report them all committed.

## Crash Windows: Why Overwrite-In-Place Corrupts

Durability isn't just "did the bytes arrive" — it's **what state is the file in
if we crash halfway**. Look at our current save:

```c
FILE *f = fopen(path, "wb");   /* truncates the file to 0 bytes! */
/* ... write header, write rows ... */
```

The instant `fopen(path, "wb")` returns, the *old* data is already destroyed.
For the entire duration of the write, the file on disk is some prefix of the
new data: empty, header-only, or half the rows. Crash anywhere in that window
and you've lost both the old version *and* the new one. Note this failure needs
no power loss — the process being killed (OOM killer, Ctrl-C, a bug) is enough.

It gets subtler: the kernel and the drive may write your data back in **any
order**. Writing bytes 0–4095 then 4096–8191 does not mean they reach the
platter in that order. A crash can leave the *second* page written and the
first one old — a file that's interleaved old/new garbage. This is why "I'll just be careful about write
order" doesn't work without explicit barriers (fsync is also an ordering
barrier). (The single-page version of this hazard is the classic **torn
write**, or **torn page**: one page write fractured into part-old, part-new
because the drive's atomic unit is only a sector — the reason Postgres logs
whole page images after each checkpoint.)

## The Fix: Write-New-Then-Rename

POSIX gives us one operation with exactly the right property:
**`rename(old, new)` is atomic**. If `new` already exists, it is replaced, and
any observer — including one that crashes and reboots — sees either the old
file or the new file, never a mixture, never a missing file. Filesystems
implement this as a single metadata update in their journal.

That gives the classic durable-save recipe, used by everything from text
editors to package managers:

1. Write the complete new contents to a **temporary file** in the same
   directory (`data.db.tmp`). Crash here? The real file is untouched; the
   leftover `.tmp` is litter, not damage.
2. `fflush` the stdio buffer, then **`fsync(fileno(f))`** — the new bytes are
   now durable, but under the temp name. (Order matters: fsync before rename,
   or you can crash into a durable *rename* of a *non-durable* file — an empty
   `data.db`.)
3. `fclose`, then **`rename("data.db.tmp", "data.db")`** — the atomic switch.
4. (Full rigor: `fsync` the *directory* too, so the rename itself — a
   directory entry update — is durable. We'll mention it, not require it.)

Failure handling falls out naturally: any error before the rename → delete the
temp file and report failure; the previous database is still intact. This
either/or property is **atomicity meeting durability**, and it's precisely
what SQLite's rollback journal and WAL achieve at page granularity: never put
the *only* copy of your data into a state you couldn't recover from. Our
whole-file version costs O(n) per save; SQLite's journaled version costs
O(changed pages) — same guarantee, better price.

## A C Portability Note: Feature-Test Macros

`fileno` is a **POSIX** function, not ISO C (`fsync` itself has needed no such
guard on glibc since 2.16, but `fileno` still does). We compile with
`-std=c17`, which asks libc for *strict* standard C — and glibc responds by
hiding POSIX declarations it doesn't have to expose. The fix is to declare
which API level you want, before any `#include`:

```c
#define _POSIX_C_SOURCE 200809L   /* "give me POSIX.1-2008" */
#include <stdio.h>
#include <unistd.h>               /* fsync */
```

This is worth knowing beyond this course: it's why a program can compile fine
with `-std=gnu17` and break with `-std=c17`. The starter code has the define
in place.

## Challenge: Atomic, Durable Save {#atomic-save points=25}

Implement `table_save_atomic(t, path)`: write the table to `<path>.tmp`, flush
and fsync it, then rename it over `path`. On *any* failure, remove the temp
file, leave whatever was previously at `path` untouched, and return -1. The
serialization helper from the previous lesson is provided.

### Starter

```c
#define _POSIX_C_SOURCE 200809L

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>   /* fsync */

#define DB_MAGIC 0xCAFEBABE
#define DB_VERSION 1

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    return t;
}

uint32_t table_insert(Table *t, const char *name, uint32_t age) {
    if (!t) return 0;
    if (t->count >= t->capacity) {
        uint32_t new_cap = t->capacity == 0 ? 16 : t->capacity * 2;
        Person *new_rows = realloc(t->rows, new_cap * sizeof(Person));
        if (!new_rows) return 0;
        t->rows = new_rows;
        t->capacity = new_cap;
    }
    strncpy(t->rows[t->count].name, name, sizeof(t->rows[t->count].name) - 1);
    t->rows[t->count].name[sizeof(t->rows[t->count].name) - 1] = '\0';
    t->rows[t->count].age = age;
    t->rows[t->count].id = t->next_id++;
    return t->rows[t->count++].id;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t);
    }
}

/* Serialize the table to an already-open stream. 0 on success, -1 on error.
   (This is your table_save body from the previous challenge, factored out.) */
int table_write_stream(Table *t, FILE *f) {
    uint32_t magic = DB_MAGIC;
    uint8_t version = DB_VERSION;
    if (fwrite(&magic, sizeof(magic), 1, f) != 1) return -1;
    if (fwrite(&version, sizeof(version), 1, f) != 1) return -1;
    if (fwrite(&t->count, sizeof(t->count), 1, f) != 1) return -1;
    if (fwrite(&t->next_id, sizeof(t->next_id), 1, f) != 1) return -1;
    if (t->count > 0 && fwrite(t->rows, sizeof(Person), t->count, f) != t->count)
        return -1;
    return 0;
}

Table *table_load(const char *path) {
    if (!path) return NULL;
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;

    Table *t = table_new();
    if (!t) { fclose(f); return NULL; }

    uint32_t magic;
    uint8_t version;
    if (fread(&magic, sizeof(magic), 1, f) != 1 || magic != DB_MAGIC) goto err;
    if (fread(&version, sizeof(version), 1, f) != 1 || version != DB_VERSION) goto err;
    if (fread(&t->count, sizeof(t->count), 1, f) != 1) goto err;
    if (fread(&t->next_id, sizeof(t->next_id), 1, f) != 1) goto err;

    if (t->count > 0) {
        t->rows = malloc(t->count * sizeof(Person));
        if (!t->rows) goto err;
        if (fread(t->rows, sizeof(Person), t->count, f) != t->count) goto err;
    }
    t->capacity = t->count;

    fclose(f);
    return t;
err:
    fclose(f);
    table_free(t);
    return NULL;
}

/* Durable, atomic save:
     1. Build the temp path "<path>.tmp".
     2. Open it "wb" and write via table_write_stream.
     3. fflush, then fsync(fileno(f)) — durability before visibility.
     4. fclose, then rename temp over path — the atomic switch.
   On ANY failure: close the stream if open, remove() the temp file,
   return -1. The file previously at `path` must be left untouched.
   Return 0 on success. */
int table_save_atomic(Table *t, const char *path) {
    if (!t || !path) return -1;

    char tmp[4096];
    if (snprintf(tmp, sizeof(tmp), "%s.tmp", path) >= (int)sizeof(tmp))
        return -1;

    /* TODO: implement steps 2-4 with full error handling. */
    return -1;
}
```

### Tests

```c
#define _POSIX_C_SOURCE 200809L

#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>
#include <sys/stat.h>   /* mkdir */

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

Table *table_new(void);
uint32_t table_insert(Table *t, const char *name, uint32_t age);
void table_free(Table *t);
Table *table_load(const char *path);
int table_save_atomic(Table *t, const char *path);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void assert_str_eq(const char *test, const char *got, const char *want) {
    if (strcmp(got, want) == 0) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got '%s', want '%s')\n", test, got, want);
        failed++;
    }
}

static int file_exists(const char *path) {
    FILE *f = fopen(path, "rb");
    if (f) { fclose(f); return 1; }
    return 0;
}

int main(void) {
    remove("/tmp/atomic.db");
    remove("/tmp/atomic.db.tmp");

    Table *t = table_new();
    table_insert(t, "Alice", 30);
    table_insert(t, "Bob", 25);

    /* Fresh save: succeeds, file loads, temp file is gone. */
    assert_eq("save_fresh", table_save_atomic(t, "/tmp/atomic.db"), 0);
    assert_eq("no_tmp_leftover", file_exists("/tmp/atomic.db.tmp"), 0);

    Table *loaded = table_load("/tmp/atomic.db");
    assert_eq("fresh_loads", loaded != NULL, 1);
    if (loaded) {
        assert_eq("fresh_count", loaded->count, 2);
        assert_str_eq("fresh_name", loaded->rows[0].name, "Alice");
        table_free(loaded);
    }

    /* Overwrite: the file is atomically replaced with the new contents. */
    table_insert(t, "Carol", 41);
    assert_eq("save_overwrite", table_save_atomic(t, "/tmp/atomic.db"), 0);
    assert_eq("no_tmp_after_overwrite", file_exists("/tmp/atomic.db.tmp"), 0);

    loaded = table_load("/tmp/atomic.db");
    assert_eq("overwrite_loads", loaded != NULL, 1);
    if (loaded) {
        assert_eq("overwrite_count", loaded->count, 3);
        assert_str_eq("overwrite_name", loaded->rows[2].name, "Carol");
        assert_eq("overwrite_next_id", loaded->next_id, 4);
        table_free(loaded);
    }

    /* Failure before the temp file: target directory doesn't exist. */
    assert_eq("bad_dir_fails",
              table_save_atomic(t, "/no/such/dir/atomic.db"), -1);

    /* Failure at rename time: the destination is a directory, so rename()
       fails after the temp file was fully written. The temp file must be
       cleaned up and -1 returned. */
    remove("/tmp/atomic_dir.tmp");
    mkdir("/tmp/atomic_dir", 0700); /* may already exist; that's fine */
    assert_eq("rename_onto_dir_fails",
              table_save_atomic(t, "/tmp/atomic_dir"), -1);
    assert_eq("tmp_cleaned_after_rename_fail",
              file_exists("/tmp/atomic_dir.tmp"), 0);

    /* The earlier good file is still intact after the failed saves. */
    loaded = table_load("/tmp/atomic.db");
    assert_eq("survivor_intact", loaded != NULL, 1);
    if (loaded) {
        assert_eq("survivor_count", loaded->count, 3);
        table_free(loaded);
    }

    /* Empty table round-trips too. */
    Table *empty = table_new();
    assert_eq("save_empty", table_save_atomic(empty, "/tmp/atomic_empty.db"), 0);
    Table *eloaded = table_load("/tmp/atomic_empty.db");
    assert_eq("empty_loads", eloaded != NULL, 1);
    if (eloaded) {
        assert_eq("empty_count", eloaded->count, 0);
        table_free(eloaded);
    }
    table_free(empty);

    table_free(t);

    return failed > 0 ? 1 : 0;
}
```

# Lesson: Basic Querying and Predicates {#querying}

Now your database can store data durably and load it back. The next piece is
**querying**: given a condition (a **predicate**), return only the rows that
match it.

A predicate is a function that tests a row: does `age > 25`? Is `name ==
"Alice"`? In C, you could pass a function pointer, but we'll use a
**predicate struct** that *describes* one condition as data:

```c
typedef struct {
    enum { PRED_EQ, PRED_LT, PRED_GT, PRED_LE, PRED_GE } op;
    char column[32];             /* "age" or "name" */
    uint32_t int_value;          /* for numeric comparisons */
    char str_value[256];         /* for string comparisons */
} Predicate;
```

## Why a Struct and Not a Function Pointer?

A function pointer would work for filtering, but it's a black box: the engine
can only call it, row by row. A predicate-as-data can be **inspected**. The
engine can look at it and think: "this is an equality test on `id`, and `id`
has an index — skip the scan entirely and do one lookup." That inspection step
is the seed of a **query optimizer**, and it's only possible because the
condition is data, not code.

This is the deep idea behind SQL itself. SQL is **declarative**: you say *what*
you want, never *how* to get it. The database parses your `WHERE age > 25`
into exactly this kind of structure (a predicate tree), then *chooses* an
execution strategy: which index to use, which table to scan first in a join,
whether to sort or hash. Two systems can run the same query a million times
apart in cost depending on that choice. Your `Predicate` struct is a one-node
predicate tree; real ones combine nodes with AND/OR/NOT.

## Why Push Filtering Into the Engine?

In the earliest data systems, applications read every record and filtered in
application code. Moving the filter into the database — "predicate pushdown" —
was a breakthrough for three reasons:

- **Indexes.** If `id == 5` and `id` is indexed, the engine reads one row
  instead of n. The application can't make that decision; it doesn't know the
  indexes exist.
- **Data movement.** The filter runs where the data lives. Filtering 10 million
  rows down to 50 inside the engine means 50 rows cross the boundary to the
  app — not 10 million. In a client/server database, that boundary is the
  network; the difference is measured in seconds.
- **Freedom to optimize.** Once the engine owns evaluation, it can compile
  predicates, evaluate them over compressed data, parallelize across cores, or
  push them all the way into remote storage nodes — all without the
  application changing a line.

The predicate is the **contract** between the application and the engine: the
app says what it wants; the engine decides how to get it efficiently.

## Scan Cost and Selectivity

Our query is a **full table scan**: test every row, O(n) per query. Whether
that's terrible depends on **selectivity** — the fraction of rows that match.
For `age > 0` (matches everything), a scan is optimal: you must touch every
row anyway, and thanks to lesson 1, scanning our contiguous array is as fast
as memory allows. For `id == 5` (matches one row), a scan is absurd — that's
what indexes fix in a later lesson. Real query planners keep *statistics*
(histograms of column values) to estimate selectivity and choose between
scan and index per query.

Note one design decision in the code below: `table_query` returns a **new
table** containing copies of the matching rows, rather than pointers into the
original. Copying costs memory, but pointers into `t->rows` would be
invalidated by the next insert that triggers a realloc — a use-after-free
handed to the caller. This tension (return copies = safe but slow; return
references = fast but lifetime-fraught) is fundamental, and it returns with
force in the concurrency lesson.

## Challenge: Query with Predicates {#query-predicates points=20}

Implement a predicate struct and query functions that filter rows by age or
name. Support all comparison operators: ==, <, >, <=, >=.

### Starter

```c
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

typedef struct {
    enum { PRED_EQ, PRED_LT, PRED_GT, PRED_LE, PRED_GE } op;
    char column[32];
    uint32_t int_value;
    char str_value[256];
} Predicate;

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    return t;
}

uint32_t table_insert(Table *t, const char *name, uint32_t age) {
    if (!t) return 0;
    if (t->count >= t->capacity) {
        uint32_t new_cap = t->capacity == 0 ? 16 : t->capacity * 2;
        Person *new_rows = realloc(t->rows, new_cap * sizeof(Person));
        if (!new_rows) return 0;
        t->rows = new_rows;
        t->capacity = new_cap;
    }
    strncpy(t->rows[t->count].name, name, sizeof(t->rows[t->count].name) - 1);
    t->rows[t->count].name[sizeof(t->rows[t->count].name) - 1] = '\0';
    t->rows[t->count].age = age;
    t->rows[t->count].id = t->next_id++;
    return t->rows[t->count++].id;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t);
    }
}

/* Test if a row matches a predicate. */
int predicate_matches(Person *p, Predicate pred) {
    /* TODO: if the column is "age", compare p->age with pred.int_value
       using the operation. If "name", compare p->name with pred.str_value
       (strcmp gives you <0 / 0 / >0 — map that onto the five operators).
       Return 1 if matches, 0 otherwise. Unknown column: return 0. */
    (void)p; (void)pred;
    return 0;
}

/* Query the table, returning a new table with matching rows. */
Table *table_query(Table *t, Predicate pred) {
    if (!t) return NULL;
    Table *result = table_new();
    if (!result) return NULL;

    for (uint32_t i = 0; i < t->count; i++) {
        if (predicate_matches(&t->rows[i], pred)) {
            if (!table_insert(result, t->rows[i].name, t->rows[i].age)) {
                table_free(result);
                return NULL;
            }
        }
    }
    return result;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

typedef struct {
    enum { PRED_EQ, PRED_LT, PRED_GT, PRED_LE, PRED_GE } op;
    char column[32];
    uint32_t int_value;
    char str_value[256];
} Predicate;

Table *table_new(void);
uint32_t table_insert(Table *t, const char *name, uint32_t age);
void table_free(Table *t);
int predicate_matches(Person *p, Predicate pred);
Table *table_query(Table *t, Predicate pred);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

int main(void) {
    Table *t = table_new();

    table_insert(t, "Alice", 30);
    table_insert(t, "Bob", 25);
    table_insert(t, "Charlie", 35);
    table_insert(t, "Diana", 22);
    table_insert(t, "Eve", 28);

    /* Query: age > 25 */
    Predicate older_than_25 = {.op = PRED_GT, .column = "age", .int_value = 25};
    Table *result1 = table_query(t, older_than_25);
    assert_eq("query_age_gt_25_count", result1->count, 3); /* Alice, Charlie, Eve */
    table_free(result1);

    /* Query: age <= 25 */
    Predicate at_most_25 = {.op = PRED_LE, .column = "age", .int_value = 25};
    Table *result2 = table_query(t, at_most_25);
    assert_eq("query_age_le_25_count", result2->count, 2); /* Bob, Diana */
    table_free(result2);

    /* Query: age == 35 */
    Predicate exact_35 = {.op = PRED_EQ, .column = "age", .int_value = 35};
    Table *result3 = table_query(t, exact_35);
    assert_eq("query_age_eq_35_count", result3->count, 1); /* Charlie */
    table_free(result3);

    /* Query: age >= 28 */
    Predicate at_least_28 = {.op = PRED_GE, .column = "age", .int_value = 28};
    Table *result7 = table_query(t, at_least_28);
    assert_eq("query_age_ge_28_count", result7->count, 3); /* Alice, Charlie, Eve */
    table_free(result7);

    /* Query: name == "Bob" */
    Predicate name_bob = {.op = PRED_EQ, .column = "name"};
    strcpy(name_bob.str_value, "Bob");
    Table *result4 = table_query(t, name_bob);
    assert_eq("query_name_eq_bob_count", result4->count, 1);
    if (result4->count > 0) assert_eq("query_name_eq_bob_age", result4->rows[0].age, 25);
    table_free(result4);

    /* Query: name > "Charlie" (lexicographic) */
    Predicate name_after_charlie = {.op = PRED_GT, .column = "name"};
    strcpy(name_after_charlie.str_value, "Charlie");
    Table *result5 = table_query(t, name_after_charlie);
    assert_eq("query_name_gt_charlie_count", result5->count, 2); /* Diana, Eve */
    table_free(result5);

    /* Query: age < 30 */
    Predicate younger_than_30 = {.op = PRED_LT, .column = "age", .int_value = 30};
    Table *result6 = table_query(t, younger_than_30);
    assert_eq("query_age_lt_30_count", result6->count, 3); /* Bob, Diana, Eve */
    table_free(result6);

    /* Unknown column matches nothing. */
    Predicate bogus = {.op = PRED_EQ, .column = "height", .int_value = 10};
    Table *result8 = table_query(t, bogus);
    assert_eq("query_unknown_column_count", result8->count, 0);
    table_free(result8);

    table_free(t);

    return failed > 0 ? 1 : 0;
}
```

# Lesson: Update and Delete Operations {#update-delete}

Databases are not just for reading. You need to **update** existing rows and
**delete** rows you no longer want. Both look trivial next to what you've
already built — a find plus a memcpy — but the *strategies* behind them are
where real engines differ most, so this lesson is as much about why as how.

## Update Strategy

Updating a row in place is simple **because our rows are fixed-size**: the new
data always fits exactly where the old data was. Notice what this bought us —
in a real database with variable-length rows, updating `"Bob"` to
`"Bartholomew"` may not fit in the row's slot, forcing the engine to relocate
the row within its page, or move it to another page and leave a forwarding
pointer behind. Postgres sidesteps in-place updates entirely: an UPDATE writes
a whole new row version and marks the old one dead (this is MVCC — multi-version
concurrency control — which also lets readers keep seeing the old version
mid-transaction). Our version:

```c
int table_update_by_id(Table *t, uint32_t id, const char *name, uint32_t age) {
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) {
            strncpy(t->rows[i].name, name, sizeof(t->rows[i].name) - 1);
            t->rows[i].name[sizeof(t->rows[i].name) - 1] = '\0';
            t->rows[i].age = age;
            return 0;
        }
    }
    return -1; /* not found */
}
```

One deliberate choice: the **ID is not updatable**. The ID is the row's
*identity* — it's what indexes point at, what other tables would reference,
what the application holds onto between requests. Letting identity change
turns every stored reference into a potential dangler.

## Delete Strategy

Deleting a row is trickier than it looks: it leaves a hole. Two ways to deal
with the hole:

1. **Remove and shift**: copy every row after the hole one position down,
   decrement count. The table stays perfectly compact — no wasted memory, scans
   stay fast — but a delete costs O(n), and *every row after the deleted one
   changes position* (remember that consequence; it comes back to bite us in
   the indexing lesson).

2. **Tombstone (soft delete)**: overwrite the row with a "deleted" marker and
   leave it there. O(1) delete, positions never change — but the table
   accumulates ghost rows, every scan pays to skip them, and the space is only
   reclaimed by a periodic **compaction/vacuum** pass that rewrites the table
   without tombstones.

Real engines overwhelmingly choose tombstones, and it's worth understanding
why: on disk, "shift everything down" means rewriting the entire file tail on
every delete — ruinous. Marking one page's row dead is a single page write.
The cost is deferred maintenance: SQLite files grow a free-page list and need
`VACUUM` to shrink; Postgres runs autovacuum continuously; log-structured
stores (LevelDB, RocksDB, Cassandra) write tombstone records and reclaim space
during compaction. The pattern — *make deletion cheap now, clean up in bulk
later* — is one of the most reused tricks in storage systems.

We'll use remove-and-shift, because our table is in memory (where a memmove is
cheap) and compactness keeps everything else simple:

```c
int table_delete_by_id(Table *t, uint32_t id) {
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) {
            /* Shift rows after i one position down */
            for (uint32_t j = i; j < t->count - 1; j++) {
                t->rows[j] = t->rows[j + 1];
            }
            t->count--;
            return 0;
        }
    }
    return -1; /* not found */
}
```

## Why IDs Are Never Reused

Delete row 2 and insert a new row: the new row gets ID 4 (or wherever
`next_id` is), never the vacated 2. This is deliberate. Somewhere out there —
in an application variable, a URL, another table, a log line — the old ID may
still exist. If ID 2 suddenly names a *different* row, every one of those
stale references silently points at the wrong data. That's not a crash; it's
worse — it's wrong answers that look right. Monotonic IDs turn stale
references into a detectable "not found" instead. (This is also why
`next_id` is saved in the file header: reload the table and the counter must
resume, not restart.)

## Challenge: Update and Delete {#crud-ops points=20}

Implement `table_update_by_id` and `table_delete_by_id`. Test that updates
modify in-place and deletes compact the table correctly.

### Starter

```c
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    return t;
}

uint32_t table_insert(Table *t, const char *name, uint32_t age) {
    if (!t) return 0;
    if (t->count >= t->capacity) {
        uint32_t new_cap = t->capacity == 0 ? 16 : t->capacity * 2;
        Person *new_rows = realloc(t->rows, new_cap * sizeof(Person));
        if (!new_rows) return 0;
        t->rows = new_rows;
        t->capacity = new_cap;
    }
    strncpy(t->rows[t->count].name, name, sizeof(t->rows[t->count].name) - 1);
    t->rows[t->count].name[sizeof(t->rows[t->count].name) - 1] = '\0';
    t->rows[t->count].age = age;
    t->rows[t->count].id = t->next_id++;
    return t->rows[t->count++].id;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t);
    }
}

Person *table_find_by_id(Table *t, uint32_t id) {
    if (!t) return NULL;
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) return &t->rows[i];
    }
    return NULL;
}

/* Update a row by ID. Return 0 on success, -1 if not found.
   The ID itself is never changed. */
int table_update_by_id(Table *t, uint32_t id, const char *name, uint32_t age) {
    /* TODO: find the row with the given ID, update name (bounded copy,
       NUL-terminate) and age, return 0. If not found, return -1. */
    (void)t; (void)id; (void)name; (void)age;
    return -1;
}

/* Delete a row by ID. Shift remaining rows down. Return 0 on success, -1 if not found. */
int table_delete_by_id(Table *t, uint32_t id) {
    /* TODO: find the row with the given ID, shift all rows after it one position down,
       decrement count, return 0. If not found, return -1. */
    (void)t; (void)id;
    return -1;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

Table *table_new(void);
uint32_t table_insert(Table *t, const char *name, uint32_t age);
void table_free(Table *t);
Person *table_find_by_id(Table *t, uint32_t id);
int table_update_by_id(Table *t, uint32_t id, const char *name, uint32_t age);
int table_delete_by_id(Table *t, uint32_t id);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void assert_str_eq(const char *test, const char *got, const char *want) {
    if (strcmp(got, want) == 0) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got '%s', want '%s')\n", test, got, want);
        failed++;
    }
}

int main(void) {
    Table *t = table_new();

    table_insert(t, "Alice", 30);
    table_insert(t, "Bob", 25);
    table_insert(t, "Charlie", 35);

    /* Test update */
    assert_eq("update_exists", table_update_by_id(t, 2, "Robert", 26), 0);
    Person *p = table_find_by_id(t, 2);
    assert_str_eq("update_name_changed", p->name, "Robert");
    assert_eq("update_age_changed", p->age, 26);
    assert_eq("update_id_unchanged", p->id, 2);
    assert_eq("update_count_unchanged", t->count, 3);

    /* Test update non-existent ID */
    assert_eq("update_nonexistent", table_update_by_id(t, 99, "X", 0), -1);

    /* Test delete */
    assert_eq("delete_exists", table_delete_by_id(t, 2), 0);
    assert_eq("count_after_delete", t->count, 2);
    assert_eq("find_deleted_returns_null", table_find_by_id(t, 2) == NULL, 1);

    /* Verify remaining rows shifted correctly */
    assert_eq("remaining_id_1", t->rows[0].id, 1);
    assert_str_eq("remaining_name_1", t->rows[0].name, "Alice");
    assert_eq("remaining_id_3", t->rows[1].id, 3);
    assert_str_eq("remaining_name_3", t->rows[1].name, "Charlie");

    /* Deleted IDs are never reused: the next insert continues from next_id. */
    uint32_t new_id = table_insert(t, "Diana", 22);
    assert_eq("no_id_reuse", new_id, 4);

    /* Test delete non-existent ID */
    assert_eq("delete_nonexistent", table_delete_by_id(t, 99), -1);

    /* Test delete first row */
    assert_eq("delete_first", table_delete_by_id(t, 1), 0);
    assert_eq("count_after_delete_first", t->count, 2);
    assert_eq("remaining_after_delete_first", t->rows[0].id, 3);

    /* Test delete last row */
    assert_eq("delete_last", table_delete_by_id(t, 4), 0);
    assert_eq("count_after_delete_last", t->count, 1);
    assert_eq("remaining_after_delete_last", t->rows[0].id, 3);

    table_free(t);

    return failed > 0 ? 1 : 0;
}
```

# Lesson: Basic Indexing {#indexing}

You've built a database that can insert, update, delete, and query. But every
lookup scans the entire table — O(n). For large tables, this is slow, and it's
slow in the way that hurts most in production: everything works fine in
development with 100 rows, then the table hits 10 million and every request
does 10 million comparisons.

The solution is **indexing**: a second data structure, maintained alongside the
table, that maps column values to row locations so lookups can skip the scan.

## Why Indexes Are a Trade, Not a Free Win

An index is **redundant data**. Everything in it can be derived from the table
— which means every write to the table must *also* update every index, or the
index silently lies. This is the fundamental index bargain:

- **Reads get faster**: O(n) scan → O(1) hash lookup or O(log n) tree descent.
- **Writes get slower**: one insert becomes 1 + (number of indexes) updates.
- **Consistency becomes your problem**: the index and the table must agree
  after *every* operation, including the weird ones (deletes that shift rows,
  loads that replace the table). Most index bugs are consistency bugs.

That's why "just index everything" is wrong: a table with ten indexes does
eleven writes per insert. Choosing which columns deserve indexes — based on
which queries actually run — is a core part of database tuning.

## Choosing the Structure: Hash vs. Sorted vs. B-Tree

An index maps column values to row positions. What structure holds the map?

- A **hash table** gives O(1) exact-match lookup (`id == 5`) but is useless
  for ranges (`age > 25`) — hashing destroys ordering by design.
- A **sorted array** gives O(log n) binary-search lookup *and* ranges (find
  the first match, walk forward), but inserting into the middle is O(n).
- A **balanced tree** gives O(log n) everything.

So why does every serious database use **B-Trees** rather than the binary
trees you learned first? Disk, again. A binary tree node holds one key and two
children, so finding one row in a billion takes ~30 node visits — and on disk,
each visit is a page read. A B-Tree node is sized to exactly one page (4 KB)
and holds *hundreds* of keys, so the tree's fanout is huge: with 200 keys per
node, a billion rows is `log₂₀₀(10⁹)` ≈ **4 levels**. Four page reads instead
of thirty — and the top levels are hot enough to always be in cache, so
usually one or two. The B-Tree is what "cache-line-friendly array" (lesson 1)
looks like when generalized into a tree: pack as much decision-making as
possible into each unit of I/O.

The violet-bordered box is one node (its keys and child pointers interleave);
each `page` shape below is the subtree a pointer leads to — three keys fan out
to four children, and real nodes hold hundreds:

```d2
direction: down

node: "B-Tree node = one 4 KB page" {
  shape: sql_table
  style.stroke: "#a78bfa"
  style.stroke-width: 2
  c0: "child ptr"
  k1: "key 25"
  c1: "child ptr"
  k2: "key 50"
  c2: "child ptr"
  k3: "key 75"
  c3: "child ptr"
}

l0: "keys < 25" { shape: page }
l1: "25 – 50" { shape: page }
l2: "50 – 75" { shape: page }
l3: "keys > 75" { shape: page }

node.c0 -> l0
node.c1 -> l1
node.c2 -> l2
node.c3 -> l3
```

We'll build the honest starter version: an index over the unique ID column,
stored as a growing array of `{id, row_index}` pairs. Its lookup is still
O(n) *inside the index* — the structure is a stand-in — but it forces you to
solve the real problem, which is **maintenance**: keeping table and index in
lockstep through inserts and deletes.

## The Maintenance Problem (Read This Before Coding)

Insert is easy: append `{id, row_position}` to the index. Delete is where it
gets interesting, because our table uses remove-and-shift: deleting the row at
position p moves *every row after p* down by one. The index entry for the
deleted ID must go — but the index entries for all those shifted rows now
point one position too high. After every delete you must also walk the index
and decrement every `row_index` greater than p.

Miss that and you get the classic index-corruption bug: lookups that return
the *wrong row* rather than failing. (This, incidentally, is another reason
real databases prefer tombstones — positions never change, so indexes never
need mass fix-ups — and why B-Tree indexes point at stable row *identifiers*
rather than physical positions.)

## Challenge: ID Index {#id-index points=25}

Implement an index on the ID column. Optimize `table_find_by_id` to use the
index instead of scanning the table. Maintain the index during inserts and
deletes — including the row_index fix-up after each delete.

### Starter

```c
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

typedef struct {
    uint32_t id;
    uint32_t row_index;
} IDIndexEntry;

typedef struct {
    IDIndexEntry *entries;
    uint32_t count;
    uint32_t capacity;
} IDIndex;

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    return t;
}

IDIndex *index_new(void) {
    IDIndex *idx = malloc(sizeof(IDIndex));
    if (!idx) return NULL;
    idx->entries = NULL;
    idx->count = 0;
    idx->capacity = 0;
    return idx;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t);
    }
}

void index_free(IDIndex *idx) {
    if (idx) {
        free(idx->entries);
        free(idx);
    }
}

/* Add an entry to the index. Return 0 on success, -1 on error. */
int index_insert(IDIndex *idx, uint32_t id, uint32_t row_index) {
    if (!idx) return -1;
    if (idx->count >= idx->capacity) {
        uint32_t new_cap = idx->capacity == 0 ? 16 : idx->capacity * 2;
        IDIndexEntry *new_entries = realloc(idx->entries, new_cap * sizeof(IDIndexEntry));
        if (!new_entries) return -1;
        idx->entries = new_entries;
        idx->capacity = new_cap;
    }
    idx->entries[idx->count].id = id;
    idx->entries[idx->count].row_index = row_index;
    idx->count++;
    return 0;
}

/* Find the row index for a given ID. Return the row index, or -1 if not found. */
int index_lookup(IDIndex *idx, uint32_t id) {
    if (!idx) return -1;
    for (uint32_t i = 0; i < idx->count; i++) {
        if (idx->entries[i].id == id) return (int)idx->entries[i].row_index;
    }
    return -1;
}

/* Remove an entry from the index and shift remaining entries down. */
int index_delete(IDIndex *idx, uint32_t id) {
    if (!idx) return -1;
    for (uint32_t i = 0; i < idx->count; i++) {
        if (idx->entries[i].id == id) {
            /* Shift entries after i down */
            for (uint32_t j = i; j < idx->count - 1; j++) {
                idx->entries[j] = idx->entries[j + 1];
            }
            idx->count--;
            return 0;
        }
    }
    return -1;
}

/* When a row is deleted from the table, every index entry pointing past the
   deleted position is now off by one. Fix them up. */
void index_shift_down(IDIndex *idx, uint32_t row_pos) {
    if (!idx) return;
    for (uint32_t i = 0; i < idx->count; i++) {
        if (idx->entries[i].row_index > row_pos) {
            idx->entries[i].row_index--;
        }
    }
}

uint32_t table_insert(Table *t, IDIndex *idx, const char *name, uint32_t age) {
    /* TODO: grow the table and insert the row as before — but also add an
       index entry mapping the new ID to its row position. If the index
       insert fails, do not count the row as inserted (return 0). */
    (void)t; (void)idx; (void)name; (void)age;
    return 0;
}

Person *table_find_by_id(Table *t, IDIndex *idx, uint32_t id) {
    /* TODO: use index_lookup to find the row index, then return &t->rows[row_index].
       No table scan allowed. Return NULL if the index doesn't know the ID. */
    (void)t; (void)idx; (void)id;
    return NULL;
}

int table_update_by_id(Table *t, IDIndex *idx, uint32_t id, const char *name, uint32_t age) {
    Person *p = table_find_by_id(t, idx, id);
    if (!p) return -1;
    strncpy(p->name, name, sizeof(p->name) - 1);
    p->name[sizeof(p->name) - 1] = '\0';
    p->age = age;
    return 0;
}

int table_delete_by_id(Table *t, IDIndex *idx, uint32_t id) {
    if (!t || !idx) return -1;

    /* TODO: find the row's position via the index. Delete the row from the
       table (shift rows down). Remove the ID from the index. Then fix up the
       remaining index entries with index_shift_down. Return 0, or -1 if the
       ID wasn't found. */
    (void)id;
    return -1;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
} Table;

typedef struct {
    uint32_t id;
    uint32_t row_index;
} IDIndexEntry;

typedef struct {
    IDIndexEntry *entries;
    uint32_t count;
    uint32_t capacity;
} IDIndex;

Table *table_new(void);
IDIndex *index_new(void);
void table_free(Table *t);
void index_free(IDIndex *idx);
int index_insert(IDIndex *idx, uint32_t id, uint32_t row_index);
int index_lookup(IDIndex *idx, uint32_t id);
int index_delete(IDIndex *idx, uint32_t id);
void index_shift_down(IDIndex *idx, uint32_t row_pos);
uint32_t table_insert(Table *t, IDIndex *idx, const char *name, uint32_t age);
Person *table_find_by_id(Table *t, IDIndex *idx, uint32_t id);
int table_update_by_id(Table *t, IDIndex *idx, uint32_t id, const char *name, uint32_t age);
int table_delete_by_id(Table *t, IDIndex *idx, uint32_t id);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void assert_str_eq(const char *test, const char *got, const char *want) {
    if (strcmp(got, want) == 0) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got '%s', want '%s')\n", test, got, want);
        failed++;
    }
}

int main(void) {
    Table *t = table_new();
    IDIndex *idx = index_new();

    /* Insert rows with indexed lookup. */
    uint32_t id1 = table_insert(t, idx, "Alice", 30);
    uint32_t id2 = table_insert(t, idx, "Bob", 25);
    uint32_t id3 = table_insert(t, idx, "Charlie", 35);

    assert_eq("insert_id1", id1, 1);
    assert_eq("insert_id2", id2, 2);
    assert_eq("insert_id3", id3, 3);
    assert_eq("index_count", idx->count, 3);

    /* Use index for fast lookup. */
    Person *p = table_find_by_id(t, idx, 2);
    assert_eq("find_by_id_found", p != NULL, 1);
    if (p) assert_str_eq("find_by_id_name", p->name, "Bob");

    /* Lookup of unknown ID fails cleanly. */
    assert_eq("find_unknown_null", table_find_by_id(t, idx, 42) == NULL, 1);

    /* Update via index. */
    assert_eq("update_via_index", table_update_by_id(t, idx, 2, "Robert", 26), 0);
    p = table_find_by_id(t, idx, 2);
    if (p) assert_str_eq("update_via_index_name", p->name, "Robert");

    /* Delete and verify the index is fixed up. */
    assert_eq("delete_via_index", table_delete_by_id(t, idx, 2), 0);
    assert_eq("table_count_after_delete", t->count, 2);
    assert_eq("index_count_after_delete", idx->count, 2);

    /* ID 3's row physically moved from position 2 to 1; the index must have
       followed it, and the lookup must return the RIGHT row, not just any row. */
    p = table_find_by_id(t, idx, 3);
    assert_eq("find_after_shift", p != NULL, 1);
    if (p) {
        assert_eq("find_after_shift_right_row", p->id, 3);
        assert_str_eq("find_after_shift_name", p->name, "Charlie");
    }

    /* Delete the first row too; ID 3 shifts again to position 0. */
    assert_eq("delete_first_via_index", table_delete_by_id(t, idx, 1), 0);
    p = table_find_by_id(t, idx, 3);
    assert_eq("find_after_second_shift", p != NULL, 1);
    if (p) assert_eq("second_shift_right_row", p->id, 3);

    /* Deleting an unknown ID reports failure and changes nothing. */
    assert_eq("delete_unknown", table_delete_by_id(t, idx, 42), -1);
    assert_eq("counts_stable_table", t->count, 1);
    assert_eq("counts_stable_index", idx->count, 1);

    table_free(t);
    index_free(idx);

    return failed > 0 ? 1 : 0;
}
```

# Lesson: Transactions — All or Nothing {#transactions}

Suppose you're moving money between two accounts: subtract 100 from row A, add
100 to row B. Your program crashes between the two updates. The database now
says the money vanished. No sequence of careful single-row operations can fix
this class of problem — what you need is a way to make *several* changes act
as *one* change. That's a **transaction**.

## ACID, Honestly

Transactions promise four properties, remembered as **ACID**:

- **Atomicity** — all of the transaction's changes happen, or none do. No
  in-between state is ever visible, even after a crash.
- **Consistency** — the database moves from one valid state to another;
  invariants (like "accounts never go negative", or *our* invariant, "the
  index agrees with the table") hold before and after.
- **Isolation** — concurrent transactions don't see each other's half-done
  work. (We'll meet the machinery for this in the concurrency lesson.)
- **Durability** — once committed, the changes survive power loss. (You built
  this in the durability lesson — commit is exactly where `fsync` goes.)

Notice these aren't four independent features — they interlock. Atomicity
without durability is pointless (all-or-nothing... until reboot). Isolation
without atomicity is meaningless (isolated from *which* state?). This lesson
adds the A; you already have the D; the C emerges from doing both right.

## How Do You Un-Do? Three Strategies

Atomicity means the database must be able to **undo** work. There are three
classic ways to keep the undo information, and they map directly onto real
systems:

1. **Snapshot**: before the transaction starts, copy the entire state. To roll
   back, restore the copy; to commit, discard it. Dead simple, brutally
   expensive at scale — you copy everything to protect anything.

2. **Undo log (rollback journal)**: before modifying each page/row, save just
   the *old version* of that piece to a side log. Rollback replays the log
   backwards; commit discards the log. Cost is proportional to what you
   *changed*, not what you *have*. Classic SQLite works this way: before
   touching a page, the original page is copied into `<db>-journal`; crash
   recovery means "if a journal exists, play it back".

3. **Redo log (write-ahead log / WAL)**: invert the idea — write the *new*
   versions to an append-only log first, and only later apply them to the real
   database. Rollback is trivial (the main file was never touched); commit is
   one fsync of the log. Crash recovery replays committed log entries forward.
   This is SQLite's WAL mode, Postgres's WAL, and essentially every modern
   engine — because appending to one log file is the cheapest durable write
   there is (sequential I/O, one fsync).

We'll implement the snapshot strategy: our table is small and lives in memory,
so "copy the rows array" is one `memcpy` — and it makes the semantics
crystal clear before you meet the log-based versions in real engines. Notice
the recipe is exactly the durability lesson's temp-file trick, relocated into
memory: *never modify the only copy; build the new state beside the old, then
switch atomically.* The same idea keeps reappearing at every layer of a
database.

## Semantics: The Fine Print

Getting transaction *semantics* right matters more than the mechanism:

- **No nesting** (for us): `BEGIN` inside a transaction is an error. Real
  systems layer *savepoints* on top for partial rollback; the flat version
  must work first.
- **Commit/rollback without begin** is an error, not a no-op. Silently
  accepting them hides bugs in the caller's transaction discipline.
- **Rollback restores everything** — including `next_id` in our version. Fun
  fact: real databases deliberately do *not* roll back their ID sequences
  (a rolled-back insert burns the ID forever). That's a concession to
  concurrency — making sequences transactional would serialize all inserters
  — and it's why production tables have gaps in their ID columns. Our
  single-threaded snapshot restores the counter for free, so we do.

## Challenge: Begin, Commit, Rollback {#txn-rollback points=25}

Implement snapshot-based transactions: `table_begin` captures the state,
`table_rollback` restores it exactly (rows, count, next_id), `table_commit`
discards the snapshot and keeps the changes. Enforce the semantics: no nested
begin, no commit/rollback outside a transaction.

### Starter

```c
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
    /* Transaction snapshot: only meaningful while in_txn is 1. */
    Person *snap_rows;
    uint32_t snap_count;
    uint32_t snap_next_id;
    int in_txn;
} Table;

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    t->snap_rows = NULL;
    t->snap_count = 0;
    t->snap_next_id = 0;
    t->in_txn = 0;
    return t;
}

uint32_t table_insert(Table *t, const char *name, uint32_t age) {
    if (!t) return 0;
    if (t->count >= t->capacity) {
        uint32_t new_cap = t->capacity == 0 ? 16 : t->capacity * 2;
        Person *new_rows = realloc(t->rows, new_cap * sizeof(Person));
        if (!new_rows) return 0;
        t->rows = new_rows;
        t->capacity = new_cap;
    }
    strncpy(t->rows[t->count].name, name, sizeof(t->rows[t->count].name) - 1);
    t->rows[t->count].name[sizeof(t->rows[t->count].name) - 1] = '\0';
    t->rows[t->count].age = age;
    t->rows[t->count].id = t->next_id++;
    return t->rows[t->count++].id;
}

Person *table_find_by_id(Table *t, uint32_t id) {
    if (!t) return NULL;
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) return &t->rows[i];
    }
    return NULL;
}

int table_delete_by_id(Table *t, uint32_t id) {
    if (!t) return -1;
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) {
            for (uint32_t j = i; j < t->count - 1; j++) {
                t->rows[j] = t->rows[j + 1];
            }
            t->count--;
            return 0;
        }
    }
    return -1;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t->snap_rows);
        free(t);
    }
}

/* Begin a transaction: snapshot rows, count, and next_id.
   Return -1 if already in a transaction (no nesting) or on allocation
   failure; 0 on success. An empty table snapshots to a NULL rows copy. */
int table_begin(Table *t) {
    /* TODO */
    (void)t;
    return -1;
}

/* Commit: discard the snapshot, keep current state.
   Return -1 if not in a transaction; 0 on success. */
int table_commit(Table *t) {
    /* TODO */
    (void)t;
    return -1;
}

/* Rollback: restore rows, count, and next_id exactly as they were at begin.
   The snapshot becomes the live rows array (capacity = snapshot count).
   Return -1 if not in a transaction; 0 on success. */
int table_rollback(Table *t) {
    /* TODO: free the current rows array, install the snapshot in its place,
       restore count/next_id, clear in_txn. Careful with ownership: after
       rollback the snapshot pointer must not be freed twice. */
    (void)t;
    return -1;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
    Person *snap_rows;
    uint32_t snap_count;
    uint32_t snap_next_id;
    int in_txn;
} Table;

Table *table_new(void);
uint32_t table_insert(Table *t, const char *name, uint32_t age);
Person *table_find_by_id(Table *t, uint32_t id);
int table_delete_by_id(Table *t, uint32_t id);
void table_free(Table *t);
int table_begin(Table *t);
int table_commit(Table *t);
int table_rollback(Table *t);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void assert_str_eq(const char *test, const char *got, const char *want) {
    if (strcmp(got, want) == 0) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got '%s', want '%s')\n", test, got, want);
        failed++;
    }
}

int main(void) {
    Table *t = table_new();

    table_insert(t, "Alice", 30);
    table_insert(t, "Bob", 25);
    assert_eq("setup_count", t->count, 2);

    /* Commit/rollback outside a transaction are errors. */
    assert_eq("commit_without_begin", table_commit(t), -1);
    assert_eq("rollback_without_begin", table_rollback(t), -1);

    /* Begin, mutate, roll back: state is exactly restored. */
    assert_eq("begin_ok", table_begin(t), 0);
    assert_eq("no_nested_begin", table_begin(t), -1);

    table_insert(t, "Charlie", 35);
    table_insert(t, "Diana", 22);
    assert_eq("txn_sees_own_inserts", t->count, 4);

    assert_eq("rollback_ok", table_rollback(t), 0);
    assert_eq("rollback_count", t->count, 2);
    assert_eq("rollback_next_id", t->next_id, 3);
    assert_eq("rolled_back_row_gone", table_find_by_id(t, 3) == NULL, 1);
    assert_str_eq("survivor_0", t->rows[0].name, "Alice");
    assert_str_eq("survivor_1", t->rows[1].name, "Bob");

    /* The table must remain fully usable after a rollback. */
    uint32_t id = table_insert(t, "Eve", 28);
    assert_eq("insert_after_rollback_id", id, 3);
    assert_eq("insert_after_rollback_count", t->count, 3);

    /* Begin, mutate, commit: changes stick. */
    assert_eq("begin2_ok", table_begin(t), 0);
    assert_eq("txn_delete", table_delete_by_id(t, 1), 0);
    assert_eq("commit_ok", table_commit(t), 0);
    assert_eq("commit_count", t->count, 2);
    assert_eq("committed_delete_stuck", table_find_by_id(t, 1) == NULL, 1);

    /* After commit, no transaction is active. */
    assert_eq("rollback_after_commit", table_rollback(t), -1);

    /* Rollback of a delete restores the deleted row's data. */
    assert_eq("begin3_ok", table_begin(t), 0);
    assert_eq("txn_delete_eve", table_delete_by_id(t, 3), 0);
    assert_eq("eve_gone_in_txn", table_find_by_id(t, 3) == NULL, 1);
    assert_eq("rollback3_ok", table_rollback(t), 0);
    Person *eve = table_find_by_id(t, 3);
    assert_eq("eve_restored", eve != NULL, 1);
    if (eve) assert_str_eq("eve_data_restored", eve->name, "Eve");

    /* An empty-table transaction works too. */
    Table *e = table_new();
    assert_eq("empty_begin", table_begin(e), 0);
    table_insert(e, "Ghost", 1);
    assert_eq("empty_rollback", table_rollback(e), 0);
    assert_eq("empty_restored", e->count, 0);
    assert_eq("empty_next_id_restored", e->next_id, 1);
    table_free(e);

    table_free(t);

    return failed > 0 ? 1 : 0;
}
```

# Lesson: Concurrency — Many Hands on One Table {#concurrency}

Everything so far assumed one thread. Real databases serve many connections at
once — and the moment two threads touch the same table, code that has worked
perfectly all course becomes a lottery. This lesson is about *why* it breaks
and the discipline that fixes it.

## What Actually Goes Wrong

Consider two threads both running our insert. It ends with:

```c
t->rows[t->count].id = t->next_id++;
return t->rows[t->count++].id;
```

`t->count++` looks atomic. It isn't — it compiles to *load, add, store*.
Interleave two threads:

```
Thread A: load count (=5)
Thread B: load count (=5)
Thread A: store 6, write row into slot 5
Thread B: store 6, write row into slot 5   ← overwrites A's row!
```

Two inserts, one row, count is 6 but slot 6 was never written — it's
uninitialized garbage that a later scan will happily serve as data. This is a
**race condition**, and it has the worst possible debugging profile: it's
timing-dependent, so it passes every test on your laptop and fires under
production load; adding printf changes the timing and makes it vanish (a
"heisenbug").

It gets worse. Our table *reallocs* when it grows. If thread A's insert
triggers `realloc` — which may move the whole array and free the old one —
while thread B is mid-scan holding a pointer into the old array, thread B is
now reading **freed memory**. And even without realloc, a reader can see a
*torn row*: writer has copied the name but not yet the age. There is no
"mostly safe" here: the C memory model says a data race is undefined
behavior, full stop.

## Critical Sections and Mutexes

The fix is **mutual exclusion**: mark the read-modify-write sequences that
must never interleave (**critical sections**) and let only one thread inside
at a time. POSIX gives us the mutex:

```c
pthread_mutex_lock(&t->lock);     /* blocks until the lock is free       */
/* ... critical section: this thread has exclusive access ... */
pthread_mutex_unlock(&t->lock);   /* next waiting thread may now enter   */
```

The rules that make mutexes work in practice:

- **The whole invariant, not the hot line.** The critical section must cover
  the *entire* sequence that takes the table from one valid state to another —
  grow-check, realloc, row copy, id assignment, count bump. Locking just
  `count++` still lets a reader see the row half-copied.
- **Every path unlocks.** Including early returns on allocation failure. A
  returned-without-unlock mutex deadlocks the next caller forever. (This is
  the C version of why other languages have `defer`/RAII.)
- **Never return pointers into locked state.** If `table_find_by_id` returns
  `&t->rows[i]` and then unlocks, the caller reads that pointer *outside* the
  lock — racing every future insert's realloc. The safe pattern is **copy
  out**: the lookup copies the row into a caller-provided struct while
  holding the lock. This is why the challenge below has a
  `table_find_copy(t, id, &out)` signature, and it's the same
  copies-vs-references tension you met in the querying lesson — concurrency
  turns "slightly risky" into "undefined behavior".

## Granularity: One Big Lock, and Why That's Respectable

We'll protect the whole table with a single mutex — every operation takes it.
Simple to reason about, obviously correct, and it serializes everything: two
CPU-heavy queries can't run simultaneously. The alternatives are a ladder of
complexity you climb only when profiling says you must:

- **Readers-writer locks** (`pthread_rwlock_t`): many concurrent readers OR
  one writer. Great when reads dominate.
- **Fine-grained locking**: lock per page/row-range so writers on different
  pages proceed in parallel. Now *deadlock* becomes possible — thread A holds
  lock 1 wanting 2, thread B holds 2 wanting 1 — and the classic cure is a
  global **lock ordering** (always acquire in address/page-number order).
- **MVCC**: writers create new row versions instead of mutating, so readers
  never block at all — Postgres's approach, and SQLite's WAL mode for readers.

For perspective: SQLite itself runs one big lock. A single writer at a time,
enforced with file locks; WAL mode relaxes readers, not writers. It handles
enormous workloads that way. "One big lock, correctly" beats "clever locks,
incorrectly" every time — you can always optimize a correct program.

## Challenge: Thread-Safe Table {#thread-safe-table points=25}

Make the table safe for concurrent use: `table_insert` and a copy-out lookup
`table_find_copy`, both holding the table's mutex for their entire critical
section. The tests hammer the table from 8 threads and then mix readers with
writers; a missing or too-narrow lock shows up as lost rows, duplicate IDs, or
a crash.

### Starter

```c
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
    pthread_mutex_t lock;
} Table;

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    if (pthread_mutex_init(&t->lock, NULL) != 0) {
        free(t);
        return NULL;
    }
    return t;
}

void table_free(Table *t) {
    if (t) {
        pthread_mutex_destroy(&t->lock);
        free(t->rows);
        free(t);
    }
}

/* Thread-safe insert. The ENTIRE grow-copy-assign sequence is one critical
   section. Returns the new row's ID, or 0 on allocation failure.
   Make sure every return path unlocks the mutex. */
uint32_t table_insert(Table *t, const char *name, uint32_t age) {
    if (!t) return 0;
    /* TODO: lock; grow if needed (double, or 16 if 0 — unlock and return 0
       on realloc failure); copy name (bounded, NUL-terminated); set age;
       assign id from next_id++; increment count; unlock; return the id. */
    (void)name; (void)age;
    return 0;
}

/* Thread-safe lookup that COPIES the row into *out while holding the lock.
   Returns 1 if found, 0 if not. We copy instead of returning a pointer
   because a pointer into rows[] is only valid while the lock is held —
   the next insert may realloc the array out from under it. */
int table_find_copy(Table *t, uint32_t id, Person *out) {
    if (!t || !out) return 0;
    /* TODO: lock; linear-scan for id; if found, copy the row into *out;
       unlock; return found. */
    (void)id;
    return 0;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>
#include <pthread.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
    pthread_mutex_t lock;
} Table;

Table *table_new(void);
void table_free(Table *t);
uint32_t table_insert(Table *t, const char *name, uint32_t age);
int table_find_copy(Table *t, uint32_t id, Person *out);

#define WRITERS 8
#define PER_WRITER 2000
#define TOTAL (WRITERS * PER_WRITER)

static Table *g_table;
static volatile int g_start;

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void *writer(void *arg) {
    long tid = (long)arg;
    while (!g_start) { /* spin so all threads start together */ }
    for (int i = 0; i < PER_WRITER; i++) {
        char name[32];
        snprintf(name, sizeof(name), "w%ld-%d", tid, i);
        if (table_insert(g_table, name, (uint32_t)(20 + tid)) == 0)
            return (void *)1; /* insert failed */
    }
    return NULL;
}

/* Readers run table_find_copy while writers are still inserting. A found row
   whose id doesn't match the query means a torn read; count the mismatches. */
static void *reader(void *arg) {
    long mismatches = 0;
    (void)arg;
    while (!g_start) { }
    for (int i = 0; i < 5000; i++) {
        Person out;
        uint32_t want = (uint32_t)(i % TOTAL) + 1;
        if (table_find_copy(g_table, want, &out) && out.id != want)
            mismatches++;
    }
    return (void *)mismatches;
}

int main(void) {
    g_table = table_new();

    /* Phase 1: 8 concurrent writers. */
    pthread_t writers[WRITERS];
    g_start = 0;
    for (long i = 0; i < WRITERS; i++)
        pthread_create(&writers[i], NULL, writer, (void *)i);
    g_start = 1;

    int insert_failures = 0;
    for (int i = 0; i < WRITERS; i++) {
        void *ret;
        pthread_join(writers[i], &ret);
        if (ret != NULL) insert_failures++;
    }

    assert_eq("no_insert_failures", insert_failures, 0);
    assert_eq("count_exact", g_table->count, TOTAL);
    assert_eq("next_id_exact", g_table->next_id, TOTAL + 1);
    assert_eq("capacity_holds_count", g_table->capacity >= g_table->count, 1);

    /* Every ID in 1..TOTAL must appear exactly once — lost updates create
       duplicates and gaps. */
    static unsigned char seen[TOTAL + 1];
    int dups = 0, out_of_range = 0;
    for (uint32_t i = 0; i < g_table->count; i++) {
        uint32_t id = g_table->rows[i].id;
        if (id < 1 || id > TOTAL) { out_of_range++; continue; }
        if (seen[id]) dups++;
        seen[id] = 1;
    }
    int missing = 0;
    for (uint32_t id = 1; id <= TOTAL; id++)
        if (!seen[id]) missing++;

    assert_eq("no_out_of_range_ids", out_of_range, 0);
    assert_eq("no_duplicate_ids", dups, 0);
    assert_eq("no_missing_ids", missing, 0);

    /* Lookup returns a correct copy. */
    Person out;
    assert_eq("find_copy_hit", table_find_copy(g_table, 1, &out), 1);
    assert_eq("find_copy_right_id", out.id, 1);
    assert_eq("find_copy_miss", table_find_copy(g_table, TOTAL + 999, &out), 0);

    /* Phase 2: readers and writers at the same time. Success = correct final
       count and zero torn reads (and, implicitly, no crash). */
    pthread_t rthreads[4], wthreads[4];
    g_start = 0;
    for (long i = 0; i < 4; i++) {
        pthread_create(&rthreads[i], NULL, reader, NULL);
        pthread_create(&wthreads[i], NULL, writer, (void *)(100 + i));
    }
    g_start = 1;

    long total_mismatches = 0;
    for (int i = 0; i < 4; i++) {
        void *ret;
        pthread_join(rthreads[i], &ret);
        total_mismatches += (long)ret;
        pthread_join(wthreads[i], &ret);
    }

    assert_eq("no_torn_reads", (int)total_mismatches, 0);
    assert_eq("mixed_phase_count", g_table->count, TOTAL + 4 * PER_WRITER);

    table_free(g_table);

    return failed > 0 ? 1 : 0;
}
```

# Final Challenge: Full Database Engine {#final points=75}

Combine everything into a complete database engine driven by a command parser
— the front door that turns text into the operations you've built:

1. **In-memory table** with dynamic growth.
2. **Atomic, durable file persistence** (write-temp + fsync + rename).
3. **Predicates and queries** to filter rows.
4. **Update and delete** operations.
5. **ID indexing** for fast lookups — kept consistent through every operation.
6. **Transactions** with begin/commit/rollback.

You'll implement `db_execute`, a command parser that reads strings like
`INSERT Alice 30` and dispatches to the right operations. This simulates a
real database's execution pipeline: **parse** the text, **validate** it,
**plan** (here: pick the operation), **execute**, and expose results. It's a
miniature of what happens to every SQL statement — SQLite compiles SQL to
bytecode for a virtual machine; our "bytecode" is just a dispatch on the verb.

The pipeline reads left to right; the cyan-bordered stage is the "virtual
machine" — for us, just a `switch` on the verb:

```d2
direction: right

cmd: "\"INSERT\nAlice 30\"" { shape: oval }
parse: "parse +\nvalidate"
dispatch: "dispatch\non verb (VM)" {
  style.stroke: "#22d3ee"
  style.stroke-width: 2
}
exec: "run table op\n-> 0 / -1" { shape: oval }

cmd -> parse -> dispatch -> exec
```

All the building blocks from previous lessons are provided complete. Your work
is the glue — and the glue is where consistency lives: LOAD must rebuild the
index for the new table; ROLLBACK must rebuild it too (the snapshot restored
old row positions, so the index is stale — this is Consistency from the ACID
lesson, enforced by *you*); SELECT must free the previous result before
storing a new one (no leaks in a long-running process).

### Command Format

- `INSERT <name> <age>` — insert a row, record its ID in `last_insert_id`
- `SELECT <column> <op> <value>` — query with a predicate (`op` is one of
  `==` `<` `>` `<=` `>=`); store matches in `db->result`, freeing any previous
  result first
- `UPDATE <id> <name> <age>` — update a row by ID (via the index)
- `DELETE <id>` — delete a row by ID (via the index, with fix-up)
- `BEGIN` / `COMMIT` / `ROLLBACK` — transaction control; ROLLBACK rebuilds
  the index after restoring the snapshot
- `SAVE <path>` — atomically persist the table
- `LOAD <path>` — load a table from disk, replace the in-memory table, rebuild
  the index
- `COUNT` — succeed (row count is readable as `db->data->count`)

Return 0 on success, -1 for unknown/malformed commands or failed operations.

### Starter

```c
#define _POSIX_C_SOURCE 200809L

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <unistd.h>

#define DB_MAGIC 0xCAFEBABE
#define DB_VERSION 1

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
    Person *snap_rows;
    uint32_t snap_count;
    uint32_t snap_next_id;
    int in_txn;
} Table;

typedef struct {
    enum { PRED_EQ, PRED_LT, PRED_GT, PRED_LE, PRED_GE } op;
    char column[32];
    uint32_t int_value;
    char str_value[256];
} Predicate;

typedef struct {
    uint32_t id;
    uint32_t row_index;
} IDIndexEntry;

typedef struct {
    IDIndexEntry *entries;
    uint32_t count;
    uint32_t capacity;
} IDIndex;

typedef struct {
    Table *data;
    IDIndex *index;
    Table *result;           /* last SELECT result, owned by the Database */
    uint32_t last_insert_id;
} Database;

/* ---- table ---- */

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    t->snap_rows = NULL;
    t->snap_count = 0;
    t->snap_next_id = 0;
    t->in_txn = 0;
    return t;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t->snap_rows);
        free(t);
    }
}

/* ---- index ---- */

IDIndex *index_new(void) {
    IDIndex *idx = malloc(sizeof(IDIndex));
    if (!idx) return NULL;
    idx->entries = NULL;
    idx->count = 0;
    idx->capacity = 0;
    return idx;
}

void index_free(IDIndex *idx) {
    if (idx) {
        free(idx->entries);
        free(idx);
    }
}

int index_insert(IDIndex *idx, uint32_t id, uint32_t row_index) {
    if (!idx) return -1;
    if (idx->count >= idx->capacity) {
        uint32_t new_cap = idx->capacity == 0 ? 16 : idx->capacity * 2;
        IDIndexEntry *new_entries = realloc(idx->entries, new_cap * sizeof(IDIndexEntry));
        if (!new_entries) return -1;
        idx->entries = new_entries;
        idx->capacity = new_cap;
    }
    idx->entries[idx->count].id = id;
    idx->entries[idx->count].row_index = row_index;
    idx->count++;
    return 0;
}

int index_lookup(IDIndex *idx, uint32_t id) {
    if (!idx) return -1;
    for (uint32_t i = 0; i < idx->count; i++) {
        if (idx->entries[i].id == id) return (int)idx->entries[i].row_index;
    }
    return -1;
}

int index_delete(IDIndex *idx, uint32_t id) {
    if (!idx) return -1;
    for (uint32_t i = 0; i < idx->count; i++) {
        if (idx->entries[i].id == id) {
            for (uint32_t j = i; j < idx->count - 1; j++) {
                idx->entries[j] = idx->entries[j + 1];
            }
            idx->count--;
            return 0;
        }
    }
    return -1;
}

void index_shift_down(IDIndex *idx, uint32_t row_pos) {
    if (!idx) return;
    for (uint32_t i = 0; i < idx->count; i++) {
        if (idx->entries[i].row_index > row_pos) {
            idx->entries[i].row_index--;
        }
    }
}

void index_rebuild(IDIndex *idx, Table *t) {
    if (!idx || !t) return;
    idx->count = 0;
    for (uint32_t i = 0; i < t->count; i++) {
        index_insert(idx, t->rows[i].id, i);
    }
}

/* ---- table operations (idx may be NULL for unindexed tables,
        e.g. query results) ---- */

uint32_t table_insert(Table *t, IDIndex *idx, const char *name, uint32_t age) {
    if (!t) return 0;
    if (t->count >= t->capacity) {
        uint32_t new_cap = t->capacity == 0 ? 16 : t->capacity * 2;
        Person *new_rows = realloc(t->rows, new_cap * sizeof(Person));
        if (!new_rows) return 0;
        t->rows = new_rows;
        t->capacity = new_cap;
    }
    strncpy(t->rows[t->count].name, name, sizeof(t->rows[t->count].name) - 1);
    t->rows[t->count].name[sizeof(t->rows[t->count].name) - 1] = '\0';
    t->rows[t->count].age = age;
    t->rows[t->count].id = t->next_id++;

    uint32_t assigned_id = t->rows[t->count].id;
    if (idx && index_insert(idx, assigned_id, t->count) != 0) return 0;

    t->count++;
    return assigned_id;
}

Person *table_find_by_id(Table *t, IDIndex *idx, uint32_t id) {
    if (!t || !idx) return NULL;
    int row_index = index_lookup(idx, id);
    if (row_index < 0) return NULL;
    return &t->rows[row_index];
}

int table_update_by_id(Table *t, IDIndex *idx, uint32_t id, const char *name, uint32_t age) {
    Person *p = table_find_by_id(t, idx, id);
    if (!p) return -1;
    strncpy(p->name, name, sizeof(p->name) - 1);
    p->name[sizeof(p->name) - 1] = '\0';
    p->age = age;
    return 0;
}

int table_delete_by_id(Table *t, IDIndex *idx, uint32_t id) {
    if (!t || !idx) return -1;
    int row_pos = index_lookup(idx, id);
    if (row_pos < 0) return -1;

    for (uint32_t j = row_pos; j < t->count - 1; j++) {
        t->rows[j] = t->rows[j + 1];
    }
    t->count--;
    index_delete(idx, id);
    index_shift_down(idx, row_pos);
    return 0;
}

/* ---- predicates ---- */

int predicate_matches(Person *p, Predicate pred) {
    if (strcmp(pred.column, "age") == 0) {
        if (pred.op == PRED_EQ) return p->age == pred.int_value;
        if (pred.op == PRED_LT) return p->age < pred.int_value;
        if (pred.op == PRED_GT) return p->age > pred.int_value;
        if (pred.op == PRED_LE) return p->age <= pred.int_value;
        if (pred.op == PRED_GE) return p->age >= pred.int_value;
    }
    if (strcmp(pred.column, "name") == 0) {
        int c = strcmp(p->name, pred.str_value);
        if (pred.op == PRED_EQ) return c == 0;
        if (pred.op == PRED_LT) return c < 0;
        if (pred.op == PRED_GT) return c > 0;
        if (pred.op == PRED_LE) return c <= 0;
        if (pred.op == PRED_GE) return c >= 0;
    }
    return 0;
}

Table *table_query(Table *t, Predicate pred) {
    if (!t) return NULL;
    Table *result = table_new();
    if (!result) return NULL;
    for (uint32_t i = 0; i < t->count; i++) {
        if (predicate_matches(&t->rows[i], pred)) {
            if (!table_insert(result, NULL, t->rows[i].name, t->rows[i].age)) {
                table_free(result);
                return NULL;
            }
        }
    }
    return result;
}

/* ---- persistence (atomic save from the durability lesson) ---- */

static int table_write_stream(Table *t, FILE *f) {
    uint32_t magic = DB_MAGIC;
    uint8_t version = DB_VERSION;
    if (fwrite(&magic, sizeof(magic), 1, f) != 1) return -1;
    if (fwrite(&version, sizeof(version), 1, f) != 1) return -1;
    if (fwrite(&t->count, sizeof(t->count), 1, f) != 1) return -1;
    if (fwrite(&t->next_id, sizeof(t->next_id), 1, f) != 1) return -1;
    if (t->count > 0 && fwrite(t->rows, sizeof(Person), t->count, f) != t->count)
        return -1;
    return 0;
}

int table_save_atomic(Table *t, const char *path) {
    if (!t || !path) return -1;
    char tmp[4096];
    if (snprintf(tmp, sizeof(tmp), "%s.tmp", path) >= (int)sizeof(tmp))
        return -1;

    FILE *f = fopen(tmp, "wb");
    if (!f) return -1;

    if (table_write_stream(t, f) != 0) goto err;
    if (fflush(f) != 0) goto err;
    if (fsync(fileno(f)) != 0) goto err;
    if (fclose(f) != 0) { f = NULL; goto err; }
    f = NULL;

    if (rename(tmp, path) != 0) goto err;
    return 0;
err:
    if (f) fclose(f);
    remove(tmp);
    return -1;
}

Table *table_load(const char *path) {
    if (!path) return NULL;
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;

    Table *t = table_new();
    if (!t) { fclose(f); return NULL; }

    uint32_t magic;
    uint8_t version;
    if (fread(&magic, sizeof(magic), 1, f) != 1 || magic != DB_MAGIC) goto err;
    if (fread(&version, sizeof(version), 1, f) != 1 || version != DB_VERSION) goto err;
    if (fread(&t->count, sizeof(t->count), 1, f) != 1) goto err;
    if (fread(&t->next_id, sizeof(t->next_id), 1, f) != 1) goto err;

    if (t->count > 0) {
        t->rows = malloc(t->count * sizeof(Person));
        if (!t->rows) goto err;
        if (fread(t->rows, sizeof(Person), t->count, f) != t->count) goto err;
    }
    t->capacity = t->count;

    fclose(f);
    return t;
err:
    fclose(f);
    table_free(t);
    return NULL;
}

/* ---- transactions ---- */

int table_begin(Table *t) {
    if (!t || t->in_txn) return -1;
    t->snap_count = t->count;
    t->snap_next_id = t->next_id;
    t->snap_rows = NULL;
    if (t->count > 0) {
        t->snap_rows = malloc(t->count * sizeof(Person));
        if (!t->snap_rows) return -1;
        memcpy(t->snap_rows, t->rows, t->count * sizeof(Person));
    }
    t->in_txn = 1;
    return 0;
}

int table_commit(Table *t) {
    if (!t || !t->in_txn) return -1;
    free(t->snap_rows);
    t->snap_rows = NULL;
    t->in_txn = 0;
    return 0;
}

int table_rollback(Table *t) {
    if (!t || !t->in_txn) return -1;
    free(t->rows);
    t->rows = t->snap_rows;
    t->count = t->snap_count;
    t->capacity = t->snap_count;
    t->next_id = t->snap_next_id;
    t->snap_rows = NULL;
    t->in_txn = 0;
    return 0;
}

/* ---- database API ---- */

Database *db_new(void) {
    Database *db = malloc(sizeof(Database));
    if (!db) return NULL;
    db->data = table_new();
    db->index = index_new();
    db->result = NULL;
    db->last_insert_id = 0;
    if (!db->data || !db->index) {
        table_free(db->data);
        index_free(db->index);
        free(db);
        return NULL;
    }
    return db;
}

void db_free(Database *db) {
    if (db) {
        table_free(db->data);
        index_free(db->index);
        if (db->result) table_free(db->result);
        free(db);
    }
}

/* Execute a command. Return 0 on success, -1 on error.

   Parsing hints:
   - Dispatch on the verb: strncmp(cmd, "INSERT ", 7) etc.; exact strcmp for
     the argument-less verbs (COUNT, BEGIN, COMMIT, ROLLBACK).
   - sscanf does the heavy lifting: "INSERT %255s %u", "UPDATE %u %255s %u",
     "DELETE %u", "SELECT %31s %7s %255s", "SAVE %4095s", "LOAD %4095s".
     Check that sscanf matched the expected number of fields.
   - SELECT: map the op token (== < > <= >=) onto the Predicate enum; for
     column "age" the value parses with strtoul into int_value, for "name" it
     copies into str_value. Free any previous db->result first.
   - LOAD: table_load into a new table; only on success free the old table,
     install the new one, and index_rebuild. A failed LOAD must leave the
     database untouched.
   - ROLLBACK: after table_rollback succeeds, index_rebuild — the restored
     rows are in their old positions and the index is stale. */
int db_execute(Database *db, const char *cmd) {
    /* TODO */
    (void)db; (void)cmd;
    return -1;
}
```

### Tests

```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>

typedef struct {
    uint32_t id;
    char name[256];
    uint32_t age;
} Person;

typedef struct {
    Person *rows;
    uint32_t count;
    uint32_t capacity;
    uint32_t next_id;
    Person *snap_rows;
    uint32_t snap_count;
    uint32_t snap_next_id;
    int in_txn;
} Table;

typedef struct {
    uint32_t id;
    uint32_t row_index;
} IDIndexEntry;

typedef struct {
    IDIndexEntry *entries;
    uint32_t count;
    uint32_t capacity;
} IDIndex;

typedef struct {
    Table *data;
    IDIndex *index;
    Table *result;
    uint32_t last_insert_id;
} Database;

Database *db_new(void);
void db_free(Database *db);
int db_execute(Database *db, const char *cmd);
void table_free(Table *t);

static int passed = 0, failed = 0;
static void assert_eq(const char *test, int got, int want) {
    if (got == want) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got %d, want %d)\n", test, got, want);
        failed++;
    }
}

static void assert_str_eq(const char *test, const char *got, const char *want) {
    if (strcmp(got, want) == 0) {
        printf("--- PASS: %s\n", test);
        passed++;
    } else {
        printf("--- FAIL: %s (got '%s', want '%s')\n", test, got, want);
        failed++;
    }
}

int main(void) {
    remove("/tmp/final.db");
    remove("/tmp/final.db.tmp");

    Database *db = db_new();

    /* INSERT */
    assert_eq("insert_alice", db_execute(db, "INSERT Alice 30"), 0);
    assert_eq("last_insert_id", db->last_insert_id, 1);
    assert_eq("insert_bob", db_execute(db, "INSERT Bob 25"), 0);
    assert_eq("insert_charlie", db_execute(db, "INSERT Charlie 35"), 0);
    assert_eq("count_after_inserts", db->data->count, 3);

    /* COUNT */
    assert_eq("count_cmd", db_execute(db, "COUNT"), 0);

    /* SELECT with a numeric predicate */
    assert_eq("select_age_gt_25", db_execute(db, "SELECT age > 25"), 0);
    assert_eq("select_result_set", db->result != NULL, 1);
    if (db->result) assert_eq("select_result_count", db->result->count, 2);

    /* SELECT with a string predicate (previous result must be replaced) */
    assert_eq("select_name_eq", db_execute(db, "SELECT name == Bob"), 0);
    if (db->result) {
        assert_eq("select_name_count", db->result->count, 1);
        assert_eq("select_name_age", db->result->rows[0].age, 25);
    }

    /* UPDATE via the index */
    assert_eq("update_bob", db_execute(db, "UPDATE 2 Robert 26"), 0);
    if (db->data->rows) assert_str_eq("update_name_check", db->data->rows[1].name, "Robert");
    assert_eq("update_missing", db_execute(db, "UPDATE 99 Nobody 1"), -1);

    /* Transactions: rollback undoes, and the index still works afterwards. */
    assert_eq("begin", db_execute(db, "BEGIN"), 0);
    assert_eq("txn_insert", db_execute(db, "INSERT Zed 99"), 0);
    assert_eq("txn_count", db->data->count, 4);
    assert_eq("rollback", db_execute(db, "ROLLBACK"), 0);
    assert_eq("rollback_count", db->data->count, 3);
    assert_eq("rollback_delete_gone_id", db_execute(db, "DELETE 4"), -1);
    assert_eq("index_alive_after_rollback", db_execute(db, "UPDATE 3 Chuck 36"), 0);
    if (db->data->rows) assert_str_eq("update_after_rollback", db->data->rows[2].name, "Chuck");

    assert_eq("commit_without_begin", db_execute(db, "COMMIT"), -1);
    assert_eq("rollback_without_begin", db_execute(db, "ROLLBACK"), -1);

    /* Committed transaction sticks. */
    assert_eq("begin2", db_execute(db, "BEGIN"), 0);
    assert_eq("txn_delete", db_execute(db, "DELETE 2"), 0);
    assert_eq("commit", db_execute(db, "COMMIT"), 0);
    assert_eq("count_after_commit", db->data->count, 2);
    assert_eq("delete_again_fails", db_execute(db, "DELETE 2"), -1);

    /* SAVE / LOAD round trip; LOAD must rebuild the index. */
    assert_eq("save_db", db_execute(db, "SAVE /tmp/final.db"), 0);

    Database *db2 = db_new();
    assert_eq("load_db", db_execute(db2, "LOAD /tmp/final.db"), 0);
    assert_eq("loaded_count", db2->data->count, 2);
    assert_eq("loaded_next_id", db2->data->next_id, 4);
    assert_eq("loaded_index_works", db_execute(db2, "UPDATE 3 Charles 37"), 0);
    assert_eq("loaded_select", db_execute(db2, "SELECT age >= 30"), 0);
    if (db2->result) assert_eq("loaded_select_count", db2->result->count, 2);

    /* A failed LOAD leaves the database untouched. */
    assert_eq("load_missing", db_execute(db2, "LOAD /tmp/does-not-exist.db"), -1);
    assert_eq("load_missing_kept_data", db2->data->count, 2);

    /* Garbage in, -1 out. */
    assert_eq("unknown_verb", db_execute(db, "EXPLODE"), -1);
    assert_eq("malformed_insert", db_execute(db, "INSERT OnlyAName"), -1);
    assert_eq("malformed_select", db_execute(db, "SELECT age"), -1);
    assert_eq("empty_cmd", db_execute(db, ""), -1);

    db_free(db);
    db_free(db2);

    return failed > 0 ? 1 : 0;
}
```

---

You've built the full stack of a real database: dynamic row storage, a
self-describing file format, *durable* atomic saves (the same
temp-file-fsync-rename discipline SQLite's journal generalizes), predicate
queries, index maintenance, snapshot transactions, thread-safe access, and a
command interpreter tying it together. Every concept here — amortized growth,
page-oriented thinking, crash windows, undo information, lock granularity —
scales directly up to SQLite, Postgres, and beyond. The difference is
engineering depth, not kind.
