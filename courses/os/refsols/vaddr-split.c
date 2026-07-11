#include <stdint.h>

/* Bits 31:22 -- which page directory entry (0..1023). */
uint32_t vaddr_pd_index(uint32_t va)
{
	return va >> 22;
}

/* Bits 21:12 -- which page table entry (0..1023). */
uint32_t vaddr_pt_index(uint32_t va)
{
	return (va >> 12) & 0x3ffu;
}

/* Bits 11:0 -- byte offset within the 4 KiB page (0..4095). */
uint32_t vaddr_offset(uint32_t va)
{
	return va & 0xfffu;
}

/*
 * The inverse: pack fields back into an address.
 * Requires pd <= 1023, pt <= 1023, off <= 4095 (caller's promise).
 */
uint32_t vaddr_make(uint32_t pd, uint32_t pt, uint32_t off)
{
	return (pd << 22) | (pt << 12) | off;
}
