---
course: embedded-pico
title: Bare-Metal Raspberry Pi Pico
language: c
description: >
  Program the RP2040 the way the hardware sees it: no SDK, just addresses
  from the datasheet. Learn what a memory-mapped register is, why volatile
  matters, how to flip GPIO pins with the SIO's atomic aliases, and how to
  route a pin to the right peripheral — ending with a tiny LED driver you
  could drop onto a real Pico.
duration_hours: 5
tags: [embedded, hardware, c]
extended_reading:
  - title: RP2040 Datasheet (SIO chapter 2.3.1, GPIO chapter 2.19)
    url: https://datasheets.raspberrypi.com/rp2040/rp2040-datasheet.pdf
  - title: Pico SDK hardware_gpio — what the SDK does for you
    url: https://www.raspberrypi.com/documentation/pico-sdk/hardware.html
---

# Lesson: Memory-Mapped Registers {#memory-mapped-registers}

On a desktop OS, hardware hides behind drivers and system calls. On a
microcontroller like the RP2040 (the chip on the Raspberry Pi Pico) the
hardware *is* memory: every peripheral is controlled by **registers**, and
each register is a 32-bit word at a fixed address. Store to the address and
the peripheral reacts; load from it and you see the peripheral's state.

The addresses come from the datasheet. For example, the RP2040's SIO
("single-cycle I/O") block — the fast path the cores use to drive GPIO pins
— lives at base address `0xd0000000`. The register that sets output pins
high, `GPIO_OUT_SET`, is at offset `0x014`, so its full address is
`0xd0000014`.

In C, "store to an address" is a pointer cast and a write:

```c
#include <stdint.h>

#define SIO_BASE      0xd0000000u
#define GPIO_OUT_SET  (SIO_BASE + 0x014u)

*(volatile uint32_t *)GPIO_OUT_SET = 1u << 25;  /* LED pin on the Pico */
```

Two things carry all the weight here:

- **`uintptr_t` / integer-to-pointer casts.** A register address arrives as
  a plain number from the datasheet. Casting it to `volatile uint32_t *`
  tells the compiler "treat this number as the location of a 32-bit word".
- **`volatile`.** It tells the compiler every read and write is *observable
  behavior* that must happen exactly as written. Without it, the optimizer
  may delete "redundant" stores or cache a load in a register — fatal when
  the value at that address is changed by hardware, not by your code.

The grader has no Pico attached, so the tests hand your functions the
addresses of ordinary variables instead of datasheet constants. The pointer
mechanics are *identical* — on real hardware the only difference is where
the number comes from.

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

A register is rarely one value — it is 32 independent switches packed into
a word. GPIO_OUT bit 25 is the Pico's LED; bit 16 might be your I2C bus.
Driving hardware means changing *your* bits without disturbing anyone
else's, which is done with masks:

```c
reg |=  mask;   /* set   every bit that is 1 in mask */
reg &= ~mask;   /* clear every bit that is 1 in mask */
reg ^=  mask;   /* flip  every bit that is 1 in mask */
```

A mask for a single pin is built by shifting: `1u << pin`. Note the `u` —
shifting a plain (signed) `int` left into bit 31 is undefined behavior. The
RP2040's GPIO pins only go up to 29, so `GPIO_OUT` never actually needs bit
31, but plenty of other 32-bit registers use every bit: the SIO has 32
hardware spinlocks, and `SIO_SPINLOCK_ST` reports all of them as a bitmap,
one bit per spinlock, straight through to bit 31. Use `1u`, not `1`, and
which register you're shifting into stops mattering.

Each of those compound assignments is really three steps: **read** the
register, **modify** the copy, **write** it back. Keep that shape in mind —
it works, but the next lesson shows why on the RP2040 you often want the
hardware to do the modify step for you.

## Challenge: Set, Clear, Toggle {#reg-bit-ops points=10}

Implement the three classic read-modify-write helpers. Each takes a pointer
to a register and a mask, and must leave every bit outside the mask exactly
as it found it.

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

Why the SET/CLR/XOR aliases when last lesson's read-modify-write already
works? Because RMW is **three** operations, and between your read and your
write an interrupt handler — or the RP2040's *second* core; it's a dual-core
chip, and both cores can drive GPIO through the same SIO block — may also
touch GPIO_OUT. Do a plain RMW and that other write can land in the gap
between your read and your write, and gets silently overwritten. Writing a
mask to `GPIO_OUT_SET` is **one** store; the hardware does the modify
atomically. This pattern is all over the RP2040, so drivers barely ever RMW
the SIO.

Instead of casting raw offsets one at a time, real drivers describe a whole
register block as a struct whose field layout mirrors the datasheet, then
point it at the base address:

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
struct lives in a header; here each file carries its own copy because
challenge files are self-contained.)

The tests play the part of the hardware: they hand you a `struct pico_sio`
in ordinary memory and then inspect which alias register your code wrote
to, and with what mask.

## Challenge: Drive a Pin Atomically {#sio-pin-ops points=15}

Implement four pin helpers using **only** the SET/CLR/XOR alias registers —
the tests verify that `gpio_out` and `gpio_oe` themselves are never written
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
`0x40014000`): pin *N*'s `GPIO_CTRL` register sits at offset `N*8 + 4`.
Its low five bits — the **FUNCSEL** field, bits 4:0 — pick which peripheral
owns the pin: UART, SPI, PWM… Function **5** is the SIO, i.e. "software
controls this pin". Until you select it, all the GPIO_OUT writing in the
world does nothing visible.

FUNCSEL is where last lesson's warning bites: `GPIO_CTRL` packs *other*
fields into the same word (output overrides, interrupt config). Most RP2040
peripherals — IO_BANK0 included — actually do get atomic bit manipulation for
free: every register is given a 4KB address slot, and writing to its address
plus `0x1000`/`0x2000`/`0x3000` atomically XORs/sets/clears bits with no
read-modify-write at all (datasheet §2.1.2). The SIO you just used is the
*exception* — it's wired straight to the cores off the normal bus, so it
can't do that trick, which is exactly why it needed its own hand-built
SET/CLR/XOR registers. That alias mechanism is one more address computation
on top of what this lesson is really after, though: the field-update shape
below — mask, or, store — which the tests exercise directly on a plain
register variable, and which you need to understand regardless of which
write mechanism eventually lands the bits. So here you'll do it by hand:

```c
uint32_t v = *ctrl;
v &= ~0x1fu;        /* clear the FUNCSEL field   */
v |= funcsel;       /* install the new function  */
*ctrl = v;
```

Mask first, then or — the classic *field update*. Getting this wrong by
writing the whole word would silently zero every other field in the
register.

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

# Final Challenge: An LED Driver {#final points=50}

Put all four lessons together into the driver every embedded project starts
with: the blinking LED. On a real Pico this exact code — pointed at
`0xd0000000` and `0x40014000` instead of test memory — drives the onboard
LED on GPIO 25.

Implement three functions:

- `led_init(sio, ctrl, pin)` — route the pin to the SIO (FUNCSEL 5,
  preserving the rest of the CTRL register), then enable output drive
  through the atomic `GPIO_OE_SET` alias.
- `led_set(sio, pin, on)` — drive the pin high when `on` is nonzero, low
  otherwise, using only the `GPIO_OUT_SET` / `GPIO_OUT_CLR` aliases.
- `led_get(sio, pin)` — return the pin's current output level (0 or 1)
  read from `GPIO_OUT`.

The tests emulate the SIO: after each of your calls they apply whatever you
wrote to the SET/CLR aliases onto `gpio_out`, exactly as the silicon would,
then clear the alias. If you write `gpio_out` directly, the emulation will
catch it.

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
