---
course: build-a-hashmap
title: Build a Hash Map in C
language: c
description: >
  C gives you no dictionary — so build one. Starting from a hash function
  and ending with a self-resizing map, learn how string keys become array
  indexes, how separate chaining absorbs collisions, why deletion wants a
  pointer-to-pointer, and what a load factor actually buys you.
duration_hours: 4
tags: [data-structures, c, memory]
extended_reading:
  - title: FNV hash — the original description
    url: http://www.isthe.com/chongo/tech/comp/fnv/
  - title: "The Lost Art of Structure Packing (memory layout background)"
    url: http://www.catb.org/esr/structure-packing/
---

# Lesson: Hash Functions {#hash-functions}

A hash map's trick is turning a string key into an array index in O(1). The
first half of that is the **hash function**: boil the bytes of a key down
to one 32-bit number. Three properties make one useful:

- **Deterministic.** Same key, same hash, every run — the map stores an
  entry by its hash and must find it by the same hash later.
- **Fast.** It runs on every single put and get.
- **Scrambled.** Similar keys must get wildly different hashes. Real keys
  arrive in families — `key-1`, `key-2`, `user_name`, `user_email` — and if
  similar keys hashed to similar numbers, whole families would pile into
  neighboring buckets and the O(1) promise would quietly die.

That last property is called **avalanche**: changing one input bit should
flip about half the output bits. The function you'll build has it — watch
what one letter does:

```
fnv1a("cat") = 0x06745c07 = 00000110011101000101110000000111
fnv1a("cab") = 0xf4743fb1 = 11110100011101000011111110110001
                            ^^^^^^^^ 14 of 32 bits flipped ^^
```

## The inject-and-smear loop

Nearly every fast string hash is the same two-beat loop: keep a running
32-bit state, and for each input byte, **inject** the byte into the state,
then **smear** the state so the byte's influence spreads. **FNV-1a**
(Fowler–Noll–Vo) is the smallest honest version of that pattern:

```
hash = 2166136261                # the "offset basis": the starting state
for each byte b of the key:
    hash = hash XOR b            # inject: flips at most the low 8 bits
    hash = hash * 16777619       # smear: spreads them across all 32 bits
                                 #        (wrapping modulo 2^32)
```

XOR alone would be a terrible hash — it only ever touches the low 8 bits,
and XOR is order-blind ("ab" and "ba" would collide). The multiply is what
turns it into a hash, and it's worth seeing why.

## Why multiplying smears

Multiplication is shifted addition. The FNV multiplier is
`16777619 = 0x01000193 = 2^24 + 2^8 + 147`, so one smear step is really:

```
hash * 16777619 = (hash << 24) + (hash << 8) + hash * 147
```

— three shifted copies of the entire state added together, with carries
rippling between them. A byte injected into the low bits gets copied 8 and
24 positions up *every round*, and the carries knock bits around in
between. Two rounds of this and there is no clean story left about where
any input bit "is" — which is exactly the point. Here is the full state
after every step of hashing `"hi"`:

```
start                0x811c9dc5
xor 'h' (0x68)       0x811c9dad     low bits poked
multiply             0xed0c3757     whole word churned
xor 'i' (0x69)       0xed0c373e     low bits poked
multiply             0x683af69a     whole word churned again
```

## Why these constants and not others

Two properties are load-bearing; the exact digits are not.

**The multiplier must be odd.** Modulo 2^32, multiplying by an odd number
is *invertible* — it's a lossless scramble: two different states can never
multiply into the same state, so the loop never destroys information. An
even multiplier is a leftward shift in disguise: every round, top bits fall
off the 32-bit cliff and zeros pile up in the bottom — and the bottom bits
are precisely what `hash % nbuckets` will read. This is measurable. Hash
100 keys (`key-0`…`key-99`) into 8 buckets:

```
multiplier 16777619 (odd, the real one):  all 8 buckets used
multiplier 16777620 (even):               only buckets 0 and 4 ever used
multiplier 256      (a pure shift):       every key lands in bucket 0
```

**The starting state must be non-zero and bit-rich.** Start at 0 and the
first XOR has nothing to scramble against: a one-byte key hashes to exactly
`byte * 16777619` — a multiplication table, not a hash ("a", "b", "c" come
out exactly 16777619 apart). A dense, randomish basis means even the first
byte of every key gets smeared against 32 bits of noise.

Within those constraints, which odd multiplier? Which starting value? Those
were **measured, not divined**: Fowler, Noll, and Vo tested candidate
primes of this bit-shape against real-world key sets and kept the one that
dispersed best (a prime also avoids sharing factors with byte patterns or
bucket counts). The basis `2166136261` is itself an FNV hash — of a
signature string of Noll's — chosen once and frozen in the spec so that
every FNV-1a implementation on earth produces identical hashes. "Magic
number" in hashing means *standardized and empirically vetted*, not
mystical. That interoperability is also what lets this challenge's tests
check your code against published FNV-1a test vectors instead of accepting
any old scramble.

Two C details make or break the implementation:

- **Unsigned arithmetic.** The running hash must be `uint32_t`: unsigned
  overflow wraps around by definition — that's the "modulo 2^32" for free.
  Signed overflow would be undefined behavior.
- **Byte values, not char values.** `char` may be signed, so a byte like
  0xE9 (é in Latin-1) could XOR in as a *negative* value and corrupt the
  hash. Read bytes through `unsigned char`.

## Challenge: FNV-1a {#fnv1a points=10}

Implement 32-bit FNV-1a over a NUL-terminated string (the NUL itself is not
hashed).

### Starter

```c
#include <stdint.h>

#define FNV_OFFSET_BASIS 2166136261u
#define FNV_PRIME        16777619u

/* 32-bit FNV-1a of the bytes of s (not including the trailing NUL). */
uint32_t fnv1a(const char *s) {
	/* TODO: start from the offset basis; for each byte, XOR then multiply */
	(void)s;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

uint32_t fnv1a(const char *s);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void) {
	/* Published FNV-1a 32-bit test vectors. */
	check(fnv1a("") == 0x811c9dc5u, "test_empty_string_is_offset_basis");
	check(fnv1a("a") == 0xe40c292cu, "test_single_byte");
	check(fnv1a("hello") == 0x4f9f2cabu, "test_hello");
	check(fnv1a("pico") == 0xfe41f444u, "test_pico");

	check(fnv1a("flamingo") == fnv1a("flamingo"), "test_deterministic");
	check(fnv1a("cat") != fnv1a("cab"), "test_similar_keys_differ");

	/* High-bit bytes must be hashed as unsigned values. */
	check(fnv1a("\xe9") == ((2166136261u ^ 0xe9u) * 16777619u),
	      "test_high_bit_byte_unsigned");

	return failed;
}
```

# Lesson: Buckets and Collisions {#buckets-and-collisions}

A 32-bit hash is too big to be an array index, so the map keeps `nbuckets`
slots and folds the hash down with modulo:

```c
size_t index = hash % nbuckets;
```

Different keys will inevitably share a bucket — with 4 billion possible
hashes squeezed into a handful of buckets, collisions are a certainty to
design for, not an error. The simplest fix is **separate chaining**: each
bucket holds the head of a singly linked list of entries, and colliding
entries just line up behind each other.

```c
struct hm_entry {
	char *key;
	int value;
	struct hm_entry *next;   /* next entry in the same bucket */
};
```

Finding a key inside a bucket is a plain list walk. One subtlety: keys are
strings, so the comparison is `strcmp(...) == 0` — comparing `char *`
pointers with `==` asks "is this the same memory?", not "is this the same
text?", and will appear to work in toy tests only to fail in production.

## Challenge: Walk a Chain {#chain-find points=10}

Implement the two small helpers a chained map is built from: fold a hash
into a bucket index, and search one bucket's chain for a key.

### Starter

```c
#include <stddef.h>
#include <stdint.h>
#include <string.h>

struct hm_entry {
	char *key;
	int value;
	struct hm_entry *next;
};

/* The bucket a hash falls into. nbuckets is always >= 1. */
size_t bucket_index(uint32_t hash, size_t nbuckets) {
	/* TODO */
	(void)hash;
	(void)nbuckets;
	return 0;
}

/* The entry with the given key, or NULL if the chain doesn't have it. */
struct hm_entry *chain_find(struct hm_entry *head, const char *key) {
	/* TODO: walk the list, compare with strcmp */
	(void)head;
	(void)key;
	return NULL;
}
```

### Tests

```c
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

struct hm_entry {
	char *key;
	int value;
	struct hm_entry *next;
};

size_t bucket_index(uint32_t hash, size_t nbuckets);
struct hm_entry *chain_find(struct hm_entry *head, const char *key);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void) {
	check(bucket_index(0, 8) == 0, "test_index_zero");
	check(bucket_index(13, 8) == 5, "test_index_wraps");
	check(bucket_index(0xffffffffu, 1) == 0, "test_index_single_bucket");

	/* A three-entry chain built by hand on the stack. */
	char k1[] = "alpha", k2[] = "beta", k3[] = "gamma";
	struct hm_entry e3 = {k3, 3, NULL};
	struct hm_entry e2 = {k2, 2, &e3};
	struct hm_entry e1 = {k1, 1, &e2};

	check(chain_find(&e1, "alpha") == &e1, "test_find_head");
	check(chain_find(&e1, "gamma") == &e3, "test_find_tail");
	check(chain_find(&e1, "delta") == NULL, "test_find_missing");
	check(chain_find(NULL, "alpha") == NULL, "test_find_empty_chain");

	/* Same text, different memory: must match by content, not pointer. */
	char other[] = "beta";
	check(chain_find(&e1, other) == &e2, "test_find_compares_content");

	return failed;
}
```

# Lesson: Put and Get {#put-and-get}

Time to assemble the map itself: a heap-allocated array of bucket heads
plus a length counter.

```c
struct hashmap {
	struct hm_entry **buckets;  /* array of nbuckets chain heads */
	size_t nbuckets;
	size_t len;                 /* number of stored entries */
};
```

`hm_get` is the two helpers from last lesson glued together: hash, index,
walk the chain. `hm_put` has more decisions in it:

- **Overwrite, don't duplicate.** If the key already exists, replace its
  value in place; `len` doesn't change. Only a genuinely new key allocates
  a node and bumps `len`.
- **Own your keys.** The caller's string may be a stack buffer that dies at
  the next `}`. The map must copy the key into memory it owns (allocate
  `strlen + 1` bytes and copy — this is what non-standard `strdup` does).
- **Report allocation failure.** `malloc` returns NULL when memory runs
  out; a library that crashes instead of returning an error is a library
  you can't ship. Return 0 on success, -1 on failure.

Returning `int *` (a pointer to the stored value) from `hm_get` instead of
`int` kills two birds: NULL cleanly signals "not found", and callers can
update a value through the pointer without a second lookup.

## Challenge: The Core API {#hm-put-get points=15}

Implement `hm_put` and `hm_get`. The starter gives you working `fnv1a` and
`hm_new` so the earlier building blocks don't need re-typing; new entries
may go at the head of their chain (it's the easy spot).

### Starter

```c
#include <stdint.h>
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
	size_t len;
};

uint32_t fnv1a(const char *s) {
	uint32_t h = 2166136261u;
	for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
		h ^= *p;
		h *= 16777619u;
	}
	return h;
}

/* A fresh map with nbuckets empty buckets, or NULL on allocation failure. */
struct hashmap *hm_new(size_t nbuckets) {
	struct hashmap *m = malloc(sizeof *m);
	if (!m)
		return NULL;
	m->buckets = calloc(nbuckets, sizeof *m->buckets);
	if (!m->buckets) {
		free(m);
		return NULL;
	}
	m->nbuckets = nbuckets;
	m->len = 0;
	return m;
}

/* Insert key=value, or overwrite the value if key is already present.
   The map stores its own copy of key. 0 on success, -1 if out of memory. */
int hm_put(struct hashmap *m, const char *key, int value) {
	/* TODO */
	(void)m;
	(void)key;
	(void)value;
	return -1;
}

/* Pointer to the value stored under key, or NULL if absent. */
int *hm_get(struct hashmap *m, const char *key) {
	/* TODO */
	(void)m;
	(void)key;
	return NULL;
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
	size_t len;
};

struct hashmap *hm_new(size_t nbuckets);
int hm_put(struct hashmap *m, const char *key, int value);
int *hm_get(struct hashmap *m, const char *key);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void) {
	/* 2 buckets + 8 keys forces every chain to hold collisions. */
	struct hashmap *m = hm_new(2);

	const char *keys[] = {"ada", "grace", "edsger", "alan",
	                      "barbara", "donald", "ken", "dennis"};
	int ok = 1;
	for (int i = 0; i < 8; i++)
		ok &= hm_put(m, keys[i], i) == 0;
	check(ok, "test_put_reports_success");
	check(m->len == 8, "test_len_counts_entries");

	ok = 1;
	for (int i = 0; i < 8; i++) {
		int *v = hm_get(m, keys[i]);
		ok &= v != NULL && *v == i;
	}
	check(ok, "test_get_finds_all_despite_collisions");

	check(hm_get(m, "linus") == NULL, "test_get_missing_is_null");

	hm_put(m, "ada", 99);
	int *v = hm_get(m, "ada");
	check(v != NULL && *v == 99, "test_put_overwrites");
	check(m->len == 8, "test_overwrite_does_not_grow_len");

	/* The map must copy keys: mutate the caller's buffer after put. */
	char temp[8];
	strcpy(temp, "turing");
	hm_put(m, temp, 42);
	strcpy(temp, "XXXXXX");
	v = hm_get(m, "turing");
	check(v != NULL && *v == 42, "test_map_owns_key_copies");

	return failed;
}
```

# Lesson: Deleting Entries {#deleting-entries}

Removing from a singly linked chain has a classic wrinkle: unlinking a node
means updating *whatever pointed to it* — the bucket head for the first
node, the previous node's `next` for the others. Handling those as two
cases works but doubles the code and the bugs.

The idiomatic C fix is a **pointer to a pointer**. Instead of walking
entries, walk the *links*:

```c
struct hm_entry **pp = &m->buckets[index];
while (*pp && strcmp((*pp)->key, key) != 0)
	pp = &(*pp)->next;
```

Now `pp` points at either the bucket head or someone's `next` field — it
doesn't matter which. Unlinking is uniform: save `*pp`, redirect `*pp` to
the doomed node's `next`, free the node. One code path, no special cases.

And because the map allocated the key copy and the node in `hm_put`, the
map must free them here — every `malloc` needs exactly one `free`, and the
grader's tests will exercise remove-then-lookup to make sure the entry is
truly gone, not just leaked.

## Challenge: Remove {#hm-remove points=10}

Implement `hm_remove` with the pointer-to-pointer walk. Free both the key
copy and the node; return 1 if a key was removed, 0 if it wasn't there.

### Starter

```c
#include <stdint.h>
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
	size_t len;
};

uint32_t fnv1a(const char *s) {
	uint32_t h = 2166136261u;
	for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
		h ^= *p;
		h *= 16777619u;
	}
	return h;
}

/* Remove key from the map, freeing the entry and its key copy.
   1 if something was removed, 0 if the key wasn't present. */
int hm_remove(struct hashmap *m, const char *key) {
	/* TODO: walk links with a struct hm_entry **, unlink, free */
	(void)m;
	(void)key;
	return 0;
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
	size_t len;
};

int hm_remove(struct hashmap *m, const char *key);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Build a one-bucket map by hand so the tests don't depend on hm_put. */
static struct hm_entry *mk(const char *key, int value, struct hm_entry *next) {
	struct hm_entry *e = malloc(sizeof *e);
	e->key = malloc(strlen(key) + 1);
	strcpy(e->key, key);
	e->value = value;
	e->next = next;
	return e;
}

int main(void) {
	struct hm_entry *head = mk("front", 1, mk("middle", 2, mk("back", 3, NULL)));
	struct hm_entry *bucket[1] = {head};
	struct hashmap m = {bucket, 1, 3};

	check(hm_remove(&m, "ghost") == 0, "test_remove_missing_returns_0");
	check(m.buckets[0] == head, "test_remove_missing_leaves_chain");

	check(hm_remove(&m, "middle") == 1, "test_remove_middle_returns_1");
	check(m.buckets[0] != NULL &&
	      m.buckets[0]->next != NULL &&
	      strcmp(m.buckets[0]->next->key, "back") == 0,
	      "test_remove_middle_relinks");

	check(hm_remove(&m, "front") == 1, "test_remove_head_returns_1");
	check(m.buckets[0] != NULL && strcmp(m.buckets[0]->key, "back") == 0,
	      "test_remove_head_updates_bucket");

	check(hm_remove(&m, "back") == 1, "test_remove_last");
	check(m.buckets[0] == NULL, "test_chain_empties_cleanly");
	check(hm_remove(&m, "back") == 0, "test_removed_key_is_gone");

	return failed;
}
```

# Final Challenge: A Growing Map {#final points=50}

A fixed bucket count betrays you at scale: chains grow with the data and
O(1) quietly rots into O(n). Real maps watch their **load factor** —
`len / nbuckets` — and when it crosses a threshold they allocate a bigger
bucket array and **rehash**: every entry's home bucket is `hash %
nbuckets`, so changing `nbuckets` moves entries; each one must be relinked
into its new bucket. Existing nodes and key copies can be reused — resizing
moves entries, it doesn't recreate them.

Assemble the complete map. `hm_new` is given; implement the rest:

- `hm_put` — as in lesson 3, but *before* inserting a new key, if
  `len >= nbuckets` double the bucket count. Overwrites never trigger a
  resize.
- `hm_get` — hash, index, walk.
- `hm_resize(m, nbuckets)` — allocate the new bucket array, relink every
  existing node by its new index, free the old array. 0 on success, -1 on
  allocation failure (in which case the map must be left untouched and
  usable).

The tests push 100 keys through a map that starts with 4 buckets, so a
missing or wrong rehash shows up immediately as lost keys.

### Starter

```c
#include <stdint.h>
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
	size_t len;
};

uint32_t fnv1a(const char *s) {
	uint32_t h = 2166136261u;
	for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
		h ^= *p;
		h *= 16777619u;
	}
	return h;
}

struct hashmap *hm_new(size_t nbuckets) {
	struct hashmap *m = malloc(sizeof *m);
	if (!m)
		return NULL;
	m->buckets = calloc(nbuckets, sizeof *m->buckets);
	if (!m->buckets) {
		free(m);
		return NULL;
	}
	m->nbuckets = nbuckets;
	m->len = 0;
	return m;
}

/* Move every entry into a fresh array of nbuckets buckets.
   0 on success; -1 on allocation failure, leaving the map unchanged. */
int hm_resize(struct hashmap *m, size_t nbuckets) {
	/* TODO */
	(void)m;
	(void)nbuckets;
	return -1;
}

/* Insert or overwrite. Doubles the bucket array first when a new key
   would push len to nbuckets or beyond. 0 on success, -1 out of memory. */
int hm_put(struct hashmap *m, const char *key, int value) {
	/* TODO */
	(void)m;
	(void)key;
	(void)value;
	return -1;
}

/* Pointer to the value under key, or NULL. */
int *hm_get(struct hashmap *m, const char *key) {
	/* TODO */
	(void)m;
	(void)key;
	return NULL;
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
	size_t len;
};

struct hashmap *hm_new(size_t nbuckets);
int hm_resize(struct hashmap *m, size_t nbuckets);
int hm_put(struct hashmap *m, const char *key, int value);
int *hm_get(struct hashmap *m, const char *key);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void) {
	struct hashmap *m = hm_new(4);

	/* 100 keys through 4 starting buckets: growth is not optional. */
	char key[16];
	int ok = 1;
	for (int i = 0; i < 100; i++) {
		sprintf(key, "key-%d", i);
		ok &= hm_put(m, key, i) == 0;
	}
	check(ok, "test_100_puts_succeed");
	check(m->len == 100, "test_len_is_100");
	check(m->nbuckets > 4, "test_map_grew");
	check(m->nbuckets >= m->len, "test_load_factor_maintained");

	ok = 1;
	for (int i = 0; i < 100; i++) {
		sprintf(key, "key-%d", i);
		int *v = hm_get(m, key);
		ok &= v != NULL && *v == i;
	}
	check(ok, "test_all_keys_survive_rehash");

	check(hm_get(m, "key-100") == NULL, "test_missing_still_null");

	hm_put(m, "key-50", -1);
	int *v = hm_get(m, "key-50");
	check(v != NULL && *v == -1, "test_overwrite_after_growth");
	check(m->len == 100, "test_overwrite_keeps_len");

	/* Explicit resize keeps everything reachable too. */
	size_t before = m->len;
	check(hm_resize(m, 512) == 0 && m->nbuckets == 512, "test_manual_resize");
	ok = m->len == before;
	for (int i = 0; i < 100; i++) {
		sprintf(key, "key-%d", i);
		ok &= hm_get(m, key) != NULL;
	}
	check(ok, "test_resize_moves_not_loses");

	return failed;
}
```
