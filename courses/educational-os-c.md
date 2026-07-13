---
course: educational-os
title: Build an Educational Operating System
language: c
description: >
  Build DuckOS, a Minix-inspired teaching kernel — the kind of OS Linus
  Torvalds ran at university before writing Linux. Start where the
  machine starts: real mode, the boot sector, freestanding C with no
  libc underneath. Print to VGA text memory, write your own kprintf,
  encode GDT descriptors and IDT gates, own physical memory frame by
  frame, build two-level page tables and a kernel heap. Then the living
  half: a Minix-style process table, a multilevel feedback scheduler,
  rendezvous message passing with deadlock detection, semaphores, the
  PIT and delta timer queues, a scancode decoder and a canonical-mode
  TTY. Add a buffer cache and a real Minix v1 filesystem — superblock,
  bitmaps, inodes, directories, namei — wire in the system call
  boundary and process lifecycle, and assemble it all into one
  event-driven kernel for the final challenge. An epilogue shows how to
  boot your own solutions in QEMU on real (virtual) hardware.
duration_hours: 45
tags: [c, systems-programming, operating-systems, kernel, unix]
extended_reading:
  - title: "Operating Systems: Design and Implementation (Tanenbaum) — the Minix book that started it all"
    url: https://en.wikipedia.org/wiki/Operating_Systems:_Design_and_Implementation
  - title: "Operating Systems: Three Easy Pieces (OSTEP) — free, and the best modern OS textbook"
    url: https://pages.cs.wisc.edu/~remzi/OSTEP/
  - title: "the xv6 book — MIT's teaching kernel, the spiritual successor to Minix"
    url: https://pdos.csail.mit.edu/6.828/2024/xv6/book-riscv-rev4.pdf
  - title: "OSDev wiki — the hobby-kernel community's collected scar tissue"
    url: https://wiki.osdev.org/
  - title: "Intel 64 and IA-32 Architectures SDM, Volume 3 — the hardware's side of the story"
    url: https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html
  - title: "LINUX is obsolete — the Tanenbaum–Torvalds debate, primary source"
    url: https://groups.google.com/g/comp.os.minix/c/wlhw16QWltI
  - title: "Linux 0.01 source — small enough to read end to end"
    url: https://mirrors.edge.kernel.org/pub/linux/kernel/Historic/
---

# Lesson: The Machine Wakes Up {#the-machine-wakes-up}

This course builds an operating system — not a toy that prints "hello"
and halts, but a small, honest kernel in the tradition of the great
teaching systems: a process table, a scheduler, message-passing IPC, a
paging memory manager, a filesystem you can hexdump. We call it
**DuckOS**, and by the final challenge you will have written the core
of every one of those pieces and driven them together as one kernel.

Why build it at all, when Linux exists? Because of how Linux exists.

## A Teaching OS Starts a Revolution

In the 1970s, universities taught operating systems from the real
thing: Bell Labs licensed Unix source to schools for a nominal fee, and
John Lions' line-by-line commentary on the Version 6 kernel let a class
read an entire production OS in a semester. Then AT&T's business side
noticed what it owned. The Version 7 license (1979) forbade teaching
from the source, and the most-studied kernel on earth became a trade
secret. OS professors were suddenly lecturing from block diagrams.

Andrew Tanenbaum, at the Vrije Universiteit in Amsterdam, decided the
fix was to stop depending on AT&T entirely. He wrote a Unix-compatible
OS from scratch — no AT&T code, small enough to read in a semester —
and shipped the full source with his 1987 textbook *Operating Systems:
Design and Implementation*. He called it **Minix**, and it ran on the
cheap hardware students actually owned: the IBM PC.

One of those students was Linus Torvalds at the University of Helsinki.
In January 1991 he bought a 386 clone, ran Minix on it, and started
writing programs to learn the 386 — a terminal emulator, then a disk
driver, then a filesystem, at which point the hobby had quietly become
an operating system. On August 25, 1991, he posted to comp.os.minix:

> I'm doing a (free) operating system (just a hobby, won't be big and
> professional like gnu) for 386(486) AT clones.

The hobby got big. Within months Tanenbaum and Torvalds were publicly
arguing over whether Linux's monolithic design made it "obsolete" on
arrival — a genuinely great argument about kernel architecture that we
take up in *Message Passing — the Microkernel Heart*, once you've built
enough of DuckOS to hold an informed opinion. For now the point is
simpler: Linux exists because a teaching OS existed, and a student who
could read one decided he could write one.

DuckOS is the OS that 1991 systems course would have handed you —
Minix-shaped, microkernel-hearted — in modern C with modern
explanations, targeting the 32-bit x86 so every number in this course
is a real number from a real machine. Which raises the first question
an OS author faces: what does that machine do when the power comes on?

## From Power Button to First Instruction

A CPU out of reset knows nothing. RAM holds garbage, the disk is an
inert platter, and the OS is the thing we haven't loaded yet. The only
guarantee is the **reset vector**: the 8086 wakes with its instruction
pointer aimed 16 bytes below the top of its 1MiB address space, at
physical `0xFFFF0`, where the motherboard maps a ROM chip — the
**BIOS**. Sixteen bytes is room for exactly one thing: a jump into the
ROM proper.

The BIOS runs its **POST** (power-on self-test), initializes the
interrupt and timer chips we'll meet in later lessons, then performs
the bootstrap, unchanged in spirit since 1981:

1. Read the first sector of the boot disk — 512 bytes, LBA 0, the
   **boot sector** — into memory at address `0x7C00`.
2. Check that its last two bytes, offsets 510 and 511, are `0x55` then
   `0xAA` — the **boot signature**.
3. If so, jump to `0x7C00`; whatever is there now owns the computer.
   If not, try the next disk.

The whole handoff at a glance — the amber terminal is the moment your 512 bytes own the machine; the dashed edge is the BIOS trying the next disk after a bad signature:

```d2
direction: right
reset: "CPU reset\nIP → 0xFFFF0" { shape: oval }
bios: "BIOS ROM:\nPOST, then\nread LBA 0\ninto 0x7C00"
sig: "bytes 510-511\n= 0x55 0xAA?"
own: "jump 0x7C00 —\nsector owns\nthe machine" { shape: oval; style.stroke: "#d97706"; style.stroke-width: 3 }
next: "try the\nnext disk"
reset -> bios -> sig
sig -> own: yes
sig -> next: no
next -> bios: { style.stroke-dash: 4 }
```

That is the entire contract. The BIOS does not parse your kernel or
check that the sector holds code rather than vacation photos — it
checks two magic bytes and jumps. The signature exists to catch the
common accident: a blank disk's zeroed sector fails the check, so the
BIOS moves on instead of executing 510 zeros.

Both numbers have stories. **512 bytes** is the sector size of the disk
formats the IBM PC shipped with — the smallest unit the controller
could read, and all the BIOS can be asked to understand before any
software runs. **`0x7C00`** is a fossil of the original PC's minimum
configuration: DOS 1.0 had to boot in 32KiB of RAM. Low memory was
spoken for (the interrupt vector table at `0x0000`–`0x03FF`, the BIOS
data area above it), so the boot sector went as high as that machine
allowed: 32KiB is `0x8000`, minus 512 bytes for the sector and 512 for
its stack and scratch data gives `0x7C00`. Machines outgrew 32KiB forty
years ago; every PC bootloader ever written — DOS's, Minix's, Linux's,
yours — still begins life at `0x7C00`.

So an OS begins as 510 bytes whose one job is to load more sectors (the
actual kernel) and jump — a *bootstrap loader*. But to write even those
bytes you must understand the strange way this CPU addresses memory
when it wakes up.

## Real Mode: Two Registers Per Address

The 8086 (1978) was a 16-bit CPU, so a bare register could name only
2^16 = 64KiB of memory. Intel wanted 1MiB — 2^20 — without paying for
32-bit registers, and the compromise haunts the PC to this day: every
address is two 16-bit values, a *segment* and an *offset*:

```
linear = segment * 16 + offset
```

Multiplying by 16 is a 4-bit shift — exactly what stretches 16-bit
registers across a 20-bit space. Segments live in dedicated registers
(`CS` for code, `DS` for data, `SS` for the stack, `ES` as a spare),
and every memory access implicitly pairs one with an offset. This is
**real mode**, and every x86 CPU since — including the one you're
reading this on — still wakes up in it, pretending to be a 1978 chip
until the OS says otherwise.

Work one by hand: segment `0x1234`, offset `0x5678`:

```
segment * 16:   0x1234  →  0x12340    (shift left one hex digit)
+ offset:                 + 0x5678
                          ────────
linear:                    0x179B8
```

And the boot sector's address, spelled the way BIOS writers spell it:
`0x07C0:0x0000` → `0x7C00`.

### One byte, 4096 names

Notice: `0x07C0:0x0000` and `0x0000:0x7C00` are different register
pairs naming the same byte. Segments start every 16 bytes but each
spans 64KiB, so they overlap massively — almost every address in the
low megabyte has exactly 4096 distinct spellings. `0x7C00` alone can be
written `0x0000:0x7C00`, `0x0700:0x0C00`, `0x07C0:0x0000`, and 1982
other ways.

This is a real source of boot bugs, not a party trick. The BIOS jumps
to the boot sector with `CS:IP` naming linear `0x7C00` — but most
BIOSes jump to `0x0000:0x7C00` while some jump to `0x07C0:0x0000`. Same
byte, different `CS`; a boot sector that assumes one spelling computes
jump targets into garbage on the other machine. Comparing two spellings
means comparing the linear addresses they produce — the first function
you'll write.

## The Top of Memory Is a Lie

Take the largest possible spelling, `0xFFFF:0xFFFF`:

```
0xFFFF0 + 0xFFFF = 0x10FFEF
```

That's above 1MiB — bit 20 is set — and the 8086 had only 20 address
lines, A0–A19. The bit simply falls off the edge of the bus and the
access **wraps**: `0xFFFF:0x0010`, linear `0x100000`, reads physical
`0x00000`. Software being software, programs came to depend on this —
DOS's CP/M-compatibility system-call entry leaned on segment
wraparound, and other code used high segments as a cheap route to low
memory.

Then the 80286 (1982) shipped with 24 address lines, and the 1984
PC/AT put it in front of DOS users. `0xFFFF:0x0010`
suddenly reached a real `0x100000`, the wrap-dependent tricks broke,
and IBM needed the PC/AT to keep running old DOS software. The fix is
one of the great kludges in hardware history: the keyboard controller —
an Intel 8042 microcontroller — had a spare output pin, so IBM wired it
into an AND gate on **address line 20**. Pin low: A20 is forced to zero
and addresses wrap like an 8086. Pin high: memory above 1MiB is
reachable. To this day, waking the full address bus of an x86 can
involve *asking the keyboard controller politely* — a rite of passage
for every bootloader.

The gate even created a prize: with A20 enabled but the CPU still in
real mode, addresses `0x100000`–`0x10FFEF` — the 65,520 bytes reachable
only through spellings like `0xFFFF:0x0010` and up — became the **High
Memory Area**. DOS's `HIMEM.SYS` existed largely to manage the A20
line, and `DOS=HIGH` moved most of DOS itself into the HMA to hand
users back conventional memory. A cottage industry of memory managers
grew from one spare pin on a keyboard chip.

So real-mode addressing has two personalities, selected by a gate:

```
A20 disabled:  linear = (segment * 16 + offset) & 0xFFFFF   (wraps)
A20 enabled:   linear =  segment * 16 + offset              (≤ 0x10FFEF)
```

## How DuckOS Challenges Work

One note before the first challenge, because it sets the pattern for
the course. The grader runs plain, headless C17 — it cannot boot a
floppy or wiggle a real A20 line. So every DuckOS challenge models the
hardware surface as plain C data and tests the pure logic: here, a disk
sector is a `uint8_t[512]` and an address is a `uint32_t`. This is less
of a cheat than it sounds — the signature checks, byte-order decoding,
and address arithmetic you write are the *exact* logic a real
bootloader runs; only the source of the bytes differs. In a real kernel
the struct IS the hardware table; here we build it in a buffer the
tests can read, and every line you write is verified the moment you
write it.

## Challenge: Real-Mode Addresses {#real-mode-address points=10}

Implement the 8086's address arithmetic, A20 gate included.

`linear_address(segment, offset, a20_enabled)` returns the physical
address of a `segment:offset` pair: compute `segment * 16 + offset` in
32-bit arithmetic (the true sum reaches `0x10FFEF`, which doesn't fit
in 16 bits). If `a20_enabled` is 0, mask with `0xFFFFF` so the result
wraps at 1MiB; otherwise return the full sum, HMA included.

`same_linear(s1, o1, s2, o2)` returns 1 if the two spellings name the
same linear address with A20 enabled, else 0 — the check a bootloader
uses to compare a BIOS's `CS:IP` against the address it expects.

The tests check the canonical boot-sector spellings (`0x07C0:0x0000` ==
`0x0000:0x7C00` == `0x7C00`), the top of the HMA with A20 on versus
wrapped with A20 off, wrap-to-zero at exactly 1MiB, that low addresses
ignore the gate, and aliases of the VGA text buffer at `0xB8000`
(you'll write to it in *A Screen to Print On*).

### Starter

```c
#include <stdint.h>

/* Real-mode (8086) address translation for DuckOS's boot path. */

#define A20_WRAP_MASK 0xFFFFFu	/* 20 address lines: A0..A19 */
#define REAL_MODE_MAX 0x10FFEFu	/* 0xFFFF:0xFFFF with A20 enabled */

/* Linear address of segment:offset. If a20_enabled is 0, the result
   wraps modulo 1MiB; otherwise it may reach up to REAL_MODE_MAX. */
uint32_t linear_address(uint16_t segment, uint16_t offset, int a20_enabled)
{
	/* TODO: shift the segment, add the offset, apply the gate */
	(void)segment;
	(void)offset;
	(void)a20_enabled;
	return 0;
}

/* Do s1:o1 and s2:o2 name the same byte, with A20 enabled?
   Returns 1 if so, 0 if not. */
int same_linear(uint16_t s1, uint16_t o1, uint16_t s2, uint16_t o2)
{
	/* TODO: two spellings match iff their linear addresses match */
	(void)s1;
	(void)o1;
	(void)s2;
	(void)o2;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

uint32_t linear_address(uint16_t segment, uint16_t offset, int a20_enabled);
int same_linear(uint16_t s1, uint16_t o1, uint16_t s2, uint16_t o2);

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
	/* The two BIOS spellings of the boot-sector address. */
	check(linear_address(0x07C0, 0x0000, 1) == 0x7C00,
	      "test_boot_segment_spelling");
	check(linear_address(0x0000, 0x7C00, 1) == 0x7C00,
	      "test_flat_spelling");
	check(linear_address(0x1234, 0x5678, 1) == 0x179B8,
	      "test_worked_example");
	check(linear_address(0xB800, 0x0000, 1) == 0xB8000,
	      "test_vga_text_buffer");

	/* Top of the High Memory Area: gate open vs gate closed. */
	check(linear_address(0xFFFF, 0xFFFF, 1) == 0x10FFEF,
	      "test_hma_top_a20_on");
	check(linear_address(0xFFFF, 0xFFFF, 0) == 0xFFEF,
	      "test_hma_top_wraps_a20_off");

	/* Exactly 1MiB: the first byte that needs address line 20. */
	check(linear_address(0xFFFF, 0x0010, 1) == 0x100000,
	      "test_first_byte_past_1mib");
	check(linear_address(0xFFFF, 0x0010, 0) == 0x00000,
	      "test_wrap_to_zero_a20_off");

	/* Below 1MiB the gate is irrelevant. */
	check(linear_address(0x07C0, 0x0000, 0) == 0x7C00,
	      "test_low_addresses_ignore_a20");

	/* Aliasing. */
	check(same_linear(0x07C0, 0x0000, 0x0000, 0x7C00) == 1,
	      "test_same_boot_spellings");
	check(same_linear(0x0700, 0x0C00, 0x07C0, 0x0000) == 1,
	      "test_third_spelling_of_7c00");
	check(same_linear(0xB800, 0x0000, 0xB000, 0x8000) == 1,
	      "test_vga_aliases");
	check(same_linear(0x07C0, 0x0000, 0x07C0, 0x0001) == 0,
	      "test_neighbors_differ");
	check(same_linear(0xFFFF, 0xFFFF, 0x0000, 0xFFEF) == 0,
	      "test_hma_is_not_the_wrap");

	return failed;
}
```

## Anatomy of the Boot Sector

Back to those 512 bytes. On PC disks the boot sector wears a second
hat: it is also the **Master Boot Record** (MBR), the disk's table of
contents:

```
offset   size   contents
------   ----   -------------------------------------------
     0    446   bootstrap code
   446     64   partition table: 4 entries x 16 bytes
   510      2   boot signature: 0x55, then 0xAA
```

Four partitions per disk, ever — a limit that shaped decades of disk
tooling (and the "extended partition" hacks around it) until GPT
replaced the format. Each 16-byte entry:

```
entry
offset   size   field
------   ----   -------------------------------------------
     0      1   status: 0x80 = active/bootable, 0x00 = inactive
   1-3      3   CHS address of first sector   (obsolete)
     4      1   partition type (0x83 Linux, 0x0C FAT32, 0 = unused)
   5-7      3   CHS address of last sector    (obsolete)
  8-11      4   LBA of first sector      (little-endian)
 12-15      4   sector count             (little-endian)
```

The CHS fields encode disk geometry that stopped matching physical
reality decades ago; everything modern uses the **LBA** (logical block
address) fields — plain sector numbers — and so will we. The
bootstrap's job in a partitioned world: find the entry whose status is
`0x80` (the *active* partition), load that partition's first sector,
chain to it. Here is a real entry, as your parser will see it:

```
offset 446:  80 20 21 00 83 8E 08 20 00 08 00 00 00 40 06 00

  80          status: bootable
  20 21 00    CHS start (ignored)
  83          type: Linux
  8E 08 20    CHS end (ignored)
  00 08 00 00 LBA start   = 0x00000800 = 2048
  00 40 06 00 sector count = 0x00064000 = 409600 (200 MiB)
```

Wait — the bytes `00 08 00 00` mean `0x00000800`? Read on.

## Little-Endian, Byte by Byte

x86 is a **little-endian** machine: multi-byte values are stored least
significant byte first, so `0x00000800` lands in memory as
`00 08 00 00`. (The convention descends from Intel's 8-bit 8080
lineage: carries propagate low-to-high, so starting arithmetic on the
low byte while the high bytes are still arriving is the order the
hardware wants.)

Your parser receives raw bytes through a `const uint8_t *` and must
reassemble values. The honest way is arithmetic:

```c
uint32_t v = (uint32_t)p[0]
           | (uint32_t)p[1] << 8
           | (uint32_t)p[2] << 16
           | (uint32_t)p[3] << 24;
```

Two things here are load-bearing:

- **The casts are not decoration.** C promotes anything narrower than
  `int` to `int` before arithmetic, so `p[3] << 24` shifts a *signed*
  int — and if `p[3]` is `0x80` or more, the shift pushes a 1 into the
  sign bit: undefined behavior in C17. Casting to `uint32_t` first
  makes every shift well-defined.
- **Refuse the cast trick** — `*(uint32_t *)(p + 8)` — for three
  separate reasons. It's UB on alignment grounds: offset 454 of a byte
  buffer has no business being read as an aligned 4-byte word. It's UB
  on *aliasing* grounds: C forbids reading memory of one type through a
  pointer to an unrelated type, and optimizers exploit that license.
  And it's wrong anyway on any big-endian machine, where the same four
  bytes reassemble as `0x00080000`. The shift-and-OR form spells out
  the on-disk byte order explicitly, so the same source decodes the
  same disk format correctly on every CPU. Disk formats have an
  endianness; your code shouldn't.

The tests plant a deliberately asymmetric LBA — `0x00003F00`, bytes
`00 3F 00 00` — that reads as `0x003F0000` if you assemble the bytes in
the wrong order.

## Challenge: Read the Boot Sector {#boot-sector points=15}

You're handed a 512-byte sector exactly as the BIOS would read it off
LBA 0. Implement the three routines DuckOS's boot path needs:

`mbr_valid(sector)` — 1 if byte 510 is `0x55` and byte 511 is `0xAA`,
else 0. The check the BIOS performs before jumping.

`mbr_parse(sector, out)` — decode the partition table into four
`struct mbr_partition` records. If the signature is invalid, return -1
and leave `out` completely untouched (a failing parser must fail
without side effects). Otherwise fill all four entries — empty ones
decode to zeros — and return 0. Per entry: `bootable` is 1 exactly when
the status byte equals `0x80` (else 0), `type` is the type byte, and
`lba_start` / `sector_count` are the little-endian 32-bit fields at
entry offsets 8 and 12.

`mbr_active_partition(sector)` — the index (0–3) of the first entry
whose status byte is exactly `0x80` *and* whose type is nonzero; -1 if
none. Both conditions matter: status `0x00` is inactive whatever the
type; a `0x80` status on a type-0 entry is table corruption, not a
bootable partition; and a status that is neither `0x00` nor `0x80` (the
tests plant `0x81`) must not count as active.

The tests build sectors byte-by-byte with a little-endian `put_le32`
helper and cover: valid, zeroed, and byte-swapped signatures; a
two-partition table checked field by field; empty entries; the `0x3F00`
byte-order trap; the untouched-`out` guarantee; an active partition in
slot 2; and the corrupt-status cases above.

### Starter

```c
#include <stdint.h>

/*
 * Master Boot Record parsing for DuckOS's boot path.
 *
 * Sector layout: bytes 0..445 bootstrap code, 446..509 partition
 * table (4 entries x 16 bytes), 510..511 signature 0x55, 0xAA.
 * Within each entry: +0 status (0x80 = active), +4 type (0 = unused),
 * +8 LBA of first sector (LE32), +12 sector count (LE32). Bytes
 * +1..3 and +5..7 are obsolete CHS fields; ignore them.
 */

#define MBR_TABLE_OFFSET 446
#define MBR_ENTRY_SIZE   16
#define MBR_ENTRIES      4

struct mbr_partition {
	uint8_t  bootable;	/* 1 iff status byte == 0x80 */
	uint8_t  type;		/* partition type ID; 0 = unused */
	uint32_t lba_start;	/* first sector of the partition */
	uint32_t sector_count;	/* length in 512-byte sectors */
};

/* 1 if sector[510..511] is the 0x55 0xAA boot signature, else 0. */
int mbr_valid(const uint8_t *sector)
{
	/* TODO */
	(void)sector;
	return 0;
}

/* Decode all four partition entries into out[0..3].
   Returns 0 on success; -1 (leaving out untouched) if the boot
   signature is invalid. */
int mbr_parse(const uint8_t *sector, struct mbr_partition out[4])
{
	/* TODO: check the signature, then decode each entry; assemble
	   the LE32 fields with shifts and ORs, casting each byte to
	   uint32_t before shifting */
	(void)sector;
	(void)out;
	return -1;
}

/* Index (0-3) of the first entry with status exactly 0x80 and a
   nonzero type byte; -1 if no such entry. */
int mbr_active_partition(const uint8_t *sector)
{
	/* TODO */
	(void)sector;
	return -1;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

struct mbr_partition {
	uint8_t  bootable;	/* 1 iff status byte == 0x80 */
	uint8_t  type;		/* partition type ID; 0 = unused */
	uint32_t lba_start;	/* first sector of the partition */
	uint32_t sector_count;	/* length in 512-byte sectors */
};

int mbr_valid(const uint8_t *sector);
int mbr_parse(const uint8_t *sector, struct mbr_partition out[4]);
int mbr_active_partition(const uint8_t *sector);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Store v at p in little-endian byte order, as x86 firmware would. */
static void put_le32(uint8_t *p, uint32_t v) {
	p[0] = (uint8_t)(v & 0xFF);
	p[1] = (uint8_t)((v >> 8) & 0xFF);
	p[2] = (uint8_t)((v >> 16) & 0xFF);
	p[3] = (uint8_t)((v >> 24) & 0xFF);
}

static void blank_sector(uint8_t *s) {
	memset(s, 0, 512);
}

static void sign_sector(uint8_t *s) {
	s[510] = 0x55;
	s[511] = 0xAA;
}

static void set_entry(uint8_t *s, int i, uint8_t status, uint8_t type,
                      uint32_t lba_start, uint32_t sector_count) {
	uint8_t *e = s + 446 + i * 16;
	e[0] = status;
	e[4] = type;
	put_le32(e + 8, lba_start);
	put_le32(e + 12, sector_count);
}

int main(void) {
	uint8_t s[512];
	struct mbr_partition p[4], q[4];
	int rc;

	blank_sector(s);
	sign_sector(s);
	check(mbr_valid(s) == 1, "test_valid_signature");

	blank_sector(s);
	check(mbr_valid(s) == 0, "test_zeroed_sector_invalid");
	s[510] = 0xAA;
	s[511] = 0x55;
	check(mbr_valid(s) == 0, "test_swapped_signature_invalid");

	/* Parsing a two-partition disk. */
	blank_sector(s);
	sign_sector(s);
	set_entry(s, 0, 0x80, 0x83, 2048, 409600);
	set_entry(s, 1, 0x00, 0x0C, 411648, 204800);
	memset(p, 0, sizeof p);
	rc = mbr_parse(s, p);
	check(rc == 0, "test_parse_returns_zero");
	check(rc == 0 && p[0].bootable == 1 && p[0].type == 0x83,
	      "test_parse_entry0_flags");
	check(rc == 0 && p[0].lba_start == 2048 && p[0].sector_count == 409600,
	      "test_parse_entry0_extents");
	check(rc == 0 && p[1].bootable == 0 && p[1].type == 0x0C &&
	      p[1].lba_start == 411648 && p[1].sector_count == 204800,
	      "test_parse_entry1_inactive");
	check(rc == 0 && p[2].bootable == 0 && p[2].type == 0 &&
	      p[2].lba_start == 0 && p[3].sector_count == 0,
	      "test_parse_empty_entries_zeroed");

	/* Byte-order trap: 00 3F 00 00 is 0x3F00, not 0x003F0000. */
	blank_sector(s);
	sign_sector(s);
	set_entry(s, 0, 0x00, 0x83, 0x00003F00, 1);
	memset(p, 0, sizeof p);
	rc = mbr_parse(s, p);
	check(rc == 0 && p[0].lba_start == 0x00003F00,
	      "test_parse_little_endian_order");

	/* Bad signature: refuse, and leave out untouched. */
	blank_sector(s);
	set_entry(s, 0, 0x80, 0x83, 2048, 409600);
	memset(p, 0xAB, sizeof p);
	memset(q, 0xAB, sizeof q);
	rc = mbr_parse(s, p);
	check(rc == -1 && memcmp(p, q, sizeof p) == 0,
	      "test_parse_rejects_bad_signature");

	/* Active-partition scan. */
	blank_sector(s);
	sign_sector(s);
	check(mbr_active_partition(s) == -1, "test_no_active_in_empty_table");
	set_entry(s, 2, 0x80, 0x83, 2048, 409600);
	check(mbr_active_partition(s) == 2, "test_active_in_slot_2");

	blank_sector(s);
	sign_sector(s);
	set_entry(s, 0, 0x00, 0x83, 2048, 100);	/* real but inactive */
	set_entry(s, 1, 0x80, 0x00, 0, 0);	/* active status, unused type */
	set_entry(s, 3, 0x80, 0x0C, 4096, 100);
	check(mbr_active_partition(s) == 3,
	      "test_skips_inactive_and_typeless");

	blank_sector(s);
	sign_sector(s);
	set_entry(s, 0, 0x81, 0x83, 2048, 100);	/* corrupt status byte */
	check(mbr_active_partition(s) == -1,
	      "test_status_must_be_exactly_0x80");

	return failed;
}
```

With these two challenges you've written the logic of the first instant
of an OS's life: the address arithmetic the CPU performs on every
access at power-on, and the parsing a bootloader does to find something
to boot. What you don't have yet is anything to build *on* — no printf,
no malloc, no memcpy, because those live in an OS's standard library
and you are now the person responsible for providing one. That is the
subject of the next lesson, *C With Nothing Underneath*.

# Lesson: C With Nothing Underneath {#freestanding-c}

In *The Machine Wakes Up* the boot sector hauled our kernel off the disk
and jumped into it. Now we get to write that kernel — in C, thankfully,
not in 16-bit assembly. But the C you have written all your life had an
enormous silent partner, and that partner just left the room.

Type `printf("hello\n");` in an ordinary program and you are not
talking to the machine. You are talking to **libc**, which formats your
string and asks the *operating system* to display it. Every comfort of
everyday C bottoms out in a request to a kernel:

```
   your code       printf("uptime: %d ticks\n", ticks)
   ------------------------------------------------------------
   libc            formats 17 bytes into a buffer, then calls
                   write(1, "uptime: 42 ticks\n", 17)
   ============== the syscall line ============================
   kernel          sys_write -> tty driver -> VGA text memory
   ------------------------------------------------------------
   hardware        characters appear at 0xB8000
```

`printf` ends in `write(2)`. `malloc` ends in `brk(2)` or `mmap(2)` —
libc asks the kernel for pages, then carves them into allocations.
`fopen` ends in `open(2)`; even `exit()` is a syscall. The standard
library is a well-dressed messenger, and every message crosses the
double line.

DuckOS lives *below* the double line. When our kernel code runs in ring
0, there is no kernel underneath it to call — we **are** the thing that
`write` and `mmap` are implemented on top of. Linking libc into a kernel
isn't forbidden by etiquette; it's circular. libc is built ON the
kernel. A kernel that calls `printf` is chasing its own tail.

Minix faced exactly this in 1987: Tanenbaum shipped it with its own
small `klib` of memory and string routines, separate from the C library
its *user programs* got. Linus did the same in 1991. So will we.

## Two C languages: hosted and freestanding

The C standard saw this coming. C17 §4 defines two conformance levels —
two different deals between you and the implementation:

- A **hosted** implementation is the one you know: the full standard
  library exists, and execution starts at `main` — `gcc` on anything
  with an OS under it.
- A **freestanding** implementation promises almost nothing: the entry
  point is implementation-defined (ours is whatever the boot sector
  jumps to), and only a short list of headers must exist.

The freestanding headers are exactly the ones that require **no code at
run time** — they are typedefs, macros, and compiler intrinsics, so
they work even when there is nothing to link against:

| Header          | What it gives a kernel                              |
|-----------------|-----------------------------------------------------|
| `<stdint.h>`    | `uint8_t`, `uint32_t` — hardware-shaped integers    |
| `<stddef.h>`    | `size_t`, `NULL`, `offsetof`                        |
| `<stdarg.h>`    | `va_list` — how *kprintf* will walk its arguments   |
| `<limits.h>`    | `CHAR_BIT`, `INT_MAX` and friends                   |
| `<stdbool.h>`   | `bool`, `true`, `false`                             |
| `<float.h>`, `<iso646.h>`, `<stdalign.h>`, `<stdnoreturn.h>` — the rest of the C17 §4 list |

Notice what's absent: `<stdio.h>`, `<stdlib.h>`, `<string.h>`. Anything
whose functions would need a body needs someone to *provide* that body,
and in ring 0 that someone is you.

You tell gcc which deal you're taking with two flags:

```
gcc -ffreestanding -nostdlib -c kernel.c
```

- `-ffreestanding` changes what the *compiler* assumes: it sets
  `__STDC_HOSTED__` to 0 and stops treating standard functions as
  special. (Hosted gcc happily rewrites `printf("hi\n")` into
  `puts("hi")` — a disaster if your kernel has a `printf` but no `puts`.)
- `-nostdlib` changes what the *linker* pulls in: no libc, no `crt0.o`
  startup file that normally calls `main`. Your entry point, your rules.

The two deals, side by side — each arrow means "is built on"; the
amber box is the layer you must now write yourself:

```d2
direction: right

hosted: "hosted C — a user program" {
  direction: down
  code: "your code"
  libc: "libc — printf, malloc, fopen"
  kern: "OS kernel"
  hw: "hardware"
  code -> libc
  libc -> kern: syscall
  kern -> hw
}

duckos: "freestanding C — DuckOS, ring 0" {
  direction: down
  code: "your kernel code"
  klib: "klib — you write it" {
    style.stroke: "#d97706"
    style.stroke-width: 3
  }
  hw: "hardware"
  code -> klib
  klib -> hw
}
```

## The compiler still believes in memcpy

Here is the surprise inside the surprise. Compile this — no `#include`
at all, `-ffreestanding` and everything:

```c
struct proc {
	int pid;
	int state;
	char name[64];
};

void adopt(struct proc *dst, const struct proc *src) {
	*dst = *src;	/* plain struct assignment */
}
```

and gcc may emit `call memcpy`. Not "may" as in rarely: for struct
assignment, array initializers like `char buf[256] = {0};`, and structs
returned by value, anything bigger than a few machine words compiles
into a call to `memcpy` or `memset`. This is documented behavior, not a
bug: gcc's manual states that even a freestanding environment must
provide `memcpy`, `memmove`, `memset`, and `memcmp`, because the
compiler reserves the right to call them.

The dependency gcc creates behind your back — red is the call the
compiler invents; amber is who has to supply it in ring 0:

```d2
direction: right

asg: "*dst = *src"
init: "buf[256] = {0}"
gcc: "gcc, even with\n-ffreestanding"
call: "call memcpy\ncall memset" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
klib: "your klib\nsupplies the body" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}

asg -> gcc
init -> gcc
gcc -> call: emits
call -> klib
```

This is the classic reason every kernel contains its own copy of these
four functions. Linux carries hand-tuned assembly versions per
architecture; Minix's klib had them; DuckOS gets them in this lesson.

One naming note before we write them. In this course our kernel code is
*hostable simulation* — the grader compiles it with an ordinary hosted
gcc so the tests can use real `printf` to report results — so we
namespace our routines `kmemset`, `kmemcpy`, `kstrlen`... to avoid
colliding with the libc the tests still link. In a real freestanding
build you would use the un-prefixed names: they are what gcc calls.

## Writing kmemcpy: why the loop is unsigned char

The heart of a byte copy is three lines:

```c
void *kmemcpy(void *dst, const void *src, size_t n) {
	unsigned char *d = dst;
	const unsigned char *s = src;
	for (size_t i = 0; i < n; i++)
		d[i] = s[i];
	return dst;
}
```

Why `unsigned char` and not `char`? Three reasons, all load-bearing:

1. **`unsigned char` is C's official "raw memory" type.** The standard
   guarantees it can inspect the bytes of *any* object without running
   afoul of aliasing rules, and every bit pattern is a valid value.
2. **Byte arithmetic must be 0–255.** The moment you compare or compute
   with bytes — and `kstrcmp` below does — a signed `char` holding
   0xE9 is the *negative* number −23. Copying happens to survive this;
   comparing does not. Using `unsigned char` everywhere means never
   having to remember which operations are safe.
3. **The standard defines these functions this way.** C17 specifies
   `memset` and friends as operating on the object *as an array of
   unsigned char*. Matching the spec means matching every hex dump
   you'll ever debug against.

Note the return value: `memcpy`, `memmove`, and `memset` all return
their destination pointer (it enables chaining like
`use(memcpy(dst, src, n))`). It is the contract; our k-versions honor it.

## Overlap: the bug that memmove was born from

`memcpy`'s contract has a famous trapdoor: **if the source and
destination regions overlap, the behavior is undefined.** That is not
the standard being lazy — it is the standard buying speed. Freed from
overlap, an implementation may copy in any order it likes: 8 bytes at a
time, back-to-front, with vector registers. Real memcpys do all of these.

But kernels *need* overlapping copies constantly: scrolling a screen
moves every line up over the previous one; deleting a directory entry
slides the rest of the block down; a TTY line discipline shuffles bytes
within one buffer. For that there is `memmove`, which promises a result
**as if** the bytes went through a temporary buffer — correct for any
overlap.

You don't need an actual temporary buffer, though. You need to pick the
right *direction*. Say `buf` holds `A B C D E F G H` and we copy 4
bytes from `buf` to `buf + 2`. Lay the two 4-byte windows over the same
buffer and the hazard is visible before a single byte moves:

```
index         0 1 2 3 4 5 6 7
buf           A B C D E F G H
src = buf     [=====]              read these 4 bytes...
dst = buf+2       [=====]          ...write them here
                  ^^^
              bytes 2-3 belong to BOTH windows
```

Watch a naive forward loop destroy its own source:

```
index         0 1 2 3 4 5 6 7
start         A B C D E F G H     want: A B A B C D G H
i=0           A B A D E F G H     dst[0] = src[0] = 'A'
i=1           A B A B E F G H     dst[1] = src[1] = 'B'
i=2           A B A B A F G H     dst[2] = src[2] — but src[2] IS
                                  dst[0]: we read back the 'A' we
                                  just wrote. 'C' is already gone.
i=3           A B A B A B G H     same again: reads our copied 'B'
result        A B A B A B G H     wanted A B A B C D G H
```

The copy caught up with itself: the write head (`dst`) ran ahead of
data the read head (`src`) hadn't reached yet. The fix is to copy the
same bytes **backward**, so every byte is read before anything
overwrites it:

```
index         0 1 2 3 4 5 6 7
start         A B C D E F G H
i=3           A B C D E D G H     dst[3] = src[3] = 'D' — still intact
i=2           A B C D C D G H     dst[2] = src[2] = 'C' — still intact
i=1           A B C B C D G H     dst[1] = src[1] = 'B'
i=0           A B A B C D G H     dst[0] = src[0] = 'A'
result        A B A B C D G H     correct
```

And it's perfectly symmetric: when the destination sits *below* the
source (`kmemmove(buf, buf + 2, 4)`), the backward loop is the one that
eats its own tail and the forward loop is safe. The rule falls straight
out of the pictures:

```
dst < src   ->  copy forward   (ascending addresses)
dst > src   ->  copy backward  (descending addresses)
dst == src  ->  nothing to do
```

You never need to test *whether* they overlap — the direction chosen by
relative order is also correct for regions that don't overlap at all.
(Pedantic aside: ISO C leaves `<` on pointers into unrelated objects
undefined. Every real kernel compares them anyway — a kernel targets
one flat address space where pointer order is just number order.)

## memset's odd little int

Look closely at the canonical signature:

```c
void *memset(void *dst, int c, size_t n);
```

Why is the fill byte an `int`? Fossil record. The interface predates
prototypes: K&R C promoted every `char` argument to `int` at the call
site, so when ANSI standardized the library in 1989 the parameter was
already an `int` everywhere, and it stayed one. The standard patches
over the history with one sentence: the value is **converted to
`unsigned char`** before filling.

The practical consequence: only the low 8 bits matter.

```c
kmemset(buf, 0x1234AB, 4);	/* fills with 0xAB 0xAB 0xAB 0xAB */
kmemset(buf, -1, 4);		/* fills with 0xFF 0xFF 0xFF 0xFF */
```

The tests plant exactly this trap — a fill value wider than a byte —
because storing the `int` instead of truncating it is a real and
recurring kernel bug.

## strcmp compares unsigned, and it matters

`strcmp` returns negative, zero, or positive as the first string sorts
before, equal to, or after the second. The subtlety is *what order
bytes sort in*. C17 (§7.24.4) is explicit: the sign is determined by
the first differing pair of characters, **both interpreted as
`unsigned char`**.

That clause exists because plain `char` is signed on most machines,
and signed comparison scrambles the top half of the byte range:

```
bytes:              'a' = 0x61        0xE9  (é in Latin-1)
as unsigned char:    97               233     ->  "a" < "\xE9"  correct
as signed char:      97               -23     ->  "\xE9" < "a"  WRONG
```

Every byte from 0x80 to 0xFF flips from "after ASCII" to "before
everything" if you compare through signed char. A kernel cares:
directory entries in the Minix filesystem (see *Directories and Path
Walking*) are raw bytes, not guaranteed ASCII, and a comparison that
disagrees with itself about byte order makes sorted structures
unsearchable. The fix costs nothing — walk the strings through
`const unsigned char *` and subtract:

```c
return (int)*pa - (int)*pb;	/* both 0..255 after promotion:
				   the difference can't overflow */
```

## One word about volatile

One more keyword completes the freestanding toolkit: `volatile`. It
tells the compiler "this memory can be seen — or changed — by something
that isn't this program," so every read and write in the source must
really happen, in order, at the width written. Ordinary variables get
cached in registers and optimized away; a memory-mapped VGA cell or a
device status register must not be. The next lesson, *A Screen to Print
On*, writes the console through a `volatile uint16_t *` for exactly
this reason. File the intuition: `volatile` is how C code admits the
hardware is watching.

## Challenge: A Kernel's String Routines {#kmem points=15}

Build DuckOS's klib: the five routines every kernel carries because
nothing underneath will provide them. The names are `k`-prefixed so the
hosted test harness (which still links libc for `printf`) doesn't
collide with the real thing.

The contracts, matching libc exactly:

- `void *kmemset(void *dst, int c, size_t n)` — fill `n` bytes of
  `dst` with the value `(unsigned char)c` (only the low 8 bits of `c`
  are used). Returns `dst`. `n == 0` writes nothing.
- `void *kmemcpy(void *dst, const void *src, size_t n)` — copy `n`
  bytes from non-overlapping `src` to `dst` (callers who can't promise
  that use `kmemmove`). Returns `dst`.
- `void *kmemmove(void *dst, const void *src, size_t n)` — copy `n`
  bytes correctly **even when the regions overlap, in either
  direction** — pick the direction from pointer order as derived
  above. Returns `dst`.
- `size_t kstrlen(const char *s)` — number of bytes before the
  terminating NUL. The empty string has length 0.
- `int kstrcmp(const char *a, const char *b)` — negative / zero /
  positive as `a` sorts before / equal to / after `b`, comparing
  bytes **as `unsigned char`**.

What the tests plant and check:

- Fill patterns verified byte-for-byte with `memcmp` against expected
  arrays, including a fill value (`0x1234AB`) that only works if you
  truncate to `unsigned char`, and zero-length calls that must leave
  the buffer untouched.
- Overlapping `kmemmove` in **both** directions, with the exact
  expected buffers from the worked example above — a forward-only loop
  fails one, a backward-only loop fails the other.
- Return values: the three memory routines must return `dst`.
- `kstrcmp` ordering both ways, the shorter-prefix case (`"abc"` vs
  `"abcd"`), and a byte-0xE9-versus-`'a'` case that returns the wrong
  sign if you compare through signed `char`.
- `kstrlen("")` — the loop must cope with finding the NUL immediately.

### Starter

```c
#include <stddef.h>

/*
 * DuckOS klib: freestanding memory and string routines.
 *
 * These are the functions gcc assumes exist even with -ffreestanding
 * (struct assignment and initializers may compile into calls to them),
 * plus the two string routines everything else in the kernel wants.
 * All loops must work on bytes as unsigned char.
 */

/* Fill n bytes of dst with (unsigned char)c; return dst. */
void *kmemset(void *dst, int c, size_t n) {
	/* TODO: truncate c to unsigned char, fill n bytes */
	(void)c;
	(void)n;
	return dst;
}

/* Copy n bytes from src to dst; regions must not overlap; return dst. */
void *kmemcpy(void *dst, const void *src, size_t n) {
	/* TODO: forward byte copy through unsigned char pointers */
	(void)src;
	(void)n;
	return dst;
}

/*
 * Copy n bytes from src to dst, correct for overlapping regions in
 * either direction; return dst.  dst < src: copy forward.
 * dst > src: copy backward so no byte is overwritten before it is read.
 */
void *kmemmove(void *dst, const void *src, size_t n) {
	/* TODO: choose direction from pointer order, then byte copy */
	(void)src;
	(void)n;
	return dst;
}

/* Length of NUL-terminated s, not counting the NUL. */
size_t kstrlen(const char *s) {
	/* TODO: count bytes until '\0' */
	(void)s;
	return 0;
}

/*
 * Compare NUL-terminated strings byte by byte AS UNSIGNED CHAR.
 * Return <0, 0, >0 as a sorts before, equal to, after b.
 */
int kstrcmp(const char *a, const char *b) {
	/* TODO: walk both as const unsigned char *; at the first
	   difference (or NUL) return the byte difference */
	(void)a;
	(void)b;
	return 0;
}
```

### Tests

```c
#include <stddef.h>
#include <stdio.h>
#include <string.h>

void *kmemset(void *dst, int c, size_t n);
void *kmemcpy(void *dst, const void *src, size_t n);
void *kmemmove(void *dst, const void *src, size_t n);
size_t kstrlen(const char *s);
int kstrcmp(const char *a, const char *b);

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
	/* ---- kmemset ---- */
	{
		unsigned char buf[8];
		memset(buf, 'x', sizeof buf);
		void *r = kmemset(buf, 0xAB, sizeof buf);
		unsigned char want[8];
		memset(want, 0xAB, sizeof want);
		check(memcmp(buf, want, sizeof buf) == 0,
		      "test_kmemset_fills_pattern");
		check(r == buf, "test_kmemset_returns_dst");
	}
	{
		/* Only the low 8 bits of c may be used. */
		unsigned char buf[4] = {0, 0, 0, 0};
		kmemset(buf, 0x1234AB, sizeof buf);
		unsigned char want[4] = {0xAB, 0xAB, 0xAB, 0xAB};
		check(memcmp(buf, want, sizeof buf) == 0,
		      "test_kmemset_truncates_int_to_byte");
	}
	{
		unsigned char buf[4] = {1, 2, 3, 4};
		kmemset(buf, 0xFF, 0);
		unsigned char want[4] = {1, 2, 3, 4};
		check(memcmp(buf, want, sizeof buf) == 0,
		      "test_kmemset_zero_length_is_noop");
	}
	{
		/* Fill the middle only; edges must survive. */
		unsigned char buf[6] = {9, 9, 9, 9, 9, 9};
		kmemset(buf + 2, 0x00, 2);
		unsigned char want[6] = {9, 9, 0, 0, 9, 9};
		check(memcmp(buf, want, sizeof buf) == 0,
		      "test_kmemset_partial_fill_respects_bounds");
	}

	/* ---- kmemcpy ---- */
	{
		unsigned char src[5] = {0xDE, 0xAD, 0xBE, 0xEF, 0x42};
		unsigned char dst[5] = {0, 0, 0, 0, 0};
		void *r = kmemcpy(dst, src, sizeof src);
		check(memcmp(dst, src, sizeof src) == 0,
		      "test_kmemcpy_copies_bytes");
		check(r == dst, "test_kmemcpy_returns_dst");
	}
	{
		unsigned char src[3] = {1, 2, 3};
		unsigned char dst[3] = {7, 7, 7};
		kmemcpy(dst, src, 0);
		unsigned char want[3] = {7, 7, 7};
		check(memcmp(dst, want, sizeof dst) == 0,
		      "test_kmemcpy_zero_length_is_noop");
	}

	/* ---- kmemmove ---- */
	{
		/* Overlap with dst above src: forward-only loops corrupt
		   this one (the worked example from the lesson). */
		unsigned char buf[8] = {'A','B','C','D','E','F','G','H'};
		void *r = kmemmove(buf + 2, buf, 4);
		check(memcmp(buf, "ABABCDGH", 8) == 0,
		      "test_kmemmove_overlap_dst_above_src");
		check(r == buf + 2, "test_kmemmove_returns_dst");
	}
	{
		/* Overlap with dst below src: backward-only loops corrupt
		   this one. */
		unsigned char buf[8] = {'A','B','C','D','E','F','G','H'};
		kmemmove(buf, buf + 2, 4);
		check(memcmp(buf, "CDEFEFGH", 8) == 0,
		      "test_kmemmove_overlap_dst_below_src");
	}
	{
		unsigned char src[4] = {10, 20, 30, 40};
		unsigned char dst[4] = {0, 0, 0, 0};
		kmemmove(dst, src, sizeof src);
		check(memcmp(dst, src, sizeof src) == 0,
		      "test_kmemmove_disjoint_regions");
	}
	{
		unsigned char buf[3] = {5, 6, 7};
		kmemmove(buf, buf, sizeof buf);
		unsigned char want[3] = {5, 6, 7};
		check(memcmp(buf, want, sizeof buf) == 0,
		      "test_kmemmove_same_pointer_is_noop");
	}

	/* ---- kstrlen ---- */
	check(kstrlen("duck") == 4, "test_kstrlen_basic");
	check(kstrlen("") == 0, "test_kstrlen_empty_string");
	check(kstrlen("a\0bc") == 1, "test_kstrlen_stops_at_first_nul");

	/* ---- kstrcmp ---- */
	check(kstrcmp("minix", "minix") == 0, "test_kstrcmp_equal");
	check(kstrcmp("abc", "abd") < 0, "test_kstrcmp_less");
	check(kstrcmp("abd", "abc") > 0, "test_kstrcmp_greater");
	check(kstrcmp("abc", "abcd") < 0, "test_kstrcmp_prefix_sorts_first");
	check(kstrcmp("abcd", "abc") > 0, "test_kstrcmp_longer_sorts_after");
	/* 0xE9 (233 unsigned, -23 as signed char) must sort AFTER 'a'
	   (97): signed-char comparison returns the wrong sign here. */
	check(kstrcmp("\xE9", "a") > 0, "test_kstrcmp_high_byte_unsigned");
	check(kstrcmp("a", "\xE9") < 0,
	      "test_kstrcmp_high_byte_unsigned_reversed");

	return failed;
}
```

# Lesson: A Screen to Print On {#vga-text-mode}

At the end of *C With Nothing Underneath*, DuckOS is a strange kind of
program: it runs, it computes, it can copy memory around — and it has no
way to tell you any of it. There is no `printf`, because there is no libc;
there is no terminal, because a terminal is something an *operating system*
provides, and we are the operating system. The machine is on, the kernel is
alive, and the screen is a blank blue rectangle.

Fixing that is the first thing every hobby kernel does, usually within
hours of first boot, because until you can print you are debugging
blind. Fortunately, the PC makes it almost absurdly easy — if you know
one magic address and one encoding rule. This lesson is about that
address, that rule, and the small pile of terminal logic (newlines,
tabs, wrapping, scrolling) that turns "I can poke a character onto the
screen" into "I have a console."

## The screen is an array

Boot a 1991 PC — or QEMU today, before any graphics mode is set — and the
video hardware comes up in **VGA text mode**: an 80-column by 25-row grid
of characters. The VGA card (Video Graphics Array, IBM, 1987 — the last
video standard IBM got the whole industry to copy) doesn't receive
characters over some protocol. Instead it continuously *reads video memory
and draws what it finds there*, 60-odd times a second. That memory is
mapped into the CPU's ordinary address space starting at physical address
`0xB8000`.

So printing on a PC is a store instruction:

```c
volatile uint16_t *vga = (volatile uint16_t *)0xB8000;
vga[0] = 0x1F44;	/* a white-on-blue 'D' in the top-left corner */
```

No driver, no syscall, no negotiation. The card will notice the new value
on its next sweep and the glyph appears. This style of device interface is
called **memory-mapped I/O** (MMIO): the hardware claims a range of
physical addresses, and reads/writes to that range go to the device
instead of to RAM.

Why `0xB8000` and not somewhere else? The original 1981 IBM PC reserved
the region from `0xA0000` to `0xFFFFF` — the top 384 KiB of the 8088's
one-megabyte address space — for hardware, which is exactly why real-mode
DOS programs famously had "640 KB of conventional memory" to work with.
Within that window, the monochrome display adapter (MDA) put its text
buffer at `0xB0000` and the color adapter (CGA) at `0xB8000`. Different
addresses on purpose: you could install *both* cards and drive two
monitors, and 1980s developers did exactly that — application on the color
screen, debugger on the mono screen. VGA inherited the color address, so
`0xB8000` it is.

And why 80×25? The 80 columns are older than the screen: they match the
80 columns of an IBM punch card, which terminals copied, which video
cards copied. The 25 rows are what fit on an NTSC-timed display. Half
the "standards" in this course are frozen accidents like these.

## Anatomy of a cell

Each of the 80 × 25 = 2000 grid positions is one 16-bit **cell**:

```
     15  14    12  11      8  7                  0
    ┌───┬─────────┬──────────┬────────────────────┐
    │ B │   bg    │    fg    │     code point     │
    └───┴─────────┴──────────┴────────────────────┘
      \______ attribute _____/ \______ glyph _____/
            (high byte)             (low byte)
```

- **Low byte: the code point.** Which character to draw, 0–255.
- **High byte: the attribute.** Low nibble = foreground color, bits 4–6 =
  background color, bit 7 = blink (about which more below).

x86 is little-endian, so the code point byte comes *first* in memory.
Writing the word `0x1F44` puts `0x44` at `0xB8000` and `0x1F` at
`0xB8001`. Spelling `Duck` in white-on-blue at the top-left corner looks
like this in a hex dump of video memory:

```
0xB8000:  44 1F  75 1F  63 1F  6B 1F  20 1F  20 1F ...
           'D'    'u'    'c'    'k'   ' '     ' '
```

The cell for row `r`, column `c` lives at index `r * 80 + c` — the same
row-major flattening you'd use for any 2-D array in C. That formula is
the skeleton of everything in this lesson's challenge.

### The code point is CP437, not ASCII

Here is a trap for the modern programmer: the low byte is **not ASCII**,
and it is definitely not UTF-8. The glyphs are burned into the adapter's
character ROM, and the set that IBM chose in 1981 became known as **code
page 437** (CP437). It *agrees* with ASCII for bytes 0x20–0x7E, which is
why naive kernels get away with pretending otherwise. But:

- Bytes 0x80–0xFF are accented letters, Greek, and — most famously — the
  **box-drawing characters** (`─ │ ┌ ┐ └ ┘ ├ ┤`, single and double line
  variants, at 0xB3–0xDA) plus shaded blocks (0xB0–0xB2, 0xDB). Every DOS
  "windowed" interface, every BIOS setup screen, every Norton Commander
  panel was drawn with these. They exist because a text-mode machine
  still wanted a UI.
- Bytes 0x01–0x1F, which ASCII reserves for control characters, have
  *glyphs* in CP437: 0x01 is a smiley face ☺, 0x02 its inverse ☻, 0x03 a
  heart, 0x0E a musical note. The VGA card knows nothing about "control
  characters" — store 0x01 in a cell and you get a smiley. IBM aimed the
  PC at hobbyists and games as much as offices, and fun glyphs beat 32
  blank cells.

That last point matters for the code you're about to write: **the
hardware will not interpret `\n` for you.** Store byte 0x0A in a cell and
VGA happily draws CP437 glyph 0x0A (an inverted circle, ◙). Newline,
carriage return, tab, backspace — all of them are *conventions* that a
console driver must implement in software by moving a cursor around. The
screen is a dumb grid; the terminal is a program. That program is this
lesson's challenge.

### The attribute byte: sixteen colors and one lie

The foreground nibble selects one of 16 colors; the background field is
3 bits, so nominally 8:

```
0x0 black       0x8 dark gray          bit 3 ("intensity")
0x1 blue        0x9 light blue         turns the dim color
0x2 green       0xA light green        into its bright twin
0x3 cyan        0xB light cyan
0x4 red         0xC light red
0x5 magenta     0xD light magenta
0x6 brown       0xE yellow
0x7 light gray  0xF white
```

(Color 6 is *brown*, not dark yellow — CGA monitors had a special circuit
that halved the green signal for exactly this one color, because IBM's
customers wanted brown. Hardware archaeology, layer upon layer.)

So `0x1F` = blue background (1), white foreground (F): the classic
"kernel panic blue." `0x4E` = yellow on red, for when things are on fire.

The lie is **bit 7**. Symmetry says it should be the background's
intensity bit, giving 16 background colors too. Instead, by default, it
makes the character *blink* — a hardware attention-getter from the
terminal era. A mode bit in the VGA attribute controller reclaims it as
bright-background (many kernels flip it at startup), but out of reset
`0x9F` is not "white on bright blue," it's "white on blue, flashing at
you." DuckOS's console carries the attribute byte around opaquely —
which attribute to use is the caller's policy.

## Two ways to talk to hardware: MMIO and ports

The VGA framebuffer taught you memory-mapped I/O. But x86 has a *second*,
completely separate address space for devices: **I/O ports**. Alongside
its 2^32 memory addresses, the CPU has 65,536 port numbers, reached only
through dedicated instructions — `out` to write a byte to a port, `in` to
read one. A port access never touches RAM and never goes through the
memory bus's caches; it's a distinct transaction the CPU signals with a
dedicated pin. This is an heirloom from Intel's 8080 lineage, where
address space was too scarce to spend on devices.

One device can straddle both worlds, and VGA does: its *framebuffer* is
MMIO at `0xB8000`, while its *control registers* — including the hardware
cursor we'll meet below — live in port space around `0x3D4`. You will see
ports again and again in this course: the 8259 interrupt controller
(*Interrupts and the IDT*) lives at ports 0x20/0x21, the programmable
timer (*The Clock Ticks*) at 0x40–0x43, the keyboard controller (*The
Keyboard*) at 0x60/0x64. Modern hardware has largely abandoned ports for
MMIO, but the PC's foundational devices were all designed in the port
era, so any x86 kernel speaks both dialects.

## Why the pointer must be `volatile`

Look again at the framebuffer pointer:

```c
volatile uint16_t *vga = (volatile uint16_t *)0xB8000;
```

The `volatile` is not decoration. The C compiler optimizes under the
assumption that memory is *just memory*: if you write a value nobody
reads, the store can be deleted; if you write the same location twice,
the first store can be deleted; independent stores can be reordered.
Consider clearing the screen:

```c
for (int i = 0; i < 80 * 25; i++)
	vga[i] = 0x0720;	/* blank: space, light gray on black */
```

Without `volatile`, the compiler sees a plain array that the program
never reads back. Dead-store elimination is allowed to delete the whole
loop — your screen-clear compiles to *nothing*, and only when optimizing,
so it works in the debug build and vanishes in release. `volatile` says
this location has side effects the compiler cannot see: every store must
actually happen, in the order written, none merged, none elided. The VGA
card is the "somebody" reading this memory, and the compiler can't know.

This is the single most common first-kernel bug in the wild, and it
recurs for every MMIO device you'll ever drive.

**Simulation note.** In a real kernel the pointer above IS the hardware —
those stores land in the video card. DuckOS, as always, is hostable: our
"video memory" is a `uint16_t buf[80 * 25]` array inside a struct, so the
tests can read the screen back and check what you drew. Same layout, same
encoding, same arithmetic — just a buffer instead of a bus. (And since
the tests *do* read it, `volatile` is unnecessary in the simulation;
remember it the day you point this code at `0xB8000`.)

## The console: a cursor and four control characters

A console driver wraps the raw grid in teletype behavior. Its state is
tiny: the current cursor position (`row`, `col`) and the attribute byte
to use for new characters. Printable bytes go into the cell under the
cursor and the cursor advances. Everything interesting is in the control
characters — and their semantics are literally mechanical, inherited from
typewriters and Teletype terminals:

- **`'\n'` (line feed, 0x0A)** — on a Teletype, the motor that rolls the
  paper up one line. For our console: cursor to column 0 of the *next*
  row. (A strict Teletype line feed kept the column; Unix folded the
  carriage return in, and we follow Unix.)
- **`'\r'` (carriage return, 0x0D)** — the lever that slams the print
  carriage back to the left margin: column 0, *same* row. Useful on its
  own for progress spinners that redraw one line in place.
- **`'\t'` (tab, 0x09)** — advance to the next **tab stop**, a column
  that's a multiple of 8. From column 5, a tab lands on 8; from column
  8, on 16 (a tab always advances at least one column). The columns
  skipped over are *written* as blanks in the current attribute — a tab
  paints background over anything it crosses. Why every 8? Teletype
  Model 33 convention, frozen into Unix, frozen into everything since.
- **`'\b'` (backspace, 0x08)** — cursor back one column, never past
  column 0, and — this surprises people — **it does not erase**. On the
  Teletype, backspace physically moved the carriage left so you could
  overstrike (that's how underlining worked: `a`, backspace, `_`).
  Erasing is the *shell's* job, which emits `"\b \b"` — back up, print a
  space over the character, back up again. The console just moves.

Two grid-boundary behaviors complete the driver:

**Wrapping.** Writing in column 79 fills the last cell of the row, and
the cursor advances to column 80 — which doesn't exist. The console wraps
to column 0 of the next row, exactly as if a `'\n'` had been typed. (Tab
never straddles the edge: 80 is a multiple of 8, so a tab's blanks stop
exactly at column 80 and the ordinary wrap rule takes over.)

The cascade of consequences — a write can trigger a wrap, and a wrap can trigger the red-bordered scroll:

```d2
direction: right

put: "printable / '\\t':\nwrite, col++"
nl: "'\\n'"
wrap: "col = 0\nrow + 1"
scroll: "scroll:\nrows 1–24 → 0–23\nblank row 24\nrow = 24" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

put -> wrap: "col hits 80"
nl -> wrap
wrap -> scroll: "row hits 25"
```

**Scrolling.** Wrapping off row 24 — or a `'\n'` on row 24 — has nowhere
to go. Real terminals scroll: every row moves up by one, the top row
falls off into oblivion, and the bottom row is freshly blanked for new
text. In video memory that's one overlapping copy plus one fill:

```
     before                        after scroll
row 0  │ oldest line   ◄─┐      │ second line
row 1  │ second line   ──┤      │ third line
row 2  │ third line    ──┤ copy │ ...
  ...  │ ...           ──┤ up   │ ...
row 23 │ ...           ──┤ one  │ newest line
row 24 │ newest line   ──┘ row  │ ░░░░ blanked ░░░░  ◄ cursor stays here
```

Concretely: move `24 * 80` cells from index `80` down to index `0` —
source and destination overlap, which is why `memmove` (or the
`kmemmove` you built in *C With Nothing Underneath*, whose whole
overlap-safe copy dance exists for moments like this) is the right
tool — then write the blank cell `(attr << 8) | ' '` into the 80 cells
of row 24, and pin the cursor's row at 24. The cursor never reaches row
25; the *content* moves instead. That's 3840 bytes of copying plus 160
of blanking — cheap enough to finish well inside one screen refresh.

The scroll as the buffer sees it — each arrow is a row moving up one slot in the same memory (the overlapping copy), and the bottom row is freshly blanked:

```d2
direction: right

before: "before" {
  shape: sql_table
  "row 0": "oldest line — falls off"
  "row 1": "second line"
  "⋮": "rows 2–23"
  "row 24": "newest line"
}

after: "after the scroll" {
  shape: sql_table
  "row 0": "second line"
  "⋮": ""
  "row 23": "newest line"
  "row 24": "blanked — cursor"
}

before."row 1" -> after."row 0"
before."⋮" -> after."⋮": "memmove"
before."row 24" -> after."row 23"
```

## The hardware cursor (prose only)

One loose end: the blinking underscore the *hardware* draws. That cursor
is not a character in the buffer — the VGA card overlays it at a position
stored in two of its own registers, and a real console driver updates
them after every write so the visible cursor tracks `row * 80 + col`.

Programming it introduces an idiom you'll meet all over the PC: the
**index/data register pair**. The VGA CRT controller has dozens of
internal registers but claims only two ports: you `out` a register
*index* to port `0x3D4` (14 = cursor-position high byte, 15 = low byte),
then read or write that register's *value* through port `0x3D5`. Two
ports multiplex an entire register file. The same two-step appears in
the CMOS real-time clock (ports 0x70/0x71) and the PIC's initialization
sequence — hardware designers reach for it whenever a device has more
registers than it can afford port numbers.

Our simulated console has no CRT controller, so the challenge tracks the
cursor purely as `row`/`col` fields — which is exactly the state a real
driver would copy into ports 0x3D4/0x3D5.

## Challenge: The Console Driver {#vga-console points=20}

Build DuckOS's console: a simulated VGA text screen with a cursor,
attribute handling, the four control characters, wrapping, and scrolling.

The screen is the `buf` array inside `struct console` — cell `(row, col)`
is `buf[row * VGA_COLS + col]`, encoded exactly as in the lesson: high
byte attribute, low byte code point. A **blank** cell is a space (0x20)
with the console's current attribute: `(attr << 8) | ' '`.

Implement three functions:

- `console_init(c, attr)` — remember `attr`, fill all 2000 cells with
  blanks, home the cursor to row 0, column 0.
- `console_putc(c, ch)` — one byte of output:
  - `'\n'`: cursor to column 0 of the next row.
  - `'\r'`: cursor to column 0, same row.
  - `'\b'`: cursor back one column; never past column 0; erases nothing.
  - `'\t'`: write blanks (space + current attribute) from the cursor
    forward until the column is the next multiple of 8 — always at least
    one column.
  - Anything else: store `(attr << 8) | (uint8_t)ch` at the cursor and
    advance one column.
  - **Wrap:** whenever the column reaches `VGA_COLS`, move to column 0
    of the next row.
  - **Scroll:** whenever the row reaches `VGA_ROWS` (via `'\n'` or via
    wrap), scroll: copy rows 1–24 up to rows 0–23 (the regions overlap —
    `memmove`, not `memcpy`), blank row 24 with the current attribute,
    and set the row to 24.
- `console_puts(c, s)` — feed each byte of the NUL-terminated string `s`
  through `console_putc`.

The tests read `buf`, `row`, and `col` directly. They check: init blanks
every cell and homes the cursor; a single `'A'` under attribute `0x1F`
encodes as `0x1F41`; `'\n'` and `'\r'` move the cursor as specified (and
`'\r'` lets the next character overwrite column 0); a tab from column 5
lands on column 8 leaving blanks in columns 5–7; writing the 80th
character of a row wraps the cursor to the next row; writing past the
bottom-right corner scrolls — a sentinel planted on row 1 moves up to row
0, the full bottom row is blanked, and the cursor's row is pinned at 24;
backspace at column 0 stays put and never erases; and a multi-line
`console_puts` leaves the cursor where the last character says it should
be.

This is hosted simulation, so `<string.h>`'s `memmove` is available and
allowed — in the real kernel you'd swap in your `kmemmove`.

### Starter

```c
#include <stdint.h>
#include <string.h>

#define VGA_COLS 80
#define VGA_ROWS 25

struct console {
	uint16_t buf[VGA_COLS * VGA_ROWS];	/* cell (r,c) = buf[r*VGA_COLS+c] */
	int row, col;				/* cursor position */
	uint8_t attr;				/* attribute byte for new characters */
};

/*
 * Set the console's attribute to attr, fill the whole screen with blank
 * cells (space, attr), and home the cursor to (0, 0).
 */
void console_init(struct console *c, uint8_t attr)
{
	/* TODO: store attr, blank all VGA_COLS * VGA_ROWS cells, home cursor */
	(void)c;
	(void)attr;
}

/*
 * Write one byte to the console.
 *
 * '\n' -> column 0 of the next row.
 * '\r' -> column 0 of the same row.
 * '\b' -> back one column (not past column 0); erases nothing.
 * '\t' -> write blanks until the column is the next multiple of 8
 *         (always advances at least one column).
 * Other bytes -> store (attr << 8) | byte at the cursor, advance a column.
 *
 * Whenever the column reaches VGA_COLS, wrap to column 0 of the next row.
 * Whenever the row reaches VGA_ROWS, scroll: move rows 1..24 up one row
 * (overlapping copy!), blank the bottom row, and set row to VGA_ROWS - 1.
 */
void console_putc(struct console *c, char ch)
{
	/* TODO: handle control characters, store printables, wrap, scroll */
	(void)c;
	(void)ch;
}

/* Write each byte of the NUL-terminated string s via console_putc. */
void console_puts(struct console *c, const char *s)
{
	/* TODO: loop console_putc over s */
	(void)c;
	(void)s;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define VGA_COLS 80
#define VGA_ROWS 25

struct console {
	uint16_t buf[VGA_COLS * VGA_ROWS];	/* cell (r,c) = buf[r*VGA_COLS+c] */
	int row, col;				/* cursor position */
	uint8_t attr;				/* attribute byte for new characters */
};

void console_init(struct console *c, uint8_t attr);
void console_putc(struct console *c, char ch);
void console_puts(struct console *c, const char *s);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static struct console con;

static uint16_t cell(int row, int col)
{
	return con.buf[row * VGA_COLS + col];
}

int main(void) {
	int i, ok;

	/* init: every cell becomes a blank (space with attr), cursor homed */
	memset(&con, 0xAA, sizeof con);
	console_init(&con, 0x07);
	ok = 1;
	for (i = 0; i < VGA_COLS * VGA_ROWS; i++)
		if (con.buf[i] != 0x0720)
			ok = 0;
	check(ok, "test_init_blanks_buffer");
	check(con.row == 0 && con.col == 0, "test_init_homes_cursor");

	/* cell encoding: attr 0x1F + 'A' (0x41) = 0x1F41 */
	console_init(&con, 0x1F);
	console_putc(&con, 'A');
	check(cell(0, 0) == 0x1F41, "test_putc_cell_encoding");
	check(con.row == 0 && con.col == 1, "test_putc_advances_cursor");

	/* newline: column 0 of the next row */
	console_init(&con, 0x07);
	console_puts(&con, "ab\ncd");
	check(cell(1, 0) == (0x0700 | 'c') && cell(1, 1) == (0x0700 | 'd') &&
	      con.row == 1 && con.col == 2,
	      "test_newline_moves_to_next_row_col0");

	/* carriage return: column 0, same row; next char overwrites */
	console_init(&con, 0x07);
	console_puts(&con, "abc");
	console_putc(&con, '\r');
	console_putc(&con, 'X');
	check(cell(0, 0) == (0x0700 | 'X') && con.row == 0 && con.col == 1,
	      "test_carriage_return_overwrites_col0");

	/* tab: from column 5, land on column 8 */
	console_init(&con, 0x2F);
	console_puts(&con, "12345");
	console_putc(&con, '\t');
	check(con.row == 0 && con.col == 8, "test_tab_from_col5_lands_col8");
	ok = 1;
	for (i = 5; i < 8; i++)
		if (cell(0, i) != 0x2F20)
			ok = 0;
	check(ok, "test_tab_writes_blanks_with_attr");

	/* wrap: the 80th character fills column 79, cursor wraps to (1,0) */
	console_init(&con, 0x07);
	for (i = 0; i < VGA_COLS; i++)
		console_putc(&con, 'x');
	check(cell(0, 79) == (0x0700 | 'x') && con.row == 1 && con.col == 0,
	      "test_wrap_at_col_80");
	console_putc(&con, 'W');
	check(cell(1, 0) == (0x0700 | 'W'), "test_char_after_wrap_lands_row1");

	/* scroll: writing past the bottom-right corner shifts rows up */
	console_init(&con, 0x07);
	console_putc(&con, '\n');
	console_putc(&con, 'S');		/* sentinel at (1, 0) */
	for (i = 0; i < 23; i++)		/* cursor to row 24 */
		console_putc(&con, '\n');
	for (i = 0; i < VGA_COLS; i++)		/* fill row 24; wrap scrolls */
		console_putc(&con, 'y');
	check(cell(0, 0) == (0x0700 | 'S'), "test_scroll_moves_sentinel_up");
	check(cell(23, 0) == (0x0700 | 'y') && cell(23, 79) == (0x0700 | 'y'),
	      "test_scroll_moves_full_row_up");
	ok = 1;
	for (i = 0; i < VGA_COLS; i++)
		if (cell(24, i) != 0x0720)
			ok = 0;
	check(ok, "test_scroll_blanks_bottom_row");
	check(con.row == 24 && con.col == 0, "test_scroll_pins_row_24");

	/* backspace: clamped at column 0, and never erases */
	console_init(&con, 0x07);
	console_putc(&con, '\b');
	check(con.row == 0 && con.col == 0, "test_backspace_at_col0_stays");
	console_puts(&con, "AB");
	console_putc(&con, '\b');
	check(con.col == 1 && cell(0, 1) == (0x0700 | 'B'),
	      "test_backspace_does_not_erase");

	/* puts: a multi-line string leaves the cursor after the last char */
	console_init(&con, 0x07);
	console_puts(&con, "hi\nthere");
	check(con.row == 1 && con.col == 5 && cell(1, 4) == (0x0700 | 'e'),
	      "test_puts_multiline_cursor");

	return failed;
}
```

# Lesson: kprintf {#kprintf}

When a kernel misbehaves, there is no debugger waiting to catch it. gdb
is a process; processes need a working kernel; the thing you are trying
to debug IS the kernel. Until you have built enough of an operating
system to run a debugger on top of it, you get exactly one diagnostic
tool, and it is the one you build yourself in week one: your own printf.

Linus Torvalds understood this immediately. Linux 0.01 — the 1991
release, ten thousand lines, no networking, no SCSI, barely a
filesystem — already contains `kernel/printk.c`. The name is `printk`,
"print kernel", because it cannot be `printf`: printf lives in libc, and
as we saw in *C With Nothing Underneath*, there is no libc in ring 0.
Every kernel since has carried the same tool under some name — and every
kernel developer will tell you it is the most-used debugging facility
they have, decades of fancy tracers notwithstanding.

Linux's printk grew one famous trick worth knowing. Kernel messages have
priorities, 0 (system is on fire) through 7 (debug chatter), and the
priority is smuggled in as a *prefix of the format string itself*:

```c
printk(KERN_WARNING "disk0: %d retries\n", tries);
```

`KERN_WARNING` is just the string literal `"<4>"`, and C pastes adjacent
string literals together at compile time, so what printk actually
receives is `"<4>disk0: %d retries\n"`. The logging core peels the
`<4>` off, records "priority 4" for that line, and tools like `dmesg`
can filter on it later. No extra argument, no new function per level —
just a three-character convention hiding in the format string. (Modern
kernels replaced `<` with an ASCII SOH byte, `"\0014"`, so that a `<`
you legitimately wanted to print can't be mistaken for a level — but the
mechanism is unchanged since the early days.)

DuckOS will call its version `kprintf`. Before writing it, notice that
printf is really two jobs glued together:

- **Formatting**: turn `("%d ticks", 100)` into the bytes
  `1 0 0   t i c k s`. Pure computation — no hardware, no I/O.
- **Output**: get those bytes in front of a human. Entirely
  device-specific.

We already did the second job: *A Screen to Print On* gave us a console
that can put a string on the VGA text buffer. This lesson builds the
first job, as a function called `kvsnprintf` — and the split is not our
invention. Your libc does exactly the same thing: `printf` is a thin
wrapper that formats into a buffer with `vsnprintf` and hands the result
to `write`. Keeping the formatter free of I/O also makes it testable,
which is precisely how this lesson's challenge will grade it.

## How arguments travel: stdarg.h

printf's signature is the strangest one in C:

```c
int kprintf(const char *fmt, ...);
```

The `...` says: after `fmt`, the caller may pass *any number of
arguments of any type*, and the function signature records nothing about
them. Somehow the function must find them anyway. The portable
machinery for that is `<stdarg.h>` — one of the handful of headers that
belongs to the *compiler*, not to libc, so it is available even in our
freestanding ring-0 world. Four macros:

```c
int kprintf(const char *fmt, ...)
{
	va_list ap;             /* a cursor over the unnamed arguments   */

	va_start(ap, fmt);      /* aim it just past the last NAMED arg   */
	int a = va_arg(ap, int);        /* fetch one argument AS an int, */
	char *s = va_arg(ap, char *);   /* advance; fetch the next...    */
	va_end(ap);             /* done; some ABIs need cleanup          */
	...
}
```

`va_arg(ap, T)` is the heart: it yields the next unnamed argument,
*interpreted as type T*, and steps the cursor past it. Note what is
missing: there is no `va_count`, no way to ask how many arguments exist
or what types they have. The format string is the only manifest. If
`fmt` says `%d %s` and the caller passed an `int` and a `char *`, all is
well. If the format string lies, you read memory that was never an
argument at all. Keep that in mind — it explains most of the rules that
follow.

### Everything arrives as int (or wider)

Try to fetch a character with `va_arg(ap, char)` and you have a bug (on
many compilers, a loud warning). Arguments passed through `...` undergo
the **default argument promotions** before they travel:

- `char`, `short` (signed or unsigned) are promoted to `int`,
- `float` is promoted to `double`.

So the `'d'` in `kprintf("%c", 'd')` arrives as a 4-byte `int` with the
value 100, and the only correct fetch is `va_arg(ap, int)`, cast back
down to `char` afterward. Why does C do this? History. Pre-ANSI C had
no prototypes: the compiler at a call site often had *no idea* what
parameter types the callee expected, so it promoted everything to a few
standard widths and both sides agreed on that convention. When ANSI C
added prototypes in 1989 it kept the old behavior exactly where
prototypes can't help — the `...` part, where the callee's expectations
are unknowable by design. A convenient side effect on a 32-bit machine:
every promoted integer argument occupies at least one full 4-byte stack
slot, which keeps the argument area a uniform array of words.

### Why the wrong type is undefined behavior

Nothing in memory records what was actually passed. `va_arg(ap, T)`
compiles to roughly "read `sizeof(T)` bytes at the cursor, advance the
cursor" — it *trusts* you. Fetch a `long long` (8 bytes) where an `int`
(4 bytes) was passed and you consume the next argument's slot as the
high half of a garbage value; every later fetch is now misaligned with
reality. The C standard calls this undefined behavior outright
(C17 7.16.1.1). This is printf's oldest footgun wearing a new hat:
`printf("%s", 42)` dereferences 42 as a pointer. In user space that's a
segfault and a core dump; in a kernel it's a triple fault and a reboot,
with your one debugging tool as the murder weapon. Discipline about
format strings is not optional at ring 0.

### What this looks like on an i386 stack

The C standard leaves *how* `va_list` works to the platform, and on the
i386 — the machine DuckOS targets, as fixed back in *The Machine Wakes
Up* — the mechanism is beautifully concrete. The i386 C calling
convention, **cdecl**, says: the caller pushes arguments onto the stack
**right to left**, then executes `CALL` (which pushes the return
address), and the caller pops the arguments afterward. So at the moment
`kprintf("%d and %x", 42, 0xff)` starts executing, the stack looks like
this (the stack grows downward; higher addresses at the top):

```
	higher addresses
	+--------------------+
	| 0x000000ff         |  <- third arg, pushed FIRST
	+--------------------+
	| 0x0000002a         |  <- second arg (42)
	+--------------------+
	| ptr to "%d and %x" |  <- first arg, pushed LAST
	+--------------------+
	| return address     |  <- pushed by CALL
	+--------------------+
	| callee's frame ... |  <- ESP points down here
	+--------------------+
	lower addresses
```

Right-to-left pushing is not an arbitrary choice — it is what makes
varargs possible at all. Pushed this way, the *first* argument always
sits at a fixed, known offset just above the return address, no matter
how many arguments follow it. Push left-to-right instead and `fmt`'s
location would depend on the number of varargs — which the callee has no
way to know. So on i386:

- `va_list` is literally a `char *`.
- `va_start(ap, fmt)` computes "the address just past `fmt`" — point at
  the `0x0000002a` slot.
- `va_arg(ap, int)` reads 4 bytes at `ap` and bumps `ap` by 4. Each
  fetch walks one slot *up* the stack, through arguments in left-to-
  right order. The promotions guarantee each slot is at least 4 bytes,
  so the walk never has to think about sub-word arguments.

This tidy pointer story is also why `va_list` must stay an opaque type
in portable code: on x86-64, the first six integer arguments travel in
registers, not on the stack, and `va_list` becomes a small struct
tracking a register-save area and an overflow area. Same four macros,
wildly different machinery. Write to the macros and both worlds work.

## Digits from repeated division

There is no `itoa` waiting for us, so: how do you turn the `int` 4096
into the characters `'4' '0' '9' '6'`? The classic answer is repeated
division by ten. Each `% 10` strips off the *last* digit; each `/ 10`
shifts the rest down:

```
4096 % 10 = 6     4096 / 10 = 409
 409 % 10 = 9      409 / 10 = 40
  40 % 10 = 0       40 / 10 = 4
   4 % 10 = 4        4 / 10 = 0     -> stop
```

The digits come out **backwards** — 6, 9, 0, 4. Fighting that (by
computing the digit count first, or dividing by descending powers of
ten) costs more than embracing it: write the digits into a small local
buffer back to front, then emit the buffer from where you stopped. A
32-bit value needs at most 10 decimal digits (4294967295), so a 10-byte
scratch buffer on the stack always suffices:

```c
char tmp[10];
int len = 0;

do {
	tmp[len++] = '0' + (val % 10);
	val /= 10;
} while (val != 0);

while (len > 0)
	emit(tmp[--len]);        /* emits 4, 0, 9, 6 -- forward again */
```

Use `do`/`while`, not `while`: the value 0 has no digits by the "divide
until nothing is left" rule, but it had better print as `"0"`, and the
do-loop's guaranteed first pass gives you that digit for free.

### The INT_MIN trap

Negative numbers look easy — emit a `'-'`, then format the absolute
value — and the obvious code has a bug that ships:

```c
if (v < 0) {
	emit('-');
	v = -v;         /* BUG: undefined behavior for one value of v */
}
```

Two's complement is asymmetric: a 32-bit `int` runs from -2147483648 to
+2147483647. The magnitude of `INT_MIN` does not fit in an `int`, so
`-v` overflows — and signed overflow in C is undefined behavior, not
"wraps around". Real compilers really do exploit this: an optimizer may
assume `-v` is positive afterwards and miscompile your comparisons. The
fix is to do the negation in *unsigned* arithmetic, where wraparound is
defined as modulo 2^32:

```c
unsigned int m = (unsigned int)v;
if (v < 0)
	m = 0u - m;     /* defined: unsigned arithmetic wraps */
```

Walk it for INT_MIN: `(unsigned int)v` is 0x80000000 = 2147483648, and
`0u - 0x80000000` wraps right back to 0x80000000 — which is exactly the
magnitude we wanted, 2147483648, now happily representable because it is
unsigned. The digit loop then runs on `m`. Every serious printf
implementation contains this exact maneuver; every naive one prints
garbage (or crashes under `-ftrapv`) for one value out of four billion,
which is precisely the kind of bug that lives for years.

### Hex is just base 16

`%x` needs no new ideas — divide by 16 instead of 10. What changes is
that digits past 9 need letters, and the clean trick is a lookup string:

```c
tmp[len++] = "0123456789abcdef"[val % 16];
```

Because 16 is 2^4, each hex digit is exactly one **nibble** — four bits
— of the value, which is why hex is the native tongue of hardware dumps
(one byte = exactly two hex digits, no remainder). Watch 0xdeadbeef
fall apart, low nibble first, exactly like the decimal loop:

```
0xdeadbeef % 16 = 0xf   ->  'f'
0x0deadbee % 16 = 0xe   ->  'e'
0x00deadbe % 16 = 0xe   ->  'e'
0x000deadb % 16 = 0xb   ->  'b'
...and so on: d, a, e, d -- emitted in reverse: "deadbeef"
```

One loop, parameterized by base, handles `%d`, `%u`, and `%x`. Treat
the value as `unsigned int` throughout and `%u`/`%x` need no special
cases at all; `%d` is the same loop after the sign maneuver above.

## The bounded-buffer contract

Formatting has to land in a buffer, and buffers have sizes. The
original `sprintf` ignored that fact — it writes as many bytes as the
format produces, period — and thereby funded several decades of security
research: one `%s` fed a longer string than the author imagined, and the
write runs off the end of the buffer into whatever lies beyond. In a
kernel that is somebody's stack frame or a neighboring data structure,
and kernel stacks are *small* (Linux historically gave each thread 8KB,
total, for its entire kernel-side call chain). The fix, standardized in
C99 after years of vendor improvisation, is `snprintf`/`vsnprintf`, and
its contract is worth stating precisely because you are about to
implement it:

1. **Never write more than `size` bytes** to the destination, the
   terminating NUL included.
2. **If `size > 0`, the result is always NUL-terminated** — even when
   the output had to be cut short.
3. **Return the length the *complete* output would have had** (not
   counting the NUL), whether or not it was truncated.

Watch the contract operate. An 8-byte buffer, a 10-character output:

```
ksnprintf(buf, 8, "pid=%d!", 12345)

index:         0  1  2  3  4  5  6  7  8  9
full output:   p  i  d  =  1  2  3  4  5  !      (10 chars)
buf[8]:        p  i  d  =  1  2  3  \0           (7 chars, NUL at size-1)
return value:  10
```

Clause 3 is the stroke of genius, and it is worth dwelling on why the
committee chose "the length that *would* have been written" over the
obvious alternatives ("bytes actually written", or "-1 on truncation",
both of which real pre-C99 implementations shipped — glibc before 2.1
returned -1, and Windows' `_snprintf` to this day may return a negative
and leave the buffer unterminated). The would-be length is the only
return value that *composes*:

- **Truncation detection is one comparison.** The caller writes
  `if (n >= (int)sizeof buf)` — the full output didn't fit, and the
  caller even knows by how much. A "-1 on truncation" convention tells
  you only that it didn't fit; "bytes written" can't distinguish "fit
  exactly" from "truncated at the brim".
- **Sizing is a dry run.** Call with `size == 0` and, by clauses 1 and
  2, nothing is written at all — `dst` may even be NULL — but clause 3
  still returns the full length. Measure first, allocate `n + 1`
  bytes, format again into a perfectly sized buffer. (DuckOS can't pull
  that trick yet — dynamic allocation arrives with *The Kernel Heap* —
  but the console messages we format into fixed stack buffers lose
  nothing: for diagnostics, a truncated line beats a corrupted stack
  every single time.)

One subtlety of clause 1 + 2 combined: the formatter must keep
*counting* characters it no longer has room to *store*. The clean shape
is a little output sink that stores a character only if it fits, but
increments its running position unconditionally — after the loop, the
position IS the would-be length, and `min(pos, size-1)` is where the NUL
belongs. The challenge starter hands you exactly that helper.

Every character funnels through the same exit — the amber-bordered step
runs unconditionally; the dashed box is the store that stops happening
once the buffer is full:

```d2
direction: right

in: "out_char(o, c)" { shape: oval }
q: "pos + 1\n< size ?" { shape: diamond }
store: "dst[pos] = c"
skip: "store nothing\n(no room)" { style.stroke-dash: 4 }
inc: "pos++\n(always)" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}

in -> q
q -> store: "fits"
q -> skip: "doesn't"
store -> inc
skip -> inc
```

## kprintf, assembled

With the formatter in hand, DuckOS's actual kprintf is four lines of
glue — format into a stack buffer, hand the result to the console layer
from *A Screen to Print On*:

```c
void kprintf(const char *fmt, ...)
{
	char buf[256];
	va_list ap;

	va_start(ap, fmt);
	kvsnprintf(buf, sizeof buf, fmt, ap);
	va_end(ap);
	console_puts(buf);
}
```

Nothing in this lesson is simulated: formatting is pure computation, and
this is byte-for-byte the code that runs in ring 0 — the only hardware
anywhere near it is the VGA text buffer that `console_puts` writes to.
Which is exactly why the tests below can grade the formatter without a
screen: they read the string out of the buffer instead.

## Challenge: kvsnprintf {#kvsnprintf points=25}

Implement `kvsnprintf`, the bounded formatting engine at the bottom of
DuckOS's kprintf. The varargs-to-`va_list` wrapper `ksnprintf` is
already written for you in the starter (it is the standard four-line
`va_start`/`va_end` dance) — your work is the engine it calls.

Contract:

- **Conversions:** `%c`, `%s`, `%d`, `%u`, `%x` (lowercase hex), and
  `%%` for a literal percent sign. No floating point, no `%p`, no
  length modifiers. Fetch `%c` with `va_arg(ap, int)` — remember the
  default promotions.
- **Field width** (for `%d`, `%u`, `%x`, `%s` only): an optional
  leading-zero flag followed by optional decimal digits between the `%`
  and the conversion, e.g. `%5d`, `%08x`, `%4s`. Output shorter than
  the width is right-justified and padded on the left with spaces — or
  with zeros when the `0` flag is present, but zero-padding applies to
  the numeric conversions only (`%04s` pads with spaces anyway). For a
  negative number the minus sign goes *before* zero padding (`%05d` of
  -42 is `-0042`) but *after* space padding (`%5d` of -42 is `  -42`),
  and the sign counts toward the width. A width smaller than the
  natural output is ignored (the value prints in full, never
  truncated). No `*`, no precision, no left-justify.
- **NULL strings:** `%s` given a NULL pointer prints `(null)`, padded
  like any other string.
- **Unknown conversions:** `%q` (or any character this contract doesn't
  list) outputs the `%` and that character literally, and consumes no
  argument.
- **Bounds (vsnprintf semantics):** never store more than `size` bytes
  including the terminating NUL; if `size > 0` the result is always
  NUL-terminated; if `size == 0` nothing is written at all.
- **Return value:** the length the complete output would have had, not
  counting the NUL — even when truncated, even when `size == 0`.
- Freestanding rules: no libc calls, no `<stdio.h>` — just loops, the
  provided `out_char` helper, and a small digit buffer on the stack.

The whole walk is a five-state machine — the amber-bordered LITERAL
state is home base, where every conversion returns:

```d2
direction: right

lit: "LITERAL" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}
flag: "FLAG"
wdt: "WIDTH"
conv: "CONVERT\nc s d u x %"
unk: "UNKNOWN\nemit '%' + c"

lit -> lit: "c != '%':\nout_char(c)"
lit -> flag: "'%'"
flag -> wdt: "'0' seen,\nor no flag"
wdt -> wdt: "digit:\nw = w*10 + d"
wdt -> conv: "known\nconversion"
wdt -> unk: "anything\nelse"
conv -> lit: "emit, resume"
unk -> lit
```

The tests drive everything through `ksnprintf` and `strcmp`: plain
strings, each conversion, INT_MIN, `%x` of 0xdeadbeef, space and zero
padding, width-too-small, `%%`, NULL `%s`, `%q` passthrough, truncation
into an 8-byte buffer (return value 10, buffer holds 7 chars + NUL), and
`size == 0` (nothing stored, full length returned). Every test checks
the return value as well as the bytes.

### Starter

```c
#include <stdarg.h>
#include <stddef.h>

/*
 * A bounded output sink. pos counts every character the full output
 * would contain; characters beyond the buffer are counted but not
 * stored. After formatting, pos is exactly the return value the
 * vsnprintf contract demands.
 */
struct out {
	char *dst;	/* destination buffer (possibly too small)      */
	size_t size;	/* capacity of dst, including room for the NUL  */
	size_t pos;	/* chars of full output produced so far         */
};

/*
 * Emit one character: store it only if it fits (always leaving room
 * for the terminating NUL), but count it unconditionally. Safe for
 * size == 0 (stores nothing, still counts).
 */
static inline void out_char(struct out *o, char c)
{
	if (o->pos + 1 < o->size)
		o->dst[o->pos] = c;
	o->pos++;
}

/*
 * kvsnprintf -- format fmt and its va_list arguments into dst,
 * storing at most size bytes including the terminating NUL.
 *
 * Conversions: %c %s %d %u %x %%, with an optional field width and
 * leading-zero flag on %d %u %x %s (zero flag pads numerics only).
 * %s of NULL prints "(null)". Unknown conversions echo the '%' and
 * the character. Returns the length of the complete output (NUL not
 * counted) even when truncated; if size > 0 dst is always
 * NUL-terminated; if size == 0 nothing is stored.
 */
int kvsnprintf(char *dst, size_t size, const char *fmt, va_list ap)
{
	struct out o = { dst, size, 0 };

	/*
	 * TODO: walk fmt. Literal characters go straight to
	 * out_char(&o, c). On '%': parse an optional '0' flag, then
	 * decimal width digits, then dispatch on the conversion
	 * character. Format integers with the repeated-division loop
	 * into a small char buffer (10 bytes covers 32 bits in
	 * decimal), emitting padding, then sign, then digits -- a
	 * helper like
	 *
	 *	static void out_num(struct out *o, unsigned int val,
	 *	                    unsigned int base, int negative,
	 *	                    int width, int zero);
	 *
	 * keeps %d, %u and %x on one code path. Handle %d's sign via
	 * an unsigned magnitude (mind INT_MIN). Finish by planting
	 * the NUL (if size > 0) and returning the would-be length.
	 */
	(void)fmt;
	(void)ap;

	if (size > 0)
		dst[0] = '\0';
	return (int)o.pos;
}

/* Convenience varargs wrapper -- already complete, leave as is. */
int ksnprintf(char *dst, size_t size, const char *fmt, ...)
{
	va_list ap;
	int n;

	va_start(ap, fmt);
	n = kvsnprintf(dst, size, fmt, ap);
	va_end(ap);
	return n;
}
```

### Tests

```c
#include <limits.h>
#include <stdarg.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>

int kvsnprintf(char *dst, size_t size, const char *fmt, va_list ap);
int ksnprintf(char *dst, size_t size, const char *fmt, ...);

static int failed;

static void check(int ok, const char *name)
{
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void)
{
	char buf[64];
	char small[8];
	char tiny[4] = "XYZ";
	int n;

	n = ksnprintf(buf, sizeof buf, "hello from ring 0");
	check(n == 17 && strcmp(buf, "hello from ring 0") == 0,
	      "test_plain_string");

	n = ksnprintf(buf, sizeof buf, "%c%c%c%c", 'd', 'u', 'c', 'k');
	check(n == 4 && strcmp(buf, "duck") == 0, "test_char_conversion");

	n = ksnprintf(buf, sizeof buf, "hello, %s!", "world");
	check(n == 13 && strcmp(buf, "hello, world!") == 0,
	      "test_string_conversion");

	n = ksnprintf(buf, sizeof buf, "%d ticks", 100);
	check(n == 9 && strcmp(buf, "100 ticks") == 0,
	      "test_decimal_conversion");

	n = ksnprintf(buf, sizeof buf, "%d", -42);
	check(n == 3 && strcmp(buf, "-42") == 0, "test_negative_decimal");

	n = ksnprintf(buf, sizeof buf, "%d", 0);
	check(n == 1 && strcmp(buf, "0") == 0, "test_decimal_zero");

	n = ksnprintf(buf, sizeof buf, "%u", 4294967295u);
	check(n == 10 && strcmp(buf, "4294967295") == 0,
	      "test_unsigned_conversion");

	n = ksnprintf(buf, sizeof buf, "%x", 0xdeadbeefu);
	check(n == 8 && strcmp(buf, "deadbeef") == 0,
	      "test_hex_conversion");

	n = ksnprintf(buf, sizeof buf, "%d", INT_MIN);
	check(n == 11 && strcmp(buf, "-2147483648") == 0, "test_int_min");

	n = ksnprintf(buf, sizeof buf, "100%% duck");
	check(n == 9 && strcmp(buf, "100% duck") == 0,
	      "test_percent_literal");

	n = ksnprintf(buf, sizeof buf, "%s", (char *)NULL);
	check(n == 6 && strcmp(buf, "(null)") == 0, "test_null_string");

	n = ksnprintf(buf, sizeof buf, "%5d", 42);
	check(n == 5 && strcmp(buf, "   42") == 0,
	      "test_width_space_padded");

	n = ksnprintf(buf, sizeof buf, "%05d", 42);
	check(n == 5 && strcmp(buf, "00042") == 0,
	      "test_width_zero_padded");

	n = ksnprintf(buf, sizeof buf, "%08x", 0xbeefu);
	check(n == 8 && strcmp(buf, "0000beef") == 0, "test_width_hex");

	n = ksnprintf(buf, sizeof buf, "%05d", -42);
	check(n == 5 && strcmp(buf, "-0042") == 0,
	      "test_width_sign_before_zeros");

	n = ksnprintf(buf, sizeof buf, "%6s", "ok");
	check(n == 6 && strcmp(buf, "    ok") == 0, "test_width_string");

	n = ksnprintf(buf, sizeof buf, "%2d", 12345);
	check(n == 5 && strcmp(buf, "12345") == 0,
	      "test_width_too_small_ignored");

	n = ksnprintf(small, sizeof small, "pid=%d!", 12345);
	check(n == 10, "test_truncation_returns_full_length");
	check(strcmp(small, "pid=123") == 0,
	      "test_truncation_nul_terminated");

	n = ksnprintf(tiny, 0, "%d", 1234);
	check(n == 4, "test_size_zero_returns_length");
	check(strcmp(tiny, "XYZ") == 0, "test_size_zero_writes_nothing");

	n = ksnprintf(buf, sizeof buf, "%q");
	check(n == 2 && strcmp(buf, "%q") == 0,
	      "test_unknown_conversion_passes_through");

	n = ksnprintf(buf, sizeof buf, "[%05x] %s: %d%%", 0x2a, "load", 87);
	check(n == 17 && strcmp(buf, "[0002a] load: 87%") == 0,
	      "test_mixed_format");

	return failed;
}
```

# Lesson: Segments and Privilege {#segmentation}

DuckOS can print now — *kprintf* gave us a voice. But everything we have
written so far runs with the processor wide open: any code can read any
byte, write any byte, jump anywhere. Before we take interrupts or run a
user program, we need the machine's permission system, and on 32-bit x86
that story starts, whether we like it or not, with **segmentation** —
the architecture's original protection scheme, which every modern OS
spends a page of code politely switching off. "Switching it off" means
programming it, so let's dig through the archaeological layer.

## From segment*16 to descriptor tables

Recall real mode from *The Machine Wakes Up*: an 8086 address is
`segment * 16 + offset`, two 16-bit registers smashed together to reach
one megabyte. There is no protection in that scheme at all — a segment
register is just a number you multiply, and any program can load any
value into DS and scribble over the interrupt vector table or its
neighbor's stack. Survivable on a single-tasking DOS box; disqualifying
for the multi-user systems Intel wanted to sell into.

The 80286 (1982) kept the *registers* but changed their *meaning*. In
the 286's new **protected mode**, the value in a segment register is no
longer a number to multiply — it is a **selector**: an index into a
table of **segment descriptors** that the OS builds in memory. Each
descriptor says where a segment starts (its **base**), how big it is
(its **limit**), what it is for (code? data? writable?), and — the new
trick — what **privilege** a program needs to touch it. Load a selector
and the CPU looks up the descriptor, checks your privilege, and from
then on range-checks every access against the limit. Wander past the end
of your segment and you don't corrupt your neighbor: you take a fault.

The 286's version was still 16-bit: 24-bit bases, 64 KiB limits, and —
infamously — no architected way back to real mode short of resetting the
CPU (IBM's AT wired a keyboard-controller pin to the reset line to work
around it, cousin to the A20 hack from the boot lesson). The 80386
(1985) widened everything: 32-bit bases, 20-bit limits with a
granularity trick we'll meet shortly, full 32-bit offsets. It also added
paging — which turned out to be the protection everybody actually wanted.

## Why every OS flattens it — and why you still build a GDT

Segmentation solves real problems awkwardly. A "full" pointer is a
selector *plus* an offset, loading segment registers is slow, and C —
the language kernels are written in — wants one flat address space where
a `char *` is an integer in disguise. Paging (see *Paging and Virtual
Memory*) gives protection **and** a flat space **and** per-page control,
so every mainstream 32-bit OS — Linux, Windows NT, BSD, Minix on the
386 — adopted the **flat model**:

- Every segment has base `0x00000000` and limit `0xFFFFF` with 4 KiB
  granularity — i.e. the entire 4 GiB address space.
- Segmentation therefore translates every address to itself. It still
  *runs* on every memory access; it just becomes a no-op.
- All real protection is delegated to paging — except one thing:
  **privilege level lives in the segment machinery**. The CPU's notion
  of "am I the kernel right now?" is literally the bottom two bits of CS.

And here is why this lesson is mandatory rather than optional: **you
cannot decline to play**. The x86 has no "no segmentation please" bit —
protected mode *requires* valid descriptors in a **Global Descriptor
Table** (GDT) before you can load a single segment register. The flat
model is not the absence of segmentation; it is a carefully constructed
GDT whose entries say "this segment is everything." DuckOS needs five:

```
index 0   the null descriptor      (mandatory, all zeros)
index 1   kernel code   ring 0     base 0, limit 4 GiB
index 2   kernel data   ring 0     base 0, limit 4 GiB
index 3   user code     ring 3     base 0, limit 4 GiB
index 4   user data     ring 3     base 0, limit 4 GiB
```

Entry 0 can never be used: the CPU reserves selector 0 as the **null
selector**. You may *load* it into a data segment register, but any
attempt to touch memory through it faults — so a forgotten, zeroed
segment register fails loudly instead of silently addressing something.

## The descriptor, in full gore

Each GDT entry is 8 bytes. Designed fresh in 1985 it would be two clean
dwords: a 32-bit base, a 32-bit limit-and-flags word. Instead, the 386
had to stay compatible with the 286, whose descriptor was already 8
bytes — only the first 6 meaningful, the last word documented as
"reserved, set to zero." The 386 kept every 286 field exactly where it
was and *bolted its new bits into the reserved word*. The result is the
most gleefully scrambled data structure in this course:

```
byte 0   limit, bits 0:7    \
byte 1   limit, bits 8:15   |
byte 2   base,  bits 0:7    |  the original 286 fields,
byte 3   base,  bits 8:15   |  frozen in place forever
byte 4   base,  bits 16:23  |
byte 5   access byte        /
byte 6   [ flags nibble ][ limit bits 16:19 ]   <- 386 additions,
byte 7   base, bits 24:31                       <- in the 286's
                                                   "reserved" word
```

A 32-bit base is stored in three pieces (bytes 2, 3, 4, then byte 7); a
20-bit limit in two (bytes 0–1, then the *low nibble* of byte 6). Nobody
would design this. Everybody has to encode it. The two interesting bytes
deserve bit-by-bit treatment.

**Byte 5, the access byte** — the segment's personality:

```
  bit 7   6   5   4   3   2    1   0
     +---+-------+---+---+----+----+---+
     | P |  DPL  | S | E | DC | RW | A |
     +---+-------+---+---+----+----+---+
```

- `P` — **present**. 0 means "no such segment"; any use faults.
  DuckOS always sets 1.
- `DPL` — **descriptor privilege level**, 0–3. The ring a program must
  effectively be in to use this segment — where "kernel only" is spelled.
- `S` — descriptor type. 1 = ordinary code/data segment. 0 = a "system"
  descriptor (task-state segments, and the gates we'll meet in
  *Interrupts and the IDT*).
- `E` — **executable**. 1 = code, 0 = data. The next two bits change
  meaning depending on this one:
- `DC` — for data, **direction** (1 = limit grows downward, meant for
  stacks, almost never used). For code, **conforming** (1 = callable
  from lower privilege without a ring switch — a subtle feature almost
  no OS uses). DuckOS sets 0.
- `RW` — for data, **writable** (0 = read-only). For code, **readable**
  (0 = execute-only). Code segments are *never* writable through their
  own selector.
- `A` — **accessed**. The CPU sets this bit itself the first time the
  segment is loaded — one of the few times hardware writes your data
  structures behind your back. Start it at 0.

**Byte 6, the split byte** — high nibble of flags, low nibble the limit's
top four bits:

```
  bit 7   6    5   4     3     0
     +---+-----+---+-----+ - - - - +
     | G | D/B | L | AVL |  limit  |
     +---+-----+---+-----+  16:19  +
```

- `G` — **granularity**, the star of the show; next section.
- `D/B` — default operand size. 1 = this is a 32-bit segment (32-bit
  offsets and operands); 0 = a 16-bit one. Always 1 for DuckOS.
- `L` — long mode (64-bit) code segment, an AMD retrofit from 2003.
  Always 0 in a 32-bit OS.
- `AVL` — "available": the one bit Intel left for the OS to use however
  it likes. Nobody ever agreed on a use. 0.

## Granularity: how 20 bits describe 4 GiB

The limit field is 20 bits, so it can count at most `0xFFFFF` — about a
million. A million *what* is the `G` bit's decision:

- `G = 0`: the limit counts **bytes**. Max segment: 1 MiB. (The 286
  world, preserved.)
- `G = 1`: the limit counts **4 KiB pages**: limit `0xFFFFF` covers
  offsets up to `0xFFFFFFFF` — the full 4 GiB.

So the magic flat-model incantation is base 0, limit `0xFFFFF`, `G=1` —
and notice the granule is 4096 bytes, the same `PAGE_SIZE` the paging
hardware uses. Not a coincidence: by 1985 Intel knew paging was the
future and sized the granule to match.

Time to assemble real descriptors. The four flat segments plus the null
entry, with every byte accounted for (this exact table, byte for byte,
is what DuckOS loads at boot):

```
                       access  flags  bytes 0..7 in memory
null                     --     --    00 00 00 00 00 00 00 00
kernel code  DPL0       0x9A    0xC   FF FF 00 00 00 9A CF 00
kernel data  DPL0       0x92    0xC   FF FF 00 00 00 92 CF 00
user code    DPL3       0xFA    0xC   FF FF 00 00 00 FA CF 00
user data    DPL3       0xF2    0xC   FF FF 00 00 00 F2 CF 00
```

Decode the kernel code entry by hand once — it is the rite of passage:

- Bytes 0–1 `FF FF`: limit bits 0:15 of `0xFFFFF`.
- Bytes 2–4 `00 00 00`: base bits 0:23 of zero.
- Byte 5 `0x9A` = `1001 1010`: P=1, DPL=00 (ring 0), S=1 (ordinary),
  E=1 (code), DC=0, RW=1 (readable), A=0.
- Byte 6 `0xCF`: flags `0xC` = `1100` (G=1 page-granular, D/B=1 32-bit,
  L=0, AVL=0) in the high nibble; limit bits 16:19 = `0xF` in the low.
- Byte 7 `00`: base bits 24:31.

The other three differ from it by exactly one or two access-byte bits:
clear E (`0x9A → 0x92`) and it's data; set DPL to 3 (`0x9A → 0xFA`,
`0x92 → 0xF2`) and it's usable from user mode. Four segments, one
byte of personality each.

## Rings, and why two are enough

The 386 offers four privilege levels, rings 0 through 3, 0 most
privileged. The idea descends from Multics, which had eight. The vision:
kernel in ring 0, device drivers in ring 1, system services in ring 2,
applications in ring 3 — each layer protected from the ones above.

Almost nobody ever shipped that. The reasons are practical:

- **Portability.** Most CPUs — then and now — offer exactly two modes,
  supervisor and user. A kernel designed around four rings can't be
  ported to a two-mode machine without redesign, so portable kernels
  treat x86 as if it too had only ring 0 and ring 3.
- **Paging doesn't see rings 1 and 2.** A page is either "supervisor"
  (rings 0–2) or "user" (ring 3). Put a driver in ring 1 and paging —
  the protection you actually rely on — considers it kernel anyway.

The honorable exceptions are worth a footnote: OS/2 ran I/O-privileged
code in ring 2, and Minix 2 — this course's guiding star — genuinely
used a middle ring, running its kernel tasks at privilege 1 between the
interrupt-level kernel (ring 0) and user processes (ring 3). Minix could
afford to: its whole thesis was isolating pieces of the OS from each
other. DuckOS, like Linux and nearly everyone else, uses 0 and 3 only.

Three privilege numbers now orbit every memory access, and keeping them
straight is half this lesson:

- **CPL** — *current* privilege level: the ring the CPU is in right now,
  read from the low two bits of CS.
- **DPL** — *descriptor* privilege level: the ring requirement stamped
  into a descriptor's access byte.
- **RPL** — *requested* privilege level: carried in the selector itself.
  Which brings us to selectors.

## Selectors: index, table, and a privilege stamp

A selector — the 16-bit value actually loaded into CS, DS, SS, ES — is
not a bare table index. It packs three fields:

```
  bit 15                          3    2    1  0
     +----------------------------+----+-------+
     |          index             | TI |  RPL  |
     +----------------------------+----+-------+

  raw = index << 3  |  TI << 2  |  RPL
```

- **index** — which descriptor. Descriptors are 8 bytes, so shifting the
  index left 3 makes the selector double as a byte offset into the table.
- **TI** — table indicator: 0 = the GDT, 1 = the LDT (Local Descriptor
  Table, an optional per-process table; a fossil DuckOS never uses, but
  the bit is always there).
- **RPL** — requested privilege level, bits 0–1.

Run the DuckOS table through the formula: kernel code is index 1 →
`1<<3 | 0 | 0` = `0x08`; kernel data `0x10`; user code index 3 with
RPL 3 → `3<<3 | 0 | 3` = `0x1B`; user data `0x23`. Those four constants
will follow us through the interrupt and system-call lessons.

The whole resolution, drawn for `0x1B`: solid arrows are the addressing
path; the dashed arrow is the RPL, which never touches addressing — it
feeds the privilege check we take up next.

```d2
direction: right

sel: "selector 0x1B" {
  shape: sql_table
  index: 3
  TI: "0 = GDT"
  RPL: 3
}

gdt: GDT {
  shape: sql_table
  0: "null"
  1: "kernel code"
  2: "kernel data"
  3: "user code"
  4: "user data"
}

desc: "entry 3, decoded" {
  shape: sql_table
  base: "0x00000000"
  limit: "4 GiB (G=1)"
  DPL: 3
}

sel.index -> gdt.3: "× 8 = byte 24"
gdt.3 -> desc.base
sel.RPL -> desc.DPL: "privilege check" {style.stroke-dash: 4}
```

## The privilege check, and the confused deputy

When code running at CPL loads a *data* segment register with a selector
whose descriptor has some DPL, the CPU checks:

```
max(CPL, RPL) <= DPL        otherwise: general-protection fault
```

The CPL half is intuitive — ring 3 code (CPL=3, so max ≥ 3) can never
load a DPL-0 segment; kernel data is simply not loadable from user mode.
But why fold in RPL? User code chooses its own selector bits — what
stops it from just choosing RPL=0?

Nothing — because RPL isn't *user* code's defense. It is the
**kernel's** defense against a trap called the **confused deputy**: a
privileged program tricked into using its own authority on an attacker's
behalf. Concretely: a user process asks the kernel (or, in good Minix
fashion, a driver) to "read from disk into the buffer described by
*this* selector:offset." The driver runs at CPL 0; if it naively loads
the user's selector and the user cheekily passed RPL 0, the check is
`max(0, 0) <= dpl` and a DPL-0 target sails through. The user just wrote
kernel memory using the driver's hands.

The fix: before using any selector received from a lower ring, the
kernel *stamps the requestor's privilege into it* — sets the selector's
RPL to the caller's CPL (x86 even has an instruction for exactly this,
`arpl`). Now the check is `max(0, 3) <= dpl`: the access is judged as if
the *user* had attempted it, no matter which ring actually executes the
instruction. The deputy carries the client's badge, not its own.

The check, drawn out — three inputs from three different places, one
comparison; the green border is the success path, red is the fault:

```d2
direction: right

cpl: "CPL\nring the CPU is in\n(CS bits 0:1)"
rpl: "RPL\nthe selector's stamp\n(selector bits 0:1)"
dpl: "DPL\nthe descriptor's bar\n(access byte)"

chk: "max(CPL, RPL) ≤ DPL ?"

ok: "segment loads" {
  style.stroke: "#16a34a"
  style.stroke-width: 3
}
gp: "#GP fault" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

cpl -> chk
rpl -> chk
dpl -> chk
chk -> ok: "yes"
chk -> gp: "no"
```

## Telling the CPU, and telling the tests

In a real kernel you hand the finished table to the CPU with one
instruction:

```
lgdt [gdtr]        ; gdtr: 2-byte limit (table size - 1),
                   ;       4-byte linear base address
```

`lgdt` loads the GDTR register with the table's location, and from that
instant every segment load walks *your* bytes. Get one bit wrong and the
machine triple-faults and reboots. DuckOS, as ever, takes the simulated
road: in a real kernel the byte buffer you're about to fill IS the
hardware table — `lgdt` would point at it; here we build it in a buffer
the tests can read, and the tests play the role of the CPU's
descriptor-decoding microcode. The only thing we skip is the reboot.

## Challenge: Encode a Descriptor {#gdt-encode points=15}

Write the encoder DuckOS uses to build every GDT entry: pack a base, a
limit, an access byte, and a flags nibble into the 8-byte descriptor
layout — scrambled exactly the way the 386 expects it.

The contract:

- `out[0]` = limit bits 0:7, `out[1]` = limit bits 8:15.
- `out[2]`, `out[3]`, `out[4]` = base bits 0:7, 8:15, 16:23.
- `out[5]` = the access byte, passed through untouched.
- `out[6]` = the flags nibble in the high four bits, limit bits 16:19 in
  the low four: `(flags << 4) | ((limit >> 16) & 0xF)`.
- `out[7]` = base bits 24:31.
- Only the low 20 bits of `limit` and the low 4 bits of `flags` are
  meaningful; mask off anything above them rather than letting it bleed
  into neighboring fields.

The tests build the null descriptor (all zeros in, all zeros out), the
canonical flat kernel code entry (`FF FF 00 00 00 9A CF 00` — compare
with the worked decode above), a deliberately scattered non-flat entry
(base `0x12345678`, limit `0xABCDE`) checked byte by byte, and an
access-byte pass-through. Every byte is checked against the layout
table; there is no partial credit from a decoder.

### Starter

```c
#include <stdint.h>

/*
 * Encode one 8-byte x86 segment descriptor.
 *
 *   base   32-bit segment start address
 *   limit  20-bit segment limit (bits 20+ ignored); unit depends on
 *          the G flag: bytes (G=0) or 4 KiB pages (G=1)
 *   access the access byte, stored verbatim: P DPL S E DC RW A
 *   flags  the 4-bit flags nibble (bits 4+ ignored): G D/B L AVL
 *
 * Layout (the 386's 286-compatible scramble):
 *
 *   out[0] limit 0:7      out[4] base 16:23
 *   out[1] limit 8:15     out[5] access byte
 *   out[2] base 0:7       out[6] (flags << 4) | limit 16:19
 *   out[3] base 8:15      out[7] base 24:31
 */
void gdt_encode(uint8_t out[8], uint32_t base, uint32_t limit,
                uint8_t access, uint8_t flags)
{
	/* TODO: slice base and limit into their scattered byte
	 * positions; pack flags and limit 16:19 into out[6]. */
	(void)base;
	(void)limit;
	(void)access;
	(void)flags;
	for (int i = 0; i < 8; i++)
		out[i] = 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

void gdt_encode(uint8_t out[8], uint32_t base, uint32_t limit,
                uint8_t access, uint8_t flags);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static void poison(uint8_t out[8]) {
	memset(out, 0x5A, 8);
}

int main(void) {
	uint8_t out[8];

	/* Null descriptor: all-zero inputs must give all-zero bytes. */
	poison(out);
	gdt_encode(out, 0, 0, 0, 0);
	{
		const uint8_t want[8] = {0, 0, 0, 0, 0, 0, 0, 0};
		check(memcmp(out, want, 8) == 0, "test_null_descriptor");
	}

	/* Flat kernel code: base 0, limit 0xFFFFF, 0x9A, flags 0xC. */
	poison(out);
	gdt_encode(out, 0x00000000u, 0xFFFFFu, 0x9A, 0xC);
	{
		const uint8_t want[8] =
			{0xFF, 0xFF, 0x00, 0x00, 0x00, 0x9A, 0xCF, 0x00};
		check(memcmp(out, want, 8) == 0, "test_flat_kernel_code");
	}

	/* Flat user data at DPL 3: only the access byte differs. */
	poison(out);
	gdt_encode(out, 0x00000000u, 0xFFFFFu, 0xF2, 0xC);
	{
		const uint8_t want[8] =
			{0xFF, 0xFF, 0x00, 0x00, 0x00, 0xF2, 0xCF, 0x00};
		check(memcmp(out, want, 8) == 0, "test_flat_user_data");
	}

	/* Scattered base and limit, every byte checked separately. */
	poison(out);
	gdt_encode(out, 0x12345678u, 0xABCDEu, 0x92, 0x4);
	check(out[0] == 0xDE, "test_scatter_limit_bits_0_7");
	check(out[1] == 0xBC, "test_scatter_limit_bits_8_15");
	check(out[2] == 0x78, "test_scatter_base_bits_0_7");
	check(out[3] == 0x56, "test_scatter_base_bits_8_15");
	check(out[4] == 0x34, "test_scatter_base_bits_16_23");
	check(out[5] == 0x92, "test_scatter_access_byte");
	check(out[6] == 0x4A, "test_scatter_flags_and_limit_16_19");
	check(out[7] == 0x12, "test_scatter_base_bits_24_31");

	/* The access byte is stored verbatim, whatever it is. */
	poison(out);
	gdt_encode(out, 0, 0, 0x5D, 0);
	check(out[5] == 0x5D && out[6] == 0x00,
	      "test_access_byte_passes_through");

	return failed;
}
```

Descriptors built; now the other half of the machinery — deciding who
may *use* them. This check is pure bit arithmetic on three small
numbers, which makes it a perfect simulation target: what follows is
exactly the comparison the segment-load microcode performs.

## Challenge: May This Ring Load That Segment? {#selector-check points=10}

Implement the selector field extractors and the data-segment privilege
check — the gatekeeping the CPU performs on every `mov ds, ax`.

The contract:

- `sel_index(raw)` — the descriptor index: bits 3:15 of the selector.
- `sel_rpl(raw)` — the requested privilege level: bits 0:1.
- `sel_is_ldt(raw)` — the TI bit (bit 2): 1 if the selector refers to
  an LDT, 0 for the GDT.
- `can_load_data_segment(cpl, selector_raw, descriptor_dpl)` — returns
  1 exactly when `max(cpl, rpl) <= descriptor_dpl`, where `rpl` is
  extracted from `selector_raw`; otherwise 0. `cpl` and
  `descriptor_dpl` are already plain ring numbers, 0–3.

The tests decode the DuckOS selectors (`0x08`, `0x10`, the user-code
selector `0x1B`) plus an LDT-flavored stranger (`0x2F`), then run the
privilege matrix: kernel loading kernel data, user denied kernel data,
user loading user data — and the confused-deputy case itself: CPL 0
presenting an RPL-3 selector against a DPL-0 segment, which must be
refused even though the *code* making the access is the kernel.

### Starter

```c
#include <stdint.h>

/*
 * A segment selector, as loaded into CS/DS/SS/ES:
 *
 *   bit 15                          3    2    1  0
 *      +----------------------------+----+-------+
 *      |          index             | TI |  RPL  |
 *      +----------------------------+----+-------+
 *
 *   raw = index << 3 | ti << 2 | rpl
 */
struct selector {
	uint16_t raw;
};

/* Descriptor table index: bits 3:15. */
int sel_index(uint16_t raw)
{
	/* TODO */
	(void)raw;
	return -1;
}

/* Requested privilege level: bits 0:1. */
int sel_rpl(uint16_t raw)
{
	/* TODO */
	(void)raw;
	return -1;
}

/* Table indicator (bit 2): 1 = LDT, 0 = GDT. */
int sel_is_ldt(uint16_t raw)
{
	/* TODO */
	(void)raw;
	return -1;
}

/*
 * The CPU's data-segment load check: code running at ring `cpl`,
 * presenting `selector_raw`, wants a segment whose descriptor has
 * privilege `descriptor_dpl`.
 *
 * Returns 1 iff max(cpl, rpl) <= descriptor_dpl, else 0.
 */
int can_load_data_segment(int cpl, uint16_t selector_raw,
                          int descriptor_dpl)
{
	/* TODO: extract the RPL, take the weaker (numerically larger)
	 * of cpl and rpl, and compare against the descriptor's DPL. */
	(void)cpl;
	(void)selector_raw;
	(void)descriptor_dpl;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

int sel_index(uint16_t raw);
int sel_rpl(uint16_t raw);
int sel_is_ldt(uint16_t raw);
int can_load_data_segment(int cpl, uint16_t selector_raw,
                          int descriptor_dpl);

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
	/* 0x08: DuckOS kernel code — index 1, GDT, RPL 0. */
	check(sel_index(0x08) == 1, "test_kernel_code_index");
	check(sel_rpl(0x08) == 0, "test_kernel_code_rpl");
	check(sel_is_ldt(0x08) == 0, "test_kernel_code_is_gdt");

	/* 0x10: DuckOS kernel data — index 2. */
	check(sel_index(0x10) == 2, "test_kernel_data_index");
	check(sel_rpl(0x10) == 0, "test_kernel_data_rpl");

	/* 0x1B: DuckOS user code — index 3, GDT, RPL 3. */
	check(sel_index(0x1B) == 3, "test_user_code_index");
	check(sel_rpl(0x1B) == 3, "test_user_code_rpl");
	check(sel_is_ldt(0x1B) == 0, "test_user_code_is_gdt");

	/* 0x2F = 0b101111: index 5, TI set, RPL 3. */
	check(sel_index(0x2F) == 5, "test_ldt_selector_index");
	check(sel_rpl(0x2F) == 3, "test_ldt_selector_rpl");
	check(sel_is_ldt(0x2F) == 1, "test_ldt_selector_is_ldt");

	/* Kernel (CPL 0, RPL 0) loading kernel data (DPL 0): allowed. */
	check(can_load_data_segment(0, 0x10, 0) == 1,
	      "test_kernel_loads_kernel_data");

	/* User (CPL 3) loading kernel data (DPL 0): denied — even if
	 * the selector cheekily claims RPL 0. */
	check(can_load_data_segment(3, 0x10, 0) == 0,
	      "test_user_denied_kernel_data");

	/* CPL 0 presenting an RPL-3 selector against DPL 0: denied.
	 * The kernel is acting on a user's behalf and stamped the
	 * user's privilege into the selector — the check must judge
	 * the access as if the user made it. */
	check(can_load_data_segment(0, 0x13, 0) == 0,
	      "test_confused_deputy_rpl3_denied");

	/* User (CPL 3, RPL 3) loading user data (DPL 3): allowed. */
	check(can_load_data_segment(3, 0x23, 3) == 1,
	      "test_user_loads_user_data");

	/* Kernel touching user-visible data (DPL 3): allowed —
	 * higher privilege may always use less-privileged segments. */
	check(can_load_data_segment(0, 0x23, 3) == 1,
	      "test_kernel_loads_user_data");

	return failed;
}
```

With the GDT built and the ring rules enforced, DuckOS finally has a
notion of *kernel* versus *user* that the hardware itself polices. The
selectors minted here — `0x08`, `0x10`, `0x1B`, `0x23` — are about to
become load-bearing: the very next lesson, *Interrupts and the IDT*,
stuffs `0x08` into every interrupt gate, because when the machine drops
whatever it's doing to service a timer tick, the first question the CPU
asks is "which code segment — and therefore which ring — handles this?"
Privilege, it turns out, was the prerequisite for interruption.

# Lesson: Interrupts and the IDT {#interrupts}

DuckOS can print — *A Screen to Print On* gave it a console and *kprintf*
gave it a voice. But it is deaf. A key is pressed, a disk finishes a
transfer, a timer expires — and the kernel has no idea. How does hardware
get the CPU's attention?

The obvious answer is to ask. Constantly. This is **polling**:

```c
for (;;) {
	if (inb(KBD_STATUS) & 1)	/* keyboard: got a byte for me? */
		handle_key(inb(KBD_DATA));
	if (inb(DISK_STATUS) & READY)	/* disk: finished that read? */
		handle_disk();
	/* ...and when, exactly, do we run user programs? */
}
```

Do the arithmetic on that keyboard check. A world-class typist produces
maybe 10 bytes per second. A 33 MHz 386 — the machine early Linux ran
on — executes on the order of ten million instructions in that same
second. Poll in a tight loop and 99.9999% of your questions get the
answer "no". Poll rarely, to get real work done between checks, and you
risk being late: the 8042 keyboard controller holds exactly one scancode,
and the disk won't wait forever either. Polling forces a rotten trade
between wasted work and missed events, and it gets worse with every
device you add.

The fix is to invert control: the CPU stops asking and the device starts
telling. This is the **interrupt**. The usual metaphor is a doorbell —
you don't check the porch every ten seconds; the visitor presses a
button and you go answer. Fine as far as it goes, but the metaphor hides
everything interesting: who decides which of several ringing doorbells
you answer first, how you ignore the one you're not dressed for, and how
pressing button #1 makes you walk to door #1 and not door #6. So let's
retire the doorbell and walk the real path, wire by wire.

## From Wire to Handler

Here is the journey of one keystroke on an i386 PC, end to end. The
numbers match the steps below (4a/4b are the two halves of the
handshake); the dashed edge is the EOI, which happens only after the
handler has run — it closes the loop in the second half of this lesson:

```d2
direction: right

kbd: keyboard {
  shape: oval
}

pic: "8259A PIC" {
  grid-columns: 1
  irr: "1. latch: IRR bit 1"
  pri: "2. priority resolve"
}

cpu: CPU {
  grid-columns: 1
  fin: "3. finish instruction"
  frame: "5. push EFLAGS, CS, EIP"
  idt: "6. IDT[0x21] → handler" {
    style.stroke: "#d97706"
  }
}

kbd -> pic: "IRQ 1"
pic -> cpu: "INTR"
pic -> cpu: "4b. vector 0x21" {
  style.stroke-width: 3
}
cpu -> pic: "4a. INTA x2"
cpu -> pic: "later: EOI" {
  style.stroke-dash: 4
}
```

Step by step:

1. **The device raises its IRQ line.** Every interrupt-capable device
   on the board owns one physical wire — an *interrupt request* line —
   into the interrupt controller. The keyboard controller has IRQ 1;
   the timer has IRQ 0. Raising the line is all a device can do. It
   cannot address the CPU, cannot say *why* it's interrupting, cannot
   jump anywhere. One wire, one bit of information: "something happened
   over here."

2. **The PIC prioritizes.** The lines converge on the 8259A
   **Programmable Interrupt Controller**, which latches each request
   and decides — when several devices shout at once — who is heard
   first. (The whole second half of this lesson is about this chip; for
   now, treat it as a fair but opinionated receptionist.) Having picked
   a winner, the PIC raises the single **INTR** pin on the CPU.

3. **The CPU finishes its current instruction.** INTR is checked at
   instruction boundaries, never mid-instruction. This makes x86
   interrupts *precise*: the interrupted program is frozen at a clean
   point where every architecturally visible register is consistent,
   which is exactly what makes it possible to resume it later as if
   nothing happened.

4. **The handshake.** The CPU answers with two pulses on the **INTA**
   (interrupt acknowledge) line. On the second pulse, the PIC places
   one byte on the data bus: the **vector number**, which of the 256
   possible interrupt causes this is. Internally the PIC also moves the
   request from "pending" to "being serviced" — bookkeeping you will
   implement yourself in this lesson's second challenge.

5. **The CPU saves the interrupted context.** It pushes three things
   onto the stack: **EFLAGS** (the flags register, including the
   interrupt-enable flag), **CS**, and **EIP** — the exact place to
   resume. For a handful of CPU exceptions (#GP and #PF among them, but
   not #DE or #BP, and never for hardware IRQs) it additionally pushes
   an **error code** describing the fault. The `iret` instruction is
   the mirror image: it pops EIP, CS, and EFLAGS and the interrupted
   code continues, oblivious. (If the interrupt arrives while the CPU
   is running ring-3 code, there's an extra wrinkle — a stack switch,
   with SS and ESP pushed too. That story belongs to *The System Call
   Boundary*.)

6. **The CPU vectors through the IDT.** The vector byte from step 4 is
   an index into the **Interrupt Descriptor Table**, an array of up to
   256 eight-byte **gate descriptors** in memory. The CPU multiplies
   the vector by 8, reads the gate, and far-jumps to the
   selector:offset it finds there. Your handler is now running.

The frame from step 5, as your handler finds it (the x86 stack grows
toward lower addresses, so the last push lands lowest):

```
        higher addresses
      +-----------------+
      |     EFLAGS      |   pushed first
      +-----------------+
      |       CS        |
      +-----------------+
      |       EIP       |   pushed last <- ESP on handler entry
      +-----------------+
        lower addresses      iret pops EIP, CS, EFLAGS: exact mirror
```

Notice the division of labor. The device knows only how to pull a wire.
The PIC knows priorities and pending requests, but nothing about
handlers. The CPU knows how to save state and index a table, but nothing
about devices. And the OS owns the table — which is the whole trick: by
filling in the IDT, the kernel decides what every possible interrupt
*means*.

## 256 Doors: the Vector Space

The vector is one byte, so there are 256 doors, numbered 0–255. Intel
reserved the first 32 — vectors 0 through 31 — for **CPU exceptions**:
interrupts the processor raises about its own execution, no external
device involved. The famous residents:

```
vector  name  cause
     0   #DE  divide error        (int x = 1/0;)
     3   #BP  breakpoint          (the INT3 instruction, 1 byte: 0xCC —
                                   debuggers patch it over your code)
     6   #UD  invalid opcode      (executed garbage)
     8   #DF  double fault        (an exception while delivering an
                                   exception; the "abandon ship" vector)
    13   #GP  general protection  (violated segment limits or privilege
                                   — *Segments and Privilege*'s enforcer)
    14   #PF  page fault          (touched an unmapped page; this one
                                   returns as the hero of *Paging and
                                   Virtual Memory*)
```

Vectors 32–255 are yours: the OS assigns them to hardware IRQs and
software-invoked services as it pleases.

Except that in 1981, IBM didn't. The 8086 manual said vectors 0–31 were
"reserved for future use", but on the 8086/8088 only 0–4 actually did
anything, and the IBM PC's designers — pressed for time and, per the
lore, unconvinced Intel would ever use the rest — programmed the PIC to
deliver IRQs 0–7 at vectors **8–15**. It worked fine, briefly. Then the
286 and 386 moved into the reserved space, and the future arrived:

```
IRQ 0  timer      -> vector  8   =  #DF double fault (286+)
IRQ 5  hard disk  -> vector 13   =  #GP general protection (286+)
IRQ 6  floppy     -> vector 14   =  #PF page fault (386+)
```

In protected mode, a kernel using IBM's mapping literally cannot tell
"the clock ticked" from "the CPU is failing so hard it's about to
reset". This is not a theoretical ambiguity — early protected-mode
systems had to write heuristic handlers that guessed which one they got.
The permanent fix is simpler: the 8259A is *programmable*, and its
initialization sequence lets you choose the base vector. So the first
thing every protected-mode OS does to the PIC during boot — Minix,
Linux, DuckOS — is **remap** it: IRQs 0–7 to vectors 32–39, IRQs 8–15
to vectors 40–47, clear of the reserved zone forever. The keyboard's
IRQ 1 becomes vector 33 (0x21) — the number in the diagram above.

## Eight Bytes per Door: the Gate Descriptor

Each IDT entry is an 8-byte **gate descriptor** answering three
questions: *where* is the handler (a segment selector plus a 32-bit
offset), *who* may invoke this vector with a software `int` instruction
(a privilege level), and *how* should the CPU enter it (the gate type).
The bytes are laid out like this:

```
bytes 0-1   offset bits 0:15    low half of the handler address
bytes 2-3   segment selector    which code segment (0x08: kernel code,
                                as encoded in *Segments and Privilege*)
byte  4     always zero         reserved
byte  5     type/attr           P, DPL, gate type (below)
bytes 6-7   offset bits 16:31   high half of the handler address
```

Yes: the 32-bit handler offset is split, low half at the front and high
half at the back, with the selector and attributes wedged in between.
This is the same scar tissue you saw on the GDT descriptor in *Segments
and Privilege*. The 80286's gates were 8 bytes with a 16-bit offset in
bytes 0–1 and bytes 6–7 unused; when the 386 widened offsets to 32
bits, the only compatible place for the new high bits was that unused
tail. Every 32-bit x86 OS ever since has assembled its handler
addresses in two pieces.

Byte 5, the **type/attr byte**, packs three fields:

```
  bit 7    bits 6:5    bit 4    bits 3:0
+--------+-----------+--------+----------+
|   P    |    DPL    |   0    |   type   |
+--------+-----------+--------+----------+
```

- **P** — present. 1 means the gate is valid; the CPU raises a fault on
  any vector whose gate has P=0. (Compare the GDT's present bit.)
- **DPL** — descriptor privilege level, 0–3. Checked *only* when the
  vector is invoked by a software `int n` instruction: the CPU compares
  CPL against DPL and raises #GP if the caller is less privileged.
  Hardware interrupts and CPU exceptions ignore it. This asymmetry is a
  security feature you'll meet again in *The System Call Boundary*:
  the syscall gate gets DPL 3 so ring-3 code may `int 0x80` into the
  kernel, while every other gate gets DPL 0 — so a user program that
  tries `int 14` to forge a page fault (with no error code on the
  stack, which would desynchronize the handler's view of the stack and
  corrupt the kernel) gets #GP instead.
- **type** — for our purposes, two values:

```
0xE   32-bit interrupt gate   CPU clears IF on entry
0xF   32-bit trap gate        CPU leaves IF alone
```

That one-bit difference — what happens to **IF**, the interrupt-enable
flag in EFLAGS — is the whole distinction. Through an *interrupt gate*,
the CPU clears IF after pushing EFLAGS, so your handler starts with
further maskable interrupts disabled; `iret` restores the pushed EFLAGS
and re-enables them automatically. Through a *trap gate*, IF stays as
it was and other interrupts can nest on top of you immediately.

When do you want each? Hardware IRQ handlers get **interrupt gates**:
they run for microseconds, they manipulate shared PIC and device state,
and the last thing they want is a second IRQ landing mid-handshake. The
`int 0x80` system-call gate is the classic **trap gate**: a syscall may
run for a comparatively long time, and there is no reason the clock
should stop ticking because some process asked to read a file.

Concretely — encode a gate for a timer handler at `0x00101234`, in the
kernel code segment `0x08`, ring 0, interrupt gate. The type/attr byte
is P=1, DPL=0, type 0xE: `1000_1110` = `0x8E`. The offset splits into
`0x1234` (low) and `0x0010` (high), each stored little-endian:

```
byte:      0     1     2     3     4     5     6     7
        +-----+-----+-----+-----+-----+-----+-----+-----+
value:  | 34  | 12  | 08  | 00  | 00  | 8E  | 10  | 00  |
        +-----+-----+-----+-----+-----+-----+-----+-----+
          offset low  selector   zero  attr  offset high
```

The full IDT is just 256 of these back to back — 2 KB of memory — and
the `lidt` instruction tells the CPU where it lives, exactly as `lgdt`
did for the GDT. In a real kernel the array you fill in IS the hardware
table: the CPU reads these bytes, unmediated, every time an interrupt
fires. Here, as usual for DuckOS, we build it in a buffer the tests can
read — the encoding is identical, only the consumer differs.

## The 8259A: Two Chips Pretending to Be One

Now the receptionist. The **Intel 8259A** is older than the IBM PC —
it was designed in the 8080 era — and it outlived generations of CPUs:
even on machines a decade newer, with the physical chip long since
absorbed into the southbridge, software still programs "the 8259A" as
if it were 1981. Every PC actually has two. Each 8259A handles 8 input
lines, and IBM wanted more than 8 devices, so a second (**slave**) PIC
is cascaded into input 2 of the first (**master**): when any of IRQs
8–15 fires, the slave signals the master on line 2, and the master
relays to the CPU. Two consequences worth remembering: IRQ 2 doesn't
exist as a device line (it's the cascade), and the slave's inputs
inherit the master's line-2 priority, slotting IRQs 8–15 between IRQ 1
and IRQ 3.

Sixteen lines through two chips — the red edge is the cascade: the slave delivers IRQs 8–15 through the master's line 2, inheriting its priority:

```d2
direction: right

hi: "IRQ 0, IRQ 1" {
  shape: oval
}
lo: "IRQs 3–7" {
  shape: oval
}
dev2: "IRQs 8–15" {
  shape: oval
}

slave: "slave 8259A\nvectors 40–47"

master: "master 8259A\nvectors 32–39"

cpu: CPU

hi -> master: "lines 0, 1"
dev2 -> slave
slave -> master: "cascade: line 2" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
lo -> master: "lines 3–7"
master -> cpu: INTR
```

Inside each chip, three 8-bit registers — one bit per IRQ line — carry
the entire state machine:

```
IRR  Interrupt Request Register   raised but not yet acknowledged
ISR  In-Service Register          acknowledged, handler still running
IMR  Interrupt Mask Register      lines the OS asked to ignore
```

A request's life: the device raises its line and the PIC sets the IRR
bit — *pending*. If the line is masked in IMR, it goes no further, but
note the IRR bit stays set: masking hides an interrupt, it doesn't
discard it, and unmasking later delivers the held request. When the CPU
acknowledges (the INTA handshake), the PIC picks the winner, clears its
IRR bit, sets its ISR bit — *in service* — and sends the vector.

Who wins? The 8259A's priority is **fixed**: line 0 highest, line 7
lowest. (After remapping, IRQ 0 is the timer — the system heartbeat
outranking everything is exactly right.) And the ISR gives the rule
teeth while a handler runs: in the chip's standard *fully nested mode*,
a pending request is delivered only if its priority is **strictly
higher** than every in-service line. While IRQ 3's handler runs, a new
IRQ 5 waits in IRR; a new IRQ 1 interrupts the IRQ 3 handler (if the
CPU has IF set — through an interrupt gate it won't, so in practice the
nesting resolves when the handler returns).

The PIC has no idea when a handler finishes — so the handler must tell
it. That's the **EOI** (end of interrupt): the handler writes the byte
`0x20` to the master's command port (also `0x20` — a coincidence that
has confused generations of kernel hackers, and after remapping, the
timer's vector is *also* 0x20). This non-specific EOI clears the
highest-priority ISR bit — "the most important thing I was doing is
done" — which is correct precisely because of fully nested mode: the
handler currently finishing must be the highest-priority one in
service. Forget the EOI and the ISR bit stays set forever, and the PIC
politely suppresses that line and everything below it: the classic
"my keyboard worked until the first keypress" bootstrapping bug. For
IRQs 8–15 the handler must EOI *both* chips — the slave got serviced,
and so did the master's line 2.

One piece of genuine lore: **spurious IRQ 7**. If a request line drops
*after* the PIC raised INTR but *before* the CPU's acknowledge — line
noise, or a device deasserting at the wrong moment — the PIC is stuck:
the handshake demands a vector, but there's no longer a request to
report. It answers with its lowest-priority vector, IRQ 7, *without
setting the ISR bit*. A robust IRQ 7 handler therefore reads the ISR
first; if bit 7 is clear, the interrupt was a ghost: return without
EOI (an EOI here would clear some *real* in-service bit and break an
unrelated handler mid-flight).

How does the OS program all this? Through the chips' I/O ports —
master at `0x20`/`0x21`, slave at `0xA0`/`0xA1` — with the
**initialization command word** sequence, ICW1 through ICW4. In prose:
writing ICW1 (`0x11`: "initialize; edge-triggered; cascade mode; ICW4
follows") to the command port resets the chip's state machine, which
then expects exactly three data-port writes. ICW2 is the **vector
base** — this is the remap: `0x20` (32) for the master, `0x28` (40)
for the slave. ICW3 describes the cascade wiring — the master gets a
bitmask saying "slave on line 2" (`0x04`), the slave gets its cascade
identity (`0x02`). ICW4 (`0x01`) selects 8086 mode, a flag that exists
because the chip predates the 8086 and still remembers how to serve an
8080. After initialization, writes to the data port land in the IMR —
so masking and unmasking lines is a single port write.

## The First Rule of Handlers: Get Out

A hardware interrupt handler runs on stolen time. Whatever the CPU was
doing — a user's computation, another kernel path — is frozen beneath
you, and through an interrupt gate, *every other maskable interrupt on
the machine* is frozen too. Every microsecond you spend is a
microsecond of system-wide deafness: at 100 timer ticks per second
(`HZ`, which DuckOS adopts in *The Clock Ticks*), a handler that dawdles
for 10 ms eats an entire tick; a slow keyboard handler drops scancodes
from the 8042's one-byte buffer.

So the discipline is absolute: a handler does the minimum the hardware
demands — acknowledge the device, grab the volatile byte, set a flag or
bump a counter, EOI — and gets out. And it must **never block**: it is
not a process, it has no identity the scheduler knows, so if it waits
for something, there is nothing to switch to and no one who will ever
wake it. The heavy lifting happens later, in a context that *can*
block. Bigger kernels formalize this as the **top half / bottom half**
split — the top half runs at interrupt time and does almost nothing;
the deferred bottom half does the real work. DuckOS keeps the idea and
skips the machinery: our clock handler in *The Clock Ticks* will just
increment the tick counter and wake sleepers, and the keyboard handler
of *The Keyboard* will stash a scancode and return. Interrupts tell the
kernel *that* something happened; deciding what to do about it is
someone else's job.

Time to build both halves of the machinery: first the doors, then the
receptionist.

## Challenge: Encode an IDT Gate {#idt-gate points=15}

Write the encoder DuckOS will call 256 times at boot: given a handler
address, a code-segment selector, a privilege level, and a gate type,
produce the 8 bytes of an IDT gate descriptor.

The contract, from the lesson:

- Bytes 0–1: bits 0:15 of `handler_offset`, little-endian.
- Bytes 2–3: `selector`, little-endian.
- Byte 4: zero.
- Byte 5: type/attr — P=1 always, the given `dpl` in bits 6:5, and gate
  type `0xF` (trap gate) if `trap` is nonzero, else `0xE` (interrupt
  gate).
- Bytes 6–7: bits 16:31 of `handler_offset`, little-endian.

The tests check the worked-example encoding exactly (an interrupt gate
in the kernel code segment at DPL 0), verify the type/attr byte of a
DPL-3 trap gate — that one is the `int 0x80` syscall gate, byte-for-
byte the entry Linux 0.01 planted at vector 128 — and use an asymmetric
offset (`0xABCD1234`) to prove both halves of the split land in the
right place. The tests pre-fill the output buffer with `0xAA`, so every
byte you're supposed to write, you must actually write.

### Starter

```c
#include <stdint.h>

#define GATE_INTR 0xE	/* 32-bit interrupt gate: CPU clears IF on entry */
#define GATE_TRAP 0xF	/* 32-bit trap gate: IF left alone */

/*
 * Encode one 8-byte IDT gate descriptor into out[0..7].
 *
 *   handler_offset  32-bit address of the handler, split 0:15 / 16:31
 *   selector        code-segment selector (bytes 2-3, little-endian)
 *   dpl             0..3, lowest ring allowed to `int n` this vector
 *   trap            nonzero: trap gate (0xF); zero: interrupt gate (0xE)
 *
 * Byte 4 is always zero. Byte 5 is P=1 | dpl<<5 | type.
 */
void idt_gate(uint8_t out[8], uint32_t handler_offset, uint16_t selector,
              int dpl, int trap)
{
	/* TODO: split the offset, place the selector, build the
	 * type/attr byte. Every multi-byte field is little-endian. */
	for (int i = 0; i < 8; i++)
		out[i] = 0;
	(void)handler_offset;
	(void)selector;
	(void)dpl;
	(void)trap;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

void idt_gate(uint8_t out[8], uint32_t handler_offset, uint16_t selector,
              int dpl, int trap);

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
	uint8_t out[8];

	/* Ring-0 interrupt gate, kernel code segment: the lesson's layout. */
	memset(out, 0xAA, sizeof(out));
	idt_gate(out, 0x00104567u, 0x08, 0, 0);
	{
		uint8_t want[8] = {0x67, 0x45, 0x08, 0x00, 0x00, 0x8E, 0x10, 0x00};
		check(memcmp(out, want, 8) == 0,
		      "test_interrupt_gate_ring0_exact_bytes");
	}

	/* The int 0x80 syscall gate: trap gate, DPL 3. */
	memset(out, 0xAA, sizeof(out));
	idt_gate(out, 0x00108abcu, 0x08, 3, 1);
	check(out[5] == 0xEF, "test_syscall_trap_gate_type_attr");
	check(out[2] == 0x08 && out[3] == 0x00, "test_syscall_gate_selector");
	check(out[4] == 0x00, "test_reserved_byte_zero");

	/* Asymmetric offset: both halves must land in the right place. */
	memset(out, 0xAA, sizeof(out));
	idt_gate(out, 0xABCD1234u, 0x10, 0, 0);
	check(out[0] == 0x34 && out[1] == 0x12, "test_offset_low_half");
	check(out[6] == 0xCD && out[7] == 0xAB, "test_offset_high_half");
	check(out[2] == 0x10 && out[3] == 0x00, "test_selector_little_endian");

	return failed;
}
```

## Challenge: An 8259 in Software {#pic8259 points=20}

Model one 8259A as a C struct and implement its state machine: raise,
acknowledge, EOI, mask. This is a single-chip model — the slave cascade
doubles the bookkeeping, not the ideas — so IRQ numbers run 0–7 and
each register is one byte, bit *n* for IRQ *n*.

```c
struct pic {
	uint8_t irr, isr, imr;	/* pending, in-service, masked */
};
```

The four operations, exactly as the lesson described the hardware:

- `void pic_raise(struct pic *p, int irq)` — a device pulls its line:
  set bit `irq` in IRR. Raising an already-pending line changes
  nothing — one bit is one deliverable request, however many times the
  wire wiggles.
- `int pic_next(struct pic *p)` — the INTA handshake. Find the
  highest-priority (lowest-numbered) IRQ that is pending in IRR, not
  masked in IMR, and strictly higher priority than every bit in ISR
  (fully nested mode). If one exists: clear its IRR bit, set its ISR
  bit, return its number. Otherwise return -1 — and change nothing.
- `void pic_eoi(struct pic *p)` — non-specific EOI: clear the
  highest-priority (lowest-numbered) set bit in ISR. The handler
  finishing is by construction the highest-priority one in service.
- `void pic_set_mask(struct pic *p, uint8_t mask)` — store the OCW1
  byte into IMR. Masking hides a pending request from `pic_next`, but
  the IRR bit survives: unmask later and the request delivers.

The tests walk the scenarios from the lesson: a full raise → ack → EOI
cycle (watching IRR and ISR move); IRQ 1 beating IRQ 5 to the punch;
a masked request held in IRR until unmasked; fully nested delivery
(with IRQ 3 in service, a raised IRQ 5 must wait but a raised IRQ 1
preempts); EOI unwinding the nest in priority order (clearing IRQ 1
before IRQ 3); `pic_next` returning -1 on a quiet chip; and a
double-raised line delivering exactly once.

### Starter

```c
#include <stdint.h>

/*
 * One 8259A, modeled in software. In the real chip these three
 * registers ARE the hardware state the INTA handshake reads and
 * writes; here they are bytes the tests can inspect. Bit n of each
 * register corresponds to IRQ n; IRQ 0 is the highest priority.
 */
struct pic {
	uint8_t irr, isr, imr;	/* pending, in-service, masked */
};

/* A device raised line irq (0..7): mark it pending. Idempotent. */
void pic_raise(struct pic *p, int irq)
{
	/* TODO: set bit irq in IRR */
	(void)p;
	(void)irq;
}

/*
 * The INTA handshake: deliver the best pending request, if any.
 * Eligible = pending in IRR, not masked in IMR, and strictly
 * higher priority (lower number) than every in-service bit in ISR.
 * Deliver the highest-priority eligible IRQ: clear IRR bit, set ISR
 * bit, return its number. If none is eligible, return -1 and leave
 * all registers untouched.
 */
int pic_next(struct pic *p)
{
	/* TODO */
	(void)p;
	return -1;
}

/* Non-specific EOI: clear the lowest-numbered set bit in ISR. */
void pic_eoi(struct pic *p)
{
	/* TODO */
	(void)p;
}

/* OCW1: replace the interrupt mask. Set bit = line ignored. */
void pic_set_mask(struct pic *p, uint8_t mask)
{
	/* TODO */
	(void)p;
	(void)mask;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

struct pic {
	uint8_t irr, isr, imr;	/* pending, in-service, masked */
};

void pic_raise(struct pic *p, int irq);
int pic_next(struct pic *p);
void pic_eoi(struct pic *p);
void pic_set_mask(struct pic *p, uint8_t mask);

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
	struct pic p = {0, 0, 0};

	/* A quiet chip has nothing to deliver. */
	check(pic_next(&p) == -1, "test_nothing_pending_is_minus_one");

	/* One full raise -> ack -> EOI cycle. */
	pic_raise(&p, 4);
	check(p.irr == (1u << 4), "test_raise_sets_irr_bit");
	check(pic_next(&p) == 4, "test_ack_returns_raised_irq");
	check(p.irr == 0 && p.isr == (1u << 4), "test_ack_moves_irr_to_isr");
	pic_eoi(&p);
	check(p.isr == 0, "test_eoi_clears_isr");

	/* Fixed priority: the lowest-numbered request wins. */
	pic_raise(&p, 5);
	pic_raise(&p, 1);
	check(pic_next(&p) == 1, "test_lowest_number_wins");
	pic_eoi(&p);
	check(pic_next(&p) == 5, "test_loser_delivered_after_eoi");
	pic_eoi(&p);

	/* Masked: invisible to pic_next, but held in IRR, not lost. */
	pic_set_mask(&p, 1u << 2);
	pic_raise(&p, 2);
	check(pic_next(&p) == -1, "test_masked_irq_not_delivered");
	pic_set_mask(&p, 0);
	check(pic_next(&p) == 2, "test_unmasked_irq_still_pending");
	pic_eoi(&p);

	/* Fully nested mode: only strictly higher priority preempts. */
	pic_raise(&p, 3);
	check(pic_next(&p) == 3, "test_nest_irq3_enters_service");
	pic_raise(&p, 5);
	check(pic_next(&p) == -1, "test_lower_priority_waits_in_irr");
	pic_raise(&p, 1);
	check(pic_next(&p) == 1, "test_higher_priority_preempts");

	/* ISR holds 1 and 3; non-specific EOI unwinds in priority order. */
	pic_eoi(&p);
	check(p.isr == (1u << 3), "test_eoi_clears_highest_priority_first");
	check(pic_next(&p) == -1, "test_irq5_still_blocked_by_irq3");
	pic_eoi(&p);
	check(p.isr == 0, "test_second_eoi_clears_irq3");
	check(pic_next(&p) == 5, "test_irq5_finally_delivered");
	pic_eoi(&p);

	/* One IRR bit = one delivery, however often the line is raised. */
	pic_raise(&p, 6);
	pic_raise(&p, 6);
	check(pic_next(&p) == 6, "test_double_raise_first_delivery");
	pic_eoi(&p);
	check(pic_next(&p) == -1, "test_double_raise_delivers_once");

	return failed;
}
```

# Lesson: Owning Physical Memory {#physical-memory}

Everything DuckOS has done so far ran in whatever memory the BIOS happened
to drop us into. We built a GDT, an IDT, a console — all in buffers we
declared and hoped nothing else was using. That hope is about to become a
liability. Every subsystem still to come needs memory: page tables in
*Paging and Virtual Memory*, the heap in *The Kernel Heap*, process
structures, the buffer cache. Before any of that, the kernel must take on
its first true resource-management job: know exactly what RAM exists, know
who is using every byte of it, and hand it out and take it back on demand.

This lesson does that in two moves. First we get the machine's memory map
from the firmware and beat it into a usable shape — it does not arrive in
one. Then we build the allocator that owns physical memory for the rest of
the course: a bitmap frame allocator, one bit per 4 KiB page of RAM.

## What the BIOS knows

The kernel cannot detect RAM by itself. There is no CPU instruction that
returns "you have 128 MiB"; memory is wired to the chipset, and the
chipset was configured by the firmware at power-on. The firmware is
therefore the only party that knows the truth, and the kernel's job is to
ask for it before leaving real mode, while BIOS services still work.

Asking has a history. Minix in 1987 could assume almost nothing — it ran
in as little as 256 KiB — and used `INT 0x12`, a PC-BIOS call as old as
the original IBM PC, which returns the number of contiguous KiB of
*conventional* memory starting at address 0. Above 1 MiB there was
`INT 0x15, AH=0x88`, which returns the KiB of *extended* memory as one
number in a 16-bit register — so it cannot even describe more than 64 MiB,
let alone describe holes. By the mid-90s machines had ACPI tables, memory-
mapped hardware, and reserved regions scattered through the address space,
and one number per era of history stopped being enough. Phoenix defined
the call every OS uses since: `INT 0x15` with `EAX = 0xE820` — the "E820
memory map", named after nothing deeper than the function number.

E820 is an iterator. Each call hands back one region: you point `ES:DI`
at a buffer, pass a continuation cookie in `EBX` (0 to start), and the
BIOS fills in a 20-byte entry and a new cookie; when the cookie comes back
0, the map is complete. One entry is:

```
offset  size  field
     0     8  base   physical start address (64-bit, even on a 32-bit CPU)
     8     8  length in bytes
    16     4  type   what the firmware says this range is
```

The types that matter:

```
type 1  usable         ordinary RAM, yours
type 2  reserved       hands off: ROM, memory-mapped hardware, firmware
type 3  ACPI reclaim   ACPI tables; RAM again once you've read them
type 4  ACPI NVS       firmware state that must survive sleep; never yours
type 5  bad            RAM that failed the firmware's test
```

Here is a plausible map from a 128 MiB machine (QEMU will show you nearly
this exact one):

```
 #  base                length              type
 0  0x0000000000000000  0x000000000009fc00  1  usable
 1  0x000000000009fc00  0x0000000000000400  2  reserved   (EBDA)
 2  0x00000000000f0000  0x0000000000010000  2  reserved   (BIOS ROM)
 3  0x0000000000100000  0x0000000007ee0000  1  usable
 4  0x0000000007fe0000  0x0000000000020000  3  ACPI reclaimable
 5  0x00000000fffc0000  0x0000000000040000  2  reserved   (firmware flash)
```

Read it like evidence. Entry 0 is the classic 640 KiB of conventional
memory — but it stops at 0x9FC00, not 0xA0000, because the last KiB is
the EBDA (entry 1). Entry 3 is the bulk of RAM: everything from 1 MiB up
to just under 128 MiB. Entry 5 is nowhere near RAM at all — it is the
firmware's flash chip mapped just below 4 GiB. And notice what is *not*
listed: the range 0xA0000–0xEFFFF appears in no entry. Absence is also
information. The rule for gaps is the rule for everything here: memory
you cannot prove is usable, is not usable.

## The mess below one megabyte

That unlisted hole is the oldest real estate on the PC, and it deserves a
map of its own, because your boot code lives in the middle of it:

```
0x00000000 +--------------------------------------+
           | real-mode IVT (1 KiB)                |
0x00000400 | BIOS data area (256 B)               |
0x00000500 |                                      |
           |  conventional memory (~638 KiB)      |
           |  your boot sector was loaded at      |
           |  0x7C00, right here in the middle    |
           |                                      |
0x0009FC00 | EBDA - extended BIOS data area       |
0x000A0000 +--------------------------------------+
           | VGA memory   (0xB8000 = the text     |
           |  buffer from "A Screen to Print On") |
0x000C0000 | video BIOS ROM                       |
0x000F0000 | system BIOS ROM                      |
0x00100000 +--------------------------------------+  1 MiB: extended
           |  RAM continues...                    |       memory
```

Everything from 0xA0000 to 0xFFFFF — 384 KiB of address space — is not
RAM. Reads and writes there go to the video card or to ROM chips; that is
how `console_putc` worked, and it is why the 8086's 1 MiB address space gave
DOS programs only 640 KiB. The EBDA just below 0xA0000 is subtler: it
*is* RAM, but the BIOS scribbles its own state there — disk geometry,
pointing-device state — and if you treat it as free and overwrite it,
your next BIOS disk call returns garbage. The firmware told you the
truth in entry 0 by ending the usable range at 0x9FC00; a kernel that
"rounds up to 640 KiB because everyone knows it's 640 KiB" corrupts the
BIOS's memory. Believe the map, not the folklore.

## Why the map arrives broken

You might expect the firmware to hand over a tidy, sorted list. The E820
interface guarantees nothing of the kind, and real firmware exercises
every freedom it has:

- **Unsorted.** The BIOS assembles the map from internal tables in
  whatever order vendor code appended them. Entries can arrive in any
  order.
- **Overlapping.** Buggy firmware is a constant of nature. Maps have
  shipped where an ACPI region overlaps a usable one, or two usable
  entries cover the same RAM twice. Linux carries a `sanitize` pass for
  exactly this; so will we.
- **Holey.** As above: unlisted ranges, and reserved islands punched
  through the middle of RAM.
- **Ignorant of you.** This is the trap. E820 describes the machine, not
  your kernel. The RAM your kernel was loaded into is reported as
  *usable* — of course it is; that is why the bootloader could load you
  there. The map has no idea you exist.

That last point produces the classic first-allocator bug, and it is worth
spelling out because nearly everyone writes it once. You initialize your
frame allocator straight from the usable map. The lowest usable frame it
knows is somewhere around 1 MiB — which is where your kernel's code is.
The very first allocation hands out a frame containing the allocator's
own instructions; the caller zeroes its new frame, as well-behaved
callers do; and the machine executes zeroes until it triple-faults. The
fix is not clever: subtract the kernel's own footprint — `[kernel_base,
kernel_end)`, known from linker symbols in a real kernel — from the map
before the allocator ever sees it.

## Normalization: from mess to truth

So the raw map goes through a pipeline. Order matters, and each stage
has one job:

1. **Filter.** Keep type-1 (usable) entries only. Everything else is
   someone else's memory. When entries disagree — a range claimed both
   usable and reserved — reserved wins; the safe reading of a conflict
   is always "not yours".
2. **Sort** by base address, so overlap becomes a purely local question:
   after sorting, a region can only overlap the one before it.
3. **Merge** overlapping and adjacent regions. Two entries covering
   `[1 MiB, 3 MiB)` and `[2 MiB, 4 MiB)` are one fact: `[1 MiB, 4 MiB)`.
   Adjacent regions (`end == next.base`) merge too — there is no seam in
   the actual RAM, so there should be none in the map.
4. **Punch out the kernel.** Remove `[kernel_base, kernel_end)` from any
   region it intersects. A region can survive this whole, get trimmed at
   one end, be split in two, or vanish entirely.
5. **Align inward** to page boundaries: round each region's base *up*,
   and its end *down*, to a multiple of 4096. Then drop anything that
   became empty.

Stage 5 is where a wrong instinct costs you a machine. The allocator we
are about to build deals in whole pages — a page is usable only if
*every one* of its 4096 bytes is usable. Consider a region the firmware
reports as `[0x3210, 0x7C00)`:

```
page grid:   0x3000      0x4000      0x5000      0x6000      0x7000      0x8000
                |-----------|-----------|-----------|-----------|-----------|
reported:          [0x3210 ...................................... 0x7C00)
inward:                 [0x4000 ......................... 0x7000)
```

The page at 0x3000 contains bytes 0x3000–0x320F that the firmware did
*not* say are usable; the page at 0x7000 contains 0x7C00–0x7FFF likewise.
Rounding outward — base down, end up — would quietly promote those
reserved bytes to "free RAM", and the allocator would eventually hand out
a frame that overlaps the EBDA or a ROM. Rounding inward throws away at
most a page-fragment at each end and can never lie. When aligning
discards memory, it discards *your claim* to memory, never someone
else's; that asymmetry is the whole principle.

One ordering subtlety: align *after* merging, not before. Two adjacent
fragments like `[0x1800, 0x2200)` and `[0x2200, 0x3900)` each round
inward to nothing — neither contains a whole page on its own — but
merged they are `[0x1800, 0x3900)`, which contains the entire page at
0x2000. The bytes were usable all along; only the map's bookkeeping was
split. Merge first and you keep the page; align first and you leak it
forever. (Punching the kernel hole can happen before or after alignment,
as long as the hole's own bounds are page-aligned — in DuckOS they
always are.)

The first challenge builds this pipeline.

The five stages in order — amber marks align-inward, which must come last:

```d2
direction: right

f: "1 filter\ntype 1 only"
s: "2 sort\nby base"
m: "3 merge\noverlap + adjacent"
k: "4 punch out\nthe kernel"
a: "5 align\ninward" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}

f -> s -> m -> k -> a
```

## From bytes to frames

Once the map is clean, the kernel stops thinking in bytes. Physical
memory is managed in **frames**: `PAGE_SIZE`-sized, `PAGE_SIZE`-aligned
chunks. On i386 the hardware picked the size for us in 1985 — the 80386's
paging unit, which we meet properly in *Paging and Virtual Memory*, works
in 4096-byte pages, so:

```c
#define PAGE_SIZE 4096
```

Because 4096 is 2^12, converting between addresses and frame numbers is
a shift, not a division:

```
frame number = addr >> 12          addr = frame << 12

addr 0x0010_3000  ->  frame 0x103 (259)
frame 259         ->  addr 0x00103000
```

and page-alignment is a mask, because `PAGE_SIZE - 1` (0xFFF) is exactly
the "offset within page" bits:

```c
x & ~(PAGE_SIZE - 1)                    /* round down to a page   */
(x + PAGE_SIZE - 1) & ~(PAGE_SIZE - 1)  /* round up to a page     */
```

Our 128 MiB example machine has 32768 frames. The allocator's whole job
is a set: which of these frames are in use? Everything else — paging,
the heap, per-process memory — is built on asking that set for a frame
and giving it back later.

## Choosing an allocator

There are three classic ways to represent that set, and every real
kernel picks one of them (or layers several).

**Bitmap.** One bit per frame: set means in use. For 4 GiB of RAM at
4 KiB per frame that is 2^20 bits — 128 KiB of bitmap, a trivial price.
Freeing is O(1) (clear a bit), allocation is a linear scan for a zero
bit, and — the sleeper feature — finding N *contiguous* frames is just
scanning for N zero bits in a row, which some hardware genuinely
requires (a DMA disk controller neither knows nor cares about your page
tables; it needs physically contiguous buffers). The whole allocator
state is one flat array you can hex-dump when it misbehaves.

**Free list.** Thread a linked list *through the free frames
themselves*: the first 4 bytes of each free frame hold the address of
the next free frame. Allocation pops the head, freeing pushes — both
O(1), and the bookkeeping costs zero bytes because it lives inside
memory that is by definition unused. The price: the list's order decays
into randomness as frames are freed, so contiguous allocation is
effectively impossible, and asking "is frame 259 free?" means walking
the whole list. Elegant, fast, and blind.

**Buddy allocator.** Manage blocks in power-of-two sizes: a free 8-page
block can split into two 4-page "buddies", and when both buddies of a
pair are free they merge back. A block's buddy is found by XORing its
address with its size — pure arithmetic, no searching — so splitting
and coalescing are O(log n), and contiguous power-of-two allocations
are first-class. The cost is real code complexity and internal
fragmentation (a 5-page request burns an 8-page block). This is what
Linux uses: the buddy system, with free lists per order from 1 page up
to 1024, has been `mm/page_alloc.c`'s core since the 1990s.

DuckOS uses the bitmap, and the reasoning is worth stating because it
is an engineering judgment, not a shrug. Our kernel manages a few
thousand frames and allocates at human timescales; an O(n) scan over a
few KiB of bitmap is nanoseconds. The bitmap gives us contiguous runs
almost for free, which the free list cannot, and it is transparent in a
way the buddy system is not — when (not if) you corrupt allocator state
while developing, you can *look at* a bitmap. This is the Minix
tradition in miniature: Tanenbaum chose, over and over, the design a
student can hold in their head over the design that benchmarks best,
and Minix's own memory manager ran on plain first-fit scans for the
same reason. Linux exists because that codebase was readable enough to
learn from; we can afford the same trade.

## Bits, words, and careful arithmetic

A bitmap is an array of 32-bit words with 32 frames filed in each, so
every operation starts by splitting a frame number into a word index
and a bit index:

```
word index = frame / 32 = frame >> 5
bit index  = frame % 32 = frame & 31

frame 40:  word 1, bit 8
frame 31:  word 0, bit 31
frame 32:  word 1, bit 0
```

The three idioms, worth memorizing because they recur in every kernel
ever written (page bitmaps, inode bitmaps — see *A Filesystem on a
Disk* — signal masks, IRQ masks):

```c
bitmap[f >> 5] |=  (1u << (f & 31));    /* set:   mark frame used  */
bitmap[f >> 5] &= ~(1u << (f & 31));    /* clear: mark frame free  */
(bitmap[f >> 5] >> (f & 31)) & 1u       /* test:  is it used?      */
```

Two details are load-bearing. First, `1u`, not `1`: `1 << 31` shifts a
*signed* int into its sign bit, which is undefined behavior in C —
the unsigned literal makes the whole expression unsigned and defined.
Second, the clear idiom is AND-with-complement, not XOR: XOR would
*toggle* the bit, so clearing an already-clear bit would set it. Freeing
a free frame should be harmless, not corrupting.

One more trick the challenge will reward: when scanning for a free
frame, you can skip 32 frames at a time by comparing whole words against
`0xFFFFFFFF` — a full word has no free frames, move on. On a mostly-full
bitmap this turns the scan from bit-at-a-time into word-at-a-time.

A note before the code: in a real kernel the E820 buffer is filled by a
BIOS call in real-mode assembly, and the bitmap guards actual silicon.
Here, as everywhere in DuckOS, we build the same structures in plain C
buffers the tests can read — the logic is byte-for-byte what a real
kernel does; only the source of the bytes is simulated.

## Challenge: Tame the Memory Map {#memmap-normalize points=15}

Implement the normalization pipeline: take a raw, ugly list of usable
regions and produce the clean, page-aligned truth the frame allocator
can be built on.

```c
struct mem_region {
	uint32_t base;
	uint32_t len;
};

int memmap_normalize(const struct mem_region *in, int n_in,
                     uint32_t kernel_base, uint32_t kernel_end,
                     struct mem_region *out, int max_out);
```

The contract:

- `in` holds `n_in` regions (`0 <= n_in <= MEMMAP_MAX`, which is 32).
  All are usable-typed — the type filtering already happened — but they
  arrive **unsorted**, possibly **overlapping** or **adjacent**, possibly
  **unaligned**, and possibly with `len == 0`. No region wraps: you may
  assume `base + len <= 0xFFFFF000` for every input.
- `[kernel_base, kernel_end)` is the kernel's own footprint. Both bounds
  are page-aligned and `kernel_end > kernel_base` — that is promised, so
  you do not need to align or validate them.
- Write the result to `out`: regions sorted by ascending base,
  non-overlapping and non-adjacent (fully merged), each with `base` and
  `len` a multiple of 4096, none intersecting the kernel footprint, and
  no empty regions.
- Return the number of regions written, or **-1** if the result needs
  more than `max_out` regions (`out` may then contain partial garbage;
  callers treat -1 as fatal).

Follow the pipeline order from the lesson: sort, merge, punch out the
kernel, align inward, drop empties. The tests include a pair of adjacent
unaligned fragments that only contain a whole page *after* merging — the
align-first shortcut will fail it. Punching before or after alignment is
fine (the kernel bounds are aligned, so both orders agree). The half-open
`[start, end)` form is much easier to sort, merge, and clip than
base+len — convert on the way in, convert back on the way out.

What the tests plant and check: an already-clean map (passes through
unchanged), overlapping and adjacent pairs (merge to one), an unsorted
map (comes out sorted), a region straddling the kernel (splits in two),
a region inside the kernel footprint (vanishes), an unaligned region
(shrinks inward), the merge-before-align pair above, a sub-page sliver
(drops entirely), an empty input (returns 0), and a split that overflows
`max_out` (returns -1).

Why merge comes before align — the green path keeps page 0x2000; the red align-first path leaks it:

```d2
direction: right

raw: "[0x1800, 0x2200)\n[0x2200, 0x3900)"

merged: "[0x1800, 0x3900)"
kept: "[0x2000, 0x3000)\npage 0x2000 kept" {
  style.stroke: "#16a34a"
  style.stroke-width: 3
}
lost: "∅  ∅\npage 0x2000 leaked" {
  style.stroke-dash: 4
  style.font-color: "#9ca3af"
}

raw -> merged: "merge first"
merged -> kept: "align inward"
raw -> lost: "align first (wrong)" {
  style.stroke: "#dc2626"
  style.stroke-dash: 4
}
```

### Starter

```c
#include <stdint.h>

#define PAGE_SIZE  4096
#define MEMMAP_MAX 32		/* n_in never exceeds this */

struct mem_region {
	uint32_t base;
	uint32_t len;
};

/*
 * Normalize a raw usable-memory map.
 *
 * in[0..n_in): usable regions; unsorted, may overlap, touch, be
 * unaligned or empty. No region wraps (base + len <= 0xFFFFF000).
 * [kernel_base, kernel_end): page-aligned, non-empty; punch it out.
 *
 * out: sorted, merged, page-aligned (inward), kernel-free, non-empty
 * regions. Returns how many, or -1 if more than max_out are needed.
 */
int memmap_normalize(const struct mem_region *in, int n_in,
                     uint32_t kernel_base, uint32_t kernel_end,
                     struct mem_region *out, int max_out)
{
	/*
	 * TODO:
	 *  1. Copy non-empty regions into local start[]/end[] arrays
	 *     (size MEMMAP_MAX) as half-open [start, end) intervals.
	 *  2. Sort them by start (insertion sort is plenty).
	 *  3. Merge overlapping/adjacent neighbors in place.
	 *  4. For each merged region: remove [kernel_base, kernel_end)
	 *     (keep the piece(s) left over -- there may be 0, 1 or 2),
	 *     round each piece's start up and end down to PAGE_SIZE,
	 *     and if it is still non-empty, append it to out --
	 *     returning -1 first if out is already full.
	 */
	(void)in;
	(void)n_in;
	(void)kernel_base;
	(void)kernel_end;
	(void)out;
	(void)max_out;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define PAGE_SIZE  4096
#define MEMMAP_MAX 32

struct mem_region {
	uint32_t base;
	uint32_t len;
};

int memmap_normalize(const struct mem_region *in, int n_in,
                     uint32_t kernel_base, uint32_t kernel_end,
                     struct mem_region *out, int max_out);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static int reg_is(const struct mem_region *r, uint32_t base, uint32_t len)
{
	return r->base == base && r->len == len;
}

int main(void)
{
	/* Kernel footprint used throughout unless a test says otherwise:
	 * [0x00400000, 0x00480000) -- 4 MiB to 4.5 MiB, page-aligned. */
	const uint32_t kb = 0x00400000, ke = 0x00480000;

	{
		struct mem_region in[] = {
			{ 0x00100000, 0x00200000 },	/* 1..3 MiB */
			{ 0x00500000, 0x00100000 },	/* 5..6 MiB */
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 2, kb, ke, out, 8);
		check(n == 2 && reg_is(&out[0], 0x00100000, 0x00200000)
			     && reg_is(&out[1], 0x00500000, 0x00100000),
		      "test_clean_map_passes_through");
	}

	{
		struct mem_region in[] = {
			{ 0x00100000, 0x00200000 },	/* 1..3 MiB   */
			{ 0x00280000, 0x00100000 },	/* 2.5..3.5   */
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 2, kb, ke, out, 8);
		check(n == 1 && reg_is(&out[0], 0x00100000, 0x00280000),
		      "test_overlapping_regions_merge");
	}

	{
		struct mem_region in[] = {
			{ 0x00100000, 0x00100000 },	/* 1..2 MiB */
			{ 0x00200000, 0x00100000 },	/* 2..3 MiB */
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 2, kb, ke, out, 8);
		check(n == 1 && reg_is(&out[0], 0x00100000, 0x00200000),
		      "test_adjacent_regions_merge");
	}

	{
		struct mem_region in[] = {
			{ 0x00500000, 0x00100000 },
			{ 0x00100000, 0x00100000 },
			{ 0x00300000, 0x00080000 },
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 3, 0x00700000, 0x00780000, out, 8);
		check(n == 3 && reg_is(&out[0], 0x00100000, 0x00100000)
			     && reg_is(&out[1], 0x00300000, 0x00080000)
			     && reg_is(&out[2], 0x00500000, 0x00100000),
		      "test_unsorted_input_sorts");
	}

	{
		/* One big region straddles the kernel: splits in two. */
		struct mem_region in[] = {
			{ 0x00100000, 0x00500000 },	/* 1..6 MiB */
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 1, kb, ke, out, 8);
		check(n == 2 && reg_is(&out[0], 0x00100000, 0x00300000)
			     && reg_is(&out[1], 0x00480000, 0x00180000),
		      "test_kernel_hole_splits_region");
	}

	{
		/* Region entirely inside the kernel footprint vanishes. */
		struct mem_region in[] = {
			{ 0x00410000, 0x00020000 },	/* inside [kb, ke) */
			{ 0x00600000, 0x00100000 },
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 2, kb, ke, out, 8);
		check(n == 1 && reg_is(&out[0], 0x00600000, 0x00100000),
		      "test_region_swallowed_by_kernel");
	}

	{
		/* Unaligned ends round inward: base up, end down. */
		struct mem_region in[] = {
			{ 0x00100800, 0x001FF000 },	/* 0x100800..0x2FF800 */
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 1, kb, ke, out, 8);
		check(n == 1 && reg_is(&out[0], 0x00101000, 0x001FE000),
		      "test_unaligned_region_aligns_inward");
	}

	{
		/* Adjacent unaligned fragments hold a whole page only
		 * once merged; align-before-merge loses it. */
		struct mem_region in[] = {
			{ 0x00001800, 0x00000a00 },	/* 0x1800..0x2200 */
			{ 0x00002200, 0x00001700 },	/* 0x2200..0x3900 */
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 2, kb, ke, out, 8);
		check(n == 1 && reg_is(&out[0], 0x00002000, 0x00001000),
		      "test_merge_before_align");
	}

	{
		/* A sliver smaller than any page-aligned span drops. */
		struct mem_region in[] = {
			{ 0x00100100, 0x00000e00 },	/* inside one page */
		};
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 1, kb, ke, out, 8);
		check(n == 0, "test_subpage_sliver_drops");
	}

	{
		struct mem_region in[1] = { { 0, 0 } };	/* unused: n_in = 0 */
		struct mem_region out[8] = { { 0, 0 } };
		int n = memmap_normalize(in, 0, kb, ke, out, 8);
		check(n == 0, "test_empty_input");
	}

	{
		/* Kernel split needs 2 output slots; only 1 given. */
		struct mem_region in[] = {
			{ 0x00100000, 0x00500000 },
		};
		struct mem_region out[1] = { { 0, 0 } };
		int n = memmap_normalize(in, 1, kb, ke, out, 1);
		check(n == -1, "test_overflow_returns_minus_one");
	}

	return failed;
}
```

## Challenge: The Frame Allocator {#frame-alloc points=20}

Build DuckOS's physical memory allocator: a bitmap over frames, where a
set bit means "in use". This is the allocator every later lesson draws
from — page tables, the kernel heap, process images all start life as a
call to `fa_alloc`.

```c
#define MAX_FRAMES 1024

struct frame_alloc {
	uint32_t bitmap[MAX_FRAMES / 32];	/* bit set = frame in use */
	uint32_t nframes;			/* frames actually managed */
};
```

`MAX_FRAMES` is the structure's capacity; `nframes` is how many frames
this machine actually has (a normalized map in hand, that is its total
page count). The gap matters: frames `nframes` and above do not exist,
and the allocator must never hand one out. The trick is to burn them at
init: mark every frame from `nframes` to `MAX_FRAMES - 1` as used, and
they become permanently unallocatable **guard bits** — no later code
path needs a range check on the scan. But that only holds if `fa_free`
refuses to clear bits outside `[0, nframes)`; a stray free of frame 40
on a 40-frame machine would quietly resurrect a nonexistent frame.

Implement five functions:

- `void fa_init(struct frame_alloc *fa, uint32_t nframes)` — set up the
  allocator with all managed frames free and all frames at `nframes` and
  beyond marked used (the guard bits). `nframes <= MAX_FRAMES` is
  promised. Note that `nframes` need not be a multiple of 32: the word
  containing frame `nframes` is part guard, part real.
- `int fa_alloc(struct frame_alloc *fa)` — allocate the **lowest-
  numbered** free frame: mark it used and return its number, or -1 if
  no frame is free. Lowest-first keeps allocation deterministic and
  packs memory toward low addresses.
- `void fa_free(struct frame_alloc *fa, int frame)` — mark `frame` free
  again. If `frame` is outside `[0, nframes)`, do nothing — this is
  what protects the guard bits.
- `int fa_alloc_run(struct frame_alloc *fa, int count)` — find the
  **first** (lowest-starting) run of `count` contiguous free frames,
  mark them all used, and return the first frame number; -1 if no such
  run exists (in which case the bitmap must be left unchanged).
  `count >= 1` is promised. We will need this later for DMA-ish
  buffers: a disk controller doing bus-master transfers needs
  physically contiguous memory, and no amount of paging cleverness can
  fake that.
- `int fa_used(const struct frame_alloc *fa)` — return how many of the
  *managed* frames (below `nframes`) are currently used. Guard bits do
  not count.

What the tests plant and check: a fresh allocator hands out frames 0, 1,
2 in order; freeing a low frame means the next alloc reuses it (lowest
wins); a small allocator exhausts to -1; an allocator with `nframes = 40`
(deliberately not a multiple of 32) never yields frame 40 or higher even
after everything — including out-of-range frame numbers — is freed;
`fa_alloc_run` skips gaps that are too small and picks the first that
fits, returns -1 (leaving state untouched) when only smaller gaps exist,
never straddles a used frame, and correctly finds a run crossing a
32-bit word boundary; and `fa_used` tracks through all of it.

### Starter

```c
#include <stdint.h>

#define MAX_FRAMES 1024

struct frame_alloc {
	uint32_t bitmap[MAX_FRAMES / 32];	/* bit set = frame in use */
	uint32_t nframes;			/* frames actually managed */
};

/*
 * Initialize: frames [0, nframes) free, frames [nframes, MAX_FRAMES)
 * permanently used (guard bits). nframes <= MAX_FRAMES is promised.
 */
void fa_init(struct frame_alloc *fa, uint32_t nframes)
{
	/* TODO: zero the bitmap, record nframes, set the guard bits */
	(void)fa;
	(void)nframes;
}

/*
 * Allocate the lowest-numbered free frame; mark it used.
 * Returns the frame number, or -1 if nothing is free.
 */
int fa_alloc(struct frame_alloc *fa)
{
	/* TODO: scan for a clear bit, set it, return its index */
	(void)fa;
	return -1;
}

/*
 * Mark a frame free. Out-of-range frames (frame < 0 or
 * frame >= nframes) are ignored -- never clear a guard bit.
 */
void fa_free(struct frame_alloc *fa, int frame)
{
	/* TODO: range-check, then clear the bit */
	(void)fa;
	(void)frame;
}

/*
 * Allocate the first run of `count` contiguous free frames
 * (count >= 1). Marks the whole run used and returns its first
 * frame number, or -1 -- with the bitmap unchanged -- if no run
 * of that length exists.
 */
int fa_alloc_run(struct frame_alloc *fa, int count)
{
	/* TODO: for each candidate start, check count bits; on the
	 * first all-free run, set them all and return the start */
	(void)fa;
	(void)count;
	return -1;
}

/*
 * How many managed frames (below nframes) are currently used?
 * Guard bits do not count.
 */
int fa_used(const struct frame_alloc *fa)
{
	/* TODO: count set bits among frames [0, nframes) */
	(void)fa;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define MAX_FRAMES 1024

struct frame_alloc {
	uint32_t bitmap[MAX_FRAMES / 32];	/* bit set = frame in use */
	uint32_t nframes;			/* frames actually managed */
};

void fa_init(struct frame_alloc *fa, uint32_t nframes);
int fa_alloc(struct frame_alloc *fa);
void fa_free(struct frame_alloc *fa, int frame);
int fa_alloc_run(struct frame_alloc *fa, int count);
int fa_used(const struct frame_alloc *fa);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void)
{
	{
		struct frame_alloc fa;
		fa_init(&fa, 64);
		check(fa_alloc(&fa) == 0 && fa_alloc(&fa) == 1
					 && fa_alloc(&fa) == 2,
		      "test_fresh_allocator_hands_out_0_1_2");
		check(fa_used(&fa) == 3, "test_used_counts_allocations");
	}

	{
		struct frame_alloc fa;
		int i;
		fa_init(&fa, 16);
		for (i = 0; i < 5; i++)
			fa_alloc(&fa);		/* frames 0..4 in use */
		fa_free(&fa, 2);
		check(fa_alloc(&fa) == 2, "test_free_then_realloc_reuses_lowest");
		fa_free(&fa, 3);
		fa_free(&fa, 1);
		check(fa_alloc(&fa) == 1, "test_lowest_free_wins");
	}

	{
		struct frame_alloc fa;
		int i, ok = 1;
		fa_init(&fa, 8);
		for (i = 0; i < 8; i++)
			ok = ok && fa_alloc(&fa) == i;
		check(ok, "test_allocates_all_managed_frames");
		check(fa_alloc(&fa) == -1, "test_exhaustion_returns_minus_one");
		check(fa_used(&fa) == 8, "test_used_at_exhaustion");
	}

	{
		/* nframes = 40: not a multiple of 32. Frames 40..1023
		 * are guard bits and must never escape. */
		struct frame_alloc fa;
		int i, f, ok = 1;
		fa_init(&fa, 40);
		for (i = 0; i < 40; i++) {
			f = fa_alloc(&fa);
			ok = ok && f >= 0 && f < 40;
		}
		check(ok, "test_partial_word_allocs_stay_below_nframes");
		check(fa_alloc(&fa) == -1, "test_partial_word_exhausts_at_40");

		/* Free everything -- including out-of-range numbers,
		 * which must be ignored, not clear guard bits. */
		for (i = 0; i < 64; i++)
			fa_free(&fa, i);
		fa_free(&fa, -1);
		check(fa_used(&fa) == 0, "test_all_freed");
		ok = 1;
		for (i = 0; i < 40; i++) {
			f = fa_alloc(&fa);
			ok = ok && f >= 0 && f < 40;
		}
		check(ok && fa_alloc(&fa) == -1,
		      "test_guard_bits_survive_wild_frees");
	}

	{
		struct frame_alloc fa;
		int i;
		fa_init(&fa, 64);
		for (i = 0; i < 12; i++)
			fa_alloc(&fa);		/* 0..11 used */
		fa_free(&fa, 2);
		fa_free(&fa, 3);		/* gap of 2 at frame 2  */
		fa_free(&fa, 5);
		fa_free(&fa, 6);
		fa_free(&fa, 7);		/* gap of 3 at frame 5  */

		/* A run of 4 fits neither gap: first fit is frame 12. */
		check(fa_alloc_run(&fa, 4) == 12, "test_run_skips_small_gaps");
		/* A run of 3 fits the gap at 5 (first fit of size 3). */
		check(fa_alloc_run(&fa, 3) == 5, "test_run_takes_first_fit");
		/* A run of 2 fits the gap at 2. */
		check(fa_alloc_run(&fa, 2) == 2, "test_run_fills_exact_gap");
		/* Everything below 16 is now used again. */
		check(fa_alloc(&fa) == 16, "test_runs_marked_used");
	}

	{
		struct frame_alloc fa;
		int i;
		fa_init(&fa, 8);
		for (i = 0; i < 8; i++)
			fa_alloc(&fa);
		fa_free(&fa, 1);
		fa_free(&fa, 3);
		fa_free(&fa, 5);	/* three gaps of 1 */
		check(fa_alloc_run(&fa, 2) == -1,
		      "test_run_fails_when_gaps_too_small");
		check(fa_used(&fa) == 5,
		      "test_failed_run_leaves_state_unchanged");
		/* 2,3 free but 4 used: a run of 3 must not straddle it. */
		fa_alloc(&fa);		/* refill frame 1 */
		fa_free(&fa, 2);
		check(fa_alloc_run(&fa, 3) == -1,
		      "test_run_never_straddles_used_frame");
		check(fa_alloc_run(&fa, 2) == 2,
		      "test_run_still_finds_pair");
	}

	{
		/* A run crossing the bit 31 / bit 32 word boundary. */
		struct frame_alloc fa;
		int i;
		fa_init(&fa, 64);
		for (i = 0; i < 64; i++)
			fa_alloc(&fa);
		for (i = 30; i < 36; i++)
			fa_free(&fa, i);
		check(fa_alloc_run(&fa, 6) == 30,
		      "test_run_crosses_word_boundary");
		check(fa_used(&fa) == 64, "test_used_after_boundary_run");
	}

	{
		struct frame_alloc fa;
		fa_init(&fa, 10);
		check(fa_used(&fa) == 0, "test_fresh_used_is_zero");
		check(fa_alloc_run(&fa, 11) == -1,
		      "test_run_longer_than_memory_fails");
		check(fa_alloc_run(&fa, 10) == 0,
		      "test_run_of_everything_succeeds");
		check(fa_used(&fa) == 10, "test_used_full");
	}

	return failed;
}
```

# Lesson: Paging and Virtual Memory {#paging}

In *Owning Physical Memory* we built an allocator that hands out 4 KiB
page frames. But every address DuckOS touches is still a physical
address: pointer `0x00100000` *is* byte `0x00100000` of RAM, for the
kernel and for anything else that runs. The moment we load two programs
this becomes fatal. Both were linked expecting their code at some fixed
address; both expect a stack just below some fixed top; a stray pointer
in one silently scribbles over the other, and nothing — not the
compiler, not the kernel — can catch it after the fact.

The idea that made multitasking safe is to stop letting software name
physical memory at all. Every process gets its own **virtual address
space**: a private, apparently empty 4 GiB in which *it* is loaded at
its favorite address. A piece of hardware, the **memory management
unit** (MMU), translates every single memory access — every instruction
fetch, every load, every store — from virtual to physical, using tables
the kernel wrote. Process A's address `0x08048000` and process B's
address `0x08048000` land in different physical frames, and neither can
even *refer to* the other's memory: no virtual address in A reaches B's
frames unless the kernel deliberately maps one. Isolation stops being a
matter of politeness and becomes a property of the address bus.

The 80386 (1985) was the first x86 that could do this, and it is a
large part of why DuckOS's spiritual ancestors diverged: Minix 1.0
targeted the 8086/8088, which had no MMU at all, so it protected
nothing and relied on segments merely to relocate. Linus Torvalds,
sitting in Helsinki with a 386, wanted the real thing — Linux 0.01 used
386 paging from day one and was unapologetically non-portable, which is
precisely what Tanenbaum attacked in the famous "Linux is obsolete"
thread of January 1992. DuckOS follows the 386 design: it is the
machine this whole course has been quietly assuming.

## What paging buys over segmentation

Segmentation, which we met in *Segments and Privilege*, can already
relocate and bound a process: give each program its own base and limit
and it lives in a window of physical memory. Minix on the 286 did
exactly that. But segments are variable-sized and must be physically
*contiguous*, and that combination ages badly:

- To grow a segment you need free space right after it — and something
  else is usually there.
- After hours of processes starting and exiting, free memory is shredded
  into gaps, none big enough for the next program, though their sum is
  plenty. That is **external fragmentation**, and the only cure is
  compaction: stop the world and slide whole segments around.

Paging's answer is to chop both worlds into fixed 4 KiB pieces. Virtual
space is divided into **pages**, physical memory into **frames** (the
very frames our allocator manages), and a table says which page sits on
which frame. That buys four things at once:

- **Uniform granularity.** Every allocation is some number of 4 KiB
  pages. Any free frame fits any page — no best-fit puzzles, no
  external fragmentation, and the frame allocator from last lesson is
  already exactly the right shape.
- **Non-contiguous backing.** A process that believes it owns 1 MiB of
  contiguous memory really owns 256 frames scattered anywhere in RAM.
  Contiguity becomes an illusion the table maintains.
- **Per-page permissions.** Each mapping carries its own bits: code can
  be mapped read-only, kernel pages can be invisible to user mode. A
  segment had one set of rights for its whole span; a page has its own.
- **The page fault as a feature.** An access to a page marked "not
  present" traps into the kernel — and crucially, the trap arrives
  *before* the access happens, with enough information to fix things
  and retry the instruction. That turns a protection mechanism into a
  programmable one: load pages from disk only when first touched
  (**demand paging**), or give `fork` the parent's pages read-only and
  copy one only when someone writes it (**copy-on-write**). DuckOS
  keeps its tables honest — every mapping is backed, and we won't
  implement COW — but the mechanism you build in this lesson is the
  same one those tricks stand on.

## Two levels of translation

On i386, a 32-bit virtual address is three bit-fields:

```
 31                22 21                12 11                    0
┌────────────────────┬────────────────────┬───────────────────────┐
│  page dir index    │  page table index  │     byte offset       │
│      10 bits       │      10 bits       │       12 bits         │
└────────────────────┴────────────────────┴───────────────────────┘
```

Translation is a two-step table walk, rooted at the **CR3** register:

1. CR3 holds the physical frame of the **page directory** (PD): one
   4 KiB page holding 1024 four-byte **page directory entries** (PDEs).
   The top 10 bits of the address pick one. Each PDE covers 4 MiB of
   virtual space (1024 × 4 KiB) and points at a page table.
2. That **page table** (PT) is again one 4 KiB page of 1024 four-byte
   **page table entries** (PTEs). The middle 10 bits pick one. The PTE
   names the physical frame.
3. The low 12 bits are carried over unchanged as the byte offset within
   that 4 KiB frame. Pages and frames are the same size, so the offset
   never needs translating.

Here is one translation, bit by bit. Take `va = 0xC0801ABC`:

```
0xC0801ABC = 1100000010 0000000001 101010111100
             PD index    PT index   offset
             = 770       = 1        = 0xABC
```

Suppose CR3 says the page directory is in frame 2 (physical `0x2000`),
and the tables contain:

```
PD entry 770  (at physical 0x2000 + 770*4 = 0x2C08):  0x00005007
                → page table in frame 5, flags 0x007 (P|W|U)
PT entry 1    (at physical 0x5000 +   1*4 = 0x5004):  0x0000B003
                → frame 0xB, flags 0x003 (P|W)
physical address = 0xB << 12 | 0xABC = 0xBABC
```

Every memory access the CPU makes goes through this. The kernel's job
is only to write the tables; the MMU walks them.

The same translation as a walk — amber is CR3 anchoring everything; the offset (dashed) bypasses the tables entirely:

```d2
direction: right

va: "va 0xC0801ABC" {
  shape: sql_table
  pdi: "PD index = 770"
  pti: "PT index = 1"
  off: "offset = 0xABC"
}

cr3: "CR3 = frame 2" {
  shape: oval
  style.stroke: "#d97706"
}

pd: "page directory (frame 2)" {
  shape: sql_table
  e770: "770: 0x00005007"
}

pt: "page table (frame 5)" {
  shape: sql_table
  e1: "1: 0x0000B003"
}

pa: "phys 0xBABC" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

cr3 -> pd
va.pdi -> pd.e770
pd.e770 -> pt
va.pti -> pt.e1
pt.e1 -> pa
va.off -> pa: "+ offset" {
  style.stroke-dash: 4
}
```

### Why two levels and not one

A single flat table looks simpler: index it with the top 20 bits, done.
Do the arithmetic on it, though. 2^32 bytes of virtual space in 2^12
byte pages is 2^20 pages; at 4 bytes per entry that is a **4 MiB table
per address space** — and it must exist in full, because the hardware
indexes it directly. With `NPROC = 16` process slots, that is 64 MiB of
RAM spent on tables alone, on machines that in 1991 shipped with 4 or 8.

The two-level tree is a bet that address spaces are **sparse** — and
they are. A typical process touches a couple of MiB of code and heap
down low, a stack up high, and the kernel's region at the top; the vast
middle is unmapped. In the tree, an unmapped 4 MiB stretch costs
exactly one PDE with its present bit clear — four bytes, and no page
table at all behind it. A small process pays for the 4 KiB directory
plus one 4 KiB table per 4 MiB region it actually uses: three regions,
16 KiB total, against the flat table's fixed 4 MiB. The price is a
second memory access per walk; the fix for *that* is the TLB, below.

(Sixty-four-bit machines double down: x86-64 walks four or five levels
of the same shape, because 2^64 is sparser still.)

## What's in an entry

PDEs and PTEs share one 32-bit layout. A frame number is 20 bits (there
are at most 2^20 frames of 2^12 bytes in a 32-bit physical space), and
because frames are 4 KiB-aligned, those 20 bits sit in the top of the
entry, leaving the low 12 bits for flags:

```
 31                                12 11      7   6   5    2   1   0
┌─────────────────────────────────────┬────────┬───┬───┬───┬───┬───┬───┐
│  frame number (phys address >> 12)  │ (misc) │ D │ A │...│U/S│R/W│ P │
└─────────────────────────────────────┴────────┴───┴───┴───┴───┴───┴───┘
```

The three flags that carry the whole protection story:

- **P (present), bit 0.** If clear, the entry is dead: the hardware
  looks at *no other bit* and raises a fault. The other 31 bits are
  yours to use — real kernels stash swap locations in not-present
  entries.
- **R/W (writable), bit 1.** Clear means read-only. Writes fault.
- **U/S (user), bit 2.** Clear means supervisor-only: ring 3 may not
  touch the page at all — this single bit is what hides the entire
  kernel from user processes.

Two more get set *by the MMU itself*: **A (accessed)** whenever the
page is touched, **D (dirty, PTEs only)** whenever it is written. They
exist so a swapping kernel can tell which pages are cold and which
would need writing back to disk — hardware-maintained bookkeeping we
note and move past, since DuckOS does not swap.

One wrinkle worth knowing: on the 386, ring 0 ignored the R/W bit —
the supervisor could write straight through a read-only mapping. The
486 added the CR0.WP flag to make the kernel honor it too, and modern
kernels always set WP (copy-on-write is implemented *in* the kernel;
if kernel writes bypass the read-only trap, COW breaks). Early Linux
on the 386 had to verify user pointers in software before every copy
for exactly this reason. DuckOS's simulated MMU behaves like a 486
with WP set: read-only means read-only for everyone.

### When P = 0: the page fault

An access that fails the walk — a not-present entry at either level,
or a permission violation — raises **#PF, vector 14**, through the IDT
mechanism from *Interrupts and the IDT*. The hardware hands the kernel
two pieces of forensic evidence:

mmu_fault_kind as a decision chain — either not-present exit leaves bit 0 clear; only a permission violation sets it:

```d2
direction: right

pde: "PDE\npresent?" {
  shape: diamond
}

pte: "PTE\npresent?" {
  shape: diamond
}

perm: "permission\nbits ok?" {
  shape: diamond
}

np: "fault, bit 0 = 0\n(not present)" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

pv: "fault, bit 0 = 1\n(protection)" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

ok: "no fault: 0" {
  shape: oval
}


pde -> pte: yes
pde -> np: no
pte -> perm: yes
pte -> np: no
perm -> ok: yes
perm -> pv: no
```

- **CR2** latches the exact virtual address the program was touching.
- An **error code** is pushed on the stack, describing the access:

```
bit 0  P    1 = the page WAS present: this is a protection violation
            0 = the page was not present
bit 1  W/R  1 = the faulting access was a write, 0 = a read
bit 2  U/S  1 = the access came from user mode (ring 3)
```

So error code `0x7` reads "a user-mode write to a present page" — some
process wrote a read-only page. Code `0x4` is "user read of a
not-present page" — with demand paging, that's a request to go load it.
Note that a kernel read of a not-present page pushes error code **0**:
every bit describes the access, none of them says "a fault happened" —
you know that because your handler is running at all. The fault
handler is the kernel's chance to either service the fault (map the
page, then return, and the CPU *re-runs the faulting instruction*) or
kill the process with the Unix rite of passage: segmentation fault.

## The TLB, or how any of this is affordable

Count the memory traffic. One `mov eax, [ebx]` is one memory access in
the program — but the walk adds a PDE read and a PTE read. Every load
becomes three; instruction fetches too. Paging as described would
roughly triple memory traffic, and the 386 would have been laughed out
of the market.

The fix is the **translation lookaside buffer**: a small cache inside
the MMU mapping recently used page numbers straight to frame numbers.
On a hit — the overwhelmingly common case, since programs hammer the
same few pages — translation costs nothing and the tables in memory
are never touched. On a miss, the MMU walks the tables once and caches
the result.

Every cache has an invalidation problem, and the TLB's is nasty because
*software* owns the tables and *hardware* owns the cache. The CPU does
not snoop your stores: writing a PTE in memory does **not** update the
TLB. The kernel must say so, and x86 gives two tools:

- `invlpg addr` — evict the one entry for that page.
- Reloading CR3 — flush the whole TLB (done anyway on every process
  switch, since a new CR3 means all old translations are wrong).

Forget, and you get the classic stale-TLB bug. The kernel unmaps a
page and hands its frame to another process; but the TLB still holds
the old translation, so the first process keeps reading — and writing
— a frame that now belongs to someone else, straight past tables that
say the mapping is gone. Nothing crashes at the scene of the crime.
The victim process corrupts mysteriously, later, somewhere else; the
bug vanishes when you add debug prints (which shift timing and evict
TLB entries); and on multiprocessors it gets worse — each CPU has its
own TLB, so unmapping means interrupting every other CPU and asking it
to invalidate too, a ritual called **TLB shootdown**. When kernel
developers speak of stale-TLB bugs, they whisper.

## The higher half

One design decision shapes every real kernel's memory map: the kernel
is mapped into **every** process's address space, at the top. On i386
the traditional split puts the kernel at `0xC0000000` and above (PD
entries 768–1023), leaving `0x00000000`–`0xBFFFFFFF` for the process:

```
0xFFFFFFFF ┌──────────────────────────┐
           │   kernel code + data     │  U/S = 0 in the PTEs:
           │   (same physical frames  │  invisible to ring 3,
           │   in EVERY process's PD  │  present in every space
0xC0000000 │   entries 768..1023)     │
           ├──────────────────────────┤
           │      user stack  ↓       │
           │                          │
           │      user heap   ↑       │  U/S = 1: the process's
           │      user code + data    │  own pages
0x00000000 └──────────────────────────┘
```

Why not give the kernel its own address space? Because every system
call and every interrupt would then need a full CR3 switch (and TLB
flush) on the way in *and* out. With the higher half mapped everywhere,
a trap just changes privilege level: the kernel's code is already
reachable, one jump away, and the U/S bit — not distance — is what
kept user code out of it a nanosecond earlier.

Booting into this layout has a chicken-and-egg step worth savoring.
The kernel is loaded at a low physical address (say 1 MiB) and runs
with paging off, so EIP holds low addresses. The instant you set the
PG bit in CR0, *every* address is translated — including the fetch of
the very next instruction, which is still a low address. If the tables
map only the higher half, that fetch faults with no handler mapped,
and the resulting triple fault reboots the machine. So boot tables map
the kernel **twice**: an **identity mapping** (low virtual → the same
low physical) to survive the switch, plus the higher-half mapping. You
enable paging, jump to a high-half label, fix the stack, and only then
drop the identity mapping — the scaffolding comes down after you've
climbed off it.

## An MMU you can hold

As always in DuckOS: in a real kernel these structs and buffers ARE
the hardware tables — the MMU dereferences the same bytes — while here
we build them in a buffer the tests can read. The simulation model for
this lesson, precisely:

- "Physical memory" is one array: `uint8_t phys[NFRAMES * PAGE_SIZE]`,
  with `NFRAMES = 64` (a roomy 256 KiB machine). A **physical address**
  is nothing but an offset into that array; frame `n` is the 4 KiB
  slice starting at `phys[n * PAGE_SIZE]`.
- `cr3` is a frame number: the frame holding the page directory.
- Page directories and page tables live **inside** `phys[]`, as arrays
  of 1024 little-endian 32-bit entries — byte-for-byte the layout the
  real MMU walks. Entry `i` of the table in frame `f` occupies bytes
  `f * PAGE_SIZE + i*4` through `... + i*4 + 3`, least significant
  byte first (it's x86).
- Frames for new page tables come from a bump allocator, `next_free` —
  a stand-in for the real frame allocator we built last lesson.

The starter gives you `pte_read` and `pte_write`, which do the
little-endian byte assembly for you, so your code can think in whole
entries while the memory stays honest bytes. Everything you write in
the two challenges below — splitting addresses, walking, mapping,
faulting — is exactly what a real i386 kernel does; only the
dereference is simulated.

## Challenge: Slice an Address {#vaddr-split points=10}

Before walking any tables, get the bit-slicing cold. Implement the
three field extractors and their inverse:

- `vaddr_pd_index(va)` — bits 31:22, the page directory index (0–1023).
- `vaddr_pt_index(va)` — bits 21:12, the page table index (0–1023).
- `vaddr_offset(va)` — bits 11:0, the byte offset (0–4095).
- `vaddr_make(pd, pt, off)` — pack three in-range fields back into the
  address they came from. Callers promise `pd <= 1023`, `pt <= 1023`,
  `off <= 4095`; behavior for out-of-range arguments is unspecified.

The tests check the corners: `0` (all fields zero), `0xFFFFFFFF` (all
fields maxed: 1023/1023/0xFFF), the canonical higher-half base
`0xC0000000` (PD index 768, everything else zero), one mid-range
address with three distinct small fields, a hand-computed `vaddr_make`
result, and that `vaddr_make` inverts the split for a batch of
addresses. Shifts and masks only — if you find yourself reaching for
division, look at the diagram again.

### Starter

```c
#include <stdint.h>

/*
 * An i386 virtual address is three bit-fields:
 *
 *    31         22 21         12 11          0
 *   [  PD index   |  PT index   |   offset    ]
 *       10 bits       10 bits       12 bits
 */

/* Bits 31:22 -- which page directory entry (0..1023). */
uint32_t vaddr_pd_index(uint32_t va)
{
	/* TODO: shift the top 10 bits down */
	(void)va;
	return 0;
}

/* Bits 21:12 -- which page table entry (0..1023). */
uint32_t vaddr_pt_index(uint32_t va)
{
	/* TODO: shift, then mask to 10 bits */
	(void)va;
	return 0;
}

/* Bits 11:0 -- byte offset within the 4 KiB page (0..4095). */
uint32_t vaddr_offset(uint32_t va)
{
	/* TODO: mask to 12 bits */
	(void)va;
	return 0;
}

/*
 * The inverse: pack fields back into an address.
 * Requires pd <= 1023, pt <= 1023, off <= 4095 (caller's promise).
 */
uint32_t vaddr_make(uint32_t pd, uint32_t pt, uint32_t off)
{
	/* TODO: shift each field into place and OR them together */
	(void)pd;
	(void)pt;
	(void)off;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

uint32_t vaddr_pd_index(uint32_t va);
uint32_t vaddr_pt_index(uint32_t va);
uint32_t vaddr_offset(uint32_t va);
uint32_t vaddr_make(uint32_t pd, uint32_t pt, uint32_t off);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void)
{
	check(vaddr_pd_index(0) == 0 && vaddr_pt_index(0) == 0 &&
	      vaddr_offset(0) == 0,
	      "test_zero_address_splits_to_zero");

	check(vaddr_pd_index(0xffffffffu) == 1023 &&
	      vaddr_pt_index(0xffffffffu) == 1023 &&
	      vaddr_offset(0xffffffffu) == 0xfff,
	      "test_all_ones_maxes_every_field");

	check(vaddr_pd_index(0xc0000000u) == 768 &&
	      vaddr_pt_index(0xc0000000u) == 0 &&
	      vaddr_offset(0xc0000000u) == 0,
	      "test_higher_half_base_is_pd_768");

	/* 0x01406007 = pd 5, pt 6, offset 7 -- all three fields distinct. */
	check(vaddr_pd_index(0x01406007u) == 5 &&
	      vaddr_pt_index(0x01406007u) == 6 &&
	      vaddr_offset(0x01406007u) == 7,
	      "test_mid_address_distinct_fields");

	check(vaddr_make(768, 42, 0x123) == 0xc002a123u,
	      "test_make_packs_fields");

	{
		uint32_t vas[] = { 0u, 0xc0000000u, 0x01406007u,
		                   0xffffffffu, 0xdeadbeefu };
		size_t i;
		int ok = 1;

		for (i = 0; i < sizeof(vas) / sizeof(vas[0]); i++) {
			uint32_t v = vas[i];

			if (vaddr_make(vaddr_pd_index(v), vaddr_pt_index(v),
			               vaddr_offset(v)) != v)
				ok = 0;
		}
		check(ok, "test_make_inverts_split");
	}

	return failed;
}
```

## Challenge: Map a Page {#page-map points=25}

Now build the MMU itself: mapping, translation, unmapping, and the
fault check — the four verbs behind every address a process will ever
use. The starter provides the machine (`struct mmu`), `mmu_init`
(zeroes physical memory and allocates frame 0 as the empty page
directory), the little-endian entry accessors `pte_read`/`pte_write`,
and the bump allocator `alloc_frame`. You implement four functions:

- `int mmu_map(struct mmu *m, uint32_t va, uint32_t frame,
  uint32_t flags)` — map the page containing `va` to physical `frame`
  with `flags` (an OR of `PTE_W`/`PTE_U`; you add `PTE_P`). Walk the
  directory: if the PDE isn't present, allocate a page-table frame
  with `alloc_frame` and install it in the PDE with flags `P|W|U`.
  Why the most permissive flags on the PDE? Because in our model (as
  in most real kernels) the **leaf PTE decides**: putting W and U on
  every PDE delegates the whole protection decision to the PTE, so
  one page's permissions live in exactly one place. (Frames handed
  out by `alloc_frame` are already zero: `mmu_init` zeroed the world
  and frames are never reused.) Then write the PTE as
  `frame << 12 | flags | PTE_P`. If the PTE is *already* present,
  return -1 and change nothing — remapping is a bug we refuse, not
  overwrite. Return 0 on success.
- `int mmu_translate(const struct mmu *m, uint32_t va,
  uint32_t *pa_out)` — the walk the hardware does: return -1 if the
  PDE or PTE is not present; otherwise store
  `frame << 12 | offset` into `*pa_out` and return 0.
- `int mmu_unmap(struct mmu *m, uint32_t va)` — clear the PTE to 0;
  -1 if the page wasn't mapped. Leave the page table itself installed
  (real kernels reclaim empty tables lazily, if ever — and remember
  the TLB section: on real hardware this is the moment you must
  `invlpg`).
- `int mmu_fault_kind(const struct mmu *m, uint32_t va, int is_write,
  int is_user)` — decide whether an access faults. Return 0 for a
  legal access; otherwise build exactly the x86 #PF error code from
  the lesson: bit 1 = `is_write`, bit 2 = `is_user`, and bit 0 set
  only if the page **was** present (a protection violation: a write
  without `PTE_W`, or any user access without `PTE_U`); bit 0 clear
  means not-present at either level. This mirrors the real pushed
  error code bit for bit — including its quirk that a kernel read of
  an unmapped page yields 0, indistinguishable here from "no fault"
  (on hardware, being inside the #PF handler is what disambiguates;
  the tests stay away from that corner).

The tests plant and check: translation on a fresh MMU fails; a mapped
page translates back with its offset bits intact; two pages under the
same PDE share one page table (watched via `next_free`); a double map
returns -1 and leaves the first mapping intact; unmap kills
translation and a second unmap fails; a user write to a read-only user
page faults with code `0x7`, a user read of a supervisor page with
`0x5`, a kernel write to an unmapped page with `0x2`, a user write to
an unmapped page with `0x6`, and legal accesses return 0; and two
mappings under *different* PDEs allocate two distinct page tables.
The test file repeats the constants and `struct mmu` exactly as given.

### Starter

```c
#include <stdint.h>
#include <string.h>

#define PAGE_SIZE 4096
#define NFRAMES   64

#define PTE_P  0x001	/* present */
#define PTE_W  0x002	/* writable */
#define PTE_U  0x004	/* user-accessible */

/*
 * The whole machine. A "physical address" is an offset into phys[];
 * frame n is the 4 KiB slice at phys[n * PAGE_SIZE]. cr3 names the
 * frame holding the page directory. Page tables live inside phys[]
 * as 1024 little-endian 32-bit entries each -- byte-for-byte the
 * layout a real i386 MMU walks.
 */
struct mmu {
	uint8_t phys[NFRAMES * PAGE_SIZE];
	uint32_t cr3;		/* frame number of the page directory */
	uint32_t next_free;	/* bump allocator: next never-used frame */
};

/* Read entry `index` of the table living in frame `frame`. */
uint32_t pte_read(const uint8_t *phys, uint32_t frame, uint32_t index)
{
	const uint8_t *p = phys + frame * PAGE_SIZE + index * 4;

	return (uint32_t)p[0] | (uint32_t)p[1] << 8 |
	       (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}

/* Write entry `index` of the table living in frame `frame`. */
void pte_write(uint8_t *phys, uint32_t frame, uint32_t index,
               uint32_t value)
{
	uint8_t *p = phys + frame * PAGE_SIZE + index * 4;

	p[0] = (uint8_t)(value & 0xff);
	p[1] = (uint8_t)((value >> 8) & 0xff);
	p[2] = (uint8_t)((value >> 16) & 0xff);
	p[3] = (uint8_t)(value >> 24);
}

/*
 * Hand out the next never-used frame. Frames are already zeroed
 * (mmu_init zeroes everything and frames are never reused). The
 * tests never exhaust NFRAMES.
 */
static uint32_t alloc_frame(struct mmu *m)
{
	return m->next_free++;
}

/* Zero physical memory; allocate frame 0 as the empty page directory. */
void mmu_init(struct mmu *m)
{
	memset(m->phys, 0, sizeof(m->phys));
	m->next_free = 0;
	m->cr3 = alloc_frame(m);
}

/*
 * Map the page containing va to physical `frame` with permission
 * `flags` (an OR of PTE_W / PTE_U; add PTE_P yourself).
 *
 * If the page directory entry is not present, allocate a page-table
 * frame and install it in the PDE with flags P|W|U (the leaf PTE is
 * what enforces permissions). Then write the PTE:
 * frame << 12 | flags | PTE_P.
 *
 * Returns 0 on success; -1 (changing nothing) if the page is already
 * mapped (PTE present).
 */
int mmu_map(struct mmu *m, uint32_t va, uint32_t frame, uint32_t flags)
{
	/* TODO: split va into PD index / PT index; walk, allocating
	 * and installing a page table if the PDE lacks PTE_P; refuse a
	 * double map; write the PTE. */
	(void)m;
	(void)va;
	(void)frame;
	(void)flags;
	return -1;
}

/*
 * The walk the hardware does on every access. Returns 0 and stores
 * frame << 12 | offset into *pa_out, or -1 (leaving *pa_out alone)
 * if the PDE or PTE is not present.
 */
int mmu_translate(const struct mmu *m, uint32_t va, uint32_t *pa_out)
{
	/* TODO: two pte_read calls and a P-bit check at each level */
	(void)m;
	(void)va;
	(void)pa_out;
	return -1;
}

/*
 * Unmap the page containing va: clear its PTE to 0. The page table
 * itself stays installed. Returns 0 on success, -1 if not mapped.
 */
int mmu_unmap(struct mmu *m, uint32_t va)
{
	/* TODO */
	(void)m;
	(void)va;
	return -1;
}

/*
 * Would this access fault? 0 = legal access. Otherwise an x86-style
 * #PF error code:
 *   bit 0: set if the page WAS present (protection violation:
 *          write without PTE_W, or user access without PTE_U);
 *          clear if the PDE or PTE was not present
 *   bit 1: set if is_write
 *   bit 2: set if is_user
 */
int mmu_fault_kind(const struct mmu *m, uint32_t va, int is_write,
                   int is_user)
{
	/* TODO: walk; not-present at either level -> code without
	 * bit 0; present but forbidden -> code with bit 0; else 0 */
	(void)m;
	(void)va;
	(void)is_write;
	(void)is_user;
	return -1;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define PAGE_SIZE 4096
#define NFRAMES   64

#define PTE_P  0x001
#define PTE_W  0x002
#define PTE_U  0x004

struct mmu {
	uint8_t phys[NFRAMES * PAGE_SIZE];
	uint32_t cr3;
	uint32_t next_free;
};

void mmu_init(struct mmu *m);
int mmu_map(struct mmu *m, uint32_t va, uint32_t frame, uint32_t flags);
int mmu_translate(const struct mmu *m, uint32_t va, uint32_t *pa_out);
int mmu_unmap(struct mmu *m, uint32_t va);
int mmu_fault_kind(const struct mmu *m, uint32_t va, int is_write,
                   int is_user);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static struct mmu M;	/* 256 KiB: static, not on the stack */

int main(void)
{
	uint32_t pa = 0;
	uint32_t before;

	mmu_init(&M);

	check(mmu_translate(&M, 0x00400000u, &pa) == -1,
	      "test_translate_on_empty_mmu_fails");

	/* va 0x00400000 = PD index 1, PT index 0: kernel-only, writable. */
	check(mmu_map(&M, 0x00400000u, 5, PTE_W) == 0,
	      "test_map_returns_ok");

	pa = 0;
	check(mmu_translate(&M, 0x00400abcu, &pa) == 0 &&
	      pa == ((5u << 12) | 0xabcu),
	      "test_map_then_translate_round_trips_offset");

	/* 0x00401000 = PD index 1, PT index 1: table already exists,
	 * so next_free must not advance again. */
	before = M.next_free;
	check(mmu_map(&M, 0x00401000u, 6, PTE_W) == 0 &&
	      M.next_free == before,
	      "test_same_pd_entry_shares_page_table");

	check(mmu_map(&M, 0x00400000u, 9, PTE_W) == -1,
	      "test_double_map_rejected");
	pa = 0;
	check(mmu_translate(&M, 0x00400000u, &pa) == 0 &&
	      pa == (5u << 12),
	      "test_double_map_left_mapping_intact");

	check(mmu_unmap(&M, 0x00401000u) == 0 &&
	      mmu_translate(&M, 0x00401000u, &pa) == -1,
	      "test_unmap_then_translate_fails");
	check(mmu_unmap(&M, 0x00401000u) == -1,
	      "test_unmap_unmapped_fails");

	/* Read-only user page (PTE_U, no PTE_W) at PD index 32. */
	check(mmu_map(&M, 0x08000000u, 7, PTE_U) == 0,
	      "test_map_user_readonly_ok");
	check(mmu_fault_kind(&M, 0x08000000u, 1, 1) == 0x7,
	      "test_user_write_to_readonly_is_0x7");
	check(mmu_fault_kind(&M, 0x08000000u, 0, 1) == 0,
	      "test_user_read_of_user_page_no_fault");

	/* 0x00400000 is supervisor-only (no PTE_U). */
	check(mmu_fault_kind(&M, 0x00400000u, 0, 1) == 0x5,
	      "test_user_read_of_supervisor_page_is_0x5");
	check(mmu_fault_kind(&M, 0x00400000u, 1, 0) == 0,
	      "test_kernel_write_to_writable_page_no_fault");

	/* Nothing mapped anywhere near 0x30000000. */
	check(mmu_fault_kind(&M, 0x30000000u, 1, 0) == 0x2,
	      "test_kernel_write_to_unmapped_is_0x2");
	check(mmu_fault_kind(&M, 0x30000000u, 1, 1) == 0x6,
	      "test_user_write_to_unmapped_is_0x6");

	/* Distinct PD entries must get distinct page tables. */
	before = M.next_free;
	check(mmu_map(&M, 0x10000000u, 10, PTE_W) == 0 &&
	      mmu_map(&M, 0x20000000u, 11, PTE_W) == 0 &&
	      M.next_free == before + 2,
	      "test_distinct_pd_entries_allocate_distinct_tables");

	return failed;
}
```

# Lesson: The Kernel Heap {#kernel-heap}

In *Owning Physical Memory* we built a frame allocator: ask it for
memory and it hands you 4096 bytes, because 4096 bytes is the only
size the paging hardware deals in. In *Paging and Virtual Memory* we
learned to map those frames wherever we want them. Between the two,
DuckOS can now get memory — in exactly one size. Extra large.

The kernel's actual appetite looks nothing like that. A timer record
for *The Clock Ticks* is a couple dozen bytes. A `struct message` is
16. A buffer-cache header in *Block Devices and the Buffer Cache* is
maybe 40. Hand each of those its own frame and you waste more than 99%
of it — a kernel that burned 4096 bytes per 40-byte object would
exhaust a 1991-sized machine (4 MB, if you were lucky) after about a
thousand small allocations. What the kernel needs is a retailer: a
layer that takes frames from the frame allocator *wholesale* and sells
them off a few dozen bytes at a time. That retailer is `kmalloc`, and
it sits on top of the frame allocator exactly the way user-space
`malloc` sits on top of `brk` and `mmap` — libc grabs big runs of
address space from the kernel, then carves them up privately, so that
a million tiny `malloc(24)` calls cost only a handful of system calls.

The design we'll build is the classic one, and it has a famous
pedigree: Section 8.7 of Kernighan and Ritchie's *The C Programming
Language* — "Example — A Storage Allocator" — lays it out in about
forty lines of C: a list of free blocks, each carrying a small header;
first-fit search; split what's too big; merge what's adjacent on free.
That 1978 textbook example is a distillation of how early Unix and its
contemporaries actually managed memory, and the same shape shows up in
Minix's memory manager, which kept a first-fit "hole list" of unused
memory regions. Doug Lea's `dlmalloc`, whose descendants still live
inside glibc today, is the same idea grown industrial-strength. DuckOS
builds the K&R lineage member: small enough to hold in your head,
real enough that every production allocator is recognizably its
descendant.

## The design space

Before committing to a design, be honest about the alternatives —
there are three families of kernel allocator, and real kernels use all
three at once, at different layers.

**Free list with headers** (what we build). Every block, allocated or
free, carries a small header recording its size and status. Allocation
walks the free blocks looking for one big enough; freeing marks the
block free and merges it with free neighbors. It handles *any* request
size, which is why it's the general-purpose layer. Its costs: the
header overhead on every allocation, linear search, and — the deep
one — **external fragmentation**: free memory that exists but is
chopped into pieces too small to satisfy a request. Knuth's *The Art
of Computer Programming* named the classic refinement, **boundary
tags**: put a size field at the *end* of each block too, so the block
before you can be found in O(1) and coalescing never requires a
search. We'll come back to that.

Fragmentation is real — 16000 bytes sit free, but no single hole fits 12000 (both red probes fail):

```d2
direction: right

req: "kmalloc(12000)" {
  shape: oval
  style.stroke: "#d97706"
}

arena: "" {
  grid-columns: 4
  grid-gap: 0

  hole1: "free\n8000" {
    style.stroke-width: 3
  }
  a1: "allocated\n8000"
  hole2: "free\n8000" {
    style.stroke-width: 3
  }
  a2: "allocated\n(the rest)"
}

req -> arena.hole1: {
  style.stroke-dash: 4
  style.stroke: "#dc2626"
}
req -> arena.hole2: "8000 < 12000" {
  style.stroke-dash: 4
  style.stroke: "#dc2626"
}
```

**Buddy allocator** (described by Knowlton in 1965; Linux's physical
page allocator is one). Round every request up to a power of two.
A block of size 2^k splits into two "buddies" of size 2^(k-1), and a
block's buddy is found by pure arithmetic — flip one bit of its
address (`addr XOR size`). Splitting and coalescing become O(1) bit
tricks, which is why buddy systems rule the *page* level. The price is
internal fragmentation: ask for 2^k + 1 bytes and you get 2^(k+1),
wasting almost half.

**Slab allocator** — the one that changed how real kernels do this.
Jeff Bonwick's slab allocator (USENIX 1994, built for SunOS 5.4)
starts from an observation about *kernel* workloads specifically: the
kernel allocates the same few dozen object types over and over —
inodes, proc structures, message buffers — each a fixed size. So don't
run a general allocator at all: keep a dedicated cache per object
type, where each "slab" is a page (or a few) diced into equal-size
slots. Freeing an object just returns its slot to the cache — no
header per object burning space, no searching, no splitting, and no
external fragmentation, because same-size objects pack perfectly.
Bonwick added two refinements that made it win outright: caches keep
freed objects in their *constructed* state, so the expensive
initialize-and-tear-down work isn't repeated on every alloc/free
cycle; and "slab coloring" offsets objects by varying amounts in
different slabs so that thousands of identical structures don't all
fight over the same CPU cache lines. Every major kernel adopted the
design — Linux's `kmalloc` today is a thin wrapper over slab caches of
power-of-two sizes (the current incarnation is called SLUB). The
honest summary: slab wins when object sizes repeat, and in a kernel
they almost always do. We build the free-list allocator anyway,
because it's the general mechanism the others refine — and because a
slab allocator still needs something underneath it that understands
arbitrary sizes.

One more choice inside the free-list family: when several free blocks
fit, which one do you take? **Best-fit** (the smallest that fits)
sounds thrifty but tends to shave blocks into useless slivers — the
leftovers are, by construction, as small as possible — and it costs a
full scan of every free block. **First-fit** takes the first block big
enough and stops. Knuth's simulations found first-fit generally equal
or better on fragmentation and obviously cheaper, and decades of
allocator research since (Wilson et al.'s 1995 survey is the classic)
back a specific variant: first-fit with the free list kept in
**address order**, which clusters allocations at low addresses and —
crucially — means blocks that are neighbors on the list are neighbors
in memory, so coalescing is a matter of looking at the block next
door. Our design gets address order for free, as you're about to see.

## A header hidden before every payload

Here is the puzzle that dictates the whole layout: `kfree(p)` takes
*only a pointer*. No size. Somehow the allocator must recover
everything it knows about that block — how big it is, whether it's
free — from the bare address. The classic trick: put the metadata
*immediately before* the payload, at a fixed offset, so the allocator
can always find it by arithmetic:

```c
struct block {
	uint32_t size;	/* payload bytes in this block (multiple of ALIGN) */
	uint32_t free;	/* 1 = on the free path */
};
```

`kmalloc` writes a header, then returns the address just *past* it.
`kfree(p)` computes `p - sizeof(struct block)` and lands exactly on
the metadata. The user code never knows the header exists — it just
notices that allocations cost 8 bytes more than they asked for.

Blocks are laid head-to-tail in one contiguous arena:

```
heap                                                      heap + HEAP_SIZE
├────────┬───────────┬────────┬──────────────────┬────────┬─────────────┤
│ hdr    │ payload   │ hdr    │ payload          │ hdr    │ payload     │
│size=40 │ 40 bytes  │size=104│ 104 bytes        │size=…  │ …           │
│free=0  │           │free=1  │                  │        │             │
└────────┴───────────┴────────┴──────────────────┴────────┴─────────────┘
  8 bytes              8 bytes                     8 bytes
```

Notice what's *missing*: a `next` pointer. Because blocks are packed
with no gaps, the next block's header is computable:

```c
next = (struct block *)((uint8_t *)hdr + sizeof(struct block) + hdr->size);
```

and the walk ends when that address reaches `heap + HEAP_SIZE`. This
is called an **implicit free list**: the "list" is the arena itself,
traversed by size arithmetic, visiting allocated and free blocks
alike. It's slower than an explicit linked list of only-free blocks
(we scan everything), but it is automatically in address order —
which, per the last section, is exactly the order that makes
coalescing trivial — and it has no pointers to corrupt or dangle.

Make the arithmetic concrete. Suppose the arena begins at address
`0x00102000` and someone calls `kmalloc(40)` on a fresh heap:

```
0x00102000  header   size=40, free=0        ← kmalloc writes this
0x00102008  payload  40 bytes               ← kmalloc returns this
0x00102030  header   size=65480, free=1     ← the split-off remainder
0x00102038  payload  65480 bytes
0x00112000  end of arena
```

Later, `kfree((void *)0x00102008)` computes `0x00102008 - 8 =
0x00102000`, reads `size=40, free=0`, and knows precisely which 48
bytes of arena it's giving back. One subtraction replaces an entire
lookup structure.

In a real kernel this arena would be a region of kernel virtual
address space, grown frame by frame from the allocator we built in
*Owning Physical Memory* and mapped by the tables from *Paging and
Virtual Memory*. Here, in DuckOS's hostable-simulation style, it's a
static C array — same bytes, same arithmetic, and the tests can watch
every move.

## Alignment: why asking for 33 gets you 40

`kmalloc` returns `void *` — the caller may store *anything* there,
so the pointer must be aligned for the most demanding type it might
hold. On i386 that's the 8-byte types: `double`, `uint64_t` (the ABI
technically tolerates less, but misaligned 8-byte accesses are slower,
can straddle cache lines, and break the atomicity guarantees of
instructions like `cmpxchg8b` — so 8-byte alignment is the de facto
contract every real `malloc` honors). K&R's allocator solved this with
a union header sized to the "most restrictive type"; we do the modern
equivalent with a constant:

```
#define ALIGN 8
```

Three things conspire to keep every payload 8-aligned, and all three
are load-bearing:

- **The arena starts aligned.** The starter declares it
  `_Alignas(ALIGN)` — a bare `uint8_t` array only promises alignment
  1, and every address in the heap is relative to its base.
- **The header is exactly 8 bytes** (two `uint32_t`s, no padding), so
  stepping over it preserves alignment.
- **Every payload size is a multiple of 8.** `kmalloc` rounds each
  request up: ask for 33, get 40. The classic idiom, for ALIGN a power
  of two:

  ```c
  n = (n + (ALIGN - 1)) & ~(uint32_t)(ALIGN - 1);
  ```

  Adding 7 pushes any non-multiple past the next boundary; masking the
  low three bits snaps it back down onto it. 33 → 40, 40 → 40, 1 → 8.

Header aligned, sizes aligned, base aligned: by induction, every
header and every payload in the arena sits on an 8-byte boundary,
forever. Break any one of the three and the property quietly dies.

One trap in the rounding idiom: `n` is a `uint32_t`, and unsigned
arithmetic wraps. Feed it `n = 0xFFFFFFFF` and `n + 7` wraps to 6,
which masks to 0 — and a first-fit search for "at least 0 bytes"
happily succeeds. Reject impossible requests (anything bigger than the
arena) *before* rounding. The tests check this.

## Splitting: don't hand a wardrobe to someone who asked for a hanger

A fresh heap is one free block of 65528 bytes (the 64 KiB arena minus
one header). If the first `kmalloc(40)` returned all of it, the heap
would be a one-shot allocator. Instead, first-fit **splits**: take the
first 40 bytes as the allocation, then write a *new* header just past
it, turning the tail into a smaller free block. One panel shows both
states: the dashed outline is the single free block *before*
`kmalloc(40)`, and the amber-bordered header is the new one the split
writes:

```d2
direction: right

arena: "was one free block: 65528" {
  style.stroke-dash: 4
  grid-columns: 4
  grid-gap: 0

  h1: "hdr\nsize=40\nfree=0"
  p1: "payload\n40 bytes"
  h2: "NEW hdr\nsize=65480\nfree=1" {
    style.stroke: "#d97706"
    style.stroke-width: 3
  }
  p2: "free payload\n65480 bytes"
}
```

The arithmetic: the tail's payload is the old size, minus the 40 we
took, minus 8 for the tail's own header — splitting costs one header.

But splitting has a floor. Suppose a 48-byte free block gets a 40-byte
request. The remainder is 8 bytes — exactly enough for a header and
*zero* bytes of payload. A size-0 free block is a cursed object: it
can never satisfy any request, it makes walks longer, and edge cases
around it breed bugs. So the rule is: **split only if the tail can
hold a header plus at least ALIGN bytes of payload** (8 + 8 = 16 in
our numbers); otherwise hand over the whole block, slack included.
The caller who asked for 40 silently receives 48 and never knows.
That slack is **internal fragmentation** — bytes allocated but never
usable — and it's the deliberate price of never manufacturing
degenerate blocks. Every real allocator makes this exact trade; only
the threshold varies. (The slack isn't lost forever, either: it
travels with the block and returns to the free pool when the block is
freed.)

## Coalescing: putting the arena back together

Splitting is entropy: blocks only ever get smaller. Run any real
workload with splitting alone and the arena decays into confetti —
plenty of free bytes, none of them contiguous, every large request
failing. **External fragmentation** is the allocator's characteristic
disease, and **coalescing** is the treatment: when a block is freed,
merge it with any *adjacent* free block into one bigger block.

Merging with the block *after* you is easy in our layout — you can
compute where it lives:

```
before:  [hdr size=40 free=1][40 bytes][hdr size=104 free=1][104 bytes]
after:   [hdr size=152 free=1][      152 bytes                        ]
```

The freed block absorbs its neighbor's header and payload:
`40 + 8 + 104 = 152`. The neighbor's old header is now just payload
bytes — dead metadata that stopped mattering the moment the walk can
no longer reach it.

Merging with the block *before* you is the hard direction: from a
header you can compute *forward*, but nothing tells you where the
previous header starts — sizes only chain left to right. Real
allocators solve this with Knuth's boundary tags (a size copy at the
end of each block, so `p - 8` reads the *previous* block's size and
one subtraction finds its header — O(1) coalescing in both
directions). DuckOS takes the simpler road that our implicit,
address-ordered layout offers: after marking a block free, make **one
full pass over the whole arena**, merging every adjacent free pair as
the walk encounters it. A left-to-right sweep that keeps merging the
current block with its successor until the successor is allocated (or
the arena ends) fixes *every* mergeable pair in one pass — including
the freed-block-and-its-predecessor case, because the sweep reaches
the predecessor first and merges forward from there. It's O(arena)
per free instead of O(1), which no production kernel would accept,
but it is *obviously correct* — and in this course, when simplicity
and speed fight over a teaching allocator, simplicity wins.

Coalescing in place — the freed header (amber) grows to absorb its free neighbor; the greyed-out middle header simply stops being a header:

```d2
direction: right

merged: "one free block: size=152" {
  grid-columns: 4
  grid-gap: 0

  h1: "hdr\nsize 40 → 152\nfree=1" {
    style.stroke: "#d97706"
    style.stroke-width: 3
  }
  p1: "payload\n40 bytes"
  h2: "old hdr\nsize=104" {
    style.stroke-dash: 4
    style.font-color: "#9ca3af"
  }
  p2: "payload\n104 bytes"
}
```

## The bugs this design invites

The header trick has a dark side: the allocator's metadata lives in
the same address space as the caller's data, guarded by nothing but
arithmetic and good manners. Two classic failures follow.

**Double free.** `kfree(p)` twice in a row looks harmless here — the
block is marked free, then marked free again. The disaster needs one
more step: between the two frees, someone else `kmalloc`s and receives
that same block. The second `kfree` now frees *live* memory out from
under its new owner, and the block gets handed out yet again — two
subsystems now scribbling on the same bytes, each corrupting the other
at a distance. The crash, when it finally comes, is nowhere near the
bug. Production allocators cheapen the detection: glibc aborts with
`double free or corruption` when a block being freed is already on the
free list.

**Writing past the end.** The caller of `kmalloc(40)` who writes 44
bytes doesn't fault — the paging unit from *Paging and Virtual
Memory* protects page boundaries, not allocation boundaries. Those 4
extra bytes land on the *next block's header* and overwrite its
`size` field. Nothing happens immediately; the wreckage is discovered
later, by an innocent party, when some walk computes
`next = hdr + 8 + size` with a garbage size and hops into the middle
of somebody's payload — which it then trustingly interprets as a
header. Say the overflow wrote ASCII `"AAAA"`: the next hop lands
`0x41414141` bytes away, far outside the arena. Our walks are bounded
by `heap + HEAP_SIZE`, so DuckOS stops rather than wandering into the
weeds — but the accounting is ruined and every block past the trample
point is lost.

Real kernels, unable to outlaw these bugs, invest heavily in making
them *loud*. Linux's allocator can fill freed memory with the poison
byte `0x6b` — so a use-after-free read returns the unmistakable
pattern `0x6b6b6b6b` instead of plausible stale data — and brackets
allocations with **redzones**, guard bytes holding a known pattern
that's verified on free: if the pattern changed, someone wrote past
their end, and the kernel says so *at the free*, close to the
culprit. The heavyweight tool is KASAN (the Kernel Address
Sanitizer): the compiler instruments every load and store to first
check a shadow map recording which bytes are legally addressable, so
an out-of-bounds write is caught at the *instant it executes*, with a
stack trace of the guilty code — not hours later when the walk trips
over the corpse. All three are the same idea at increasing cost:
convert "silent corruption discovered by an innocent" into "immediate
report naming the guilty."

Now build the allocator they're all protecting.

## Challenge: kmalloc {#kmalloc points=30}

Implement DuckOS's kernel heap: a first-fit allocator with splitting,
and a coalescing `kfree`, over a 64 KiB static arena — plus the two
introspection helpers the rest of the kernel (and the tests) use to
watch it work.

The layout is the lesson's implicit list. Blocks sit head-to-tail in
the arena: `[struct block][payload][struct block][payload]...` — no
next pointer; the next header lives at
`(uint8_t *)hdr + sizeof(struct block) + hdr->size`, and every walk
stops at the arena's end.

The contract, function by function:

- `void kheap_init(void)` — reset the heap: write one header at the
  start of the arena making the whole thing a single free block
  (`size = HEAP_SIZE - sizeof(struct block)`, `free = 1`). Tests call
  this before every scenario; it must fully reset prior state.
- `void *kmalloc(uint32_t n)` — `n == 0` returns NULL. Reject any
  request larger than the arena *before* rounding (the round-up of a
  huge `n` wraps to a small number — see the lesson). Round `n` up to
  a multiple of `ALIGN`. First-fit: walk from the arena start, take
  the first free block with `size >= n`. Split it when the remainder
  can hold a header plus at least `ALIGN` payload bytes
  (`size - n >= sizeof(struct block) + ALIGN`); otherwise allocate
  the whole block. Return the payload pointer (always 8-aligned), or
  NULL if nothing fits.
- `void kfree(void *p)` — NULL is a no-op. Otherwise find the header
  at `p - sizeof(struct block)`, mark it free, then make one full
  left-to-right pass over the arena merging every adjacent pair of
  free blocks (keep merging a free block with its successor until the
  successor is allocated or the arena ends, then move on).
- `uint32_t kheap_free_bytes(void)` — the sum of all free blocks'
  payload sizes. Headers never count.
- `uint32_t kheap_largest_free(void)` — the payload size of the
  single largest free block (0 if none).

What the tests do: verify a fresh heap is one `HEAP_SIZE - 8`-byte
free block; check returned pointers are 8-aligned and that an
allocation is billed at payload + one header (and that 33 is billed
as 40 + 8); allocate neighbors, fill them with different byte
patterns, and check neither bled into the other; free the middle
block of three and demand the next same-size `kmalloc` reuse exactly
that hole (address equality — that's first-fit); fill the arena
completely, free two adjacent blocks, and expect one merged hole of
`96 + 8 + 96` bytes that a `kmalloc(200)` reuses; build three
non-adjacent 8000-byte holes and demand `kmalloc(12000)` fail even
though 16000 bytes are free (fragmentation is real, and the test is
named for it); check exact fits and too-small tails don't split;
`kmalloc(0)` is NULL, `kfree(NULL)` changes nothing, oversized and
wrap-around requests fail; and finally exhaust the arena, free
everything, and allocate it all again.

### Starter

```c
#include <stddef.h>
#include <stdint.h>

#define HEAP_SIZE 65536
#define ALIGN 8

/* Every block starts with this header; its payload follows
   immediately.  Blocks are laid head-to-tail in the arena:

	[struct block][payload][struct block][payload]...

   There is no next pointer: the following block's header lives at
	(uint8_t *)hdr + sizeof(struct block) + hdr->size
   and every walk stops at heap + HEAP_SIZE. */
struct block {
	uint32_t size;	/* payload bytes in this block (multiple of ALIGN) */
	uint32_t free;	/* 1 = on the free path */
};

/* The arena.  _Alignas keeps the first payload (heap + 8) 8-aligned;
   headers and payload sizes being multiples of 8 keeps every later
   payload aligned too. */
static _Alignas(ALIGN) uint8_t heap[HEAP_SIZE];

/* Reset the heap: one free block spanning the whole arena. */
void kheap_init(void) {
	/* TODO: write one header at the start of the arena:
	   size = HEAP_SIZE - sizeof(struct block), free = 1. */
	(void)heap;
}

/* Allocate n bytes.
   - n == 0: return NULL.
   - Reject n > HEAP_SIZE BEFORE rounding (round-up would wrap).
   - Round n up to a multiple of ALIGN.
   - First fit: take the first free block with size >= n.
   - Split when the leftover could hold a header plus at least ALIGN
     payload bytes; otherwise hand over the whole block.
   Returns the payload pointer (8-aligned), or NULL if no fit. */
void *kmalloc(uint32_t n) {
	/* TODO */
	(void)n;
	return NULL;
}

/* Free the block whose payload is p.  NULL is a no-op.  Mark the
   block free, then make one full pass over the arena merging every
   adjacent pair of free blocks (keep merging a free block with its
   successor until the successor is allocated or the arena ends). */
void kfree(void *p) {
	/* TODO */
	(void)p;
}

/* Sum of all free blocks' payload bytes (headers don't count). */
uint32_t kheap_free_bytes(void) {
	/* TODO */
	return 0;
}

/* Payload size of the largest single free block (0 if none). */
uint32_t kheap_largest_free(void) {
	/* TODO */
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define HEAP_SIZE 65536
#define ALIGN 8

struct block {
	uint32_t size;	/* payload bytes in this block (multiple of ALIGN) */
	uint32_t free;	/* 1 = on the free path */
};

void kheap_init(void);
void *kmalloc(uint32_t n);
void kfree(void *p);
uint32_t kheap_free_bytes(void);
uint32_t kheap_largest_free(void);

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
	uint32_t full = HEAP_SIZE - (uint32_t)sizeof(struct block);

	/* A fresh heap is one giant free block. */
	kheap_init();
	check(kheap_free_bytes() == full, "test_init_one_free_block");
	check(kheap_largest_free() == full, "test_init_largest_spans_arena");

	/* Basic allocation: aligned, non-NULL, billed payload + header. */
	kheap_init();
	void *p1 = kmalloc(40);
	check(p1 != NULL && (uintptr_t)p1 % ALIGN == 0,
	      "test_alloc_aligned_non_null");
	check(kheap_free_bytes() == full - 40 - (uint32_t)sizeof(struct block),
	      "test_alloc_billed_payload_plus_header");

	void *p2 = kmalloc(40);
	check(p1 != NULL && p2 != NULL &&
	      (uintptr_t)p2 >= (uintptr_t)p1 + 40 + sizeof(struct block),
	      "test_two_allocs_disjoint_and_ordered");

	/* Neighboring allocations must not overlap: write full patterns
	   to both, then verify each survived intact. */
	{
		int ok = 0;
		if (p1 != NULL && p2 != NULL) {
			memset(p1, 0xAA, 40);
			memset(p2, 0x55, 40);
			ok = 1;
			for (int i = 0; i < 40; i++) {
				if (((uint8_t *)p1)[i] != 0xAA ||
				    ((uint8_t *)p2)[i] != 0x55)
					ok = 0;
			}
		}
		check(ok, "test_neighbor_writes_do_not_bleed");
	}

	/* Requests round up to a multiple of ALIGN: 33 is billed as 40. */
	kheap_init();
	uint32_t before = kheap_free_bytes();
	void *pr = kmalloc(33);
	check(pr != NULL &&
	      before - kheap_free_bytes() == 40 + (uint32_t)sizeof(struct block),
	      "test_request_rounds_up_to_align");

	/* Free the middle of three; first-fit must reuse exactly that
	   hole for the next same-size request (address equality). */
	kheap_init();
	void *a = kmalloc(64);
	void *b = kmalloc(64);
	void *c = kmalloc(64);
	kfree(b);
	void *r = kmalloc(64);
	check(a != NULL && b != NULL && c != NULL && r == b,
	      "test_freed_middle_hole_reused");

	/* An exact fit consumes the hole without splitting: free bytes
	   drop by exactly the payload, no extra header appears. */
	kheap_init();
	a = kmalloc(64);
	b = kmalloc(64);
	c = kmalloc(64);
	kfree(b);
	uint32_t fb = kheap_free_bytes();
	r = kmalloc(64);
	check(b != NULL && c != NULL && r == b &&
	      kheap_free_bytes() == fb - 64,
	      "test_exact_fit_no_split");

	/* A tail too small for header + ALIGN is not split off: a 56-byte
	   request from a 64-byte hole takes all 64 (remainder 8 < 16). */
	kheap_init();
	a = kmalloc(64);
	b = kmalloc(64);
	c = kmalloc(64);
	kfree(b);
	fb = kheap_free_bytes();
	r = kmalloc(56);
	check(b != NULL && c != NULL && r == b &&
	      kheap_free_bytes() == fb - 64,
	      "test_no_split_when_tail_too_small");

	/* Coalescing: fill the arena completely, then free two adjacent
	   blocks; they must merge into one hole spanning both payloads
	   plus the header between them, and be reused as one. */
	kheap_init();
	a = kmalloc(96);
	b = kmalloc(96);
	c = kmalloc(96);
	void *rest = kmalloc(kheap_largest_free());
	kfree(a);
	kfree(b);
	check(a != NULL && b != NULL && c != NULL && rest != NULL &&
	      kheap_largest_free() == 96 + sizeof(struct block) + 96,
	      "test_coalesce_with_next");
	void *big = kmalloc(200);
	check(a != NULL && big == a, "test_coalesced_hole_reused");

	/* Fragmentation is real: three non-adjacent 8000-byte holes make
	   16000 free bytes, but a 12000-byte request must still fail. */
	kheap_init();
	void *f1 = kmalloc(8000);
	void *f2 = kmalloc(8000);
	void *f3 = kmalloc(8000);
	rest = kmalloc(kheap_largest_free());
	kfree(f1);
	kfree(f3);
	check(f1 != NULL && f2 != NULL && f3 != NULL && rest != NULL &&
	      kheap_free_bytes() == 16000 && kheap_largest_free() == 8000 &&
	      kmalloc(12000) == NULL,
	      "test_fragmentation_is_real");

	/* Degenerate arguments. */
	kheap_init();
	check(kmalloc(0) == NULL, "test_kmalloc_zero_is_null");
	uint32_t fb0 = kheap_free_bytes();
	kfree(NULL);
	check(kheap_free_bytes() == fb0, "test_kfree_null_is_noop");
	check(kmalloc(HEAP_SIZE) == NULL, "test_oversize_request_fails");
	check(kmalloc(0xFFFFFFFFu) == NULL, "test_huge_request_does_not_wrap");

	/* Exhaust the arena, then recover all of it. */
	kheap_init();
	void *all = kmalloc(full);
	check(all != NULL, "test_alloc_entire_arena");
	check(kmalloc(8) == NULL, "test_exhausted_arena_returns_null");
	kfree(all);
	check(kheap_free_bytes() == full && kmalloc(full) != NULL,
	      "test_exhaust_and_recover");

	return failed;
}
```

# Lesson: Processes: the Kernel's Bookkeeping {#processes}

Every program on your machine believes it owns the computer: a CPU all
to itself, registers nobody else touches, a stack that stays put. All of
it is a lie, and the kernel is the liar. There is one CPU (on our
1991-vintage target, exactly one), and the kernel rations it out in
slices a few milliseconds long, swapping one program's world out and
another's in so fast that each believes it never stopped.

A **process** is the unit of that lie: a running program plus everything
needed to freeze it mid-instruction and later resume it so perfectly it
cannot tell it was paused. Strip away the mystique and a process is
three things: **saved registers** (the *context* — where it was
executing, its stack pointer, its flags; dissected below), **an address
space** (the page directory we built in *Paging and Virtual Memory*),
and **bookkeeping** (identity, state, parentage — pure accounting, no
hardware in sight).

Where the three meet is the **process table**, and it is no exaggeration
to call it *the* central data structure of the kernel. Nearly every
chapter ahead is secretly about one of its fields: the scheduler picks
among its RUNNABLE entries (*Scheduling*), IPC blocks entries in SENDING
and RECEIVING (*Message Passing — the Microkernel Heart*), the clock
wakes its SLEEPING ones (*The Clock Ticks*), and `wait` reaps its
ZOMBIEs (*Birth, Death, and Zombies*). Get this structure right and the
rest of the kernel has somewhere to live.

## A fixed array, on purpose

Minix declares its process table as a compile-time-sized array of
`struct proc`, its length fixed by the constant `NR_PROCS` — no linked
list, no allocation, no growth. DuckOS follows suit:

```c
#define NPROC 16

struct proc proc_table[NPROC];
```

Sixteen processes, forever. Linux, by contrast, allocates a
`task_struct` per process and links it into dynamic lists; tens of
thousands run happily. The trade is worth stating honestly. The fixed
array cannot fail: creating a process never calls an allocator, so it
cannot die out-of-memory halfway through, and code running in awkward
contexts — an interrupt handler waking a sleeper, the scheduler
mid-switch — can walk the table with no locks against an allocator and
no fear it moves. (We built `kmalloc` in *The Kernel Heap*; notice that
nothing in this lesson uses it. Keeping allocation out of the kernel's
hottest paths is a design position, not an accident.) A slot index is
also a perfect handle: it fits in an `int`, never dangles, and can be
stored in other tables without reference counting — Minix's IPC
addresses processes by slot number for exactly this reason. The price is
a hard ceiling and wasted slack: the seventeenth `fork` fails no matter
how much RAM is free, and sixteen slots sit in memory whether two
processes exist or sixteen. Linux pays complexity — allocation, locking,
RCU-protected lists — to remove the ceiling, because a general-purpose
OS must. For a teaching kernel the honest choice is Minix's: every
algorithm ahead becomes "a loop over 16 array entries," which is exactly
the transparency we want.

## The canonical `struct proc`

Later lessons bolt on fields (a message buffer, a wakeup tick, a
priority), but three are the skeleton everything hangs from. In a real
kernel this array IS the master record of the machine's work; here we
build it in a struct the tests can read:

```c
enum proc_state {
	PROC_UNUSED,	/* slot free — not a process at all */
	PROC_EMBRYO,	/* being created; not yet runnable */
	PROC_RUNNABLE,	/* ready: would run if given the CPU */
	PROC_RUNNING,	/* executing on the CPU right now */
	PROC_SENDING,	/* blocked: waiting for receiver (IPC) */
	PROC_RECEIVING,	/* blocked: waiting for a message (IPC) */
	PROC_SLEEPING,	/* blocked: waiting for a clock tick */
	PROC_ZOMBIE	/* exited; waiting for parent to reap */
};

struct proc {
	int pid;		/* public identity; 0 means none yet */
	enum proc_state state;
	int parent;		/* slot index of parent, -1 = none */
};
```

Note `parent` is a *slot index*, not a pid — inside the kernel, slots
are the native name for a process (they index straight into the array).
Pids are for the outside world; we draw that line sharply below.

## The state machine

A process is always in exactly one state, and only a few transitions are
legal:

The amber state is the one at most one process can occupy per CPU;
every blocked state funnels back through RUNNABLE, never straight to
RUNNING:

```d2
grid-columns: 4
grid-gap: 90

UNUSED
EMBRYO
RUNNABLE
RUNNING: RUNNING {
  style.stroke: "#d97706"
  style.stroke-width: 3
}
ZOMBIE
SENDING
RECEIVING
SLEEPING

UNUSED -> EMBRYO: pt_alloc
EMBRYO -> RUNNABLE: creation done
RUNNABLE -> RUNNING: dispatch
RUNNING -> RUNNABLE: quantum expires
RUNNING -> SENDING: send blocks
RUNNING -> RECEIVING: receive blocks
RUNNING -> SLEEPING: sleep(n)
SENDING -> RUNNABLE: partner receives
RECEIVING -> RUNNABLE: message arrives
SLEEPING -> RUNNABLE: timer fires
RUNNING -> ZOMBIE: exit
ZOMBIE -> UNUSED: parent reaps (wait)
```

Who sets each state, and why:

- **UNUSED** — set by `pt_init` at boot and by `wait` when a parent
  reaps a dead child. The only state `pt_alloc` will claim.
- **EMBRYO** — set by `pt_alloc` the instant a slot is claimed. It
  closes a race: the slot is taken (a second `fork` cannot grab it) but
  its context and address space are still being built, so the scheduler
  must not dispatch it yet.
- **RUNNABLE** — set when creation finishes and on every wakeup: "give
  me the CPU and I will make progress." Choosing among these is the
  whole of *Scheduling*, next lesson.
- **RUNNING** — set by the scheduler on dispatch. One CPU, so at most
  one slot is ever RUNNING.
- **SENDING / RECEIVING** — set by the IPC primitives when a process
  blocks mid-send (no receiver waiting) or mid-receive (no message has
  arrived). The soul of a microkernel; their lesson is *Message
  Passing — the Microkernel Heart*.
- **SLEEPING** — set when a process pauses itself for some number of
  clock ticks; the timer machinery of *The Clock Ticks* moves it back
  to RUNNABLE when its tick arrives.
- **ZOMBIE** — set by `exit`. The process is dead, but the slot cannot
  be freed yet: the parent has the right to ask how it died, so the
  corpse holds the exit status until `wait` collects it (*Birth, Death,
  and Zombies*).

One rule hides in that list: **nobody frees a slot except the parent's
`wait`**. If `exit` went straight to UNUSED, the exit status would have
nowhere to live, and a concurrent `fork` could recycle the slot while
the parent still held its number.

## Slots versus pids

A process now has two names. The **slot** is the array index, 0 to
`NPROC - 1`: stable for the process's whole life, and how the kernel
names processes internally — but *recycled* the moment its occupant is
reaped. The **pid** is the public identity: a positive integer handed
out from a monotonically increasing counter, `next_pid`. Slot 5 may be
reused; its new tenant gets a new pid.

Why not slot numbers everywhere? Because of a race as old as Unix.
Suppose pids were recycled as eagerly as slots:

```
shell:  runs your program as pid 7
you:    decide to kill it:  kill(7)   ...but you're slow to type
pid 7:  exits on its own
kernel: recycles slot AND pid 7 for the mail daemon
you:    press enter — kill(7) murders the mail daemon
```

The `kill` was aimed at a name, and the name changed owners between aim
and fire. Monotonic pids shrink the window from "whenever the slot is
reused" to "after 30,000 more process creations" — in practice never on
a machine this size. (The stale-name race still *exists*; pid reuse
after wraparound is why modern systems eventually grew pidfds.)

Two details make the counter honest, and both are in Minix. First,
**wraparound**: pids are small ints by tradition — Minix caps them at
30000, and DuckOS adopts `PID_MAX 30000` — and past the cap the counter
wraps back to 1, never 0 (pid 0 is reserved by convention, and a zeroed
table entry must not look like a real pid). Second, **skip the living**:
after a wrap, the counter's next value might belong to a process that
has been alive the whole time, and a duplicate pid would make `kill` and
`wait` ambiguous forever after — so allocation scans the table and skips
any candidate pid a live slot still holds. With at most `NPROC` pids in
use and 30,000 to choose from, the skip loop terminates fast. You will
implement exactly this — lowest free slot, monotonic counter, wrap,
skip — in the first challenge.

## What a context actually is

When the kernel pauses a process, the CPU state it must save is smaller
than you might think — because the compiler already does half the work.
The i386 C calling convention (**cdecl**, the System V i386 ABI) splits
the general registers into two teams:

```
caller-saved:  EAX  ECX  EDX      (a call may destroy these)
callee-saved:  EBX  ESI  EDI  EBP (a function must preserve these)
```

Consider a **cooperative** switch: the process itself calls into the
kernel (a yield, a sleep), and down that call chain the kernel calls
`swtch(old, new)` to change stacks. From the process's point of view,
`swtch` is just a function call — so the compiler has *already spilled*
EAX, ECX, and EDX anywhere it cared about their values. The minimum the
switch must save by hand is the callee-saved team, plus the one register
that says where to resume:

```c
struct cpu_context {	/* what the switch code saves/restores */
	uint32_t edi, esi, ebx, ebp;	/* callee-saved (cdecl) */
	uint32_t eip;	/* return address the switch "returns" to */
};
```

Where is ESP? Implicit: real switch code pushes this struct onto the
outgoing process's own stack, so the struct's address *is* the saved
stack pointer. And nobody writes `eip` explicitly — it is the return
address that `call swtch` pushed. Restoring is then tiny (xv6's version
is six instructions): point ESP at the new context, pop
EDI/ESI/EBX/EBP, `ret` — and the `ret` pops the saved EIP, resuming the
new process exactly where it once called `swtch`.

An **involuntary** pause is bigger. When the timer interrupt ends a
quantum, the process wasn't calling anything — EAX might hold half an
evaluated expression, so nothing is pre-saved. The hardware itself
pushes the critical minimum before the handler runs, and `iret` pops it
on the way out:

```c
struct trap_frame {	/* what iret pops, innermost last */
	uint32_t eip;		/* where the process was executing */
	uint32_t cs;		/* its code segment (ring in low 2 bits) */
	uint32_t eflags;	/* its flags — including IF, bit 9 */
	uint32_t esp;		/* its stack pointer  (pushed only on   */
	uint32_t ss;		/* its stack segment   ring transition) */
};
```

EIP, CS, and EFLAGS are always pushed; ESP and SS join them only when
the interrupt crossed a privilege boundary (user code interrupted into
the ring-0 kernel must get the kernel's stack, so the user's must be
remembered). The handler's entry stub pushes the general registers on
top — the frame is the hardware's half of the bargain, as we saw in
*Interrupts and the IDT*. So "the context" is two nested layers: a trap
frame that gets a process in and out of the kernel, and a `cpu_context`
that switches between kernel stacks. In a real kernel both live on the
process's kernel stack; here they are plain structs the tests can read.

## The beautiful trick: forging a frame for a process that never ran

Restoring a context means popping registers that were saved when the
process last ran. A brand-new process has never run: there is nothing
to restore. How does it ever start?

You cheat. You **forge the saved state** — build by hand, in memory,
exactly the bytes that *would* have been saved had the process been
interrupted at its very first instruction. Forge a `cpu_context` whose
`eip` is the entry function and whose callee-saved registers are zero
(a new process has no history to preserve); then do nothing special at
all — just let the ordinary switch code "restore" the forgery. Its
`ret` pops your forged `eip`, and the CPU is suddenly *returning into
code that has never been called*. To go all the way to user mode, forge
the outer layer too: a trap frame claiming the process was
"interrupted" at its first user instruction, with ring-3 selectors and
a fresh user stack. Run the normal interrupt-return path, and `iret`
dutifully "resumes" a program that never existed until now, dropping to
ring 3 in the same step.

Spell the trick out, because it is the heart of every kernel you will
ever read: **process creation is impersonating a past that never
happened, so that the ordinary resume path becomes the launch path.**
No special first-run code, no flag saying "this one's new." xv6 does
this, Minix does this, Linux does this. Hunt a kernel for the starter
motor and you will find a forger.

## The bug that ships in every first kernel

One forged field has ended more hobby-kernel careers than any other:
EFLAGS. Bit 9 of EFLAGS is **IF, the interrupt flag** — the CPU
delivers interrupts only while it is 1:

```
EFLAGS:  ... 11 10  9  8  7  6  5  4  3  2  1  0
                    IF          = 0x200
```

Forge the frame the lazy way — `eflags = 0` — and everything *appears*
to work: the `iret` loads your forged flags, the first process starts,
prints its first output... and the machine is now sealed shut. IF is 0,
so the timer interrupt from *Interrupts and the IDT* is never
delivered, so the scheduler never runs again, so the first process
keeps the CPU forever. Nothing crashes; no fault fires; the kernel
quietly becomes a single-program loader. And because the visible
symptom is "my second process never starts," people debug the scheduler
and the process table — everywhere except the one zeroed field in the
forge. It is a rite-of-passage bug. The fix is one constant — forge
`eflags = EFLAGS_IF`, so every process is born with interrupts
enabled — and the lesson is permanent: *a forged frame is trusted
absolutely, so every field you forge is policy.*

Time to build both halves: the table, then the forge.

## Challenge: The Process Table {#proc-table points=15}

Implement DuckOS's process table: initialization, slot allocation with
monotonic pid assignment, lookup by pid, and a state census.

The contract, precisely:

- `pt_init(pt)` — mark all `NPROC` slots `PROC_UNUSED`, set `next_pid`
  to 1.
- `pt_alloc(pt, parent_slot)` — find the **lowest-indexed** UNUSED
  slot; if none, return -1 and change nothing. Otherwise assign the
  next free pid: starting from `next_pid`, skip any candidate currently
  held by a live slot (any slot whose state is not `PROC_UNUSED`),
  wrapping from `PID_MAX` back to 1. Store the pid, set state
  `PROC_EMBRYO`, set `parent` to `parent_slot`, leave `next_pid` one
  past the issued pid (wrapping the same way), and return the slot.
- `pt_find_pid(pt, pid)` — the slot whose live entry has this pid, or
  -1. UNUSED slots never match, whatever stale pid they hold.
- `pt_count(pt, s)` — how many slots are in state `s`.

The tests plant and check: a fresh table is all-UNUSED; the first
allocation returns slot 0 with pid 1; pids are sequential; a full table
returns -1; freeing a middle slot (by setting it UNUSED) makes the next
allocation reuse that slot but with a fresh pid; lookup ignores UNUSED
slots; with `next_pid = PID_MAX`, pid 30000 is issued and the counter
then wraps to 1; and after a wrap, a pid still held by a live slot is
skipped.

### Starter

```c
#include <stddef.h>

#define NPROC   16
#define PID_MAX 30000

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;		/* public identity; issued by pt_alloc */
	enum proc_state state;	/* PROC_UNUSED means the slot is free */
	int parent;		/* slot index of parent, -1 = none */
};

struct proc_table {
	struct proc procs[NPROC];
	int next_pid;		/* next candidate pid, 1..PID_MAX */
};

/* Mark every slot PROC_UNUSED; pid numbering starts at 1. */
void pt_init(struct proc_table *pt)
{
	/* TODO: clear all NPROC slots to PROC_UNUSED, set next_pid = 1 */
	(void)pt;
}

/*
 * Claim the lowest UNUSED slot for a new process: assign the next free
 * pid (skipping pids held by live slots, wrapping PID_MAX -> 1), set
 * state PROC_EMBRYO and parent, and return the slot index.
 * Returns -1 if the table is full.
 */
int pt_alloc(struct proc_table *pt, int parent_slot)
{
	/* TODO: find lowest UNUSED slot (or return -1);
	 * pick a pid: start at next_pid, skip any pid a live slot holds,
	 * wrap to 1 after PID_MAX; advance next_pid one past the issued
	 * pid (same wrap); fill in the slot and return its index. */
	(void)pt;
	(void)parent_slot;
	return -1;
}

/* Slot index of the live process with this pid, or -1 if none.
 * UNUSED slots never match. */
int pt_find_pid(const struct proc_table *pt, int pid)
{
	/* TODO */
	(void)pt;
	(void)pid;
	return -1;
}

/* Number of slots currently in state s. */
int pt_count(const struct proc_table *pt, enum proc_state s)
{
	/* TODO */
	(void)pt;
	(void)s;
	return 0;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

#define NPROC   16
#define PID_MAX 30000

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;
	enum proc_state state;
	int parent;
};

struct proc_table {
	struct proc procs[NPROC];
	int next_pid;
};

void pt_init(struct proc_table *pt);
int pt_alloc(struct proc_table *pt, int parent_slot);
int pt_find_pid(const struct proc_table *pt, int pid);
int pt_count(const struct proc_table *pt, enum proc_state s);

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
	struct proc_table pt;
	int i, s, ok;

	/* init: every slot UNUSED, pid counter starts at 1 */
	memset(&pt, 0xFF, sizeof pt);	/* poison so init must do the work */
	pt_init(&pt);
	ok = 1;
	for (i = 0; i < NPROC; i++)
		if (pt.procs[i].state != PROC_UNUSED)
			ok = 0;
	check(ok, "test_init_all_slots_unused");
	check(pt.next_pid == 1, "test_init_next_pid_is_1");

	/* first allocations: lowest slot, sequential pids, EMBRYO, parent */
	memset(&pt, 0, sizeof pt);
	pt_init(&pt);
	s = pt_alloc(&pt, -1);
	check(s == 0 && pt.procs[0].pid == 1, "test_first_alloc_slot_0_pid_1");
	check(pt.procs[0].state == PROC_EMBRYO && pt.procs[0].parent == -1,
	      "test_first_alloc_embryo_parent_none");
	s = pt_alloc(&pt, 0);
	check(s == 1 && pt.procs[1].pid == 2 && pt.procs[1].parent == 0,
	      "test_sequential_pids");

	/* fill the table: 17th allocation fails */
	memset(&pt, 0, sizeof pt);
	pt_init(&pt);
	ok = 1;
	for (i = 0; i < NPROC; i++)
		if (pt_alloc(&pt, -1) != i)
			ok = 0;
	check(ok, "test_fill_table_slots_in_order");
	check(pt_alloc(&pt, -1) == -1, "test_full_table_returns_minus_1");

	/* free a middle slot by hand: realloc reuses it with a FRESH pid */
	pt.procs[5].state = PROC_UNUSED;
	s = pt_alloc(&pt, -1);
	check(s == 5 && pt.procs[5].pid == 17,
	      "test_realloc_lowest_slot_fresh_pid");

	/* lookup by pid */
	memset(&pt, 0, sizeof pt);
	pt_init(&pt);
	pt_alloc(&pt, -1);		/* pid 1 in slot 0 */
	pt_alloc(&pt, 0);		/* pid 2 in slot 1 */
	check(pt_find_pid(&pt, 2) == 1, "test_find_pid_live");
	check(pt_find_pid(&pt, 999) == -1, "test_find_pid_missing");
	pt.procs[3].pid = 42;		/* stale pid in an UNUSED slot */
	check(pt_find_pid(&pt, 42) == -1, "test_find_pid_ignores_unused");

	/* wraparound: PID_MAX is issued, then the counter wraps to 1 */
	memset(&pt, 0, sizeof pt);
	pt_init(&pt);
	pt.next_pid = PID_MAX;
	s = pt_alloc(&pt, -1);
	check(s == 0 && pt.procs[0].pid == PID_MAX, "test_pid_max_is_issued");
	pt.procs[0].state = PROC_UNUSED;	/* pid 1 not in use */
	s = pt_alloc(&pt, -1);
	check(s == 0 && pt.procs[0].pid == 1, "test_pid_wraps_to_1");

	/* after a wrap, pids held by live slots are skipped */
	memset(&pt, 0, sizeof pt);
	pt_init(&pt);
	pt_alloc(&pt, -1);		/* pid 1 stays alive in slot 0 */
	pt.next_pid = PID_MAX;
	pt_alloc(&pt, -1);		/* issues PID_MAX in slot 1 */
	s = pt_alloc(&pt, -1);		/* wraps; 1 is taken -> expect 2 */
	check(s == 2 && pt.procs[2].pid == 2, "test_wrap_skips_pid_in_use");

	/* census */
	memset(&pt, 0, sizeof pt);
	pt_init(&pt);
	pt_alloc(&pt, -1);
	pt_alloc(&pt, 0);
	pt_alloc(&pt, 0);
	pt.procs[1].state = PROC_RUNNABLE;
	check(pt_count(&pt, PROC_EMBRYO) == 2 &&
	      pt_count(&pt, PROC_RUNNABLE) == 1 &&
	      pt_count(&pt, PROC_UNUSED) == NPROC - 3, "test_count_by_state");

	return failed;
}
```

## Challenge: Forge a First Frame {#context-frame points=10}

Build the forger from the prose above: initialize both layers of saved
state for a process that has never run, so the ordinary resume paths
(`ret` for the kernel switch, `iret` for the drop to user mode) launch
it.

The contract, precisely:

- `context_init(c, entry)` — forge a kernel switch context: all four
  callee-saved registers (`edi`, `esi`, `ebx`, `ebp`) zero — a new
  process has no history to preserve — and `eip = entry`, so the switch
  code's `ret` "returns" into the entry point.
- `trap_frame_init_user(f, entry, user_stack_top)` — forge the frame
  `iret` pops to enter user mode for the first time: `eip = entry`,
  `cs = UCODE_SEL`, `ss = UDATA_SEL`, `esp = user_stack_top`, and
  `eflags = EFLAGS_IF` — interrupts ON. Remember the sealed-shut
  machine from the prose: forge `eflags` as 0 and the first timer tick
  never arrives.

The tests poison both structs and then check every field (so every
field must be written), verify the IF bit is set in the forged EFLAGS,
and assert that both user selectors carry requested privilege level 3
in their low two bits — the forged frame is what actually drops the CPU
to ring 3.

### Starter

```c
#include <stdint.h>

struct cpu_context {          /* what the switch code saves/restores */
	uint32_t edi, esi, ebx, ebp;   /* callee-saved (cdecl) */
	uint32_t eip;                  /* return address the switch "returns" to */
};

struct trap_frame {           /* what iret pops, innermost last */
	uint32_t eip; uint32_t cs; uint32_t eflags; uint32_t esp; uint32_t ss;
};

#define EFLAGS_IF 0x200		/* bit 9: interrupts enabled */
#define KCODE_SEL 0x08		/* kernel code selector (ring 0) */
#define UCODE_SEL 0x1B		/* user code selector (ring 3) */
#define UDATA_SEL 0x23		/* user data/stack selector (ring 3) */

/* Forge the kernel switch context for a never-run process: zero the
 * callee-saved registers, point eip at the entry function. */
void context_init(struct cpu_context *c, uint32_t entry)
{
	/* TODO */
	(void)c;
	(void)entry;
}

/* Forge the trap frame that iret pops to enter user mode for the
 * first time: entry point, user code/stack selectors, the given user
 * stack top — and interrupts ENABLED, or the first tick never comes. */
void trap_frame_init_user(struct trap_frame *f, uint32_t entry,
                          uint32_t user_stack_top)
{
	/* TODO */
	(void)f;
	(void)entry;
	(void)user_stack_top;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

struct cpu_context {          /* what the switch code saves/restores */
	uint32_t edi, esi, ebx, ebp;   /* callee-saved (cdecl) */
	uint32_t eip;                  /* return address the switch "returns" to */
};

struct trap_frame {           /* what iret pops, innermost last */
	uint32_t eip; uint32_t cs; uint32_t eflags; uint32_t esp; uint32_t ss;
};

#define EFLAGS_IF 0x200
#define KCODE_SEL 0x08
#define UCODE_SEL 0x1B
#define UDATA_SEL 0x23

void context_init(struct cpu_context *c, uint32_t entry);
void trap_frame_init_user(struct trap_frame *f, uint32_t entry,
                          uint32_t user_stack_top);

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
	struct cpu_context c;
	struct trap_frame f;

	/* poison, then forge: every field must be written */
	memset(&c, 0xAA, sizeof c);
	context_init(&c, 0x00101234u);
	check(c.edi == 0 && c.esi == 0 && c.ebx == 0 && c.ebp == 0,
	      "test_context_callee_saved_zeroed");
	check(c.eip == 0x00101234u, "test_context_eip_is_entry");

	memset(&f, 0xAA, sizeof f);
	trap_frame_init_user(&f, 0x08048000u, 0xBFFFF000u);
	check(f.eip == 0x08048000u, "test_frame_eip_is_entry");
	check(f.esp == 0xBFFFF000u, "test_frame_esp_is_user_stack_top");
	check(f.cs == UCODE_SEL, "test_frame_cs_user_code");
	check(f.ss == UDATA_SEL, "test_frame_ss_user_data");
	check(f.eflags == EFLAGS_IF, "test_frame_eflags_exactly_if");
	check((f.eflags & EFLAGS_IF) != 0,
	      "test_frame_interrupts_enabled_at_birth");
	check((f.cs & 3) == 3 && (f.ss & 3) == 3,
	      "test_frame_selectors_request_ring_3");

	return failed;
}
```

The forged frame plus the table you built above are the complete raw
material of multiprogramming: slots that remember, and state that can
be resumed — or invented. All that's missing is the referee that
decides *which* RUNNABLE slot gets the CPU next, and for how long.
That is *Scheduling*, and it is next.

# Lesson: Scheduling {#scheduling}

DuckOS has one CPU and, thanks to *Processes: the Kernel's Bookkeeping*,
a table of up to `NPROC` processes that could all be `PROC_RUNNABLE` at
once. The scheduler answers exactly one question: **who runs next?** That
is the whole job. Everything else in this lesson is the uncomfortable
discovery that every possible answer is a *policy*, and every policy has
victims.

Consider the machine this course pretends to be: a 386 in 1991, running
a Unix-like system for one impatient developer. Two processes are
runnable. One is a compiler chewing through a kernel build — it would
happily use every cycle for the next ten minutes. The other is a text
editor that needs the CPU for a few hundred microseconds every time a key
goes down, and then goes back to sleep. Any policy that makes the
compile finish sooner does it by stealing cycles the editor wanted *right
now*; any policy that makes typing feel instant stretches out the build.
The two classic figures of merit pull in opposite directions:

- **Turnaround time** — how long until a job *finishes*. Batch metric.
  The compiler cares about this.
- **Response time** — how long until a runnable process *starts running*.
  Interactive metric. The editor cares about nothing else.

There is a provably optimal policy for average turnaround: **shortest
job first** (SJF) — always run the job with the least work remaining.
The intuition fits in four lines. Two jobs arrive together, A needs 10
seconds, B needs 1:

```
run A then B:  A done at t=10, B done at t=11   average = 10.5 s
run B then A:  B done at t=1,  A done at t=11   average =  6.0 s
```

Same total work, nearly half the average wait, just by putting the short
job first: a long job delays everyone behind it, a short job barely
does. Hold that thought — SJF has one fatal flaw (the kernel cannot see
the future and does not know which job is short), and the second half of
this lesson is a scheduler that fakes clairvoyance surprisingly well.

## Round-robin: the honest baseline

First, the simplest policy that is actually fair: **round-robin**. Keep
runnable processes in a FIFO queue. Pop the head, let it run for a fixed
slice of time called the **quantum**, and when the quantum expires, push
it on the tail and pop the next head. Every runnable process gets the
CPU at regular intervals, no process can hog it, and the mechanism is
two integers and an array.

What makes the quantum *expire* is the hardware timer. As *The Clock
Ticks* covers in detail, DuckOS programs the interval timer to interrupt
`HZ` times per second, with `#define HZ 100` — one **tick** every 10 ms.
The timer interrupt handler decrements the running process's remaining
quantum; at zero, it marks the process for preemption and the kernel
switches to the next one on the way out of the interrupt. So quanta are
measured in ticks: a 4-tick quantum is 40 ms of wall-clock CPU time.

Choosing the quantum length is a real engineering trade, and it is worth
doing the arithmetic instead of hand-waving:

- **Too short, and switching eats the machine.** A context switch is not
  free: save one process's registers, restore another's, switch address
  spaces, and — the hidden cost — run for a while with caches and TLB
  full of the *previous* process's data. Call it 30 µs of direct and
  indirect cost on our 386. With a 100 µs quantum, the machine spends
  30 of every 130 µs on overhead — 23% of the CPU gone, doing no one's
  work. With a 40 ms quantum the same 30 µs is 0.075%. Noise.
- **Too long, and typing feels like telnet to the moon.** Suppose ten
  processes are runnable and the quantum is 100 ms. After your editor
  uses its slice, it re-queues behind nine others: the next keystroke
  can wait 900 ms to echo. That is a round-trip within shouting distance
  of Earth–Moon light delay, and it feels exactly as remote. At a 40 ms
  quantum the worst case drops to 360 ms — still visible. Interactive
  feel degrades linearly with quantum × queue length.

Real systems land in the tens of milliseconds: long enough that switch
overhead vanishes, short enough that a human doesn't notice the queue.
Minix gave user processes 100 ms. DuckOS's round-robin baseline uses 4
ticks — 40 ms at `HZ 100`.

## The run queue is a ring

The data structure under all of this is almost embarrassingly small. A
run queue holds **proc-table slot numbers** — the integer indexes into
the `struct proc` table from *Processes: the Kernel's Bookkeeping* —
not pointers, and not PIDs. Slots are dense, bounded by `NPROC`, and
survive being written into fixed-size arrays; that is exactly why Minix
schedulers pass slot numbers around too.

Since at most `NPROC` processes exist, at most `NPROC` can be queued,
so the queue is a fixed array used as a **circular buffer**: a `head`
index marks the oldest entry, a `count` says how many are queued, and
the tail — where the next push lands — is computed, not stored:

```
tail index = (head + count) % NPROC
```

Pushing writes at the tail and bumps `count`. Popping reads at `head`,
advances `head` by one (wrapping modulo `NPROC`), and drops `count`.
Nothing is ever shifted or copied. After the queue has churned for a
while, the live region can straddle the end of the array:

```
index:     0     1     2     3          14    15
items:  [ 12 ][  3 ][  - ][  - ] ... [  - ][  9 ]
           |                                  |
           |    tail = (15 + 3) % 16 = 2      head = 15, count = 3
           |
pop order: 9 (index 15), then 12 (index 0), then 3 (index 1)
```

The entry at index 15 is *older* than the one at index 0 — the ring
wrapped. Both `head` and the computed tail chase each other around the
array forever, and FIFO order is preserved because order lives in the
`head`/`count` arithmetic, not in the array positions themselves.

One framing note before you build it, and it applies to everything in
this lesson: in a real kernel, the "pick next process" decision runs
inside the timer interrupt path, in the handful of instructions between
acknowledging the interrupt and returning to (someone's) user code. In
DuckOS the scheduler is the same C data structure it would be there,
but hosted: here the tests play the role of the clock, calling the tick
function where the timer interrupt would.

## Challenge: The Run Queue {#runqueue points=10}

Implement the circular FIFO of proc-table slots that every scheduler in
this course sits on.

The contract:

- `rq_init(q)` — make the queue empty.
- `rq_push(q, slot)` — append `slot` at the tail. Returns -1 if the
  queue is full (`count == NPROC`), else 0. The queue does not reject
  duplicates — it is a dumb ring; policy lives above it.
- `rq_pop(q)` — remove and return the oldest entry, or -1 if the queue
  is empty.
- `rq_contains(q, slot)` — 1 if `slot` is currently queued, else 0.

The tests check: a fresh queue pops -1; several pushes pop back in FIFO
order; pushing `NPROC` entries succeeds and one more returns -1; a
push/pop workload long enough to wrap `head` past the end of the array
still comes out in FIFO order; and `rq_contains` answers correctly
before and after the entry in question is popped.

### Starter

```c
#define NPROC 16

struct runqueue {
	int items[NPROC];	/* proc slots, FIFO */
	int head;		/* index of oldest entry */
	int count;
};

/* Make the queue empty. */
void rq_init(struct runqueue *q) {
	/* TODO: reset head and count */
	(void)q;
}

/* Append slot at the tail: index (head + count) % NPROC.
 * Returns 0, or -1 if the queue is already full. */
int rq_push(struct runqueue *q, int slot) {
	/* TODO */
	(void)q;
	(void)slot;
	return -1;
}

/* Remove and return the oldest entry (at head), advancing head with
 * wraparound. Returns -1 if the queue is empty. */
int rq_pop(struct runqueue *q) {
	/* TODO */
	(void)q;
	return -1;
}

/* 1 if slot is somewhere in the queue, else 0. Walk the live region:
 * count entries starting at head, indexes taken modulo NPROC. */
int rq_contains(const struct runqueue *q, int slot) {
	/* TODO */
	(void)q;
	(void)slot;
	return 0;
}
```

### Tests

```c
#include <stdio.h>

#define NPROC 16

struct runqueue {
	int items[NPROC];	/* proc slots, FIFO */
	int head;		/* index of oldest entry */
	int count;
};

void rq_init(struct runqueue *q);
int rq_push(struct runqueue *q, int slot);
int rq_pop(struct runqueue *q);
int rq_contains(const struct runqueue *q, int slot);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static void test_pop_empty(void) {
	struct runqueue q = {0};

	rq_init(&q);
	check(rq_pop(&q) == -1, "test_pop_empty_returns_minus_one");
}

static void test_fifo_order(void) {
	struct runqueue q = {0};
	int ok;

	rq_init(&q);
	ok = rq_push(&q, 3) == 0;
	ok = ok && rq_push(&q, 7) == 0;
	ok = ok && rq_push(&q, 1) == 0;
	ok = ok && rq_pop(&q) == 3;
	ok = ok && rq_pop(&q) == 7;
	ok = ok && rq_pop(&q) == 1;
	ok = ok && rq_pop(&q) == -1;
	check(ok, "test_fifo_order");
}

static void test_push_full(void) {
	struct runqueue q = {0};
	int i, ok = 1;

	rq_init(&q);
	for (i = 0; i < NPROC; i++)
		ok = ok && rq_push(&q, i) == 0;
	check(ok, "test_push_until_full_succeeds");
	check(rq_push(&q, 0) == -1, "test_push_full_returns_minus_one");
	check(rq_pop(&q) == 0, "test_full_queue_still_pops_oldest");
}

static void test_wraparound(void) {
	struct runqueue q = {0};
	int i, ok = 1;

	rq_init(&q);
	for (i = 0; i < 12; i++)
		ok = ok && rq_push(&q, i) == 0;
	for (i = 0; i < 12; i++)
		ok = ok && rq_pop(&q) == i;
	/* head now sits at index 12; these pushes wrap past NPROC */
	for (i = 0; i < 10; i++)
		ok = ok && rq_push(&q, (i + 3) % NPROC) == 0;
	for (i = 0; i < 10; i++)
		ok = ok && rq_pop(&q) == (i + 3) % NPROC;
	ok = ok && rq_pop(&q) == -1;
	check(ok, "test_wraparound_keeps_fifo");
}

static void test_contains(void) {
	struct runqueue q = {0};

	rq_init(&q);
	rq_push(&q, 5);
	rq_push(&q, 9);
	check(rq_contains(&q, 5) == 1 && rq_contains(&q, 9) == 1,
	      "test_contains_present");
	check(rq_contains(&q, 4) == 0, "test_contains_absent");
	rq_pop(&q);	/* removes 5 */
	check(rq_contains(&q, 5) == 0 && rq_contains(&q, 9) == 1,
	      "test_contains_false_after_pop");
}

int main(void) {
	test_pop_empty();
	test_fifo_order();
	test_push_full();
	test_wraparound();
	test_contains();
	return failed;
}
```

## Multilevel feedback: SJF without a crystal ball

Round-robin's honesty is also its flaw: it treats the compiler and the
editor identically. The editor wakes for a keystroke and must wait its
turn behind processes that will each burn a full 40 ms slice; the
compiler gets interrupted every 40 ms for switches it never needed. We
know from the SJF arithmetic that short-running work should go first —
but the kernel is not told job lengths, and interactive processes do not
even *have* one; they run in short bursts forever.

The trick, and it is one of the loveliest in operating systems, is to
let processes classify themselves by their own behavior. Keep several
run queues at different priorities — DuckOS uses `#define NQ 4`, with
queue 0 the highest — and apply one feedback rule:

- **Burn your entire quantum? Demoted one queue.** Only CPU-bound work
  runs until the timer takes the CPU away.
- **Block before the quantum expires — on a keystroke, a disk read, an
  IPC receive? Stay at your current level.** Giving up the CPU
  voluntarily is what interactive processes do; the scheduler rewards
  exactly that.

A process's *past* burst behavior predicts its next burst, so within a
few quanta every process sinks to the level that matches what it is: the
editor bounces along at queue 0 blocking early every time, the compiler
sinks to the bottom, and the scheduler always picks from the highest
non-empty queue. Short-burst work goes first — SJF's ordering — without
the kernel ever being told a job length. And the classification is
self-correcting: when the compiler finishes and the ray tracer's window
gets typed into, the demotions and stays re-sort everyone.

DuckOS also grows the quantum as priority drops:

```
#define QUANTUM(q) (1 << (q))	/* queue 0: 1 tick ... queue 3: 8 ticks */
```

Interactive processes at queue 0 get 10 ms slices — they rarely use even
that before blocking, and if a CPU hog sneaks in, it is caught and
demoted after a single tick. CPU-bound processes at queue 3 get 80 ms:
they run less *often*, but in longer pieces, which is precisely what
throughput wants — fewer context switches, warmer caches. Doubling the
quantum per level is not a DuckOS invention: **CTSS**, Corbató's 1962
Compatible Time-Sharing System at MIT — the machine that invented
multilevel feedback scheduling — doubled its quantum at each lower
level for exactly this reason.

```
queue 0 (quantum 1 tick):  [ editor ][ shell ]      <- always picked first
queue 1 (quantum 2 ticks): [ make ]
queue 2 (quantum 4 ticks): []
queue 3 (quantum 8 ticks): [ cc1 ][ ray tracer ]    <- runs when all above empty
```

## Minix's three queues

This design is on the family tree of the OS we are imitating. Minix 1.0
scheduled from exactly this picture — an array of prioritized FIFO run
queues — with three of them, named for what lived there:

- `TASK_Q`, highest: kernel tasks — the clock task, the disk and
  terminal drivers, which in a microkernel are processes like any other.
- `SERVER_Q`: the memory manager (MM) and file system (FS) servers.
- `USER_Q`, lowest: everything you actually ran, round-robin with a
  100 ms quantum.

The reasoning is pure microkernel: once your disk driver is a process,
"the driver runs when hardware needs it" must be expressed as *scheduler
policy* rather than as the kernel just doing whatever it likes. A
keystroke's interrupt makes the terminal task runnable; the terminal
task must then actually preempt your compile, or input gets lost —
hence the strict rule "highest non-empty queue wins," which DuckOS
keeps. Minix's queue assignments were static, though: a process was
born a task, a server, or a user and stayed in its queue for life —
classification by decree. What DuckOS layers on top is the CTSS-style
feedback rule, so the queue levels get discovered from behavior instead
of assigned by birthright. (Later Minix versions grew more queues and
adjustable priorities; the three-queue picture is the 1987 one, the one
a student in Helsinki would have been reading.)

## Starvation, and the periodic boost

Every priority scheme buys its latency wins with the same hidden
currency: the low-priority process's lunch. Picture the ray tracer,
long since demoted to queue 3, while you interactively edit and
recompile: queues 0 and 1 are never empty for long, so queue 3 is never
reached, and the ray tracer makes *zero* progress. Not slow progress —
none. That is **starvation**, and it is not a corner case; any workload
with a steady interactive load on top starves the bottom queue.

The four queues and every way out of them — red edges are demotions earned by burning a quantum; the dashed loop is the periodic boost; the dashed box is a blocked process parked outside all queues at its recorded level:

```d2
direction: down

q0: "queue 0 · 1 tick · picked first" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}
q1: "queue 1 · 2 ticks"
q2: "queue 2 · 4 ticks"
q3: "queue 3 · 8 ticks · last resort"

q0 -> q1: "burn full quantum: down one level" {
  style.stroke: "#dc2626"
  style.stroke-width: 2
}
q1 -> q2: {
  style.stroke: "#dc2626"
  style.stroke-width: 2
}
q2 -> q3: {
  style.stroke: "#dc2626"
  style.stroke-width: 2
}
q3 -> q0: "boost: all → queue 0" {
  style.stroke-dash: 4
}

blocked: "blocked\n(in no queue)" {
  style.stroke-dash: 4
}
q1 -> blocked: "block early: keep your level"
blocked -> q1: "wake: re-enqueue at recorded level" {
  style.stroke-dash: 4
}
```

Worse, priority schedulers invite sabotage. A CPU hog that sleeps for a
millisecond just before its quantum expires "blocks voluntarily" every
time and camps at queue 0 forever — gaming the feedback rule. Versions
of this attack worked on real commercial Unixes.

The classic fix for both is crude and effective: the **priority boost**.
Periodically — say once per second, a trigger that in DuckOS arrives
through the timer machinery of *The Clock Ticks* — move every queued
process back to queue 0. The ray tracer is guaranteed to see the CPU at
least once per boost period, so starvation becomes bounded delay; and
any hog that connived its way to high priority gets re-sorted honestly,
because after the boost everyone must re-earn their level through the
same demotions. The cost is a brief post-boost period where the editor
shares queue 0 with the ray tracer — the price of the guarantee, paid
once a second.

One deliberate wrinkle in the DuckOS contract you are about to build: a
process that blocks is *removed* from the scheduler entirely — a blocked
process is in `PROC_RECEIVING` or `PROC_SLEEPING`, not runnable, so it
belongs in no run queue. It keeps its priority level as a matter of
record, and whoever wakes it — the IPC delivery in *Message Passing —
the Microkernel Heart*, the timer expiry in *The Clock Ticks* — re-
enqueues it at that recorded level. The final challenge wires those
wake-ups into this exact scheduler.

## Challenge: Multilevel Feedback {#mlq-schedule points=25}

Build the DuckOS scheduler: `NQ` circular FIFO queues (0 = highest
priority), the feedback rule, and the boost. The queues hold proc-table
slot numbers, and each level is the same ring-buffer scheme as *The Run
Queue* — `head[q]` marks the oldest entry at level `q`, `count[q]` how
many are queued, tail computed as `(head[q] + count[q]) % NPROC`.

The contract, precisely:

- `sched_init(s)` — all queues empty, `current = -1`, every `prio[]`
  entry -1, `quantum_left = 0`.
- `sched_enqueue(s, slot, prio)` — append `slot` to queue `prio` and
  record `prio[slot] = prio`. Returns -1 if `prio` is outside
  `0..NQ-1`, if `slot` is outside `0..NPROC-1`, if that queue is full,
  or if the slot is already present — meaning it is sitting in *some*
  queue or is the currently running slot. A blocked slot is not
  present (it is in no queue and not running), so re-enqueueing it
  succeeds; that is how wake-ups work. Returns 0 on success.
- `sched_pick(s)` — pop the head of the lowest-numbered non-empty
  queue; make it `current` with `quantum_left = QUANTUM(that queue)`.
  Its `prio[]` entry keeps the level it was picked from — a running
  process still "belongs" to its queue level. Returns the slot, or -1
  if every queue is empty (`current` stays -1; a real kernel would sit
  in the idle loop). The kernel only calls `sched_pick` when nothing
  is running (`current == -1`), and the tests respect that.
- `sched_tick(s)` — one timer tick. If nothing is running, return 0.
  Otherwise decrement `quantum_left`; if it reaches 0 the quantum is
  burned: demote `current` one level (capping at `NQ-1` — there is no
  queue below the bottom), re-enqueue it at that new level, clear
  `current` to -1, and return 1 (preempted). If quantum remains,
  return 0.
- `sched_block(s)` — the running process blocked voluntarily (an IPC
  receive, a sleep) before its quantum ran out. It is NOT demoted:
  blocking early is interactive behavior, and keeping its level is the
  feedback reward for it. Set `current = -1` and leave `prio[slot]`
  unchanged — but do NOT re-enqueue the slot. A blocked process is
  not runnable; whoever wakes it calls `sched_enqueue` with its
  recorded priority.
- `sched_boost(s)` — the starvation fix. Drain queues `1..NQ-1`, in
  that order, appending each drained slot to the tail of queue 0 in
  the order popped (so relative FIFO order is preserved), updating
  `prio[]` to 0 as you go. The running slot and blocked slots are in
  no queue, so a boost does not touch them.

The tests plant scenarios for each rule: picking from the highest
non-empty queue and FIFO order within one level; a queue-0 process
preempted after exactly 1 tick and a queue-2 process after exactly 4;
demotion landing exactly one level down and capping at `NQ-1`; a
demoted CPU hog losing the next pick to a fresh queue-0 arrival; a
block that preserves `prio[]` and allows re-enqueue at the same level
with a full fresh quantum; a boost that moves a queue-1 and a queue-3
slot to queue 0 (queue-1 first) and leaves nothing behind; and picks
from an empty scheduler returning -1.

### Starter

```c
#define NPROC 16
#define NQ 4
#define QUANTUM(q) (1 << (q))	/* queue 0: 1 tick ... queue 3: 8 ticks */

struct sched {
	int queue[NQ][NPROC];	/* circular FIFO per level */
	int head[NQ];
	int count[NQ];
	int prio[NPROC];	/* current queue of each slot; -1 = not present */
	int current;		/* running slot, -1 = none */
	int quantum_left;	/* ticks left for current */
};

/* Empty scheduler: no queued slots, nothing running, all prio[] -1. */
void sched_init(struct sched *s) {
	/* TODO: heads/counts to 0, prio[] to -1, current -1, quantum 0 */
	(void)s;
}

/* Append slot to queue prio; record prio[slot].
 * -1 if prio not in 0..NQ-1, slot not in 0..NPROC-1, queue full, or
 * slot already present (queued anywhere, or running). 0 on success. */
int sched_enqueue(struct sched *s, int slot, int prio) {
	/* TODO */
	(void)s;
	(void)slot;
	(void)prio;
	return -1;
}

/* Pop the lowest-numbered non-empty queue; set current and
 * quantum_left = QUANTUM(level); prio[slot] keeps its level.
 * Returns the slot, or -1 if all queues are empty (current stays -1).
 * Only called when current == -1. */
int sched_pick(struct sched *s) {
	/* TODO */
	(void)s;
	return -1;
}

/* One timer tick. No current: return 0. Otherwise decrement
 * quantum_left; at 0, demote current one level (cap NQ-1),
 * re-enqueue it there, clear current, return 1. Else return 0. */
int sched_tick(struct sched *s) {
	/* TODO */
	(void)s;
	return 0;
}

/* Current blocked voluntarily: current = -1, prio[slot] unchanged,
 * slot NOT re-enqueued (its waker re-enqueues it). No demotion —
 * blocking early is what interactive processes do. */
void sched_block(struct sched *s) {
	/* TODO */
	(void)s;
}

/* Starvation fix: drain queues 1..NQ-1 in order into the tail of
 * queue 0, preserving pop order, setting each prio[] to 0. */
void sched_boost(struct sched *s) {
	/* TODO */
	(void)s;
}
```

### Tests

```c
#include <stdio.h>

#define NPROC 16
#define NQ 4
#define QUANTUM(q) (1 << (q))	/* queue 0: 1 tick ... queue 3: 8 ticks */

struct sched {
	int queue[NQ][NPROC];	/* circular FIFO per level */
	int head[NQ];
	int count[NQ];
	int prio[NPROC];	/* current queue of each slot; -1 = not present */
	int current;		/* running slot, -1 = none */
	int quantum_left;	/* ticks left for current */
};

void sched_init(struct sched *s);
int sched_enqueue(struct sched *s, int slot, int prio);
int sched_pick(struct sched *s);
int sched_tick(struct sched *s);
void sched_block(struct sched *s);
void sched_boost(struct sched *s);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static void test_pick_empty(void) {
	struct sched s = {0};

	sched_init(&s);
	check(sched_pick(&s) == -1, "test_pick_on_empty_returns_minus_one");
}

static void test_pick_highest(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	ok = sched_enqueue(&s, 5, 2) == 0;
	ok = ok && sched_enqueue(&s, 3, 0) == 0;
	ok = ok && sched_pick(&s) == 3;	/* queue 0 beats queue 2 */
	sched_block(&s);
	ok = ok && sched_pick(&s) == 5;
	check(ok, "test_pick_highest_nonempty_queue");
}

static void test_fifo_within_level(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	ok = sched_enqueue(&s, 1, 1) == 0;
	ok = ok && sched_enqueue(&s, 2, 1) == 0;
	ok = ok && sched_enqueue(&s, 4, 1) == 0;
	ok = ok && sched_pick(&s) == 1;
	sched_block(&s);
	ok = ok && sched_pick(&s) == 2;
	sched_block(&s);
	ok = ok && sched_pick(&s) == 4;
	check(ok, "test_fifo_within_level");
}

static void test_enqueue_rejects(void) {
	struct sched s = {0};

	sched_init(&s);
	check(sched_enqueue(&s, 0, -1) == -1 &&
	      sched_enqueue(&s, 0, NQ) == -1,
	      "test_enqueue_rejects_bad_prio");
	sched_enqueue(&s, 7, 1);
	check(sched_enqueue(&s, 7, 2) == -1,
	      "test_enqueue_rejects_queued_slot");
	sched_pick(&s);	/* slot 7 is now running */
	check(sched_enqueue(&s, 7, 0) == -1,
	      "test_enqueue_rejects_running_slot");
}

static void test_quantum_queue0(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	sched_enqueue(&s, 2, 0);
	ok = sched_pick(&s) == 2;
	ok = ok && sched_tick(&s) == 1;	/* QUANTUM(0) == 1 tick */
	check(ok, "test_queue0_preempted_after_one_tick");
}

static void test_quantum_queue2(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	sched_enqueue(&s, 2, 2);
	ok = sched_pick(&s) == 2;
	ok = ok && sched_tick(&s) == 0;
	ok = ok && sched_tick(&s) == 0;
	ok = ok && sched_tick(&s) == 0;
	ok = ok && sched_tick(&s) == 1;	/* QUANTUM(2) == 4 ticks */
	check(ok, "test_queue2_preempted_after_four_ticks");
}

static void test_demotion(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	sched_enqueue(&s, 4, 0);
	sched_pick(&s);
	ok = sched_tick(&s) == 1;
	ok = ok && s.prio[4] == 1;	/* one level down */
	ok = ok && sched_pick(&s) == 4;	/* re-queued, still schedulable */
	ok = ok && sched_tick(&s) == 0;
	ok = ok && sched_tick(&s) == 1;	/* QUANTUM(1) == 2 ticks */
	ok = ok && s.prio[4] == 2;
	check(ok, "test_demotion_one_level_per_burn");
}

static void test_demotion_caps(void) {
	struct sched s = {0};
	int i, ok;

	sched_init(&s);
	sched_enqueue(&s, 6, 3);
	ok = sched_pick(&s) == 6;
	for (i = 0; i < 7; i++)
		ok = ok && sched_tick(&s) == 0;
	ok = ok && sched_tick(&s) == 1;	/* QUANTUM(3) == 8 ticks */
	ok = ok && s.prio[6] == 3;	/* no queue below the bottom */
	ok = ok && sched_pick(&s) == 6;
	check(ok, "test_demotion_caps_at_bottom");
}

static void test_hog_loses_to_fresh(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	sched_enqueue(&s, 1, 0);
	sched_pick(&s);
	ok = sched_tick(&s) == 1;	/* hog burns quantum, demoted to 1 */
	ok = ok && sched_enqueue(&s, 2, 0) == 0;
	ok = ok && sched_pick(&s) == 2;	/* fresh queue-0 arrival wins */
	check(ok, "test_demoted_hog_loses_to_fresh_arrival");
}

static void test_block_keeps_priority(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	sched_enqueue(&s, 3, 1);
	ok = sched_pick(&s) == 3;
	ok = ok && sched_tick(&s) == 0;	/* one tick used, one left */
	sched_block(&s);
	ok = ok && s.prio[3] == 1;	/* no demotion for blocking */
	ok = ok && sched_enqueue(&s, 3, s.prio[3]) == 0;	/* wake-up */
	ok = ok && sched_pick(&s) == 3;
	ok = ok && sched_tick(&s) == 0;
	ok = ok && sched_tick(&s) == 1;	/* fresh full quantum */
	check(ok, "test_block_keeps_priority");
}

static void test_boost(void) {
	struct sched s = {0};
	int ok;

	sched_init(&s);
	ok = sched_enqueue(&s, 9, 3) == 0;
	ok = ok && sched_enqueue(&s, 4, 1) == 0;
	sched_boost(&s);
	ok = ok && s.prio[9] == 0 && s.prio[4] == 0;
	ok = ok && sched_pick(&s) == 4;	/* queue 1 drained before queue 3 */
	sched_block(&s);
	ok = ok && sched_pick(&s) == 9;	/* former queue-3 slot, now queue 0 */
	sched_block(&s);
	ok = ok && sched_pick(&s) == -1;	/* nothing left behind */
	check(ok, "test_boost_moves_all_to_queue0");
}

int main(void) {
	test_pick_empty();
	test_pick_highest();
	test_fifo_within_level();
	test_enqueue_rejects();
	test_quantum_queue0();
	test_quantum_queue2();
	test_demotion();
	test_demotion_caps();
	test_hog_loses_to_fresh();
	test_block_keeps_priority();
	test_boost();
	return failed;
}
```

# Lesson: Message Passing — the Microkernel Heart {#message-passing}

Everything we have built so far — the proc table from *Processes: the
Kernel's Bookkeeping*, the queues from *Scheduling* — has been machinery
for keeping processes apart. Separate slots, separate contexts, separate
turns on the CPU. But an operating system is not a collection of
hermits. The filesystem needs the disk driver. A program that calls
`read()` needs the filesystem. The memory manager needs to tell the
kernel a process died. Sooner or later, the pieces of the system must
talk, and *how* they talk is the single most consequential design
decision in an operating system. This lesson is about the answer Minix
chose, the answer DuckOS inherits, and the most famous argument in
operating-systems history — which was about exactly this.

## Two ways to build an operating system

The obvious answer is: don't make them talk at all, just let them call
each other. Put the scheduler, the filesystem, the drivers, memory
management, and the network stack in one program, all in ring 0, one
address space. When the filesystem needs the disk driver it makes a
function call — a few cycles, arguments in registers, done. This is the
**monolithic kernel**, and it is how Unix was built, and how Linux is
built to this day.

The price is that the kernel becomes one enormous program where every
part trusts every other part completely. A wild pointer in a sound-card
driver can scribble over the filesystem's buffer cache — same address
space, no protection, nothing to stop it. Every line of driver code
somebody contributes runs with total authority over your disk contents.

The microkernel answer inverts this. Shrink the kernel proper to the
few things that *must* run in ring 0 — flipping page tables, fielding
interrupts, switching contexts — and evict everything else into ordinary
processes. In Minix, the filesystem is a process. The memory manager is
a process. Each driver is a process. They live in separate address
spaces, get scheduled like anything else, and when the filesystem needs
the disk driver it cannot call it — there is a protection boundary in
the way. It sends it a **message**, through the kernel:

```
        MONOLITHIC (Linux)                MICROKERNEL (Minix, DuckOS)

  +--------------------------+       +------+  +------+  +--------+
  |  scheduler   filesystem  |       |  FS  |  |  MM  |  | driver |
  |  drivers     memory mgmt |       +--+---+  +--+---+  +---+----+
  |  net stack   everything  |          |  messages |        |
  |         (ring 0)         |       +--+-----------+--------+----+
  +--------------------------+       | kernel: IPC, sched, MMU    |
                                     +----------------------------+
```

The kernel proper does almost nothing but pass those messages. That is
not an exaggeration: strip a microkernel down and what remains is a
scheduler and a post office. A buggy driver process can crash — and the
system can restart it — without the filesystem ever noticing more than
an unanswered message. The cost is equally plain: what used to be a
function call is now two context switches and a copy, mediated by the
kernel. You are trading cycles for isolation.

Two answers to the same question — everything in ring 0 calling functions, or servers exchanging messages through a tiny kernel:

```d2
grid-columns: 2
horizontal-gap: 40

mono: "monolithic (Linux)" {
  ring0: "one program, ring 0" {
    grid-columns: 2
    sched: scheduler
    fs: filesystem
    drv: drivers
    mm: memory mgmt
  }
}

micro: "microkernel (Minix, DuckOS)" {
  direction: down
  fs: FS
  mm: MM
  drv: driver
  k: "kernel (ring 0):\nIPC, sched, MMU"
  fs -> k
  mm -> k: messages
  drv -> k
}
```

## "LINUX is obsolete"

On January 29, 1992, Andrew Tanenbaum — author of Minix, the teaching
OS this course is modeled on — posted to the Usenet group comp.os.minix
under the subject line "LINUX is obsolete." Linux was five months old.
Tanenbaum's argument was the one above, delivered without anesthetic:
microkernels had won the argument in the research community, and
"writing a monolithic system in 1991 is a truly poor idea" — a giant
step back into the 1970s. In a follow-up he twisted the knife at the
student he'd never had: "Be thankful you are not my student. You would
not get a high grade for such a design."

Torvalds, twenty-two years old and defending his five-month-old kernel
on its author's home turf, gave no ground: "Your job is being a
professor and researcher: That's one hell of a good excuse for some of
the brain-damages of minix," and, for the scoreboard, "linux still
beats the pants of minix in almost all areas." His practical point was
real: Linux was faster, and it existed, and its monolithic design was a
big part of why one person could make it fast on cheap hardware.

History's verdict turned out messier than either side predicted.
Torvalds won the terrain everyone was looking at: Linux took the
server, the desktop, and (as Android's kernel) the phone — monolithic
everywhere. But look where nobody was looking. QNX, a rendezvous
message-passing microkernel like the one you're about to build, ships
in hundreds of millions of cars. seL4, a microkernel small enough to
formally *prove correct*, guards military and aviation systems — a
proof that is only tractable because the kernel is tiny. The baseband
processor in essentially every phone — the computer inside your
computer that speaks to the cell network — runs a small
message-passing kernel, frequently an L4 descendant. And in 2017 it
emerged that Intel's Management Engine, a hidden computer inside
virtually every Intel chipset since about 2015, runs Minix 3 itself —
prompting Tanenbaum to note, in an open letter to Intel, that his
teaching OS was plausibly the most widely deployed operating system on
earth. Inside machines whose main OS is Linux.

So: where an OS crash means a lost document, the fast monolith won.
Where it means a crashed car, the microkernel won. Keep both halves of
that verdict in mind while you build the mechanism they were fighting
over.

## Rendezvous: no mailboxes, no buffers

Say "message passing" and most programmers picture a mailbox: the
sender drops a message in a queue and moves on; the receiver picks it
up whenever. Minix does **not** work that way, and the reason is worth
internalizing before we write a line of code.

A mailbox has to live somewhere. If the kernel buffers messages, the
kernel needs a message allocator, a per-mailbox queue, a policy for
when a mailbox fills up (block the sender after all? drop messages?
kill someone?), and every message is copied *twice* — sender's memory
to kernel buffer, kernel buffer to receiver's memory. That is a lot of
machinery for a kernel whose whole ambition is to be small.

Minix instead uses **rendezvous** semantics — the two parties must
meet:

- `send(dst, msg)` blocks the caller until `dst` actually takes the
  message.
- `receive(src, buf)` blocks the caller until someone actually sends.

Whichever party arrives first waits for the other. When both are
present, the kernel copies the message **exactly once**, directly from
the sender's buffer into the receiver's, and both continue. Look at
what this buys:

- **No kernel buffer management.** The message never lives in kernel
  memory. The only bookkeeping is a few ints in the proc table: who is
  blocked, on whom.
- **One copy**, sender to receiver, and since messages are fixed-size —
  in DuckOS, a `struct message` of four ints — that copy is a
  constant-time structure assignment. No length checks, no variable
  allocation. (Real Minix messages are a union of a handful of fixed
  layouts, all the same size, for exactly this reason.)
- **Backpressure for free.** A producer that outruns its consumer
  simply blocks. No queue to overflow, no policy to design — the
  problem structurally cannot occur.

And the costs, because there are always costs:

- **Schedules become coupled.** A sender cannot fire-and-forget; it
  runs no faster than its receiver drains. In a monolith you pay for
  isolation with cycles; here you also pay with concurrency.
- **Deadlock becomes YOUR problem.** If two processes each block
  sending to the other, no third party will ever deliver either
  message. They will wait, in `PROC_SENDING`, until the heat death of
  the machine. We will deal with this head-on below.

## The handshake, both ways

Time to make this concrete with the DuckOS proc table. Alongside the
fields you built in *Processes: the Kernel's Bookkeeping*, IPC adds
three: `send_to` (which slot I'm blocked sending to), `recv_from`
(which slot — or `ANY` — I'm blocked receiving from), and a one-message
buffer. Two proc states carry the whole protocol: `PROC_SENDING` and
`PROC_RECEIVING`. In a real kernel these transitions happen inside the
trap handler with interrupts off; here the "kernel" is a plain C struct
the tests can read.

**Receiver first.** Process B (slot 5) asks for a message before anyone
has sent one; process A (slot 2) sends later:

```
   proc B, slot 5                        proc A, slot 2
   --------------                        --------------
   receive(ANY, &msg)
     kernel scans: nobody is
     SENDING to slot 5
     => block B:
        state     = PROC_RECEIVING
        recv_from = ANY
        user_out  = &msg     (where delivery should land)

                  ... B is off the run queue; time passes ...

                                         send(5, &m)
                                           kernel sees B RECEIVING,
                                           recv_from matches:
                                             copy m -> *B.user_out
                                             stamp m_source = 2
                                             B: PROC_RUNNABLE,
                                                recv_from = -1
                                           A never blocks; returns 0
   (B is scheduled again)
   msg is already filled in;
   receive returns 0
```

Note who does the work: the *sender's* trap into the kernel performs
the delivery, into memory the receiver named when it blocked. By the
time B runs again there is nothing left to do — its `receive` simply
returns.

**Sender first.** Now A sends before B is listening:

```
   proc A, slot 2                        proc B, slot 5
   --------------                        --------------
   send(5, &m)
     kernel: B is not RECEIVING
     (deadlock walk — see below —
      finds no cycle)
     => block A:
        state   = PROC_SENDING
        send_to = 5
        buf     = m          (parked in A's proc slot)

                  ... A is off the run queue; time passes ...

                                         receive(ANY, &msg)
                                           kernel scans slots 0..15
                                           for SENDING, send_to == 5:
                                           finds A (slot 2)
                                             copy A.buf -> msg
                                             stamp m_source = 2
                                             A: PROC_RUNNABLE,
                                                send_to = -1
                                           returns 0 immediately
   (A is scheduled again;
    its send returns 0)
```

Symmetric, with one asymmetry worth noticing: a blocked sender's
message is parked in its own proc-table slot (`buf`), because the
receiver hasn't told anyone where it wants delivery yet. A blocked
receiver instead leaves a forwarding address (`user_out`). Either way
the message is copied exactly once.

Two scanning rules complete the picture. `receive` may name a specific
source slot, or `ANY`; a directed receive must *skip* senders it didn't
ask for — they stay blocked, waiting their turn. And when several
senders are queued for the same receiver, our kernel scans the proc
table in slot order, so `ANY` takes the lowest-numbered waiting sender:
deterministic, trivially fair enough for a teaching kernel, and exactly
what your tests will check.

## Deadlock: the price of blocking

Here is the failure mode rendezvous hands you. Process A sends to B; B
isn't receiving, so A blocks. Now B sends to A. A isn't receiving — A
is blocked in `send`, and will be forever, because the only process
that could ever receive its message is the one now blocking on it:

```
        A --send--> B
        ^           |
        +---send----+        both PROC_SENDING, neither will
                             ever reach a receive.  Forever.
```

Nothing rescues them. There is no timeout, no queue that drains, no
third party — rendezvous means each is waiting for the other to arrive
at a meeting neither can attend. The same trap generalizes to any
cycle: A blocked sending to B, B blocked sending to C, and C now sends
to A.

Minix's defense is almost embarrassingly cheap, and it works because
of one structural fact: a blocked process is blocked sending to
**exactly one** destination. So the "waiting on" relationships form
chains, not webs, and the kernel can check for a cycle by walking a
chain. Before blocking a sender, follow the `send_to` links starting
from the destination, for as long as each process on the path is
itself `PROC_SENDING`. If the walk ever arrives back at the process
that's trying to send, blocking it would close a cycle — so the kernel
refuses, returning `-EDEADLK` instead of blocking:

The walk that says no — C's send would close the loop, so it is refused (red dashed) before anyone blocks:

```d2
direction: down

a: "A — PROC_SENDING"
b: "B — PROC_SENDING"
c: "C — the caller"

a -> b: "send_to (hop 1)"
b -> c: "send_to (hop 2)"
c -> a: "refused: -EDEADLK" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
  style.stroke-dash: 4
}
```

```
   A --send--> B --send--> C          C now calls send(A, &m).
   ^                       |          Walk from A:
   +--------send-----------+            A is SENDING, to B  -> hop to B
          (proposed)                    B is SENDING, to C  -> hop to C
                                        C is... the caller.  CYCLE.
                                      Refuse: return -EDEADLK.
                                      C stays RUNNABLE.
```

Compare a walk that finds no cycle: with A blocked on B and B blocked
on C as before, a *fourth* process D sends to A. The walk from A hops
to B, hops to C — and C isn't sending, so the chain ends without ever
reaching D. D blocks legitimately: it's at the back of a queue, not in
a cycle. The distinction matters — long chains are normal (a pipeline
of processes each waiting on the next), and only a chain that bends
back to *you* is fatal.

The walk visits at most one process per proc slot, and in practice IPC
chains in a microkernel are two or three hops (user process → server →
driver), so this costs a handful of loads on every send that's about
to block. That's the whole deadlock story — no graph algorithms, no
timeouts, no lock ordering discipline. One honest caveat: the kernel
detects only cycles of *sends*. Two processes that each block in
`receive`, waiting for the other to send first, deadlock just as
thoroughly, and no walk can distinguish that from a server that's
legitimately idle. The kernel solves what it can see; protocol design
handles the rest.

And the walk that says yes — D's send lands on a chain that ends at a RUNNABLE process, so D may safely park (dashed = the block that is allowed to happen):

```d2
direction: right

d: "D\ncaller"
a: "A\nSENDING"
b: "B\nSENDING"
c: "C\nRUNNABLE"

d -> a: "allowed" {
  style.stroke-dash: 4
}
a -> b: "hop 1"
b -> c: "hop 2"
```

## The kernel stamps the return address

One field of `struct message` gets special treatment, and it's the
reason microkernel IPC can bear the weight of an operating system.
`m_source` — who sent this — is **never taken from the sender**. The
sender can write anything it likes there; on delivery, the kernel
overwrites it with the sender's actual slot number. On both paths: the
immediate rendezvous copy, and the parked-in-`buf` copy drained by a
later receive.

Think about what the filesystem server does with an incoming request:
it looks up *that process's* open-file table, checks *that process's*
permissions, and sends the reply to *that process*. Every one of those
decisions keys off `m_source`. If a process could forge it, any
process could read any file by impersonating a more privileged one,
and the entire microkernel edifice — drivers and servers as mutually
untrusting processes — would collapse. Because the stamp comes from
the kernel, a message's origin is as trustworthy as the kernel itself,
and servers can make authority decisions over plain IPC. It is the
exact moral of a postmark versus a return address scribbled on the
envelope.

This is also the punchline the next stretch of the course builds on:
in a microkernel, `send` and `receive` are, to a first approximation,
the **only two real system calls**. What a Unix program calls `read()`
becomes: build a message, send it to the FS server, receive the reply.
*The System Call Boundary* shows that trap machinery in detail — and
`m_source` is what lets the server at the far end believe what the
trap tells it.

## Challenge: Rendezvous {#ipc-rendezvous points=35}

Implement DuckOS's rendezvous IPC: `k_send` and `k_receive` over the
static proc table. The starter defines the kernel structures and gives
you two working helpers — `k_init` (all slots `PROC_UNUSED`, links
cleared) and `k_mkproc` (bring a slot up `PROC_RUNNABLE`), which the
tests use to stage processes. You implement the two calls.

`int k_send(struct kernel *k, int src, int dst, const struct message *m)`

- Validate: if `dst` is out of range (not `0..NPROC-1`) or that slot is
  `PROC_UNUSED`, return `-ESRCH`. If `src == dst`, return `-EDEADLK`
  (the tightest possible cycle).
- If `dst` is `PROC_RECEIVING` and its `recv_from` is `ANY` or equals
  `src`: rendezvous. Copy `*m` into the receiver's landing pad — its
  `user_out` pointer, or its `buf` if `user_out` is NULL — then
  overwrite that copy's `m_source` with `src`, make `dst`
  `PROC_RUNNABLE` with `recv_from = -1`, and return 0. `src` never
  blocks on this path.
- Otherwise, run the deadlock walk before blocking: starting from
  `dst`, while the process you're looking at is `PROC_SENDING`, hop to
  its `send_to`. If the walk reaches `src`, return `-EDEADLK` without
  blocking or modifying anyone. If the chain ends elsewhere, block the
  sender: state `PROC_SENDING`, `send_to = dst`, park `*m` in
  `src`'s `buf`, and return 0.

`int k_receive(struct kernel *k, int rcv, int from, struct message *out)`

- Validate: `from` must be `ANY` (always acceptable) or a valid,
  non-`PROC_UNUSED` slot; otherwise return `-ESRCH`.
- Scan slots `0..NPROC-1` **in order** for a process that is
  `PROC_SENDING` with `send_to == rcv`, and — if `from` is not `ANY` —
  whose slot equals `from`. On a match: copy its `buf` into `*out`,
  stamp `out->m_source` with the sender's slot number, unblock the
  sender (`PROC_RUNNABLE`, `send_to = -1`), and return 0. The in-order
  scan is what makes `ANY` take the lowest waiting slot.
- No matching sender: block the receiver — state `PROC_RECEIVING`,
  `recv_from = from`, `user_out = out` — and return 0. (Returning 0
  while blocked stands in for "the call completes after the process is
  next scheduled"; the tests inspect the proc table to see the block.)

The tests stage processes with `k_mkproc` and then check: `-ESRCH` for
unused and out-of-range slots in both calls; both handshake orders,
including that the payload lands, `m_source` is stamped over a forged
value on both the rendezvous and the parked-in-`buf` path, and both
parties end `PROC_RUNNABLE` with links cleared; that a directed
receive blocks a wrong-source sender rather than accepting it, and
skips an earlier waiting sender for the requested one; that `ANY`
takes the lowest-numbered of two waiting senders; self-send, 2-cycle,
and 3-cycle refusal with `-EDEADLK` (the refused sender must stay
unblocked, everyone already blocked stays blocked); and that a chain
which does not cycle is allowed to block.

### Starter

```c
#include <stddef.h>

/*
 * DuckOS rendezvous IPC.  In a real kernel these functions run in the
 * trap handler with interrupts off and the proc table lives in ring 0;
 * here the "kernel" is a struct the tests can read.
 */

#define NPROC 16
#define ANY (-1)
#define ESRCH 3
#define EDEADLK 35
#define EINVAL 22

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct message {
	int m_source;	/* stamped by the kernel on delivery */
	int m_type;
	int m_i1;
	int m_i2;
};

struct proc {
	int pid;
	enum proc_state state;
	int send_to;		/* slot we are blocked sending to, -1 */
	int recv_from;		/* slot (or ANY) we are blocked receiving from, -1 = not receiving */
	struct message buf;	/* out: message being sent; in: landing pad */
	struct message *user_out; /* where a blocked receiver wants delivery (points into test memory) */
};

struct kernel {
	struct proc procs[NPROC];
};

/* Reset every slot: PROC_UNUSED, links cleared.  (Provided.) */
void k_init(struct kernel *k) {
	for (int i = 0; i < NPROC; i++) {
		k->procs[i].pid = 0;
		k->procs[i].state = PROC_UNUSED;
		k->procs[i].send_to = -1;
		k->procs[i].recv_from = -1;
		k->procs[i].user_out = NULL;
	}
}

/* Bring a slot to life as PROC_RUNNABLE.  (Provided; tests use it.) */
void k_mkproc(struct kernel *k, int slot, int pid) {
	k->procs[slot].pid = pid;
	k->procs[slot].state = PROC_RUNNABLE;
	k->procs[slot].send_to = -1;
	k->procs[slot].recv_from = -1;
	k->procs[slot].user_out = NULL;
}

/*
 * src sends *m to dst.
 *
 * Returns -ESRCH if dst is out of range or PROC_UNUSED, -EDEADLK if
 * src == dst or if blocking src would close a cycle of senders.
 * If dst is PROC_RECEIVING from ANY or from src: deliver *m into dst's
 * user_out (or its buf if user_out is NULL), stamping m_source = src,
 * wake dst (PROC_RUNNABLE, recv_from = -1), return 0 without blocking.
 * Otherwise walk send_to links from dst while they point at PROC_SENDING
 * processes; if the walk reaches src, refuse with -EDEADLK.  Else block
 * src: PROC_SENDING, send_to = dst, *m parked in src's buf; return 0.
 */
int k_send(struct kernel *k, int src, int dst, const struct message *m) {
	/* TODO: validate dst, then self-send; try immediate delivery to a
	   matching receiver; otherwise deadlock walk, then block src. */
	(void)k;
	(void)src;
	(void)dst;
	(void)m;
	return -1;
}

/*
 * rcv receives into *out from `from` (a slot, or ANY).
 *
 * Returns -ESRCH if from is neither ANY nor a live slot.  Scans slots
 * 0..NPROC-1 in order for a PROC_SENDING process with send_to == rcv
 * (and slot == from unless from is ANY): copies its buf into *out with
 * m_source = sender's slot, wakes the sender (PROC_RUNNABLE,
 * send_to = -1), returns 0.  If no sender matches, blocks rcv:
 * PROC_RECEIVING, recv_from = from, user_out = out; returns 0.
 */
int k_receive(struct kernel *k, int rcv, int from, struct message *out) {
	/* TODO: validate from; drain the lowest matching waiting sender,
	   or block rcv with a landing pad for a future sender. */
	(void)k;
	(void)rcv;
	(void)from;
	(void)out;
	return -1;
}
```

### Tests

```c
#include <stddef.h>
#include <stdio.h>

#define NPROC 16
#define ANY (-1)
#define ESRCH 3
#define EDEADLK 35
#define EINVAL 22

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct message {
	int m_source;	/* stamped by the kernel on delivery */
	int m_type;
	int m_i1;
	int m_i2;
};

struct proc {
	int pid;
	enum proc_state state;
	int send_to;		/* slot we are blocked sending to, -1 */
	int recv_from;		/* slot (or ANY) we are blocked receiving from, -1 = not receiving */
	struct message buf;	/* out: message being sent; in: landing pad */
	struct message *user_out; /* where a blocked receiver wants delivery (points into test memory) */
};

struct kernel {
	struct proc procs[NPROC];
};

void k_init(struct kernel *k);
void k_mkproc(struct kernel *k, int slot, int pid);
int k_send(struct kernel *k, int src, int dst, const struct message *m);
int k_receive(struct kernel *k, int rcv, int from, struct message *out);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static void test_send_validation(void) {
	struct kernel k;
	struct message m = { 0, 1, 2, 3 };

	k_init(&k);
	k_mkproc(&k, 0, 100);
	check(k_send(&k, 0, NPROC, &m) == -ESRCH, "test_send_dst_out_of_range");
	check(k_send(&k, 0, -1, &m) == -ESRCH, "test_send_dst_negative");
	check(k_send(&k, 0, 4, &m) == -ESRCH, "test_send_dst_unused");
	check(k_send(&k, 0, 0, &m) == -EDEADLK, "test_send_to_self_deadlk");
	check(k.procs[0].state == PROC_RUNNABLE,
	      "test_failed_send_does_not_block");
}

static void test_receive_validation(void) {
	struct kernel k;
	struct message out = { -1, -1, -1, -1 };

	k_init(&k);
	k_mkproc(&k, 1, 101);
	check(k_receive(&k, 1, NPROC, &out) == -ESRCH,
	      "test_recv_from_out_of_range");
	check(k_receive(&k, 1, 6, &out) == -ESRCH, "test_recv_from_unused");
}

static void test_receiver_first_any(void) {
	struct kernel k;
	struct message out = { -1, -1, -1, -1 };
	struct message m = { 99, 42, 7, 8 };	/* m_source forged as 99 */

	k_init(&k);
	k_mkproc(&k, 2, 102);	/* sender */
	k_mkproc(&k, 5, 105);	/* receiver */

	check(k_receive(&k, 5, ANY, &out) == 0 &&
	      k.procs[5].state == PROC_RECEIVING &&
	      k.procs[5].recv_from == ANY,
	      "test_recv_any_blocks_receiver");

	check(k_send(&k, 2, 5, &m) == 0, "test_rendezvous_send_returns_zero");
	check(out.m_type == 42 && out.m_i1 == 7 && out.m_i2 == 8,
	      "test_rendezvous_payload_delivered");
	check(out.m_source == 2, "test_m_source_stamped_over_forgery");
	check(k.procs[5].state == PROC_RUNNABLE && k.procs[5].recv_from == -1,
	      "test_receiver_unblocked");
	check(k.procs[2].state == PROC_RUNNABLE,
	      "test_sender_never_blocked_on_rendezvous");
}

static void test_receiver_first_directed(void) {
	struct kernel k;
	struct message out = { -1, -1, -1, -1 };
	struct message m2 = { 0, 7, 0, 0 };
	struct message m5 = { 30, 88, 1, 2 };	/* m_source forged as 30 */

	k_init(&k);
	k_mkproc(&k, 2, 102);
	k_mkproc(&k, 5, 105);
	k_mkproc(&k, 9, 109);	/* receiver, wants slot 5 only */

	check(k_receive(&k, 9, 5, &out) == 0 &&
	      k.procs[9].state == PROC_RECEIVING &&
	      k.procs[9].recv_from == 5,
	      "test_recv_directed_blocks");

	/* A send from slot 2 must block, not satisfy a receive from 5. */
	check(k_send(&k, 2, 9, &m2) == 0 &&
	      k.procs[2].state == PROC_SENDING && k.procs[2].send_to == 9 &&
	      k.procs[9].state == PROC_RECEIVING && out.m_type == -1,
	      "test_wrong_source_send_blocks_not_delivers");

	check(k_send(&k, 5, 9, &m5) == 0 &&
	      out.m_type == 88 && out.m_source == 5 &&
	      k.procs[9].state == PROC_RUNNABLE,
	      "test_matching_source_delivered");
}

static void test_sender_first(void) {
	struct kernel k;
	struct message m = { 99, 13, 21, 34 };	/* m_source forged as 99 */
	struct message out = { -1, -1, -1, -1 };

	k_init(&k);
	k_mkproc(&k, 3, 103);
	k_mkproc(&k, 6, 106);

	check(k_send(&k, 3, 6, &m) == 0 &&
	      k.procs[3].state == PROC_SENDING && k.procs[3].send_to == 6,
	      "test_sender_first_blocks");

	check(k_receive(&k, 6, ANY, &out) == 0 &&
	      out.m_type == 13 && out.m_i1 == 21 && out.m_i2 == 34,
	      "test_receive_drains_waiting_sender");
	check(out.m_source == 3, "test_m_source_stamped_on_buffered_path");
	check(k.procs[3].state == PROC_RUNNABLE && k.procs[3].send_to == -1,
	      "test_sender_unblocked_by_receive");
}

static void test_two_waiting_senders(void) {
	struct kernel k;
	struct message m3 = { 0, 300, 0, 0 };
	struct message m7 = { 0, 700, 0, 0 };
	struct message out = { -1, -1, -1, -1 };

	k_init(&k);
	k_mkproc(&k, 1, 101);	/* receiver */
	k_mkproc(&k, 3, 103);
	k_mkproc(&k, 7, 107);

	/* Slot 7 queues first in time, but 3 is the lower slot. */
	k_send(&k, 7, 1, &m7);
	k_send(&k, 3, 1, &m3);

	check(k_receive(&k, 1, ANY, &out) == 0 &&
	      out.m_type == 300 && out.m_source == 3,
	      "test_any_picks_lowest_waiting_slot");
	check(k.procs[7].state == PROC_SENDING && k.procs[7].send_to == 1,
	      "test_other_sender_still_waiting");

	/* A directed receive skips the earlier (lower) waiting sender. */
	k_send(&k, 3, 1, &m3);	/* 3 waits again alongside 7 */
	check(k_receive(&k, 1, 7, &out) == 0 &&
	      out.m_type == 700 && out.m_source == 7 &&
	      k.procs[3].state == PROC_SENDING,
	      "test_directed_receive_skips_lower_sender");
}

static void test_deadlock_cycles(void) {
	struct kernel k;
	struct message m = { 0, 1, 0, 0 };

	/* 2-cycle: 4 blocked sending to 8; 8 sends to 4. */
	k_init(&k);
	k_mkproc(&k, 4, 104);
	k_mkproc(&k, 8, 108);
	k_send(&k, 4, 8, &m);
	check(k_send(&k, 8, 4, &m) == -EDEADLK, "test_two_cycle_refused");
	check(k.procs[8].state == PROC_RUNNABLE && k.procs[8].send_to == -1,
	      "test_refused_sender_not_blocked");
	check(k.procs[4].state == PROC_SENDING && k.procs[4].send_to == 8,
	      "test_original_sender_still_blocked");

	/* 3-cycle: 0 -> 1 -> 2, then 2 sends to 0. */
	k_init(&k);
	k_mkproc(&k, 0, 100);
	k_mkproc(&k, 1, 101);
	k_mkproc(&k, 2, 102);
	k_send(&k, 0, 1, &m);
	k_send(&k, 1, 2, &m);
	check(k_send(&k, 2, 0, &m) == -EDEADLK, "test_three_cycle_refused");
	check(k.procs[2].state == PROC_RUNNABLE,
	      "test_three_cycle_sender_not_blocked");
}

static void test_chain_without_cycle(void) {
	struct kernel k;
	struct message m = { 0, 1, 0, 0 };

	/* 0 blocked on 1, 1 blocked on 2; 3 sends to 0.  The walk from 0
	   ends at 2 (not SENDING) without reaching 3: no cycle, so 3 may
	   block at the back of the chain. */
	k_init(&k);
	k_mkproc(&k, 0, 100);
	k_mkproc(&k, 1, 101);
	k_mkproc(&k, 2, 102);
	k_mkproc(&k, 3, 103);
	k_send(&k, 0, 1, &m);
	k_send(&k, 1, 2, &m);
	check(k_send(&k, 3, 0, &m) == 0 &&
	      k.procs[3].state == PROC_SENDING && k.procs[3].send_to == 0,
	      "test_chain_without_cycle_blocks");
}

int main(void) {
	test_send_validation();
	test_receive_validation();
	test_receiver_first_any();
	test_receiver_first_directed();
	test_sender_first();
	test_two_waiting_senders();
	test_deadlock_cycles();
	test_chain_without_cycle();
	return failed;
}
```

# Lesson: Sharing Without Tearing {#synchronization}

Everything we have built so far conspires to create one problem. In
*Scheduling* we gave DuckOS a timer-driven scheduler that can rip the
CPU away from a process between any two instructions; processes also
share kernel structures to scribble on. Put those together and you get
the defining bug family of operating systems: two flows of control
touching the same memory at once, each one correct alone, wrong
together. This lesson is about **races** — what they look like at the
instruction level, why they hide from testing — and the escalating
series of weapons an OS uses against them: disabling interrupts, atomic
hardware instructions, spinlocks, and finally Dijkstra's semaphore,
which you will build for DuckOS.

## The three instructions inside `nready++`

Suppose the kernel keeps a count of runnable processes, and two places
in the code increment it: `nready = nready + 1;`. One line of C — but
the CPU cannot add to memory in one conceptual step the way the syntax
implies. The classic i386 compilation is three instructions, a
**load / add / store**:

```
mov  eax, [nready]    ; load the current value into a register
add  eax, 1           ; bump the register
mov  [nready], eax    ; store the register back to memory
```

Three instructions means two gaps, and *Scheduling* taught us exactly
what lives in gaps: the timer interrupt. Say `nready` is 5, and process
A and process B each execute the increment. Here is one interleaving
the scheduler is fully entitled to produce:

```
     Process A                  Process B              nready in memory
     ---------                  ---------              ----------------
     mov eax,[nready]                                        5
       (A's eax = 5)
  <-- timer fires: A is preempted, its eax=5 saved in its context -->
                                mov eax,[nready]             5
                                  (B's eax = 5)
                                add eax,1
                                  (B's eax = 6)
                                mov [nready],eax             6
  <-- B's quantum ends: A resumes, its saved eax=5 restored -->
     add eax,1
       (A's eax = 6)
     mov [nready],eax                                        6   <-- !!
```

Two increments happened; the counter moved from 5 to 6. One update was
silently **lost**: A did its arithmetic on a value that was stale by
the time A stored, and A's store pasted right over B's. Nobody crashed,
nothing was reported — the kernel now simply believes one fewer process
is runnable than actually is. This is a **race condition**: the result
depends on the precise timing of who ran when. If the load/add/store
had executed as one indivisible unit, no interleaving could have split
them — the three-instruction window is the whole crime scene.

## Why "it works when I test it" is the horror

Look at the numbers. The dangerous window is two instruction boundaries
out of the millions of instructions a process executes per quantum, and
the timer fires only `HZ` = 100 times a second. A preemption lands
inside that exact window perhaps once in millions of runs: you can
hammer this code all afternoon and never see the bug, ship it, and
watch a counter drift on one machine in a thousand — unreproducibly, in
a different place each time, with a corpse that looks nothing like the
cause. Such bugs are called **heisenbugs**: observing them changes the
timing and makes them vanish (add a debug print inside the window and
the race stops manifesting). The most infamous real-world case is the
Therac-25 radiation therapy machine (1985–87), where a race between
operator input and the machine's setup task let a fast typist configure
a lethal beam — testers never typed fast enough to catch it. Races are
not ordinary bugs that testing shakes out; they must be made
*impossible by construction*.

## Critical sections, and the interrupt hammer

The code between the load and the store is a **critical section**: a
region that touches shared state and must not interleave with any other
region touching the same state. The rule we want is **mutual
exclusion** — at most one flow of control inside at a time.

A uniprocessor kernel has a brutally cheap way to get it. On a single
CPU, the only thing that can wedge another flow of control into your
instruction stream is an interrupt. So:

```
cli                   ; clear interrupt flag: CPU ignores maskable IRQs
mov  eax, [nready]    ;
add  eax, 1           ;   the critical section, now indivisible
mov  [nready], eax    ;
sti                   ; set interrupt flag: IRQs delivered again
```

No interrupt, no preemption, no interleaving. Early Unix kernels were
full of this pattern (dressed up as `spl` calls), and the real Minix
kernel relied on it too: its kernel layer ran with interrupts mostly
masked and never preempted itself. DuckOS may use the same trick for
short kernel sections. But notice how narrow its validity is:

- **It's a privilege, not a tool.** `cli` in ring 3 takes a
  general-protection fault — rightly, since a user process that could
  disable interrupts could freeze the machine forever. So it does
  nothing for synchronizing *user* processes.
- **It doesn't survive a second CPU.** `cli` clears the interrupt flag
  of the executing processor only; another CPU walks straight into the
  critical section without any interrupt being involved.
- **It's rude even when legal.** With interrupts off, the world is on
  hold: the 8259 PIC remembers only one pending interrupt per line, so
  a second clock tick arriving while the first is still undelivered is
  simply lost, and lost ticks make the kernel's clock drift behind wall
  time (*The Clock Ticks* shows why every tick is precious). Disable
  interrupts for nanoseconds, never milliseconds.

So `cli`/`sti` is a real answer for tiny uniprocessor kernel sections
and a non-answer for everything else. Mutual exclusion that works in
ring 3 and across CPUs needs help from the hardware — something better
than "stop the world."

## The hardware gift: atomic exchange

Here's a tempting pure-software lock, and it's wrong:

```c
while (locked) { }    /* wait for the lock to be free...   */
locked = 1;           /* ...then take it.                   */
```

Wrong for the exact reason `nready++` was wrong: between *seeing*
`locked == 0` and *setting* `locked = 1` there is a gap, and two CPUs
(or two interleaved processes) can both sail through it and both "own"
the lock. A lock built on a race cannot cure races.

The fix must come from below the software: a single instruction that
reads the old value and writes a new one as one indivisible bus
operation, leaving no gap to slip into. x86 has carried one since the
very first 8086 in 1978: `xchg` with a memory operand swaps a register
with memory and implicitly asserts the bus LOCK signal for the
duration, so no other processor (and no DMA device) can touch that
memory between the read half and the write half. This is the
**test-and-set** primitive, and one instruction of it buys a working
lock — the **spinlock**:

```
acquire:
	mov  al, 1
.retry:
	xchg al, [lock]       ; atomically: al <- old value, lock <- 1
	test al, al
	jnz  .retry           ; old value was 1: already held; spin
	ret                   ; old value was 0: WE locked it, atomically

release:
	mov  byte [lock], 0   ; a plain store is enough to unlock
```

The beauty is in what `xchg` returns. If the old value was 0, then
*you* are the one who changed it to 1 — the swap and the claim were the
same indivisible act, so exactly one contender can ever see 0. If it
was 1, you wrote 1 over 1 (harmless) and loop until the holder stores
0. And it works in ring 3 and across CPUs, because the atomicity lives
in the memory bus, not in the interrupt flag.

### When spinning is fine, and when it's criminal

A spinlock waiter doesn't sleep, it *spins*, burning CPU on an `xchg`
treadmill; whether that's acceptable is purely a question of how long
the wait is. For a microsecond critical section — another CPU updating
a run queue for fifty instructions — spinning is *cheaper* than any
alternative, since a context switch costs far more; real SMP kernels
protect their short internal sections exactly this way. But spin-
waiting on I/O is criminal. Suppose the lock guards a buffer whose
holder is waiting ~10ms for a disk platter: at `HZ` = 100 your entire
quantum is 10ms, so a spinning waiter burns its *whole quantum*
accomplishing nothing — and the scheduler from *Scheduling*, seeing a
process eat its full quantum, will classify it as CPU-bound and demote
its priority for the crime of waiting. On a uniprocessor it's worse:
while you spin, the lock holder — the only process that could release
the lock — isn't running. You are burning CPU to prevent the very
event you're waiting for.

For anything longer than a few dozen instructions, a waiter should
**sleep**, giving up the CPU until the holder wakes it. We already have
the machinery — `PROC_SLEEPING`, and a scheduler that skips sleepers.
What's missing is a disciplined object that decides who sleeps and who
wakes. It was invented in 1965.

## Dijkstra's semaphore

Edsger Dijkstra, building the THE multiprogramming system at the
Technische Hogeschool Eindhoven, distilled synchronization into one
integer with two operations, named **P** and **V** (from Dutch
*proberen*, to try, and *verhogen*, to raise); the English-speaking
world says **down** and **up**. Sixty years on, every mutex, condition
variable, and bounded queue you have ever used is a descendant.

A **semaphore** is an integer counter plus a queue of sleeping
processes:

- the **value** counts free units of some resource — buffer slots,
  devices, permission-to-enter tokens. It is never negative.
- **down(s)**: value positive? Decrement and continue — a unit is
  yours, no waiting. Zero? Go to sleep on the semaphore's queue.
- **up(s)**: anyone sleeping? Wake exactly one. Nobody? Increment.

Initialize the value to 1 and you get a **mutex** — a lock: the first
`down` takes 1→0 and enters the critical section, any other `down`
finds 0 and sleeps, and the holder's `up` wakes exactly one sleeper (or
restores the value to 1 if nobody's queued). Mutual exclusion, with
waiters sleeping instead of spinning. Initialize to N for an N-slot
resource pool; to 0 for a pure wait-for-event signal.

Two design decisions inside `up` deserve a hard look, because the
challenge tests both:

**Who gets woken? The one who waited longest.** The queue is FIFO. If
`up` woke an *arbitrary* waiter, an unlucky process could lose the
wakeup lottery forever while newer arrivals leapfrog it — that is
**starvation**. FIFO gives *bounded waiting*: with k processes queued
ahead of you, you run after at most k wakeups, guaranteed. (FIFO's
known dark side, the **convoy effect** — under heavy contention a
strict queue marches everyone at the speed of the slowest lock holder —
is real but beyond DuckOS's pay grade. FIFO is the honest default.)

The waiter queue in motion — a blocked down() joins at the tail; up() always pops the head (longest waiter first):

```d2
direction: right

blocker: "down() finds\nvalue == 0" {
  shape: oval
  style.stroke: "#d97706"
}

sem: "struct semaphore  (value = 0)" {
  grid-rows: 1
  c: "slot 9\ntail"
  b: "slot 3"
  a: "slot 5\nhead"
}

woken: "PROC_RUNNABLE\nagain" {
  shape: oval
}

blocker -> sem.c: "enqueue,\nPROC_SLEEPING"
sem.a -> woken: "up() pops\nthe head"
```

**When `up` wakes a waiter, the value stays 0.** This is the classic
subtlety. The tempting implementation — `up` increments, the woken
process re-checks and decrements — leaves a hole: between the increment
and the woken process actually running, a *third* process can call
`down`, see value 1, and steal the unit morally promised to the queue's
head, sending the woken waiter straight back to sleep. Do that in a
loop and the FIFO guarantee is fiction. The correct semantics is a
**direct handoff**: with a waiter present, `up` never releases the unit
into the wild at all — it hands it straight to the queue head, which
wakes *already owning* its unit. Value stays 0, queue shrinks by one,
nobody cuts the line.

The handoff, drawn — the thick edge is what actually happens; the red dashed path is the increment-and-hope bug where a third process steals the unit:

```d2
direction: right

up: "up(s)" {
  shape: oval
}

sem: "semaphore" {
  v: "value: stays 0"
  h: "head: slot 7\n→ RUNNABLE"
}

thief: "3rd process:\ndown() sees 1,\nsteals; head\nsleeps again" {
  style.stroke-dash: 4
  style.font-color: "#9ca3af"
}

up -> sem.h: "handoff: wake\nOWNING the unit" {
  style.stroke-width: 3
}
up -> sem.v: "increment?" {
  style.stroke: "#dc2626"
  style.stroke-dash: 4
}
sem.v -> thief: {
  style.stroke: "#dc2626"
  style.stroke-dash: 4
}
```

One honest caveat: the semaphore's own fields — the value, the queue —
are themselves shared state, so `down` and `up` are themselves tiny
critical sections. That isn't circular; it's a two-level design: the
semaphore's few-instruction internals are guarded by the cheap tools
from earlier (`cli`/`sti` on a uniprocessor, a spinlock on SMP), and
the semaphore then guards the long-lived resource with sleeping
waiters. Microseconds of spinning to avoid milliseconds of it: that is
the whole trade.

## What Minix did instead

Here's the twist worth savoring: Minix barely uses semaphores at all —
and *Message Passing — the Microkernel Heart* already showed you why.
In a pure message-passing OS, every shared resource is owned by exactly
one server process: the file system server owns the buffer cache, the
memory manager owns the memory maps. Nobody else *can* touch that state
— it lives in another address space, so the hardware's memory
protection is the mutual exclusion. To use the resource you message the
owner, and the owner runs a serial loop: receive one request, handle it
completely, answer, receive the next. Fifty clients are automatically
serialized because a single-threaded server only does one thing at a
time. That is synchronization *by architecture* — the rendezvous you
built in the IPC lesson quietly doubles as the lock — and it was one of
Tanenbaum's strongest cards in the famous debate.

So why this lesson? Because the trick pushes the problem down a level
rather than erasing it. *Inside* the kernel that delivers the messages,
shared structures remain — the proc table, the run queues, the clock —
touched by syscall paths and interrupt handlers alike; and any kernel
that ever wants a second CPU needs sleeping locks as a first-class
citizen. So DuckOS carries semaphores for the intra-kernel case,
exactly the honest position real kernels ended up in: message passing
for the architecture, semaphores for the plumbing. Time to build the
plumbing.

## Challenge: Semaphores {#semaphore points=20}

Implement DuckOS's counting semaphore: an integer value plus a FIFO
wait queue of process slots, with direct-handoff wake semantics. In a
real kernel, `sem_down` would run with interrupts disabled and blocking
would trigger a context switch; here we model it as plain C over a
`struct proc` array, and the tests play scheduler — they call your
functions in scripted orders and inspect the states you leave behind.

The semaphore holds its waiters in a **circular buffer**: `waiters[]`
is storage, `head` is the index of the longest-waiting slot, and
`count` is how many are queued. Empty is `count == 0`; the next free
position is `(head + count) % NPROC`; popping advances `head`
(wrapping) and decrements `count`. Since at most `NPROC` processes
exist, the queue can never overflow in real life — but guard it anyway:
defensive checks are how a kernel catches impossible states instead of
trusting them.

The contract, exactly:

- `void sem_init(struct semaphore *s, int value)` — set the counter to
  `value`, treating a negative `value` as 0 (the value is *never*
  negative), and make the wait queue empty.
- `int sem_down(struct semaphore *s, struct proc *procs, int slot)` —
  the process in `procs[slot]` tries to acquire:
  - value > 0: decrement it and return **0** (acquired; the caller
    does not block, and its state is untouched).
  - value == 0: append `slot` at the tail of the FIFO, set
    `procs[slot].state = PROC_SLEEPING`, and return **1** (blocked).
  - queue already holds `NPROC` waiters: return **-1** without
    touching anything (the can't-happen guard).
- `int sem_up(struct semaphore *s, struct proc *procs)` —
  - waiters present: pop the FIFO head, set that process's state to
    `PROC_RUNNABLE`, and return **its slot number**. The value stays
    0 — the unit is handed directly to the woken process, per the
    starvation-hole discussion above. Do not increment.
  - no waiters: increment the value and return **-1**.
- `int sem_value(const struct semaphore *s)` — the current counter.
- `int sem_waiting(const struct semaphore *s)` — how many are queued.

What the tests check: `sem_init` clamps a negative initial value to 0;
with an initial value of 2, two downs both return 0 and leave the value
at 0 without touching process states; a third down returns 1, marks its
process `PROC_SLEEPING`, and shows up in `sem_waiting`; a run of ups
wakes four queued waiters in exact FIFO order (returned slots and
resulting states both checked, including that a still-queued process
stays asleep); up with an empty queue returns -1 and increments; the
handoff subtlety (after up-with-waiter, the value is still 0 and a
fresh down blocks); a block/wake sequence long enough to wrap `head`
around the circular buffer twice; a mixed sequence during which the
value is never observed negative; the full-queue guard; and the
binary-mutex pattern (init 1, then down/up/down, none blocking).

### Starter

```c
#define NPROC 16

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;
	enum proc_state state;
};

struct semaphore {
	int value;		/* > 0: that many free units; never negative */
	int waiters[NPROC];	/* FIFO of blocked slots (circular buffer) */
	int head;		/* index of the longest-waiting slot */
	int count;		/* number of queued waiters */
};

/* Set the counter (negative treated as 0) and empty the wait queue. */
void sem_init(struct semaphore *s, int value) {
	/* TODO: clamp value at 0; reset head and count */
	(void)s;
	(void)value;
}

/*
 * procs[slot] tries to acquire one unit.
 * Returns 0 if acquired without blocking (value was > 0),
 *         1 if the caller blocked (queued FIFO, state -> PROC_SLEEPING),
 *        -1 if the wait queue is somehow already full (cannot happen
 *           with NPROC processes, but guard anyway).
 */
int sem_down(struct semaphore *s, struct proc *procs, int slot) {
	/* TODO: decrement-and-go when positive; otherwise enqueue at
	 * (head + count) % NPROC, sleep the process, report blocked */
	(void)s;
	(void)procs;
	(void)slot;
	return -1;
}

/*
 * Release one unit. If waiters are queued, hand the unit directly to
 * the FIFO head: pop it, mark it PROC_RUNNABLE, return its slot number
 * (the value stays 0 — do NOT increment). With no waiters, increment
 * the value and return -1.
 */
int sem_up(struct semaphore *s, struct proc *procs) {
	/* TODO: pop head (wrapping) and wake, or bump the value */
	(void)s;
	(void)procs;
	return -1;
}

/* Current counter value. */
int sem_value(const struct semaphore *s) {
	/* TODO */
	(void)s;
	return -1;
}

/* Number of processes blocked on the semaphore. */
int sem_waiting(const struct semaphore *s) {
	/* TODO */
	(void)s;
	return -1;
}
```

### Tests

```c
#include <stdio.h>

#define NPROC 16

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;
	enum proc_state state;
};

struct semaphore {
	int value;
	int waiters[NPROC];
	int head;
	int count;
};

void sem_init(struct semaphore *s, int value);
int sem_down(struct semaphore *s, struct proc *procs, int slot);
int sem_up(struct semaphore *s, struct proc *procs);
int sem_value(const struct semaphore *s);
int sem_waiting(const struct semaphore *s);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static void reset_procs(struct proc *procs) {
	int i;

	for (i = 0; i < NPROC; i++) {
		procs[i].pid = 100 + i;
		procs[i].state = PROC_RUNNABLE;
	}
}

int main(void) {
	struct proc procs[NPROC];
	struct semaphore s;
	int i, ok, a, b, c;

	/* init clamps a negative value to 0 */
	reset_procs(procs);
	sem_init(&s, -3);
	check(sem_value(&s) == 0 && sem_waiting(&s) == 0,
	      "test_init_clamps_negative_to_zero");
	sem_init(&s, 2);
	check(sem_value(&s) == 2 && sem_waiting(&s) == 0,
	      "test_init_positive_value");

	/* two downs on value 2 both acquire without blocking */
	reset_procs(procs);
	sem_init(&s, 2);
	a = sem_down(&s, procs, 0);
	b = sem_down(&s, procs, 1);
	check(a == 0 && b == 0 && sem_value(&s) == 0,
	      "test_down_acquires_while_positive");
	check(procs[0].state == PROC_RUNNABLE &&
	      procs[1].state == PROC_RUNNABLE,
	      "test_down_acquire_leaves_state_alone");

	/* the third down finds 0 and blocks */
	check(sem_down(&s, procs, 2) == 1 &&
	      procs[2].state == PROC_SLEEPING &&
	      sem_waiting(&s) == 1 && sem_value(&s) == 0,
	      "test_down_blocks_at_zero");

	/* up wakes waiters in FIFO order */
	reset_procs(procs);
	sem_init(&s, 0);
	sem_down(&s, procs, 5);
	sem_down(&s, procs, 3);
	sem_down(&s, procs, 9);
	sem_down(&s, procs, 1);
	check(sem_waiting(&s) == 4, "test_waiters_counted");
	a = sem_up(&s, procs);
	b = sem_up(&s, procs);
	c = sem_up(&s, procs);
	check(a == 5 && b == 3 && c == 9, "test_up_wakes_fifo_order");
	check(procs[5].state == PROC_RUNNABLE &&
	      procs[3].state == PROC_RUNNABLE &&
	      procs[9].state == PROC_RUNNABLE &&
	      procs[1].state == PROC_SLEEPING,
	      "test_up_wakes_only_the_head");
	check(sem_waiting(&s) == 1 && sem_value(&s) == 0,
	      "test_waiting_count_shrinks");

	/* up with no waiters increments the value */
	reset_procs(procs);
	sem_init(&s, 0);
	check(sem_up(&s, procs) == -1 && sem_value(&s) == 1,
	      "test_up_without_waiters_increments");

	/* direct handoff: waking a waiter must NOT increment */
	reset_procs(procs);
	sem_init(&s, 0);
	sem_down(&s, procs, 7);			/* blocks */
	a = sem_up(&s, procs);
	check(a == 7 && sem_value(&s) == 0 &&
	      procs[7].state == PROC_RUNNABLE,
	      "test_handoff_keeps_value_zero");
	check(sem_down(&s, procs, 8) == 1 &&
	      procs[8].state == PROC_SLEEPING,
	      "test_down_after_handoff_still_blocks");

	/* head must wrap around the circular buffer correctly */
	reset_procs(procs);
	sem_init(&s, 0);
	ok = 1;
	for (i = 0; i < 40; i++) {
		int slot = i % NPROC;

		if (sem_down(&s, procs, slot) != 1)
			ok = 0;
		if (sem_up(&s, procs) != slot)
			ok = 0;
		if (procs[slot].state != PROC_RUNNABLE)
			ok = 0;
		if (sem_value(&s) != 0 || sem_waiting(&s) != 0)
			ok = 0;
	}
	check(ok, "test_fifo_wraps_circular_buffer");

	/* mixed traffic: the value is never observed negative */
	reset_procs(procs);
	sem_init(&s, 1);
	ok = 1;
	sem_down(&s, procs, 0);		/* acquires: value 1 -> 0 */
	if (sem_value(&s) < 0)
		ok = 0;
	sem_down(&s, procs, 1);		/* blocks */
	sem_down(&s, procs, 2);		/* blocks */
	if (sem_value(&s) < 0)
		ok = 0;
	sem_up(&s, procs);		/* hands off to 1 */
	sem_up(&s, procs);		/* hands off to 2 */
	if (sem_value(&s) != 0)
		ok = 0;
	sem_up(&s, procs);		/* no waiters: 0 -> 1 */
	sem_up(&s, procs);		/* no waiters: 1 -> 2 */
	check(ok && sem_value(&s) == 2 && sem_waiting(&s) == 0,
	      "test_value_never_negative_mixed");

	/* binary mutex usage: init 1, lock/unlock/lock */
	reset_procs(procs);
	sem_init(&s, 1);
	a = sem_down(&s, procs, 4);	/* lock: acquired */
	b = sem_up(&s, procs);		/* unlock: no waiters */
	c = sem_down(&s, procs, 4);	/* lock again: acquired */
	check(a == 0 && b == -1 && c == 0 && sem_value(&s) == 0,
	      "test_binary_mutex_sequence");

	/* the can't-happen guard: a full queue rejects further waiters */
	reset_procs(procs);
	sem_init(&s, 0);
	ok = 1;
	for (i = 0; i < NPROC; i++)
		if (sem_down(&s, procs, i) != 1)
			ok = 0;
	check(ok && sem_waiting(&s) == NPROC, "test_all_slots_can_wait");
	check(sem_down(&s, procs, 0) == -1 && sem_waiting(&s) == NPROC,
	      "test_full_queue_guarded");

	return failed;
}
```

# Lesson: The Clock Ticks {#clock}

Everything DuckOS has built so far is reactive. The kernel sits dead in
memory until something enters it: a syscall from below, an interrupt
from outside. Take away the interrupts and a kernel is a library —
it runs only when a process politely calls it. A process that never
makes a syscall (`for (;;) ;`) would own the CPU forever, and the
multilevel feedback queue we built in *Scheduling* would be an elegant
data structure that nobody ever consults.

The fix is a piece of hardware whose only job is to interrupt on a
schedule: a timer. Wire it to the interrupt controller (IRQ0 on the
8259 — the highest-priority line, and that's no accident), and the
kernel is guaranteed to get control back, no matter what user code
does. The timer interrupt is the heartbeat of the operating system.
Preemptive multitasking is not a scheduling algorithm; it's this one
interrupt line plus the *decision* to act on it.

## The 8253 and the strangest number in your PC

The timer chip in the original IBM PC — and, emulated with perfect
fidelity, in every PC since — is the Intel 8253 Programmable Interval
Timer (its successor the 8254 adds a status readback; nobody minds the
difference). It is a box of three 16-bit down-counters fed by a
1.193182 MHz clock:

```
#define PIT_HZ 1193182u		/* the PIT input clock, in Hz */
```

Why 1,193,182 Hz? This is a genuinely great story. The NTSC color
television standard put its color subcarrier at 3.579545 MHz, so by
1981 crystal oscillators at that frequency were the cheapest precision
component money could buy — every TV in America had one. IBM's
engineers took one TV crystal, multiplied by 4 to get the 14.31818 MHz
system clock, divided by 3 for the CPU (4.77 MHz — the famous original
PC speed), and divided by 12 for the PIT: 14.31818 / 12 = 1.193182
MHz. Your gaming rig in the 2020s still tells time in units derived
from the hue of 1953 broadcast television.

The whole chain at a glance — the amber-bordered box is the counter
DuckOS programs; each edge multiplies or divides the frequency, and
÷ 11932 is the divisor we'll compute below:

```d2
direction: right

xtal: "TV crystal\n3.579545 MHz" {
  shape: oval
}
sys: "system clock\n14.31818 MHz"
cpu: "8088 CPU\n4.77 MHz"
pit: "PIT input\n1.193182 MHz" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}
irq: "IRQ0\n100 Hz"

xtal -> sys: "× 4"
sys -> cpu: "÷ 3"
sys -> pit: "÷ 12"
pit -> irq: "÷ 11932"
```

Each counter loads a 16-bit *divisor*, counts down from it at PIT_HZ,
fires its output on reaching zero, reloads, and repeats. Channel 0's
output is wired to IRQ0. So the interrupt rate is:

```
	interrupt rate = PIT_HZ / divisor
```

Two details every kernel programmer trips on:

- The divisor is 16 bits, so the *slowest* you can go is 65536 — and
  the hardware spells 65536 as 0. Writing a divisor of 0 means "count
  65536," giving 1193182 / 65536 = 18.2 Hz. That's the famous DOS
  clock: IBM's BIOS never reprogrammed the divisor, so DOS ticked
  18.2 times per second for twenty years.
- You almost never get the rate you asked for. 1193182 is not
  divisible by 100, or by 1000, or by much of anything useful. You
  compute the *nearest* divisor and accept the error: asking for
  100 Hz gets divisor 11932, which actually delivers 99.998 Hz. Every
  OS on x86 drifts by this sliver and corrects it elsewhere (NTP,
  RTC resync). Precision timekeeping is a whole other lesson; ours is
  rate generation.

In a real kernel, programming channel 0 is three `outb` instructions:
a mode byte to port 0x43 (channel 0, rate generator, low-then-high
byte access), then the divisor's low byte and high byte to port 0x40.
Here, per our usual bargain, we compute the numbers and let the tests
play the part of the silicon — the arithmetic is the part people get
wrong, not the `outb`.

## Ticks: the kernel's currency of time

DuckOS programs the PIT for `HZ` interrupts per second:

```
#define HZ 100			/* ticks per second: one every 10 ms */
```

From that moment on, the kernel does not think in seconds or
milliseconds. It thinks in **ticks**. The scheduler quantum from
*Scheduling* is in ticks. A sleeping process's wake-up time is in
ticks. Every timeout in the system is in ticks. One global counter —
traditionally called `jiffies` in Linux, `ticks` here — increments in
the interrupt handler and is the kernel's entire notion of elapsed
time.

HZ is a trade you should feel in your hands. At HZ 100 a tick is
10 ms: a `sleep(1ms)` request must round up to 10 ms, and the
scheduler can't preempt finer than that. Raise HZ and time gets
finer — but the interrupt itself has a cost (save registers, walk
timer lists, maybe reschedule, iret), paid HZ times a second whether
or not there's work. Linux shipped at HZ 100 for a decade, moved to
1000 when desktops wanted snappier interaction, retreated to 250 as a
default when servers complained about the overhead, and finally went
*tickless* — program the timer for the next actual deadline instead
of a fixed beat. Minix in 1987 used HZ 60 (that TV crystal again —
it matched the North American mains and display refresh). DuckOS uses
100 because the arithmetic is easy to check by eye.

Converting user-facing milliseconds to ticks has one rule: **round
up**. A driver that asks to sleep 15 ms and gets woken after 10 ms
had its contract violated; waking at 20 ms is merely sluggish.
Sleeping *at least* as long as asked is the invariant. Hence

```
	ticks = (ms * HZ + 999) / 1000
```

— the classic integer ceiling division, which you'll now implement.

## The delta queue

On every tick the kernel must answer: *did any timer just expire?*
Processes sleeping until tick 15083, a TTY timeout at tick 15090, a
scheduler boost at tick 15100 — there may be dozens pending. Scanning
every pending timer on every tick is O(pending) work done HZ times a
second, almost all of it discovering "no, not yet."

The classic fix — in Minix's `clock.c`, in every serious kernel since
— is to keep the timers sorted by expiry and store each one's delay as
a **delta from the timer before it**. Arm timers 5, 8, 8, and 12 ticks
out, and the list looks like this:

```
   head
    |
    v
  +-------+     +-------+     +-------+     +-------+
  | delta |     | delta |     | delta |     | delta |
  |   5   | --> |   3   | --> |   0   | --> |   4   | --> (end)
  +-------+     +-------+     +-------+     +-------+
   fires at      5+3 = 8       8+0 = 8      8+4 = 12
   tick +5
```

The magic: the tick handler touches **only the head**. Decrement its
delta; if it hit zero, pop it — and keep popping while the next delta
is zero, because a delta of 0 means "same instant as the one before
me." The two timers armed for tick +8 expire together, in the order
they were armed. Total per-tick cost: one decrement, usually nothing
else. All the sorting work moved to `arm`, which happens rarely, off
the hot path.

Insertion walks the list consuming deltas: to arm a timer 8 ticks out
into the list above, you'd walk past the 5 (8 − 5 = 3 remaining),
see the next delta 3 is bigger than... equal to 3 — walk past it too
(equal expiry goes *behind* existing timers: first armed, first
fired), walk past the existing delta-0 entry for the same reason, and
insert in front of the 4 with delta 0. Whatever follows the insertion
point must have its delta reduced by the newcomer's, so the tail of
the list still encodes the same absolute instants.

Here is that fixup on a minimal example — two timers at +5 and +12
(deltas 5 and 7), arming a third at +8. Solid arrows are the list
before the splice; dashed arrows are the links afterward (red = the
pointer that gets rewritten). The successor's delta shrinks from 7 to
4, so +12 still means +12:

```d2
direction: right

head: head {
  shape: oval
  style.stroke: "#d97706"
}
t5: "Δ 5\nfires +5"
new: "Δ 3\nfires +8" {
  style.stroke: "#16a34a"
  style.stroke-width: 3
}
t12: "Δ 7 → 4\nfires +12"
nil: "∅" {shape: text}

head -> t5
t5 -> t12: before
t5 -> new: {
  style.stroke-dash: 4
  style.stroke: "#dc2626"
}
new -> t12: {
  style.stroke-dash: 4
}
t12 -> nil
```

Cancellation is the inverse, and it's where delta queues bite the
unwary: unlinking a timer must **add** its delta to its successor, or
every timer behind it fires early. Minix got this right in 1987; a
depressing number of hobby kernels since have not. Our tests check it
both ways.

Cancelling the middle of three timers at +3, +6, +10: the
red-bordered node is unlinked, and its Δ 3 travels along the dashed
red link to the survivor, whose delta grows from 4 to 7 — skip the
donation and it fires at +7, three ticks early:

```d2
direction: right

head: head {
  shape: oval
  style.stroke: "#d97706"
}
a: "Δ 3\nfires +3"
b: "Δ 3\nfires +6" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
c: "Δ 4 → 7\nstill fires +10"
nil: "∅" {shape: text}

head -> a
a -> b
b -> c
a -> c: "+3 donated" {
  style.stroke-dash: 4
  style.stroke: "#dc2626"
}
c -> nil
```

What runs when a timer fires? In DuckOS, expiry hands back an *owner*
— an opaque int the arming code chose. The clock driver doesn't know
or care that owner 3 is a process slot to make RUNNABLE (that wiring
happens in the final challenge, *DuckOS, Assembled*) and owner 200 is
a scheduler boost. The delta queue is pure mechanism; policy lives
with the caller — the same separation we've drawn in every lesson
since *Scheduling*.

## Challenge: Programming the PIT {#pit-divisor points=10}

Compute what the kernel would feed the 8253, and be honest about the
rates it can't hit. Three functions:

`pit_divisor(hz)` returns the divisor for a desired interrupt rate,
**rounded to nearest** — `(PIT_HZ + hz/2) / hz` — then clamped to the
hardware's range: results above 65536 (including the hz == 0 case)
become 65536, results below 1 become 1. Return the *true* divisor
(1..65536): the caller would write `divisor & 0xFFFF` to the chip, so
65536 goes over the wire as 0, but that encoding is the caller's
problem, not yours.

`pit_actual_hz(divisor)` answers "what rate will I really get?" —
`PIT_HZ / divisor`, rounded to nearest, treating divisor 0 as the
hardware does: 65536.

`ms_to_ticks(ms)` converts milliseconds to ticks at `HZ` (100),
rounding **up** so a sleep never ends early. 0 ms is 0 ticks.

The tests pin the classics: 100 Hz → 11932, the round trip back to
100 Hz, 1000 Hz → 1193, the 18.2 Hz DOS clock falling out of the
divisor-0 convention, and the clamps at both ends.

### Starter

```c
#include <stdint.h>

#define PIT_HZ 1193182u		/* the PIT input clock, in Hz */
#define HZ 100			/* DuckOS ticks per second */

/*
 * Divisor for a desired interrupt rate, rounded to nearest, clamped
 * to the 8253's real range [1, 65536]. hz == 0 asks for "as slow as
 * possible": 65536. (The chip encodes 65536 as a written 0 -- that
 * translation is the out-port code's job, not ours.)
 */
uint32_t pit_divisor(uint32_t hz)
{
	/* TODO: guard hz == 0; divide rounding to nearest; clamp both
	 * ends. Mind the order -- the rounded quotient can exceed
	 * 65536 only for tiny hz, and can reach 0 only for huge hz. */
	(void)hz;
	return 0;
}

/* The rate a divisor really delivers: PIT_HZ / divisor, rounded to
 * nearest. Divisor 0 means 65536, as on the chip. */
uint32_t pit_actual_hz(uint32_t divisor)
{
	/* TODO */
	(void)divisor;
	return 0;
}

/* Milliseconds to ticks at HZ, rounding UP: a sleep may run long,
 * never short. */
uint32_t ms_to_ticks(uint32_t ms)
{
	/* TODO: integer ceiling division */
	(void)ms;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define PIT_HZ 1193182u
#define HZ 100

uint32_t pit_divisor(uint32_t hz);
uint32_t pit_actual_hz(uint32_t divisor);
uint32_t ms_to_ticks(uint32_t ms);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void)
{
	check(pit_divisor(100) == 11932, "test_divisor_for_100hz");
	check(pit_actual_hz(11932) == 100, "test_actual_hz_round_trip");
	check(pit_divisor(1000) == 1193, "test_divisor_for_1000hz");
	check(pit_divisor(60) == 19886, "test_divisor_rounds_to_nearest");

	/* Asking for 18 Hz wants divisor 66288 -- more than 16 bits.
	 * The chip's floor is 18.2 Hz: clamp to 65536. */
	check(pit_divisor(18) == 65536, "test_slow_request_clamps_high");
	check(pit_divisor(1) == 65536, "test_1hz_clamps_high");
	check(pit_divisor(0) == 65536, "test_zero_hz_means_slowest");

	/* Faster than the input clock: divisor floors at 1. */
	check(pit_divisor(2000000) == 1, "test_fast_request_clamps_low");

	/* Divisor 0 is the chip's spelling of 65536: the DOS clock. */
	check(pit_actual_hz(0) == 18, "test_divisor_zero_is_dos_clock");
	check(pit_actual_hz(65536) == 18, "test_divisor_65536_same_rate");
	check(pit_actual_hz(1) == PIT_HZ, "test_divisor_one_full_rate");

	check(ms_to_ticks(10) == 1, "test_10ms_is_one_tick");
	check(ms_to_ticks(15) == 2, "test_15ms_rounds_up");
	check(ms_to_ticks(1) == 1, "test_1ms_rounds_up_to_a_tick");
	check(ms_to_ticks(0) == 0, "test_0ms_is_zero_ticks");
	check(ms_to_ticks(1000) == 100, "test_one_second_is_hz_ticks");

	return failed;
}
```

## Challenge: The Delta Queue {#timer-queue points=20}

Build the timer list the tick handler walks. Timers live in a static
pool of `NTIMERS` entries (no allocation in the interrupt path —
the same discipline as the proc table in *Processes: the Kernel's
Bookkeeping*), linked by index. Each entry's `delta` is ticks
*after the entry before it*; the head's delta is ticks from now.

`tq_arm(q, owner, ticks)` schedules owner to expire `ticks` ticks
from now (`ticks >= 1`; 0 is a bug, return -1). Find a free pool slot
(-1 if none), walk the list consuming deltas to find the insertion
point — equal expiry inserts **after** existing entries (FIFO among
equals) — link the new timer in with the remaining delta, and
subtract that delta from its successor, if any. Returns the pool
index: that's the timer id `tq_cancel` takes.

`tq_tick(q, expired, max)` is the per-tick work. Empty list: return
0. Otherwise decrement the head's delta once, then pop **every**
leading timer whose delta is 0, storing owners into `expired[]` in
list order (the tests never pass a max smaller than the expiry
count), and return how many expired.

`tq_cancel(q, id)` unlinks a pending timer: add its delta to its
successor (that's the bite — skip it and everything behind fires
early), free the slot, return 0. Bad or free id: -1.

`tq_remaining(q, id)` reports how many ticks until `id` fires — the
sum of deltas from the head through `id` — or 0 if id is invalid or
not pending. The tests use it to X-ray your delta arithmetic without
caring how you chain the pool internally.

### Starter

```c
#include <stdint.h>

#define NTIMERS 16

struct timer {
	int in_use;
	int owner;		/* who to wake; opaque to the queue */
	uint32_t delta;		/* ticks AFTER the previous list entry */
	int next;		/* pool index of next timer, -1 = end */
};

struct timerq {
	struct timer t[NTIMERS];
	int head;		/* pool index of first timer, -1 = empty */
};

/* Empty queue: every pool slot free, no list. */
void tq_init(struct timerq *q)
{
	/* TODO: clear in_use on every slot, head = -1 */
	(void)q;
}

/*
 * Arm a timer: owner expires `ticks` ticks from now (ticks >= 1).
 * Returns the timer id (pool index), or -1 if ticks == 0 or the pool
 * is exhausted. Insert in expiry order; equal expiry goes AFTER
 * existing timers. Remember to subtract the new timer's delta from
 * its successor.
 */
int tq_arm(struct timerq *q, int owner, uint32_t ticks)
{
	/* TODO: find a free slot; walk from head consuming deltas
	 * while the current entry's delta <= remaining; splice in;
	 * fix the successor's delta. */
	(void)q;
	(void)owner;
	(void)ticks;
	return -1;
}

/*
 * One clock tick. Decrement the head's delta; pop every leading
 * timer with delta 0 into expired[] (at most max entries are
 * stored; the tests never overflow). Returns the number expired.
 */
int tq_tick(struct timerq *q, int *expired, int max)
{
	/* TODO */
	(void)q;
	(void)expired;
	(void)max;
	return 0;
}

/*
 * Cancel a pending timer. Its delta must be ADDED to its successor,
 * or every timer behind it fires early. Returns 0, or -1 if id is
 * out of range or not armed.
 */
int tq_cancel(struct timerq *q, int id)
{
	/* TODO: find the predecessor, unlink, donate the delta */
	(void)q;
	(void)id;
	return -1;
}

/* Ticks until timer id fires: sum of deltas from head through id.
 * 0 if id is invalid or not pending. */
uint32_t tq_remaining(const struct timerq *q, int id)
{
	/* TODO */
	(void)q;
	(void)id;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define NTIMERS 16

struct timer {
	int in_use;
	int owner;		/* who to wake; opaque to the queue */
	uint32_t delta;		/* ticks AFTER the previous list entry */
	int next;		/* pool index of next timer, -1 = end */
};

struct timerq {
	struct timer t[NTIMERS];
	int head;		/* pool index of first timer, -1 = empty */
};

void tq_init(struct timerq *q);
int tq_arm(struct timerq *q, int owner, uint32_t ticks);
int tq_tick(struct timerq *q, int *expired, int max);
int tq_cancel(struct timerq *q, int id);
uint32_t tq_remaining(const struct timerq *q, int id);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

int main(void)
{
	struct timerq q;
	int exp[8];
	int i, n, id, id5, id8, ok;

	/* A timer for +3 fires on the 3rd tick, not before, not after. */
	tq_init(&q);
	id = tq_arm(&q, 7, 3);
	ok = id >= 0;
	ok = ok && tq_tick(&q, exp, 8) == 0;
	ok = ok && tq_tick(&q, exp, 8) == 0;
	n = tq_tick(&q, exp, 8);
	check(ok && n == 1 && exp[0] == 7, "test_single_timer_exact_tick");
	check(tq_tick(&q, exp, 8) == 0, "test_expired_timer_gone");

	/* Ticking an empty queue is free. */
	tq_init(&q);
	check(tq_tick(&q, exp, 8) == 0, "test_tick_empty_queue");

	/* Two timers on the same tick expire together, in arm order. */
	tq_init(&q);
	tq_arm(&q, 1, 2);
	tq_arm(&q, 2, 2);
	tq_tick(&q, exp, 8);
	n = tq_tick(&q, exp, 8);
	check(n == 2 && exp[0] == 1 && exp[1] == 2,
	      "test_same_tick_fifo_expiry");

	/* Deltas encode differences: arm 5 then 8 and both report their
	 * absolute remaining time. */
	tq_init(&q);
	id5 = tq_arm(&q, 5, 5);
	id8 = tq_arm(&q, 8, 8);
	check(tq_remaining(&q, id5) == 5 && tq_remaining(&q, id8) == 8,
	      "test_remaining_after_in_order_arm");

	/* Insert BEFORE an existing timer: the later one's remaining
	 * time must not change (its delta was fixed up). */
	tq_init(&q);
	id8 = tq_arm(&q, 8, 8);
	id5 = tq_arm(&q, 5, 5);
	check(tq_remaining(&q, id5) == 5 && tq_remaining(&q, id8) == 8,
	      "test_insert_before_fixes_successor");

	/* And both still fire at the right instants. */
	for (i = 0, n = 0; i < 5; i++)
		n = tq_tick(&q, exp, 8);
	check(n == 1 && exp[0] == 5, "test_early_insert_fires_first");
	for (i = 0, n = 0; i < 3; i++)
		n = tq_tick(&q, exp, 8);
	check(n == 1 && exp[0] == 8, "test_late_timer_fires_on_time");

	/* Cancel the middle timer of three; the last stays accurate. */
	tq_init(&q);
	tq_arm(&q, 1, 3);
	id = tq_arm(&q, 2, 6);
	tq_arm(&q, 3, 10);
	check(tq_cancel(&q, id) == 0, "test_cancel_returns_ok");
	check(tq_remaining(&q, id) == 0, "test_cancelled_not_pending");
	ok = 1;
	for (i = 0, n = 0; i < 3; i++)
		n = tq_tick(&q, exp, 8);
	ok = ok && n == 1 && exp[0] == 1;
	for (i = 0, n = 0; i < 7; i++) {
		n = tq_tick(&q, exp, 8);
		if (i < 6 && n != 0)
			ok = 0;		/* nothing may fire early */
	}
	check(ok && n == 1 && exp[0] == 3,
	      "test_cancel_middle_keeps_tail_on_time");

	/* Cancelling the head donates its delta to the new head. */
	tq_init(&q);
	id = tq_arm(&q, 1, 4);
	tq_arm(&q, 2, 9);
	tq_cancel(&q, id);
	check(tq_remaining(&q, tq_arm(&q, 3, 20)) == 20 &&
	      tq_tick(&q, exp, 8) == 0,
	      "test_cancel_head_no_early_fire");

	/* (that arm also proves the freed slot is reusable) */

	/* Degenerate arms. */
	tq_init(&q);
	check(tq_arm(&q, 1, 0) == -1, "test_arm_zero_ticks_rejected");
	for (i = 0; i < NTIMERS; i++)
		tq_arm(&q, i, 5);
	check(tq_arm(&q, 99, 5) == -1, "test_pool_exhaustion");
	check(tq_cancel(&q, -1) == -1 && tq_cancel(&q, NTIMERS) == -1,
	      "test_cancel_bad_id");
	tq_init(&q);
	check(tq_cancel(&q, 0) == -1, "test_cancel_unarmed_id");

	return failed;
}
```

The delta queue is the last standalone mechanism DuckOS needs. Every
piece is now on the bench: a screen, a heap, page tables, a proc
table, a scheduler, IPC, and a heartbeat. The keyboard and the
filesystem lessons ahead add the devices a real system talks to — and
then the final challenge bolts the core together and turns the crank.

# Lesson: The Keyboard {#keyboard}

The clock we built in *The Clock Ticks* is a device, but it only ever
says one thing: tick. The keyboard is the first device that *speaks* —
and writing its driver teaches the lesson every driver you will ever
write repeats in a different costume. Hardware does not deliver what
your program wants. It delivers events in its own vocabulary, shaped by
the electrical and historical constraints of the device, and the
driver's job is two things: **translation** (turn the device's
vocabulary into the kernel's) and **state** (remember enough of the
recent past to translate correctly, because the device's messages only
make sense in context).

The keyboard is the perfect first driver because both halves are
visible in miniature. The vocabulary is genuinely alien — you will see
below that the byte the keyboard sends when you press `A` contains no
trace of the letter A — and the translation genuinely needs memory:
the same byte must decode differently depending on what arrived before
it. By the end of this lesson you will have written a real scancode
decoder, the same state machine that sits at the bottom of every PC
operating system since 1981.

## From Keypress to Interrupt

The keyboard is not wired straight to the CPU. Inside the keyboard
itself sits a small microcontroller (an Intel 8048 in the original IBM
PC keyboard) that scans the key matrix a few hundred times a second,
notices a key changing state, and clocks a byte out serially over the
keyboard cable. On the motherboard, another microcontroller — the
famous **Intel 8042**, the "keyboard controller" — receives that byte,
latches it into a one-byte output buffer readable at I/O **port
0x60**, and raises **IRQ1**.

From there the path is exactly the machinery we built in *Interrupts
and the IDT*: IRQ1 enters pin 1 of the master 8259 PIC, which (after
our remapping) delivers vector 33 to the CPU, which indexes the IDT
and lands in the keyboard handler. The handler's contract with the
8042 is simple but strict: read port 0x60 to fetch the byte — the
read is also what frees the 8042's buffer for the next one — then
send EOI to the PIC:

The whole delivery path — one byte rides an interrupt from the keyboard to your amber decoder, and only real characters continue on:

```d2
direction: right

kb: "keyboard\n(8048)" {
  shape: oval
}
isr: "IRQ1 → vec 33:\nsc = inb(0x60)"
dec: "kbd_decode\n(&state, sc)" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}
tty: "tty_input(ch)\nif ch != KEY_NONE" {
  shape: oval
}

kb -> isr: "byte @\nport 0x60"
isr -> dec
dec -> tty
```

```c
void kbd_interrupt(void)
{
	uint8_t sc = inb(0x60);		/* one scancode byte */
	int ch = kbd_decode(&kbd_state, sc);
	if (ch != KEY_NONE)
		tty_input(ch);		/* hand off to the line discipline */
	pic_send_eoi(1);
}
```

One interrupt, one byte. Everything interesting happens inside
`kbd_decode`, and that is what you will build. In a real kernel the
bytes come from port 0x60 inside this IRQ1 handler; here, as
everywhere in DuckOS, we build the driver as hostable C — the tests
play the role of the 8042 and feed your decoder the exact byte
stream a real keyboard would produce.

## Keys, Not Characters

Here is the byte the keyboard sends when you press the `A` key:

```
0x1E
```

Not 0x41 (`'A'`), not 0x61 (`'a'`). The keyboard has no idea what
letter is painted on the keycap. What it reports is a **key number** —
an index of the physical switch that closed — called a **scancode**.
And it reports *two* events per keystroke, because pressing and
releasing are separate facts:

- the **make code** when the key goes down: `0x1E`
- the **break code** when it comes back up: make code with bit 7 set,
  `0x1E | 0x80 = 0x9E`

Hold the key and the keyboard helpfully repeats the make code
(**typematic repeat**, ~10.9 codes/second after a half-second delay —
both configurable by sending the keyboard a command):

```
press A, hold, release:   1E  1E  1E  1E  9E
                          │   └── repeats ──┘  │
                          make                 break
```

This is **scancode set 1**, the vocabulary of the original 1981 IBM
PC/XT keyboard. Later keyboards actually speak set 2 on the wire, but
the 8042 by default *translates* everything back to set 1 before your
driver sees it — IBM could not break the software installed base — so
set 1 is what arrives at port 0x60 on essentially every PC, and set 1
is what every PC operating system decodes. The codes are transparently
positional: they simply count across the physical rows of the XT
keyboard, which you can still see in the layout today:

The decode order as a decision ladder — each numbered test either answers immediately (right column) or falls through to the next; ctrl (red) wins before any table lookup:

```d2
grid-columns: 2
grid-gap: 40

d1: "1 · e0 pending?" {shape: diamond}
r1: "clear e0; arrow make → KEY_*,\nanything else → KEY_NONE"
d2: "2 · byte == 0xE0?" {shape: diamond}
r2: "set e0 → KEY_NONE"
d3: "3 · bit 7 set? (break)" {shape: diamond}
r3: "shift-- (floor 0), ctrl = 0\n→ KEY_NONE"
d4: "4 · modifier make?" {shape: diamond}
r4: "shift++ / ctrl = 1 / caps ^= 1\n→ KEY_NONE"
d5: "5 · ctrl held + letter?" {shape: diamond}
r5: "letter - 'a' + 1" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
tbl: "shift > 0 ? keymap_shift : keymap\n0 → KEY_NONE; caps flips letters only"

d1 -> r1: yes
d1 -> d2: no
d2 -> r2: yes
d2 -> d3: no
d3 -> r3: yes
d3 -> d4: no
d4 -> r4: yes
d4 -> d5: no
d5 -> r5: "yes: ctrl wins"
d5 -> tbl: no
```

```
 01    02  03  04  05  06  07  08  09  0A  0B  0C  0D   0E
[Esc]  [1] [2] [3] [4] [5] [6] [7] [8] [9] [0] [-] [=] [Bksp]

 0F    10  11  12  13  14  15  16  17  18  19  1A  1B   1C
[Tab]  [Q] [W] [E] [R] [T] [Y] [U] [I] [O] [P] [[] []] [Enter]

 3A     1E  1F  20  21  22  23  24  25  26  27  28
[Caps]  [A] [S] [D] [F] [G] [H] [J] [K] [L] [;] [']

 2A      2C  2D  2E  2F  30  31  32  33  34  35   36
[Shift]  [Z] [X] [C] [V] [B] [N] [M] [,] [.] [/] [Shift]

 1D           39
[Ctrl]      [Space]
```

`Q W E R T Y U I O P` really is `10 11 12 13 14 15 16 17 18 19` — the
encoding *is* the geometry.

Why positions instead of characters? Because which character a key
produces is **policy, not mechanism**, and policy belongs in software.
A French AZERTY keyboard, a German QWERTZ keyboard, and a Dvorak
typist's remapped board are all, electrically, the same switches
sending the same scancodes; only a table in the driver differs. Bake
`'a'` into the hardware and every layout on earth needs different
silicon. There is a second, subtler reason: characters cannot express
*held keys*. Shift produces no character, yet the driver must know it
is down; a game needs to know that `W` is still held three seconds
after the press. Make/break pairs carry exactly that information — a
character stream never could.

## A Character Is a Function of State

So the driver receives key transitions and must produce characters.
But look at what that requires: the byte `0x1E` should become `'a'`,
or `'A'`, or ASCII 1 (Ctrl-A) — *depending on bytes that arrived
earlier and have not been "un-arrived" yet*. A character is not a
property of a scancode. It is a function of the scancode **and the
modifier state**:

```
char = f(scancode, shift_held, ctrl_held, caps_lock)
```

That makes the driver a **state machine**, and the state it carries is
exactly the recent history the scancodes alone don't repeat:

```c
struct kbd {
	int shift;	/* count of shift keys held (L+R) */
	int ctrl;	/* left ctrl held */
	int caps;	/* caps lock toggle */
	int e0;		/* saw 0xE0 prefix, awaiting second byte */
};
```

Note `shift` is a **count**, not a flag. There are two shift keys
(makes `0x2A` and `0x36`), and fast typists really do overlap them:
right shift down for the `H` of "Hi", left shift down for the `!`
before right shift is fully up. A boolean that any shift release sets
to false would drop the second shift — a real bug that has shipped in
real drivers. Count presses up and releases down, and shifted means
`shift > 0`.

Watch the whole machine run. Typing `Hi!` produces ten interrupts and
three characters:

```
byte  meaning             state after   decoder returns
----  ------------------  -----------   ---------------
0x2A  left shift make     shift=1       KEY_NONE
0x23  'h' key make        shift=1       'H'
0xA3  'h' key break       shift=1       KEY_NONE
0xAA  left shift break    shift=0       KEY_NONE
0x17  'i' key make        shift=0       'i'
0x97  'i' key break       shift=0       KEY_NONE
0x2A  left shift make     shift=1       KEY_NONE
0x02  '1' key make        shift=1       '!'
0x82  '1' key break       shift=1       KEY_NONE
0xAA  left shift break    shift=0       KEY_NONE
```

Seven of the ten bytes produce no character at all — they exist only
to update state. This is why pressing shift by itself prints nothing:
its make code is a pure state transition, `KEY_NONE` to the caller.

## Caps Lock Is Not a Sticky Shift

It is tempting to implement caps lock as "shift that stays on," and it
is wrong in two observable ways.

First, caps lock affects **letters only**. With caps on, `0x02` still
produces `'1'`, not `'!'` — anyone who has typed a numeric password
with caps lock on has relied on this. Shift, by contrast, selects a
whole second character for every key.

Second, caps and shift **cancel on letters** — the behavior is an XOR
of the two, not an OR:

```
                    caps off    caps on
key 0x1E            'a'         'A'
shift + key 0x1E    'A'         'a'      <- shift undoes caps
key 0x02            '1'         '1'      <- caps ignores digits
shift + key 0x02    '!'         '!'
```

Mechanically: caps is a toggle flipped on its **make** code (releasing
caps lock does nothing, and the keyboard's typematic repeat never
applies in our tests, so you may toggle on every make you see). Then,
after shift has selected the character, if caps is on and the result
is a letter, flip that letter's case — and touch nothing else.

One more piece of driver truth hides in that little LED on the key:
the caps lock light does *not* turn itself on. The toggle lives in
your `struct kbd`, in software, and a real driver must send a command
byte (0xED) back through the 8042 to make the LED agree with the
state. If an OS crashes hard enough, you can sometimes still see the
corpse's last opinion in the LEDs.

## Ctrl and the Codes Below 0x20

Hold ctrl and press `C`, and the driver should emit neither `'c'` nor
`'C'` but the single byte `0x03`. This is ASCII's design showing
through: the control characters 1–26 were laid out so that Ctrl+letter
is the letter with its two high bits cleared:

```
'c'      = 0110 0011  (0x63)
Ctrl-C   = 0000 0011  (0x03)  = 'c' & 0x1F  =  ('c' - 'a' + 1)
```

which is why the terminals of the 1970s could generate every control
code from a keyboard — and why the mapping survives in every driver
since:

```
ctrl +   byte   ASCII name   what the TTY will do with it
  c      0x03   ETX          interrupt character -> SIGINT
  d      0x04   EOT          end of input
  h      0x08   BS           the same byte as backspace
  i      0x09   HT           the same byte as tab
  z      0x1A   SUB          suspend -> SIGTSTP
```

Notice the keyboard driver itself attaches **no meaning** to 0x03. It
translates and passes on. Deciding that ETX means "kill the foreground
process" is the business of the layer above us — the line discipline
we build in *The TTY and the Line Discipline*, where 0x03 becomes the
interrupt character VINTR. Drivers translate; policy lives upstairs.

For decoding, the rule is: if ctrl is held and the key's *unshifted*
mapping is a letter `'a'..'z'`, return `letter - 'a' + 1`. Ctrl wins
over shift and caps — Ctrl-C is 0x03 no matter how it was capitalized.

## The 0xE0 Prefix: When One Byte Isn't Enough

Set 1 make codes must fit in 7 bits, because bit 7 means break. That
gave the 83-key XT keyboard room to spare — until 1986, when the IBM
Model M "Enhanced" keyboard grew to 101 keys and added, among others,
a **dedicated arrow cluster** (before that, arrows existed only on the
numeric keypad, shared with the digits via NumLock). The 7-bit code
space couldn't cleanly fit the newcomers, and worse, existing software
had the old codes burned in. IBM's escape hatch: new keys send **two
bytes**, the prefix `0xE0` followed by a second code — and, in a
genuinely elegant hack, the second byte *reuses the code of the old
key it duplicates*:

```
key            make     break
up arrow       E0 48    E0 C8      (keypad 8 is:  48 / C8)
down arrow     E0 50    E0 D0      (keypad 2 is:  50 / D0)
left arrow     E0 4B    E0 CB      (keypad 4 is:  4B / CB)
right arrow    E0 4D    E0 CD      (keypad 6 is:  4D / CD)
right ctrl     E0 1D    E0 9D      (left ctrl is: 1D / 9D)
```

Old software that ignored the unfamiliar `E0` byte saw the new up
arrow as keypad-8 — which was the up arrow key it already understood.
Backward compatibility by pun.

For the driver, `0xE0` means the current byte cannot be decoded alone:
the machine needs a **one-byte memory**. That is the `e0` field in
`struct kbd`: seeing `0xE0` sets the flag and produces nothing;
whatever byte comes next is interpreted in "extended" context and
clears the flag. Extended *break* codes (second byte with bit 7 set)
just clear state silently, like ordinary breaks — and any extended
code you don't recognize should decode to `KEY_NONE`, flag cleared,
leaving the machine clean for the next byte. That tolerance is not
just defensive style: real keyboards inject extra `E0 2A` / `E0 AA`
"fake shift" sequences around some navigation keys for even more
backward-compatibility reasons, and drivers that choke on unexpected
extended codes get corrupted by real hardware within seconds.

Since arrows aren't characters, the decoder's return type must be
wider than `char`: we return an `int` that is either an ASCII value
(> 0), `KEY_NONE` (0), or an out-of-band key code (≥ 0x100, safely
above any ASCII value) for the arrows.

## The Decoder, Assembled

Every rule above composes into one function with a fixed order of
checks. On each byte:

1. **E0 pending?** Clear the flag. Arrow make codes map to `KEY_UP` /
   `KEY_DOWN` / `KEY_LEFT` / `KEY_RIGHT`; anything else — extended
   breaks included — is `KEY_NONE`.
2. **The byte is 0xE0?** Set the flag, return `KEY_NONE`.
3. **Bit 7 set (break)?** Strip bit 7. A shift break decrements the
   shift count (never below zero — a stray release must not wedge the
   machine); a ctrl break clears ctrl. All breaks return `KEY_NONE`.
4. **Modifier make?** Shift makes increment the count; ctrl make sets
   ctrl; caps make toggles caps. All return `KEY_NONE`.
5. **Anything else** is a printable key's make code: apply ctrl, then
   shift, then caps, and look the character up.

That ordered list is the whole driver. Time to write it.

## Challenge: Scancodes to Characters {#scancode-decode points=20}

Implement the DuckOS scancode decoder: `kbd_init`, which resets the
driver state, and `kbd_decode`, which consumes one scancode byte and
returns what the rest of the kernel should see.

The starter provides — fully implemented, keep them as-is — the two
set-1 lookup tables `keymap` (unshifted) and `keymap_shift` (shifted),
covering letters, digits, space, enter (`'\n'`), backspace (`'\b'`),
tab, and US punctuation, with 0 in every unmapped slot. It also
defines the modifier scancodes and the extended arrow codes. You
implement the two functions.

The contract, in decode order (it matches the numbered list above):

- `kbd_init(k)` zeroes all four state fields.
- `kbd_decode(k, sc)` returns an ASCII character (> 0), a `KEY_*`
  arrow code (≥ 0x100), or `KEY_NONE` (0).
- If `k->e0` is set: clear it; the four arrow make codes return their
  `KEY_*` values; any other second byte (including extended breaks,
  bit 7 set) returns `KEY_NONE`.
- `0xE0` sets `k->e0` and returns `KEY_NONE`.
- Break codes (`sc & 0x80`): a shift break decrements `shift` but
  never below 0; a ctrl break clears `ctrl`; caps break does nothing.
  Every break returns `KEY_NONE`.
- Modifier makes: shift makes increment `shift` (both shift keys feed
  the same counter), ctrl make sets `ctrl`, caps make toggles `caps`
  (the tests never send typematic repeats of caps lock). All return
  `KEY_NONE`.
- Any other make code: if `ctrl` is set and the *unshifted* table maps
  the key to `'a'..'z'`, return `letter - 'a' + 1`. Otherwise select
  `keymap_shift` when `shift > 0`, else `keymap`; a 0 entry means
  unmapped → `KEY_NONE`; then, if `caps` is set, flip the case of
  letters (and only letters) and return the result.

The tests replay real byte streams at your decoder: plain makes and
breaks, overlapped double-shift typing (`test_two_shifts` — the
counter bug described above fails it), stray shift releases, the
caps/shift XOR table, `caps + '1'`, Ctrl-C as ETX and its release,
all four extended arrows, extended breaks leaving the state clean, a
stray `0xE0` swallowing exactly one following byte, enter, backspace,
and an unmapped scancode.

### Starter

```c
#include <stdint.h>

/* kbd_decode returns: an ASCII char (> 0), KEY_NONE (0), or one of
 * these out-of-band codes (>= 0x100, above any ASCII value). */
#define KEY_NONE  0
#define KEY_UP    0x100
#define KEY_DOWN  0x101
#define KEY_LEFT  0x102
#define KEY_RIGHT 0x103

/* Set-1 modifier make codes (their breaks are these | 0x80). */
#define SC_LSHIFT 0x2A
#define SC_RSHIFT 0x36
#define SC_LCTRL  0x1D
#define SC_CAPS   0x3A

/* Second byte of the 0xE0-prefixed arrow make codes. */
#define SC_E0_UP    0x48
#define SC_E0_DOWN  0x50
#define SC_E0_LEFT  0x4B
#define SC_E0_RIGHT 0x4D

struct kbd {
	int shift;	/* count of shift keys held (L+R) */
	int ctrl;	/* left ctrl held */
	int caps;	/* caps lock toggle */
	int e0;		/* saw 0xE0 prefix, awaiting second byte */
};

/* Scancode set 1 -> ASCII, unshifted. 0 = no character. */
static const char keymap[128] = {
	[0x02] = '1',  [0x03] = '2', [0x04] = '3',  [0x05] = '4',
	[0x06] = '5',  [0x07] = '6', [0x08] = '7',  [0x09] = '8',
	[0x0A] = '9',  [0x0B] = '0', [0x0C] = '-',  [0x0D] = '=',
	[0x0E] = '\b', [0x0F] = '\t',
	[0x10] = 'q',  [0x11] = 'w', [0x12] = 'e',  [0x13] = 'r',
	[0x14] = 't',  [0x15] = 'y', [0x16] = 'u',  [0x17] = 'i',
	[0x18] = 'o',  [0x19] = 'p', [0x1A] = '[',  [0x1B] = ']',
	[0x1C] = '\n',
	[0x1E] = 'a',  [0x1F] = 's', [0x20] = 'd',  [0x21] = 'f',
	[0x22] = 'g',  [0x23] = 'h', [0x24] = 'j',  [0x25] = 'k',
	[0x26] = 'l',  [0x27] = ';', [0x28] = '\'', [0x29] = '`',
	[0x2B] = '\\',
	[0x2C] = 'z',  [0x2D] = 'x', [0x2E] = 'c',  [0x2F] = 'v',
	[0x30] = 'b',  [0x31] = 'n', [0x32] = 'm',  [0x33] = ',',
	[0x34] = '.',  [0x35] = '/',
	[0x39] = ' ',
};

/* Scancode set 1 -> ASCII with shift held. 0 = no character. */
static const char keymap_shift[128] = {
	[0x02] = '!',  [0x03] = '@', [0x04] = '#',  [0x05] = '$',
	[0x06] = '%',  [0x07] = '^', [0x08] = '&',  [0x09] = '*',
	[0x0A] = '(',  [0x0B] = ')', [0x0C] = '_',  [0x0D] = '+',
	[0x0E] = '\b', [0x0F] = '\t',
	[0x10] = 'Q',  [0x11] = 'W', [0x12] = 'E',  [0x13] = 'R',
	[0x14] = 'T',  [0x15] = 'Y', [0x16] = 'U',  [0x17] = 'I',
	[0x18] = 'O',  [0x19] = 'P', [0x1A] = '{',  [0x1B] = '}',
	[0x1C] = '\n',
	[0x1E] = 'A',  [0x1F] = 'S', [0x20] = 'D',  [0x21] = 'F',
	[0x22] = 'G',  [0x23] = 'H', [0x24] = 'J',  [0x25] = 'K',
	[0x26] = 'L',  [0x27] = ':', [0x28] = '"',  [0x29] = '~',
	[0x2B] = '|',
	[0x2C] = 'Z',  [0x2D] = 'X', [0x2E] = 'C',  [0x2F] = 'V',
	[0x30] = 'B',  [0x31] = 'N', [0x32] = 'M',  [0x33] = '<',
	[0x34] = '>',  [0x35] = '?',
	[0x39] = ' ',
};

/* Reset the decoder: no modifiers held, caps off, no pending prefix. */
void kbd_init(struct kbd *k)
{
	/* TODO: zero all four fields */
	(void)k;
}

/* Consume one scancode byte; return an ASCII char (> 0), a KEY_*
 * code, or KEY_NONE. Updates *k as a side effect. Decode order:
 * pending 0xE0, then 0xE0 itself, then breaks, then modifier makes,
 * then character lookup (ctrl beats shift beats caps). */
int kbd_decode(struct kbd *k, uint8_t sc)
{
	/* TODO:
	 * 1. if k->e0: clear it; arrow makes -> KEY_*; else KEY_NONE
	 * 2. 0xE0: set k->e0, KEY_NONE
	 * 3. breaks (sc & 0x80): shift-- (floor 0), ctrl = 0; KEY_NONE
	 * 4. modifier makes: shift++ / ctrl = 1 / caps ^= 1; KEY_NONE
	 * 5. else look up: ctrl+letter -> letter - 'a' + 1;
	 *    shift picks keymap_shift; caps flips letter case only;
	 *    0 in the table -> KEY_NONE
	 */
	(void)k;
	(void)sc;
	(void)keymap;
	(void)keymap_shift;
	return KEY_NONE;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define KEY_NONE  0
#define KEY_UP    0x100
#define KEY_DOWN  0x101
#define KEY_LEFT  0x102
#define KEY_RIGHT 0x103

struct kbd {
	int shift;	/* count of shift keys held (L+R) */
	int ctrl;	/* left ctrl held */
	int caps;	/* caps lock toggle */
	int e0;		/* saw 0xE0 prefix, awaiting second byte */
};

void kbd_init(struct kbd *k);
int kbd_decode(struct kbd *k, uint8_t sc);

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
	struct kbd k = {0};

	/* kbd_init must clear pre-existing state. */
	k.shift = 3; k.ctrl = 1; k.caps = 1; k.e0 = 1;
	kbd_init(&k);
	check(k.shift == 0 && k.ctrl == 0 && k.caps == 0 && k.e0 == 0,
	      "test_init_zeroes_state");

	/* Plain make -> character; its break -> nothing. */
	kbd_init(&k);
	check(kbd_decode(&k, 0x1E) == 'a', "test_plain_letter_make");
	check(kbd_decode(&k, 0x9E) == KEY_NONE, "test_letter_break_is_silent");

	/* Shift is a state change, not a character. */
	kbd_init(&k);
	check(kbd_decode(&k, 0x2A) == KEY_NONE, "test_shift_alone_is_silent");
	check(kbd_decode(&k, 0x1E) == 'A', "test_shift_uppercases");
	kbd_decode(&k, 0xAA);			/* left shift break */
	check(kbd_decode(&k, 0x1E) == 'a', "test_shift_release_restores");

	/* Both shifts held, one released: still shifted. */
	kbd_init(&k);
	kbd_decode(&k, 0x2A);			/* left shift make */
	kbd_decode(&k, 0x36);			/* right shift make */
	kbd_decode(&k, 0xB6);			/* right shift break */
	check(kbd_decode(&k, 0x1E) == 'A', "test_two_shifts");

	/* A stray release must not push the count below zero. */
	kbd_init(&k);
	kbd_decode(&k, 0xAA);			/* break with none held */
	kbd_decode(&k, 0x2A);			/* now press once */
	check(kbd_decode(&k, 0x1E) == 'A',
	      "test_stray_shift_release_floors_at_zero");

	/* Caps lock: toggles on make only; XORs with shift; letters only. */
	kbd_init(&k);
	kbd_decode(&k, 0x3A);			/* caps make: toggle on */
	kbd_decode(&k, 0xBA);			/* caps break: no effect */
	check(kbd_decode(&k, 0x1E) == 'A', "test_caps_uppercases");
	kbd_decode(&k, 0x2A);			/* shift make */
	check(kbd_decode(&k, 0x1E) == 'a', "test_caps_shift_lowercases");
	kbd_decode(&k, 0xAA);			/* shift break */
	check(kbd_decode(&k, 0x02) == '1', "test_caps_leaves_digits");

	/* Shift on a digit row key picks the symbol. */
	kbd_init(&k);
	kbd_decode(&k, 0x2A);
	check(kbd_decode(&k, 0x02) == '!', "test_shift_digit_symbol");

	/* Ctrl+letter -> control code; release restores the letter. */
	kbd_init(&k);
	kbd_decode(&k, 0x1D);			/* ctrl make */
	check(kbd_decode(&k, 0x2E) == 3, "test_ctrl_c_is_etx");
	kbd_decode(&k, 0x9D);			/* ctrl break */
	check(kbd_decode(&k, 0x2E) == 'c', "test_ctrl_release_clears");

	/* Extended (0xE0-prefixed) arrow makes. */
	kbd_init(&k);
	kbd_decode(&k, 0xE0);
	int up = kbd_decode(&k, 0x48);
	kbd_decode(&k, 0xE0);
	int down = kbd_decode(&k, 0x50);
	kbd_decode(&k, 0xE0);
	int left = kbd_decode(&k, 0x4B);
	kbd_decode(&k, 0xE0);
	int right = kbd_decode(&k, 0x4D);
	check(up == KEY_UP && down == KEY_DOWN &&
	      left == KEY_LEFT && right == KEY_RIGHT,
	      "test_extended_arrows");

	/* Extended break: silent, and the machine is clean afterwards. */
	kbd_init(&k);
	kbd_decode(&k, 0xE0);
	int brk = kbd_decode(&k, 0xC8);		/* E0 C8: up arrow break */
	int after = kbd_decode(&k, 0x1E);
	check(brk == KEY_NONE && after == 'a',
	      "test_extended_break_leaves_state_clean");

	/* A stray 0xE0 swallows exactly one byte, then decoding resumes. */
	kbd_init(&k);
	kbd_decode(&k, 0xE0);
	int eaten = kbd_decode(&k, 0x1E);	/* unknown E0 partner */
	int resumed = kbd_decode(&k, 0x1E);	/* plain 'a' make again */
	check(eaten == KEY_NONE && resumed == 'a',
	      "test_unknown_e0_partner_is_swallowed");

	/* Enter and backspace map to their control characters. */
	kbd_init(&k);
	check(kbd_decode(&k, 0x1C) == '\n', "test_enter_is_newline");
	check(kbd_decode(&k, 0x0E) == '\b', "test_backspace");

	/* A make code with no table entry decodes to nothing. */
	kbd_init(&k);
	check(kbd_decode(&k, 0x5B) == KEY_NONE, "test_unknown_scancode");

	return failed;
}
```

# Lesson: The TTY and the Line Discipline {#tty-line-discipline}

Open a terminal, run `cat` with no arguments, and type `hellp`. Now press
backspace and fix it. The `p` vanishes from the screen, you type `o`,
press Enter, and `cat` dutifully prints `hello` back.

Here is the question this lesson turns on: **who erased the `p`?**

Not `cat`. At the moment you pressed backspace, `cat` was parked inside a
`read()` system call and had not yet received a single byte of your
typing. Not the shell either — the shell called `wait()` when it launched
`cat` and is fast asleep until `cat` exits. The only software awake and
watching your keystrokes was the **kernel**. The kernel saw the backspace,
removed the `p` from a buffer *inside kernel memory*, and sent the byte
sequence that wiped it off your screen. There is a line editor living in
the kernel, and you have used it every day without knowing its name: the
**line discipline**.

This is the deepest strangeness in Unix's terminal handling, and it has a
thoroughly practical justification. If the kernel didn't do line editing,
every program that reads from a terminal — every shell, every REPL, every
`cat`, every hastily written script that prompts "continue? y/n" — would
have to implement backspace itself, and they would all do it slightly
differently, and half of them would forget. Instead, Unix put one line
editor in one place, below every process, so that a program can call
`read()` on file descriptor 0 and receive *finished lines*, with all the
typos already edited out. The program never knows the backspace happened.

## Why it's called a "tty"

The name is a fossil. `tty` is short for **teletypewriter** — a physical
machine, most famously the Teletype Corporation **Model 33 ASR** (1963),
which is what "the terminal" actually was when Unix was written. The
Model 33 was an electromechanical printer with a keyboard: no screen, no
cursor, no memory. It hammered characters onto a roll of paper at ten per
second, in uppercase, loudly. Ken Thompson and Dennis Ritchie wrote the
first Unix on a PDP-7 whose console was exactly this machine, and the
kernel abstraction for "a thing a human types at" has been called a tty
ever since — `/dev/tty`, `getty`, `stty`, the `TTY` column in `ps`.

Two properties of that hardware still shape terminal behavior today:

**Echo is the computer's job.** A Model 33 in full-duplex mode did *not*
print what you typed. The keyboard sent each character down the wire to
the computer, and the printer printed only what came back up the wire. If
the computer didn't send your `h` back, you typed blind. So the convention
became: the receiving system **echoes** input back to the terminal. That
is still true. When you type into a shell today, the glyphs you see were
not drawn by your keyboard or your terminal emulator acting alone — they
were echoed by the line discipline, character by character. (Proof you
can try: run `stty -echo` and type. The keys work; nothing appears. `stty
echo` restores sanity.) Echo being software-controlled is also why
password prompts can turn it off.

**You cannot un-print ink.** On paper, erasing is physically impossible,
so early Unix didn't even try: in Seventh Edition Unix the default erase
character was `#` and the line-kill character was `@`, and "correcting" a
typo just meant printing the erase character and remembering. A session
transcript from 1979 really does look like `cat fiel##le`. Only when
glass terminals (VT100s and friends) replaced paper did visually erasing
a character become possible — and the line discipline grew the echo
choreography we'll implement below.

## The layer cake

In *The Keyboard* we built the bottom of the input stack: the 8042
controller hands us scancodes, and the keyboard driver translates them
into ASCII. The line discipline is the next layer up, and the boundary
between the layers is worth drawing precisely:

```
   key press
       │
       ▼
   8042 controller ──► scancode 0x23            (lesson: The Keyboard)
       │
       ▼
   keyboard driver ──► ASCII 'h'                (lesson: The Keyboard)
       │
       ▼
   LINE DISCIPLINE                              (this lesson)
     · appends 'h' to the EDIT buffer
     · echoes 'h' back to the screen
     · on erase/kill: rewrites the edit buffer, echoes the fix-up
     · on '\n' or ^D: commits the line to the COOKED queue
       │
       ▼
   cooked queue ──► read() ──► user process
```

The keyboard driver knows about hardware and knows nothing about lines.
The user process knows about lines and knows nothing about keys. The line
discipline is the adapter in between — and in a real kernel it is
pluggable. Classic Unix let you *swap* the line discipline on a serial
port at runtime, and that hook is how early networking snuck in: SLIP and
later PPP ran IP over a phone line by replacing the terminal line
discipline with one that spoke packets instead of lines. Same wire, same
driver, different discipline. DuckOS will keep exactly one discipline,
the canonical one, but the seam is the same. And per this course's usual
disclaimer: in a real kernel these buffers live in the tty driver and are
filled from the keyboard interrupt handler; here we build the discipline
as a plain struct whose "input" is a function call, so the tests can
feed it keys and read its buffers directly.

## Cooked and raw

Everything above describes **canonical mode**, affectionately called
**cooked mode**: the kernel cooks your keystrokes into lines before
serving them. But consider `vi`. An editor must react to every keystroke
*immediately* — `x` deletes a character now, not after you press Enter —
and it wants backspace delivered as a byte, not interpreted, because the
editor has its own opinions about what backspace means. So the tty also
offers **raw mode**: no editing, no line buffering, usually no echo;
every byte goes straight from the driver to the process, and the program
takes over all the work the line discipline was doing.

The control surface for all of this is **termios** (and its command-line
face, `stty`): a struct of flags and special characters that a process
can get and set per-terminal. `ICANON` switches canonical mode on and
off; `ECHO` controls echo; `VERASE`, `VKILL`, and `VEOF` let you rebind
which bytes mean erase, kill, and end-of-file (`stty erase '^H'` is the
ancient incantation from when backspace keys disagreed about sending
`0x08` versus DEL `0x7F` — modern terminals send DEL, which is why our
`CH_ERASE` below is `0x7F`). Run `stty -a` in any terminal and you are
looking at the knobs of your kernel's line discipline. That one
paragraph is all the termios we need: DuckOS implements canonical mode
only, with the bindings fixed.

## Canonical mode, precisely

Here is the contract, stated carefully, because every clause of it is
load-bearing:

1. Input accumulates in an **edit buffer** that is *invisible to
   readers*. While a line is being typed, `read()` behaves as if no
   input exists at all.
2. Only a committing character makes data readable: `'\n'` commits the
   line *including* the newline; end-of-file (`^D`) commits it without
   one (details below).
3. Until the commit, the past is rewritable: **erase** (backspace)
   removes the last character, **kill** (`^U`) removes the whole line.
   The reader will never know. This is why erasing works identically in
   every program — the program isn't involved.
4. Once committed, bytes move to the **cooked queue** and become
   ordinary readable data. Commitment is one-way: you cannot backspace
   over a line you already entered.

The canonical-mode data path — nothing crosses from the amber edit buffer to the green cooked queue except a commit, which is why read() blocks mid-line:

```d2
direction: right

kbd: "keyboard\ndriver" {
  shape: oval
}
edit: "edit buffer\n(readers can't\nsee it)" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}
cooked: "cooked\nqueue" {
  style.stroke: "#16a34a"
  style.stroke-width: 3
}
proc: "process" {
  shape: oval
}

kbd -> edit: append
edit -> edit: "erase/kill"
edit -> cooked: "'\\n' or ^D" {
  style.stroke-width: 3
}
cooked -> proc: "tty_read()"
proc -> edit: "-EAGAIN" {
  style.stroke-dash: 4
  style.stroke: "#dc2626"
}
```

Clause 1 explains a behavior that surprises nearly everyone the first
time and is a top-tier interview question besides: this program

```c
char c;
read(0, &c, 1);	/* ask the terminal for ONE byte */
```

does **not** return when you press a key. You asked for one byte; the
kernel has bytes in the edit buffer; and still `read()` blocks — until
you press Enter, because until the commit those bytes *do not exist* as
far as readers are concerned. The moment you press Enter, `read()`
returns exactly 1, delivering the first byte of the line, and the rest
of the line waits in the cooked queue for the next `read()`. Which
reveals clause 4's fine print: the cooked queue is a **byte stream, not
a message queue**. Lines are a property of *when data becomes
available*, not of how it is parceled out — a small buffer gets a line
in pieces, a large buffer might get two lines in one gulp.

## The erase choreography

When you erase a character, two separate things must be undone: the byte
in the edit buffer, and the glyph on the screen. The buffer part is
trivial — decrement a length. The screen part is a small dance, because
terminals have no "delete the last glyph" command. The line discipline
echoes exactly three bytes:

```
"\b \b"
 │  │ │
 │  │ └─ backspace: move the cursor left again
 │  └─── space: overwrite the doomed glyph with a blank
 └────── backspace: move the cursor left, onto the glyph
```

`'\b'` (`0x08`) only *moves the cursor*; it is non-destructive, a fossil
of the print head shuffling left on paper. Echo `"\b"` alone and the `p`
in `hellp` would still be visible with the cursor sitting on it — type
`o` and you'd see `hello`, but erase at the end of a line and the
stale glyph would just sit there. So: step back, stamp a space over the
body, step back again so the next character lands in the right place.

One rule with the force of law: **never echo an erase for a character
that was never echoed.** If the edit buffer is empty and the user leans
on backspace, the line discipline must do nothing — not "\b \b" per
keypress. Each "\b \b" wipes one screen cell, and with no characters of
yours left to wipe, it would start eating cells that belong to someone
else: the shell prompt, or the previous program's output. The display
is a shared ledger; erase only what you wrote. (Line kill, `^U`, is
just erase in a loop — one "\b \b" per character actually removed, and
therefore exactly zero if the line is already empty.)

## ^D, the most misunderstood key in Unix

^D's two personalities — everything hinges on whether the edit line is empty at that instant:

```d2
direction: right

d: "^D arrives" {
  shape: oval
}

q: "edit line\nempty?" {
  shape: diamond
}

eof: "set eof flag\nnext read() returns 0\n= end-of-file" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

commit: "commit line as-is\nno '\\n' appended, no echo\nread() gets the bytes" {
  style.stroke: "#16a34a"
  style.stroke-width: 3
}

d -> q
q -> eof: "yes"
q -> commit: "no"
```

Ask a room of programmers what `^D` does and most will say "it sends
EOF." Every word of that is wrong in an instructive way. `^D` is ASCII
`0x04`, EOT, *End of Transmission* — and the line discipline never
delivers it to anyone. It is an instruction **to the line discipline**,
and it means: *commit the edit buffer right now, exactly as it stands*.
Everything people observe about `^D` falls out of that one sentence plus
one convention. Two cases:

**`^D` mid-line** (edit buffer non-empty): the partial line is committed
to the cooked queue as-is — **no newline appended**, and nothing echoed.
A blocked `read()` wakes up and receives the partial line. That's it.
Not EOF. The program just gets data that doesn't end in `'\n'`.

**`^D` at the start of a line** (edit buffer empty): committing zero
bytes means a blocked `read()` returns... 0. And a zero return from
`read()` is, by convention as old as Unix, the definition of
**end-of-file**. That is the punchline: *EOF is not a character*. There
is no EOF byte in the stream (contrast CP/M and DOS, where `^Z` = `0x1A`
was literally stored in text files as a terminator — Unix files just
end). EOF is `read()` saying "zero bytes, and not because you should
retry." `cat` exits on `^D` for exactly the same reason it exits at the
end of a disk file: its `read()` returned 0.

Watch both cases in one transcript. You run `cat`; your keystrokes are
marked; everything else is echo or `cat`'s output:

```
$ cat
duck⏎                you type "duck", Enter — the line discipline echoes
                     it and commits "duck\n" to the cooked queue
duck                 cat's read() returns 5; cat writes "duck\n" back
quack^D              you type "quack", then ^D: nothing is echoed for
                     the ^D, but "quack" (5 bytes, no newline) commits
quackquack^D         cat's read() returns 5; cat writes "quack" — with
                     no newline, it lands on YOUR line, right after your
                     typing; you press ^D again
quackquack$          the edit buffer is empty now, so this ^D commits
                     zero bytes: read() returns 0, cat sees EOF and
                     exits; the shell prompt lands on the same messy line
```

That doubled `quackquack` is the mid-line `^D` made visible, and the
transcript answers the classic riddle of why ending input mid-line takes
`^D` *twice*: the first `^D` flushes the partial line (read returns 5 —
data, not EOF), which leaves the edit buffer empty, so the second `^D`
is an at-line-start `^D` and produces the 0-byte read. After Enter, one
`^D` suffices, because Enter already left the buffer empty.

One more subtlety our implementation will honor: EOF is **sticky**
relative to the cooked queue, not destructive of it. If two lines are
already committed and then `^D` arrives at line start, readers drain the
queued lines first and get the 0 afterward — end of file comes after the
file.

## Challenge: Canonical Mode {#tty-canon points=25}

Build the DuckOS line discipline: a canonical-mode tty as a plain struct.
Keystrokes arrive one at a time via `tty_input()` (in the real kernel,
called from the keyboard interrupt path we built in *The Keyboard*);
user processes drain committed bytes via `tty_read()`. The struct keeps
three buffers — the edit line, the cooked queue, and a transcript of
everything echoed, so the tests can check the screen choreography too.

Implement three functions:

**`void tty_init(struct tty *t)`** — empty tty: all four lengths and the
`eof` flag zero.

**`void tty_input(struct tty *t, char c)`** — process one input byte:

- `'\b'` or `CH_ERASE` (DEL, `0x7F`): if the edit line is non-empty,
  drop its last char and echo exactly `"\b \b"`. If empty: do nothing,
  echo nothing.
- `CH_KILL` (`^U`): erase the whole edit line — echo one `"\b \b"` per
  character removed, then set `len` to 0.
- `CH_EOF` (`^D`): if the edit line is empty, set `eof = 1` (sticky).
  Otherwise commit the partial line to the cooked queue **without**
  adding `'\n'`. Echo nothing in either case.
- `'\n'`: echo it; append it to the edit line if there is room (a full
  line drops the newline like any other char); commit the line to the
  cooked queue; reset `len` to 0.
- Printable bytes (`0x20`–`0x7E`): if the line is full (`len ==
  TTY_BUF`), drop the byte — a real tty beeps; we suffer in silence.
  Otherwise append it to the edit line and echo it.
- Anything else: ignore entirely.

"Commit" means `memcpy` the edit line onto the end of `cooked` at
`cooked_len` and add its length — assume it fits; the tests stay small.

**`int tty_read(struct tty *t, char *buf, int n)`** — if cooked bytes
exist: copy out `min(n, cooked_len)` of them, shift the remainder down
to the front of `cooked`, and return the count. Bytes, not messages —
a small `n` returns part of a line and leaves the rest queued. If no
cooked bytes exist: return 0 if `eof` is set (that 0 *is* EOF), else
`-EAGAIN`. A real kernel would instead block the caller — mark it
`PROC_RECEIVING`-style and wake it from the interrupt handler, the same
rendezvous machinery as *Message Passing — the Microkernel Heart* and
*Scheduling* — but our
hosted tests have no scheduler to yield to, so we report "would block"
as an errno.

The starter provides working echo helpers (`echo_c`, `echo_s`) that
append to the transcript and silently drop past `TTY_BUF`; route all
echo through them. The tests: verify typed chars are unreadable before
Enter (`-EAGAIN`); check exact committed bytes for Enter, erase, kill,
and both `^D` cases (including that a mid-line `^D` commit has **no**
newline and that `^D^D` mid-line yields data then EOF); drain two queued
lines in order and in pieces; confirm erase echoes exactly `"\b \b"`
(and nothing on an empty line); cap an overlong line at `TTY_BUF` and
still commit it; and diff the full echo transcript of an editing
session against `"ab\b \bc\n"`.

### Starter

```c
#include <string.h>	/* memcpy, memmove — for commit and tty_read */

#define TTY_BUF 256
#define EAGAIN 11

#define CH_ERASE 0x7F	/* DEL — what modern terminals send for backspace */
#define CH_KILL  0x15	/* ^U */
#define CH_EOF   0x04	/* ^D */

struct tty {
	char line[TTY_BUF];	/* edit buffer: current uncommitted line */
	int  len;
	char cooked[TTY_BUF];	/* committed bytes readable by tty_read */
	int  cooked_len;
	int  eof;		/* ^D at line start seen; sticky */
	char echo[TTY_BUF];	/* transcript of everything echoed */
	int  echo_len;
};

/* Append one byte to the echo transcript; silently drop when full. */
static void echo_c(struct tty *t, char c)
{
	if (t->echo_len < TTY_BUF)
		t->echo[t->echo_len++] = c;
}

/* Append a string to the echo transcript. */
static void echo_s(struct tty *t, const char *s)
{
	while (*s)
		echo_c(t, *s++);
}

/* Reset t to an empty tty: no edit line, no cooked bytes, no echo, no EOF. */
void tty_init(struct tty *t)
{
	/* TODO: zero len, cooked_len, echo_len, eof */
	(void)t;
}

/*
 * Canonical-mode processing of one input byte (see the prompt's table):
 * erase/kill edit the line, '\n' and ^D commit it, printables append,
 * everything else is ignored. Echo only through echo_c/echo_s.
 */
void tty_input(struct tty *t, char c)
{
	/* TODO: implement the canonical-mode rules */
	(void)t;
	(void)c;
	(void)echo_c;	/* references so -Wall stays quiet until you use them */
	(void)echo_s;
}

/*
 * Copy up to n committed bytes into buf, shifting the remainder down.
 * Returns the byte count; 0 for EOF (only when no cooked bytes remain
 * and eof is set); -EAGAIN when there is nothing to read yet.
 */
int tty_read(struct tty *t, char *buf, int n)
{
	/* TODO */
	(void)t;
	(void)buf;
	(void)n;
	return -EAGAIN;
}
```

### Tests

```c
#include <stdio.h>
#include <string.h>

#define TTY_BUF 256
#define EAGAIN 11

#define CH_ERASE 0x7F
#define CH_KILL  0x15
#define CH_EOF   0x04

struct tty {
	char line[TTY_BUF];
	int  len;
	char cooked[TTY_BUF];
	int  cooked_len;
	int  eof;
	char echo[TTY_BUF];
	int  echo_len;
};

void tty_init(struct tty *t);
void tty_input(struct tty *t, char c);
int tty_read(struct tty *t, char *buf, int n);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Fresh tty, struct poisoned first so tty_init has to do its job. */
static void mk(struct tty *t)
{
	memset(t, 0x5a, sizeof *t);
	tty_init(t);
}

static void feed(struct tty *t, const char *s)
{
	while (*s)
		tty_input(t, *s++);
}

/* True iff the echo transcript is exactly the n bytes at want. */
static int echo_is(const struct tty *t, const char *want, int n)
{
	return t->echo_len == n && memcmp(t->echo, want, (size_t)n) == 0;
}

int main(void)
{
	struct tty t;
	char buf[512];
	int r, i;

	/* Clause 1: typed bytes are invisible to readers before Enter. */
	mk(&t);
	feed(&t, "hi");
	check(tty_read(&t, buf, 16) == -EAGAIN, "test_no_read_before_enter");

	/* Enter commits; the reader gets the line INCLUDING '\n'. */
	mk(&t);
	feed(&t, "hi\n");
	r = tty_read(&t, buf, 16);
	check(r == 3 && memcmp(buf, "hi\n", 3) == 0,
	      "test_enter_commits_line");

	/* Two lines queue up; a big read drains both, in order. */
	mk(&t);
	feed(&t, "ab\ncd\n");
	r = tty_read(&t, buf, 16);
	check(r == 6 && memcmp(buf, "ab\ncd\n", 6) == 0,
	      "test_two_lines_queue_in_order");

	/* Bytes, not messages: a small n leaves the remainder queued. */
	mk(&t);
	feed(&t, "hello\n");
	r = tty_read(&t, buf, 2);
	check(r == 2 && memcmp(buf, "he", 2) == 0,
	      "test_partial_read_first_bytes");
	r = tty_read(&t, buf, 16);
	check(r == 4 && memcmp(buf, "llo\n", 4) == 0,
	      "test_partial_read_remainder");

	/* Erase removes the last uncommitted char. */
	mk(&t);
	feed(&t, "ab");
	tty_input(&t, CH_ERASE);
	feed(&t, "c\n");
	r = tty_read(&t, buf, 16);
	check(r == 3 && memcmp(buf, "ac\n", 3) == 0,
	      "test_erase_removes_char");

	/* Erase on an empty line: no state change, and NOTHING echoed. */
	mk(&t);
	tty_input(&t, CH_ERASE);
	check(t.len == 0 && t.echo_len == 0,
	      "test_erase_empty_line_noop");

	/* ^U wipes the whole uncommitted line. */
	mk(&t);
	feed(&t, "abc");
	tty_input(&t, CH_KILL);
	feed(&t, "xy\n");
	r = tty_read(&t, buf, 16);
	check(r == 3 && memcmp(buf, "xy\n", 3) == 0,
	      "test_kill_wipes_line");

	/* Erase echoes exactly backspace, space, backspace. */
	mk(&t);
	tty_input(&t, 'a');
	tty_input(&t, CH_ERASE);
	check(echo_is(&t, "a\b \b", 4), "test_erase_echo_is_bs_sp_bs");

	/* Kill echoes one "\b \b" per erased character. */
	mk(&t);
	feed(&t, "abc");
	tty_input(&t, CH_KILL);
	check(echo_is(&t, "abc\b \b\b \b\b \b", 12),
	      "test_kill_echo_per_char");

	/* ^D at line start: read() returns 0 — EOF — and it is sticky. */
	mk(&t);
	tty_input(&t, CH_EOF);
	check(tty_read(&t, buf, 16) == 0, "test_eof_at_start_reads_zero");
	check(tty_read(&t, buf, 16) == 0, "test_eof_is_sticky");

	/* ^D mid-line commits the partial line: NO newline, NO echo. */
	mk(&t);
	feed(&t, "abc");
	tty_input(&t, CH_EOF);
	r = tty_read(&t, buf, 16);
	check(r == 3 && memcmp(buf, "abc", 3) == 0 && echo_is(&t, "abc", 3),
	      "test_eof_midline_commits_no_newline");

	/* ^D ^D mid-line: first flushes the partial line, second is EOF. */
	mk(&t);
	feed(&t, "abc");
	tty_input(&t, CH_EOF);
	tty_input(&t, CH_EOF);
	r = tty_read(&t, buf, 16);
	check(r == 3 && memcmp(buf, "abc", 3) == 0,
	      "test_double_eof_flushes_first");
	check(tty_read(&t, buf, 16) == 0, "test_double_eof_then_eof");

	/* Chars past TTY_BUF are dropped; the capped line still commits. */
	mk(&t);
	for (i = 0; i < 300; i++)
		tty_input(&t, 'x');
	check(t.len == TTY_BUF, "test_overlong_line_caps_len");
	tty_input(&t, '\n');
	r = tty_read(&t, buf, 512);
	{
		int all_x = (r == TTY_BUF);

		for (i = 0; i < r && i < 512; i++)
			if (buf[i] != 'x')
				all_x = 0;
		check(all_x, "test_overlong_line_still_commits");
	}

	/* The whole screen story for "ab<erase>c<Enter>". */
	mk(&t);
	feed(&t, "ab");
	tty_input(&t, CH_ERASE);
	feed(&t, "c\n");
	check(echo_is(&t, "ab\b \bc\n", 7), "test_echo_transcript");

	return failed;
}
```

# Lesson: Block Devices and the Buffer Cache {#buffer-cache}

Everything DuckOS has touched so far answered at electronic speed. The
VGA buffer from *A Screen to Print On* is memory. The page tables from
*Paging and Virtual Memory* are memory. Even the keyboard, slow as human
fingers are, hands us its byte the instant we ask the 8042 for it. The
next three lessons build a filesystem, and a filesystem lives on the one
device in a 1991 machine that is *not* electronic: a spinning platter of
rust with a mechanical arm hovering over it. Before we can afford to
talk to that device, we need the single most important performance
structure in a classic Unix kernel: the buffer cache.

## Two kinds of device

Unix sorts devices into two families, and the split survives to this day
in `ls -l /dev` as the leading `c` or `b`.

A **character device** is a stream: bytes arrive or depart in order, and
you cannot seek. The keyboard from *The Keyboard* is the archetype — you
take scancodes as they happen; there is no asking for "the 40th
keystroke of next Tuesday." The TTY sits on top of it, still a stream.

A **block device** is an array: a disk presents itself as blocks
numbered `0..N-1`, and you may read or write any of them, in any order,
as many times as you like. The catch is the unit. You cannot ask a disk
for one byte. The hardware reads and writes whole **sectors** (512 bytes
on every drive of the era) because each sector flies under the head as
one continuous analog signal, framed by address marks and protected by
its own error-correcting code. The smallest possible transaction is one
sector; anything finer is your problem, in software.

Minix FS — and therefore DuckOS — groups two sectors into one 1024-byte
**block** and does all filesystem I/O in that unit:

```
#define BLOCK_SIZE 1024      /* Minix v1: two 512-byte sectors */
```

So the driver interface for a block device is exactly two operations —
read block *n* into memory, write memory to block *n* — and everything
above it must think in whole blocks. To read one directory entry (16
bytes, as we'll see in *Directories and Path Walking*), you read the
entire 1024-byte block that contains it.

## The arithmetic of an eternity

Why is caching disk blocks *the* classic performance win, rather than
one optimization among many? Because of how grotesque the speed gap is.
To read a random block, a 1991 drive must physically move: swing the
head arm to the right track (a **seek**), then wait for the platter to
rotate the right sector under the head. For a decent consumer drive of
the day, seek plus rotational delay averaged around 15 milliseconds.

Fifteen milliseconds sounds like nothing. Put it next to the CPU:

```
average seek + rotation (1991 drive):     ~15 ms  =  0.015 s
386DX-33 clock:                           33,000,000 cycles/second

cycles spent waiting for ONE block:       0.015 x 33,000,000
                                        = 495,000 cycles
```

At roughly one cycle per simple instruction, that is about **500,000
instructions** the CPU could have executed in the time it took the arm
to find one block — and even charging the 386 its real-world several
cycles per instruction, it is still well over a hundred thousand. In
human terms: if a memory access were one second, a disk seek would be
better than five days. A kernel that goes to the disk every time the
filesystem wants a block is a kernel that spends its life standing
still.

The saving grace is **temporal locality**: filesystem traffic is wildly
repetitive. The superblock, the inode and zone bitmaps, the inode table
blocks, the blocks of `/` and `/bin` — the same few dozen blocks are
touched over and over, by every path lookup, every allocation, every
`ls`. Keep recently used blocks in RAM and most "disk reads" never reach
the disk. That RAM is the **buffer cache**: a fixed array of block-sized
buffers, each holding a copy of one disk block, plus the bookkeeping to
find them, share them, and throw the right one out when space runs low.

## The contract: one block, one buffer

Why uniqueness matters — two processes pin the same buffer, so both updates land in one copy and one write-back carries both:

```d2
direction: right

a: "process A\nsets size" {
  shape: oval
  style.stroke: "#d97706"
}
b: "process B\nsets nlinks" {
  shape: oval
  style.stroke: "#d97706"
}

buf: "buffer[2]" {
  shape: sql_table
  blockno: "7"
  refcnt: "2 (two pins)"
  data: "both updates"
}

d7: "disk block 7"

a -> buf: pin
b -> buf: pin
buf -> d7: "one write-back\ncarries both" {style.stroke-dash: 4}
```

Classic Unix names the two ends of the contract `bread` and `brelse`
("buffer read", "buffer release") — the names appear in V6, live on in
xv6, and every kernel since has some equivalent pair. DuckOS calls them
`bc_get` and `bc_release`:

Where the cache sits — the thick edge is the hot path that costs no I/O; only misses, dirty evictions, and sync ever reach the disk (its counters are how the tests catch cheating):

```d2
direction: down

fs: "filesystem code"

cache: "buffer cache (RAM)" {
  b0: "buf 0"
  b1: "buf 1"
  b2: "…"
  b7: "buf 7"
}

disk: "struct disk" {
  shape: sql_table
  blocks: "64 x 1024 bytes"
  nreads: "++ per disk_read"
  nwrites: "++ per disk_write"
}

fs -> cache: "bc_get / bc_release\n(hits stop here: no I/O)" {
  style.stroke-width: 3
}
cache -> disk: "disk_read\n(miss only)"
cache -> disk: "disk_write\n(dirty evict, sync)"
```

- `bc_get(cache, disk, blockno)` returns a buffer holding the current
  contents of block `blockno`, reading from the disk only if no buffer
  already holds it.
- `bc_release(cache, buf)` says "I'm done with this buffer for now."

The load-bearing word in `bc_get`'s contract is **the**: it returns
*the* unique in-memory copy of that block. The cache must never, under
any circumstances, hold two buffers for the same block number. This is
the cache's one sacred invariant, and it is worth seeing exactly what
breaks without it.

Suppose block 7 holds part of the inode table, and the invariant is
violated — two buffers both claim block 7. Process A is extending a file
whose inode lives there; process B is linking a second name to another
inode in the same block:

```
      buffer[2]                     buffer[5]      <-- BUG: same block
  +------------------+          +------------------+
  | copy of block 7  |          | copy of block 7  |
  | A: size = 2048   |          | B: nlinks = 2    |
  +--------+---------+          +--------+---------+
           |  flushed first              |  flushed second: WINS
           v                             v
           +--------> disk block 7 <-----+

  result on disk: nlinks = 2, but size is the OLD value.
  A's update existed only in buffer[2] and is silently gone.
```

Each buffer started from the same disk contents, each process patched
its own copy, and whichever copy is written back last overwrites the
other's change wholesale. Nobody gets an error. The file's size quietly
reverts; the damage surfaces days later as a truncated file or an fsck
complaint about blocks owned by no one. This is the classic *lost
update*, and a buffer cache with duplicate buffers manufactures it out
of correct code.

Uniqueness dissolves the problem before it starts. If every taker of
block 7 receives a pointer to the *same* buffer, then A's write is
instantly visible to B — they are literally the same bytes at the same
address. Cache coherence by construction: there is nothing to keep
coherent, because there is only one copy.

### Reference counts are pins, not locks

If several processes can hold the same buffer, the cache must know when
a buffer is in use. Each buffer carries a **reference count**: `bc_get`
increments it, `bc_release` decrements it. While `refcnt > 0` the buffer
is **pinned** — the cache must not recycle it for another block, because
somebody is holding a pointer into its data.

Releasing is *not* destroying. When the count drops to zero, the buffer
keeps its contents and its block number; it merely becomes a
**candidate** for eviction. Think of a library book: returning it puts
it back on the shelf — it isn't shredded — and if you ask for it again
before someone else needs the shelf space, it's handed straight back to
you with no trip to the warehouse. A re-`get` of a released block that
hasn't been evicted yet is a hit, and hits are the entire point.

(A real multiprocessor kernel also needs a per-buffer *lock* so two
CPUs don't mutate the data simultaneously — xv6's `bread` returns the
buffer locked. DuckOS is single-threaded through the cache, as Minix
effectively was, so the reference count alone carries the contract.)

## Write-back, and the price of lying

When someone modifies a buffer, when does the change reach the disk?
Two schools:

- **Write-through**: every modification is written to disk immediately.
  Simple, safe, and slow — every logical write costs a 15 ms physical
  one.
- **Write-back**: mark the buffer **dirty** and keep going. The block is
  written out later — when the buffer is about to be evicted and reused
  for another block, or when someone explicitly asks for a flush.

DuckOS is write-back: `bc_mark_dirty` sets a flag, and the actual
`disk_write` happens at eviction time or when `bc_sync` walks the cache
and flushes every dirty buffer. The win is enormous because writes
cluster even harder than reads. Append to a file a hundred times and you
update the same inode block a hundred times; a write-through cache pays
for a hundred seeks, a write-back cache pays for one, having *absorbed*
the other ninety-nine in RAM. Bitmap blocks and directory blocks behave
the same way.

The price is that the disk is now, deliberately, out of date — the cache
is lying to the disk about the state of the world, and the lie is only
safe if the kernel eventually gets to flush. Cut the power first and
every dirty buffer evaporates: data written by programs that were told
"success" is simply gone, and worse, *metadata* can be half-gone — a
directory entry flushed but not the inode it points to. This is why
`sync` exists as a system call and a command; why classic Unix ran an
`update` daemon that called `sync` every 30 seconds as a damage limiter;
why shutdown is a ritual and not just flipping the switch. Any 1991
Minix user without a UPS knew the liturgy that followed a power cut:
reboot, and sit through `fsck` doing penance over the filesystem,
reconciling bitmaps against reality and gathering orphaned blocks into
`lost+found`. (Making crash recovery cheap — journaling, copy-on-write —
is a 1990s-and-later story; DuckOS, true to its year, just flushes and
hopes.)

## Choosing the victim: LRU

The cache is a fixed array — Minix sized it with `NR_BUFS`, DuckOS with
`NBUF` — so a miss on a full cache must evict somebody. Pinned buffers
are untouchable, so the choice is among buffers with `refcnt == 0`. But
*which* one?

Evict the wrong buffer and you pay for it 15 ms at a time: throw out the
inode bitmap and the very next file creation takes a seek it didn't need
to. The policy that classic kernels settled on is **least recently
used** — evict the candidate whose last use is furthest in the past. The
argument is temporal locality read forwards: the recent past is the best
cheap predictor of the near future, so the block untouched the longest
is the block least likely to be missed. LRU needs no foresight, no
profile of the workload, just an ordering by last use — and for the
metadata-heavy traffic a filesystem generates, it is very hard to beat.

It does have one famous failure mode. Sequentially scan a file larger
than the cache — a big `grep`, a backup — and every block of it is used
exactly once, never again. Each freshly read scan block is by definition
the *most* recently used, so LRU dutifully keeps it and evicts your
whole hot set — superblock, bitmaps, hot directories — to make room for
bytes that will never be touched twice. The scan ends, and the system
limps through the aftermath re-reading everything that matters, one seek
at a time. Real systems bolt scan resistance onto LRU for exactly this
reason — Linux's page cache keeps separate active/inactive lists and
demands a second touch before promotion; PostgreSQL routes bulk scans
through a small ring buffer so they can't flood shared memory; 2Q and
segmented LRU are the textbook variants. DuckOS keeps plain LRU and, in
the honest 1991 spirit, simply owns the weakness.

## Minix's cache, and ours

This design is lifted, with pride, from Minix. The Minix filesystem
declares `struct buf` and the `NR_BUFS`-sized buffer array in
`fs/buf.h`, and the cache logic lives beside it in the FS server (its
`get_block`/`put_block` are our `bc_get`/`bc_release`). Minix keeps the
buffers threaded on a doubly linked **LRU chain**: the front of the
chain is the least recently used buffer, the rear the most; `put_block`
hangs a released buffer on the rear, and a miss evicts from the front.
A separate hash table finds a block's buffer without scanning. That
buys O(1) lookup and O(1) LRU maintenance — which matters when the cache
is big (Minix defaulted to a few dozen buffers; a modern page cache is
most of your RAM).

DuckOS trades that machinery for clarity. Instead of a chain, each
buffer carries a `lastuse` **timestamp** stamped from a monotonic
counter that ticks on every `bc_get`; eviction is "scan all `NBUF`
buffers, take the unpinned one with the smallest stamp." With `NBUF` of
8, the linear scan costs nothing and the code stays transparent — the
same LRU order the chain encodes structurally, we just compute on
demand. In a real kernel this array would front a real disk controller
and the "timestamp" pressure would push you back toward the chain; here
the disk is a struct in memory that the tests can inspect, which is
exactly what makes the cache's behavior checkable.

Two details of the challenge's victim rule deserve a word before you
meet the code. First, an **invalid** buffer (one that has never held a
block) is always preferred over evicting real data — eviction has a
cost, an empty slot doesn't. Second, if every buffer is pinned, `bc_get`
must fail and return NULL rather than corrupt something; a real kernel
would sleep until a buffer frees up, but failing loudly is the honest
single-threaded translation. And the flush-before-reuse ordering is not
negotiable: if the victim is dirty, its old contents must reach the disk
*before* the buffer is overwritten with the new block — reuse first and
the dirty data has no home to be saved from; it is simply lost.

bc_get end to end — the red box is the flush-before-reuse step; skip it and the victim's last writes silently die:

```d2
direction: right

hit: "holds\nblockno?" {shape: diamond}
rethit: "hit: pin, stamp\nNO disk I/O"
victim: "victim: invalid buf,\nelse oldest unpinned\n(none → NULL)"
dirty: "victim\ndirty?" {shape: diamond}
flush: "write OLD\nblock FIRST" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
load: "read new:\nvalid, pinned"

hit -> rethit: yes
hit -> victim: no
victim -> dirty
dirty -> flush: yes
flush -> load
dirty -> load: no
```

## Challenge: The Buffer Cache {#buf-cache points=25}

Build the DuckOS buffer cache: `NBUF` buffers fronting a 64-block toy
disk, with reference-count pinning, timestamp LRU eviction, and dirty
write-back. The disk driver (`disk_read`/`disk_write`) is provided and
counts every real I/O — the tests use those counters to tell your hits
from your misses.

Implement five functions against the structs in the starter:

- `void bc_init(struct bcache *c)` — put the cache in its empty state:
  every buffer invalid, clean, unpinned, and the clock at 0.
- `struct buf *bc_get(struct bcache *c, struct disk *d, uint32_t
  blockno)` — return the unique buffer for `blockno`:
  - `blockno >= NDISK`: return NULL (and touch nothing).
  - **Hit** (some buffer is valid with a matching `blockno`): increment
    its `refcnt`, stamp `lastuse = ++c->clock`, return it. No disk I/O.
  - **Miss**: choose a victim — any *invalid* buffer first; otherwise
    the valid buffer with `refcnt == 0` and the smallest `lastuse`. If
    every buffer is pinned, return NULL. If the victim is valid and
    dirty, flush it with `disk_write` *before* reuse. Then `disk_read`
    the new block into it and set `valid = 1`, `dirty = 0`,
    `refcnt = 1`, `blockno`, `lastuse = ++c->clock`.
- `void bc_release(struct bcache *c, struct buf *b)` — decrement
  `refcnt`, never below zero. The buffer stays valid: a later `bc_get`
  of the same block must still hit.
- `void bc_mark_dirty(struct buf *b)` — mark the buffer's data newer
  than the disk's.
- `int bc_sync(struct bcache *c, struct disk *d)` — `disk_write` every
  valid dirty buffer, clear each one's dirty flag, and return how many
  were flushed (so a second immediate sync returns 0).

The tests plant bytes directly in `disk.blocks`, watch `nreads` and
`nwrites`, and check among other things: that a first get costs exactly
one read and pins the buffer; that re-gets hit (same pointer — the
uniqueness rule); that a fully pinned cache refuses service and that
releasing exactly one buffer makes it the next victim; that LRU spares
a recently re-touched block and evicts the stalest one; that dirty
eviction writes back (and clean eviction doesn't); that data survives
a full evict-and-reload round trip; and that `bc_sync` flushes each
dirty buffer exactly once.

### Starter

```c
#include <stdint.h>
#include <string.h>

#define NBUF 8			/* buffers in the cache */
#define BLOCK_SIZE 1024		/* Minix v1: two 512-byte sectors */
#define NDISK 64		/* blocks on our toy disk */

/*
 * The "hardware". In a real kernel a read means commanding a disk
 * controller and sleeping ~15ms until its interrupt; here the disk is
 * an array in memory that the tests can inspect, and nreads/nwrites
 * count the real I/Os your cache performs.
 */
struct disk {
	uint8_t blocks[NDISK][BLOCK_SIZE];
	int nreads;
	int nwrites;
};

struct buf {
	int valid;		/* 1 if this buffer holds a copy of block `blockno` */
	int dirty;		/* 1 if data is newer than the disk's copy */
	int refcnt;		/* pins; >0 means in use, never evict */
	uint32_t blockno;	/* which disk block this buffer caches */
	uint32_t lastuse;	/* stamp from cache->clock at each get */
	uint8_t data[BLOCK_SIZE];
};

struct bcache {
	struct buf bufs[NBUF];
	uint32_t clock;		/* monotonic, ++ on every bc_get */
};

/* ---- Provided driver: each call below is one real disk I/O. ---- */

void disk_read(struct disk *d, uint32_t blockno, uint8_t *dst) {
	memcpy(dst, d->blocks[blockno], BLOCK_SIZE);
	d->nreads++;
}

void disk_write(struct disk *d, uint32_t blockno, const uint8_t *src) {
	memcpy(d->blocks[blockno], src, BLOCK_SIZE);
	d->nwrites++;
}

/* ---- Your cache. ---- */

/* Empty cache: all buffers invalid/clean/unpinned, clock = 0. */
void bc_init(struct bcache *c) {
	/* TODO: zero every buffer's flags and the clock */
	(void)c;
}

/*
 * Return THE buffer for `blockno`, pinned (refcnt bumped) and stamped
 * (lastuse = ++c->clock).
 *
 *   - blockno >= NDISK: return NULL.
 *   - Hit (valid buffer with matching blockno): no disk I/O.
 *   - Miss: victim = any invalid buffer, else the valid refcnt==0
 *     buffer with the smallest lastuse; all pinned -> NULL.
 *     Flush the victim first if it is valid && dirty, THEN disk_read
 *     the new block and set valid=1, dirty=0, refcnt=1, blockno.
 */
struct buf *bc_get(struct bcache *c, struct disk *d, uint32_t blockno) {
	/* TODO */
	(void)c;
	(void)d;
	(void)blockno;
	return NULL;
}

/* Drop one pin (refcnt floors at 0). Data stays cached and valid. */
void bc_release(struct bcache *c, struct buf *b) {
	/* TODO */
	(void)c;
	(void)b;
}

/* The caller modified b->data; remember that the disk is now stale. */
void bc_mark_dirty(struct buf *b) {
	/* TODO */
	(void)b;
}

/*
 * Flush every valid dirty buffer to disk and clear its dirty flag.
 * Returns the number of buffers flushed.
 */
int bc_sync(struct bcache *c, struct disk *d) {
	/* TODO */
	(void)c;
	(void)d;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define NBUF 8
#define BLOCK_SIZE 1024
#define NDISK 64

struct disk {
	uint8_t blocks[NDISK][BLOCK_SIZE];
	int nreads;
	int nwrites;
};

struct buf {
	int valid;
	int dirty;
	int refcnt;
	uint32_t blockno;
	uint32_t lastuse;
	uint8_t data[BLOCK_SIZE];
};

struct bcache {
	struct buf bufs[NBUF];
	uint32_t clock;
};

void disk_read(struct disk *d, uint32_t blockno, uint8_t *dst);
void disk_write(struct disk *d, uint32_t blockno, const uint8_t *src);
void bc_init(struct bcache *c);
struct buf *bc_get(struct bcache *c, struct disk *d, uint32_t blockno);
void bc_release(struct bcache *c, struct buf *b);
void bc_mark_dirty(struct buf *b);
int bc_sync(struct bcache *c, struct disk *d);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static struct bcache cache;
static struct disk dk;

/* Every test starts from a blank disk and a freshly initialized cache. */
static void fresh(void) {
	memset(&dk, 0, sizeof(dk));
	memset(&cache, 0, sizeof(cache));
	bc_init(&cache);
}

static void test_first_get(void) {
	fresh();
	memset(dk.blocks[3], 0xAB, BLOCK_SIZE);
	struct buf *b = bc_get(&cache, &dk, 3);
	check(b != NULL && dk.nreads == 1, "test_first_get_reads_disk_once");
	check(b != NULL && b->refcnt == 1 && b->valid && b->blockno == 3,
	      "test_first_get_pins_buffer");
	check(b != NULL && b->data[0] == 0xAB &&
	      b->data[BLOCK_SIZE - 1] == 0xAB,
	      "test_get_returns_disk_data");
}

static void test_hits(void) {
	fresh();
	struct buf *a = bc_get(&cache, &dk, 3);
	struct buf *b = bc_get(&cache, &dk, 3);
	check(b != NULL && dk.nreads == 1, "test_reget_is_a_hit");
	check(b != NULL && b->refcnt == 2, "test_reget_bumps_refcnt");
	check(a != NULL && a == b, "test_same_block_same_buffer");
	bc_release(&cache, b);
	bc_release(&cache, a);
	struct buf *again = bc_get(&cache, &dk, 3);
	check(again != NULL && again == a && dk.nreads == 1,
	      "test_release_then_get_still_hits");
}

static void test_pinning(void) {
	fresh();
	struct buf *pinned[NBUF];
	for (uint32_t i = 0; i < NBUF; i++)
		pinned[i] = bc_get(&cache, &dk, i);
	check(bc_get(&cache, &dk, 8) == NULL, "test_all_pinned_get_fails");
	bc_release(&cache, pinned[2]);
	struct buf *b = bc_get(&cache, &dk, 8);
	check(b != NULL && b->blockno == 8, "test_release_one_get_succeeds");
	check(b != NULL && b == pinned[2],
	      "test_eviction_reuses_released_buffer");
}

static void test_lru(void) {
	fresh();
	for (uint32_t i = 0; i < NBUF; i++)
		bc_release(&cache, bc_get(&cache, &dk, i));
	/* Touch block 0 again: block 1 becomes the least recently used. */
	bc_release(&cache, bc_get(&cache, &dk, 0));
	int reads_before = dk.nreads;
	struct buf *b9 = bc_get(&cache, &dk, 9);
	check(b9 != NULL && dk.nreads == reads_before + 1,
	      "test_miss_after_fill_reads_disk");
	bc_release(&cache, b9);
	struct buf *b0 = bc_get(&cache, &dk, 0);
	check(b0 != NULL && dk.nreads == reads_before + 1,
	      "test_lru_spares_recently_touched");
	bc_release(&cache, b0);
	struct buf *b1 = bc_get(&cache, &dk, 1);
	check(b1 != NULL && dk.nreads == reads_before + 2,
	      "test_lru_evicts_least_recent");
}

static void test_dirty_writeback(void) {
	fresh();
	struct buf *b = bc_get(&cache, &dk, 0);
	if (b != NULL) {
		memset(b->data, 0x5A, BLOCK_SIZE);
		bc_mark_dirty(b);
	}
	bc_release(&cache, b);
	/* Pin 7 other blocks so block 0's buffer is the only candidate,
	 * then force an eviction with an 8th. */
	for (uint32_t i = 1; i <= 7; i++)
		bc_get(&cache, &dk, i);
	struct buf *b8 = bc_get(&cache, &dk, 8);
	check(b8 != NULL && dk.nwrites == 1,
	      "test_dirty_eviction_writes_back");
	check(dk.blocks[0][0] == 0x5A && dk.blocks[0][BLOCK_SIZE - 1] == 0x5A,
	      "test_writeback_data_reaches_disk");
	bc_release(&cache, b8);
	struct buf *back = bc_get(&cache, &dk, 0);
	check(back != NULL && back->data[123] == 0x5A,
	      "test_reget_after_eviction_reads_written_data");
}

static void test_clean_eviction(void) {
	fresh();
	bc_release(&cache, bc_get(&cache, &dk, 0));
	for (uint32_t i = 1; i <= 7; i++)
		bc_get(&cache, &dk, i);
	struct buf *b8 = bc_get(&cache, &dk, 8);
	check(b8 != NULL && dk.nwrites == 0,
	      "test_clean_eviction_does_not_write");
}

static void test_sync(void) {
	fresh();
	struct buf *a = bc_get(&cache, &dk, 10);
	struct buf *b = bc_get(&cache, &dk, 11);
	struct buf *c = bc_get(&cache, &dk, 12);
	if (a != NULL) {
		memset(a->data, 0x11, BLOCK_SIZE);
		bc_mark_dirty(a);
	}
	if (c != NULL) {
		memset(c->data, 0x33, BLOCK_SIZE);
		bc_mark_dirty(c);
	}
	bc_release(&cache, a);
	bc_release(&cache, b);
	bc_release(&cache, c);
	int n = bc_sync(&cache, &dk);
	check(n == 2 && dk.nwrites == 2, "test_sync_flushes_dirty_count");
	check(dk.blocks[10][7] == 0x11 && dk.blocks[12][7] == 0x33 &&
	      dk.blocks[11][7] == 0x00,
	      "test_sync_writes_dirty_blocks_only");
	check(bc_sync(&cache, &dk) == 0 && dk.nwrites == 2,
	      "test_second_sync_flushes_nothing");
}

static void test_data_integrity(void) {
	fresh();
	static uint8_t pattern[BLOCK_SIZE];
	for (int i = 0; i < BLOCK_SIZE; i++)
		pattern[i] = (uint8_t)(i * 7 + 3);
	struct buf *b = bc_get(&cache, &dk, 42);
	if (b != NULL) {
		memcpy(b->data, pattern, BLOCK_SIZE);
		bc_mark_dirty(b);
	}
	bc_release(&cache, b);
	/* Cycle 8 other blocks through to force block 42 out. */
	for (uint32_t i = 0; i < NBUF; i++)
		bc_release(&cache, bc_get(&cache, &dk, i));
	struct buf *back = bc_get(&cache, &dk, 42);
	check(back != NULL && memcmp(back->data, pattern, BLOCK_SIZE) == 0,
	      "test_evict_reget_bytes_match");
}

static void test_bounds(void) {
	fresh();
	check(bc_get(&cache, &dk, NDISK) == NULL,
	      "test_blockno_out_of_range_null");
	check(bc_get(&cache, &dk, 4096) == NULL && dk.nreads == 0,
	      "test_out_of_range_touches_no_disk");
}

int main(void) {
	test_first_get();
	test_hits();
	test_pinning();
	test_lru();
	test_dirty_writeback();
	test_clean_eviction();
	test_sync();
	test_data_integrity();
	test_bounds();
	return failed;
}
```

# Lesson: A Filesystem on a Disk {#fs-on-disk}

In *Block Devices and the Buffer Cache* we flattened the disk into the
cleanest abstraction in the kernel: an array of 1024-byte blocks, numbered
0 to N-1, with `read` and `write` by block number. That is genuinely all a
disk is. It has no files, no directories, no names, no types — just
numbered buckets of bytes. Everything you have ever called "a file" is a
fiction that software layers on top of that array.

A filesystem is that fiction made precise: a data structure serialized
onto the block array. But it is a data structure under a constraint that
no in-memory structure ever faces. When the power goes out, every pointer
in RAM evaporates. The machine that boots tomorrow remembers *nothing* —
no root pointer, no handle, no context. It gets one gift: the disk still
holds its bytes, and the blocks are still numbered the same way.

So the first rule of on-disk data structures is: **there is nothing to
follow until you have somewhere to stand.** Every structure must live at a
block number that is either *known* — fixed forever by the format
specification, the same on every disk — or *computed* — derived by
arithmetic from numbers you already read. A linked list whose head lives
"wherever malloc put it" is unfindable. A table whose start block is
written down in a header at a known block is findable forever. That single
constraint explains the entire layout you are about to see: a fixed
anchor block, a handful of counts inside it, and everything else at an
offset you can compute from those counts with grade-school arithmetic.

## The Minix v1 filesystem

DuckOS speaks the **Minix version 1 filesystem**, and the choice is not
sentimental — it is the direct ancestor of the Linux filesystem family.
Tanenbaum designed it in 1987 to be simple enough to teach from, and when
Linus Torvalds wrote Linux in 1991 he implemented Minix FS compatibility
first, for the bluntest possible reason: his hard disk was already
formatted with it, and he wanted his new kernel to mount his own files.
Linux 0.01 shipped with Minix FS as *the* filesystem. Its limits — 64MB
per filesystem, 14-character file names — chafed almost immediately, and
"ext" (1992) exists precisely to escape them. Learn Minix v1 and you can
read ext2's design as a list of answers to complaints.

Here is the entire on-disk layout, in order, from block 0:

```
+--------------+-------------+---------------+--------------+-----------+-------------+
| block 0      | block 1     | inode bitmap  | zone bitmap  | inode     | data zones  |
| boot block   | superblock  | imap_blocks   | zmap_blocks  | table     | ...to end   |
| (FS ignores) |             | blocks        | blocks       |           | of disk     |
+--------------+-------------+---------------+--------------+-----------+-------------+
  KNOWN          KNOWN         KNOWN (starts   COMPUTED       COMPUTED     COMPUTED
                               at block 2)
```

- **Block 0, the boot block.** Reserved for the boot sector we parsed back
  in *The Machine Wakes Up*. The filesystem never reads or writes it —
  the format simply promises not to touch it, so the BIOS and the FS can
  share one disk without a treaty negotiation.
- **Block 1, the superblock.** The anchor. Its location is fixed by the
  spec — *known* — and every other region's location is *computed* from
  the counts stored inside it.
- **The inode bitmap**, starting at block 2 (known: right after the
  superblock), running for `imap_blocks` blocks. One bit per inode:
  allocated or free.
- **The zone bitmap**, immediately after: `zmap_blocks` blocks, one bit
  per data zone.
- **The inode table**: the array of all inodes — the per-file metadata
  records that *Inodes: Files Without Names* dissects. Minix v1 inodes
  are 32 bytes each, packed 32 to a block.
- **Data zones**: everything else, to the end of the disk. File contents
  and directory contents live here.

Watch the known/computed rule pay off with a worked example. Take an 8MB
disk: 8192 blocks of 1024 bytes. Suppose `mkfs` chose 2688 inodes (about
one per three blocks — a classic ratio, betting that the average file is
a few KB). The superblock stores five counts; everything else is
arithmetic:

```
ninodes       = 2688
nzones        = 8192
imap_blocks   = 1        (2688+1 bits fit in one 8192-bit block)
zmap_blocks   = 1        (8192 bits = exactly one block)
firstdatazone = 88

inode bitmap start = 2                        (fixed by the format)
zone bitmap start  = 2 + imap_blocks     = 3
inode table start  = 2 + imap + zmap     = 4
inode table size   = ceil(2688 * 32 / 1024) = 84 blocks
first data zone    = 4 + 84              = 88 (matches firstdatazone!)

block 0        boot block
block 1        superblock
block 2        inode bitmap
block 3        zone bitmap
blocks 4..87   inode table (2688 inodes x 32 bytes)
blocks 88..8191  data zones
```

The same arithmetic as a dependency picture — each arrow means "is an
input to," and the amber-bordered value is the one that is also stored
redundantly in the superblock:

```d2
direction: right

sb: "superblock (block 1)" {
  shape: sql_table
  imap_blocks: "= 1"
  zmap_blocks: "= 1"
  ninodes: "= 2688"
}

imap: "imap start\n2 (fixed)"
zmap: "zmap start\n2 + 1 = 3"
itab: "itable start\n3 + 1 = 4"
itlen: "itable blocks\nceil(2688·32 / 1024) = 84"
fdz: "first data zone\n4 + 84 = 88" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}

imap -> zmap
zmap -> itab
itab -> fdz
sb.imap_blocks -> zmap
sb.zmap_blocks -> itab
sb.ninodes -> itlen
itlen -> fdz
```

Note that `firstdatazone` is *both* stored and recomputable — the
superblock writes it down even though you could derive it. Redundancy
like that is deliberate in filesystem design: it gives a consistency
checker (`fsck`) something to cross-examine. If the stored number and the
computed number disagree, the disk is lying about something.

## The superblock: the root of trust

Everything hangs off block 1, so let's read it byte by byte. The Minix v1
superblock is an 18-byte record at offset 0 of the block (the rest of the
block is unused):

```
offset  size  field          meaning
     0     2  ninodes        inodes are numbered 1..ninodes
     2     2  nzones         total zones on the device, metadata included
     4     2  imap_blocks    blocks of inode bitmap
     6     2  zmap_blocks    blocks of zone bitmap
     8     2  firstdatazone  first zone that holds file/dir data
    10     2  log_zone_size  log2(blocks per zone) — 0 for us
    12     4  max_size       largest file size the format can represent
    16     2  magic          0x137F
```

Two of these deserve suspicion before trust.

**The magic number is a seatbelt.** `0x137F` says "I am a Minix v1
filesystem" — and nothing else on earth is supposed to say it. Why does
mount refuse without it? Because every other field is just plausible-
looking integers. Hand the kernel a disk formatted as ext2 — or a disk of
pure static — and offsets 0 through 11 will still *decode* to some
ninodes, some bitmap sizes. If the kernel believed them, it would compute
bitmap locations inside somebody's file data, "allocate" by flipping bits
in the middle of their thesis, and scribble inodes over their photos. The
magic check converts "catastrophic misinterpretation" into "mount:
unknown filesystem type" — a two-byte tripwire. (Each variant got its own
magic: Minix v2 is `0x2468`; the 30-character-filename flavor of v1 is
`0x138F`. Same seatbelt, different cars.)

**Zones are not quite blocks.** Minix allocates file data in *zones*: a
zone is `1 << log_zone_size` consecutive blocks. The idea was to let big
disks allocate in larger clusters (fewer bitmap bits, better contiguity)
without changing the format. In practice virtually every Minix v1 disk
ever formatted used `log_zone_size = 0` — one block per zone — and the
zone machinery just adds a layer of terminology. DuckOS assumes 0 and
says so out loud: our parser *rejects* any other value rather than
half-supporting it. From here on, when we say "zone," you may read
"block," and `nzones` is the size of the whole device in blocks.

`max_size` is worth decoding once, because it foreshadows *Inodes:
Files Without Names*. A
Minix v1 inode points at 7 direct zones, 1 indirect zone (a block holding
512 zone numbers), and 1 double-indirect zone (512 x 512). So the biggest
possible file is:

```
(7 + 512 + 512*512) zones * 1024 bytes = 268,966,912 bytes = 0x10081C00
```

That is the value mkfs writes into `max_size`. Notice the *filesystem*
limit is different and smaller: `nzones` is a 16-bit field, so a device
can have at most 65,535 zones — 64MB. That u16 is the wall ext was built
to tear down; no file on a Minix v1 disk ever got near 256MB.

## The bitmaps: an allocation ledger

Creating a file needs a free inode; writing to it needs free zones. "Is
block 4711 in use?" must be answerable without walking every file on the
disk, so Minix keeps the answer precomputed: one bit per inode in the
inode bitmap, one bit per zone in the zone bitmap. Bit set = in use, bit
clear = free. Allocation is "find the lowest clear bit, set it";
freeing is "clear the bit." The bitmaps are the ledger, and like any
ledger they can disagree with reality after a crash — which is exactly
what `fsck` re-derives them from.

Now the detail this whole lesson has been building toward. Dump the
freshly-formatted bitmaps and you find something odd: **bit 0 of each
bitmap is already set** — pre-marked "in use" by mkfs, forever, for an
inode and a zone that do not exist.

The reason is a design decision that echoes through the next two lessons.
Minix made the number 0 mean "nothing," everywhere:

- A directory entry (as we'll see in *Directories and Path Walking*)
  is a 2-byte inode number plus a 14-byte name. Deleting a file just
  zeroes the inode number — inode 0 means **"empty slot."**
- A zone number of 0 inside an inode (as we'll see in *Inodes: Files
  Without Names*) means **"no zone here"** — a hole in a sparse file,
  reading as zeros without costing a block.

A number system where 0 means "nothing" cannot also hand out 0 as a valid
name. If the allocator ever returned inode 0, the file it named would be
indistinguishable from a deleted directory entry; if it returned zone 0,
that block of data would be indistinguishable from a hole. So inode and
zone numbering starts at 1, and bit 0 is pre-set as a guard so no scan of
the bitmap can ever allocate it. One pre-burned bit buys an unambiguous
null for the entire filesystem. Cheap at twice the price — and when your
directory-scanning code in a later lesson checks `inode != 0`, this bit
is why that check is sound.

The whole convention in one picture — the red-bordered box is the
pre-burned guard bit, and everything downstream leans on it:

```d2
direction: right

guard: "bit 0 of each bitmap\npre-set by mkfs" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
never: "allocator can never\nhand out the number 0"
dirent: "dirent with inode 0\n= empty slot"
hole: "zone 0 in an inode\n= hole (reads as zeros)"

guard -> never: "so"
never -> dirent: "0 is free to mean"
never -> hole: "0 is free to mean"
```

## Bytes on disk are not structs in memory

One last discipline before we write code. The superblock on disk is a
sequence of little-endian integers — `ninodes` is stored low byte first,
because Minix was built on the 8086 lineage and x86 is little-endian. The
tempting way to "parse" it is one line:

```c
struct superblock *sb = (struct superblock *)block;   /* NO. */
```

Cast the buffer pointer to a struct pointer, done. Early kernels —
Minix and Linux included — did exactly this, and it worked, because the
code only ever ran on the machine the format was designed on. It is
still how *not* to write filesystem code, for three reasons — the three
sins of struct-punning:

- **Alignment.** The compiler assumes a `struct superblock *` is aligned
  for its members. A buffer has no such obligation — read the superblock
  into `buf + 1` and a 4-byte load of `max_size` lands on an odd address.
  x86 shrugs; ARM and SPARC deliver a bus error; C calls it undefined
  behavior either way.
- **Endianness.** On a big-endian machine the same cast decodes
  `ninodes = 2688` (0x0A80, stored as `80 0A`) as 0x800A = 32778. No
  crash, no warning — just a kernel that computes every layout offset
  wrong and walks off the disk.
- **Padding.** The compiler may insert invisible bytes between members to
  align them. Our particular struct happens to pack cleanly on most ABIs
  — which is the most dangerous case, because the cast *works today* and
  breaks the day someone adds a field or changes a type.

So we parse explicitly, byte by byte, with arithmetic that is correct on
every machine that can run C:

```c
uint16_t v = (uint16_t)(b[0] | (b[1] << 8));          /* little-endian */
uint32_t w = (uint32_t)b[0] | ((uint32_t)b[1] << 8)
           | ((uint32_t)b[2] << 16) | ((uint32_t)b[3] << 24);
```

`b[0] | (b[1] << 8)` reads "low byte, plus high byte shifted into place."
It compiles to a single load on a little-endian machine anyway — you pay
nothing for the portability. This is the same explicitness we practiced
on the MBR in *The Machine Wakes Up*, now applied to a structure the
kernel will *trust* — which is precisely why it must be decoded, then
validated, and never merely reinterpreted.

In a real kernel, the superblock's bytes arrive in a buffer handed over
by the buffer cache from the last lesson; here the tests build the 1024
bytes in an array and hand them to you — same bytes, no spinning rust
required.

## Challenge: Reading the Superblock {#superblock-parse points=15}

Mount, step one: you are given the raw 1024 bytes of disk block 1, and
you must produce a validated `struct superblock` plus the layout numbers
everything else will be computed from.

Implement `sb_parse` and the four layout helpers in the starter.

`sb_parse(block, out)` decodes the fields at these byte offsets (all
little-endian; `max_size` is the one 32-bit field):

```
ninodes@0  nzones@2  imap_blocks@4  zmap_blocks@6
firstdatazone@8  log_zone_size@10  max_size@12 (u32)  magic@16
```

Decode with explicit byte arithmetic (`b[0] | (b[1] << 8)`) — do not cast
the buffer to a struct pointer. Then validate, returning `-EINVAL` if any
check fails and 0 on success:

- `magic` must be `MINIX_MAGIC` (0x137F).
- `log_zone_size` must be 0 (we don't speak multi-block zones).
- `imap_blocks` and `zmap_blocks` must both be nonzero (a filesystem
  with no allocation ledger is not a filesystem).
- `firstdatazone` must not start inside the metadata: it is invalid if
  it is less than `2 + imap_blocks + zmap_blocks`, and invalid if it is
  `>= nzones` (data can't start past the end of the device). (A fussier
  check would also add the inode table's blocks to the lower bound —
  real fsck does — but the superblock-stated minimum is what we enforce
  here.)

The layout helpers compute the map we drew above, from a parsed
superblock:

- `sb_imap_start` — always 2 (boot block, superblock, then the bitmap).
- `sb_zmap_start` — `2 + imap_blocks`.
- `sb_inode_table_start` — `2 + imap_blocks + zmap_blocks`.
- `sb_inode_table_blocks` — inodes are 32 bytes each in v1, so
  `ninodes * 32` bytes rounded *up* to whole blocks.

The tests build a valid superblock byte-by-byte (the 8MB example from the
lesson: 2688 inodes, 8192 zones, 1+1 bitmap blocks, first data zone 88,
`max_size` 0x10081C00) and check every decoded field; they then corrupt
it one field at a time — magic 0x137E, nonzero `log_zone_size`,
`firstdatazone` of 3 (inside the bitmaps) and 8192 (== nzones) — and
expect `-EINVAL` for each. The layout helpers are checked against a
second, hand-computed example (96 inodes, 1 imap block, 3 zmap blocks),
including the round-up case where `ninodes * 32` is not a multiple of
1024. `max_size` is planted with four distinct bytes, so a parser that
reads only 16 bits — or assembles the bytes in the wrong order — cannot
pass by luck.

### Starter

```c
#include <stdint.h>

#define MINIX_MAGIC 0x137F
#define BLOCK_SIZE 1024
#define EINVAL 22

struct superblock {
	uint16_t ninodes;	/* inodes 1..ninodes */
	uint16_t nzones;	/* total zones incl. metadata region */
	uint16_t imap_blocks;	/* blocks of inode bitmap */
	uint16_t zmap_blocks;	/* blocks of zone bitmap */
	uint16_t firstdatazone;
	uint16_t log_zone_size;	/* must be 0 for us */
	uint32_t max_size;
	uint16_t magic;
};

/*
 * Decode and validate the superblock from `block`, the raw BLOCK_SIZE
 * bytes of disk block 1. On-disk layout (little-endian):
 *   ninodes@0  nzones@2  imap_blocks@4  zmap_blocks@6
 *   firstdatazone@8  log_zone_size@10  max_size@12 (u32)  magic@16
 * Returns 0 and fills *out on success; -EINVAL if magic is wrong,
 * log_zone_size != 0, either bitmap size is 0, or firstdatazone is
 * < 2 + imap_blocks + zmap_blocks or >= nzones.
 */
int sb_parse(const uint8_t *block, struct superblock *out) {
	/* TODO: decode each field with b[0] | (b[1] << 8) arithmetic
	 * (four bytes for max_size), then run the validation checks. */
	(void)block;
	(void)out;
	return -EINVAL;
}

/* First block of the inode bitmap. Fixed by the format. */
uint32_t sb_imap_start(const struct superblock *sb) {
	/* TODO */
	(void)sb;
	return 0;
}

/* First block of the zone bitmap: right after the inode bitmap. */
uint32_t sb_zmap_start(const struct superblock *sb) {
	/* TODO */
	(void)sb;
	return 0;
}

/* First block of the inode table: right after both bitmaps. */
uint32_t sb_inode_table_start(const struct superblock *sb) {
	/* TODO */
	(void)sb;
	return 0;
}

/* Blocks the inode table occupies: ninodes 32-byte inodes, rounded
 * up to whole blocks. */
uint32_t sb_inode_table_blocks(const struct superblock *sb) {
	/* TODO */
	(void)sb;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define MINIX_MAGIC 0x137F
#define BLOCK_SIZE 1024
#define EINVAL 22

struct superblock {
	uint16_t ninodes;	/* inodes 1..ninodes */
	uint16_t nzones;	/* total zones incl. metadata region */
	uint16_t imap_blocks;	/* blocks of inode bitmap */
	uint16_t zmap_blocks;	/* blocks of zone bitmap */
	uint16_t firstdatazone;
	uint16_t log_zone_size;	/* must be 0 for us */
	uint32_t max_size;
	uint16_t magic;
};

int sb_parse(const uint8_t *block, struct superblock *out);
uint32_t sb_imap_start(const struct superblock *sb);
uint32_t sb_zmap_start(const struct superblock *sb);
uint32_t sb_inode_table_start(const struct superblock *sb);
uint32_t sb_inode_table_blocks(const struct superblock *sb);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Poke little-endian integers into the fake disk block, byte by byte. */
static void put16(uint8_t *b, uint32_t off, uint16_t v) {
	b[off] = (uint8_t)(v & 0xff);
	b[off + 1] = (uint8_t)(v >> 8);
}

static void put32(uint8_t *b, uint32_t off, uint32_t v) {
	b[off] = (uint8_t)(v & 0xff);
	b[off + 1] = (uint8_t)((v >> 8) & 0xff);
	b[off + 2] = (uint8_t)((v >> 16) & 0xff);
	b[off + 3] = (uint8_t)((v >> 24) & 0xff);
}

/* The 8MB worked example from the lesson. */
static void mk_valid(uint8_t *block) {
	memset(block, 0, BLOCK_SIZE);
	put16(block, 0, 2688);		/* ninodes */
	put16(block, 2, 8192);		/* nzones */
	put16(block, 4, 1);		/* imap_blocks */
	put16(block, 6, 1);		/* zmap_blocks */
	put16(block, 8, 88);		/* firstdatazone */
	put16(block, 10, 0);		/* log_zone_size */
	put32(block, 12, 0x10081C00u);	/* max_size: 4 distinct bytes */
	put16(block, 16, MINIX_MAGIC);
}

int main(void) {
	uint8_t block[BLOCK_SIZE];
	struct superblock sb = {0};

	mk_valid(block);
	check(sb_parse(block, &sb) == 0, "test_valid_parse_returns_zero");
	check(sb.ninodes == 2688, "test_field_ninodes");
	check(sb.nzones == 8192, "test_field_nzones");
	check(sb.imap_blocks == 1, "test_field_imap_blocks");
	check(sb.zmap_blocks == 1, "test_field_zmap_blocks");
	check(sb.firstdatazone == 88, "test_field_firstdatazone");
	check(sb.log_zone_size == 0, "test_field_log_zone_size");
	check(sb.magic == MINIX_MAGIC, "test_field_magic");
	/* u32 with four distinct bytes: a 16-bit read or a byte-order
	 * mixup cannot produce this value. */
	check(sb.max_size == 0x10081C00u, "test_field_max_size_u32");

	mk_valid(block);
	put16(block, 16, 0x137E);
	check(sb_parse(block, &sb) == -EINVAL, "test_bad_magic_einval");

	mk_valid(block);
	put16(block, 10, 1);
	check(sb_parse(block, &sb) == -EINVAL,
	      "test_nonzero_log_zone_size_einval");

	mk_valid(block);
	put16(block, 4, 0);
	check(sb_parse(block, &sb) == -EINVAL, "test_zero_imap_einval");

	mk_valid(block);
	put16(block, 6, 0);
	check(sb_parse(block, &sb) == -EINVAL, "test_zero_zmap_einval");

	/* Metadata region is blocks 0..3 here (imap 1 + zmap 1); a first
	 * data zone of 3 would start inside the zone bitmap. */
	mk_valid(block);
	put16(block, 8, 3);
	check(sb_parse(block, &sb) == -EINVAL,
	      "test_firstdatazone_in_metadata_einval");

	mk_valid(block);
	put16(block, 8, 8192);	/* == nzones: past the last zone (8191) */
	check(sb_parse(block, &sb) == -EINVAL,
	      "test_firstdatazone_past_end_einval");

	/* Layout helpers on a second hand-computed example. */
	{
		struct superblock ex = {0};
		ex.ninodes = 96;
		ex.nzones = 720;
		ex.imap_blocks = 1;
		ex.zmap_blocks = 3;
		ex.firstdatazone = 9;
		ex.magic = MINIX_MAGIC;
		check(sb_imap_start(&ex) == 2, "test_imap_start");
		check(sb_zmap_start(&ex) == 3, "test_zmap_start");
		check(sb_inode_table_start(&ex) == 6,
		      "test_inode_table_start");
		/* 96 inodes * 32 bytes = 3072 bytes = exactly 3 blocks */
		check(sb_inode_table_blocks(&ex) == 3,
		      "test_inode_table_blocks_exact");
		/* 100 * 32 = 3200 bytes: 3 blocks + 128 bytes -> 4 blocks */
		ex.ninodes = 100;
		check(sb_inode_table_blocks(&ex) == 4,
		      "test_inode_table_blocks_rounds_up");
	}

	return failed;
}
```

## Challenge: The Allocation Bitmaps {#fs-bitmap points=15}

Step two of mount: the blocks after the superblock hold the allocation
ledger. This challenge builds the bit-level operations that every
`create`, `write`, `unlink`, and `truncate` in the next lessons will
lean on.

The bitmap convention is LSB-first, matching Minix on x86: **bit n lives
in byte `n / 8`, at mask `1 << (n % 8)`**. So bits 0..7 are byte 0
(bit 0 is that byte's least significant bit), bits 8..15 are byte 1, and
setting bit 8 turns byte 1 from `0x00` to `0x01`. The map is just a
`uint8_t` array; the caller tells you how many bits are meaningful.

Here is a map with bit 0 (reserved, as mkfs left it) and bit 8 set —
note that bit numbers run right-to-left *within* each byte, because bit
n sits at the byte's `1 << (n % 8)` position:

```
        byte 0 = 0x01                 byte 1 = 0x01
bit:   7  6  5  4  3  2  1  0       15 14 13 12 11 10  9  8
     +--+--+--+--+--+--+--+--+     +--+--+--+--+--+--+--+--+
     | 0| 0| 0| 0| 0| 0| 0| 1|     | 0| 0| 0| 0| 0| 0| 0| 1|
     +--+--+--+--+--+--+--+--+     +--+--+--+--+--+--+--+--+
                              ^                            ^
              bit 0: reserved, never allocated       bit 8: byte 1's LSB
```

Remember the lesson's punchline: numbering starts at 1. `mkfs` presets
bit 0 of each bitmap at format time, and your allocator must *also*
refuse to hand out 0 on its own — even on a carelessly zeroed map where
bit 0 is clear, `bm_alloc` never returns 0. Defense in depth: the
pre-set bit protects well-formed disks, the `[1, nbits)` scan protects
against everything else.

Implement:

- `bm_isset(map, bit)` — 1 if the bit is set, 0 if clear.
- `bm_set(map, bit)` / `bm_clear(map, bit)` — flip one bit, disturb
  nothing else.
- `bm_alloc(map, nbits)` — find the lowest *clear* bit in `[1, nbits)`
  (bit 0 is reserved: never examine-and-return it), set it, and return
  its number. If every bit in that range is set, return `-ENOSPC` —
  the ledger's way of saying "disk full."
- `bm_count_free(map, nbits)` — how many clear bits in `[1, nbits)`;
  this is the number `df` would report, and it too excludes bit 0.

Lowest-first allocation matters: it means freed numbers are reused
promptly and allocation is deterministic — the tests rely on both.
`nbits` is the exact count of valid bits and need not be a multiple of
8: with `nbits = 10`, bit 9 is allocatable and bit 10 is not, even
though byte 1 has room for more.

The tests hand in zeroed maps and set bit 0 themselves (playing the role
of mkfs), then check: a fresh map allocates 1, then 2, then 3; clearing
bit 2 makes the next allocation return 2 (lowest-first); filling bits
1..7 makes the next allocation return 8 and flips exactly the low bit of
byte 1; a map of all `0xff` returns `-ENOSPC`; a fully *zeroed* map (bit
0 sloppily clear) still allocates 1, never 0, and `bm_count_free` still
excludes bit 0 from its count; and with `nbits = 10` allocation stops
after bit 9 with `-ENOSPC`.

### Starter

```c
#include <stdint.h>

#define ENOSPC 28

/*
 * Allocation bitmaps, Minix layout: bit n lives in byte n / 8 at mask
 * 1 << (n % 8) (LSB-first). Bit 0 of every on-disk bitmap is reserved
 * and preset by mkfs — inode and zone numbering starts at 1, because
 * 0 means "empty dirent slot" / "hole" elsewhere in the filesystem.
 */

/* 1 if `bit` is set in `map`, else 0. */
int bm_isset(const uint8_t *map, uint32_t bit) {
	/* TODO */
	(void)map;
	(void)bit;
	return 0;
}

/* Set `bit` in `map`, leaving every other bit untouched. */
void bm_set(uint8_t *map, uint32_t bit) {
	/* TODO */
	(void)map;
	(void)bit;
}

/* Clear `bit` in `map`, leaving every other bit untouched. */
void bm_clear(uint8_t *map, uint32_t bit) {
	/* TODO */
	(void)map;
	(void)bit;
}

/*
 * Allocate: find the lowest clear bit in [1, nbits), set it, return
 * its number. Never return 0, even if bit 0 is (wrongly) clear.
 * Return -ENOSPC if no bit in [1, nbits) is clear.
 */
int bm_alloc(uint8_t *map, uint32_t nbits) {
	/* TODO */
	(void)map;
	(void)nbits;
	return -ENOSPC;
}

/* Count of clear (free) bits in [1, nbits) — bit 0 never counts. */
uint32_t bm_count_free(const uint8_t *map, uint32_t nbits) {
	/* TODO */
	(void)map;
	(void)nbits;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define ENOSPC 28

int bm_isset(const uint8_t *map, uint32_t bit);
void bm_set(uint8_t *map, uint32_t bit);
void bm_clear(uint8_t *map, uint32_t bit);
int bm_alloc(uint8_t *map, uint32_t nbits);
uint32_t bm_count_free(const uint8_t *map, uint32_t nbits);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* A zeroed map with bit 0 preset — exactly what mkfs writes. */
static void fresh(uint8_t *map, uint32_t len) {
	memset(map, 0, len);
	bm_set(map, 0);
}

int main(void) {
	uint8_t map[8];	/* 64 bits */

	fresh(map, sizeof map);
	check(bm_isset(map, 0) == 1, "test_set_then_isset");

	check(bm_alloc(map, 64) == 1, "test_fresh_map_allocs_one");
	{
		int a = bm_alloc(map, 64);
		int b = bm_alloc(map, 64);
		check(a == 2 && b == 3, "test_sequential_allocs");
	}

	bm_clear(map, 2);
	check(bm_isset(map, 2) == 0, "test_clear_then_isset");
	check(bm_alloc(map, 64) == 2, "test_freed_bit_reused_lowest_first");

	/* set/clear touch only their own bit */
	fresh(map, sizeof map);
	bm_set(map, 5);
	check(bm_isset(map, 5) == 1 && bm_isset(map, 4) == 0 &&
	      bm_isset(map, 6) == 0, "test_set_disturbs_only_its_bit");
	bm_clear(map, 5);
	check(bm_isset(map, 5) == 0, "test_isset_clear_roundtrip");

	/* crossing a byte boundary: fill 1..7, next alloc is 8, which
	 * lives in byte 1 at mask 1 << 0 */
	fresh(map, sizeof map);
	{
		int i, ok = 1;
		for (i = 1; i <= 7; i++)
			if (bm_alloc(map, 64) != i)
				ok = 0;
		check(ok, "test_allocs_fill_first_byte");
	}
	check(bm_alloc(map, 64) == 8, "test_alloc_crosses_byte_boundary");
	check((map[1] & 1) == 1, "test_bit8_is_byte1_lsb");

	/* full map */
	memset(map, 0xff, sizeof map);
	check(bm_alloc(map, 64) == -ENOSPC, "test_full_map_enospc");

	/* bit 0 is never allocated, even when carelessly clear */
	memset(map, 0, sizeof map);	/* no fresh(): bit 0 left clear */
	check(bm_alloc(map, 64) == 1, "test_bit0_never_allocated");
	check(bm_isset(map, 0) == 0, "test_alloc_leaves_bit0_alone");
	memset(map, 0, sizeof map);
	check(bm_count_free(map, 64) == 63, "test_count_free_excludes_bit0");

	/* count_free on a partially filled map */
	fresh(map, sizeof map);
	bm_alloc(map, 64);
	bm_alloc(map, 64);
	bm_alloc(map, 64);
	check(bm_count_free(map, 64) == 60, "test_count_free_partial");

	/* nbits need not be a multiple of 8 */
	fresh(map, sizeof map);
	{
		int i, ok = 1;
		for (i = 1; i <= 9; i++)
			if (bm_alloc(map, 10) != i)
				ok = 0;
		check(ok, "test_nbits_ten_allocs_one_through_nine");
	}
	check(bm_alloc(map, 10) == -ENOSPC, "test_nbits_ten_then_enospc");

	return failed;
}
```

# Lesson: Inodes: Files Without Names {#inodes}

Here is the sharpest idea in the Unix filesystem, and it fits in one
sentence: **a file is not its name.**

Every other filesystem of the era welded the two together. In CP/M and
MS-DOS FAT, the directory entry *is* the file: one record holding the
name, the size, the attributes, and the number of the first cluster, all
in one place. A file has exactly one name, the name is structural, and
everything the OS knows about the file is filed under it. It feels
obvious. It is also a trap.

Unix — and Minix, faithfully — splits the record in two:

- The **inode** ("index node") is an anonymous, numbered record that
  holds *everything* about a file — its size, its mode (type and
  permission bits), owner, timestamps, and above all the list of disk
  zones holding its data. Everything, that is, except one thing: the
  inode does not know its own name.
- A **directory** is just a file — an ordinary file whose bytes happen
  to be a table mapping names to inode numbers, 16 bytes per entry in
  Minix v1. We'll parse those tables in *Directories and Path Walking*;
  for now the only fact that matters is that a name is a (string, inode
  number) pair stored in some *other* file's data zones.

That one decoupling buys three things FAT structurally cannot have.
**Hard links**: two directory entries in two different directories can
carry the same inode number, giving one file two equally-real names —
the inode's link count (`i_nlinks`) just says how many. **Safe
rename**: renaming a file rewrites a 16-byte directory entry and
touches neither the inode nor a single data zone, which is why `mv` of
a 2GB file within a filesystem is instant. And the classic Unix
temp-file idiom, **open-then-unlink**: a process opens a file, deletes
its only name, and keeps using it — the inode stays alive as long as
someone holds it open, and the data self-destructs the moment the last
descriptor closes, crash or no crash.

The price of the split is one level of indirection on every path
lookup, and one genuinely odd property: given an inode, there is no way
to ask "what is your name?" — the kernel would have to search every
directory on the disk (that's literally what `find / -inum N` does).
Unix decided the trade was worth it in 1969, and no serious filesystem
since has decided otherwise.

## The 32 bytes that describe a file

Minix v1 spends exactly 32 bytes of disk on each inode:

```
offset  size  field      meaning
------  ----  ---------  ------------------------------------------
     0     2  i_mode     file type + permission bits (u16)
     2     2  i_uid      owner (u16)
     4     4  i_size     file length in BYTES, not blocks (u32)
     8     4  i_mtime    modification time, Unix seconds (u32)
    12     1  i_gid      group (u8)
    13     1  i_nlinks   how many directory entries point here (u8)
    14    18  i_zone[9]  nine u16 zone numbers -- the interesting part
------  ----
    32 bytes total
```

Thirty-two bytes means exactly 32 inodes per 1KB block, and the inode
table — which you located via the superblock in *A Filesystem on a
Disk* — is simply an array of these, indexed by inode number. Numbering
starts at 1 (inode 1 is the root directory in Minix), so slot 0 of the
table is dead space, mirroring the permanently-set bit 0 in the inode
bitmap from that lesson. Turning an inode number into a disk address is
pure arithmetic — no search, no lookup structure:

```
block  = inode_table_start + (ino - 1) / 32
offset = ((ino - 1) % 32) * 32
```

In a real kernel this 32-byte struct IS the on-disk record — the driver
reads the block and casts. Here, as everywhere in DuckOS, we model the
disk as a plain buffer the tests can build and inspect.

Notice what the struct does *not* contain, besides a name: no pointer
to a parent directory (an inode can be in two directories at once, so
"the" parent doesn't exist), and no list of who has it open (that's
in-memory kernel state, not filesystem state). The on-disk inode is
purely: what kind of thing is this, who may touch it, how long is it,
and *where are its bytes*. The rest of this lesson is about that last
question.

## Seven, one, and one

Nine u16 zone slots have to describe the location of anything from an
empty file to the largest file the filesystem allows. Minix v1 splits
them into three tiers:

- `i_zone[0..6]` — seven **direct** slots. Each holds the zone number
  of a data zone outright. With `BLOCK_SIZE = 1024`, that covers the
  first **7 × 1024 = 7,168 bytes** of the file with zero extra disk
  reads.
- `i_zone[7]` — one **indirect** slot. It names a zone that contains no
  file data at all, only more zone numbers: 1024 bytes / 2 bytes per
  u16 = **512 entries**. Those cover file blocks 7 through 518, adding
  **512 × 1024 = 524,288 bytes** at the cost of one extra read.
- `i_zone[8]` — one **double-indirect** slot. It names a zone of 512
  entries, each of which names an *indirect* zone of 512 entries, each
  of which names a data zone: **512 × 512 = 262,144 blocks**, another
  **268,435,456 bytes** — 256MB — at the cost of two extra reads.

Add it up:

```
tier              file blocks      bytes
direct                      7          7,168
indirect                  512        524,288
double indirect       262,144    268,435,456
                     --------   ------------
total                 262,663    268,966,912   (~256.5 MB max file)
```

Two design pressures are visible in these numbers. First, the seven
direct slots are not an accident: the overwhelming majority of files —
in 1987 and on your machine right now (`ls -l /etc | awk '$5 < 7168'`
catches most of it) — fit in 7KB, so the common case pays for no
indirection at all. The tree only grows as deep as the file is big.

Second, look at the ceiling: the zone tree can address a 256MB file,
but every zone *number* in it is a u16. At most 65,535 addressable
zones × 1KB each = **a 64MB filesystem, total**, no matter how the
tree is shaped. The addressing scheme outruns the address width by a
factor of four. That 64MB wall — along with 14-character filenames —
is exactly what pushed Rémy Card to write ext, the "extended
filesystem", in April 1992: the first filesystem built on Linux's
then-new VFS layer, which existed largely so Linux could escape the
Minix filesystem it had borrowed for compatibility (early Linux
development happened on disks shared with a Minix install). Ext kept
this same inode design — it just widened the numbers.

## The map, drawn

```
struct inode                                            file
+------------+                                          block
| i_zone[0] -----------------------------> [ data ]       0
| i_zone[1] -----------------------------> [ data ]       1
|    ...     |                               ...          ...
| i_zone[6] -----------------------------> [ data ]       6
|            |    indirect zone
|            |    (512 u16 entries)
| i_zone[7] ----> +---------+
|            |    | e[0]   -------------> [ data ]        7
|            |    |  ...    |               ...           ...
|            |    | e[511] --------------> [ data ]      518
|            |    +---------+
|            |    double zone       indirect zones
| i_zone[8] ----> +---------+
+------------+    | e[0]   ----> +---------+
                  |  ...    |    | e[0]   ---> [ data ]  519
                  | e[511] -|    |  ...    |    ...      ...
                  +---------+    | e[511] ---> [ data ]  1030
                       |
                       +-------> ... 511 more indirect zones,
                                 the last ending at file
                                 block 262662
```

Read it top to bottom and the shape of the algorithm falls out: the
file block index tells you which tier you're in, and the tiers are
consumed in order — direct first, then the indirect window of 512,
then the double window of 512×512. Everything below is subtraction,
division, and remainder.

## Walking to block 700 by hand

Say a read needs file block 700 — the bytes at offsets 716,800 through
717,823. Which zone holds them? Do exactly what the code will do:

```
fileblock = 700

700 >= 7 ?            yes -- not direct.       700 - 7   = 693
693 >= 512 ?          yes -- not indirect.     693 - 512 = 181
181 <  512 * 512 ?    yes -- double indirect:

    level-0 index = 181 / 512 = 0      entry 0 of the double zone
    level-1 index = 181 % 512 = 181    entry 181 of that indirect zone
```

So the walk is: read the zone named by `i_zone[8]`; take its entry 0,
which names an indirect zone; read that; take *its* entry 181, which
names the data zone; read the data. Three disk reads where a direct
block needs one — a fixed, known worst case. Each subtraction above
re-bases `fileblock` into the current tier's window, and the final
divide/mod splits the window offset into (which indirect zone, which
entry) — precisely the two array indexes the disk layout demands. When
you write `inode_bmap` below, this arithmetic is the whole function.

The whole tree at once — one worked fan-out per level; every arrow is 'the u16 here names the zone there':

```d2
direction: right

ino: "struct inode" {
  shape: sql_table
  "zones[0..6]": "7 direct"
  "zones[7]": "indirect"
  "zones[8]": "double"
}

d06: "data\nfile blocks 0..6"

ind: "indirect zone (512 u16)" {
  shape: sql_table
  "e[0]": ""
  "⋮": ""
  "e[511]": ""
}

d7: "data — block 7"
d518: "data — block 518"

dbl: "double zone (512 u16)" {
  shape: sql_table
  "e[0]": ""
  "⋮": ""
  "e[511]": ""
}

ind2: "indirect zone" {
  shape: sql_table
  "e[0]": ""
  "⋮": ""
  "e[511]": ""
}

d519: "data — block 519"
d1030: "data — block 1030"

more: "… 511 more indirect zones,\nlast data block = 262,662" {
  shape: text
}

ino."zones[0..6]" -> d06
ino."zones[7]" -> ind
ind."e[0]" -> d7
ind."e[511]" -> d518
ino."zones[8]" -> dbl
dbl."e[0]" -> ind2
dbl."e[511]" -> more
ind2."e[0]" -> d519
ind2."e[511]" -> d1030
```

## Holes: files with less data than size

One value is conspicuously special in every slot of this tree: **zone
number 0 never names a real zone.** Recall the bitmap lesson — zone
numbering starts at 1 and bit 0 of the zone bitmap is permanently set
precisely so that 0 can be reserved as "no zone here." A 0 in a zone
slot is a **hole**: that stretch of the file simply has no disk block.

Holes arise naturally. `lseek()` a fresh file to offset 1,000,000,
write one byte, and the filesystem allocates a zone for the final
block, sets `i_size = 1000001` — and leaves every intervening zone
slot 0. Nothing forces it to allocate blocks nobody wrote. A `read()`
that lands in a hole returns zeros, synthesized in the kernel without
touching the disk; a later `write()` into the hole allocates a real
zone on the spot. The visible symptom is two commands disagreeing:

```
$ ls -l sparse     ->  1000001 bytes     (reports i_size)
$ du -k sparse     ->  1 KB              (reports allocated zones)
```

`ls` reads the inode's claim; `du` counts actual blocks. Database
files, core dumps, and VM disk images all lean on this — a 10GB image
that's 98% unwritten costs 200MB of disk.

Now the elegant part: the hole convention applies at *every level* of
the tree. If `i_zone[7]` itself is 0, there is no indirect zone at all
— file blocks 7 through 518 are all holes, a 512-block hole for the
price of one u16. Same for `i_zone[8]`, and same for any single entry
*inside* a double zone: one zero u16 at level 0 stands for 512 absent
data blocks. Your `inode_bmap` must therefore be prepared to hit a 0
at any depth and report "hole" without reading further — reading
"zone 0" as if it were data would hand back the boot block.

Holes at every level — greyed boxes are blocks that exist only as zeros; a single 0 in zones[8] stands in for a quarter-million of them:

```d2
direction: right

ino: "struct inode" {
  shape: sql_table
  "zones[2]": "0"
  "zones[7]": "zone 200"
  "zones[8]": "0"
}

ind: "indirect zone 200" {
  shape: sql_table
  "e[0]": "zone 20"
  "e[1]": "0"
}

d: "data — block 7"

h1: "1 block of zeros" {
  style.stroke-dash: 4
  style.font-color: "#9ca3af"
}

h2: "1 block of zeros" {
  style.stroke-dash: 4
  style.font-color: "#9ca3af"
}

h3: "262,144 blocks of zeros" {
  style.stroke-dash: 4
  style.font-color: "#9ca3af"
}

ino."zones[2]" -> h1: hole {style.stroke-dash: 4}
ino."zones[7]" -> ind
ind."e[0]" -> d
ind."e[1]" -> h2: hole {style.stroke-dash: 4}
ino."zones[8]" -> h3: hole {style.stroke-dash: 4}
```

## Why pointer trees and not extents

Modern filesystems (ext4, XFS, btrfs) describe file layout with
**extents** — (start block, length) pairs — instead of one pointer per
block. For a large file laid out contiguously, one extent record
replaces thousands of zone numbers, and that compactness is why they
won. So why does DuckOS, like Minix, use the pointer tree?

Because the pointer tree is *arithmetic* and extents are a *search*.
Given a file block index, the tree lookup is the fixed subtract/divide
chain you just walked — same instructions for every block of every
file, worst case three reads, implementable in a page of C with no
allocation and no data-dependent control flow. An extent lookup must
search a variable-length list (or a B+tree of extent records, as ext4
actually keeps) to find which extent covers the block: variable-shaped
metadata, rebalancing on split, and a dozen new failure modes. For a
teaching OS the choice makes itself; it's also the honest historical
one — the pointer-tree design survived from V7 Unix through Minix into
ext2/ext3 (which widened it to 12 direct slots and added a triple
indirect), and extents only displaced it in ext4 in 2008.

The tree's real cost is metadata reads on big files: sweeping through
a 100MB file re-reads the same double and indirect zones over and
over. In practice the buffer cache from *Block Devices and the Buffer
Cache* absorbs almost all of it — an indirect zone consulted for file
block 519 is still cached when block 520 needs it a microsecond later,
so the steady-state overhead of the tree is close to zero extra I/O.
Mechanism in the filesystem, performance in the cache: each layer does
its own job.

## Challenge: bmap {#inode-bmap points=25}

`bmap` — block map — is the seam this whole lesson exists for: the
function that turns "file block N of this inode" into "zone Z of the
disk." Every byte the filesystem ever reads or writes on a file's
behalf funnels through it, and once it exists, the rest of file I/O
stops caring that indirect zones exist at all. You'll build `bmap`
first, then prove the point by building byte-level `read()` on top of
it in a dozen lines.

The disk is `const uint8_t *disk`: `NDISK` contiguous 1KB zones, zone
`n` starting at byte `n * BLOCK_SIZE`. Indirect and double zones hold
little-endian u16 zone numbers packed back to back — parse them
explicitly with the provided `get_le16`; never cast the disk pointer
to `uint16_t *` (alignment aside, the bytes' order is the disk
format's business, not the host CPU's).

**`inode_bmap(ino, disk, fileblock)`** returns the zone number holding
file block `fileblock`, `0` for a hole, or a negative errno:

- `fileblock < NDIRECT` (7): the answer is `zones[fileblock]` — which
  may itself be 0, a hole.
- The next `PTRS_PER_BLOCK` (512) blocks go through `zones[7]`. If
  `zones[7]` is 0 the whole window is a hole: return 0. Otherwise read
  entry `fileblock - NDIRECT` of that zone; an entry of 0 is a hole.
- The next `PTRS_PER_BLOCK * PTRS_PER_BLOCK` (262,144) blocks go
  through `zones[8]`, two levels deep, exactly as in the worked
  example: re-base, then `/ PTRS_PER_BLOCK` picks the level-0 entry
  and `% PTRS_PER_BLOCK` the level-1 entry. A 0 at either level — the
  double zone itself, a level-0 entry, or the final entry — is a hole:
  return 0.
- Any `fileblock` beyond the double-indirect range: `-EINVAL`.

And one rule that outranks all of the above: **every zone number, from
`zones[]` or read off the disk, must be checked `< NDISK` before you
use it — return `-EIO` if not.** This is a security boundary, not
pedantry. The zone numbers live in the disk image, and the disk image
is *input* — a corrupted filesystem, a hostile USB stick, a fuzzer's
output. An unchecked entry of 5000 on a 4096-zone disk sends the
kernel indexing past the end of the buffer; in a real kernel that's an
out-of-bounds read in ring 0, and mount-time parsing bugs of exactly
this shape have a long CVE pedigree in real filesystems. The tests
plant a corrupt entry and expect `-EIO`, not a lucky read. (`-EIO`
also can never collide with a real answer: valid returns are 0 through
`NDISK - 1`.)

**`inode_read(ino, disk, off, dst, n)`** reads up to `n` bytes at byte
offset `off` into `dst`, returning the number of bytes read or a
negative errno:

- If `off >= ino->size`, return 0 — reads past EOF read nothing.
- Clamp `n` so the read never extends past `ino->size`.
- Loop over the file blocks the range touches, calling `inode_bmap`
  for each. Copy the right slice of each zone (the first and last
  blocks may be partial — the offset within a block is
  `off % BLOCK_SIZE`). A hole (bmap returned 0) contributes zeros to
  `dst`, no disk access. A negative bmap return propagates out
  immediately.
- Return the total bytes copied.

Notice what `inode_read` does *not* contain: any mention of indirect
zones. One loop, one call per block — the abstraction pays for itself
on the first day.

The tests build a synthetic disk image in a static array: direct zones
with known fill bytes, an indirect zone whose first and last entries
are populated, a double zone wired so that file block 700 resolves
through indices 0 and 181 exactly as the prose example, holes at every
level (a zeroed direct slot, a zeroed entry inside a live indirect
zone, an inode with no indirect zone at all), one corrupt entry of
5000 that must yield `-EIO`, and an `i_size` chosen so reads clamp
mid-block. Byte-level reads are checked byte by byte, including the
zeros inside a hole.

### Starter

```c
#include <stdint.h>
#include <string.h>

#define BLOCK_SIZE 1024
#define NDIRECT 7
#define PTRS_PER_BLOCK (BLOCK_SIZE / 2)	/* 512 u16 zone numbers */
#define NDISK 4096	/* zones on our toy disk */
#define EINVAL 22
#define EIO 5

/*
 * The two fields of the 32-byte Minix v1 inode that bmap needs.
 * zones[] holds ZONE NUMBERS: zone n starts at disk + n * BLOCK_SIZE.
 * Zone number 0 never names a real zone (numbering starts at 1), so a
 * 0 in any slot -- or in any indirect entry -- means "hole".
 */
struct inode {
	uint32_t size;		/* file length in bytes */
	uint16_t zones[9];	/* [0..6] direct, [7] indirect, [8] double */
};

/* One little-endian u16: indirect zones are arrays of these. */
static inline uint16_t get_le16(const uint8_t *p)
{
	return (uint16_t)(p[0] | ((uint16_t)p[1] << 8));
}

/*
 * Map a file block index to a zone number.
 *
 * Returns the zone number (1..NDISK-1), 0 if the block is a hole,
 * -EINVAL if fileblock is beyond the double-indirect range, or -EIO
 * if any zone number encountered (in zones[] or read from disk) is
 * >= NDISK. Never index the disk with an unchecked zone number.
 */
int inode_bmap(const struct inode *ino, const uint8_t *disk,
	       uint32_t fileblock)
{
	/*
	 * TODO: three tiers, in order.
	 *   fileblock < NDIRECT: answer is zones[fileblock].
	 *   next PTRS_PER_BLOCK blocks: via zones[7]; a 0 pointer or a
	 *     0 entry is a hole (return 0).
	 *   next PTRS_PER_BLOCK * PTRS_PER_BLOCK: via zones[8], two
	 *     levels (/ and % PTRS_PER_BLOCK); 0 at any level is a hole.
	 *   beyond that: -EINVAL.
	 * Check EVERY zone number < NDISK before using it; else -EIO.
	 */
	(void)ino;
	(void)disk;
	(void)fileblock;
	return 0;
}

/*
 * Read up to n bytes at byte offset off into dst.
 *
 * Returns bytes read (0 if off >= size; n is clamped so the read
 * never passes size), or a negative errno propagated from bmap.
 * Holes read as zeros.
 */
int inode_read(const struct inode *ino, const uint8_t *disk,
	       uint32_t off, uint8_t *dst, uint32_t n)
{
	/*
	 * TODO: clamp n to size - off, then loop over the file blocks
	 * the range touches: bmap each, copy the right slice of the
	 * zone (first/last blocks may be partial), memset zeros for a
	 * hole, and bail out on a negative bmap return.
	 */
	(void)ino;
	(void)disk;
	(void)off;
	(void)dst;
	(void)n;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define BLOCK_SIZE 1024
#define NDIRECT 7
#define PTRS_PER_BLOCK (BLOCK_SIZE / 2)	/* 512 u16 zone numbers */
#define NDISK 4096	/* zones on our toy disk */
#define EINVAL 22
#define EIO 5

struct inode {
	uint32_t size;		/* file length in bytes */
	uint16_t zones[9];	/* [0..6] direct, [7] indirect, [8] double */
};

int inode_bmap(const struct inode *ino, const uint8_t *disk,
	       uint32_t fileblock);
int inode_read(const struct inode *ino, const uint8_t *disk,
	       uint32_t off, uint8_t *dst, uint32_t n);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* The synthetic disk: NDISK contiguous zones, zero-filled to start. */
static uint8_t disk[(size_t)NDISK * BLOCK_SIZE];

static void put_le16(uint8_t *p, uint16_t v)
{
	p[0] = (uint8_t)(v & 0xff);
	p[1] = (uint8_t)(v >> 8);
}

/* Write entry idx of the indirect/double zone `zone`. */
static void set_entry(uint16_t zone, uint32_t idx, uint16_t val)
{
	put_le16(disk + (size_t)zone * BLOCK_SIZE + (size_t)idx * 2, val);
}

/* Fill data zone `zone` with a recognizable byte. */
static void fill_zone(uint16_t zone, uint8_t val)
{
	memset(disk + (size_t)zone * BLOCK_SIZE, val, BLOCK_SIZE);
}

/*
 * The main test inode. size = 7.5 blocks so reads clamp mid-block 7.
 *
 *   file block 0..6  -> zones 10,11,0(hole),13,14,15,16
 *   file block 7     -> indirect zone 200, entry 0   -> zone 20
 *   file block 8     -> indirect zone 200, entry 1   -> 0 (hole)
 *   file block 16    -> indirect zone 200, entry 9   -> 5000 (corrupt!)
 *   file block 518   -> indirect zone 200, entry 511 -> zone 21
 *   file block 519   -> double 300, e[0] -> 301, e[0]   -> zone 30
 *   file block 700   -> double 300, e[0] -> 301, e[181] -> zone 31
 */
static struct inode ino;

/* An inode with every slot 0: one giant hole. */
static struct inode hole_ino;

static void build_disk(void)
{
	ino.size = 7 * BLOCK_SIZE + 512;
	ino.zones[0] = 10;
	ino.zones[1] = 11;
	ino.zones[2] = 0;	/* hole in the direct slots */
	ino.zones[3] = 13;
	ino.zones[4] = 14;
	ino.zones[5] = 15;
	ino.zones[6] = 16;
	ino.zones[7] = 200;	/* indirect zone */
	ino.zones[8] = 300;	/* double-indirect zone */

	fill_zone(10, 10);
	fill_zone(11, 11);
	fill_zone(13, 13);
	fill_zone(14, 14);
	fill_zone(15, 15);
	fill_zone(16, 16);
	fill_zone(20, 20);
	fill_zone(21, 21);
	fill_zone(30, 30);
	fill_zone(31, 31);

	set_entry(200, 0, 20);		/* file block 7 */
	set_entry(200, 9, 5000);	/* file block 16: >= NDISK, corrupt */
	set_entry(200, 511, 21);	/* file block 518 */

	set_entry(300, 0, 301);		/* level 0: entry 0 -> indirect 301 */
	set_entry(301, 0, 30);		/* file block 519 */
	set_entry(301, 181, 31);	/* file block 700 */

	hole_ino.size = 600 * BLOCK_SIZE;
}

int main(void)
{
	build_disk();

	/* --- bmap: the three tiers --- */
	check(inode_bmap(&ino, disk, 0) == 10, "test_direct_block_0");
	check(inode_bmap(&ino, disk, 6) == 16, "test_direct_block_6");
	check(inode_bmap(&ino, disk, 7) == 20, "test_indirect_first_entry");
	check(inode_bmap(&ino, disk, 518) == 21, "test_indirect_last_entry");
	check(inode_bmap(&ino, disk, 519) == 30, "test_double_first");
	/* 700 - 7 = 693; 693 - 512 = 181; 181/512 = 0, 181%512 = 181 */
	check(inode_bmap(&ino, disk, 700) == 31, "test_block_700");

	/* --- bmap: holes at every level --- */
	check(inode_bmap(&ino, disk, 2) == 0, "test_direct_hole");
	check(inode_bmap(&hole_ino, disk, 100) == 0,
	      "test_missing_indirect_zone_is_hole");
	check(inode_bmap(&ino, disk, 8) == 0, "test_indirect_hole_entry");

	/* --- bmap: corruption and range errors --- */
	check(inode_bmap(&ino, disk, 16) == -EIO, "test_corrupt_zone_is_eio");
	check(inode_bmap(&ino, disk,
			 NDIRECT + PTRS_PER_BLOCK +
			 (uint32_t)PTRS_PER_BLOCK * PTRS_PER_BLOCK) == -EINVAL,
	      "test_past_double_range_is_einval");

	/* --- read: spans the direct -> indirect boundary --- */
	{
		uint8_t buf[100];
		int r, i, ok;

		memset(buf, 0xAA, sizeof buf);
		r = inode_read(&ino, disk, 6 * BLOCK_SIZE + 1000, buf, 100);
		ok = (r == 100);
		for (i = 0; i < 24; i++)	/* tail of block 6 */
			ok = ok && buf[i] == 16;
		for (i = 24; i < 100; i++)	/* head of block 7 */
			ok = ok && buf[i] == 20;
		check(ok, "test_read_spans_direct_indirect");
	}

	/* --- read: a hole yields zeros, then real data resumes --- */
	{
		static uint8_t buf[1100];
		int r, i, ok;

		memset(buf, 0xAA, sizeof buf);
		r = inode_read(&ino, disk, BLOCK_SIZE + 1000, buf, 1100);
		ok = (r == 1100);
		for (i = 0; i < 24; i++)	/* tail of block 1 */
			ok = ok && buf[i] == 11;
		for (i = 24; i < 24 + 1024; i++)	/* block 2: hole */
			ok = ok && buf[i] == 0;
		for (i = 24 + 1024; i < 1100; i++)	/* head of block 3 */
			ok = ok && buf[i] == 13;
		check(ok, "test_read_hole_reads_zeros");
	}

	/* --- read: offset at/past size reads nothing --- */
	{
		uint8_t buf[8];

		memset(buf, 0xAA, sizeof buf);
		check(inode_read(&ino, disk, ino.size + 100, buf, 8) == 0,
		      "test_read_off_past_size");
	}

	/* --- read: n is clamped at size (size ends mid-block 7) --- */
	{
		static uint8_t buf[4096];
		int r, i, ok;

		memset(buf, 0xAA, sizeof buf);
		r = inode_read(&ino, disk, 7 * BLOCK_SIZE, buf, 4096);
		ok = (r == 512);
		for (i = 0; i < 512; i++)
			ok = ok && buf[i] == 20;
		check(ok, "test_read_clamped_at_size");
	}

	return failed;
}
```

# Lesson: Directories and Path Walking {#directories}

Run `ls` in a directory and something reads bytes off a disk and
prints names. It is tempting to imagine directory machinery deep in
the kernel doing something exotic. Here is the demystification this
lesson is built on: **a directory is a file.** It has an inode like
any file (recall *Inodes: Files Without Names*), it stores its bytes
in zones like any file, and `ls` is little more than `read()` plus a
decoder loop. The only thing that makes a directory special is what
its bytes mean: a table mapping *names* to *inode numbers*. The
kernel enforces exactly one rule on top — user processes may not
`write()` a directory, because a corrupt name table strands every
file beneath it.

That is the whole trick by which Unix gives files names while keeping
the inode nameless: names live in directories, directories are files,
and the two systems meet only at the inode number.

The two systems side by side — names on the left, file bodies on the right, and inode numbers as the only bridge:

```d2
direction: right

names: "name tables (directories)" {
  bin: "/usr/bin" {
    shape: sql_table
    cc: "cc → 143"
    ld: "ld → 91"
  }
  ada: "/home/ada" {
    shape: sql_table
    mycc: "mycc → 143"
  }
}

ino: "inode table" {
  i143: "inode 143" {
    shape: sql_table
    n: "nlinks = 2"
    s: "mode, size, zones…"
    x: "(no name field)"
  }
}

data: "file bytes"

names.bin.cc -> ino.i143
names.ada.mycc -> ino.i143
ino.i143 -> data
```

## Sixteen bytes per name

The Minix v1 directory entry is brutally simple:

```
  offset  size  field
  ------  ----  -----------------------------------------
       0     2  inode number (u16, little-endian)
       2    14  name (NUL-padded, NOT NUL-terminated
                when exactly 14 bytes long)
  ------  ----  -----------------------------------------
      16 bytes total
```

A directory's content is just these records, back to back. An empty
directory is 32 bytes: two entries, `.` and `..` — and they are
*real entries on disk*, planted by mkfs, not path-parser magic. `.`
holds the directory's own inode number; `..` holds the parent's.
That's why a directory's link count starts at 2 (its parent's entry
for it, plus its own `.`), and it's how the tree can be walked upward
with nothing but the tree itself.

Two fields of sixteen bytes, and still two classic traps:

**Trap one: inode 0 means "no entry."** Remember the punchline from
*A Filesystem on a Disk*: in every corner of Minix, 0 means nothing —
a hole in a file, an empty dirent slot. When you `unlink` a file, the
kernel does not shuffle the directory to close the gap; it writes a
0 over the entry's inode field and moves on. The fourteen name bytes
are left behind as a ghost. Decades of undelete tools were built on
exactly this laziness: the name (and, until reuse, the inode it
pointed to) is still right there. Your scanner must *skip* zero-inode
slots — but a lookup must skip them even when the ghost name matches,
because a matching ghost is still deleted.

**Trap two: the name field is not a C string.** Fourteen bytes hold
the name, NUL-padded — but a name of exactly fourteen characters
fills the field completely and has **no terminator**. Aim `strcmp`
at it and the comparison runs off the entry into the next one (or
off the buffer). This bug class is everywhere fixed-width name
fields live: FAT's 8.3 entries, tar headers, utmp records. The safe
pattern is the one you'll write in `dir_name_eq`: bound every
comparison by the field width, and demand a NUL *only* when the name
is shorter than the field.

Why fourteen? Because 2 + 14 = 16, and sixteen divides a 1 KiB block
evenly — entries never straddle blocks and the scan is pure
arithmetic. When Berkeley's FFS and later ext wanted longer names,
they paid for them with variable-length records, in-entry length
fields, and padding logic — a real cost in code and corruption
surface that Minix simply declined to pay. The 14-character limit
chafed (it is on the short list of complaints ext was written to
answer — see *Inodes: Files Without Names*), but you should
understand it as a trade, not an oversight.

## namei: the oldest function in Unix

Somewhere in every Unix descendant is a function that turns
`/usr/bin/cc` into an inode. In Sixth Edition it was called `namei`
— *name to i-number* — and the name survives in kernels (and kernel
folklore) to this day. The algorithm hasn't changed in fifty years:

```
  namei("/usr/bin/cc"):
      cur = root inode                        # '/' anchors the walk
      "usr"  -> look up in cur's table  -> inode 24   (a directory)
      "bin"  -> look up in inode 24     -> inode 87   (a directory)
      "cc"   -> look up in inode 87     -> inode 143  (whatever it is)
      return 143
```

Each step is a `dir_lookup` — the function you build in the first
challenge — against the *current* directory only. There is no global
name table; the path IS the search plan. The subtleties are all in
the walking rules:

The same walk over real tables — each hop reads one directory's bytes; the bottom path shows the walk dying with ENOTDIR when it tries to descend through a file:

```d2
direction: right

r: "inode 1 — /" {
  shape: sql_table
  usr: "usr → 24"
  etc: "etc → 5"
}

u: "inode 24" {
  shape: sql_table
  bin: "bin → 87"
}

b: "inode 87" {
  shape: sql_table
  cc: "cc → 143"
}

t: "inode 143\ndone" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

p: "inode 6\n(a file)"

err: "descend?\nENOTDIR" {
  shape: text
  style.font-color: "#dc2626"
}

r.usr -> u: "\"usr\""
u.bin -> b: "\"bin\""
b.cc -> t: "\"cc\""
r.etc -> p: "\"passwd\""
p -> err: {
  style.stroke: "#dc2626"
  style.stroke-dash: 4
}
```

- **Runs of slashes collapse.** `/usr//bin` and `/usr/bin` name the
  same file; empty components are skipped, not looked up.
- **`.` and `..` are ordinary lookups** in a real Minix — they hit
  the actual on-disk entries mkfs planted. Our simulation keeps the
  name table flat, so the walker handles them itself: `.` stays put,
  `..` moves to the parent.
- **Root's `..` is root.** The topmost directory is its own parent —
  the walk cannot escape the tree by stacking `../../../..`. This
  little containment rule is why `chroot` jails are even conceivable
  (and the fine print of escaping badly-built ones is a security
  literature of its own).
- **Every component you descend *through* must be a directory.**
  `/etc/passwd/x` fails with `ENOTDIR` the moment the walk tries to
  go through `passwd` — a distinct error from `ENOENT` (no such
  entry), and programs genuinely branch on the difference.
- **A trailing slash asserts "this is a directory."** `/usr/bin/`
  resolves fine; `/etc/passwd/` must fail with `ENOTDIR` even though
  `/etc/passwd` resolves. The slash is a claim, and namei checks it.

What we're *not* doing deserves a sentence each, so you know where
the hooks go in a grown-up kernel. Permissions: each descent would
check execute permission on the directory before looking inside.
Symlinks: a resolved component might be a link, whose target text is
spliced into the remaining path (with a loop counter, because links
can cycle). Mount points: crossing one swaps which filesystem's root
the walk continues in. All three are *per-step* interventions in
exactly the loop you're about to write — which is why getting the
loop right matters.

One design choice to note: real Minix silently *truncated* each
component to 14 characters, so `/very-long-name-here` and its
15-character sibling collided on disk. DuckOS chooses strictness — a
component longer than `NAME_LEN` simply does not exist (`ENOENT`) —
because silent truncation is the kind of footgun that turns two
different names into one file.

## Challenge: Reading a Directory {#dirent-scan points=15}

Decode raw directory bytes: `data` is the content of a directory
file (its size a multiple of `DIRENT_SIZE`), exactly as `inode_read`
from *Inodes: Files Without Names* would hand it to you.

`dir_name_eq(entry_name, name)` — does the C string `name` equal the
fixed 14-byte field at `entry_name`? Longer than `NAME_LEN` can never
match. Compare bytewise over `strlen(name)`; if the name is shorter
than the field, the field's next byte must be NUL (else you matched a
prefix of a longer stored name). No `strcmp` — the field may lack a
terminator.

`dir_lookup(data, size, name)` — scan the entries; skip slots whose
inode is 0 (deleted — even if the ghost name matches); return the
first live match's inode number, or 0 for no match. Inode numbers are
u16 little-endian, parsed explicitly as always.

`dir_count(data, size)` — how many live entries.

`dir_entry_name(data, size, idx, out)` — find the idx'th live entry
(0-based, in table order), copy its name into `out` as a proper
NUL-terminated C string (`out` has room for `NAME_LEN + 1`), and
return its inode; 0 if there are not that many live entries. This is
the loop inside every `ls` you have ever run.

### Starter

```c
#include <stdint.h>
#include <string.h>

#define DIRENT_SIZE 16
#define NAME_LEN 14

/*
 * A Minix v1 directory entry, as raw bytes:
 *   bytes 0-1   inode number, u16 little-endian; 0 = empty slot
 *   bytes 2-15  name, NUL-padded, NOT NUL-terminated at full length
 */

/*
 * 1 if the C string `name` names the fixed field at `entry_name`.
 * Bound every comparison by NAME_LEN; require a NUL in the field
 * only when strlen(name) < NAME_LEN. Never strcmp the raw field.
 */
int dir_name_eq(const uint8_t *entry_name, const char *name)
{
	/* TODO: reject strlen > NAME_LEN; compare bytewise; check
	 * the field ends (NUL) exactly where the name does. */
	(void)entry_name;
	(void)name;
	return 0;
}

/*
 * Scan a directory's bytes for `name`. Skips deleted slots
 * (inode == 0) even when their ghost name matches. Returns the
 * first live match's inode number, or 0 if absent.
 */
uint16_t dir_lookup(const uint8_t *data, uint32_t size, const char *name)
{
	/* TODO: walk in DIRENT_SIZE strides; parse the u16 inode
	 * little-endian (data[off] | data[off+1] << 8). */
	(void)data;
	(void)size;
	(void)name;
	return 0;
}

/* Live (inode != 0) entries in the table. */
int dir_count(const uint8_t *data, uint32_t size)
{
	/* TODO */
	(void)data;
	(void)size;
	return 0;
}

/*
 * The idx'th live entry (0-based, table order): copy its name into
 * out as a NUL-terminated string (out has NAME_LEN + 1 bytes) and
 * return its inode number. 0 if fewer than idx + 1 live entries.
 */
int dir_entry_name(const uint8_t *data, uint32_t size, int idx, char *out)
{
	/* TODO: count live entries down to idx; memcpy NAME_LEN bytes
	 * then plant out[NAME_LEN] = '\0'. */
	(void)data;
	(void)size;
	(void)idx;
	(void)out;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define DIRENT_SIZE 16
#define NAME_LEN 14

int dir_name_eq(const uint8_t *entry_name, const char *name);
uint16_t dir_lookup(const uint8_t *data, uint32_t size, const char *name);
int dir_count(const uint8_t *data, uint32_t size);
int dir_entry_name(const uint8_t *data, uint32_t size, int idx, char *out);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/*
 * Plant entry `slot` in a raw directory image. Writes the name
 * bytewise with NO terminator when it fills the field -- exactly
 * what mkfs puts on disk.
 */
static void plant(uint8_t *img, int slot, uint16_t ino, const char *name)
{
	uint8_t *e = img + slot * DIRENT_SIZE;
	size_t i, n = strlen(name);

	e[0] = (uint8_t)(ino & 0xff);
	e[1] = (uint8_t)(ino >> 8);
	for (i = 0; i < NAME_LEN; i++)
		e[2 + i] = i < n ? (uint8_t)name[i] : 0;
}

int main(void)
{
	uint8_t img[8 * DIRENT_SIZE];
	char nm[NAME_LEN + 1];
	int r;

	memset(img, 0xEE, sizeof img);	/* poison: no free NULs */
	plant(img, 0, 1, ".");
	plant(img, 1, 1, "..");
	plant(img, 2, 42, "duck");
	plant(img, 3, 0, "deleted");	/* ghost: name intact, inode 0 */
	plant(img, 4, 300, "fourteen-chars");	/* exactly NAME_LEN */
	plant(img, 5, 7, "abc");
	plant(img, 6, 9, "abcd");
	plant(img, 7, 500, "tail");

	check(dir_lookup(img, sizeof img, "duck") == 42,
	      "test_lookup_finds_entry");
	check(dir_lookup(img, sizeof img, "tail") == 500,
	      "test_lookup_finds_last_entry");
	check(dir_lookup(img, sizeof img, ".") == 1 &&
	      dir_lookup(img, sizeof img, "..") == 1,
	      "test_dot_and_dotdot_are_real_entries");
	check(dir_lookup(img, sizeof img, "swim") == 0,
	      "test_lookup_miss_returns_zero");

	/* The full-width name has no NUL; the compare must not run
	 * into the next entry's bytes. */
	check(dir_lookup(img, sizeof img, "fourteen-chars") == 300,
	      "test_fourteen_char_name");

	/* Prefixes must not match, in either direction. */
	check(dir_lookup(img, sizeof img, "abc") == 7 &&
	      dir_lookup(img, sizeof img, "abcd") == 9,
	      "test_prefix_no_false_match");
	check(dir_lookup(img, sizeof img, "duc") == 0 &&
	      dir_lookup(img, sizeof img, "ducks") == 0,
	      "test_partial_names_miss");

	/* A 15-char query shares its first 14 bytes with the planted
	 * full-width name -- and must still miss. */
	check(dir_lookup(img, sizeof img, "fourteen-charsX") == 0,
	      "test_overlong_query_misses");

	/* Deleted slot: ghost name intact, but inode 0 means gone. */
	check(dir_lookup(img, sizeof img, "deleted") == 0,
	      "test_deleted_slot_skipped");

	check(dir_count(img, sizeof img) == 7, "test_count_skips_deleted");

	/* Entry iteration: index counts LIVE entries only. */
	r = dir_entry_name(img, sizeof img, 2, nm);
	check(r == 42 && strcmp(nm, "duck") == 0,
	      "test_entry_name_by_live_index");
	r = dir_entry_name(img, sizeof img, 3, nm);	/* skips the ghost */
	check(r == 300 && strcmp(nm, "fourteen-chars") == 0 &&
	      strlen(nm) == NAME_LEN,
	      "test_entry_name_terminates_full_width");
	check(dir_entry_name(img, sizeof img, 7, nm) == 0,
	      "test_entry_index_past_end");

	/* dir_name_eq directly: the field-boundary cases. */
	{
		uint8_t f[NAME_LEN];

		memset(f, 0, sizeof f);
		memcpy(f, "cat", 3);
		check(dir_name_eq(f, "cat") == 1 &&
		      dir_name_eq(f, "ca") == 0 &&
		      dir_name_eq(f, "cats") == 0,
		      "test_name_eq_short_name");
		memcpy(f, "cats-and-ducks", NAME_LEN);	/* no NUL fits */
		check(dir_name_eq(f, "cats-and-ducks") == 1 &&
		      dir_name_eq(f, "cats-and-duck") == 0 &&
		      dir_name_eq(f, "cats-and-ducks!") == 0,
		      "test_name_eq_full_width");
	}

	return failed;
}
```

## Challenge: namei {#path-resolve points=25}

Now the walk itself. To keep this challenge about the *walking rules*
rather than disk plumbing (you already proved the byte-level scan
above, and `inode_bmap` proved the zone walk), the filesystem here is
a flat in-memory table: one record per file, each knowing its own
inode number, its parent directory's inode, whether it is a
directory, and its name within that parent. The starter provides the
two lookups; every rule from the lesson — and every error code — is
yours.

`path_resolve(fs, path)` returns the inode number of the file `path`
names, or a negative errno:

- Empty string, or any path not starting with `/`: `-EINVAL`
  (DuckOS has no per-process current directory yet, so relative
  paths are meaningless here).
- Start at `ROOT_INO` and walk component by component. Runs of
  slashes collapse. `.` stays put; `..` moves to the parent — and
  root's parent is root.
- A component longer than `NAME_LEN`: `-ENOENT` (we are stricter
  than real Minix's silent truncation, on purpose).
- A missing component: `-ENOENT`. Descending *through* (or asserting
  a trailing slash on) a non-directory: `-ENOTDIR`.
- `/` alone resolves to `ROOT_INO`.

The rule that catches most first attempts: when a component is
followed by a slash — `x` in `/x/y`, or the final `bin` in
`/usr/bin/` — whatever it resolved to must be a directory *at that
moment*, or the answer is `-ENOTDIR`, never `-ENOENT`.

### Starter

```c
#include <stdint.h>
#include <string.h>

#define NAME_LEN 14
#define NFILES 32
#define ENOENT 2
#define ENOTDIR 20
#define EINVAL 22
#define ROOT_INO 1

/*
 * One file in a flat name table: the same facts a real walk gathers
 * from dirents and inodes, laid out for clarity. ino 0 = free slot.
 * The root is its own parent -- ROOT_INO's record has parent
 * ROOT_INO.
 */
struct nfile {
	uint16_t ino;		/* 0 = free slot */
	uint16_t parent;	/* inode of the containing directory */
	int is_dir;
	char name[NAME_LEN + 1];	/* entry name within parent */
};

/* Record for inode `ino`, or NULL. (Provided.) */
const struct nfile *fs_find(const struct nfile *fs, uint16_t ino)
{
	for (int i = 0; i < NFILES; i++)
		if (fs[i].ino != 0 && fs[i].ino == ino)
			return &fs[i];
	return NULL;
}

/*
 * Inode of the entry called `name` (exactly -- no truncation) inside
 * directory dir_ino, or 0. Named children only: "." and ".." are the
 * walker's job, not the table's. (Provided.)
 */
uint16_t fs_lookup_in(const struct nfile *fs, uint16_t dir_ino,
                      const char *name)
{
	for (int i = 0; i < NFILES; i++)
		if (fs[i].ino != 0 && fs[i].parent == dir_ino &&
		    fs[i].ino != ROOT_INO &&
		    strcmp(fs[i].name, name) == 0)
			return fs[i].ino;
	return 0;
}

/*
 * namei. Absolute paths only (else -EINVAL; empty is -EINVAL too).
 * Walk from ROOT_INO applying the lesson's rules: slash runs
 * collapse; "." stays; ".." goes to the parent (root's parent is
 * root); components longer than NAME_LEN are -ENOENT; missing
 * entries are -ENOENT; descending through -- or asserting a
 * trailing slash on -- a non-directory is -ENOTDIR. Returns the
 * final inode number.
 */
int path_resolve(const struct nfile *fs, const char *path)
{
	/*
	 * TODO: the loop. Keep a cursor into path and a `cur` inode.
	 *   1. validate the leading '/'
	 *   2. skip slashes; at NUL, return cur
	 *   3. measure the component (to next '/' or NUL);
	 *      overlong -> -ENOENT
	 *   4. resolve it: "." / ".." via fs_find(cur)->parent /
	 *      fs_lookup_in; 0 -> -ENOENT
	 *   5. if the component is followed by '/', the result must
	 *      be a directory -> else -ENOTDIR
	 *   6. advance and repeat
	 */
	(void)fs;
	(void)path;
	return -EINVAL;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define NAME_LEN 14
#define NFILES 32
#define ENOENT 2
#define ENOTDIR 20
#define EINVAL 22
#define ROOT_INO 1

struct nfile {
	uint16_t ino;
	uint16_t parent;
	int is_dir;
	char name[NAME_LEN + 1];
};

const struct nfile *fs_find(const struct nfile *fs, uint16_t ino);
uint16_t fs_lookup_in(const struct nfile *fs, uint16_t dir_ino,
                      const char *name);
int path_resolve(const struct nfile *fs, const char *path);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/*
 * The tree under test:
 *
 *   /            ino 1
 *   /usr         ino 2   (dir)
 *   /usr/bin     ino 3   (dir)
 *   /usr/bin/cc  ino 4   (file)
 *   /etc         ino 5   (dir)
 *   /etc/passwd  ino 6   (file)
 *   /vmduck      ino 7   (file)
 */
static const struct nfile FS[NFILES] = {
	{ 1, 1, 1, "/" },
	{ 2, 1, 1, "usr" },
	{ 3, 2, 1, "bin" },
	{ 4, 3, 0, "cc" },
	{ 5, 1, 1, "etc" },
	{ 6, 5, 0, "passwd" },
	{ 7, 1, 0, "vmduck" },
};

int main(void)
{
	check(path_resolve(FS, "/") == 1, "test_root_resolves");
	check(path_resolve(FS, "/usr") == 2, "test_single_component");
	check(path_resolve(FS, "/usr/bin/cc") == 4, "test_nested_path");
	check(path_resolve(FS, "/vmduck") == 7, "test_file_in_root");

	/* Slash runs collapse. */
	check(path_resolve(FS, "//usr///bin") == 3,
	      "test_slash_runs_collapse");

	/* Dot: stay put. */
	check(path_resolve(FS, "/./usr/.") == 2, "test_dot_stays_put");

	/* Dotdot: up one; root's parent is root. */
	check(path_resolve(FS, "/usr/..") == 1, "test_dotdot_ascends");
	check(path_resolve(FS, "/..") == 1, "test_root_dotdot_is_root");
	check(path_resolve(FS, "/../../usr") == 2,
	      "test_dotdot_cannot_escape_root");

	/* The kitchen sink. */
	check(path_resolve(FS, "/usr/./bin/../../etc//passwd") == 6,
	      "test_mixed_walk");

	/* ENOENT: missing entries, and overlong components. */
	check(path_resolve(FS, "/nope") == -ENOENT, "test_missing_enoent");
	check(path_resolve(FS, "/usr/nope/cc") == -ENOENT,
	      "test_missing_middle_enoent");
	check(path_resolve(FS, "/fifteen-chars-x") == -ENOENT,
	      "test_overlong_component_enoent");

	/* ENOTDIR: through a file, or trailing slash on a file. */
	check(path_resolve(FS, "/etc/passwd/x") == -ENOTDIR,
	      "test_through_file_enotdir");
	check(path_resolve(FS, "/etc/passwd/") == -ENOTDIR,
	      "test_trailing_slash_on_file_enotdir");
	check(path_resolve(FS, "/usr/bin/") == 3,
	      "test_trailing_slash_on_dir_ok");

	/* EINVAL: relative and empty paths. */
	check(path_resolve(FS, "usr/bin") == -EINVAL,
	      "test_relative_einval");
	check(path_resolve(FS, "") == -EINVAL, "test_empty_einval");

	/* ENOENT beats a would-be ENOTDIR deeper in: the walk fails at
	 * the first bad step. */
	check(path_resolve(FS, "/nope/passwd/x") == -ENOENT,
	      "test_first_error_wins");

	return failed;
}
```

With `namei` working, DuckOS can turn any absolute path into an
inode, an inode into zones via `bmap`, and zones into cached bytes
via the buffer cache: the entire read path of a Unix filesystem,
every layer of which you have now built with your own hands. What
remains is the boundary where user programs ask for all of this
politely — the system call — and the machinery of processes being
born, dying, and being mourned. Then we assemble the whole bird.

# Lesson: The System Call Boundary {#system-calls}

Everything DuckOS has built so far — the proc table, the scheduler, the
filesystem — lives in ring 0, and everything a user program does happens
in ring 3. As we saw in *Segments and Privilege*, the CPU enforces that
wall: ring 3 code cannot touch kernel memory, cannot execute privileged
instructions, cannot talk to devices. Which raises the obvious question —
if a user program can't do anything, how does it do *anything*? How does
`cat` read a file when reading a disk requires ring 0?

The answer is the **system call**: the one and only door through the
wall. A user program puts a request in a well-known place, executes one
special instruction, and the CPU itself — not the program — transfers
control to a kernel-chosen address in ring 0. The kernel does the work
(or refuses), and returns a result.

Everything about how that door is built follows from a single fact:

**The caller is untrusted. Not incompetent — adversarial.**

That distinction matters. A merely buggy caller passes a misaligned
pointer or an off-by-one length, and a kernel that survives "reasonable
mistakes" survives it. An adversarial caller has read your source code,
knows exactly which check you forgot, and constructs the one input in
four billion that slips through it. The system call boundary is where
the kernel meets code written specifically to break the kernel — every
malware author, every privilege-escalation exploit, starts here. So as
you read this lesson, keep asking the attacker's question: *what is the
worst value this register could possibly hold?*

## One door: `int 0x80`

On i386, the classic entry mechanism is a **software interrupt**:

```
	mov eax, 4        ; syscall number: write
	mov ebx, 1        ; arg 1: file descriptor
	mov ecx, buf      ; arg 2: buffer address
	mov edx, 13       ; arg 3: length
	int 0x80          ; knock on the kernel's door
	                  ; ...EAX now holds the result
```

`int 0x80` is the vector Linux made famous, and DuckOS uses it too. It
works exactly like the hardware interrupts from *Interrupts and the
IDT*: the CPU looks up entry 0x80 in the IDT, finds a gate pointing at
kernel code, switches to the kernel stack (from the TSS), pushes the
user's EIP/CS/EFLAGS/ESP, and jumps. One detail makes it usable from
ring 3: recall that every gate has a DPL, and the CPU refuses `int n`
from code less privileged than the gate's DPL. DuckOS sets vector 0x80's
gate to **DPL 3** — ring 3 may *enter* here — while every other vector
stays DPL 0, so a user program cannot fake a disk interrupt or a page
fault by executing `int 14`. The gate's *target* is still a ring 0 code
segment: the caller chooses *when* to enter the kernel, never *where*.
The landing address is the kernel's syscall entry stub, period.

(Historical aside: this path got so hot that Intel and AMD eventually
added dedicated fast entry instructions — `sysenter` and `syscall` —
that skip the IDT lookup entirely. Modern kernels use those; the gate
mechanism is the one that teaches you what's actually happening.)

Notice the calling convention in that assembly. It is the whole ABI:

```
	EAX  in:  syscall number        out:  return value / -errno
	EBX  in:  argument 1
	ECX  in:  argument 2
	EDX  in:  argument 3
```

Why registers, and not the C convention of pushing arguments on the
stack? Because *the user stack belongs to the enemy*. To read arguments
off the user stack, the kernel would have to dereference the user's ESP
— a value the user controls completely. ESP could point at unmapped
memory (kernel page-faults on a read it chose to do), at kernel memory
(the kernel tricked into reading itself), or at memory another thread
is concurrently rewriting. The kernel can't trust the user's stack
pointer even enough to *read* through it without the same validation
we'll build for every other pointer below. Registers sidestep all of
it: when the trap fires, the CPU saves the user's registers into a
trap frame on the *kernel* stack — as we laid out in *Processes: the
Kernel's Bookkeeping* — and the kernel reads the arguments from its
own memory. Three registers isn't many, but most calls need few
arguments, and the ones that need more pass a pointer to a struct —
which then gets validated like any other user pointer.

How many doors does a kernel need? Here the design philosophies split.
A monolithic kernel like Linux gives every service its own syscall —
open, read, fork, mmap, socket... — and the count grows for decades
(Linux passed 300 long ago). Minix, true to the microkernel creed from
*Message Passing — the Microkernel Heart*, needs essentially **two**:
`send` and `receive`. "Read a file" is not a syscall; it is a *message*
to the filesystem server, and the only thing the kernel itself does is
move the message. DuckOS follows Minix in spirit — most of our services
speak IPC — but the trap mechanism underneath is identical either way:
some register says what you want, `int 0x80` gets you across, and the
kernel decides whether you may have it.

The full round trip — user registers in, one gate, one table lookup, and the result (or -errno) back in EAX:

```d2
direction: right

user: "ring 3 (user)" {
  regs: "registers" {
    shape: sql_table
    EAX: "4 = write"
    EBX: "1 (fd)"
    ECX: "buf"
    EDX: "13 (len)"
  }
}

gate: "IDT[0x80]\nDPL 3 gate" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}

kernel: "ring 0 (kernel)" {
  table: "syscall_table" {
    shape: sql_table
    0: sys_exit
    1: sys_fork
    4: sys_write
    7: "NULL"
  }
}

user.regs -> gate: "int 0x80"
gate -> kernel.table.4: "table[nr]"
kernel.table.4 -> user.regs: "EAX = result / -errno" {
  style.stroke-dash: 4
}
```

## The dispatch table

Inside the kernel, the entry stub has a trap frame full of untrusted
registers and must get to the right handler. The classic structure is
an array of function pointers indexed by the syscall number:

```c
	int (*syscall_table[NSYSCALLS])(...);

	handler = syscall_table[nr];   /* nr came from user EAX */
```

In a real kernel this table is a static array the entry stub indexes
with an assembly instruction; here we build it as a plain C struct the
tests can read. Either way, that innocent-looking index is the single
most attacked line of code in the kernel, because `nr` is a raw value
from a user register. Two checks stand between it and disaster, and
both the *order* and the *types* matter:

1. **Range first.** Is `nr` a valid index at all? If not: `-ENOSYS`.
2. **Null second.** Table slots can be empty (syscall numbers get
   reserved, removed, or never implemented). Empty slot: `-ENOSYS`.

Skip the range check and you index outside the table. Do the null check
"first" and you've already read `syscall_table[nr]` out of bounds to do
it — the null check only means anything *after* the range check proved
the slot exists.

And now the type. Suppose the kernel writes the obvious thing:

```c
	int nr = frame->eax;            /* signed int — the bug */
	if (nr > NSYSCALLS - 1)
		return -ENOSYS;
	return syscall_table[nr](...);
```

An attacker loads EAX with `0xFFFFFFFF` and traps. As a signed int,
that's `-1`. Is `-1 > 7`? No — the check passes. The kernel then
evaluates `syscall_table[-1]`: the four bytes *immediately before the
table* in kernel memory, interpreted as a function pointer, called in
ring 0.

```
	0x00107ff8:  <some other kernel variable>
	0x00107ffc:  <...its last 4 bytes>       <- syscall_table[-1]
	0x00108000:  syscall_table[0]  (sys_exit)
	0x00108004:  syscall_table[1]  (sys_fork)
	...
```

What lives before the table is whatever the linker put there — and with
a more negative `nr`, *anywhere* below it. An attacker who can influence
any of that memory (a buffer the kernel copied their data into, a saved
register, a length field) now controls a function pointer the kernel
will jump through with full privileges. Game over — from one signed
comparison. This is not hypothetical: more than one production kernel
has shipped exactly this bug in a trap path, and "negative syscall
number" is a standard line in every kernel-audit checklist because it
keeps coming back.

The fix costs nothing: treat the number as what it actually is, an
unsigned 32-bit register.

```c
	uint32_t nr = frame->eax;       /* unsigned — the fix */
	if (nr >= NSYSCALLS)
		return -ENOSYS;
```

There are no negative unsigned numbers. The attacker's `0xFFFFFFFF` is
now just 4294967295, which fails `>= 8` along with every other bad
value. One comparison, both directions closed. You will implement this
exact check in the challenge.

The gauntlet in order — one unsigned comparison retires the negative-number exploit before the table is ever touched:

```d2
direction: right

c1: "nr <\nNSYSCALLS ?"
c2: "table[nr]\n!= NULL ?"
c3: "user_range_ok\n(addr, len) ?"

run: "handler runs" {
  style.stroke: "#16a34a"
  style.stroke-width: 3
}

e1: "-ENOSYS" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
e2: "-ENOSYS" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}
e3: "-EFAULT" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

c1 -> c2: yes
c2 -> c3: yes
c3 -> run: yes
c1 -> e1: no
c2 -> e2: no
c3 -> e3: no
```

## Pointers from the other side

Range-checking a syscall number is the warm-up. The real minefield is
**arguments that are pointers**. Consider `write(fd, buf, len)`: the
kernel must read `len` bytes from `buf`. But once we're in ring 0, the
CPU protection that stopped ring 3 from touching kernel memory is *off*
— the kernel can read and write everything. If it dereferences `buf`
without looking at it, the kernel becomes a **confused deputy**: a
privileged agent tricked into using its own authority on the attacker's
behalf.

Concretely: pass a *kernel* address as the buffer to `write()`, and the
obedient kernel copies kernel memory out through the file descriptor —
the attacker just read arbitrary kernel memory (passwords, keys, other
processes' data) using the kernel's own hands. Pass a kernel address to
`read()`, and the kernel copies attacker-chosen bytes from a file *over*
kernel memory — even worse. Same bug, both directions: the kernel did
exactly what it was told, for someone who had no right to ask.

So the iron rule: **the kernel never dereferences a user-supplied
address without first proving the entire range lies in user space.** In
DuckOS's address space layout, user space is:

```
	0x00000000 ─┬─ (below USER_BASE: NULL page + reserved — reject)
	0x08048000 ─┤  USER_BASE: classic i386 ELF load address
	            │      ... user code, data, heap, stack ...
	0xC0000000 ─┤  USER_TOP: kernel owns everything from here up
	0xFFFFFFFF ─┘
```

(The odd-looking `0x08048000` is a real piece of i386 archaeology —
it's where the System V ABI told ELF executables to load, leaving the
low 128 MB clear; the region below it also swallows NULL and small
integers misused as pointers. `0xC0000000` is the classic 3 GB/1 GB
user/kernel split that Linux used for decades.)

The check looks trivial: `[addr, addr + len)` must fit inside
`[USER_BASE, USER_TOP)`. Here is the version everyone writes first:

```c
	if (addr >= USER_BASE && addr + len <= USER_TOP)
		/* looks safe... */
```

It has a hole an adversary can drive a truck through: `addr + len` is
32-bit arithmetic, and 32-bit arithmetic **wraps**. Work the attack in
hex. The attacker asks the kernel to write from `addr = 0xFFFFFFF0`,
`len = 0x20` — a 32-byte range starting 16 bytes below the top of
kernel memory:

```
	addr + len = 0xFFFFFFF0 + 0x00000020
	           = 0x1_0000_0010        (33 bits...)
	           = 0x00000010           (...but uint32_t keeps 32)

	check 1:  0xFFFFFFF0 >= 0x08048000  -> true   (it's huge)
	check 2:  0x00000010 <= 0xC0000000  -> true   (it wrapped)

	verdict: "valid user range"  — for 32 bytes of kernel memory
```

Both comparisons pass, and the kernel proceeds to copy through the top
of its own address space. This is a genuinely adversarial off-by-one:
no normal program ever computes `addr + len` anywhere near the wrap, so
the bug sits invisible through years of testing until someone hostile
aims for exactly the values that trigger it.

The fix is to never compute `addr + len` at all. Validate the *start*,
then compare the *length* against the room that remains — subtraction
instead of addition:

```c
	if (addr < USER_BASE || addr >= USER_TOP)
		return 0;                      /* start not in user space */
	return len <= USER_TOP - addr;         /* room left above addr */
```

Once the first line proves `addr < USER_TOP`, the value
`USER_TOP - addr` is exact — a subtraction of a smaller unsigned number
from a bigger one can't wrap. For the attack above, `addr` already
fails `addr < USER_TOP`; and for a wrap staged *inside* user space
(say `addr = 0xBFFFFFF0, len = 0x50000000`), the room left is `0x10`
and `0x50000000 <= 0x10` is false. Every path closed, still just two
comparisons. Real kernels wrap this logic in a helper with a name like
`access_ok()` and *culturally enforce* that no user pointer is ever
touched except through it — the check is easy; remembering to apply it
on all several hundred syscalls is the hard part.

## Returning failure: the `-errno` convention

One register, EAX, carries the result back to ring 3 — and a syscall
can fail. Where does the error go? There is no second return register
in this ABI, so results and errors must *share* EAX, which means some
bit pattern has to be sacrificed to mean "error." The Unix convention:
**errors are small negative numbers**. Success returns a non-negative
result; failure returns `-errno` — `-EFAULT` is `-14`, `-ENOSYS` is
`-38`. That's why DuckOS handlers return `int` and why you've seen
`return -EINVAL;` scattered through every lesson since *Owning Physical
Memory*: it's the kernel's native error idiom, and the syscall boundary
is where it comes from.

User programs never see `-14`, because libc translates at the boundary.
Every syscall wrapper does this dance:

```c
	int ret = do_syscall(...);           /* raw EAX comes back */
	if (ret < 0 && ret >= -4095) {
		errno = -ret;                /* stash the code */
		return -1;                   /* the POSIX failure value */
	}
	return ret;
```

This is the real reason `errno` exists as a variable at all: the raw
kernel interface returns one value, POSIX promised programmers two
("-1 means failure, `errno` says why"), and a global variable is where
libc parks the second one. `errno` isn't something the kernel sets —
it's a user-space fiction maintained by the wrapper.

The convention has a wart worth knowing. If `-1` through `-4095` mean
"error," then a syscall can never legitimately *return* a value in that
range as data. For `read()` that's fine — byte counts aren't negative.
But `mmap()` returns an address, and addresses are just numbers: the
kernel must guarantee it never hands out a mapping in the top 4095
bytes of the address space, or a valid pointer would be mistaken for an
error code. That's why `MAP_FAILED` is defined as `(void *)-1` and why
the last page below `0xFFFFFFFF` is permanently unmappable on such
ABIs. When one register moonlights as two channels, some values get
caught moonlighting as both.

## Challenge: Dispatch {#syscall-dispatch points=20}

Build DuckOS's syscall layer: the range check that guards user
pointers, the dispatch table, and one sample handler that uses the
check. In a real kernel the entry stub reaches this code straight from
the `int 0x80` gate; here the tests play the part of the trap frame,
handing you the raw register values.

Implement, in the starter below:

- `user_range_ok(addr, len)` — return 1 iff every byte of
  `[addr, addr + len)` lies inside `[USER_BASE, USER_TOP)`. `len == 0`
  is acceptable when `addr` itself is a user address. Reject
  `addr < USER_BASE`, reject any span that reaches `USER_TOP`, and be
  **wrap-safe**: `addr = 0xFFFFFFF0, len = 0x20` must be rejected, so
  check the start first, then `len <= USER_TOP - addr` — never compute
  `addr + len` (the lesson shows why the addition form fails).
- `st_init(t)` — every slot NULL.
- `st_register(t, nr, fn)` — `nr >= NSYSCALLS` is a kernel bug:
  `-EINVAL`. Otherwise install `fn` (which may be NULL, to remove a
  handler) and return 0.
- `syscall_dispatch(k, t, nr, a1, a2, a3)` — `nr >= NSYSCALLS` returns
  `-ENOSYS`; note `nr` is `uint32_t`, so a "negative" EAX from user
  space is a huge unsigned value and this single unsigned comparison
  kills the classic negative-number exploit. An empty (NULL) slot also
  returns `-ENOSYS`. Otherwise call the handler and return its result.
- `sys_klog(k, addr, len, unused)` — a sample handler: "log `len` bytes
  from user buffer `addr`." Validate with `user_range_ok(addr, len)`
  and return `-EFAULT` on failure *before touching anything*. The copy
  itself is simulated: on success append one `'W'` to `k->log` (only if
  there's room) and return `(int)len`.

The tests register a handler that encodes its three arguments into its
return value (to prove arguments route through unchanged), then attack:
`nr == NSYSCALLS`, `nr == 0xFFFFFFFF` (the "-1 syscall"), an empty
slot, a removed handler, a kernel address, an address below
`USER_BASE`, a span crossing `USER_TOP`, and the wraparound range from
the lesson. They also verify `sys_klog` refuses a bad pointer with
`-EFAULT` and leaves the log untouched.

### Starter

```c
#include <stdint.h>

#define NSYSCALLS 8
#define ENOSYS 38
#define EFAULT 14
#define EINVAL 22

#define USER_BASE 0x08048000u	/* classic i386 ELF load address */
#define USER_TOP  0xC0000000u	/* kernel owns the top gigabyte */

/*
 * The kernel state a handler can touch. Real handlers see much more;
 * here it is just an append-only log the tests inspect.
 */
struct kernel {
	char log[64];
	int log_len;
};

/*
 * A system call handler. Arguments arrive as three raw 32-bit values
 * — exactly what the trap frame captured from EBX/ECX/EDX. Whether an
 * argument is a length, a file descriptor, or a pointer is the
 * handler's problem, and the handler must assume every one of them is
 * a lie. Returns a non-negative result, or -errno.
 */
typedef int (*syscall_fn)(struct kernel *k, uint32_t a1, uint32_t a2, uint32_t a3);

struct syscall_table {
	syscall_fn fn[NSYSCALLS];
};

/*
 * Return 1 iff the whole range [addr, addr+len) lies in user space,
 * [USER_BASE, USER_TOP); 0 otherwise. len == 0 is fine when addr is
 * itself a user address. Must be wrap-safe: validate addr first, then
 * compare len against the room remaining (USER_TOP - addr). Never
 * compute addr + len.
 */
int user_range_ok(uint32_t addr, uint32_t len)
{
	/* TODO: reject addr outside [USER_BASE, USER_TOP), then
	   check len <= USER_TOP - addr */
	(void)addr;
	(void)len;
	return 0;
}

/* Empty the table: every slot NULL. */
void st_init(struct syscall_table *t)
{
	/* TODO */
	(void)t;
}

/*
 * Install fn as the handler for syscall nr (fn == NULL removes the
 * handler). Returns 0, or -EINVAL if nr is out of range —
 * registration is a kernel-side act, so a bad nr is a kernel bug:
 * EINVAL, not ENOSYS.
 */
int st_register(struct syscall_table *t, uint32_t nr, syscall_fn fn)
{
	/* TODO: bounds-check nr, then install */
	(void)t;
	(void)nr;
	(void)fn;
	return -1;
}

/*
 * The kernel side of `int 0x80`: nr arrived in EAX, a1..a3 in
 * EBX/ECX/EDX. Unknown or unimplemented syscall -> -ENOSYS. nr is
 * uint32_t on purpose: a "negative" number from user space is a huge
 * unsigned value, and one unsigned range check rejects it.
 */
int syscall_dispatch(struct kernel *k, const struct syscall_table *t,
                     uint32_t nr, uint32_t a1, uint32_t a2, uint32_t a3)
{
	/* TODO: range-check nr (unsigned), null-check the slot, call it */
	(void)k;
	(void)t;
	(void)nr;
	(void)a1;
	(void)a2;
	(void)a3;
	return -1;
}

/*
 * Sample handler: "write len bytes from user buffer addr to the
 * kernel log." The copy is simulated — on success append one 'W' to
 * k->log (only if log_len < sizeof(k->log)) and return (int)len. The
 * part that is NOT simulated is the guard: reject a bad user range
 * with -EFAULT before touching anything.
 */
int sys_klog(struct kernel *k, uint32_t addr, uint32_t len, uint32_t unused)
{
	/* TODO: user_range_ok or -EFAULT; append 'W'; return (int)len */
	(void)k;
	(void)addr;
	(void)len;
	(void)unused;
	return -1;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define NSYSCALLS 8
#define ENOSYS 38
#define EFAULT 14
#define EINVAL 22

#define USER_BASE 0x08048000u
#define USER_TOP  0xC0000000u

struct kernel {
	char log[64];
	int log_len;
};

typedef int (*syscall_fn)(struct kernel *k, uint32_t a1, uint32_t a2, uint32_t a3);

struct syscall_table {
	syscall_fn fn[NSYSCALLS];
};

int user_range_ok(uint32_t addr, uint32_t len);
void st_init(struct syscall_table *t);
int st_register(struct syscall_table *t, uint32_t nr, syscall_fn fn);
int syscall_dispatch(struct kernel *k, const struct syscall_table *t,
                     uint32_t nr, uint32_t a1, uint32_t a2, uint32_t a3);
int sys_klog(struct kernel *k, uint32_t addr, uint32_t len, uint32_t unused);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Encodes its arguments so tests can see they routed through intact. */
static int encode_args(struct kernel *k, uint32_t a1, uint32_t a2, uint32_t a3)
{
	(void)k;
	return (int)(a1 + a2 * 10u + a3 * 100u);
}

int main(void)
{
	struct kernel k = {{0}, 0};
	struct syscall_table t;

	st_init(&t);

	check(st_register(&t, 3, encode_args) == 0 &&
	      syscall_dispatch(&k, &t, 3, 1, 2, 3) == 321,
	      "test_dispatch_routes_args");
	check(syscall_dispatch(&k, &t, 5, 0, 0, 0) == -ENOSYS,
	      "test_unregistered_slot");
	check(syscall_dispatch(&k, &t, NSYSCALLS, 0, 0, 0) == -ENOSYS,
	      "test_nr_equal_to_table_size");
	check(syscall_dispatch(&k, &t, 0xFFFFFFFFu, 1, 2, 3) == -ENOSYS,
	      "test_negative_nr");
	check(st_register(&t, NSYSCALLS, encode_args) == -EINVAL,
	      "test_register_out_of_range");
	check(st_register(&t, 3, NULL) == 0 &&
	      syscall_dispatch(&k, &t, 3, 1, 2, 3) == -ENOSYS,
	      "test_unregister_then_dispatch");

	check(user_range_ok(0x08050000u, 4096) == 1,
	      "test_user_buffer_ok");
	check(user_range_ok(0xC0000000u, 4) == 0,
	      "test_reject_kernel_addr");
	check(user_range_ok(0x1000u, 16) == 0,
	      "test_reject_below_user_base");
	check(user_range_ok(USER_TOP - 4, 8) == 0,
	      "test_reject_span_crossing_top");
	check(user_range_ok(0xFFFFFFF0u, 0x20) == 0,
	      "test_wraparound");
	check(user_range_ok(USER_BASE, 0) == 1,
	      "test_len_zero_at_base");

	check(sys_klog(&k, 0xC0000000u, 8, 0) == -EFAULT && k.log_len == 0,
	      "test_klog_bad_pointer_efault");
	check(sys_klog(&k, 0x08048000u, 5, 0) == 5 &&
	      k.log_len == 1 && k.log[0] == 'W',
	      "test_klog_good_pointer_logs");

	return failed;
}
```

# Lesson: Birth, Death, and Zombies {#process-lifecycle}

Unix process life is four verbs: `fork` copies a process, `exec` replaces
its program, `exit` ends it, and `wait` collects the result. That's the
whole API. There is no `spawn_with_47_options` call; every shell, daemon,
and pipeline in 1991 — and today — is built by composing those four. It is
one of the great economies of design in systems history, and by this point
in the course you've already met half of it: fork's real work is memory
work — duplicating an address space, page table by page table — and we
built that machinery in *Paging and Virtual Memory*. What fork copies,
this lesson kills and buries.

One honest gap before we start: DuckOS has no `exec`. exec's job is to be
a loader — open an executable file, parse its header, map its segments
into a fresh address space, and jump to its entry point. We have the
filesystem pieces (*Inodes: Files Without Names*, *Directories and
Path Walking*) and the paging pieces, but wiring them into a binary loader is a project of its
own, and our simulated processes don't run real machine code anyway. So
DuckOS processes are born by `fork` (well — by a test-scaffolding `spawn`)
and are forever stuck running "the program they were born with." Real
Minix, of course, had exec from day one; it's how the shell runs anything.

What we build instead, completely, is the *end* of the story: `exit` and
`wait`, and the strange undead state between them.

## Why exit() can't just clean up

Naively, exit should be simple: the process is done, so free everything —
pages, kernel stack, file handles, and the `struct proc` slot itself. Set
the slot back to `PROC_UNUSED` and move on.

But exit takes an argument. `exit(0)` means "I succeeded"; `exit(1)` means
"I failed." That status is a message *to the parent*, and here's the
problem: at the moment a child exits, the parent may not be listening. It
might be busy. It might not call `wait` for another ten seconds, or ten
minutes. The status has to be stored *somewhere* until the parent comes
asking — and every other scrap of the dead process is being freed.

The answer Unix chose: the one thing exit does **not** free is the process
table slot. The pages go, the stacks go, but the `struct proc` stays,
holding exactly three things anyone will ever ask about a dead process:

```
            struct proc, after exit(42):
            +----------------------------------+
            | pid          7                   |   who died
            | exit_status  0x2A00              |   how it went
            | parent       slot 1              |   who to tell
            | state        PROC_ZOMBIE         |
            +----------------------------------+
            everything else about the process: freed
```

A corpse with a toe tag. This state is called a **zombie** — dead, but
still occupying a slot — and it is worth saying loudly: *a zombie is not a
bug*. A zombie is a mailbox. It exists precisely so that the answer to
"how did my child do?" survives the child. Every process that ever exits
becomes a zombie, if only for the microseconds until its parent reaps it.

The *bug* is a zombie **leak**: a parent that forks children and never
waits for them. Each dead child parks in the table forever. Run `ps` on
such a system and you'll see the classic symptom — a column of processes
marked `<defunct>`, unkillable (they're already dead; there is nothing
left to kill), each pinning a table slot. On DuckOS the table is
Minix-sized — `NPROC` is 16 — so a leaky parent exhausts the whole system
after a dozen forks and the kernel can never create a process again.
Production systems have met this exact death at larger scale; every
sysadmin eventually learns that `<defunct>` means "go find the parent that
forgot to wait," because the only ways a zombie leaves the table are its
parent reaping it or its parent dying.

## The status word

Look at that `exit_status` above: the process called `exit(42)`, and the
table stores `0x2A00` — 42 shifted up a byte. That's not a DuckOS quirk;
it's the traditional Unix **status word** encoding, and it's shaped by a
question exit doesn't answer: *how* did the process die? A process can end
two ways — voluntarily (it called exit) or violently (the kernel killed it
with a signal: segfault, SIGKILL, ^C). wait reports both through one
16-bit word by splitting it:

```
             15             8 7              0
            +----------------+----------------+
status word |  exit status   |  term. signal  |
            +----------------+----------------+
             high byte:        low byte:
             what exit(n)      which signal killed it
             said              (0 = it wasn't killed)
```

A normal `exit(status)` encodes as:

```c
(status & 0xff) << 8
```

For `exit(42)`: 42 is `0x2A`, shift it up a byte, and the word is
`0x2A00`. The low byte is left `0x00` — the reserved "no signal killed me"
marker. DuckOS has no signals (another honest gap — we never built signal
delivery), so our low byte is always zero, but we keep the encoding
anyway, because the *shape* of the word is the contract, and the shape is
why the C library gives you macros instead of raw bits:

```c
WIFEXITED(w)     /* low byte == 0: it exited normally   */
WEXITSTATUS(w)   /* (w >> 8) & 0xff: undo the shift     */
WTERMSIG(w)      /* low 7 bits: the signal, if killed   */
```

`WEXITSTATUS` exists to hide the shift. Code that writes `status >> 8` by
hand works right up until it runs on a system that encodes differently —
and such systems existed, which is why POSIX standardized the macros, not
the layout.

The `& 0xff` mask hides a trap worth knowing. Only one byte of the exit
status survives. `exit(300)`? 300 is `0x12C`; masked to `0x2C`, that's 44
— your parent sees `exit(44)`. And the all-time classic: `exit(256)`
masks to **0**. A program can fail and report *success* because its exit
code happened to be a multiple of 256. Shell scripters meet the same wall
as `exit -1` mysteriously becoming 255. One byte. That's the pipe.

## Orphans, and why init exists

The zombie protocol assumes the parent will eventually call wait. But
parents die too — sometimes *before* their children. Now what? The child's
`parent` field points at a corpse (or worse, at a slot that's been freed
and reused by an unrelated process). When this child eventually exits,
who reaps *it*?

Unix's answer is adoption. When a process dies, the kernel walks the table
and **reparents** every one of its children to **init**, the process with
pid 1. This is the moment init stops being trivia ("the first process")
and becomes structural: init is the system's designated next-of-kin. Its
core job — in V7 Unix, in Minix, in every descendant — is to sit in a
loop calling wait, forever, reaping whatever orphans history dumps on it.
That loop is why orphaned processes don't leak: no matter how deep the
family tree, the root of it always waits. (It's also why the kernel treats
pid 1 as sacred — Linux panics with `Attempted to kill init!` — because a
system whose next-of-kin has died can never bury anyone again.)

Daemons exploit reparenting on purpose. The classic "double fork" trick —
fork, fork again, let the middle process exit — deliberately orphans the
grandchild so init adopts it, and the daemon runs on with no controlling
parent to answer to. Reparenting isn't just a cleanup path; it's a tool.

One subtlety that our challenge tests will hunt for: the dying parent may
leave behind children that are *already zombies* — kids that exited and
were never reaped. Those corpses get reparented too, and init must be
able to reap them **immediately**. If init is blocked in wait at that
moment, handing it a corpse and not waking it up means the corpse sits
there until some unrelated event happens to wake init — possibly forever.
Inheriting a zombie is a wait-worthy event.

The whole surgery in one picture — solid arrows are the parent-of links
while A lives; the red dashed arrows are where those links are rewritten
the moment A dies; the grey dashed box is a child that is already a corpse:

```d2
direction: down

init: "init\npid 1"

A: "A — dying" {
  style.stroke: "#dc2626"
  style.stroke-width: 3
}

B: "B (alive)"

Z: "Z (zombie)" {
  style.stroke-dash: 4
  style.font-color: "#9ca3af"
}

init -> A
A -> B
A -> Z

init -> B: {
  style.stroke: "#dc2626"
  style.stroke-dash: 4
}
init -> Z: "wake init if waiting" {
  style.stroke: "#dc2626"
  style.stroke-dash: 4
}
```

## wait: a three-way fork in the road

Everything above meets in `wait`. When a process calls it, exactly one of
three situations holds, and the behavior is a small state machine:

```
what the table shows            what wait does
------------------------------  ---------------------------------------
some child is a ZOMBIE          reap it NOW: copy out its status,
                                free its slot, return its pid
children exist, all alive       block: mark the caller SLEEPING and
                                waiting, return; a child's exit will
                                wake it to try again
no children at all              error: -ECHILD — blocking would be
                                sleeping for a wakeup that can't come
```

The third row matters more than it looks. If wait blocked when you have
no children, nothing in the universe could ever wake you — no child means
no future exit pointed at you. Returning `-ECHILD` instead is also what
terminates init's reap loop... never, by design, since init always has
children on a running system; but it's what makes `while (wait(&st) >= 0)`
a correct idiom everywhere else.

### The handshake, and the bug that haunts it

Blocking creates the coordination problem: exit and wait are a handshake
across two processes. The dying child must check "is my parent blocked in
wait right now?" and if so, wake it. In DuckOS terms: set the parent back
to `PROC_RUNNABLE` and clear its `waiting` flag.

Here is the classic bug, and it's a genuine war story from every kernel
that ever got this wrong. Suppose exit does its steps in this order:

```
1. wake my parent (if waiting)
2. mark myself ZOMBIE
```

The woken parent runs, re-scans its children... and finds no zombie —
step 2 hasn't happened yet. (On a single CPU with no preemption inside
the kernel this can't interleave, but add interrupts or a second CPU and
it will, about once a week, in production, at night.) So the parent does
the only sane thing: goes back to sleep. *Then* the child marks itself
zombie — but its one wakeup has already been spent. The parent sleeps
forever next to a corpse it will never see. This is the **lost wakeup**,
and the fix is pure ordering: **become a zombie first, then wake the
parent.** Once the corpse is on the table before the wakeup fires, a
woken parent's re-scan is guaranteed to find it. Our tests plant this
scenario; the ordering inside your `k_exit` is load-bearing.

The two orderings side by side — same four events, and the only
difference is which one the child does first (the red box is the step
that arrives after its wakeup has already been spent):

```d2
grid-columns: 2

wrong: "the lost wakeup" {
  w1: "1  child wakes parent"
  w2: "2  parent re-scans:\nno zombie yet"
  w3: "3  parent sleeps again"
  w4: "4  child marks ZOMBIE —\nwakeup already spent" {
    style.stroke: "#dc2626"
    style.stroke-width: 3
  }
  w1 -> w2 -> w3 -> w4
}

right: "zombie first" {
  r1: "1  child marks ZOMBIE"
  r2: "2  child wakes parent"
  r3: "3  parent re-scans:\nfinds the corpse"
  r4: "4  reap: copy status,\nfree the slot"
  r1 -> r2 -> r3 -> r4
}
```

Notice also *what the wakeup carries*: nothing. exit doesn't hand its
status directly to the sleeping parent — it just marks the corpse and
makes the parent runnable, and the parent **re-calls wait and re-scans
the table**. We could do a direct handoff (exit writes its pid and status
straight into the waiter and completes the wait on its behalf), and some
kernels do. But re-scan wins on simplicity: there is exactly one piece of
code that reaps — the scan loop in wait — instead of two that must agree
forever. And it's robust by construction: a spurious wakeup, or two
children exiting at once, or a corpse inherited from a dying relative all
collapse into the same behavior, "wake up, look again." If you've met
condition variables, this is Mesa semantics — the wakeup is a hint, the
re-check is the truth — and it's the discipline we already leaned on in
*Sharing Without Tearing*.

In a real kernel these transitions happen on live hardware state; here,
as always in DuckOS, the process table is a plain C struct in a buffer,
so the tests can plant a family tree, kill somebody, and inspect exactly
who got woken, who got adopted, and who got reaped. The final challenge —
*DuckOS, Assembled* — drives this same machinery together with the
multilevel scheduler and the rendezvous IPC from *Message Passing — the
Microkernel Heart*: exit wakes waiters, the scheduler picks who runs
next, and wait reaps. This
lesson is the last organ of that body.

## Challenge: exit and wait {#wait-exit points=25}

Implement process death and reaping for the DuckOS process table:
`pt_find_pid`, `k_exit`, and `k_wait`. The starter provides `pt_init`
(clears the table to `PROC_UNUSED`) and `pt_spawn` (test scaffolding that
plants a `PROC_RUNNABLE` process in a slot); the tests use those to build
family trees. Slots are identified by index; the `parent` field holds the
parent's *slot index*, or `-1` for init.

**`int pt_find_pid(const struct proc_table *pt, int pid)`** — return the
slot index of the live process with this pid, or `-1` if none. "Live"
means any state except `PROC_UNUSED` (zombies still own their pid — that
is the whole point of a toe tag). Unused slots must be ignored even if a
stale `pid` field happens to match.

**`int k_exit(struct proc_table *pt, int slot, int status)`** — terminate
the process in `slot`:

- If `slot` is out of range, `PROC_UNUSED`, or already `PROC_ZOMBIE`,
  return `-ESRCH` (you can't die twice).
- Encode the status word: `exit_status = (status & 0xff) << 8`. Set
  `state = PROC_ZOMBIE` and `waiting = 0`. Do this **before** any wakeup
  (remember the lost-wakeup ordering).
- Reparent every child (every non-UNUSED proc whose `parent == slot`) to
  init's **slot** — find it with `pt_find_pid(pt, INIT_PID)`. The tests
  always plant init, but handle its absence by parking children on
  `parent = -1` rather than reading a bogus slot.
- If the dying process's own parent is blocked in wait (`waiting == 1`),
  wake it: `state = PROC_RUNNABLE`, `waiting = 0`. (A parent of `-1`
  means no one to wake.)
- If any of the reparented children were already `PROC_ZOMBIE`, init just
  inherited a corpse: if init is blocked in wait, wake init the same way.
- Return 0.

**`int k_wait(struct proc_table *pt, int slot, int *status)`** — the
three-outcome state machine, for the caller in `slot`:

- If `slot` is out of range, or the caller is `PROC_UNUSED` or
  `PROC_ZOMBIE`, return `-EINVAL` (the dead don't wait). `status` is
  never NULL.
- Scan the table for children (`parent == slot`, state not UNUSED). If
  any child is `PROC_ZOMBIE`, reap the one in the **lowest slot index**:
  store its `exit_status` through `status`, free the slot
  (`state = PROC_UNUSED`, `pid = 0`), and return the child's former pid.
- If children exist but none are zombies, block: `waiting = 1`,
  `state = PROC_SLEEPING`, return 0. (The woken caller re-calls `k_wait`
  — the tests do this for you.)
- No children at all: return `-ECHILD`.

The tests plant init (pid `INIT_PID` in slot 0, parent `-1`) and build
trees on top: they check the `0x2A00` encoding and the `& 0xff` masking,
that zombies keep pid and slot, blocking and wakeup across an exit, both
reparenting flavors (live orphans and inherited corpses — including
waking a blocked init), `-ECHILD` before and after the last reap,
`-ESRCH` on double exit, and lowest-slot-first reap order.

### Starter

```c
#define NPROC 16
#define ECHILD 10
#define ESRCH 3
#define EINVAL 22
#define INIT_PID 1

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;
	enum proc_state state;
	int parent;		/* slot index of parent; -1 for init */
	int exit_status;	/* valid once ZOMBIE */
	int waiting;		/* 1 = blocked in k_wait (state SLEEPING) */
};

struct proc_table {
	struct proc procs[NPROC];
};

/* Clear every slot to PROC_UNUSED. (Provided.) */
void pt_init(struct proc_table *pt)
{
	for (int i = 0; i < NPROC; i++) {
		pt->procs[i].pid = 0;
		pt->procs[i].state = PROC_UNUSED;
		pt->procs[i].parent = -1;
		pt->procs[i].exit_status = 0;
		pt->procs[i].waiting = 0;
	}
}

/*
 * Test scaffolding: place a RUNNABLE process in a slot. (Provided.)
 * parent_slot is a slot index, or -1 for init itself.
 */
void pt_spawn(struct proc_table *pt, int slot, int pid, int parent_slot)
{
	pt->procs[slot].pid = pid;
	pt->procs[slot].state = PROC_RUNNABLE;
	pt->procs[slot].parent = parent_slot;
	pt->procs[slot].exit_status = 0;
	pt->procs[slot].waiting = 0;
}

/*
 * Slot index of the live (non-UNUSED) process with this pid, or -1.
 * Zombies are live for this purpose: they still own their pid.
 */
int pt_find_pid(const struct proc_table *pt, int pid)
{
	/* TODO: scan; skip UNUSED slots even if their pid field matches */
	(void)pt;
	(void)pid;
	return -1;
}

/*
 * Terminate the process in `slot` with the given exit status.
 * Returns 0, or -ESRCH if the slot is invalid, UNUSED, or already ZOMBIE.
 */
int k_exit(struct proc_table *pt, int slot, int status)
{
	/*
	 * TODO:
	 *  - validate; -ESRCH for out-of-range / UNUSED / ZOMBIE
	 *  - exit_status = (status & 0xff) << 8; ZOMBIE; waiting = 0
	 *    (mark the corpse BEFORE any wakeup — lost-wakeup ordering)
	 *  - reparent children to init's slot (pt_find_pid(pt, INIT_PID);
	 *    park them on -1 if init is missing)
	 *  - wake my parent if it is waiting (RUNNABLE, waiting = 0)
	 *  - if init inherited a ZOMBIE child and init is waiting, wake init
	 */
	(void)pt;
	(void)slot;
	(void)status;
	return -1;
}

/*
 * Reap a dead child of the caller in `slot`, block for one, or fail.
 * Zombie child: reap the lowest slot; *status = its exit_status; free
 * the slot (UNUSED, pid 0); return the child's former pid. Live children
 * only: waiting = 1, SLEEPING, return 0. No children: -ECHILD. Invalid
 * or dead caller: -EINVAL.
 */
int k_wait(struct proc_table *pt, int slot, int *status)
{
	/* TODO: the three-outcome scan */
	(void)pt;
	(void)slot;
	(void)status;
	return -1;
}
```

### Tests

```c
#include <stdio.h>

#define NPROC 16
#define ECHILD 10
#define ESRCH 3
#define EINVAL 22
#define INIT_PID 1

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;
	enum proc_state state;
	int parent;		/* slot index of parent; -1 for init */
	int exit_status;	/* valid once ZOMBIE */
	int waiting;		/* 1 = blocked in k_wait (state SLEEPING) */
};

struct proc_table {
	struct proc procs[NPROC];
};

void pt_init(struct proc_table *pt);
void pt_spawn(struct proc_table *pt, int slot, int pid, int parent_slot);
int pt_find_pid(const struct proc_table *pt, int pid);
int k_exit(struct proc_table *pt, int slot, int status);
int k_wait(struct proc_table *pt, int slot, int *status);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Fresh table with init (pid 1) planted in slot 0. */
static void setup(struct proc_table *pt)
{
	pt_init(pt);
	pt_spawn(pt, 0, INIT_PID, -1);
}

int main(void)
{
	struct proc_table pt;
	int st, r;

	/* --- pt_find_pid --- */
	setup(&pt);
	pt_spawn(&pt, 3, 42, 0);
	check(pt_find_pid(&pt, 42) == 3, "test_find_pid_hit");
	check(pt_find_pid(&pt, 99) == -1, "test_find_pid_miss");

	pt_init(&pt);		/* all UNUSED; every pid field is 0 */
	check(pt_find_pid(&pt, 0) == -1, "test_find_pid_ignores_unused");

	setup(&pt);
	pt_spawn(&pt, 2, 7, 0);
	k_exit(&pt, 2, 0);
	check(pt_find_pid(&pt, 7) == 2, "test_find_pid_sees_zombie");

	/* --- exit: encoding and the zombie state --- */
	setup(&pt);
	pt_spawn(&pt, 1, 7, 0);
	r = k_exit(&pt, 1, 42);
	check(r == 0 && pt.procs[1].exit_status == 0x2A00,
	      "test_status_encoding");
	check(pt.procs[1].state == PROC_ZOMBIE && pt.procs[1].pid == 7,
	      "test_zombie_keeps_pid_and_slot");

	setup(&pt);
	pt_spawn(&pt, 1, 7, 0);
	k_exit(&pt, 1, 300);	/* 300 & 0xff = 44 = 0x2C */
	check(pt.procs[1].exit_status == 0x2C00, "test_status_masked");

	setup(&pt);
	pt_spawn(&pt, 1, 7, 0);
	k_exit(&pt, 1, 0);
	check(k_exit(&pt, 1, 0) == -ESRCH, "test_double_exit_esrch");
	check(k_exit(&pt, 5, 0) == -ESRCH && k_exit(&pt, -1, 0) == -ESRCH &&
	      k_exit(&pt, NPROC, 0) == -ESRCH, "test_exit_bad_slot_esrch");

	/* --- wait: the three outcomes --- */
	setup(&pt);
	pt_spawn(&pt, 1, 7, 0);
	k_exit(&pt, 1, 42);
	st = -1;
	r = k_wait(&pt, 0, &st);
	check(r == 7 && st == 0x2A00, "test_wait_reaps_zombie");
	check(pt.procs[1].state == PROC_UNUSED && pt.procs[1].pid == 0,
	      "test_reap_frees_slot");
	st = -1;
	check(k_wait(&pt, 0, &st) == -ECHILD, "test_wait_after_reap_echild");

	setup(&pt);
	pt_spawn(&pt, 1, 7, 0);
	st = -1;
	r = k_wait(&pt, 0, &st);
	check(r == 0 && pt.procs[0].waiting == 1 &&
	      pt.procs[0].state == PROC_SLEEPING,
	      "test_wait_blocks_on_live_children");

	/* child exits while the parent is blocked: parent must wake */
	r = k_exit(&pt, 1, 5);
	check(r == 0 && pt.procs[0].state == PROC_RUNNABLE &&
	      pt.procs[0].waiting == 0, "test_exit_wakes_blocked_parent");
	st = -1;
	r = k_wait(&pt, 0, &st);	/* the woken parent re-scans */
	check(r == 7 && st == 0x0500, "test_woken_parent_reaps");

	setup(&pt);
	st = -1;
	check(k_wait(&pt, 0, &st) == -ECHILD, "test_wait_no_children_echild");

	setup(&pt);
	st = -1;
	check(k_wait(&pt, 3, &st) == -EINVAL &&
	      k_wait(&pt, -1, &st) == -EINVAL, "test_wait_invalid_slot");

	/* --- orphans and reparenting --- */
	setup(&pt);
	pt_spawn(&pt, 1, 7, 0);		/* A, child of init   */
	pt_spawn(&pt, 2, 9, 1);		/* B, child of A      */
	k_exit(&pt, 1, 0);		/* A dies; B orphaned */
	check(pt.procs[2].parent == 0, "test_orphan_reparented_to_init");

	/* B is already a corpse when A dies: init inherits a zombie */
	setup(&pt);
	pt_spawn(&pt, 1, 7, 0);
	pt_spawn(&pt, 2, 9, 1);
	k_exit(&pt, 2, 3);		/* B dies unreaped    */
	k_exit(&pt, 1, 0);		/* A dies             */
	check(pt.procs[2].parent == 0 && pt.procs[2].state == PROC_ZOMBIE,
	      "test_orphaned_zombie_reparented");
	st = -1;
	r = k_wait(&pt, 0, &st);	/* lowest slot first: A in slot 1 */
	check(r == 7, "test_init_reaps_own_child_first");
	st = -1;
	r = k_wait(&pt, 0, &st);	/* then the inherited corpse B */
	check(r == 9 && st == 0x0300, "test_init_reaps_inherited_corpse");

	/*
	 * The deep case: init is BLOCKED in wait when it inherits a corpse.
	 * Chain: init -> P -> A -> B. B dies (zombie). init waits: its only
	 * child P is alive, so init sleeps. A dies: A's parent is P (not
	 * waiting), but A's zombie child B lands on init — init must wake.
	 */
	setup(&pt);
	pt_spawn(&pt, 1, 4, 0);		/* P */
	pt_spawn(&pt, 2, 7, 1);		/* A */
	pt_spawn(&pt, 3, 9, 2);		/* B */
	k_exit(&pt, 3, 5);		/* B is a corpse on A's hands */
	st = -1;
	k_wait(&pt, 0, &st);		/* init blocks: P is alive */
	k_exit(&pt, 2, 0);		/* A dies; init inherits zombie B */
	check(pt.procs[0].state == PROC_RUNNABLE && pt.procs[0].waiting == 0,
	      "test_init_inherits_corpse");
	st = -1;
	r = k_wait(&pt, 0, &st);
	check(r == 9 && st == 0x0500, "test_init_reaps_after_handoff");

	/* --- reap order --- */
	setup(&pt);
	pt_spawn(&pt, 2, 21, 0);
	pt_spawn(&pt, 5, 22, 0);
	k_exit(&pt, 5, 1);		/* higher slot dies first... */
	k_exit(&pt, 2, 2);
	st = -1;
	r = k_wait(&pt, 0, &st);	/* ...but lowest slot is reaped first */
	check(r == 21 && st == 0x0200, "test_lowest_slot_reaped_first");
	st = -1;
	r = k_wait(&pt, 0, &st);
	check(r == 22 && st == 0x0100, "test_second_zombie_reaped_next");

	return failed;
}
```

# Final Challenge: DuckOS, Assembled {#final-duckos points=100}

Twenty-two lessons ago the machine woke up with 16 bits, one
megabyte of addressable memory, and a 512-byte boot sector. Since
then, every lesson built one organ on the bench, in isolation: a
console to print on, a heap to allocate from, page tables to map
with, a proc table to book-keep in, a scheduler to choose with, a
message channel to talk through, a clock to keep the beat, a
filesystem to remember with. Isolation was the point — each
mechanism has invariants worth studying on their own — but no organ
is an organism. A kernel is what happens when these parts share one
address space and one arbiter, and every interesting kernel bug
lives in the seams between them.

This final challenge is the seams. You will assemble a working
DuckOS core: the proc table from *Processes: the Kernel's
Bookkeeping*, the multilevel feedback queue from *Scheduling*, the
rendezvous IPC from *Message Passing — the Microkernel Heart*, tick
wakeups in the spirit of *The Clock Ticks*, and the zombie state
machine from *Birth, Death, and Zombies* — one `struct kernel`, one
set of entry points, one observable stream of decisions. It is the
same shape Minix's `kernel/main.c` and `proc.c` trace out — the
loop Linus Torvalds stared at through the winter of 1991 before
deciding he'd rather write his own.

### How a kernel is tested without hardware

Be clear-eyed about what runs here. In a real DuckOS, processes
would execute user instructions between kernel entries, and the
kernel would regain control only at a trap or an interrupt — the
boundary we mapped in *The System Call Boundary*. In this
simulation, the test *is* every process's user code: it calls a
kernel entry point exactly where a real process would trap, and it
calls `k_tick` exactly where the PIT would interrupt. The kernel
under test cannot tell the difference — its entire job was only
ever to respond to entries in the right order.

What we grade, therefore, is the kernel's *observable behavior*:
who was chosen to run, who blocked and why, who was woken by what,
who died and who mourned. The kernel records each such decision in
a trace log — an append-only array of events — and the tests assert
on exact trace sequences. This is not a workaround; it is how
operating systems courses at real universities grade schedulers,
and how kernel developers regression-test theirs (Linux's
scheduler has been traced by `ftrace` for two decades). If the
trace is right, the kernel is right.

### The integration contracts

Each mechanism arrives from its lesson with a contract the others
now have to honor. These seams are where assembled kernels break,
so read this list twice:

- **The scheduler never knows why.** A process blocks for IPC, for
  sleep, for wait — the run queue only learns that `current` went
  away. Whoever *wakes* a process re-enqueues it; nothing else
  does. Wake without enqueue is the classic "process in limbo" bug:
  RUNNABLE forever, chosen never.
- **Blocking is rewarded; delivery does not preempt.** A process
  that blocks keeps its priority level (the MLQ's reward for
  yielding early). And when a send finds its receiver — or a
  receive drains a parked sender — the *woken* party goes to the
  back of its run queue; the *caller keeps the CPU*. Rendezvous
  delivery is a favor, not a surrender.
- **Timer wakeups restore, never demote.** A sleeper re-enters its
  own priority queue at `wake_at`, with a fresh quantum when next
  scheduled. Only *burning* a full quantum demotes.
- **Exit must cope with the current process dying.** `k_exit` ends
  with `current == -1` and someone else's problem; the corpse is
  marked ZOMBIE *before* any parent is woken — mark-then-wake, or
  the woken parent re-scans, finds nothing, and sleeps forever
  (the lost-wakeup bug from *Birth, Death, and Zombies*, now with a
  scheduler attached).
- **A waiter is nowhere.** A process blocked in `k_wait` is
  SLEEPING with `waiting == 1`: not in any run queue, and — mind
  this — *invisible to the tick handler*, whose sleeper scan must
  skip waiters or a stale `wake_at` will resurrect them.

Who un-blocks whom — each BLK_ reason has exactly one waker, and every path back to the CPU goes through a run queue:

```d2
direction: down

blocked: "blocked — in NO run queue" {
  grid-columns: 4
  s: "BLK_SEND\nSENDING\nsend_to = dst"
  r: "BLK_RECV\nRECEIVING\nrecv_from set"
  z: "BLK_SLEEP\nSLEEPING\nwaiting = 0"
  w: "BLK_WAIT\nSLEEPING\nwaiting = 1" {
    style.stroke: "#dc2626"
    style.stroke-width: 3
  }
}

rq: "rq_push — back of its OWN priority queue" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}

blocked.s -> rq: dst's k_receive drains it
blocked.r -> rq: a matching k_send delivers
blocked.z -> rq: "k_tick: wake_at <= ticks"
blocked.w -> rq: child's k_exit
```

And the state each entry point owns — every mutation flows into the same struct; the red edge is the one rule with no exception (blocking always clears current):

```d2
direction: right

ep: "the eight entry points" {
  grid-columns: 2
  k_spawn
  k_send
  k_schedule
  k_receive
  k_tick
  k_exit
  k_sleep
  k_wait
}

k: "struct kernel" {
  shape: sql_table
  procs: "proc[16]"
  rq: "rq[4] of slots"
  current: "slot or -1"
  ticks: "uint32_t"
  trace: "kevent[128]"
}

ep.k_spawn -> k.rq: "enqueue new"
ep.k_schedule -> k.current: "pop lowest,\nfresh quantum"
ep.k_tick -> k.rq: "wake sleepers,\ndemote the hog"
ep.k_send -> k.rq: "enqueue woken\npartner / waiter"
ep -> k.current: "any block or exit:\ncurrent = -1" {
  style.stroke: "#dc2626"
}
ep -> k.trace: "every decision" {style.stroke-dash: 4}
```

### One handshake, end to end

Here is the shape of the finale's grand scenario, as the trace
records it. A server receives; a client sends and naps; the reply
has to park because the client is asleep; the clock delivers the
client back; everyone dies in an orderly fashion and init buries
them:

```
  init                server              client            trace
  ----                ------              ------            -----
  wait -> blocks                                            BLOCK(0, wait)
                      recv(ANY) -> blocks                   BLOCK(1, recv)
                                          send(1)  ------>  WAKE(1)   [delivered]
                                          sleep(2)          BLOCK(2, sleep)
                      send(2) -> parks                      BLOCK(1, send)
                                                 tick...    WAKE(2)   [clock]
                                          recv(ANY) ----->  WAKE(1)   [drained]
                                          exit              EXIT(2), WAKE(0)
                      exit                                  EXIT(1)
  wait -> reaps 2, reaps 3                                  REAP, REAP
```

Every arrow is a seam from the list above. If your trace matches,
your kernel just booted, served, slept, woke, and died on purpose.

You have built every piece of this before. What follows is the
last and best kind of systems programming: no new ideas, only the
discipline of making old ideas true at the same time. When the
final test passes, go read what you can now read — Tanenbaum's
`proc.c`, the xv6 sources, the Linux 0.01 tarball (it's tiny, and
it's online). None of it will be foreign. You'll recognize the
proc table, the queues, the rendezvous, the zombie parade — the
same machine, wearing history.

### Starter

```c
#include <stdint.h>

#define NPROC 16
#define NQ 4
#define ANY (-1)
#define QUANTUM(q) (1 << (q))	/* queue 0: 1 tick ... queue 3: 8 */
#define INIT_PID 1
#define ESRCH 3
#define EDEADLK 35
#define ECHILD 10

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct message {
	int m_source;		/* stamped by the kernel on delivery */
	int m_type;
	int m_i1;
	int m_i2;
};

/* The kernel narrates every decision into the trace. */
enum ev_type { EV_SPAWN, EV_RUN, EV_PREEMPT, EV_BLOCK, EV_WAKE,
               EV_EXIT, EV_REAP };

/* EV_BLOCK reasons, carried in the event's arg field. */
#define BLK_SEND  0
#define BLK_RECV  1
#define BLK_SLEEP 2
#define BLK_WAIT  3

struct kevent {
	enum ev_type type;
	int slot;	/* who the event is about */
	int arg;	/* per-type extra (see each entry point) */
};

#define NTRACE 128

struct proc {
	int pid;
	enum proc_state state;
	int parent;		/* slot index of parent; -1 = none */
	int prio;		/* current MLQ level, 0..NQ-1 */
	int send_to;		/* slot blocked sending to, -1 */
	int recv_from;		/* slot or ANY blocked receiving from, -1 */
	struct message buf;	/* parked outbound message */
	struct message *user_out;	/* blocked receiver's landing pad */
	uint32_t wake_at;	/* absolute tick to wake (k_sleep) */
	int waiting;		/* 1 = blocked in k_wait */
	int exit_status;	/* valid once ZOMBIE */
};

struct kernel {
	struct proc procs[NPROC];
	int next_pid;
	int rq[NQ][NPROC];	/* circular FIFO run queue per level */
	int rq_head[NQ];
	int rq_count[NQ];
	int current;		/* RUNNING slot, or -1 */
	int quantum_left;
	uint32_t ticks;
	struct kevent trace[NTRACE];
	int ntrace;
};

/* ---- Provided plumbing: proven in earlier lessons. ---- */

/* Append an event; silently drop past NTRACE (never in the tests). */
void trace_emit(struct kernel *k, enum ev_type type, int slot, int arg)
{
	if (k->ntrace < NTRACE) {
		k->trace[k->ntrace].type = type;
		k->trace[k->ntrace].slot = slot;
		k->trace[k->ntrace].arg = arg;
		k->ntrace++;
	}
}

/* The per-level circular FIFO from *Scheduling*. */
void rq_push(struct kernel *k, int level, int slot)
{
	k->rq[level][(k->rq_head[level] + k->rq_count[level]) % NPROC]
		= slot;
	k->rq_count[level]++;
}

int rq_pop(struct kernel *k, int level)
{
	int slot = k->rq[level][k->rq_head[level]];

	k->rq_head[level] = (k->rq_head[level] + 1) % NPROC;
	k->rq_count[level]--;
	return slot;
}

int rq_empty(const struct kernel *k, int level)
{
	return k->rq_count[level] == 0;
}

/* Cold boot: empty table, empty queues, tick 0, empty trace. */
void k_init(struct kernel *k)
{
	for (int i = 0; i < NPROC; i++) {
		k->procs[i].pid = 0;
		k->procs[i].state = PROC_UNUSED;
		k->procs[i].parent = -1;
		k->procs[i].prio = 0;
		k->procs[i].send_to = -1;
		k->procs[i].recv_from = -1;
		k->procs[i].user_out = 0;
		k->procs[i].wake_at = 0;
		k->procs[i].waiting = 0;
		k->procs[i].exit_status = 0;
	}
	k->next_pid = 1;
	for (int q = 0; q < NQ; q++) {
		k->rq_head[q] = 0;
		k->rq_count[q] = 0;
	}
	k->current = -1;
	k->quantum_left = 0;
	k->ticks = 0;
	k->ntrace = 0;
}

/* ---- The eight entry points: yours. ---- */

/*
 * Create a process: lowest UNUSED slot, pid = next_pid++, RUNNABLE,
 * parent as given, prio clamped to 0..NQ-1, links cleared; enqueue
 * at prio; trace EV_SPAWN(slot, pid). Returns the slot, or -1 if
 * the table is full.
 */
int k_spawn(struct kernel *k, int parent_slot, int prio)
{
	/* TODO */
	(void)k;
	(void)parent_slot;
	(void)prio;
	return -1;
}

/*
 * Pick who runs: if current != -1 just return it. Pop the lowest-
 * numbered non-empty queue; that slot becomes RUNNING and current,
 * quantum_left = QUANTUM(level); trace EV_RUN(slot, level). Returns
 * the slot, or -1 when every queue is empty (the idle loop).
 */
int k_schedule(struct kernel *k)
{
	/* TODO */
	(void)k;
	return -1;
}

/*
 * The clock interrupt. ticks++. First wake every sleeper that is
 * due: state SLEEPING, waiting == 0 (NEVER wake a waiter -- its
 * wake_at is stale), wake_at <= ticks. Each woken: RUNNABLE,
 * enqueued at its own prio, trace EV_WAKE(slot, 0). Then charge the
 * quantum: if a process is current, quantum_left--; on reaching 0
 * demote it one level (cap NQ-1), RUNNABLE, re-enqueue at the new
 * level, trace EV_PREEMPT(slot, newprio), current = -1.
 */
void k_tick(struct kernel *k)
{
	/* TODO: wake sleepers in slot order, then handle the quantum */
	(void)k;
}

/*
 * current sends *m to dst (rendezvous, from *Message Passing*).
 * No current, or dst out of range / UNUSED: -ESRCH. dst == current:
 * -EDEADLK. If dst is RECEIVING from ANY or from current: copy *m
 * into its user_out (or buf if user_out is 0) stamping m_source =
 * current, dst becomes RUNNABLE + enqueued at its prio, trace
 * EV_WAKE(dst, current); the SENDER KEEPS RUNNING. Otherwise walk
 * send_to from dst while those procs are SENDING; reaching current
 * means -EDEADLK (refuse, don't block). Else block current:
 * SENDING, send_to = dst, buf = *m, trace EV_BLOCK(current,
 * BLK_SEND), current = -1. Returns 0 unless an error above.
 */
int k_send(struct kernel *k, int dst, const struct message *m)
{
	/* TODO */
	(void)k;
	(void)dst;
	(void)m;
	return -ESRCH;
}

/*
 * current receives into *out from `from` (a slot, or ANY). No
 * current, or from neither ANY nor a live slot: -ESRCH. Scan slots
 * 0..NPROC-1 for a SENDING proc with send_to == current matching
 * from: copy its buf to *out with m_source = sender slot, sender
 * RUNNABLE + enqueued at its prio, trace EV_WAKE(sender, current);
 * the RECEIVER KEEPS RUNNING. No match: block current — RECEIVING,
 * recv_from = from, user_out = out, trace EV_BLOCK(current,
 * BLK_RECV), current = -1. Returns 0 unless an error above.
 */
int k_receive(struct kernel *k, int from, struct message *out)
{
	/* TODO */
	(void)k;
	(void)from;
	(void)out;
	return -ESRCH;
}

/*
 * current sleeps nticks. No current: -ESRCH. nticks == 0: return 0,
 * still running. Else SLEEPING, wake_at = ticks + nticks, waiting =
 * 0, trace EV_BLOCK(current, BLK_SLEEP), current = -1. Returns 0.
 */
int k_sleep(struct kernel *k, uint32_t nticks)
{
	/* TODO */
	(void)k;
	(void)nticks;
	return -ESRCH;
}

/*
 * current dies. No current: -ESRCH. Mark the corpse FIRST: state
 * ZOMBIE, exit_status = (status & 0xff) << 8, waiting = 0, trace
 * EV_EXIT(slot, encoded status). Reparent every child to init's
 * slot (the live proc with pid INIT_PID; parent -1 if init is
 * missing). Then the wakeups, each one RUNNABLE + enqueued + trace
 * EV_WAKE(woken, dying slot): the dying proc's parent if it is
 * waiting; and init, if it just inherited a ZOMBIE child and is
 * waiting (skip if it was already woken as the parent). current =
 * -1. Returns 0.
 */
int k_exit(struct kernel *k, int status)
{
	/* TODO */
	(void)k;
	(void)status;
	return -ESRCH;
}

/*
 * current waits for a child. No current: -ESRCH. A ZOMBIE child
 * exists: reap the lowest slot — *status = its exit_status, slot
 * freed (UNUSED, pid 0), trace EV_REAP(current, child's pid) —
 * return the child's pid; THE CALLER KEEPS RUNNING. Only live
 * children: block — waiting = 1, SLEEPING, trace EV_BLOCK(current,
 * BLK_WAIT), current = -1, return 0. No children at all: -ECHILD.
 */
int k_wait(struct kernel *k, int *status)
{
	/* TODO */
	(void)k;
	(void)status;
	return -ESRCH;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

#define NPROC 16
#define NQ 4
#define ANY (-1)
#define QUANTUM(q) (1 << (q))
#define INIT_PID 1
#define ESRCH 3
#define EDEADLK 35
#define ECHILD 10

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct message {
	int m_source;
	int m_type;
	int m_i1;
	int m_i2;
};

enum ev_type { EV_SPAWN, EV_RUN, EV_PREEMPT, EV_BLOCK, EV_WAKE,
               EV_EXIT, EV_REAP };

#define BLK_SEND  0
#define BLK_RECV  1
#define BLK_SLEEP 2
#define BLK_WAIT  3

struct kevent {
	enum ev_type type;
	int slot;
	int arg;
};

#define NTRACE 128

struct proc {
	int pid;
	enum proc_state state;
	int parent;
	int prio;
	int send_to;
	int recv_from;
	struct message buf;
	struct message *user_out;
	uint32_t wake_at;
	int waiting;
	int exit_status;
};

struct kernel {
	struct proc procs[NPROC];
	int next_pid;
	int rq[NQ][NPROC];
	int rq_head[NQ];
	int rq_count[NQ];
	int current;
	int quantum_left;
	uint32_t ticks;
	struct kevent trace[NTRACE];
	int ntrace;
};

void k_init(struct kernel *k);
int k_spawn(struct kernel *k, int parent_slot, int prio);
int k_schedule(struct kernel *k);
void k_tick(struct kernel *k);
int k_send(struct kernel *k, int dst, const struct message *m);
int k_receive(struct kernel *k, int from, struct message *out);
int k_sleep(struct kernel *k, uint32_t nticks);
int k_exit(struct kernel *k, int status);
int k_wait(struct kernel *k, int *status);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Do the n events at trace[start..] match `want` exactly? */
static int trace_match(const struct kernel *k, int start,
                       const struct kevent *want, int n)
{
	if (start + n > k->ntrace)
		return 0;
	for (int i = 0; i < n; i++) {
		const struct kevent *e = &k->trace[start + i];

		if (e->type != want[i].type || e->slot != want[i].slot ||
		    e->arg != want[i].arg)
			return 0;
	}
	return 1;
}

static struct kernel K;

int main(void)
{
	struct message m, in, got;
	int r, t0, st;

	/* --- Boot: spawn and schedule. --- */
	k_init(&K);
	r = k_spawn(&K, -1, 0);
	check(r == 0 && K.procs[0].pid == 1 &&
	      K.procs[0].state == PROC_RUNNABLE, "test_spawn_init");
	{
		struct kevent want[] = { { EV_SPAWN, 0, 1 } };

		check(trace_match(&K, 0, want, 1), "test_spawn_traced");
	}
	r = k_schedule(&K);
	check(r == 0 && K.current == 0 &&
	      K.procs[0].state == PROC_RUNNING &&
	      K.quantum_left == QUANTUM(0), "test_schedule_runs_init");
	{
		struct kevent want[] = { { EV_RUN, 0, 0 } };

		check(trace_match(&K, 1, want, 1), "test_run_traced");
	}

	/* --- Priority order across levels. --- */
	r = k_spawn(&K, 0, 2);
	check(r == 1 && k_spawn(&K, 0, 1) == 2, "test_spawn_children");
	k_sleep(&K, 100);
	check(K.current == -1 && K.procs[0].state == PROC_SLEEPING,
	      "test_sleep_blocks_current");
	check(k_schedule(&K) == 2, "test_higher_priority_first");
	k_sleep(&K, 100);
	check(k_schedule(&K) == 1, "test_lower_priority_next");
	k_sleep(&K, 100);
	check(k_schedule(&K) == -1 && K.current == -1,
	      "test_idle_when_all_blocked");

	/* --- Quantum, preemption, demotion. --- */
	k_init(&K);
	k_spawn(&K, -1, 0);
	k_spawn(&K, 0, 0);
	k_schedule(&K);			/* slot 0, QUANTUM(0) = 1 */
	t0 = K.ntrace;
	k_tick(&K);
	check(K.current == -1 && K.procs[0].prio == 1 &&
	      K.procs[0].state == PROC_RUNNABLE,
	      "test_quantum_expiry_demotes");
	{
		struct kevent want[] = { { EV_PREEMPT, 0, 1 } };

		check(trace_match(&K, t0, want, 1), "test_preempt_traced");
	}
	check(k_schedule(&K) == 1, "test_demoted_hog_loses_to_fresh");
	k_tick(&K);			/* slot 1 burns out too */
	check(k_schedule(&K) == 0 && K.quantum_left == QUANTUM(1),
	      "test_fifo_at_new_level_fresh_quantum");

	/* --- Sleep timing; blocking keeps priority. --- */
	k_init(&K);
	k_spawn(&K, -1, 0);
	k_spawn(&K, 0, 2);
	k_schedule(&K);
	k_sleep(&K, 100);		/* park init */
	k_schedule(&K);			/* worker, prio 2 */
	k_sleep(&K, 3);
	t0 = K.ntrace;
	k_tick(&K);
	k_tick(&K);
	check(K.procs[1].state == PROC_SLEEPING && K.ntrace == t0,
	      "test_sleeper_not_woken_early");
	k_tick(&K);
	check(K.procs[1].state == PROC_RUNNABLE,
	      "test_sleeper_wakes_on_time");
	{
		struct kevent want[] = { { EV_WAKE, 1, 0 } };

		check(trace_match(&K, t0, want, 1), "test_wake_traced");
	}
	r = k_schedule(&K);
	check(r == 1 && K.procs[1].prio == 2 &&
	      K.quantum_left == QUANTUM(2),
	      "test_blocking_keeps_priority");

	/* --- IPC: receiver first. --- */
	k_init(&K);
	k_spawn(&K, -1, 0);		/* 0: init   */
	k_spawn(&K, 0, 0);		/* 1: server */
	k_spawn(&K, 0, 0);		/* 2: client */
	k_schedule(&K);
	k_sleep(&K, 1000);		/* park init */
	check(k_schedule(&K) == 1, "test_server_scheduled");
	in.m_source = in.m_type = in.m_i1 = in.m_i2 = 0;
	r = k_receive(&K, ANY, &in);
	check(r == 0 && K.current == -1 &&
	      K.procs[1].state == PROC_RECEIVING,
	      "test_receive_blocks_when_no_sender");
	k_schedule(&K);			/* client */
	m.m_source = 999;		/* forged; kernel must stamp */
	m.m_type = 7;
	m.m_i1 = 11;
	m.m_i2 = 22;
	r = k_send(&K, 1, &m);
	check(r == 0 && in.m_type == 7 && in.m_i1 == 11 &&
	      in.m_source == 2, "test_rendezvous_delivery_and_stamp");
	check(K.current == 2 && K.procs[1].state == PROC_RUNNABLE,
	      "test_send_does_not_preempt_sender");

	/* --- IPC: sender first (continuing the same kernel). --- */
	r = k_send(&K, 1, &m);		/* server not receiving now */
	check(r == 0 && K.current == -1 &&
	      K.procs[2].state == PROC_SENDING && K.procs[2].send_to == 1,
	      "test_sender_first_blocks");
	check(k_schedule(&K) == 1, "test_server_runs_again");
	got.m_source = got.m_type = got.m_i1 = got.m_i2 = 0;
	r = k_receive(&K, ANY, &got);
	check(r == 0 && got.m_source == 2 && got.m_type == 7 &&
	      K.current == 1 && K.procs[2].state == PROC_RUNNABLE,
	      "test_receive_drains_parked_sender");

	/* --- Deadlock refusal. --- */
	k_init(&K);
	k_spawn(&K, -1, 0);		/* 0: A */
	k_spawn(&K, 0, 0);		/* 1: B */
	k_schedule(&K);			/* A */
	m.m_type = 1;
	k_send(&K, 1, &m);		/* A blocks sending to B */
	k_schedule(&K);			/* B */
	r = k_send(&K, 0, &m);
	check(r == -EDEADLK && K.current == 1 &&
	      K.procs[1].state == PROC_RUNNING, "test_deadlock_refused");
	check(k_send(&K, 1, &m) == -EDEADLK, "test_self_send_refused");
	check(k_send(&K, 5, &m) == -ESRCH, "test_send_to_unused_esrch");

	/* --- exit and wait, with the wakeup handshake. --- */
	k_init(&K);
	k_spawn(&K, -1, 0);		/* 0: init, pid 1 */
	k_spawn(&K, 0, 0);		/* 1: child, pid 2 */
	k_schedule(&K);			/* init */
	st = -1;
	r = k_wait(&K, &st);
	check(r == 0 && K.current == -1 && K.procs[0].waiting == 1 &&
	      K.procs[0].state == PROC_SLEEPING,
	      "test_wait_blocks_on_live_child");
	k_schedule(&K);			/* child */
	t0 = K.ntrace;
	k_exit(&K, 42);
	check(K.procs[1].state == PROC_ZOMBIE &&
	      K.procs[0].state == PROC_RUNNABLE && K.procs[0].waiting == 0,
	      "test_exit_wakes_waiting_parent");
	{
		struct kevent want[] = {
			{ EV_EXIT, 1, 0x2A00 },
			{ EV_WAKE, 0, 1 },
		};

		check(trace_match(&K, t0, want, 2),
		      "test_mark_zombie_then_wake");
	}
	check(k_schedule(&K) == 0, "test_woken_parent_rescheduled");
	st = -1;
	t0 = K.ntrace;
	r = k_wait(&K, &st);
	check(r == 2 && st == 0x2A00 &&
	      K.procs[1].state == PROC_UNUSED && K.current == 0,
	      "test_woken_parent_reaps");
	{
		struct kevent want[] = { { EV_REAP, 0, 2 } };

		check(trace_match(&K, t0, want, 1), "test_reap_traced");
	}
	check(k_wait(&K, &st) == -ECHILD, "test_echild_after_reap");

	/* --- Orphans land on init. --- */
	k_init(&K);
	k_spawn(&K, -1, 0);		/* 0: init, pid 1 */
	k_spawn(&K, 0, 0);		/* 1: P,    pid 2 */
	k_spawn(&K, 1, 0);		/* 2: C,    pid 3, child of P */
	k_schedule(&K);			/* init */
	k_wait(&K, &st);		/* blocks: P is alive */
	k_schedule(&K);			/* P */
	k_exit(&K, 0);			/* P dies; C orphaned */
	check(K.procs[2].parent == 0, "test_orphan_reparented_to_init");
	check(K.procs[0].state == PROC_RUNNABLE,
	      "test_init_woken_by_own_child");
	k_schedule(&K);			/* C: queued since spawn, ahead
					 * of the freshly woken init */
	k_exit(&K, 7);			/* C dies; init inherits a corpse */
	k_schedule(&K);			/* now init buries both: */
	check(k_wait(&K, &st) == 2, "test_init_reaps_own_child");
	r = k_wait(&K, &st);
	check(r == 3 && st == 0x0700, "test_init_reaps_inherited_child");

	/* --- The grand finale: one whole life of a system. --- */
	k_init(&K);
	k_spawn(&K, -1, 0);		/* 0: init,   pid 1, prio 0 */
	k_spawn(&K, 0, 0);		/* 1: server, pid 2, prio 0 */
	k_spawn(&K, 0, 1);		/* 2: client, pid 3, prio 1 */
	k_schedule(&K);			/* init */
	k_wait(&K, &st);		/* init blocks: 2 live children */
	k_schedule(&K);			/* server (level 0 first) */
	in.m_source = in.m_type = in.m_i1 = in.m_i2 = 0;
	k_receive(&K, ANY, &in);	/* server blocks */
	k_schedule(&K);			/* client (level 1) */
	m.m_source = 0;
	m.m_type = 42;
	m.m_i1 = 1;
	m.m_i2 = 2;
	k_send(&K, 1, &m);		/* delivered; client keeps CPU */
	k_sleep(&K, 2);			/* client naps until tick 2 */
	k_schedule(&K);			/* server */
	m.m_type = 43;
	m.m_i1 = in.m_i1 + in.m_i2;	/* the server's arithmetic: 3 */
	m.m_i2 = 0;
	r = k_send(&K, 2, &m);		/* client asleep: reply parks */
	check(r == 0 && K.procs[1].state == PROC_SENDING,
	      "test_finale_reply_parks");
	k_tick(&K);
	k_tick(&K);			/* client due at tick 2 */
	check(K.procs[2].state == PROC_RUNNABLE,
	      "test_finale_clock_returns_client");
	k_schedule(&K);			/* client */
	got.m_source = got.m_type = got.m_i1 = got.m_i2 = 0;
	k_receive(&K, ANY, &got);	/* drains the parked reply */
	check(got.m_type == 43 && got.m_i1 == 3 && got.m_source == 1,
	      "test_finale_reply_received");
	k_exit(&K, 0);			/* client exits; init wakes */
	k_schedule(&K);			/* server (ahead of init in q0) */
	k_exit(&K, 0);			/* server exits */
	k_schedule(&K);			/* init */
	r = k_wait(&K, &st);
	check(r == 2, "test_finale_init_reaps_server");
	r = k_wait(&K, &st);
	check(r == 3 && k_wait(&K, &st) == -ECHILD && K.ticks == 2,
	      "test_finale_init_reaps_client");
	{
		struct kevent want[] = {
			{ EV_SPAWN,   0, 1 },		/* init       */
			{ EV_SPAWN,   1, 2 },		/* server     */
			{ EV_SPAWN,   2, 3 },		/* client     */
			{ EV_RUN,     0, 0 },
			{ EV_BLOCK,   0, BLK_WAIT },
			{ EV_RUN,     1, 0 },
			{ EV_BLOCK,   1, BLK_RECV },
			{ EV_RUN,     2, 1 },
			{ EV_WAKE,    1, 2 },		/* delivery   */
			{ EV_BLOCK,   2, BLK_SLEEP },
			{ EV_RUN,     1, 0 },
			{ EV_BLOCK,   1, BLK_SEND },	/* reply parks */
			{ EV_WAKE,    2, 0 },		/* the clock  */
			{ EV_RUN,     2, 1 },
			{ EV_WAKE,    1, 2 },		/* drained    */
			{ EV_EXIT,    2, 0 },
			{ EV_WAKE,    0, 2 },		/* init wakes */
			{ EV_RUN,     1, 0 },
			{ EV_EXIT,    1, 0 },
			{ EV_RUN,     0, 0 },
			{ EV_REAP,    0, 2 },
			{ EV_REAP,    0, 3 },
		};

		check(K.ntrace == 22 && trace_match(&K, 0, want, 22),
		      "test_duckos_boots");
	}

	return failed;
}
```

# Lesson: Epilogue: Boot It for Real {#boot-for-real}

Every lesson in this course made you the same promise: *in a real
kernel this struct IS the hardware table; here we build it in a
buffer the tests can read.* This epilogue is the receipt. There are
no challenges below and nothing to submit — the grader lives in a
container with no CPU to hand you and no screen to draw on. What
there is instead: about two hundred lines of shim that take code you
have already written — your console, your printf, your descriptor
encoders, your PIT math, your scancode decoder — and boot them on a
(virtual) 32-bit PC. At the end you will watch an operating system
made of your own solutions print its banner in VGA green, tick a
hundred times a second, and echo your keystrokes, in QEMU on your
own machine.

You need two things installed: `qemu-system-i386` (package
`qemu-base` on Arch, `qemu-system-x86` on Debian/Fedora) and a gcc
that can target 32-bit (`gcc -m32`; any distro gcc can). No
cross-compiler, no bootloader to install, no disk image to bless.

## The shortcut: multiboot

*The Machine Wakes Up* walked the honest path — reset vector, real
mode, a 512-byte boot sector, A20 — because that is the machine's
truth and you should know it. Having learned it, you now get to skip
it. The Multiboot standard (written for GRUB in the 90s) says: if
your kernel is an ELF file that carries a small magic header, a
compliant loader will do the real-mode drudgery for you and jump to
your entry point already in 32-bit protected mode, A20 open, with a
flat 4 GiB view of memory. QEMU speaks it natively:

```
qemu-system-i386 -kernel duckos.elf
```

No GRUB, no ISO, no boot sector. The price of admission is twelve
bytes:

```
.section .multiboot
.align 4
.long 0x1BADB002          /* multiboot v1 magic */
.long 0                   /* flags: nothing fancy */
.long -(0x1BADB002)       /* checksum: the three sum to zero */
```

The loader hands you a CPU mid-flight, though — running on the
LOADER's GDT, with no stack and interrupts off. Your first job is a
stack; your second is replacing borrowed descriptor tables with your
own. Conveniently, you wrote the encoder for those in *Segments and
Privilege*.

The whole boot, end to end — after kmain hands control to the hlt loop, the green interrupt path is the only thing that ever runs:

```d2
direction: right

qemu: "qemu -kernel:\nreads multiboot\nheader" {
  shape: oval
}
start: "_start:\nstack + cld\n(no GDT yet)"
kmain: "kmain: console,\ngdt, idt, pic, pit" {
  style.stroke: "#d97706"
  style.stroke-width: 3
}
idle: "sti;\nhlt loop" {
  shape: oval
}
irq: "IRQ 0 / 1:\nyour handlers" {
  style.stroke: "#16a34a"
  style.stroke-width: 3
}

qemu -> start
start -> kmain
kmain -> idle
irq -> idle: "wake, iret,\nsleep" {
  style.stroke-dash: 4
}
```

## The project

Fourteen small files. Seven of them are YOUR solutions, pasted in
with seams noted below; the rest is new and printed here in full.

```
duckos/
  Makefile      new: 25 lines
  linker.ld     new: place the kernel at 1 MiB
  boot.S        new: multiboot header + 8 instructions
  isr.S         new: two interrupt stubs
  duckos.h      new: shared declarations (below)
  klib.c        YOUR kmem solution, renamed
  console.c     YOUR vga-console solution, verbatim
  printf.c      YOUR kvsnprintf solution, verbatim
  kbd.c         YOUR scancode-decode solution, verbatim
  gdt.c         YOUR gdt_encode + 20 new lines to load it
  idt.c         YOUR idt_gate + 15 new lines to load it
  pic.c         new: the real 8259 (your pic8259 was the model)
  pit.c         YOUR pit_divisor + 8 new lines of outb
  kernel.c      new: kmain — wires everything together
```

### The seams, honestly listed

Porting simulation to metal takes exactly four edits, and each one
teaches something:

1. **The console needs no edits — only a cast.** `struct console`
   puts its cell array first, so a console living AT `0xB8000` puts
   its cells exactly where the VGA hardware reads them:

   ```c
   #define VGA_CONSOLE ((struct console *)0xB8000)
   ```

   The `row`/`col`/`attr` fields land just past the visible 4000
   bytes, in VGA memory the text mode never displays — free scratch.
   Every `console_putc` you wrote in *A Screen to Print On* now
   draws real characters. (Delete its `#include <string.h>` — the
   next seam explains why.)

2. **Your kmem functions get their real names.** Rename `kmemset` →
   `memset`, `kmemcpy` → `memcpy`, `kmemmove` → `memmove`, `kstrlen`
   → `strlen`. *C With Nothing Underneath* told you gcc assumes
   these four exist even in freestanding mode; now it's true — your
   console's scroll calls your memmove on real video memory. There
   is no `<string.h>` to include anymore. You are the library.

3. **The 8259 model stays on the bench.** Your `pic_next`/`pic_eoi`
   modeled the chip's decision logic; the real chip runs that logic
   in silicon, and what the kernel owes it is configuration: the
   ICW init sequence (in `pic.c` below) that moves IRQs off the CPU
   exception vectors — the design collision *Interrupts and the
   IDT* warned about — and one `outb(0x20, 0x20)` of EOI per
   interrupt.

4. **Everything else is verbatim.** `kvsnprintf` (its headers,
   `stdarg.h` and `stddef.h`, are freestanding — the compiler
   provides them, not libc), `gdt_encode`, `idt_gate`,
   `pit_divisor`, `kbd_decode`: paste your solutions unchanged.

### boot.S — entry

```
	.section .multiboot
	.align 4
	.long 0x1BADB002
	.long 0
	.long -(0x1BADB002)

	.section .bss
	.align 16
stack_bottom:
	.skip 16384			/* 16 KiB kernel stack */
stack_top:

	.section .text
	.global _start
_start:
	mov $stack_top, %esp
	cld				/* SysV ABI: DF clear before C */
	call kmain
1:	cli
	hlt
	jmp 1b
```

### linker.ld — where the kernel lives

```
ENTRY(_start)
SECTIONS
{
	. = 1M;
	.text   : { *(.multiboot) *(.text*) }
	.rodata : { *(.rodata*) }
	.data   : { *(.data*) }
	.bss    : { *(COMMON) *(.bss*) }
}
```

One megabyte: the first address past the real-mode museum — the
ROMs, the VGA window, the EBDA — that *Owning Physical Memory*
mapped out. The multiboot header must be early in the file so the
loader finds it; listing it first in `.text` guarantees that.

### isr.S — the two stubs

The CPU pushes EFLAGS/CS/EIP and vectors through your IDT; these
stubs save the registers C is allowed to clobber, call your handler,
and `iret` — the hardware/software handshake from *Interrupts and
the IDT*, eight instructions long:

```
	.section .text
	.global irq0_stub
irq0_stub:
	pusha
	cld
	call timer_isr
	popa
	iret

	.global irq1_stub
irq1_stub:
	pusha
	cld
	call kbd_isr
	popa
	iret
```

### duckos.h — the shared header

Collect your structs and prototypes (console, kbd, the k-functions
under their new names, `kvsnprintf`/`ksnprintf`) into one header,
and add the two functions every driver lesson simulated. They are
one instruction each:

```c
static inline void outb(uint16_t port, uint8_t val)
{
	__asm__ volatile("outb %0, %1" : : "a"(val), "Nd"(port));
}

static inline uint8_t inb(uint16_t port)
{
	uint8_t val;

	__asm__ volatile("inb %1, %0" : "=a"(val) : "Nd"(port));
	return val;
}
```

### gdt.c — your encoder, loaded

Below your unmodified `gdt_encode`, add the loading half. The three
descriptors are exactly the ones you encoded by hand in *Segments
and Privilege*: null, flat ring-0 code (`0x9A`, flags `0xC`), flat
ring-0 data (`0x92`, flags `0xC`). New: the `lgdt` instruction takes
a 6-byte pointer (16-bit limit, 32-bit base) — note the `packed`,
or the compiler pads it — and CS only reloads via a far jump:

```c
static uint8_t gdt[3][8];

struct gdtr {
	uint16_t limit;
	uint32_t base;
} __attribute__((packed));

void gdt_install(void)
{
	static struct gdtr gdtr;

	memset(gdt[0], 0, 8);				/* the null seat */
	gdt_encode(gdt[1], 0, 0xFFFFF, 0x9A, 0xC);	/* 0x08: code */
	gdt_encode(gdt[2], 0, 0xFFFFF, 0x92, 0xC);	/* 0x10: data */

	gdtr.limit = sizeof(gdt) - 1;
	gdtr.base = (uint32_t)(uintptr_t)gdt;

	__asm__ volatile(
		"lgdt %0\n\t"
		"ljmp $0x08, $1f\n"	/* far jump reloads CS */
		"1:\n\t"
		"mov $0x10, %%eax\n\t"
		"mov %%eax, %%ds\n\t"
		"mov %%eax, %%es\n\t"
		"mov %%eax, %%fs\n\t"
		"mov %%eax, %%gs\n\t"
		"mov %%eax, %%ss\n\t"
		: : "m"(gdtr) : "eax", "memory");
}
```

### idt.c — your gates, served

Below your unmodified `idt_gate`: 256 zeroed (= not-present) gates,
two filled. After the PIC remap, the timer arrives at vector 0x20
and the keyboard at 0x21; both gates point at the stubs from isr.S
through your new code selector:

```c
static uint8_t idt[256][8];

struct idtr {
	uint16_t limit;
	uint32_t base;
} __attribute__((packed));

void idt_install(void)
{
	static struct idtr idtr;

	memset(idt, 0, sizeof(idt));
	idt_gate(idt[0x20], (uint32_t)(uintptr_t)irq0_stub, 0x08, 0, 0);
	idt_gate(idt[0x21], (uint32_t)(uintptr_t)irq1_stub, 0x08, 0, 0);

	idtr.limit = sizeof(idt) - 1;
	idtr.base = (uint32_t)(uintptr_t)idt;
	__asm__ volatile("lidt %0" : : "m"(idtr) : "memory");
}
```

### pic.c — the real 8259 pair

```c
#define PIC1_CMD  0x20
#define PIC1_DATA 0x21
#define PIC2_CMD  0xA0
#define PIC2_DATA 0xA1

void pic_remap(void)
{
	outb(PIC1_CMD, 0x11);	/* ICW1: init, expect ICW4 */
	outb(PIC2_CMD, 0x11);
	outb(PIC1_DATA, 0x20);	/* ICW2: master -> vectors 0x20-0x27 */
	outb(PIC2_DATA, 0x28);	/* ICW2: slave  -> vectors 0x28-0x2F */
	outb(PIC1_DATA, 0x04);	/* ICW3: slave hangs off line 2 */
	outb(PIC2_DATA, 0x02);	/* ICW3: slave identity */
	outb(PIC1_DATA, 0x01);	/* ICW4: 8086 mode */
	outb(PIC2_DATA, 0x01);

	/* Mask all but IRQ 0 (PIT) and IRQ 1 (keyboard). */
	outb(PIC1_DATA, 0xFC);
	outb(PIC2_DATA, 0xFF);
}

void pic_send_eoi(int irq)
{
	if (irq >= 8)
		outb(PIC2_CMD, 0x20);
	outb(PIC1_CMD, 0x20);
}
```

### pit.c — your divisor, on the wire

Below your unmodified `pit_divisor`, the three `outb`s *The Clock
Ticks* described — mode byte, then the divisor low/high. Note the
encoding your challenge already handles: 65536 goes over the wire
as 0.

```c
void pit_program(uint32_t hz)
{
	uint32_t d = pit_divisor(hz);

	outb(0x43, 0x36);
	outb(0x40, (uint8_t)(d & 0xFF));
	outb(0x40, (uint8_t)((d >> 8) & 0xFF));
}
```

### kernel.c — kmain

The wiring. Order matters and every line of it is a lesson callback:
console first (so later stages can narrate), then GDT, IDT, PIC,
PIT, then — the moment of truth — `sti`:

```c
#include "duckos.h"

static struct console *cons;
static struct kbd kbd;
static volatile uint32_t ticks;

void kprintf(const char *fmt, ...)
{
	char buf[256];
	va_list ap;

	va_start(ap, fmt);
	kvsnprintf(buf, sizeof buf, fmt, ap);
	va_end(ap);
	console_puts(cons, buf);
}

void timer_isr(void)
{
	ticks++;
	if (ticks % HZ == 0)
		kprintf("[%5u] tick: alive %u s\n",
			ticks / HZ, ticks / HZ);
	pic_send_eoi(0);
}

void kbd_isr(void)
{
	uint8_t sc = inb(0x60);
	int c = kbd_decode(&kbd, sc);

	if (c == '\b') {
		console_puts(cons, "\b \b");	/* the TTY lesson's erase */
	} else if (c > 0 && c < 0x100) {
		char s[2] = { (char)c, 0 };

		console_puts(cons, s);
	}
	pic_send_eoi(1);
}

void kmain(void)
{
	cons = VGA_CONSOLE;		/* the seam */
	console_init(cons, 0x0A);	/* bright green, obviously */

	kprintf("DuckOS 0.1 -- quack. it boots.\n\n");
	gdt_install();
	kprintf("[gdt] descriptors by gdt_encode, loaded via lgdt\n");
	idt_install();
	kprintf("[idt] gates by idt_gate, loaded via lidt\n");
	pic_remap();
	kprintf("[pic] remapped to 0x20-0x2F, IRQ 0+1 unmasked\n");
	pit_program(HZ);
	kprintf("[pit] divisor %u -> %u Hz\n", pit_divisor(HZ), HZ);
	kprintf("[cpu] sti -- type something!\n\n");
	kbd_init(&kbd);
	__asm__ volatile("sti");

	for (;;)
		__asm__ volatile("hlt");
}
```

The closing loop deserves its sentence: `hlt` stops the CPU until
the next interrupt. Your kernel burns no cycles idling — it sleeps
until the PIT or the keyboard has news, exactly the discipline the
scheduler lessons preached, in one instruction.

### Makefile

```
CC      = gcc
CFLAGS  = -m32 -std=c17 -ffreestanding -fno-pie -fno-stack-protector \
          -fno-builtin -fno-asynchronous-unwind-tables -Wall -Wextra -O1
LDFLAGS = -m32 -nostdlib -static -Wl,--build-id=none -T linker.ld

OBJS = boot.o isr.o klib.o console.o printf.o gdt.o idt.o \
       pic.o pit.o kbd.o kernel.o

duckos.elf: $(OBJS) linker.ld
	$(CC) $(LDFLAGS) -o $@ $(OBJS)

%.o: %.c duckos.h
	$(CC) $(CFLAGS) -c -o $@ $<

%.o: %.S
	$(CC) -m32 -c -o $@ $<

run: duckos.elf
	qemu-system-i386 -kernel duckos.elf

clean:
	rm -f $(OBJS) duckos.elf
```

The flags are the freestanding contract from *C With Nothing
Underneath*, now for real: `-ffreestanding -fno-builtin` (no libc
assumptions beyond your four functions), `-fno-pie` (the kernel is
linked to run at exactly 1 MiB), `-fno-stack-protector` (the
protector's runtime lives in libc — there is no libc).

## Run it

```
make run
```

A QEMU window opens on a black screen; your banner prints in green;
the boot log narrates each subsystem coming up; once a second a
tick line arrives via IRQ 0; and everything you type is echoed by
IRQ 1 through your scancode decoder — shift, caps, backspace (with
the `\b \b` erase from *The TTY and the Line Discipline*), the
lot. Every visible character on that screen went through
`console_putc`. Every one of those tick lines went through your
`kvsnprintf`, was timed by your `pit_divisor`, arrived through a
gate encoded by your `idt_gate`, in a segment described by your
`gdt_encode`.

## Where you go from here

The shim runs two of the course's modules on real metal and leaves
the rest as an exercise ramp, roughly in order of ambition: point
your frame allocator at the multiboot memory map (the loader leaves
one in memory — pass flag bit 6 and read it, exactly the E820 shape
from *Owning Physical Memory*); load your page directory into CR3
and flip the paging bit; forge a `struct cpu_context` (from
*Processes: the Kernel's Bookkeeping*) and write the twenty-line
assembly switch that makes your scheduler preempt for real; wire
`int 0x80` through a DPL-3 gate to your dispatch table. Each is a
weekend, none is magic, and you have already written the hard half
of every one.

Minix shipped to Tanenbaum's students as a working system they
could boot, read end to end, and change. That was the whole
pedagogical bet — and one Helsinki student took it further than
anyone planned. The bet is now yours to collect on: the OS on that
screen is small, honest, and entirely explicable, and every line of
it is either twelve bytes of magic number or code you wrote
yourself. Quack.

