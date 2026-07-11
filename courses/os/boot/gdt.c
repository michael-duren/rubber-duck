#include "duckos.h"

/* The gdt-encode challenge, verbatim. */
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

/*
 * The table itself, and the part the course only described: lgdt,
 * then reload every segment register so the CPU actually uses it.
 * These are the exact three descriptors *Segments and Privilege*
 * encoded by hand: null, flat ring-0 code (0x9A/0xC), flat ring-0
 * data (0x92/0xC).
 */
static uint8_t gdt[3][8];

struct gdtr {
	uint16_t limit;
	uint32_t base;
} __attribute__((packed));

void gdt_install(void)
{
	static struct gdtr gdtr;

	memset(gdt[0], 0, 8);				/* the null seat */
	gdt_encode(gdt[1], 0, 0xFFFFF, 0x9A, 0xC);	/* 0x08: code   */
	gdt_encode(gdt[2], 0, 0xFFFFF, 0x92, 0xC);	/* 0x10: data   */

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
