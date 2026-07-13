# DuckOS Course — Shared Authoring Spec

You are writing ONE section of a large course document for the Rubber Duck
platform: "Build an Educational Operating System" in C. The course builds
**DuckOS**, a Minix-inspired educational operating system — Minix (Tanenbaum,
1987) being the teaching OS Linus Torvalds ran at the University of Helsinki
before writing Linux in 1991. The narrative voice: we are building the kind
of OS a systems course in 1991 would have handed you, with modern C and
modern explanations.

Your section will be concatenated with the others in order, so follow this
spec EXACTLY. Deviations break the ingest parser or the grader.

## Platform format rules (hard requirements)

- A lesson starts: `# Lesson: Title {#slug}` (H1). Everything until the next
  H1 belongs to it.
- A challenge starts: `## Challenge: Title {#slug points=N}` (H2), inside a
  lesson. Prompt text runs until `### Starter`.
- `### Starter` is followed (eventually) by ONE fenced ```c block = the file
  the learner edits (becomes `solution.c`). `### Tests` is followed by ONE
  fenced ```c block = `test_solution.c`. Both required, in that order.
- Inside lesson prose you may use `##` and `###` headings freely, BUT never
  begin one with `Challenge:`, `Starter`, or `Tests`.
- Do NOT write any frontmatter, and do NOT write a `# Final Challenge`
  heading (one section owner is assigned that; it's marked in the outline).
- Use exactly the slugs and points assigned to you in the outline below.

## How grading works (drives every code decision)

`cc -std=c17 -Wall -O1 -o test_bin solution.c test_solution.c && ./test_bin`
on Alpine Linux (musl, gcc). Exit code = failure count. Notes:

- The two files are separate translation units compiled together. The test
  file declares prototypes for the solution's functions and REPEATS any
  struct/enum/#define definitions it needs (they must match the starter's
  exactly).
- No `main` in the starter. `main` lives in the test file.
- stdlib-only, and NO `-lm` (avoid <math.h> functions needing libm). No
  glibc extensions; musl-compatible. No VLAs, no `alloca`. Deterministic:
  no `rand`, `time`, no reading files or env.
- Tests must print one line per test case in exactly this format:
  `--- PASS: test_name` / `--- FAIL: test_name`, run EVERY test (never
  abort on first failure), and `return failed;` from main. Use this harness
  verbatim in every Tests block:

  ```
  static int failed;

  static void check(int ok, const char *name) {
  	if (ok) {
  		printf("--- PASS: %s\n", name);
  	} else {
  		printf("--- FAIL: %s\n", name);
  		failed++;
  	}
  }
  ```

- Test names: `test_snake_case`, descriptive (`test_coalesce_with_next`).
- The UNMODIFIED starter must: compile warning-free, link with the tests,
  run to completion WITHOUT crashing (no segfault/UB on the TODO stubs —
  tests must tolerate stub return values like 0/NULL/-1), print at least
  one `--- FAIL:` line, and exit non-zero.
- A correct reference solution must make every test print `--- PASS:` and
  exit 0.
- Starters: give real scaffolding — struct definitions, constants,
  documented function signatures, `/* TODO: ... */` bodies with `(void)x;`
  casts so `-Wall` stays clean and stub returns (0/NULL/-1) so tests can
  run. The learner should never need to invent an API, only implement one.

## C style

- Tabs for indentation in C code (match the platform's other C courses).
- C17, `<stdint.h>` fixed-width types for anything hardware-shaped
  (`uint8_t buf[512]`, `uint32_t` page table entries).
- Comments explain contracts and units, not restate code.
- Each challenge is SELF-CONTAINED: it must not require code from any other
  challenge. Re-declare whatever structs/constants it needs in its own
  starter (use the canonical DuckOS definitions below so the course reads
  as one system).

## Prose style (match the platform's best courses)

- Teach the WHY relentlessly: why the hardware works this way, what breaks
  without the mechanism, what the historical pressure was. Byte-level
  concrete examples (hex dumps, worked translations, drawn memory layouts
  in ``` fenced blocks) beat abstract description.
- Historical asides are welcome and on-theme (Minix, the Tanenbaum–Torvalds
  debate, 8259 PIC lore, why the A20 gate exists...). Keep them accurate.
- Every challenge prompt states the exact contract: inputs, outputs, edge
  cases, and what the tests plant/check.
- Wrap prose at ~76 columns like the existing courses.
- The lesson should stand on its own but MAY reference other lessons by
  title ("as we saw in *Paging and Virtual Memory*", "the final challenge
  wires this into..."). Use the outline below for accurate references.
- The OS is called **DuckOS**. The target ISA for concreteness is 32-bit
  x86 (i386) — the machine Minix and early Linux ran on — but all code is
  hostable simulation: we model registers/tables/devices as plain C data.
  Say this once per lesson where relevant: "in a real kernel this struct
  IS the hardware table; here we build it in a buffer the tests can read."

## Canonical DuckOS conventions (use these names/values when your section touches them)

- `#define NPROC 16` — static process table, Minix-style.
- Process states: `PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE` (an enum
  `proc_state`).
- `struct proc` core fields (sections may add fields they need):
  `int pid; enum proc_state state; int parent; /* slot index, -1 = none */`
- Messages (IPC): `struct message { int m_source; int m_type; int m_i1;
  int m_i2; };` — fixed-size, Minix-style. `#define ANY (-1)`.
- Scheduler: `#define NQ 4` priority queues, 0 = highest; quantum in ticks.
- `#define HZ 100` — clock ticks per second.
- `#define PAGE_SIZE 4096`.
- Disk: `#define BLOCK_SIZE 1024` (Minix v1). Filesystem numbers follow
  Minix FS v1: magic `0x137F`, 16-byte dirents (2-byte inode + 14-byte
  name, NOT NUL-terminated when full), 7 direct zones + 1 indirect +
  1 double-indirect per inode, zone/inode bitmaps where bit 0 is reserved
  (inode and zone numbering starts at 1).
- Little-endian byte order everywhere bytes are parsed (it's x86).
- Error returns: negative errno-style ints; define the few you need, e.g.
  `#define EAGAIN 11`, `#define EINVAL 22`, `#define ENOENT 2`,
  `#define ENOTDIR 20`, `#define EDEADLK 35`, `#define ESRCH 3`,
  `#define ECHILD 10`, `#define EFAULT 14`, `#define ENOSYS 38`.

## Course outline (for cross-references; write ONLY your assigned section)

Points and slugs are fixed. "Ch" lines are challenges.

- S01 `# Lesson: The Machine Wakes Up {#the-machine-wakes-up}` — history
  (Minix→Linux), BIOS, real mode, the boot sector.
  Ch `{#real-mode-address points=10}` — segment:offset → linear address.
  Ch `{#boot-sector points=15}` — parse/validate a 512-byte MBR.
- S02 `# Lesson: C With Nothing Underneath {#freestanding-c}` —
  freestanding vs hosted, why no libc in ring 0.
  Ch `{#kmem points=15}` — kmemset/kmemcpy/kmemmove/kstrlen/kstrcmp.
- S03 `# Lesson: A Screen to Print On {#vga-text-mode}` — VGA text buffer.
  Ch `{#vga-console points=20}` — console with control chars + scrolling.
- S04 `# Lesson: kprintf {#kprintf}` — varargs and formatting.
  Ch `{#kvsnprintf points=25}` — bounded vsnprintf subset.
- S05 `# Lesson: Segments and Privilege {#segmentation}` — GDT, rings,
  selectors. Ch `{#gdt-encode points=15}`; Ch `{#selector-check points=10}`.
- S06 `# Lesson: Interrupts and the IDT {#interrupts}` — vectors, gates,
  8259 PIC. Ch `{#idt-gate points=15}`; Ch `{#pic8259 points=20}`.
- S07 `# Lesson: Owning Physical Memory {#physical-memory}` — E820 map,
  frames. Ch `{#memmap-normalize points=15}`; Ch `{#frame-alloc points=20}`.
- S08 `# Lesson: Paging and Virtual Memory {#paging}` — two-level tables.
  Ch `{#vaddr-split points=10}`; Ch `{#page-map points=25}`.
- S09 `# Lesson: The Kernel Heap {#kernel-heap}` — kmalloc design.
  Ch `{#kmalloc points=30}` — first-fit free list, split + coalesce.
- S10 `# Lesson: Processes: the Kernel's Bookkeeping {#processes}` — the
  proc table. Ch `{#proc-table points=15}`; Ch `{#context-frame points=10}`.
- S11 `# Lesson: Scheduling {#scheduling}` — round-robin → multilevel
  queues. Ch `{#runqueue points=10}`; Ch `{#mlq-schedule points=25}`.
- S12 `# Lesson: Message Passing — the Microkernel Heart {#message-passing}`
  — send/receive/sendrec rendezvous, deadlock.
  Ch `{#ipc-rendezvous points=35}`.
- S13 `# Lesson: Sharing Without Tearing {#synchronization}` — races,
  test-and-set, semaphores. Ch `{#semaphore points=20}`.
- S14 `# Lesson: The Clock Ticks {#clock}` — PIT, ticks, timers.
  Ch `{#pit-divisor points=10}`; Ch `{#timer-queue points=20}`.
- S15 `# Lesson: The Keyboard {#keyboard}` — 8042, scancode set 1.
  Ch `{#scancode-decode points=20}`.
- S16 `# Lesson: The TTY and the Line Discipline {#tty-line-discipline}` —
  canonical mode. Ch `{#tty-canon points=25}`.
- S17 `# Lesson: Block Devices and the Buffer Cache {#buffer-cache}` —
  LRU, dirty write-back. Ch `{#buf-cache points=25}`.
- S18 `# Lesson: A Filesystem on a Disk {#fs-on-disk}` — Minix FS layout.
  Ch `{#superblock-parse points=15}`; Ch `{#fs-bitmap points=15}`.
- S19 `# Lesson: Inodes: Files Without Names {#inodes}` — direct/indirect
  zones. Ch `{#inode-bmap points=25}`.
- S20 `# Lesson: Directories and Path Walking {#directories}` — dirents,
  namei. Ch `{#dirent-scan points=15}`; Ch `{#path-resolve points=25}`.
- S21 `# Lesson: The System Call Boundary {#system-calls}` — traps,
  dispatch, user pointers. Ch `{#syscall-dispatch points=20}`.
- S22 `# Lesson: Birth, Death, and Zombies {#process-lifecycle}` — fork,
  exit, wait, reparenting. Ch `{#wait-exit points=25}`.
- S23 `# Final Challenge: DuckOS, Assembled {#final-duckos points=100}` —
  ONE H1 final challenge (no lesson heading): proc table + multilevel
  scheduler + rendezvous IPC + timers + exit/wait driven as one kernel.

## Deliverables and verification (do ALL of this before reporting)

Work in the scratchpad directory given in your task prompt.

1. Write your section's markdown to the assigned `sections/sNN.md` path.
   Hit your assigned line-count target (targets are generous; prose depth
   and worked examples get you there honestly — never pad with filler,
   repetition, or vacuous bullet lists).
2. For EACH challenge in your section: extract the Starter block to
   `work/<slug>/solution.c` and the Tests block to
   `work/<slug>/test_solution.c`. Then:
   a. `cc -std=c17 -Wall -Wextra -O1 -o test_bin solution.c test_solution.c`
      must succeed with ZERO warnings.
   b. `./test_bin` (starter as-is) must run to completion, print at least
      one `--- FAIL:`, and exit non-zero (check `$?`). It must NOT crash.
   c. Write a correct reference solution to `work/<slug>/refsol.c`
      (same file with TODOs implemented — keep it; the coordinator audits
      it). Compile it with the same flags against the tests: zero
      warnings, all lines `--- PASS:`, exit 0.
   Fix the section markdown if any step fails, and re-verify. The markdown
   file is the source of truth — code you verified must be byte-identical
   to the code in the markdown (re-extract after edits).
3. Final report (your return message): section file path, total line
   count, per-challenge verification results (compile/starter-fails/refsol-
   passes), and any spec deviations you had to make (should be none).
