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
	out[0] = (uint8_t)(limit & 0xFF);
	out[1] = (uint8_t)((limit >> 8) & 0xFF);
	out[2] = (uint8_t)(base & 0xFF);
	out[3] = (uint8_t)((base >> 8) & 0xFF);
	out[4] = (uint8_t)((base >> 16) & 0xFF);
	out[5] = access;
	out[6] = (uint8_t)(((flags & 0xF) << 4) | ((limit >> 16) & 0xF));
	out[7] = (uint8_t)((base >> 24) & 0xFF);
}
