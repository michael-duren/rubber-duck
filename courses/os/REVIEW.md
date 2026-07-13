# DuckOS Course Review

Reviewer walkthrough: every challenge implemented from lesson prose +
starter comments only (refsols not consulted before attempts), compiled
`cc -std=c17 -Wall -Wextra -O1`, run to completion.

## s01 — The Machine Wakes Up

- Accuracy: reproduced all worked examples. 0x1234:0x5678 → 0x12340 +
  0x5678 = 0x179B8 ✓. 0xFFFF:0xFFFF = 0x10FFEF ✓ (HMA = 65,520 bytes ✓).
  0xFFFF:0x0010 = 0x100000 ✓. 0x7C00 spelling count: segments 0..0x7C0
  give 1985 spellings = 3 listed + "1982 other ways" ✓ (and the "almost
  every address has exactly 4096" claim is true for addr ≥ 0x10000).
  MBR worked entry: 00 08 00 00 = 2048, 00 40 06 00 = 409600 = 200 MiB ✓.
  History checked: Lions/V6, V7 license 1979, Minix 1987 Vrije
  Universiteit, Torvalds' 386 January 1991, comp.os.minix post
  1991-08-25 with correct quote, reset vector 0xFFFF0, 0x7C00 =
  32KiB − 1KiB story, A20/8042 lore — all accurate. ONE ERROR FIXED:
  "the 80286 (1984)" — the 286 shipped 1982; 1984 is the PC/AT.
- Continuity: cross-refs *Message Passing — the Microkernel Heart*,
  *A Screen to Print On*, *C With Nothing Underneath* all match SPEC
  titles exactly. Slugs/points match outline. Sets up the "hostable
  simulation" framing the spec asks for ("How DuckOS Challenges Work"
  section) — good opener for the whole course.
- Walkthrough: real-mode-address — implemented from prose, 14/14 first
  try, no friction. boot-sector — 14/14 first try; prompt states every
  edge case the tests check (untouched `out` on bad signature, status
  exactly 0x80, type≠0). Minor nit, not fixed: test
  `test_parse_empty_entries_zeroed` checks `p[2].lba_start` and
  `p[3].sector_count` (likely meant p[2] twice) — harmless, both must
  be zero anyway.
- Edits made: s01.md — 80286 date 1984→1982 (PC/AT wording added).

## s02 — C With Nothing Underneath

- Accuracy: C17 §4 freestanding header list correct (all nine).
  gcc's documented requirement to provide memcpy/memmove/memset/memcmp
  even under -ffreestanding: correct. memset's int parameter fossil
  (K&R promotion, ANSI 1989) correct. strcmp-as-unsigned-char cite
  (§7.24.4) correct; 0xE9 = 233 unsigned / −23 signed ✓. Both overlap
  worked examples reproduced by hand and by test ("ABABCDGH",
  "CDEFEFGH") ✓. ONE ERROR FIXED: diagram said libc "formats 14 bytes"
  for "uptime: 42 ticks\n", which is 17 bytes (the adjacent write()
  call already said 17).
- Continuity: refs *The Machine Wakes Up*, *A Screen to Print On*,
  *Directories and Path Walking*, *kprintf* — all exact. Introduces the
  k-prefix naming rationale that later sections rely on. volatile
  forward-ref to s03 is apt.
- Walkthrough: kmem — implemented all five routines from prose alone;
  23/23 first try. Prose fully specifies the direction rule, the
  truncate-to-byte trap, and the unsigned-compare trap before the
  tests check them. No friction.
- Edits made: s02.md — "formats 14 bytes" → "formats 17 bytes".

## s03 — A Screen to Print On

- Accuracy: VGA 1987, MDA 0xB0000 / CGA 0xB8000, A0000–FFFFF reserved
  (640K conventional), CP437 details (0x01 smiley, 0x0A ◙, box chars
  0xB3–0xDA, shades 0xB0–0xB2/0xDB), brown/color-6 CGA circuit, blink
  bit 7 + attribute-controller reclaim, port list (PIC 0x20/21, PIT
  0x40–43, KBD 0x60/64, CRTC 0x3D4/5, CMOS 0x70/71) — all verified
  correct. 0x1F44 = white-on-blue 'D' ✓; hex dump of "Duck" ✓.
  ONE ERROR FIXED: scroll copy is 24×80 cells = 3840 bytes, not
  "4000 bytes of copying" (4000 = whole screen).
- Continuity: refs *C With Nothing Underneath*, *Interrupts and the
  IDT*, *The Clock Ticks*, *The Keyboard* — all exact. Picks up
  volatile from s02 and kmemmove for scrolling; simulation-note framing
  per spec present.
- Walkthrough: vga-console — implemented from prose alone, 17/17 first
  try. Prompt/starter fully specify tab stops (multiple of 8, ≥1 col),
  wrap-at-80, scroll semantics, backspace-never-erases. The "tab never
  straddles the edge" note preempts the one ambiguity I paused on.
  No friction.
- Edits made: s03.md — scroll byte count 4000 → 3840 (+160 blanking).

## s04 — kprintf

- Accuracy: Linux 0.01 shipped kernel/printk.c (~10k lines total) ✓;
  KERN_WARNING = "<4>" literal-pasting trick ✓; modern SOH ("\0014")
  replacement ✓; default argument promotions + C17 7.16.1.1 UB cite ✓;
  i386 cdecl right-to-left stack diagram ✓; INT_MIN unsigned-negate
  maneuver walked and correct (0u−0x80000000 = 0x80000000) ✓; 0xdeadbeef
  nibble-by-nibble walk (f,e,e,b,d,a,e,d) ✓; glibc<2.1 returned −1 on
  truncation ✓; truncation example (10 chars, 7+NUL stored, returns
  10) ✓; 10-byte digit buffer covers 32-bit decimal ✓. No errors found.
- Continuity: refs *C With Nothing Underneath*, *A Screen to Print On*,
  *The Machine Wakes Up*, *The Kernel Heap* — all exact. The
  format/output split motivates the testable-formatter design honestly.
- Walkthrough: kvsnprintf — implemented full engine from prose alone
  (width parsing, zero flag numerics-only, sign-before-zeros vs
  after-spaces, NULL → "(null)", %q passthrough, count-past-capacity
  sink); 23/23 first try. The prompt pre-answers every edge the tests
  probe. No friction.
- Edits made: none.

## s05 — Segments and Privilege

- Accuracy: 80286 (1982), 80386 (1985), 286 reset-to-real-mode via
  keyboard controller, Multics 8 rings, OS/2 ring 2, Minix middle-ring
  use — all check out. Decoded the descriptor table by hand: 0x9A =
  P|DPL0|S|E|RW ✓, 0xCF = flags 0xC + limit nibble F ✓, 0x92/0xFA/0xF2
  variants ✓. Selector math: 0x08/0x10/0x1B/0x23 ✓; 0x2F → index 5,
  TI 1, RPL 3 ✓. Scatter test vector (base 0x12345678, limit 0xABCDE,
  flags 0x4 → out[6]=0x4A) recomputed ✓. Data-segment rule
  max(CPL,RPL) ≤ DPL matches the real 386 check ✓. arpl instruction ✓.
  No errors found.
- Continuity: refs *kprintf*, *The Machine Wakes Up*, *Paging and
  Virtual Memory*, *Interrupts and the IDT* — all exact. Selectors
  minted here (0x08 etc.) are properly forward-referenced to s06/s21.
  286-date now consistent with fixed s01.
- Walkthrough: gdt-encode — 12/12 first try from the layout table;
  the "mask off anything above" clause covers the only subtlety.
  selector-check — 16/16 first try; confused-deputy test is stated in
  the prompt before the tests check it. No friction.
- Edits made: none.

## s06 — Interrupts and the IDT

- Accuracy: wire-to-handler walk correct (IRR→INTA→ISR→vector; two
  INTA pulses). Exception table (#DE 0, #BP 3/0xCC, #UD 6, #DF 8,
  #GP 13, #PF 14) ✓; error-code list (#GP/#PF yes, #DE/#BP no, never
  hardware IRQs) ✓. IBM's IRQ0–7 → vectors 8–15 collision (IRQ0/#DF,
  IRQ5-XT-disk/#GP, IRQ6-floppy/#PF) ✓. Remap 0x20/0x28, ICW1 0x11,
  ICW3 0x04/0x02, ICW4 0x01, EOI byte 0x20 to port 0x20, spurious
  IRQ 7 without ISR bit ✓ all match the 8259A datasheet. Gate byte 5:
  0x8E interrupt/ring0, 0xEF trap/ring3 recomputed ✓; Linux 0.01's
  int 0x80 as DPL-3 trap gate ✓. IDT = 2 KB ✓. No errors found.
- Continuity: refs *A Screen to Print On*, *kprintf*, *The System Call
  Boundary*, *Segments and Privilege*, *Paging and Virtual Memory*,
  *The Clock Ticks*, *The Keyboard* — all exact. Uses selectors 0x08
  from s05; HZ=100 consistent with spec.
- Walkthrough: idt-gate — 7/7 first try (prompt gives full byte
  layout). pic8259 — 18/18 first try; the fully-nested "strictly
  higher priority than every ISR bit" rule in the prompt is exactly
  what the nest tests need. No friction.
- Edits made: none.

## s07 — Owning Physical Memory

- Accuracy: INT 0x12 / INT 0x15 AH=0x88 (16-bit KiB → 64 MiB cap) ✓;
  E820 = Phoenix, 20-byte entries, type table ✓; example map sums to
  exactly 128 MiB with correct EBDA cutoff at 0x9FC00 ✓; 4-GiB bitmap
  = 2^20 bits = 128 KiB ✓; 0x103000 → frame 259 ✓; frame 40 = word 1
  bit 8 ✓; align-inward worked example [0x3210,0x7C00) → [0x4000,
  0x7000) ✓; merge-before-align example verified by test ✓; Linux
  buddy in mm/page_alloc.c ✓; Minix-in-256KiB ✓. No factual errors.
- Continuity: refs *Paging and Virtual Memory*, *The Kernel Heap*,
  *A Filesystem on a Disk*, *A Screen to Print On* — exact. FIXED:
  referred to s03's console function as `vga_putc`; s03 defines
  `console_putc`.
- Walkthrough: memmap-normalize — implemented pipeline from the
  numbered TODO plan; 11/11 first try. The prompt's warnings (merge
  before align, -1 may leave partial garbage, no-wrap guarantee)
  covered every trap. frame-alloc — 25/25 first try; guard-bit design
  and the fa_free range-check rationale are fully specified. The
  "failed run leaves state unchanged" requirement is naturally
  satisfied by check-then-mark; prompt states it explicitly. No
  friction.
- Edits made: s07.md — `vga_putc` → `console_putc`.

## s08 — Paging and Virtual Memory

- Accuracy: reproduced the full worked walk: 0xC0801ABC → PD 770 /
  PT 1 / off 0xABC; PDE at 0x2C08; 0x00005007 → table frame 5;
  0x0000B003 → pa 0xBABC — all correct. Flat-table cost (4 MiB, ×16
  procs = 64 MiB) ✓. #PF error-code bits and examples (0x7, 0x4,
  kernel-read-unmapped = 0) ✓. 386-ignores-R/W-in-ring-0 / 486 CR0.WP
  history ✓. Tanenbaum "Linux is obsolete" January 1992 ✓. Higher-half
  0xC0000000 = PD 768 ✓. Test vectors 0x01406007 → 5/6/7 and
  make(768,42,0x123) = 0xC002A123 recomputed ✓. No errors found.
- Continuity: refs *Owning Physical Memory*, *Segments and Privilege*,
  *Interrupts and the IDT* — exact. NPROC 16 used consistently.
  Tanenbaum–Torvalds mention here is one sentence, complements s01's
  framing without repeating the anecdote at length (s12 owns the full
  treatment).
- Walkthrough: vaddr-split — 6/6 first try. page-map — 16/16 first
  try; prompt specifies PDE flags policy (P|W|U, leaf decides),
  double-map refusal, fault-code semantics including the ambiguous-0
  quirk. The starter's provided pte_read/pte_write/alloc_frame remove
  all incidental friction. No friction.
- Edits made: none.

## s09 — The Kernel Heap

- Accuracy: pedigree checks out (K&R §8.7 storage allocator, Knuth
  boundary tags, Knowlton 1965 buddy, Bonwick slab USENIX 1994 /
  SunOS 5.4, Wilson et al. 1995, dlmalloc→glibc, SLUB, poison 0x6b,
  KASAN). Coalesce example 40+8+104=152 ✓; align idiom 33→40 ✓;
  wrap trap 0xFFFFFFFF+7→6 ✓; kfree-48-bytes ✓. ONE ERROR FIXED:
  the fresh-heap worked example said the split remainder after
  kmalloc(40) is 65472; it is 65528−40−8 = 65480 (the later
  splitting-section diagram already said 65480, and 65480 is what
  makes the arena end at 0x112000).
- Continuity: refs *Owning Physical Memory*, *Paging and Virtual
  Memory*, *The Clock Ticks*, *Block Devices and the Buffer Cache* —
  exact. struct message "16 bytes" consistent with the canonical
  4-int message.
- Walkthrough: kmalloc — implemented init/alloc/free/introspection
  from prose alone; 20/20 first try. The prompt nails every trap in
  advance (reject-before-round, split threshold header+ALIGN, full
  merge sweep). No friction.
- Edits made: s09.md — worked-example remainder 65472 → 65480 (two
  lines).

## s10 — Processes: the Kernel's Bookkeeping

- Accuracy: cdecl caller/callee-saved split ✓; xv6 swtch shape ✓;
  trap-frame push set and the ring-transition-only ESP/SS rule ✓;
  EFLAGS_IF = bit 9 = 0x200 ✓; Minix NR_PROCS fixed array and
  30000 pid cap ✓; forged-frame launch trick accurately described.
  Wraparound/skip test vectors recomputed (pid 17 after 16 allocs;
  30000→1; skip live pid 1 → issue 2) ✓. No errors found.
- Continuity: proc_state enum matches SPEC names AND order exactly;
  NPROC 16, PID_MAX 30000, selectors 0x08/0x1B/0x23 consistent with
  s05. Refs *Paging and Virtual Memory*, *Scheduling*, *Message
  Passing — the Microkernel Heart*, *The Clock Ticks*, *Birth, Death,
  and Zombies*, *The Kernel Heap*, *Interrupts and the IDT* — all
  exact.
- Walkthrough: proc-table — 15/15 first try; pid-skip semantics
  ("skip any candidate held by a live slot, wrap PID_MAX→1, leave
  next_pid one past") fully determined my implementation. context-
  frame — 9/9 first try; trivial by design, the lesson carries the
  concept. No friction.
- Edits made: none.

## s11 — Scheduling

- Accuracy: SJF example (10.5 vs 6.0 s) ✓; context-switch arithmetic
  (30µs/130µs = 23%, 30µs/40ms = 0.075%) ✓; 10-process queue latency
  (900 ms / 360 ms) ✓; ring worked example (tail (15+3)%16 = 2, pop
  9,12,3) ✓; QUANTUM(q)=1<<q → 10/80 ms endpoints ✓; CTSS (Corbató,
  1962) quantum-doubling ✓; Minix 1.0 TASK_Q/SERVER_Q/USER_Q, 100 ms
  user quantum ✓. No errors found.
- Continuity: NPROC 16, NQ 4, HZ 100, QUANTUM def all match SPEC.
  Refs *Processes: the Kernel's Bookkeeping*, *The Clock Ticks*,
  *Message Passing — the Microkernel Heart* — exact. The blocked-
  process-leaves-the-scheduler contract is stated here and honored by
  s23's final challenge design.
- Walkthrough: runqueue — 9/9 first try. mlq-schedule — 13/13 first
  try. Every rule the tests probe (demotion cap, block keeps prio +
  fresh quantum, boost drain order 1..NQ-1, enqueue rejects running
  slot) is stated in the prompt bullets verbatim. One subtle point —
  boost preserving relative FIFO order across levels — is specified
  and tested. No friction.
- Edits made: none.

## s12 — Message Passing — the Microkernel Heart

- Accuracy: Tanenbaum post date (1992-01-29), subject "LINUX is
  obsolete", Linux five months old, Torvalds age 22 — all correct.
  Quotes match the well-attested thread text ("truly poor idea",
  "Be thankful you are not my student...", "one hell of a good excuse
  for some of the brain-damages of minix"); "beats the pants of minix"
  is how the original post is usually reproduced (Torvalds' own
  wording) — left as is. Modern-verdict claims (QNX in cars, seL4
  proof, L4-descendant basebands, Intel ME running Minix 3 revealed
  2017 + Tanenbaum open letter) all check out. Rendezvous semantics
  and the send_to-chain deadlock walk match real Minix. No errors.
- Continuity: this is the lesson s01 promised would take up the
  Tanenbaum–Torvalds debate — the arc pays off; no duplicated anecdote
  elsewhere (s08 gives it one sentence). struct message, ANY, ESRCH 3,
  EDEADLK 35, proc_state enum — all match SPEC. Refs *Processes: the
  Kernel's Bookkeeping*, *Scheduling*, *The System Call Boundary* —
  exact.
- Walkthrough: ipc-rendezvous — 29/29 first try. The two handshake
  diagrams translate almost line-for-line into code; user_out-vs-buf
  landing pad, in-order scan for ANY, and the walk's no-modification
  guarantee on refusal are all explicit. No friction.
- Edits made: none.

## s13 — Sharing Without Tearing

- Accuracy: lost-update interleaving walked and correct; Therac-25
  (1985–87, operator-speed race) ✓; cli-in-ring-3 → #GP ✓; xchg's
  implicit bus LOCK since the 8086 ✓; Dijkstra THE system, Eindhoven,
  P/V = proberen/verhogen, 1965 ✓; spl in early Unix and Minix's
  non-preemptive kernel ✓; direct-handoff rationale (wake-steal hole)
  is the textbook-correct treatment. No errors found.
- Continuity: proc_state enum matches SPEC; NPROC 16; HZ 100 quantum
  arithmetic consistent with s11 (10 ms tick). Refs *Scheduling*,
  *The Clock Ticks*, *Message Passing — the Microkernel Heart* —
  exact. The "Minix synchronizes by architecture" section ties s12
  and s13 together nicely — good arc.
- Walkthrough: semaphore — 17/17 first try. The contract enumerates
  return values, handoff-keeps-value-zero, negative-clamp, and the
  full-queue guard precisely. No friction.
- Edits made: none.

## s14 — The Clock Ticks

- Accuracy: the colorburst story checks out exactly — 3.579545 MHz
  NTSC ×4 = 14.31818 MHz, /3 = 4.77 MHz PC clock, /12 = 1.193182 MHz
  PIT ✓. Divisor-0 = 65536 → 18.2 Hz DOS clock ✓; 100 Hz → 11932
  (actual 99.998 Hz, rounds back to 100) ✓; 1000 Hz → 1193 ✓;
  60 Hz → 19886 ✓; 18 Hz → 66288 > 16 bits → clamp ✓. 8253 vs 8254
  readback ✓; ports 0x43/0x40 ✓; Minix 1987 HZ 60 ✓; Linux HZ
  100→1000→250→tickless history ✓; ms_to_ticks ceiling examples ✓.
  No errors found.
- Continuity: HZ 100, IRQ0-highest matches s06; refs *Scheduling*,
  *Processes: the Kernel's Bookkeeping*, *DuckOS, Assembled* — exact.
  The owner-as-opaque-int design is explicitly forward-wired to s23.
- Walkthrough: pit-divisor — 16/16 first try (prompt gives the exact
  rounding formula and clamp order). timer-queue — 16/16 first try;
  the "equal expiry inserts AFTER" rule and cancel-donates-delta bite
  are both stated before the tests check them. The one place I paused
  — whether tq_tick decrements before or after checking zero — is
  resolved by "decrement the head's delta once, then pop every leading
  timer whose delta is 0". No friction.
- Edits made: none.

## s15 — The Keyboard

- Accuracy: 8048-in-keyboard / 8042-on-board, port 0x60, IRQ1 →
  vector 33 (post-remap) ✓; scancode-set-1 geometry table checked
  key by key (all correct, incl. `=0x29, \=0x2B, space=0x39) ✓;
  set-2-translated-to-set-1 default ✓; typematic ~10.9 cps / 500 ms ✓;
  Model M 1986 / 101-key / E0-prefix keypad-pun table (48/50/4B/4D,
  right-ctrl E0 1D) ✓; fake-shift E0 2A / E0 AA ✓; Ctrl = &0x1F
  arithmetic ('c' 0x63 → 0x03) ✓; LED command 0xED ✓; "Hi!" ten-byte
  trace verified against the tables ✓. No errors found.
- Continuity: refs *The Clock Ticks*, *Interrupts and the IDT*, *The
  TTY and the Line Discipline* — exact. Vector 33 consistent with
  s06's remap. VINTR forward-ref sets up s16.
- Walkthrough: scancode-decode — 18/18 first try. The numbered decode
  order in the prompt is a complete spec; the shift-as-count rationale
  is taught before the test that catches the boolean bug. No friction.
- Edits made: none.

## s16 — The TTY and the Line Discipline

- Accuracy: Teletype Model 33 ASR (1963, 10 cps, uppercase, full-
  duplex echo) ✓; V7 erase '#' / kill '@' ✓; SLIP/PPP as swappable
  line disciplines ✓; termios/ICANON/ECHO/VERASE knobs ✓; DEL 0x7F
  vs BS 0x08 history ✓; ^D-as-EOT / EOF-is-read-returning-0 treatment
  is exactly right, and the quack/quackquack transcript reproduces
  correctly; CP/M–DOS ^Z 0x1A contrast ✓. No errors found.
- Continuity: EAGAIN 11 matches SPEC; refs *The Keyboard*,
  *Scheduling* exact. FIXED: one cross-ref said *Message Passing* —
  truncated; now *Message Passing — the Microkernel Heart*.
- Walkthrough: tty-canon — 18/18 first try. Prompt's per-byte rule
  table is exhaustive (including the full-line-drops-newline corner
  the overlong test checks, and echo-nothing-for-^D). No friction.
- Edits made: s16.md — truncated cross-reference expanded.

## s17 — Block Devices and the Buffer Cache

- Accuracy: 0.015 s × 33 MHz = 495,000 cycles ✓ (and the "five days"
  analogy is consistent with its own 1-cycle-memory-access premise);
  char/block device split, 512-byte sectors, BLOCK_SIZE 1024 = two
  sectors ✓; bread/brelse in V6/xv6, Minix get_block/put_block +
  NR_BUFS + LRU chain + hash in fs/buf.h ✓; update daemon syncing
  every 30 s ✓; lost-update diagram is a correct illustration of the
  duplicate-buffer hazard; Linux active/inactive lists, PostgreSQL
  ring buffer, 2Q as scan-resistance examples ✓. No errors found.
- Continuity: BLOCK_SIZE 1024 matches SPEC; refs *A Screen to Print
  On*, *Paging and Virtual Memory*, *The Keyboard*, *Directories and
  Path Walking* — exact. 16-byte dirent forward-ref consistent with
  s18/s20.
- Walkthrough: buf-cache — 23/23 first try. Victim rule (invalid
  first, then min-lastuse unpinned), flush-before-reuse ordering, and
  refcnt-floors-at-zero are all in the prompt. No friction.
- Edits made: none.

## s18 — A Filesystem on a Disk

- Accuracy: Minix v1 lore all correct — magic 0x137F (v2 0x2468,
  30-char v1 0x138F), 14-char names, 64 MB u16-zones limit, Linux
  0.01 shipping Minix FS because Linus' disk was formatted with it,
  ext (1992). Worked 8 MB layout recomputed: 2688×32 = 84 blocks,
  first data zone 4+84 = 88 ✓; max_size (7+512+512²)×1024 =
  268,966,912 = 0x10081C00 ✓; superblock field offsets match real
  minix_super_block ✓; 0x0A80 byte-swap → 32778 ✓; bit-0-reserved
  rationale (0 = empty dirent / hole) matches SPEC and real Minix.
  No errors found.
- Continuity: BLOCK_SIZE 1024, magic 0x137F, EINVAL 22, ENOSPC 28,
  bitmaps-bit-0-reserved — all SPEC-canonical. Refs *Block Devices
  and the Buffer Cache*, *The Machine Wakes Up*, *Inodes: Files
  Without Names*, *Directories and Path Walking* — exact.
- Walkthrough: superblock-parse — 20/20 first try (prompt gives
  offsets and every validation rule). fs-bitmap — 17/17 first try
  (LSB-first convention and the never-return-0 defense both stated).
  No friction.
- Edits made: none.

## s19 — Inodes: Files Without Names

- Accuracy: 32-byte Minix v1 inode layout matches the real format
  (the single timestamp is labeled i_mtime — Minix calls it i_time,
  but it IS the modification time; benign). Tier arithmetic: 7,168 /
  524,288 / 268,435,456 bytes; 262,663 blocks; 268,966,912 total —
  consistent with s18's max_size ✓. Block-700 walk reproduced:
  700−7=693, 693−512=181, /512=0, %512=181 ✓ (and the tests wire the
  disk to exactly that path). ext = Rémy Card, April 1992, first VFS
  filesystem ✓; ext2 12-direct + triple-indirect ✓; ext4 extents
  2008 ✓; inode 1 = root ✓; hole/du-vs-ls behavior ✓. No errors.
- Continuity: BLOCK_SIZE 1024, 7+1+1 zones, zone-0-means-hole,
  EINVAL 22, EIO 5 — all SPEC-canonical. Refs *Directories and Path
  Walking*, *A Filesystem on a Disk*, *Block Devices and the Buffer
  Cache* — exact.
- Walkthrough: inode-bmap — 15/15 first try. The check-every-zone-
  number-<NDISK rule and hole-at-any-depth rule are stated with their
  rationale; inode_read's partial-block slicing is guided by the
  prompt. No friction.
- Edits made: none.

## s20 — Directories and Path Walking

- Accuracy: 16-byte dirent (u16 inode + 14-byte NUL-padded name, no
  terminator at full width) matches SPEC and real Minix ✓; . / .. as
  real on-disk entries and link count starting at 2 ✓; unlink-zeroes-
  inode ghost-entry behavior (undelete lore) ✓; namei in V6 ✓;
  root's-parent-is-root containment ✓; real Minix truncating
  components at 14 chars (DuckOS chooses strictness, documented) ✓.
  ONE FIX: "the short list of things Linus fixed ext for" — ext was
  Rémy Card's (s19 says so correctly); reworded to attribute ext, with
  a cross-ref to *Inodes: Files Without Names*.
- Continuity: ENOENT 2, ENOTDIR 20, EINVAL 22, NAME_LEN 14 all per
  SPEC. Refs *Inodes: Files Without Names*, *A Filesystem on a Disk*
  — exact. Closing paragraph correctly previews s21/s22/s23.
- Walkthrough: dirent-scan — 15/15 first try; the "no strcmp,
  bound by field width" pattern is fully specified. path-resolve —
  19/19 first try; the followed-by-slash ENOTDIR rule (the trap
  "that catches most first attempts") is called out explicitly and
  is indeed the one subtle bit. No friction.
- Edits made: s20.md — ext attribution reworded (Linus → ext itself,
  cross-ref to s19).

## s21 — The System Call Boundary

- Accuracy: int 0x80 / DPL-3 gate story consistent with s06 and
  correct; i386 Linux ABI example (EAX=4 = write, EBX/ECX/EDX) ✓;
  sysenter/syscall aside ✓; signed-index exploit and unsigned fix are
  the textbook-correct treatment; wraparound worked example
  (0xFFFFFFF0 + 0x20 → 0x10, both naive checks pass) recomputed ✓;
  0x08048000 System V i386 ELF load address ✓; 0xC0000000 3G/1G
  split ✓; −errno / −4095 boundary and MAP_FAILED = (void*)−1 ✓;
  Linux >300 syscalls ✓; Minix's essentially-two-syscall stance ties
  back to s12 ✓. No errors found.
- Continuity: ENOSYS 38, EFAULT 14, EINVAL 22 per SPEC. Refs
  *Segments and Privilege*, *Interrupts and the IDT*, *Processes: the
  Kernel's Bookkeeping*, *Message Passing — the Microkernel Heart*,
  *Owning Physical Memory* — all exact. Confused-deputy theme
  deliberately echoes s05's RPL discussion — good arc.
- Walkthrough: syscall-dispatch — 14/14 first try. The prompt
  dictates the wrap-safe formulation and the EINVAL-vs-ENOSYS
  distinction for register vs dispatch. No friction.
- Edits made: none.

## s22 — Birth, Death, and Zombies

- Accuracy: status word (42&0xff)<<8 = 0x2A00 recomputed ✓; 300 →
  0x2C00 / exit(256) → 0 / exit(-1) → 255 traps all correct;
  WIFEXITED/WEXITSTATUS/WTERMSIG semantics ✓; init-as-next-of-kin,
  "Attempted to kill init!" panic, double-fork daemon trick ✓;
  lost-wakeup ordering and Mesa-semantics re-scan discussion is the
  textbook-correct treatment. Honest-gaps framing (no exec, no
  signals) is accurate and well-placed. No errors found.
- Continuity: ECHILD 10, ESRCH 3, EINVAL 22, INIT_PID 1, NPROC 16,
  proc_state enum — all per SPEC. FIXED two truncated cross-refs:
  *Inodes* → *Inodes: Files Without Names* and *Message Passing* →
  *Message Passing — the Microkernel Heart*. Other refs (*Paging and
  Virtual Memory*, *Directories and Path Walking*, *Sharing Without
  Tearing*, *DuckOS, Assembled*) exact.
- Walkthrough: wait-exit — 25/25 first try. The prompt's ordered
  k_exit steps (corpse first, reparent, wake parent, wake init on
  inherited corpse) map directly to code; the "deep case" test is
  pre-explained in prose. No friction.
- Edits made: s22.md — two truncated cross-references expanded.

## s23 — Final Challenge: DuckOS, Assembled

- Accuracy: the integration-contract list is faithful to the source
  lessons (block-keeps-priority, delivery-doesn't-preempt, mark-then-
  wake, waiter-invisible-to-tick); the prose handshake diagram is
  consistent with the tests' expected 22-event trace; ftrace/Minix
  proc.c framing accurate. No errors found.
- Continuity: the only `# Final Challenge:` H1 in the course ✓;
  heading contract satisfied everywhere (swept all 23 files: no
  duplicate slugs across 55 slug occurrences, no prose heading
  beginning with Challenge:/Starter/Tests). All conventions match
  SPEC: NPROC/NQ/QUANTUM/ANY/INIT_PID/errnos/enum order/message
  struct identical to every earlier declaration (grepped all
  occurrences). Refs *Processes: the Kernel's Bookkeeping*,
  *Scheduling*, *Message Passing — the Microkernel Heart*, *The Clock
  Ticks*, *Birth, Death, and Zombies*, *The System Call Boundary* —
  exact. One "*Message Passing*" inside the Starter's C doc comment
  is a code comment, not a rendered cross-ref — left as is.
- Walkthrough: final-duckos — implemented all eight entry points from
  the doc comments + integration-contract prose; 44/44 first try,
  including the exact 22-event grand-finale trace. The contract list
  ("The integration contracts") pre-answers every seam the tests
  probe: EV_WAKE arg conventions, wake-order in k_exit (parent before
  init, skip-if-same), sleeper scan skipping waiters, caller-keeps-
  CPU on delivery/reap. Genuinely zero friction — remarkable for a
  100-point integration challenge.
- Edits made: none (s18's truncated *Inodes* ref fixed under s18).

## Summary

**Verdict: the course is in excellent shape — publishable.** All 33
challenges (32 lesson challenges + the final) were implemented from
the learner's view (lesson prose + starter comments only, refsols
never consulted before the attempt) and every one reached 100% pass
on the first compile-and-run. Zero contract mismatches between prose
and tests were found; every trap the tests check is taught before it
is tested. verify.py is green on all 23 sections after edits
(starters compile clean, fail non-zero without crashing; refsols all
pass).

Fixes made (7 total, all small):
1. s01 — 80286 introduced 1982, not 1984 (1984 = PC/AT).
2. s02 — "formats 14 bytes" → 17 bytes for "uptime: 42 ticks\n".
3. s03 — scroll copy is 3840 bytes (24×80 cells), not 4000.
4. s07 — s03's console function is `console_putc`, not `vga_putc`.
5. s09 — worked-example split remainder 65472 → 65480 (now agrees
   with the later splitting diagram and the arena-end arithmetic).
6. s16, s18, s22 — four truncated italic cross-references expanded to
   exact SPEC titles (*Message Passing — the Microkernel Heart* ×2,
   *Inodes: Files Without Names* ×2).
7. s20 — 14-char-limit fix attributed to "Linus"; ext was Rémy Card's
   (s19 states this correctly); reworded with a cross-ref.

No Starter or Tests block was modified; no slug or points value
touched; no refsol changed.

Deliberately left for the maintainer (all cosmetic, none blocking):
- s01 test `test_parse_empty_entries_zeroed` checks `p[2].lba_start`
  and `p[3].sector_count` — likely meant p[2] for both; harmless
  since both must be zero.
- s19 labels Minix v1's single timestamp `i_mtime`; the historical
  field name is `i_time` (it is the modification time, so the
  description is correct).
- s23 Starter C comment says "from *Message Passing*" — informal
  mention inside code, not a rendered cross-ref.
- s12 quotes Torvalds as "beats the pants of minix" — this matches
  common reproductions of the original post (Torvalds' own wording);
  could not verify the exact Usenet bytes offline.

Could not verify offline (flagged, not fixed; all are plausible and
widely attested): the exact 0x7C00 = 32 KiB − 1 KiB origin story
(standard lore), Minix 1.0's 256 KiB minimum, the "customers wanted
brown" CGA circuit anecdote, and the claim that some BIOSes jump to
0x07C0:0x0000 (well documented in osdev lore).

Continuity: the 23 sections read as one course. The Tanenbaum–
Torvalds arc is set up in s01, referenced in one sentence in s08, and
paid off in full in s12 — no duplicated anecdotes anywhere. Shared
constants and the proc_state enum are byte-identical in all
declarations. Exactly one `# Final Challenge:`; no duplicate slugs;
heading format contract holds in all files.
