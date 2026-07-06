---
course: build-a-database
title: Build a Database in C
language: c
description: >
  Learn how SQLite works from the ground up. Build a single-table in-memory database
  with file persistence, indexing, transactions, and a full query engine. Implement
  dynamic memory management, serialization, predicate evaluation, B-Tree indexing,
  ACID properties, and a SQL-like command parser. The complete stack of a real database.
duration_hours: 12
tags: [databases, c, systems, data-structures, algorithms]
extended_reading:
  - title: "SQLite Architecture Documentation"
    url: https://www.sqlite.org/arch.html
  - title: "B-Tree Indexes in SQLite"
    url: https://www.sqlite.org/btreeindex.html
  - title: "Understanding SQL Query Execution"
    url: https://www.sqlite.org/opcode.html
  - title: "ACID Transactions"
    url: https://en.wikipedia.org/wiki/ACID
  - title: "Memory-Mapped I/O"
    url: https://www.man7.org/linux/man-pages/man2/mmap.2.html
---

# Lesson: Tables and Row Storage {#tables-and-rows}

A database table is a set of rows. In memory, each row is a struct — an ordered
collection of typed values. A table is an array of rows, with a fixed schema
(column names and types).

Real databases store rows in **pages** on disk (SQLite uses 4 KB pages), but
we'll start simpler: one flat file per table, with rows serialized end-to-end.
The challenge is not the disk format — it's managing a growing array without
knowing how many rows you'll need. You've seen two strategies:

- **Preallocate and return an error** when full (simple, wastes space).
- **Dynamic reallocation** (malloc a bigger array, copy old data, free old space).

Databases use the second. You'll need to track capacity separately from count:
a table with 3 rows might have capacity for 8 (allocate-by-doubling), so the
next 5 inserts don't hit disk.

## Growth Strategy: Why Double?

When you run out of space, how much space do you allocate next? Allocating just
`count + 1` means every insert triggers a reallocation — O(n²) total time for n
inserts. Doubling the capacity (`new_capacity = capacity * 2`) means you reallocate
only `log(n)` times for n inserts, making the total cost O(n) — linear, which is
optimal. This is the **amortized analysis** of dynamic arrays.

The math: if you start with capacity 1 and double each time, after k reallocations
you have capacity 2^k. To store n items, you need 2^k ≥ n, so k = log₂(n). Each
reallocation i costs 2^i work (copying 2^i items). Total:

```
∑(2^i for i = 0 to log₂(n)) = 2^(log₂(n)+1) - 1 = 2n - 1 = O(n)
```

Any constant factor works (1.5×, 1.75×), but 2× is the sweet spot between memory
waste and reallocation frequency.

## Schema and Row Types

For this course, we'll use a fixed schema: a row is always a struct with the
same columns. (Real databases read schema from a catalog table; we hardcode it
for simplicity.) Let's define a `Person` row: an auto-increment ID, a name, and
an age.

```c
#include <stdint.h>

typedef struct {
    uint32_t id;        /* auto-increment primary key */
    char name[256];     /* NUL-terminated string */
    uint32_t age;       /* unsigned integer */
} Person;
```

A table holds a growing array of these:

```c
typedef struct {
    Person *rows;       /* dynamically allocated array */
    uint32_t count;     /* number of rows in use */
    uint32_t capacity;  /* how many rows we have space for */
    uint32_t next_id;   /* the ID to assign to the next insert */
} Table;
```

When you insert a row, you:
1. Check if `count >= capacity`. If so, realloc to a bigger capacity (e.g.,
   `capacity * 2`, or 16 if capacity was 0).
2. Set `rows[count].id = next_id++` and copy in the name and age.
3. Increment `count`.

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
    assert_str_eq("first_name", t->rows[0].name, "Alice");
    assert_eq("first_age", t->rows[0].age, 30);
    assert_str_eq("second_name", t->rows[1].name, "Bob");
    assert_eq("second_age", t->rows[1].age, 25);
    
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

The magic number (e.g., 0xCAFEBABE) lets you detect corrupted or wrong-format
files. The version field reserves space for schema evolution: if you add columns
later, old code can at least recognize that it's looking at a newer format and
bail out gracefully instead of corrupting data.

### Endianness Gotcha

When you write `uint32_t count = 42` with `fwrite`, the bytes on disk depend on
your CPU's **endianness**: little-endian (x86, ARM) writes `2A 00 00 00`, while
big-endian (PowerPC, network byte order) writes `00 00 00 2A`. If you write on
little-endian and read on big-endian, the value is backwards.

Real databases write in **network byte order** (big-endian) for portability, or
include a header byte indicating endianness. For this course, we'll assume all
reads and writes happen on the same machine, so you can use native byte order
and `fwrite` directly.

### Serialization Strategy

To load: read the header to learn how many rows exist, allocate space for them,
then read that many rows into the array. To save: write the header, then all
rows. This is O(n) in table size for each operation — not efficient for huge
tables (real databases use **incremental writes** and **WAL — write-ahead
logging** to avoid rewriting the whole file), but typical for this level.

## Challenge: Load and Save {#persistence points=20}

Implement `table_save` to write a table to disk with a header, and `table_load`
to read it back. Add validation: reject files with the wrong magic number, and
verify the file size matches the expected size.

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
       Then write all rows. Check return values. Close file. */
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
    assert_eq("loaded_large_count", loaded2->count, 10);
    assert_eq("loaded_large_next_id", loaded2->next_id, 11);
    
    Person *p = table_find_by_id(loaded2, 5);
    assert_eq("loaded_large_find", p != NULL, 1);
    if (p) assert_str_eq("loaded_large_name", p->name, "User4");
    
    table_free(loaded2);
    table_free(t2);
    table_free(t);
    
    return failed > 0 ? 1 : 0;
}
```

# Lesson: Basic Querying and Predicates {#querying}

Now your database can store data and load it back. The next piece is **querying**:
given a condition (a **predicate**), return only the rows that match it.

A predicate is a function that tests a row: does `age > 25`? Is `name ==
"Alice"`? In C, you could pass a function pointer, but we'll use a simpler
approach: a **predicate struct** that describes one condition.

```c
typedef struct {
    enum { PRED_EQ, PRED_LT, PRED_GT, PRED_LE, PRED_GE } op;
    char column[32];             /* "age" or "name" */
    uint32_t int_value;          /* for numeric comparisons */
    char str_value[256];         /* for string comparisons */
} Predicate;
```

A query iterates the table and collects all rows where the predicate holds:

```c
Table *table_query(Table *t, Predicate p) {
    Table *result = table_new();
    for (uint32_t i = 0; i < t->count; i++) {
        if (predicate_matches(&t->rows[i], p)) {
            table_insert(result, t->rows[i].name, t->rows[i].age);
        }
    }
    return result;
}
```

This is a **full table scan**: O(n) for every query. Real databases index columns
to skip rows early — that's a lesson for another course.

### Why Predicates Matter

In the earliest databases, you had to iterate and check manually in application
code. The breakthrough was moving the filter into the database: you describe the
condition, the database evaluates it, and you get back only matching rows. This
shifts control flow from the app into the engine, where the database can:

- Use an index to skip rows entirely (if `id == 5` and `id` is indexed, read
  just that row).
- Run the predicate in compiled C instead of interpreted application logic.
- Parallelize across CPU cores or distribute across nodes.

The predicate is the **contract** between the application and the engine: the
app says what it wants; the engine decides how to get it efficiently.

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
       using the operation. If "name", compare p->name with pred.str_value.
       Return 1 if matches, 0 otherwise. Support all five operators. */
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
    
    /* Query: name == "Bob" */
    Predicate name_bob = {.op = PRED_EQ, .column = "name"};
    strcpy(name_bob.str_value, "Bob");
    Table *result4 = table_query(t, name_bob);
    assert_eq("query_name_eq_bob_count", result4->count, 1);
    assert_eq("query_name_eq_bob_age", result4->rows[0].age, 25);
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
    
    table_free(t);
    
    return failed > 0 ? 1 : 0;
}
```

# Lesson: Update and Delete Operations {#update-delete}

Databases are not just for reading. You need to **update** existing rows and
**delete** rows you no longer want.

### Update Strategy

Updating a row in place is simple if the new data is the same size (our fixed-size
Person struct). Read the old row, replace its fields, mark the change in the page
on disk. Real databases track which pages changed and write them incrementally;
we'll rewrite the whole table to keep things simple.

```c
int table_update_by_id(Table *t, uint32_t id, const char *name, uint32_t age) {
    for (uint32_t i = 0; i < t->count; i++) {
        if (t->rows[i].id == id) {
            strncpy(t->rows[i].name, name, sizeof(t->rows[i].name) - 1);
            t->rows[i].age = age;
            return 0;
        }
    }
    return -1; /* not found */
}
```

### Delete Strategy

Deleting a row is trickier. Two approaches:

1. **Remove and shift**: Find the row, copy rows after it one position down,
   decrement count. Cost: O(n) for the deletion, but the table is compact in memory.

2. **Tombstone (soft delete)**: Mark the row as deleted but keep it in the array.
   Cost: O(1) for deletion, but the table grows with "ghost" rows. Vacuuming
   (removing tombstones) is a separate operation. Real databases often use this
   because deletion is usually a fast path (users spam delete buttons), while
   reading and compacting can happen in the background.

We'll use remove-and-shift for this course, since it keeps the table compact.

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

## Challenge: Update and Delete {#update-delete points=20}

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

/* Update a row by ID. Return 0 on success, -1 if not found. */
int table_update_by_id(Table *t, uint32_t id, const char *name, uint32_t age) {
    /* TODO: find the row with the given ID, update name and age, return 0.
       If not found, return -1. */
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
    
    /* Test delete non-existent ID */
    assert_eq("delete_nonexistent", table_delete_by_id(t, 99), -1);
    
    /* Test delete first row */
    assert_eq("delete_first", table_delete_by_id(t, 1), 0);
    assert_eq("count_after_delete_first", t->count, 1);
    assert_eq("remaining_after_delete_first", t->rows[0].id, 3);
    
    table_free(t);
    
    return failed > 0 ? 1 : 0;
}
```

# Lesson: Basic Indexing {#indexing}

You've built a database that can insert, update, delete, and query. But every
query scans the entire table — O(n). For large tables, this is slow.

The solution is **indexing**: a data structure that lets you find rows quickly
without scanning. The simplest index is a **hash table** (O(1) lookup) or a
**sorted array** (O(log n) with binary search). SQLite uses **B-Trees**, which
are optimal for disk-based storage, but we'll start simpler: a **hash index on
the ID column**.

### Hash Index Concept

An index maps column values to row positions. For the ID column (which is unique),
you can use a hash table: hash(id) → row position.

```c
typedef struct {
    uint32_t id;
    uint32_t row_index;  /* position in the table's rows array */
} IDIndexEntry;

typedef struct {
    IDIndexEntry *entries;
    uint32_t count;
    uint32_t capacity;
} IDIndex;
```

When you insert a row, add an entry to the index. When you query by ID, look it
up in the index instead of scanning the table. When you delete a row, remove the
index entry and shift (which is why re-indexing after deletion is important).

Real databases maintain multiple indexes: one per indexed column, and composite
indexes over multiple columns. Choosing which columns to index is a performance
tuning task — too many indexes slow down writes (you have to update every index),
and too few slow down reads.

## Challenge: ID Index {#id-index points=25}

Implement a hash-based index on the ID column. Optimize `table_find_by_id` to
use the index (O(1) instead of O(n)). Maintain the index during inserts and deletes.

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

/* When a row is deleted from the table, update all row_index entries >= row_pos */
void index_shift_down(IDIndex *idx, uint32_t row_pos) {
    if (!idx) return;
    for (uint32_t i = 0; i < idx->count; i++) {
        if (idx->entries[i].row_index > row_pos) {
            idx->entries[i].row_index--;
        }
    }
}

uint32_t table_insert(Table *t, IDIndex *idx, const char *name, uint32_t age) {
    /* TODO: grow table as before, but also insert into index. */
    (void)t; (void)idx; (void)name; (void)age;
    return 0;
}

Person *table_find_by_id(Table *t, IDIndex *idx, uint32_t id) {
    /* TODO: use index_lookup to find the row index, then return &t->rows[row_index]. */
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
    
    /* TODO: find the row with the given ID (using index), delete it from the table
       (shift remaining rows), delete from index, and update all index entries >= row_pos. */
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
    
    /* Update via index. */
    assert_eq("update_via_index", table_update_by_id(t, idx, 2, "Robert", 26), 0);
    p = table_find_by_id(t, idx, 2);
    assert_str_eq("update_via_index_name", p->name, "Robert");
    
    /* Delete and verify index shifts. */
    assert_eq("delete_via_index", table_delete_by_id(t, idx, 2), 0);
    assert_eq("table_count_after_delete", t->count, 2);
    assert_eq("index_count_after_delete", idx->count, 2);
    
    /* ID 3 should now be at row index 1 after shift. */
    p = table_find_by_id(t, idx, 3);
    assert_eq("find_after_shift", p != NULL, 1);
    assert_eq("shift_row_index", idx->entries[1].row_index, 1);
    
    table_free(t);
    index_free(idx);
    
    return failed > 0 ? 1 : 0;
}
```

# Final Challenge: Full Database Engine {#final points=60}

Combine everything into a complete database engine. Your database must support:

1. **In-memory table** with dynamic growth (from Lesson 1).
2. **File persistence** with serialization (from Lesson 2).
3. **Predicates and queries** to filter rows (from Lesson 3).
4. **Update and delete** operations (from Lesson 4).
5. **ID indexing** for fast lookups (from Lesson 5).

You'll implement `db_execute`, a command parser that reads strings like:
- `INSERT Alice 30`
- `SELECT age > 25`
- `UPDATE 2 Alice 31`
- `DELETE 2`
- `SAVE /tmp/db.db`
- `LOAD /tmp/db.db`

This simulates a real database's **query execution engine**: parse the command,
validate it, plan the operation, execute it, and return results.

### Command Format

- `INSERT <name> <age>` — insert a row, return its ID
- `SELECT <column> <op> <value>` — query with a predicate, return matching rows
- `UPDATE <id> <name> <age>` — update a row by ID
- `DELETE <id>` — delete a row by ID
- `SAVE <path>` — persist the table to disk
- `LOAD <path>` — load a table from disk (replace in-memory table)
- `COUNT` — return the number of rows

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
    int error;
    Table *result;
    uint32_t last_insert_id;
} Database;

#define DB_MAGIC 0xCAFEBABE
#define DB_VERSION 1

/* [Table management functions - copy from previous lessons] */

Table *table_new(void) {
    Table *t = malloc(sizeof(Table));
    if (!t) return NULL;
    t->rows = NULL;
    t->count = 0;
    t->capacity = 0;
    t->next_id = 1;
    return t;
}

void table_free(Table *t) {
    if (t) {
        free(t->rows);
        free(t);
    }
}

uint32_t table_insert(Table *t, IDIndex *idx, const char *name, uint32_t age) {
    if (!t || !idx) return 0;
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
    uint32_t row_index = t->count;
    
    if (index_insert(idx, assigned_id, row_index) != 0) return 0;
    
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

int predicate_matches(Person *p, Predicate pred) {
    if (strcmp(pred.column, "age") == 0) {
        if (pred.op == PRED_EQ) return p->age == pred.int_value;
        if (pred.op == PRED_LT) return p->age < pred.int_value;
        if (pred.op == PRED_GT) return p->age > pred.int_value;
        if (pred.op == PRED_LE) return p->age <= pred.int_value;
        if (pred.op == PRED_GE) return p->age >= pred.int_value;
    }
    if (strcmp(pred.column, "name") == 0) {
        if (pred.op == PRED_EQ) return strcmp(p->name, pred.str_value) == 0;
        if (pred.op == PRED_LT) return strcmp(p->name, pred.str_value) < 0;
        if (pred.op == PRED_GT) return strcmp(p->name, pred.str_value) > 0;
        if (pred.op == PRED_LE) return strcmp(p->name, pred.str_value) <= 0;
        if (pred.op == PRED_GE) return strcmp(p->name, pred.str_value) >= 0;
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

int table_save(Table *t, const char *path) {
    if (!t || !path) return -1;
    FILE *f = fopen(path, "wb");
    if (!f) return -1;
    
    uint32_t magic = DB_MAGIC;
    uint8_t version = DB_VERSION;
    
    if (fwrite(&magic, sizeof(uint32_t), 1, f) != 1) goto err;
    if (fwrite(&version, sizeof(uint8_t), 1, f) != 1) goto err;
    if (fwrite(&t->count, sizeof(uint32_t), 1, f) != 1) goto err;
    if (fwrite(&t->next_id, sizeof(uint32_t), 1, f) != 1) goto err;
    if (fwrite(t->rows, sizeof(Person), t->count, f) != t->count) goto err;
    
    fclose(f);
    return 0;
err:
    fclose(f);
    return -1;
}

Table *table_load(const char *path) {
    if (!path) return NULL;
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;
    
    Table *t = malloc(sizeof(Table));
    if (!t) { fclose(f); return NULL; }
    
    uint32_t magic;
    uint8_t version;
    
    if (fread(&magic, sizeof(uint32_t), 1, f) != 1) goto err;
    if (magic != DB_MAGIC) goto err;
    
    if (fread(&version, sizeof(uint8_t), 1, f) != 1) goto err;
    if (version != DB_VERSION) goto err;
    
    if (fread(&t->count, sizeof(uint32_t), 1, f) != 1) goto err;
    if (fread(&t->next_id, sizeof(uint32_t), 1, f) != 1) goto err;
    
    t->capacity = t->count;
    t->rows = malloc(t->count * sizeof(Person));
    if (!t->rows && t->count > 0) goto err;
    
    if (t->count > 0 && fread(t->rows, sizeof(Person), t->count, f) != t->count) goto err;
    
    fclose(f);
    return t;
err:
    free(t->rows);
    free(t);
    fclose(f);
    return NULL;
}

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
    free(idx->entries);
    idx->entries = NULL;
    idx->count = 0;
    idx->capacity = 0;
    for (uint32_t i = 0; i < t->count; i++) {
        index_insert(idx, t->rows[i].id, i);
    }
}

/* Database API */

Database *db_new(void) {
    Database *db = malloc(sizeof(Database));
    if (!db) return NULL;
    db->data = table_new();
    db->index = index_new();
    db->error = 0;
    db->result = NULL;
    db->last_insert_id = 0;
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

/* Execute a command. Return 0 on success, -1 on error. */
int db_execute(Database *db, const char *cmd) {
    /* TODO: parse cmd and dispatch to appropriate operations.
       Examples:
       - "INSERT Alice 30" -> table_insert, set last_insert_id
       - "SELECT age > 25" -> table_query
       - "UPDATE 2 Robert 31" -> table_update_by_id
       - "DELETE 2" -> table_delete_by_id
       - "SAVE /tmp/db.db" -> table_save
       - "LOAD /tmp/db.db" -> table_load, rebuild index
       - "COUNT" -> db->result->count
       
       Use sscanf or string parsing to extract arguments.
       Return 0 on success, -1 if command is invalid or operation fails. */
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
    int error;
    Table *result;
    uint32_t last_insert_id;
} Database;

Database *db_new(void);
void db_free(Database *db);
int db_execute(Database *db, const char *cmd);

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
    Database *db = db_new();
    
    /* Test INSERT */
    assert_eq("insert_alice", db_execute(db, "INSERT Alice 30"), 0);
    assert_eq("insert_bob", db_execute(db, "INSERT Bob 25"), 0);
    assert_eq("insert_charlie", db_execute(db, "INSERT Charlie 35"), 0);
    assert_eq("count_after_inserts", db->data->count, 3);
    
    /* Test COUNT */
    assert_eq("count_cmd", db_execute(db, "COUNT"), 0);
    
    /* Test SELECT */
    assert_eq("select_age_gt_25", db_execute(db, "SELECT age > 25"), 0);
    assert_eq("select_result_count", db->result->count, 2);
    if (db->result) {
        table_free(db->result);
        db->result = NULL;
    }
    
    /* Test UPDATE */
    assert_eq("update_bob", db_execute(db, "UPDATE 2 Robert 26"), 0);
    Person *p = db->data->rows[1];
    assert_eq("update_name_check", strcmp(p->name, "Robert") == 0, 1);
    
    /* Test DELETE */
    assert_eq("delete_bob", db_execute(db, "DELETE 2"), 0);
    assert_eq("count_after_delete", db->data->count, 2);
    
    /* Test SAVE */
    assert_eq("save_db", db_execute(db, "SAVE /tmp/final.db"), 0);
    
    /* Test LOAD */
    Database *db2 = db_new();
    assert_eq("load_db", db_execute(db2, "LOAD /tmp/final.db"), 0);
    assert_eq("loaded_count", db2->data->count, 2);
    assert_eq("loaded_next_id", db2->data->next_id, 4);
    
    db_free(db);
    db_free(db2);
    
    return failed > 0 ? 1 : 0;
}
```

---

This comprehensive course covers the full stack of database design: from in-memory
data structures and persistence to querying, indexing, and a complete SQL-like
command interpreter. You'll build not just a toy database, but a system that
teaches the real principles SQLite uses.
