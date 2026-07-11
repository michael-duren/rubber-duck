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
	uint8_t type = trap ? GATE_TRAP : GATE_INTR;

	out[0] = (uint8_t)(handler_offset & 0xFF);
	out[1] = (uint8_t)((handler_offset >> 8) & 0xFF);
	out[2] = (uint8_t)(selector & 0xFF);
	out[3] = (uint8_t)(selector >> 8);
	out[4] = 0;
	out[5] = (uint8_t)(0x80 | ((dpl & 3) << 5) | type);
	out[6] = (uint8_t)((handler_offset >> 16) & 0xFF);
	out[7] = (uint8_t)((handler_offset >> 24) & 0xFF);
}
