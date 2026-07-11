#include <stdint.h>

/*
 * Real-mode (8086) address translation for DuckOS's boot path.
 *
 * A 16-bit segment and 16-bit offset combine into a linear address:
 * segment * 16 + offset. With the A20 gate disabled the bus has only
 * 20 usable lines, so bit 20 is dropped and addresses wrap at 1MiB.
 */

#define A20_WRAP_MASK 0xFFFFFu	/* 20 address lines: A0..A19 */
#define REAL_MODE_MAX 0x10FFEFu	/* 0xFFFF:0xFFFF with A20 enabled */

/* Linear address of segment:offset. If a20_enabled is 0, the result
   wraps modulo 1MiB; otherwise it may reach up to REAL_MODE_MAX. */
uint32_t linear_address(uint16_t segment, uint16_t offset, int a20_enabled)
{
	uint32_t linear = (uint32_t)segment * 16u + offset;

	if (!a20_enabled)
		linear &= A20_WRAP_MASK;
	return linear;
}

/* Do s1:o1 and s2:o2 name the same byte, with A20 enabled?
   Returns 1 if so, 0 if not. */
int same_linear(uint16_t s1, uint16_t o1, uint16_t s2, uint16_t o2)
{
	return linear_address(s1, o1, 1) == linear_address(s2, o2, 1);
}
