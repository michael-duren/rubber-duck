#include "duckos.h"

/*
 * COM1, 38400 8N1. Not part of the course -- it's here so
 * `qemu-system-i386 -kernel duckos.elf -display none -serial stdio`
 * mirrors the boot to your terminal, which is how you script tests
 * against a kernel with no screen.
 */

#define COM1 0x3F8

void serial_init(void)
{
	outb(COM1 + 1, 0x00);	/* no interrupts */
	outb(COM1 + 3, 0x80);	/* DLAB on */
	outb(COM1 + 0, 0x03);	/* divisor 3 = 38400 baud */
	outb(COM1 + 1, 0x00);
	outb(COM1 + 3, 0x03);	/* 8N1, DLAB off */
	outb(COM1 + 2, 0xC7);	/* FIFO on, cleared */
	outb(COM1 + 4, 0x0B);	/* DTR | RTS | OUT2 */
}

void serial_putc(char c)
{
	while (!(inb(COM1 + 5) & 0x20))
		;		/* wait for the transmit buffer */
	outb(COM1, (uint8_t)c);
}

void serial_puts(const char *s)
{
	while (*s) {
		if (*s == '\n')
			serial_putc('\r');
		serial_putc(*s++);
	}
}
