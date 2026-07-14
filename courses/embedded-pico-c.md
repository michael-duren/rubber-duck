---
course: embedded-pico
title: Raspberry Pi Pico W — From Registers to a Network Scanner
language: c
description: >
  Start bare-metal on the RP2040 — memory-mapped registers, GPIO, and a
  blinking LED with no SDK — then build real firmware for the Pico W: join
  WiFi, scan the network, and stream every result over a TCP socket to a
  listener on your laptop. You'll wire an LED on a breadboard, flash real
  hardware, and solve the problems that only show up on the wire: framing,
  retries, checksums, and a WiFi link that drops mid-scan.
duration_hours: 10
tags: [embedded, hardware, c, networking, wifi, pico-w]
extended_reading:
  - title: RP2040 Datasheet (SIO chapter 2.3.1, GPIO chapter 2.19)
    url: https://datasheets.raspberrypi.com/rp2040/rp2040-datasheet.pdf
  - title: Raspberry Pi Pico C/C++ SDK (build, flash, cyw43, lwIP)
    url: https://datasheets.raspberrypi.com/pico/raspberry-pi-pico-c-sdk.pdf
  - title: Connecting to the Internet with Raspberry Pi Pico W (WiFi + lwIP)
    url: https://datasheets.raspberrypi.com/picow/connecting-to-the-internet-with-pico-w.pdf
  - title: lwIP raw TCP API (tcp_new / tcp_connect / tcp_write / tcp_recv)
    url: https://www.nongnu.org/lwip/2_1_x/group__tcp__raw.html
---

# Lesson: Setting Up Your Environment {#environment-setup}

This course has two halves. In the first you learn to talk to the RP2040 the
way the silicon does — no SDK, just addresses from the datasheet — ending with a
blinking LED you build register by register. Those challenges are graded **in
your browser**: pure C that compiles and runs on the grader, so you need nothing
installed to start them. In the second half you put a real **Pico W** on your
desk and flash that knowledge onto it — joining WiFi, scanning the network, and
streaming results to a listener on your laptop over a TCP socket you write
yourself.

The reusable *logic* stays browser-testable (address math, subnet math, framing
a byte stream, checksums, a retry state machine — that is how real embedded
teams work: test the logic on the host, flash the glue). But to actually
**build and flash firmware** you need a cross-compiler toolchain on your
machine, and setting it up is the single step most people get wrong. So do it
now, before you're itching to flash — get a known-good example building first,
and the rest of the course just works.

Raspberry Pi ships a VS Code extension that automates all of this. This lesson
deliberately walks the **manual** path instead: it's editor-agnostic (Neovim,
CLion, plain `make` — anything), it works over SSH and in headless CI, and
understanding what the extension does for you is what lets you fix a build when
it breaks. If you *do* want the extension, install "Raspberry Pi Pico" in VS
Code, let it fetch the SDK and toolchain, and skip ahead — but skim this so you
know what it set up.

## What you're actually installing

Your laptop's CPU is x86-64 or Apple-silicon ARM; the Pico's is a 32-bit Arm
Cortex-M0+ (Cortex-M33 on the Pico 2 W). You can't run its code locally, so you
**cross-compile**: a compiler that runs on your machine but emits Cortex-M
binaries. Three pieces:

- **`arm-none-eabi-gcc`** — the cross-compiler and linker ("none-eabi" =
  bare-metal, no operating system).
- **CMake plus a build tool** (`make` or Ninja) — the SDK is CMake-based; CMake
  generates the build, `make` runs it.
- **The Pico SDK** — Raspberry Pi's C/C++ SDK: headers, libraries, the CMake
  glue, and — as git submodules — the WiFi driver (`cyw43`) and TCP/IP stack
  (`lwIP`) the second half of this course needs.

## Step 1 — get the SDK (with its submodules)

Clone it somewhere stable (your home directory is fine) and initialize its
submodules. **The submodules are not optional here** — lwIP and the CYW43 driver
live in them, and without them every WiFi build fails with missing `lwip/…`
headers:

```bash
git clone https://github.com/raspberrypi/pico-sdk.git ~/pico-sdk
cd ~/pico-sdk
git submodule update --init          # fetches lwIP, cyw43, tinyusb, …
```

`git clone --recurse-submodules https://github.com/raspberrypi/pico-sdk.git
~/pico-sdk` does both in one command if you prefer.

## Step 2 — install the toolchain

### Linux, and Windows via WSL

On Windows, **don't build natively** — install WSL 2 with Ubuntu (`wsl --install`
in an admin PowerShell, reboot, finish Ubuntu's first-run setup) and do
everything inside that Ubuntu shell. From here on, "Linux" and "WSL" are the
same thing; the only differences are USB flashing and serial, covered at the end
of this lesson.

On Debian/Ubuntu (native or WSL):

```bash
sudo apt update
sudo apt install cmake gcc-arm-none-eabi libnewlib-arm-none-eabi \
                 libstdc++-arm-none-eabi-newlib build-essential git python3
```

That is the exact package set from Raspberry Pi's SDK docs — `cmake`,
`gcc-arm-none-eabi`, `libnewlib-arm-none-eabi`, `libstdc++-arm-none-eabi-newlib`
— plus `build-essential`, `git`, and `python3`, which the SDK's own build steps
(picotool, pioasm) rely on.

### macOS

Install [Homebrew](https://brew.sh) if you haven't, then:

```bash
brew install cmake
brew install --cask gcc-arm-embedded    # the Arm GNU embedded toolchain
```

Use the **`gcc-arm-embedded` cask**, *not* the older
`brew tap ArmMbed/homebrew-formulae && brew install arm-none-eabi-gcc` route:
that tap is deprecated and produces a `cannot read spec file 'nosys.specs'`
error at link time. The cask is the reliable path on both Intel and
Apple-silicon Macs.

## Step 3 — point your builds at the SDK

CMake finds the SDK through the **`PICO_SDK_PATH`** environment variable. Set it
in your shell profile so every new terminal has it:

```bash
echo 'export PICO_SDK_PATH="$HOME/pico-sdk"' >> ~/.bashrc   # ~/.zshrc on macOS
source ~/.bashrc
echo "$PICO_SDK_PATH"        # should print /home/you/pico-sdk
```

(Alternatively pass `-DPICO_SDK_PATH=~/pico-sdk` to every `cmake` invocation —
the environment variable just saves you repeating it.)

Each project also needs one file copied in from the SDK:
**`pico_sdk_import.cmake`**, which teaches CMake how to locate and initialize the
SDK. Copy it out of the SDK's `external/` directory into your project folder:

```bash
cp "$PICO_SDK_PATH/external/pico_sdk_import.cmake" .
```

In `CMakeLists.txt` it must be `include`d **before** your `project()` line, with
`pico_sdk_init()` called **after** it. You'll see this exact shape in the
`CMakeLists.txt` later in this course — this is why the order matters:

```cmake
include(pico_sdk_import.cmake)   # BEFORE project()
project(myapp C CXX ASM)
pico_sdk_init()                  # AFTER project()
```

## Step 4 — prove it works before you trust it

Don't wait until you've written firmware to discover the toolchain is broken.
Build Raspberry Pi's own examples now as a smoke test — if this produces a
`.uf2`, your whole setup is correct:

```bash
git clone https://github.com/raspberrypi/pico-examples.git ~/pico-examples
cd ~/pico-examples
cmake -B build -DPICO_BOARD=pico_w .          # -DPICO_BOARD=pico_w is essential
cmake --build build --target picow_blink      # the Pico W blink example
ls build/pico_w/blink/picow_blink.uf2         # ← success looks like this
```

`-DPICO_BOARD=pico_w` selects the Pico W's pin map and pulls in the radio; leave
it off and you build for a plain Pico and hit the "the LED isn't on GPIO 25"
surprise later. (The classic `mkdir build && cd build && cmake ..` from the docs
does the same thing; `cmake -B build` / `cmake --build build` is just the newer
spelling this course uses.) A `.uf2` on disk means you can compile, link, and
package for the board — you're done here. Flashing that file onto real hardware
is the next lesson but one.

## WSL: flashing and serial

Two things behave differently under WSL, because WSL doesn't see USB devices by
default:

- **Flashing is easy anyway.** Put the Pico in BOOTSEL mode (a later lesson) and
  its `RPI-RP2` drive appears in **Windows** Explorer, not in WSL. Build in WSL,
  then drag the `.uf2` onto that drive from Windows — that split is completely
  normal. (Keep your project on the Linux filesystem, `~/…`, not `/mnt/c/…`;
  builds are far faster there.)
- **Serial (`printf` over USB) needs a bridge.** The Pico's `/dev/ttyACM0`
  won't appear in WSL unless you attach it with
  [`usbipd-win`](https://learn.microsoft.com/windows/wsl/connect-usb)
  (`usbipd list`, then `usbipd attach --wsl --busid <id>`). Simpler: read the
  serial port from a Windows terminal such as PuTTY instead. On native Linux and
  macOS it just appears — `/dev/ttyACM0` on Linux, `/dev/tty.usbmodem*` on macOS
  — and `sudo apt install minicom` then `minicom -b 115200 -o -D /dev/ttyACM0`
  reads it.

## If you'd rather automate it

Two shortcuts, both fine:

- **VS Code extension** — "Raspberry Pi Pico" fetches the SDK, toolchain, and
  build configuration through a GUI. Good if you live in VS Code.
- **`pico_setup.sh`** — Raspberry Pi's all-in-one script that installs the
  dependencies and clones the SDK and examples for you:
  ```bash
  wget https://raw.githubusercontent.com/raspberrypi/pico-setup/master/pico_setup.sh
  chmod +x pico_setup.sh
  ./pico_setup.sh
  ```
  Handy on a fresh Raspberry Pi OS or Ubuntu box — it does Steps 1–3 above.

## When it goes wrong

| Symptom | Cause and fix |
|---|---|
| `fatal error: lwip/….h: No such file` | SDK submodules missing — `cd ~/pico-sdk && git submodule update --init`. |
| CMake can't find the SDK / `PICO_SDK_PATH` unset | Not exported in this shell — redo Step 3, or pass `-DPICO_SDK_PATH=`. |
| `cannot read spec file 'nosys.specs'` (macOS) | The deprecated ArmMbed tap — switch to `brew install --cask gcc-arm-embedded`. |
| Example builds but the LED does nothing on a Pico W | Built without `-DPICO_BOARD=pico_w`, so the LED is mapped to GPIO 25 instead of the CYW43 pin. |
| `arm-none-eabi-gcc: command not found` | Toolchain not installed or not on `PATH` — redo Step 2. |

When `arm-none-eabi-gcc --version`, `cmake --version`, `echo $PICO_SDK_PATH`,
and a freshly built `picow_blink.uf2` all work, your environment is ready for
everything that follows.

# Lesson: Memory-Mapped Registers {#memory-mapped-registers}

Now to the silicon itself. On a desktop OS, hardware hides behind drivers and
system calls. On a microcontroller like the RP2040 (the chip on the Pico) the
hardware *is* memory: every peripheral is controlled by **registers**, and each
register is a 32-bit word at a fixed address. Store to the address and the
peripheral reacts; load from it and you see the peripheral's state.

The addresses come from the datasheet. For example, the RP2040's SIO
("single-cycle I/O") block — the fast path the cores use to drive GPIO pins —
lives at base address `0xd0000000`. The register that sets output pins high,
`GPIO_OUT_SET`, is at offset `0x014`, so its full address is `0xd0000014`.

In C, "store to an address" is a pointer cast and a write:

```c
#include <stdint.h>

#define SIO_BASE      0xd0000000u
#define GPIO_OUT_SET  (SIO_BASE + 0x014u)

*(volatile uint32_t *)GPIO_OUT_SET = 1u << 25;  /* LED pin on the Pico */
```

Two things carry all the weight here:

- **`uintptr_t` / integer-to-pointer casts.** A register address arrives as a
  plain number from the datasheet. Casting it to `volatile uint32_t *` tells
  the compiler "treat this number as the location of a 32-bit word".
- **`volatile`.** It tells the compiler every read and write is *observable
  behavior* that must happen exactly as written. Without it, the optimizer may
  delete "redundant" stores or cache a load in a register — fatal when the
  value at that address is changed by hardware, not by your code.

The grader has no Pico attached, so the tests hand your functions the addresses
of ordinary variables instead of datasheet constants. The pointer mechanics are
*identical* — on real hardware the only difference is where the number comes
from.

## Challenge: Read and Write a Register {#mmio-read-write points=10}

Implement `mmio_write32` and `mmio_read32`: given a register's address as a
plain integer, store or load a 32-bit value through it. This pair is the
foundation every later challenge builds on.

### Starter

```c
#include <stdint.h>

/* Store value into the 32-bit register at addr. */
void mmio_write32(uintptr_t addr, uint32_t value) {
	/* TODO: cast addr to a volatile uint32_t pointer and store through it */
	(void)addr;
	(void)value;
}

/* Load and return the 32-bit register at addr. */
uint32_t mmio_read32(uintptr_t addr) {
	/* TODO */
	(void)addr;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

void mmio_write32(uintptr_t addr, uint32_t value);
uint32_t mmio_read32(uintptr_t addr);

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
	/* The tests stand in for the hardware: a plain variable plays the
	   register, and its address plays the datasheet constant. */
	volatile uint32_t reg = 0xdeadbeefu;

	mmio_write32((uintptr_t)&reg, 0x00000001u);
	check(reg == 0x00000001u, "test_write_stores_value");

	reg = 0xcafef00du;
	check(mmio_read32((uintptr_t)&reg) == 0xcafef00du, "test_read_returns_value");

	mmio_write32((uintptr_t)&reg, 0u);
	check(reg == 0u, "test_write_zero");

	return failed;
}
```

# Lesson: Bits, Masks, and Read-Modify-Write {#bits-and-masks}

A register is rarely one value — it is 32 independent switches packed into a
word. GPIO_OUT bit 25 is the Pico's LED; bit 16 might be your I2C bus. Driving
hardware means changing *your* bits without disturbing anyone else's, which is
done with masks:

```c
reg |=  mask;   /* set   every bit that is 1 in mask */
reg &= ~mask;   /* clear every bit that is 1 in mask */
reg ^=  mask;   /* flip  every bit that is 1 in mask */
```

A mask for a single pin is built by shifting: `1u << pin`. Note the `u` —
shifting a plain (signed) `int` left into bit 31 is undefined behavior. The
RP2040's GPIO pins only go up to 29, so `GPIO_OUT` never actually needs bit 31,
but plenty of other 32-bit registers use every bit: the SIO has 32 hardware
spinlocks, and `SIO_SPINLOCK_ST` reports all of them as a bitmap, one bit per
spinlock, straight through to bit 31. Use `1u`, not `1`, and which register
you're shifting into stops mattering.

Each of those compound assignments is really three steps: **read** the
register, **modify** the copy, **write** it back. Keep that shape in mind — it
works, but the next lesson shows why on the RP2040 you often want the hardware
to do the modify step for you.

## Challenge: Set, Clear, Toggle {#reg-bit-ops points=10}

Implement the three classic read-modify-write helpers. Each takes a pointer to
a register and a mask, and must leave every bit outside the mask exactly as it
found it.

### Starter

```c
#include <stdint.h>

/* Set every bit of *reg that is 1 in mask. */
void reg_set_bits(volatile uint32_t *reg, uint32_t mask) {
	/* TODO */
	(void)reg;
	(void)mask;
}

/* Clear every bit of *reg that is 1 in mask. */
void reg_clear_bits(volatile uint32_t *reg, uint32_t mask) {
	/* TODO */
	(void)reg;
	(void)mask;
}

/* Flip every bit of *reg that is 1 in mask. */
void reg_toggle_bits(volatile uint32_t *reg, uint32_t mask) {
	/* TODO */
	(void)reg;
	(void)mask;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

void reg_set_bits(volatile uint32_t *reg, uint32_t mask);
void reg_clear_bits(volatile uint32_t *reg, uint32_t mask);
void reg_toggle_bits(volatile uint32_t *reg, uint32_t mask);

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
	volatile uint32_t reg;

	reg = 0x0000ff00u;
	reg_set_bits(&reg, 1u << 0);
	check(reg == 0x0000ff01u, "test_set_preserves_other_bits");

	reg = 0xffffffffu;
	reg_clear_bits(&reg, 1u << 31);
	check(reg == 0x7fffffffu, "test_clear_high_pin_no_ub");

	reg = 0x00000005u;
	reg_toggle_bits(&reg, 0x0000000fu);
	check(reg == 0x0000000au, "test_toggle_flips_only_mask");

	reg = 0x12345678u;
	reg_set_bits(&reg, 0u);
	reg_clear_bits(&reg, 0u);
	reg_toggle_bits(&reg, 0u);
	check(reg == 0x12345678u, "test_empty_mask_is_noop");

	return failed;
}
```

# Lesson: GPIO Through the SIO {#sio-gpio}

The SIO block at `0xd0000000` is how RP2040 code drives pins. Its GPIO
registers sit at these offsets (datasheet §2.3.1.7):

| offset  | register       | what it does                      |
|---------|----------------|-----------------------------------|
| `0x010` | `GPIO_OUT`     | output level of each pin          |
| `0x014` | `GPIO_OUT_SET` | write mask: set those OUT bits    |
| `0x018` | `GPIO_OUT_CLR` | write mask: clear those OUT bits  |
| `0x01c` | `GPIO_OUT_XOR` | write mask: flip those OUT bits   |
| `0x020` | `GPIO_OE`      | output *enable* for each pin      |
| `0x024` | `GPIO_OE_SET`  | write mask: set those OE bits     |
| `0x028` | `GPIO_OE_CLR`  | write mask: clear those OE bits   |
| `0x02c` | `GPIO_OE_XOR`  | write mask: flip those OE bits    |

Why the SET/CLR/XOR aliases when last lesson's read-modify-write already works?
Because RMW is **three** operations, and between your read and your write an
interrupt handler — or the RP2040's *second* core; it's a dual-core chip, and
both cores can drive GPIO through the same SIO block — may also touch
GPIO_OUT. Do a plain RMW and that other write can land in the gap between your
read and your write, and gets silently overwritten. Writing a mask to
`GPIO_OUT_SET` is **one** store; the hardware does the modify atomically. This
pattern is all over the RP2040, so drivers barely ever RMW the SIO.

Instead of casting raw offsets one at a time, real drivers describe a whole
register block as a struct whose field layout mirrors the datasheet, then point
it at the base address:

```c
struct pico_sio {
	volatile uint32_t cpuid;        /* 0x000 */
	volatile uint32_t gpio_in;      /* 0x004 */
	volatile uint32_t gpio_hi_in;   /* 0x008 */
	volatile uint32_t _pad;         /* 0x00c (reserved) */
	volatile uint32_t gpio_out;     /* 0x010 */
	volatile uint32_t gpio_out_set; /* 0x014 */
	volatile uint32_t gpio_out_clr; /* 0x018 */
	volatile uint32_t gpio_out_xor; /* 0x01c */
	volatile uint32_t gpio_oe;      /* 0x020 */
	volatile uint32_t gpio_oe_set;  /* 0x024 */
	volatile uint32_t gpio_oe_clr;  /* 0x028 */
	volatile uint32_t gpio_oe_xor;  /* 0x02c */
};

#define SIO ((volatile struct pico_sio *)0xd0000000u)
```

The padding field is load-bearing: every field must land on its datasheet
offset, so reserved holes get explicit placeholders. (In a real project the
struct lives in a header; here each file carries its own copy because challenge
files are self-contained.)

The tests play the part of the hardware: they hand you a `struct pico_sio` in
ordinary memory and then inspect which alias register your code wrote to, and
with what mask.

## Challenge: Drive a Pin Atomically {#sio-pin-ops points=15}

Implement four pin helpers using **only** the SET/CLR/XOR alias registers — the
tests verify that `gpio_out` and `gpio_oe` themselves are never written
directly.

### Starter

```c
#include <stdint.h>

struct pico_sio {
	volatile uint32_t cpuid;        /* 0x000 */
	volatile uint32_t gpio_in;      /* 0x004 */
	volatile uint32_t gpio_hi_in;   /* 0x008 */
	volatile uint32_t _pad;         /* 0x00c (reserved) */
	volatile uint32_t gpio_out;     /* 0x010 */
	volatile uint32_t gpio_out_set; /* 0x014 */
	volatile uint32_t gpio_out_clr; /* 0x018 */
	volatile uint32_t gpio_out_xor; /* 0x01c */
	volatile uint32_t gpio_oe;      /* 0x020 */
	volatile uint32_t gpio_oe_set;  /* 0x024 */
	volatile uint32_t gpio_oe_clr;  /* 0x028 */
	volatile uint32_t gpio_oe_xor;  /* 0x02c */
};

/* Make pin an output (set its GPIO_OE bit atomically). */
void gpio_enable_output(volatile struct pico_sio *sio, unsigned pin) {
	/* TODO */
	(void)sio;
	(void)pin;
}

/* Drive pin high. */
void gpio_set_high(volatile struct pico_sio *sio, unsigned pin) {
	/* TODO */
	(void)sio;
	(void)pin;
}

/* Drive pin low. */
void gpio_set_low(volatile struct pico_sio *sio, unsigned pin) {
	/* TODO */
	(void)sio;
	(void)pin;
}

/* Invert pin. */
void gpio_toggle(volatile struct pico_sio *sio, unsigned pin) {
	/* TODO */
	(void)sio;
	(void)pin;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

struct pico_sio {
	volatile uint32_t cpuid;
	volatile uint32_t gpio_in;
	volatile uint32_t gpio_hi_in;
	volatile uint32_t _pad;
	volatile uint32_t gpio_out;
	volatile uint32_t gpio_out_set;
	volatile uint32_t gpio_out_clr;
	volatile uint32_t gpio_out_xor;
	volatile uint32_t gpio_oe;
	volatile uint32_t gpio_oe_set;
	volatile uint32_t gpio_oe_clr;
	volatile uint32_t gpio_oe_xor;
};

void gpio_enable_output(volatile struct pico_sio *sio, unsigned pin);
void gpio_set_high(volatile struct pico_sio *sio, unsigned pin);
void gpio_set_low(volatile struct pico_sio *sio, unsigned pin);
void gpio_toggle(volatile struct pico_sio *sio, unsigned pin);

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
	struct pico_sio sio;

	/* On real silicon writes to the aliases are decoded by the SIO; here
	   they just land in the struct, so we can see exactly what you wrote. */
	memset((void *)&sio, 0, sizeof sio);
	gpio_enable_output(&sio, 25);
	check(sio.gpio_oe_set == (1u << 25), "test_enable_output_uses_oe_set");
	check(sio.gpio_oe == 0, "test_enable_output_never_rmw");

	memset((void *)&sio, 0, sizeof sio);
	gpio_set_high(&sio, 25);
	check(sio.gpio_out_set == (1u << 25), "test_set_high_uses_out_set");
	check(sio.gpio_out == 0 && sio.gpio_out_clr == 0 && sio.gpio_out_xor == 0,
	      "test_set_high_touches_only_out_set");

	memset((void *)&sio, 0, sizeof sio);
	gpio_set_low(&sio, 7);
	check(sio.gpio_out_clr == (1u << 7), "test_set_low_uses_out_clr");

	/* Pin 31 isn't a real RP2040 GPIO (the user bank stops at 29) — it's used
	   here only to prove your shift math reaches the top bit cleanly, same
	   reason the masks lesson picked SIO_SPINLOCK_ST over GPIO_OUT. */
	memset((void *)&sio, 0, sizeof sio);
	gpio_toggle(&sio, 31);
	check(sio.gpio_out_xor == (1u << 31), "test_toggle_pin31_uses_out_xor");

	return failed;
}
```

# Lesson: Routing Pins with FUNCSEL {#funcsel}

The SIO can only drive a pin the pin mux actually routes to it. Each of the
RP2040's 30 user GPIOs has a control register in the IO_BANK0 block (base
`0x40014000`): pin *N*'s `GPIO_CTRL` register sits at offset `N*8 + 4`. Its low
five bits — the **FUNCSEL** field, bits 4:0 — pick which peripheral owns the
pin: UART, SPI, PWM… Function **5** is the SIO, i.e. "software controls this
pin". Until you select it, all the GPIO_OUT writing in the world does nothing
visible.

FUNCSEL is where last lesson's warning bites: `GPIO_CTRL` packs *other* fields
into the same word (output overrides, interrupt config). Most RP2040
peripherals — IO_BANK0 included — actually do get atomic bit manipulation for
free: every register is given a 4KB address slot, and writing to its address
plus `0x1000`/`0x2000`/`0x3000` atomically XORs/sets/clears bits with no
read-modify-write at all (datasheet §2.1.2). The SIO you just used is the
*exception* — it's wired straight to the cores off the normal bus, so it can't
do that trick, which is exactly why it needed its own hand-built SET/CLR/XOR
registers. That alias mechanism is one more address computation on top of what
this lesson is really after, though: the field-update shape below — mask, or,
store — which the tests exercise directly on a plain register variable, and
which you need to understand regardless of which write mechanism eventually
lands the bits. So here you'll do it by hand:

```c
uint32_t v = *ctrl;
v &= ~0x1fu;        /* clear the FUNCSEL field   */
v |= funcsel;       /* install the new function  */
*ctrl = v;
```

Mask first, then or — the classic *field update*. Getting this wrong by writing
the whole word would silently zero every other field in the register.

## Challenge: Select a Pin Function {#set-funcsel points=10}

Implement `gpio_set_funcsel`: update only bits 4:0 of the control register,
preserving all 27 other bits.

### Starter

```c
#include <stdint.h>

#define GPIO_CTRL_FUNCSEL_MASK 0x1fu
#define GPIO_FUNC_SIO 5u

/* Set the FUNCSEL field (bits 4:0) of *ctrl to funcsel, leaving every
   other bit untouched. */
void gpio_set_funcsel(volatile uint32_t *ctrl, unsigned funcsel) {
	/* TODO: read, clear the field, or in the new value, write back */
	(void)ctrl;
	(void)funcsel;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

void gpio_set_funcsel(volatile uint32_t *ctrl, unsigned funcsel);

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
	volatile uint32_t ctrl;

	ctrl = 0;
	gpio_set_funcsel(&ctrl, 5);
	check(ctrl == 5, "test_funcsel_written");

	ctrl = 0xffffffe0u;  /* every non-FUNCSEL bit lit */
	gpio_set_funcsel(&ctrl, 5);
	check(ctrl == (0xffffffe0u | 5u), "test_other_fields_preserved");

	ctrl = 0x0000001fu;  /* old function: all field bits set */
	gpio_set_funcsel(&ctrl, 2);
	check(ctrl == 2, "test_old_function_cleared_first");

	return failed;
}
```

# Lesson: Your First Firmware — the LED Driver {#led-driver}

Put all four lessons together into the driver every embedded project starts
with: the blinking LED. On a bare Raspberry Pi Pico this exact code — pointed at
`0xd0000000` and `0x40014000` instead of test memory — drives the onboard LED
on GPIO 25. It is your "hello world" for firmware, and later it becomes the
status light for your scanner: solid means streaming, a fast blink means the
WiFi dropped and you're reconnecting.

One thing to flag now, because it bites everyone: **on the Pico W the onboard
LED is not on GPIO 25.** The wireless model moved the LED onto a pin of the
CYW43 WiFi chip, so you drive it with `cyw43_arch_gpio_put(...)` instead of the
SIO. The register driver you're about to write still runs perfectly on a Pico
W — it just controls a header pin, which is why the next lessons wire a real
LED onto a breadboard so this knowledge transfers straight to the hardware you
scan with.

Implement three functions:

- `led_init(sio, ctrl, pin)` — route the pin to the SIO (FUNCSEL 5, preserving
  the rest of the CTRL register), then enable output drive through the atomic
  `GPIO_OE_SET` alias.
- `led_set(sio, pin, on)` — drive the pin high when `on` is nonzero, low
  otherwise, using only the `GPIO_OUT_SET` / `GPIO_OUT_CLR` aliases.
- `led_get(sio, pin)` — return the pin's current output level (0 or 1) read
  from `GPIO_OUT`.

The tests emulate the SIO: after each of your calls they apply whatever you
wrote to the SET/CLR aliases onto `gpio_out`, exactly as the silicon would,
then clear the alias. If you write `gpio_out` directly, the emulation will
catch it.

## Challenge: An LED Driver {#led-driver-build points=50}

### Starter

```c
#include <stdint.h>

#define GPIO_CTRL_FUNCSEL_MASK 0x1fu
#define GPIO_FUNC_SIO 5u

struct pico_sio {
	volatile uint32_t cpuid;        /* 0x000 */
	volatile uint32_t gpio_in;      /* 0x004 */
	volatile uint32_t gpio_hi_in;   /* 0x008 */
	volatile uint32_t _pad;         /* 0x00c (reserved) */
	volatile uint32_t gpio_out;     /* 0x010 */
	volatile uint32_t gpio_out_set; /* 0x014 */
	volatile uint32_t gpio_out_clr; /* 0x018 */
	volatile uint32_t gpio_out_xor; /* 0x01c */
	volatile uint32_t gpio_oe;      /* 0x020 */
	volatile uint32_t gpio_oe_set;  /* 0x024 */
	volatile uint32_t gpio_oe_clr;  /* 0x028 */
	volatile uint32_t gpio_oe_xor;  /* 0x02c */
};

/* Route pin to the SIO and enable output drive. */
void led_init(volatile struct pico_sio *sio, volatile uint32_t *ctrl,
              unsigned pin) {
	/* TODO */
	(void)sio;
	(void)ctrl;
	(void)pin;
}

/* Drive pin high (on != 0) or low (on == 0) via the atomic aliases. */
void led_set(volatile struct pico_sio *sio, unsigned pin, int on) {
	/* TODO */
	(void)sio;
	(void)pin;
	(void)on;
}

/* Current output level of pin, from GPIO_OUT: 0 or 1. */
int led_get(const volatile struct pico_sio *sio, unsigned pin) {
	/* TODO */
	(void)sio;
	(void)pin;
	return -1;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define GPIO_FUNC_SIO 5u

struct pico_sio {
	volatile uint32_t cpuid;
	volatile uint32_t gpio_in;
	volatile uint32_t gpio_hi_in;
	volatile uint32_t _pad;
	volatile uint32_t gpio_out;
	volatile uint32_t gpio_out_set;
	volatile uint32_t gpio_out_clr;
	volatile uint32_t gpio_out_xor;
	volatile uint32_t gpio_oe;
	volatile uint32_t gpio_oe_set;
	volatile uint32_t gpio_oe_clr;
	volatile uint32_t gpio_oe_xor;
};

void led_init(volatile struct pico_sio *sio, volatile uint32_t *ctrl,
              unsigned pin);
void led_set(volatile struct pico_sio *sio, unsigned pin, int on);
int led_get(const volatile struct pico_sio *sio, unsigned pin);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

/* Emulate the SIO's decode of the atomic aliases: fold SET/CLR/XOR writes
   into gpio_out/gpio_oe and reset the aliases, like the silicon would. */
static void sio_step(struct pico_sio *sio) {
	sio->gpio_out |= sio->gpio_out_set;
	sio->gpio_out &= ~sio->gpio_out_clr;
	sio->gpio_out ^= sio->gpio_out_xor;
	sio->gpio_out_set = sio->gpio_out_clr = sio->gpio_out_xor = 0;
	sio->gpio_oe |= sio->gpio_oe_set;
	sio->gpio_oe &= ~sio->gpio_oe_clr;
	sio->gpio_oe ^= sio->gpio_oe_xor;
	sio->gpio_oe_set = sio->gpio_oe_clr = sio->gpio_oe_xor = 0;
}

int main(void) {
	struct pico_sio sio;
	volatile uint32_t ctrl;
	unsigned pin = 25;  /* the Pico's onboard LED */

	memset((void *)&sio, 0, sizeof sio);
	ctrl = 0xaaaaaa00u;  /* pre-existing non-FUNCSEL config to preserve */

	led_init(&sio, &ctrl, pin);
	sio_step(&sio);
	check((ctrl & 0x1fu) == GPIO_FUNC_SIO, "test_init_selects_sio_function");
	check((ctrl & ~0x1fu) == 0xaaaaaa00u, "test_init_preserves_ctrl_fields");
	check(sio.gpio_oe == (1u << pin), "test_init_enables_output");

	led_set(&sio, pin, 1);
	check(sio.gpio_out == 0, "test_set_does_not_write_out_directly");
	sio_step(&sio);
	check(sio.gpio_out == (1u << pin), "test_led_on");
	check(led_get(&sio, pin) == 1, "test_get_reads_high");

	led_set(&sio, pin, 0);
	sio_step(&sio);
	check(sio.gpio_out == 0, "test_led_off");
	check(led_get(&sio, pin) == 0, "test_get_reads_low");

	/* A second pin must not disturb the first. */
	led_set(&sio, pin, 1);
	sio_step(&sio);
	led_set(&sio, 7, 1);
	sio_step(&sio);
	check(sio.gpio_out == ((1u << pin) | (1u << 7)),
	      "test_pins_are_independent");

	return failed;
}
```

# Lesson: What You Need — the Hardware {#hardware-and-toolchain}

The register challenges ran in the grader's memory, and your toolchain is set up
from the first lesson. From here on there is a real board on your desk. This
lesson is the shopping list; the next one flashes your first real firmware.

## The hardware

You need surprisingly little. The one non-negotiable is a Pico **W** (or Pico
2 W) — the plain Pico has no radio and cannot do the second half of this
course.

| Item | Notes |
|------|-------|
| **Raspberry Pi Pico W** (or Pico 2 W) | The WiFi model. ~$6–7. Pre-soldered headers save you a step. |
| **USB cable** | Micro-USB (Pico W) or USB-C (Pico 2 W). Must be a **data** cable, not charge-only — a charge-only cable is the #1 "my board is dead" cause. |
| **A computer** | Linux, macOS, or Windows. This runs the build toolchain and the listener. |
| **A 2.4 GHz WiFi network** | The CYW43 radio is **2.4 GHz only**. It cannot see or join a 5 GHz-only SSID — a very common first-day trap. |
| Breadboard + 1 LED + 330 Ω resistor + 2 jumper wires | *Optional but recommended.* An external status LED you can actually watch while the scan runs, wired with the driver you just built. |

That is the whole bill of materials. No debugger probe, no soldering iron
(if your headers are pre-attached), no shield.

## The board, oriented

You will refer to physical pins constantly, so anchor yourself now. The Pico W
has 40 pins, 20 down each long edge, numbered 1–40 counterclockwise starting
from the top-left when the **USB connector faces up**. A few you will use in
this course:

```text
        USB (top)
   ┌───────▄▄▄───────┐
 1 │ GP0          VBUS│ 40   ← 5 V from USB
 2 │ GP1          VSYS│ 39
 3 │ GND          GND │ 38
 …  │ …             …  │
20 │ GP15         GND │ 23   ← our LED signal (pin 20) + a ground (pin 23)
   │                  │
   │   [ RP2040 ]     │
   │   [ CYW43  ]     │      ← radio + onboard LED live here
   └──────────────────┘
```

Two rules that save hours:

- **3.3 V logic.** Every GPIO is 3.3 V. Do **not** feed 5 V into a GP pin. Use
  `VBUS`/`VSYS` for 5 V only if you know what you're doing; for our LED we drive
  it from a 3.3 V GP pin, which is exactly right.
- **Pin number ≠ GP number.** "GP15" is the *logical* GPIO the code names;
  "pin 20" is the *physical* header position you plug a wire into. The printed
  pinout card (or the datasheet's pinout page) maps between them — keep it next
  to you.

## The toolchain (already done)

You installed the cross-compiler, CMake, and the SDK in the first lesson. Quick
confirmation that you're ready: `arm-none-eabi-gcc --version` prints a version,
`echo $PICO_SDK_PATH` prints your SDK path, and you built `picow_blink.uf2`
there. If any of those isn't true, go back and finish that setup before
continuing — the rest of this half depends on it.

One optional extra worth grabbing now: **picotool**, which flashes and inspects
boards over USB without the drag-and-drop dance (`brew install picotool` on
macOS, `sudo apt install picotool` on recent Ubuntu, or build it from source).
The next lesson turns all of this into a running LED.

# Lesson: Build, Flash, Blink — Running Real Firmware {#build-flash-blink}

Time to make hardware move. You'll build a firmware image, flash it, and watch
an LED blink — first the onboard one, then one you wire yourself. This is the
loop you'll repeat for the rest of the course: **edit → `cmake --build` → drag
the `.uf2` → observe**.

## A minimal blink

A pico-sdk program is ordinary C with `main()`; the SDK provides the drivers.
Here is the whole thing for the **Pico W**, using the CYW43 LED helper (not the
SIO — remember the last lesson's warning):

```c
/* blink.c */
#include "pico/stdlib.h"
#include "pico/cyw43_arch.h"

int main(void) {
	stdio_init_all();
	if (cyw43_arch_init()) {          /* powers up the WiFi chip; the LED
	                                     hangs off it, so we need this even
	                                     just to blink */
		return 1;
	}
	while (true) {
		cyw43_arch_gpio_put(CYW43_WL_GPIO_LED_PIN, 1);
		sleep_ms(250);
		cyw43_arch_gpio_put(CYW43_WL_GPIO_LED_PIN, 0);
		sleep_ms(250);
	}
}
```

Want to prove your register driver runs on real silicon instead? Wire an
external LED (next section) and drive **its** GP pin with `sio_hw` — the
pico-sdk exposes the very SIO struct you built, so `sio_hw->gpio_oe_set = 1u <<
15;` is your `gpio_enable_output` verbatim. The bare-metal half of this course
was not a simulation; it was this code.

## Telling CMake about it

The SDK is CMake-based. A `CMakeLists.txt` names your program, its sources, and
which SDK libraries to link:

```cmake
cmake_minimum_required(VERSION 3.13)
include(pico_sdk_import.cmake)      # a copy of this file ships in the SDK
project(blink C CXX ASM)
pico_sdk_init()

add_executable(blink blink.c)
target_link_libraries(blink pico_stdlib pico_cyw43_arch_none)

pico_enable_stdio_usb(blink 1)      # printf() over the USB serial port
pico_add_extra_outputs(blink)       # also emit blink.uf2 (what we flash)
```

Copy `pico_sdk_import.cmake` from `$PICO_SDK_PATH/external/` next to your
`CMakeLists.txt`. Then configure and build — note `-DPICO_BOARD=pico_w`, which
selects the right pins and pulls in the radio:

```bash
export PICO_SDK_PATH=~/pico-sdk
cmake -B build -DPICO_BOARD=pico_w .
cmake --build build
# → build/blink.uf2
```

## Flashing: BOOTSEL and UF2

The Pico exposes a bootloader as a USB mass-storage disk. To flash:

1. **Unplug** the Pico.
2. **Hold the BOOTSEL button** (the only button on the board) and plug the USB
   back in. Keep holding until it enumerates.
3. A drive named **`RPI-RP2`** appears. Release the button.
4. **Copy `build/blink.uf2` onto that drive.** The board reboots itself the
   instant the copy finishes and starts running your code.

`.uf2` ("USB Flashing Format") is a self-describing image the bootloader knows
how to unpack to flash — that's why a plain file copy works. Prefer the command
line? `picotool load -f build/blink.uf2` does the same and even forces BOOTSEL
for you if the board is running.

The onboard LED should now blink twice a second. If it doesn't: re-check you
used a **data** USB cable, that the drive really was `RPI-RP2`, and that the
build printed no errors.

## Wiring an external status LED

The onboard LED is fine, but part of the point of a microcontroller is driving
*your own* circuit — and an external LED exercises the GPIO driver you wrote.
Here is the circuit: a GP pin pushes current through a **current-limiting
resistor** into the LED's long leg (anode, `+`), and out the short leg (cathode,
`−`) back to ground.

```d2
direction: right
pico: "Raspberry Pi Pico W" {
  shape: sql_table
  gp15: "GP15   (pin 20)"
  gnd: "GND    (pin 23)"
  vbus: "VBUS   (pin 40)"
}
r: "resistor\n330 ohm" { style.stroke: "#fbbf24" }
led: "LED\nlong leg = +" { style.stroke: "#34d399" }
pico.gp15 -> r: "signal"
r -> led: "anode (+)"
led -> pico.gnd: "cathode (-)"
```

On the breadboard, physically:

```text
 Pico GP15 (pin 20) ──▶ [ 330Ω ] ──▶ ▶│ LED ──▶ Pico GND (pin 23)
                        resistor      long leg    (any GND pin works)
```

- The **resistor is not optional.** An LED is a diode; without a series
  resistor it pulls as much current as the pin can source and burns out the LED
  (and stresses the pin). 330 Ω on 3.3 V gives a safe ~5 mA.
- **LEDs are polarized.** The long leg is `+`, and the flat notch on the rim
  marks the `−` (cathode) side. Backwards, it simply won't light — no damage,
  just confusing. If it stays dark, flip it.
- Resistor orientation doesn't matter (resistors aren't polarized); LED
  orientation does.

To drive **GP15** instead of the onboard LED, initialize it with the SDK
(`gpio_init(15); gpio_set_dir(15, GPIO_OUT); gpio_put(15, on);`) — or, to run
your own driver, `sio_hw->gpio_oe_set = 1u << 15;` then
`sio_hw->gpio_out_set = 1u << 15;`. Same registers, real pin, real light.

You now have a board that runs your code and a light you can see. Next: get it
on the network.

# Lesson: Getting on WiFi {#wifi-join}

The whole point of the Pico *W* is the radio. Joining a network with the SDK is
a few calls, but each hides a real concept — station mode, association, DHCP —
worth understanding, because when scanning fails later it's usually one of
these.

Here is the shape of the journey your data takes: the Pico joins an access
point, the access point's router hands it an IP by DHCP, and from then on the
Pico can open a TCP socket to your laptop sitting on the same network.

```d2
direction: right
pico: "Pico W\nTCP client" { style.stroke: "#22d3ee"; style.stroke-width: 2 }
ap: "WiFi AP\n2.4 GHz"
laptop: "your laptop\nlisten.py  :9000"
pico -> ap: "join + scan"
ap -> laptop: "framed records\n[len][payload][crc]"
```

## Joining, in code

Bring up the driver in **station mode** (the Pico is a client of an AP, not an
AP itself), then connect with a timeout so a wrong password fails loudly
instead of hanging:

```c
#include "pico/stdlib.h"
#include "pico/cyw43_arch.h"
#include "lwip/netif.h"

int main(void) {
	stdio_init_all();
	if (cyw43_arch_init()) { printf("wifi init failed\n"); return 1; }
	cyw43_arch_enable_sta_mode();

	printf("joining %s …\n", WIFI_SSID);
	int rc = cyw43_arch_wifi_connect_timeout_ms(
	    WIFI_SSID, WIFI_PASS, CYW43_AUTH_WPA2_AES_PSK, 30000);
	if (rc) { printf("join failed: %d\n", rc); return 1; }

	/* DHCP has run by the time connect returns success. Read our config
	   off the default netif. */
	struct netif *nif = netif_default;
	char ip[16], mask[16], gw[16];
	ip4addr_ntoa_r(netif_ip4_addr(nif),    ip,   sizeof ip);
	ip4addr_ntoa_r(netif_ip4_netmask(nif), mask, sizeof mask);
	ip4addr_ntoa_r(netif_ip4_gw(nif),      gw,   sizeof gw);
	printf("joined: ip=%s mask=%s gw=%s\n", ip, mask, gw);

	/* We linked pico_cyw43_arch_lwip_threadsafe_background, so the SDK
	   services WiFi and lwIP on an interrupt — main just idles. */
	while (true) sleep_ms(1000);
}
```

Notice `ip4addr_ntoa_r`, the **re-entrant** formatter, with a caller buffer.
Its convenient cousin `ip4addr_ntoa(addr)` returns a pointer into a single
shared static buffer — so `printf("%s %s", ip4addr_ntoa(a), ip4addr_ntoa(b))`
prints the *same* address twice, because both pointers alias that one buffer and
the second call overwrites the first before `printf` runs. That is a genuinely
common bug; the `_r` variant with your own buffers sidesteps it.

`WIFI_SSID` and `WIFI_PASS` are compiled in as `-D` defines (never hard-code
secrets into source you might commit). The scanner's `CMakeLists.txt` in a
later lesson passes them at configure time.

## The netmask is not decoration

That `mask` you just read — `255.255.255.0` for a typical home network — is what
tells you *which addresses to scan* when you sweep the LAN. Combined with your
own IP it defines the range of hosts on your subnet: everything from the network
address up to the broadcast address. Computing that range is the first piece of
real scanner logic, and it's pure arithmetic you can test on your workstation.

An IPv4 address is 32 bits. `192.168.1.10` packs to `0xC0A8010A`. The netmask's
1-bits mark the **network** part, its 0-bits the **host** part:

```text
 ip        192.168.1.10   1100 0000 1010 1000 0000 0001 0000 1010
 mask /24  255.255.255.0  1111 1111 1111 1111 1111 1111 0000 0000
 ─────────────────────────────────────────────────────────────────
 network   ip &  mask  →  192.168.1.0    (host bits forced to 0)
 broadcast ip | ~mask  →  192.168.1.255  (host bits forced to 1)
 hosts to scan: 192.168.1.1 … 192.168.1.254   (254 usable)
```

## Challenge: Subnet Math {#subnet-math points=15}

Implement the address arithmetic a LAN sweep is built on. All addresses are
host-order `uint32_t` (so `192.168.1.10` is `0xC0A8010A`).

### Starter

```c
#include <stdint.h>

/* The network (base) address: the part the mask keeps. */
uint32_t net_network(uint32_t ip, uint32_t mask) {
	/* TODO */
	(void)ip;
	(void)mask;
	return 0;
}

/* The broadcast address: the network with every host bit set to 1. */
uint32_t net_broadcast(uint32_t ip, uint32_t mask) {
	/* TODO */
	(void)ip;
	(void)mask;
	return 0;
}

/* Count of usable host addresses (excludes network + broadcast). A /31 or /32
   has none. Example: a /24 has 254. */
uint32_t net_usable_hosts(uint32_t mask) {
	/* TODO */
	(void)mask;
	return 0;
}

/* 1 if ip belongs to the network defined by (network, mask), else 0. Here
   network is assumed already masked (its host bits are 0). */
int net_contains(uint32_t network, uint32_t mask, uint32_t ip) {
	/* TODO */
	(void)network;
	(void)mask;
	(void)ip;
	return 0;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

uint32_t net_network(uint32_t ip, uint32_t mask);
uint32_t net_broadcast(uint32_t ip, uint32_t mask);
uint32_t net_usable_hosts(uint32_t mask);
int net_contains(uint32_t network, uint32_t mask, uint32_t ip);

static int failed;

static void check(int ok, const char *name) {
	if (ok) {
		printf("--- PASS: %s\n", name);
	} else {
		printf("--- FAIL: %s\n", name);
		failed++;
	}
}

static uint32_t ipv4(uint8_t a, uint8_t b, uint8_t c, uint8_t d) {
	return ((uint32_t)a << 24) | ((uint32_t)b << 16) | ((uint32_t)c << 8) | d;
}

int main(void) {
	uint32_t ip = ipv4(192, 168, 1, 10);
	uint32_t m24 = 0xffffff00u;

	check(net_network(ip, m24) == ipv4(192, 168, 1, 0), "test_network_24");
	check(net_broadcast(ip, m24) == ipv4(192, 168, 1, 255), "test_broadcast_24");
	check(net_usable_hosts(m24) == 254u, "test_usable_24");
	check(net_usable_hosts(0xfffffffcu) == 2u, "test_usable_30");
	check(net_usable_hosts(0xfffffffeu) == 0u, "test_usable_31");
	check(net_usable_hosts(0xffffffffu) == 0u, "test_usable_32");

	uint32_t net = ipv4(192, 168, 1, 0);
	check(net_contains(net, m24, ipv4(192, 168, 1, 200)) == 1, "test_contains_yes");
	check(net_contains(net, m24, ipv4(192, 168, 2, 5)) == 0, "test_contains_no");

	return failed;
}
```

# Lesson: Scanning — the WiFi Survey and the LAN Sweep {#scanning}

Now the payload. Your scanner produces two kinds of "network information", and
this lesson builds both on-device. First the easy, always-works one — a survey
of the WiFi around you — then the literal IP scan: sweeping your subnet for live
hosts.

## Survey: what access points are in range

The CYW43 can do a passive/active scan and report every access point it hears.
You hand `cyw43_wifi_scan` a callback; the driver invokes it once per AP found:

```c
#include "pico/cyw43_arch.h"

static int on_scan_result(void *env, const cyw43_ev_scan_result_t *r) {
	char ssid[33];
	ssid_to_str(r->ssid, r->ssid_len, ssid, sizeof ssid);   /* from the challenge */
	printf("AP %-20s ch=%2d rssi=%d auth=%u %02x:%02x:%02x:%02x:%02x:%02x\n",
	       ssid, r->channel, r->rssi, r->auth_mode,
	       r->bssid[0], r->bssid[1], r->bssid[2],
	       r->bssid[3], r->bssid[4], r->bssid[5]);
	return 0;   /* nonzero would abort the scan */
}

void survey(void) {
	cyw43_wifi_scan_options_t opts = {0};
	if (cyw43_wifi_scan(&cyw43_state, &opts, NULL, on_scan_result) == 0) {
		/* Wait for the scan to finish; in background mode the SDK drives it
		   and fires on_scan_result for us — we just idle until it's done. */
		while (cyw43_wifi_scan_active(&cyw43_state)) {
			sleep_ms(100);
		}
	}
}
```

Each `cyw43_ev_scan_result_t` gives you:

| field       | type          | meaning                                    |
|-------------|---------------|--------------------------------------------|
| `ssid`      | `uint8_t[32]` | network name — **not** null-terminated     |
| `ssid_len`  | `uint8_t`     | how many of those 32 bytes are real        |
| `bssid`     | `uint8_t[6]`  | the AP's MAC address                        |
| `rssi`      | `int16_t`     | signal strength in dBm (closer to 0 = stronger) |
| `channel`   | `uint16_t`    | 2.4 GHz channel, 1–13                       |
| `auth_mode` | `uint8_t`     | security: open, WPA, WPA2, …                |

Two of these are traps worth handling in code, which is exactly the next
challenge:

- **`ssid` is not a C string.** It's raw bytes with a separate length. Passing
  it to `printf("%s", …)` reads past the end until it hits a random zero — a
  classic embedded overrun. You must copy exactly `ssid_len` bytes and add your
  own terminator. A length of 0 means a **hidden** network.
- **`rssi` is a signed dBm number**, typically −30 (right next to the AP) to
  −90 (barely there). To show it as signal bars you bucket it into a small
  range.

## Challenge: Format a Scan Result {#format-ap points=10}

Turn raw radio fields into safe, printable output.

### Starter

```c
#include <stddef.h>
#include <stdint.h>

/* A Pico W scan hands you an SSID as up to 32 raw bytes that are NOT
   null-terminated, plus a length. Produce a safe, null-terminated C string in
   out (capacity out_sz). A zero-length SSID is a hidden network: write
   "<hidden>". Never write past out_sz; always null-terminate when out_sz > 0. */
void ssid_to_str(const uint8_t *ssid, size_t len, char *out, size_t out_sz) {
	/* TODO */
	(void)ssid;
	(void)len;
	(void)out;
	(void)out_sz;
}

/* Map an RSSI in dBm to a 0..4 signal-bar count:
     >= -50 : 4
   -60..-51 : 3
   -70..-61 : 2
   -80..-71 : 1
     < -80  : 0                                                            */
int rssi_to_bars(int rssi) {
	/* TODO */
	(void)rssi;
	return 0;
}
```

### Tests

```c
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

void ssid_to_str(const uint8_t *ssid, size_t len, char *out, size_t out_sz);
int rssi_to_bars(int rssi);

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
	char b[16];

	ssid_to_str((const uint8_t *)"HomeNet", 7, b, sizeof b);
	check(strcmp(b, "HomeNet") == 0, "test_ssid_plain");

	ssid_to_str((const uint8_t *)"", 0, b, sizeof b);
	check(strcmp(b, "<hidden>") == 0, "test_ssid_hidden");

	char small[4];
	ssid_to_str((const uint8_t *)"LongName", 8, small, sizeof small);
	check(strcmp(small, "Lon") == 0 && small[3] == '\0', "test_ssid_truncates");

	/* A worst case: 32 non-terminated bytes. */
	uint8_t raw[32];
	memset(raw, 'A', sizeof raw);
	char big[40];
	ssid_to_str(raw, sizeof raw, big, sizeof big);
	check(strlen(big) == 32, "test_ssid_full_32_bytes");

	check(rssi_to_bars(-40) == 4 && rssi_to_bars(-50) == 4, "test_bars_4");
	check(rssi_to_bars(-55) == 3, "test_bars_3");
	check(rssi_to_bars(-61) == 2, "test_bars_2");
	check(rssi_to_bars(-80) == 1, "test_bars_1");
	check(rssi_to_bars(-90) == 0, "test_bars_0");

	return failed;
}
```

## Sweep: which hosts on my LAN are alive

The survey looks at the *air*. The other kind of scan looks at the *wire*: given
your subnet (from the subnet-math challenge), try to reach each address and see
who answers. The classic technique is a **TCP connect scan** — attempt a TCP
connection to a common port on each host:

- the connection **completes** → host is up and that port is open;
- the connection is **refused** (an RST comes back) → host is up, port closed;
- **nothing comes back** before a timeout → treat the host as down.

```d2
next: "next host\n192.168.1.N" { shape: oval }
conn: "tcp_connect(host, 80)"
up_open: "UP, port open" { style.stroke: "#34d399" }
up_closed: "UP, port closed" { style.stroke: "#fbbf24" }
down: "no answer -> down" { style.stroke: "#64748b" }
next -> conn
conn -> up_open: "connected_cb"
conn -> up_closed: "err RST"
conn -> down: "poll timeout"
up_open -> next: "emit record"
up_closed -> next: "emit record"
down -> next
```

Doing this well needs the lwIP TCP client from the next lesson, so the full
sweep firmware lives there and in the final project. The point here: the sweep's
*range* comes straight from `net_network`/`net_broadcast`, and its *result* is
just another record to frame and stream — the same pipe the survey uses. Two
scanners, one uplink.

# Lesson: A TCP Client on the Pico {#tcp-client}

To send anything to your laptop you need a TCP client on the Pico. The SDK's
default WiFi mode (`pico_cyw43_arch_lwip_threadsafe_background`) runs the lwIP
stack for you in the background; you drive it with lwIP's **raw callback API**.
There are no blocking `connect()`/`send()` calls like on a desktop — instead you
register callbacks and lwIP calls *you* when something happens.

The client walks a small state machine:

```d2
closed: "CLOSED"
connecting: "CONNECTING"
connected: "CONNECTED" { style.stroke: "#34d399"; style.stroke-width: 2 }
closed -> connecting: "tcp_connect()"
connecting -> connected: "connected_cb  ERR_OK"
connecting -> closed: "err_cb / timeout"
connected -> connected: "tcp_write + tcp_output"
connected -> closed: "tcp_close() / err_cb"
```

Two rules keep raw-API code correct:

- **Guard lwIP calls with `cyw43_arch_lwip_begin()` / `..._end()`** whenever you
  call into lwIP from your `main` loop (not from inside a callback — callbacks
  already run with the lock held). In background mode the stack runs on another
  context; these calls are the lock.
- **`tcp_write` only queues; `tcp_output` flushes.** And pass
  `TCP_WRITE_FLAG_COPY` unless you can guarantee the buffer outlives the send —
  a stack buffer cannot.

Here is a minimal, reusable uplink:

```c
#include "pico/cyw43_arch.h"
#include "lwip/tcp.h"

struct uplink {
	struct tcp_pcb *pcb;
	bool connected;
};

static err_t up_on_connected(void *arg, struct tcp_pcb *pcb, err_t err) {
	struct uplink *u = arg;
	u->connected = (err == ERR_OK);
	return ERR_OK;
}

/* lwIP calls this on a fatal error; the pcb is already freed — do NOT touch it. */
static void up_on_err(void *arg, err_t err) {
	struct uplink *u = arg;
	u->pcb = NULL;
	u->connected = false;
}

bool uplink_open(struct uplink *u, const char *host_ip, u16_t port) {
	ip_addr_t addr;
	if (!ip4addr_aton(host_ip, &addr)) return false;
	cyw43_arch_lwip_begin();
	u->pcb = tcp_new_ip_type(IPADDR_TYPE_V4);
	u->connected = false;
	tcp_arg(u->pcb, u);
	tcp_err(u->pcb, up_on_err);
	err_t e = tcp_connect(u->pcb, &addr, port, up_on_connected);
	cyw43_arch_lwip_end();
	return e == ERR_OK;
}

/* Send one already-framed buffer. Returns false on any error. */
bool uplink_send(struct uplink *u, const uint8_t *buf, size_t n) {
	cyw43_arch_lwip_begin();
	err_t e = ERR_CONN;
	if (u->pcb && u->connected) {
		e = tcp_write(u->pcb, buf, n, TCP_WRITE_FLAG_COPY);
		if (e == ERR_OK) e = tcp_output(u->pcb);
	}
	cyw43_arch_lwip_end();
	return e == ERR_OK;
}
```

`tcp_connect` returns immediately; you know you're really connected only when
`up_on_connected` fires with `ERR_OK`. In `main` you'd spin
`cyw43_arch_poll(); sleep_ms(10);` until `u->connected` (or you give up). That
"try, wait, give up, try again" shape is the retry logic — and it's pure enough
to test on the host.

## Challenge: Retry with Backoff {#retry-backoff points=15}

Networks fail. When a connect times out you retry — but not in a tight loop that
hammers the AP; you **back off**, waiting longer after each failure. And you
track connection state so the reconnect loop knows what to do next.

### Starter

```c
#include <stdint.h>

/* Exponential backoff: base 500 ms, doubling each attempt, capped at 16000 ms.
   attempt 0 -> 500, 1 -> 1000, 2 -> 2000, ... 5 -> 16000, and every later
   attempt stays at 16000. Must not overflow for large attempt numbers. */
uint32_t backoff_delay_ms(unsigned attempt) {
	/* TODO */
	(void)attempt;
	return 0;
}

/* The reconnect loop's state machine. */
enum conn_state { CONN_WIFI_DOWN, CONN_WIFI_UP, CONN_SOCKET_UP };
enum conn_event { EV_WIFI_JOINED, EV_WIFI_LOST, EV_SOCKET_OPEN, EV_SOCKET_ERR };

/* Next state given the current state and an event. An event that does not
   apply to the current state leaves the state unchanged. The transitions:
     WIFI_DOWN + WIFI_JOINED -> WIFI_UP
     WIFI_UP   + SOCKET_OPEN -> SOCKET_UP
     WIFI_UP   + WIFI_LOST   -> WIFI_DOWN
     SOCKET_UP + SOCKET_ERR  -> WIFI_UP     (socket died, WiFi may be fine)
     SOCKET_UP + WIFI_LOST   -> WIFI_DOWN                                   */
enum conn_state conn_next(enum conn_state s, enum conn_event e) {
	/* TODO */
	(void)e;
	return s;
}
```

### Tests

```c
#include <stdint.h>
#include <stdio.h>

uint32_t backoff_delay_ms(unsigned attempt);
enum conn_state { CONN_WIFI_DOWN, CONN_WIFI_UP, CONN_SOCKET_UP };
enum conn_event { EV_WIFI_JOINED, EV_WIFI_LOST, EV_SOCKET_OPEN, EV_SOCKET_ERR };
enum conn_state conn_next(enum conn_state s, enum conn_event e);

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
	check(backoff_delay_ms(0) == 500, "test_backoff_0");
	check(backoff_delay_ms(1) == 1000, "test_backoff_1");
	check(backoff_delay_ms(3) == 4000, "test_backoff_3");
	check(backoff_delay_ms(5) == 16000, "test_backoff_cap_reached");
	check(backoff_delay_ms(6) == 16000, "test_backoff_capped");
	check(backoff_delay_ms(40) == 16000, "test_backoff_no_overflow");

	check(conn_next(CONN_WIFI_DOWN, EV_WIFI_JOINED) == CONN_WIFI_UP, "test_join");
	check(conn_next(CONN_WIFI_UP, EV_SOCKET_OPEN) == CONN_SOCKET_UP, "test_open");
	check(conn_next(CONN_SOCKET_UP, EV_SOCKET_ERR) == CONN_WIFI_UP, "test_sock_err");
	check(conn_next(CONN_SOCKET_UP, EV_WIFI_LOST) == CONN_WIFI_DOWN, "test_wifi_lost");
	check(conn_next(CONN_WIFI_DOWN, EV_SOCKET_OPEN) == CONN_WIFI_DOWN, "test_irrelevant_noop");

	return failed;
}
```

# Lesson: Framing — Where Does One Record End? {#framing}

You can now open a socket and push bytes. But TCP is a **byte stream, not a
message stream**: it guarantees your bytes arrive in order, and nothing else. It
does *not* preserve your `tcp_write` boundaries. Send three scan records and the
listener might `recv()` all three in one chunk, or the first one split down the
middle, or two-and-a-half. This is the single most common mistake in
socket programming: assuming one write equals one read.

So how does the listener know where one scan record ends and the next begins?

The wrong answer is "put a newline between them." An SSID can contain *any*
byte — including a newline or a tab — so a delimiter can appear inside your
data and desynchronize the stream. The robust answer is **length-prefix
framing**: before each record's bytes, send its length. The reader reads the
length, then reads exactly that many bytes. No byte value is special, because
the length says precisely how far to go.

Our frame — used by the firmware, the listener, and the final challenge — is:

```text
 ┌────────────┬───────────────────────┬────────────┐
 │  length    │        payload        │   crc16    │
 │  2 bytes   │      "length" bytes    │  2 bytes   │
 │ big-endian │  the scan record text  │ big-endian │
 └────────────┴───────────────────────┴────────────┘
   e.g. for  "AP\tHomeNet\t-52\t6\twpa2"  (21 bytes):
   00 15 | 41 50 09 48 6f 6d 65 4e 65 74 09 ... | <crc hi> <crc lo>
    └21┘   A  P \t  H  o  m  e  N  e  t \t
```

(The `crc16` is integrity, which we add in the reliability lesson and the
final; for now, focus on the length prefix.) Because the length comes first,
the reader always knows exactly how many payload bytes to expect — even if TCP
hands them over one byte at a time.

This lesson's challenge is the **encoder** — the Pico side. The next lesson's is
the **decoder** — the harder, stream-reassembly side the listener needs.

## Challenge: Encode a Frame {#frame-encode points=15}

### Starter

```c
#include <stddef.h>
#include <stdint.h>

/* Encode one frame: a 2-byte big-endian payload length, then the payload
   bytes. (The CRC comes later; this frame is length + payload only.)
   Return the total number of bytes written to out, or 0 on error: payload
   longer than 65535, or out too small to hold 2 + n bytes. */
size_t frame_encode(const uint8_t *payload, size_t n, uint8_t *out, size_t out_sz) {
	/* TODO */
	(void)payload;
	(void)n;
	(void)out;
	(void)out_sz;
	return 0;
}
```

### Tests

```c
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

size_t frame_encode(const uint8_t *payload, size_t n, uint8_t *out, size_t out_sz);

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
	uint8_t out[16];

	size_t w = frame_encode((const uint8_t *)"hi", 2, out, sizeof out);
	check(w == 4, "test_returns_total_length");
	check(out[0] == 0x00 && out[1] == 0x02, "test_length_prefix_big_endian");
	check(out[2] == 'h' && out[3] == 'i', "test_payload_follows");

	w = frame_encode((const uint8_t *)"", 0, out, sizeof out);
	check(w == 2 && out[0] == 0 && out[1] == 0, "test_empty_payload");

	uint8_t tiny[2];
	check(frame_encode((const uint8_t *)"hi", 2, tiny, sizeof tiny) == 0,
	      "test_out_too_small_returns_0");

	return failed;
}
```

# Lesson: Reassembling the Stream — the Listener {#deframing}

The Pico frames and sends; your laptop must **de-frame**. This is the mirror
image, and it's genuinely harder, because the decoder receives bytes in
whatever clumps TCP feels like, and a single frame may straddle two of them:

```d2
direction: right
s1: "TCP recv #1\n[len=5][ h e l ]"
s2: "TCP recv #2\n[ l o ][len=3][ w ]"
buf: "accumulator\nbuffer" { style.stroke: "#22d3ee"; style.stroke-width: 2 }
r1: "record: hello" { style.stroke: "#34d399" }
r2: "record ...\nwait for rest" { style.stroke-dash: 4; style.font-color: "#9ca3af" }
s1 -> buf
s2 -> buf
buf -> r1: "len bytes present"
buf -> r2: "len not satisfied"
```

The pattern is an **accumulator buffer**: append every chunk that arrives, then
repeatedly try to peel a complete frame off the front. A frame is complete only
when the buffer holds the 2 length bytes *and* the `length` payload bytes that
follow. If it does, emit the record and shift the rest of the buffer down; if
not, wait for more bytes.

## The listener, in Python

On your laptop this is a short program. It listens on a port, and for each chunk
that arrives, drains as many complete frames as it can:

```python
#!/usr/bin/env python3
# listen.py — receive framed scan records from the Pico and print them.
import socket, sys

def crc16_ccitt(data: bytes) -> int:
    crc = 0xFFFF
    for byte in data:
        crc ^= byte << 8
        for _ in range(8):
            crc = ((crc << 1) ^ 0x1021) & 0xFFFF if crc & 0x8000 else (crc << 1) & 0xFFFF
    return crc

def frames(conn):
    """Yield (crc_ok, payload) for each complete [len][payload][crc] frame."""
    buf = bytearray()
    while True:
        chunk = conn.recv(4096)
        if not chunk:
            return                       # Pico closed the socket
        buf += chunk
        while len(buf) >= 2:             # peel off every complete frame
            n = (buf[0] << 8) | buf[1]
            if len(buf) < 2 + n + 2:     # header + payload + crc not all here yet
                break
            payload = bytes(buf[2:2 + n])
            crc = (buf[2 + n] << 8) | buf[2 + n + 1]
            del buf[:2 + n + 2]          # consume the frame
            yield crc16_ccitt(payload) == crc, payload

def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9000
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(("0.0.0.0", port))
    srv.listen(1)
    print(f"listening on :{port} — flash the Pico now")
    conn, addr = srv.accept()
    print(f"pico connected from {addr[0]}")
    for ok, payload in frames(conn):
        line = payload.decode("utf-8", "replace").replace("\t", "  ")
        print(("   " if ok else "!! ") + line)

if __name__ == "__main__":
    main()
```

Run it before you flash the Pico:

```text
$ python3 listen.py
listening on :9000 — flash the Pico now
pico connected from 192.168.1.37
   IP  192.168.1.37  255.255.255.0  192.168.1.1
   AP  HomeNet       -52  6   wpa2
   AP  Neighbor5G    -71  11  wpa2
   AP  <hidden>      -80  1   open
   HOST  192.168.1.1    up    open
   HOST  192.168.1.37   up    self
```

Note it accepts one connection and reads until the Pico closes — exactly what a
single scan session produces. The `crc_ok` flag prints `!!` in front of any
record whose checksum didn't match, so corruption is visible rather than silent.

## Challenge: Decode the Stream {#frame-decode points=20}

Implement the accumulator in C — the same logic the Python does, and the piece
you'd need for a C listener that shares the Pico's exact code. A decoder that
survives frames split across arbitrary chunk boundaries is the heart of this
whole course.

### Starter

```c
#include <stddef.h>
#include <stdint.h>
#include <string.h>

#define FRAME_MAX_PAYLOAD 512

/* Incremental de-framer: bytes arrive from a TCP stream in arbitrary chunks;
   pull out complete [len][payload] records as they become available. */
struct framer {
	uint8_t buf[2 + FRAME_MAX_PAYLOAD];
	size_t  have;   /* bytes currently buffered */
};

void framer_init(struct framer *f) {
	/* TODO */
	(void)f;
}

/* Append up to n incoming bytes. Bytes that would overflow the buffer are
   dropped (the caller is expected to drain with framer_next between pushes). */
void framer_push(struct framer *f, const uint8_t *data, size_t n) {
	/* TODO */
	(void)f;
	(void)data;
	(void)n;
}

/* If a complete record is buffered: copy its payload into payload (which has
   FRAME_MAX_PAYLOAD capacity), store its length in *len, remove the frame from
   the buffer, and return 1. Otherwise return 0. */
int framer_next(struct framer *f, uint8_t *payload, size_t *len) {
	/* TODO */
	(void)f;
	(void)payload;
	(void)len;
	return 0;
}
```

### Tests

```c
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define FRAME_MAX_PAYLOAD 512

struct framer {
	uint8_t buf[2 + FRAME_MAX_PAYLOAD];
	size_t  have;
};

void framer_init(struct framer *f);
void framer_push(struct framer *f, const uint8_t *data, size_t n);
int framer_next(struct framer *f, uint8_t *payload, size_t *len);

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
	/* Two records on the wire: "hello" (5) then "hi" (2). */
	uint8_t stream[] = {
		0x00, 0x05, 'h', 'e', 'l', 'l', 'o',
		0x00, 0x02, 'h', 'i',
	};
	uint8_t p[FRAME_MAX_PAYLOAD];
	size_t l;

	/* Fed in awkward chunks, a frame that straddles a boundary must wait. */
	struct framer f;
	framer_init(&f);
	framer_push(&f, stream, 3);              /* len + 1 payload byte only */
	check(framer_next(&f, p, &l) == 0, "test_partial_frame_waits");
	framer_push(&f, stream + 3, 4);          /* rest of "hello" arrives */
	check(framer_next(&f, p, &l) == 1 && l == 5 && memcmp(p, "hello", 5) == 0,
	      "test_first_record");
	framer_push(&f, stream + 7, 4);          /* the "hi" frame */
	check(framer_next(&f, p, &l) == 1 && l == 2 && memcmp(p, "hi", 2) == 0,
	      "test_second_record");
	check(framer_next(&f, p, &l) == 0, "test_buffer_drained");

	/* Both records delivered in a single chunk must both come out. */
	struct framer g;
	framer_init(&g);
	framer_push(&g, stream, sizeof stream);
	check(framer_next(&g, p, &l) == 1 && l == 5, "test_back_to_back_1");
	check(framer_next(&g, p, &l) == 1 && l == 2, "test_back_to_back_2");
	check(framer_next(&g, p, &l) == 0, "test_back_to_back_done");

	return failed;
}
```

# Lesson: Surviving a Dropped Connection {#reliability}

On a desk with good WiFi everything works. The course is about what happens when
it doesn't: the AP reboots, you walk the Pico out of range, the laptop's
listener isn't up yet. Robust firmware treats a dropped link as normal and
recovers.

The reliability loop uses everything you built. The connection state machine
(`conn_next`) tracks where you are; the backoff (`backoff_delay_ms`) paces
retries; the LED reports status so you can debug without a serial cable:

```d2
direction: right
join: "JOIN WIFI\nLED: slow blink"
stream: "STREAMING\nLED: on" { style.stroke: "#34d399"; style.stroke-width: 2 }
recon: "RECONNECT\nLED: fast blink" { style.stroke: "#f87171"; style.stroke-width: 2 }
join -> stream: "link up + socket open"
stream -> stream: "send record"
stream -> recon: "socket err / WiFi drop"
recon -> join: "backoff, then retry"
```

The main loop, sketched:

```c
enum conn_state st = CONN_WIFI_DOWN;
unsigned attempt = 0;

while (true) {
	switch (st) {
	case CONN_WIFI_DOWN:
		led_blink_slow();
		if (cyw43_arch_wifi_connect_timeout_ms(WIFI_SSID, WIFI_PASS,
		        CYW43_AUTH_WPA2_AES_PSK, 30000) == 0) {
			st = conn_next(st, EV_WIFI_JOINED);
		} else {
			sleep_ms(backoff_delay_ms(attempt++));   /* pace the retries */
		}
		break;
	case CONN_WIFI_UP:
		led_blink_fast();
		if (uplink_open(&up, HOST_IP, HOST_PORT) && wait_connected(&up)) {
			attempt = 0;                              /* success resets backoff */
			st = conn_next(st, EV_SOCKET_OPEN);
		} else if (!wifi_is_up()) {
			st = conn_next(st, EV_WIFI_LOST);
		}
		break;
	case CONN_SOCKET_UP:
		led_on();
		if (!scan_and_stream(&up)) {                  /* a send failed */
			st = conn_next(st, EV_SOCKET_ERR);
		}
		break;
	}
}
```

Two design choices worth calling out:

- **Integrity, not just delivery.** TCP checksums catch transmission bit-flips,
  but not a bug that mangles a record *before* it's sent. The CRC-16 in each
  frame lets the listener flag a bad record (`!!`) instead of trusting garbage —
  cheap insurance that makes corruption visible. You'll implement it in the
  final challenge.
- **A partial record is not a valid record.** If the socket dies mid-frame, the
  listener's accumulator simply never completes that frame and waits — it never
  emits half a record. Length-prefix framing gives you that property for free,
  which is exactly why we didn't newline-delimit.

Everything you need for the final is now on the table: subnet math for the
sweep, safe formatting for the survey, framing to delimit records, backoff and a
state machine for recovery. The final challenge assembles the one piece that
ties the wire together end to end — the protocol codec with integrity — which is
the literal `proto.c` your firmware and (a C) listener would share.

# Final Challenge: The Scan Protocol Codec {#final-protocol points=50}

Build the complete on-the-wire protocol: length-prefix framing **plus** a
CRC-16 integrity check, as both an encoder and a streaming decoder. This is the
module the scanner firmware links against — the code you unit-test here is,
byte for byte, the code that runs on the Pico and the logic the Python listener
mirrors.

The full frame is `[u16 length][payload][u16 crc16(payload)]`, all big-endian.
Implement:

- `crc16_ccitt(data, n)` — CRC-16/CCITT-FALSE: polynomial `0x1021`, initial
  value `0xFFFF`, no bit reflection, no final XOR. The standard check value is
  `crc16_ccitt("123456789", 9) == 0x29B1` — use it to know you got it right.
- `proto_encode(payload, n, out, out_sz)` — write the full frame; return its
  total byte length, or 0 on error (payload over `PROTO_MAX_PAYLOAD`, or `out`
  too small).
- `proto_next(f, payload, len)` — pull the next frame from the accumulator.
  Return `1` for a valid record (payload copied out, `*len` set, frame
  consumed); `-1` for a record whose CRC did **not** match (frame consumed,
  payload discarded — a corrupt record must not wedge the stream); `0` when more
  bytes are needed.

### Starter

```c
#include <stddef.h>
#include <stdint.h>
#include <string.h>

#define PROTO_MAX_PAYLOAD 512

/* CRC-16/CCITT-FALSE: poly 0x1021, init 0xFFFF, no reflection, no final XOR.
   crc16_ccitt("123456789", 9) == 0x29B1. */
uint16_t crc16_ccitt(const uint8_t *data, size_t n) {
	/* TODO */
	(void)data;
	(void)n;
	return 0;
}

/* Encode a full frame: [u16 len][payload][u16 crc16(payload)], big-endian.
   Return bytes written, or 0 on error (n > PROTO_MAX_PAYLOAD, or out_sz too
   small to hold 2 + n + 2 bytes). */
size_t proto_encode(const uint8_t *payload, size_t n, uint8_t *out, size_t out_sz) {
	/* TODO */
	(void)payload;
	(void)n;
	(void)out;
	(void)out_sz;
	return 0;
}

/* Incremental decoder with integrity checking. */
struct framer {
	uint8_t buf[2 + PROTO_MAX_PAYLOAD + 2];
	size_t  have;
};

void framer_init(struct framer *f) {
	/* TODO */
	(void)f;
}

void framer_push(struct framer *f, const uint8_t *data, size_t n) {
	/* TODO */
	(void)f;
	(void)data;
	(void)n;
}

/* Pull the next frame:
    1  valid record  (payload copied to payload, *len set, frame consumed)
   -1  CRC mismatch  (frame consumed, payload discarded)
    0  need more bytes                                                      */
int proto_next(struct framer *f, uint8_t *payload, size_t *len) {
	/* TODO */
	(void)f;
	(void)payload;
	(void)len;
	return 0;
}
```

### Tests

```c
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#define PROTO_MAX_PAYLOAD 512

uint16_t crc16_ccitt(const uint8_t *data, size_t n);
size_t proto_encode(const uint8_t *payload, size_t n, uint8_t *out, size_t out_sz);

struct framer {
	uint8_t buf[2 + PROTO_MAX_PAYLOAD + 2];
	size_t  have;
};
void framer_init(struct framer *f);
void framer_push(struct framer *f, const uint8_t *data, size_t n);
int proto_next(struct framer *f, uint8_t *payload, size_t *len);

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
	/* 1. The standard CRC check value. */
	check(crc16_ccitt((const uint8_t *)"123456789", 9) == 0x29B1, "test_crc_vector");

	/* 2. Encode -> decode round trip. */
	const uint8_t *msg = (const uint8_t *)"AP\tHomeNet\t-52\t6\twpa2";
	size_t mlen = strlen((const char *)msg);
	uint8_t out[600];
	size_t w = proto_encode(msg, mlen, out, sizeof out);
	check(w == mlen + 4, "test_encode_total_length");

	struct framer f;
	uint8_t p[PROTO_MAX_PAYLOAD];
	size_t l;

	framer_init(&f);
	framer_push(&f, out, w);
	check(proto_next(&f, p, &l) == 1 && l == mlen && memcmp(p, msg, mlen) == 0,
	      "test_roundtrip");
	check(proto_next(&f, p, &l) == 0, "test_drained_after_one");

	/* 3. A flipped payload byte must be caught by the CRC. */
	framer_init(&f);
	out[4] ^= 0xFF;                 /* corrupt one payload byte */
	framer_push(&f, out, w);
	check(proto_next(&f, p, &l) == -1, "test_crc_detects_corruption");
	out[4] ^= 0xFF;                 /* restore for the next test */

	/* 4. Two frames, second delivered a byte short then completed. */
	size_t w2 = proto_encode((const uint8_t *)"hi", 2, out + w, sizeof out - w);
	framer_init(&f);
	framer_push(&f, out, w + w2 - 1);            /* hold back the final byte */
	check(proto_next(&f, p, &l) == 1, "test_split_first_frame_ok");
	check(proto_next(&f, p, &l) == 0, "test_split_second_frame_waits");
	framer_push(&f, out + w + w2 - 1, 1);        /* deliver the last byte */
	check(proto_next(&f, p, &l) == 1 && l == 2 && memcmp(p, "hi", 2) == 0,
	      "test_split_second_frame_completes");

	return failed;
}
```

## Putting it on the hardware

The grader proves your codec is correct. The board proves it *works*. To run the
whole thing for real, your project directory holds:

```text
picoscan/
├─ CMakeLists.txt
├─ pico_sdk_import.cmake       # copied from $PICO_SDK_PATH/external/
├─ lwipopts.h                  # lwIP config (adapt pico-examples' common one)
├─ proto.h  proto.c            # the codec you just wrote — unchanged
├─ main.c                      # join WiFi → survey + sweep → frame → uplink
└─ listen.py                   # runs on your laptop
```

The `CMakeLists.txt` links the WiFi + lwIP stack and passes your secrets and the
listener's address in at configure time:

```cmake
cmake_minimum_required(VERSION 3.13)
include(pico_sdk_import.cmake)
project(picoscan C CXX ASM)
pico_sdk_init()

add_executable(picoscan main.c proto.c)
target_include_directories(picoscan PRIVATE ${CMAKE_CURRENT_LIST_DIR})  # lwipopts.h
target_link_libraries(picoscan
    pico_stdlib
    pico_cyw43_arch_lwip_threadsafe_background)
target_compile_definitions(picoscan PRIVATE
    WIFI_SSID=\"${WIFI_SSID}\"
    WIFI_PASS=\"${WIFI_PASS}\"
    HOST_IP=\"${HOST_IP}\"
    HOST_PORT=9000)
pico_enable_stdio_usb(picoscan 1)
pico_add_extra_outputs(picoscan)
```

Then, from the top: start the listener, build with your network and laptop IP,
flash, and watch.

```bash
# 1. On the laptop — find its LAN IP (say 192.168.1.50) and start listening:
python3 listen.py            # prints: listening on :9000 …

# 2. Build the firmware, baking in your WiFi + the laptop's IP:
export PICO_SDK_PATH=~/pico-sdk
cmake -B build -DPICO_BOARD=pico_w \
      -DWIFI_SSID="YourSSID" -DWIFI_PASS="YourPassword" \
      -DHOST_IP="192.168.1.50" .
cmake --build build

# 3. Flash: hold BOOTSEL, plug in, then:
picotool load -f build/picoscan.uf2
```

Within a few seconds the Pico joins WiFi (slow blink → solid), the listener
prints `pico connected from …`, and framed records start streaming: the Pico's
own IP config, then every access point it can hear, then every live host on your
subnet. Pull the AP's power or walk out of range and the LED flips to a fast
blink; the backoff loop keeps trying, and when the link returns the stream
resumes — no half-records, no crash. That is working firmware, and every hard
part of it — the addresses, the subnet range, the frame boundaries, the
integrity check, the recovery — is code you wrote and tested here.
