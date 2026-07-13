#include "duckos.h"

/*
 * kmain: DuckOS, actually booting.
 *
 * Everything with a course lesson behind it is the course's own code
 * (see the sed-level diffs in the epilogue): the console IS the
 * vga-console challenge writing to real VGA memory, the banner IS
 * kvsnprintf, the GDT/IDT bytes come from gdt_encode/idt_gate, the
 * PIT divisor from pit_divisor, every keystroke goes through
 * kbd_decode. The new parts are the ~200 lines of shim around them.
 */

static struct console *cons;	/* = VGA_CONSOLE: the seam */
static struct kbd kbd;
static volatile uint32_t ticks;

void kprintf(const char *fmt, ...)
{
	char buf[256];
	va_list ap;

	va_start(ap, fmt);
	kvsnprintf(buf, sizeof buf, fmt, ap);
	va_end(ap);
	console_puts(cons, buf);
	serial_puts(buf);
}

/* IRQ 0: the heartbeat. One line per second, forever. */
void timer_isr(void)
{
	ticks++;
	if (ticks % HZ == 0)
		kprintf("[%5u.%02u] tick: DuckOS has been alive %u second%s\n",
			ticks / HZ, ticks % HZ, ticks / HZ,
			ticks / HZ == 1 ? "" : "s");
	pic_send_eoi(0);
}

/* IRQ 1: scancode in, character out -- the whole keyboard lesson. */
void kbd_isr(void)
{
	uint8_t sc = inb(0x60);
	int c = kbd_decode(&kbd, sc);

	if (c == '\b') {
		console_puts(cons, "\b \b");	/* the tty lesson's erase */
		serial_puts("\b \b");
	} else if (c > 0 && c < 0x100) {
		char s[2] = { (char)c, 0 };

		kprintf("%s", s);
	} else if (c >= KEY_UP && c <= KEY_RIGHT) {
		static const char *arrows[] = {
			"<UP>", "<DOWN>", "<LEFT>", "<RIGHT>"
		};

		kprintf("%s", arrows[c - KEY_UP]);
	}
	pic_send_eoi(1);
}

void kmain(void)
{
	cons = VGA_CONSOLE;

	serial_init();
	console_init(cons, 0x0A);	/* bright green on black, obviously */

	kprintf("\n");
	kprintf("  =============================================\n");
	kprintf("   DuckOS 0.1 -- quack. it boots.\n");
	kprintf("   console: the vga-console challenge, verbatim\n");
	kprintf("   printf:  the kvsnprintf challenge, verbatim\n");
	kprintf("  =============================================\n\n");

	gdt_install();
	kprintf("[gdt] 3 descriptors encoded by gdt_encode, loaded via lgdt\n");

	idt_install();
	kprintf("[idt] gates 0x20/0x21 encoded by idt_gate, loaded via lidt\n");

	pic_remap();
	kprintf("[pic] 8259 pair remapped to 0x20-0x2F, IRQ 0+1 unmasked\n");

	pit_program(HZ);
	kprintf("[pit] divisor %u -> %u Hz heartbeat\n",
		pit_divisor(HZ), HZ);

	kprintf("[cpu] sti -- interrupts on. type something!\n\n");
	__asm__ volatile("sti");

	for (;;)
		__asm__ volatile("hlt");	/* wake me for interrupts */
}
