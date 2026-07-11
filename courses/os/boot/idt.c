#include "duckos.h"

#define GATE_INTR 0xE
#define GATE_TRAP 0xF

/* The idt-gate challenge, verbatim. */
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

/*
 * 256 gates; all not-present (zeroed) except the two we serve.
 * After pic_remap, IRQ 0 arrives at vector 0x20 and IRQ 1 at 0x21.
 * KCODE_SEL 0x08 is the code descriptor gdt_install just loaded.
 */
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
