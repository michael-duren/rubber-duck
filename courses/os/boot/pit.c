#include "duckos.h"

#define PIT_HZ 1193182u		/* the PIT input clock, in Hz */

/*
 * Divisor for a desired interrupt rate, rounded to nearest, clamped
 * to the 8253's real range [1, 65536]. hz == 0 asks for "as slow as
 * possible": 65536.
 */
uint32_t pit_divisor(uint32_t hz)
{
	uint32_t d;

	if (hz == 0)
		return 65536;
	d = (PIT_HZ + hz / 2) / hz;
	if (d > 65536)
		return 65536;
	if (d < 1)
		return 1;
	return d;
}

/* The rate a divisor really delivers: PIT_HZ / divisor, rounded to
 * nearest. Divisor 0 means 65536, as on the chip. */
uint32_t pit_actual_hz(uint32_t divisor)
{
	if (divisor == 0)
		divisor = 65536;
	return (PIT_HZ + divisor / 2) / divisor;
}

/* Milliseconds to ticks at HZ, rounding UP: a sleep may run long,
 * never short. */
uint32_t ms_to_ticks(uint32_t ms)
{
	return (ms * HZ + 999) / 1000;
}

/*
 * The three outb's the lesson described: mode byte to 0x43 (channel
 * 0, rate generator, lobyte/hibyte), then the divisor to 0x40.
 * A divisor of 65536 goes over the wire as 0 -- the chip's spelling.
 */
void pit_program(uint32_t hz)
{
	uint32_t d = pit_divisor(hz);

	outb(0x43, 0x36);
	outb(0x40, (uint8_t)(d & 0xFF));
	outb(0x40, (uint8_t)((d >> 8) & 0xFF));
}
