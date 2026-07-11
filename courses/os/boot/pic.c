#include "duckos.h"

/*
 * The real 8259 pair. The course's pic8259 challenge modeled the
 * IRR/ISR/IMR dance in bytes; this file is its hardware counterpart:
 * the initialization word sequence that remaps the master PIC from
 * its power-on vector base 0x08 (which collides with CPU exceptions
 * -- the design wart *Interrupts and the IDT* complained about) up
 * to 0x20, and the EOI that clears the in-service bit pic_eoi
 * cleared in software.
 */

#define PIC1_CMD  0x20
#define PIC1_DATA 0x21
#define PIC2_CMD  0xA0
#define PIC2_DATA 0xA1

void pic_remap(void)
{
	outb(PIC1_CMD, 0x11);	/* ICW1: init, expect ICW4 */
	outb(PIC2_CMD, 0x11);
	outb(PIC1_DATA, 0x20);	/* ICW2: master vectors 0x20-0x27 */
	outb(PIC2_DATA, 0x28);	/* ICW2: slave  vectors 0x28-0x2F */
	outb(PIC1_DATA, 0x04);	/* ICW3: slave on master line 2 */
	outb(PIC2_DATA, 0x02);	/* ICW3: slave identity */
	outb(PIC1_DATA, 0x01);	/* ICW4: 8086 mode */
	outb(PIC2_DATA, 0x01);

	/* Mask everything except IRQ 0 (PIT) and IRQ 1 (keyboard). */
	outb(PIC1_DATA, 0xFC);
	outb(PIC2_DATA, 0xFF);
}

void pic_send_eoi(int irq)
{
	if (irq >= 8)
		outb(PIC2_CMD, 0x20);
	outb(PIC1_CMD, 0x20);
}
