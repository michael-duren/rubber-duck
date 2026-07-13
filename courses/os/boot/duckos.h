#ifndef DUCKOS_H
#define DUCKOS_H

#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>

/* ---- klib.c: the kmem challenge, wearing the names gcc expects ---- */
void *memset(void *dst, int c, size_t n);
void *memcpy(void *dst, const void *src, size_t n);
void *memmove(void *dst, const void *src, size_t n);
size_t strlen(const char *s);

/* ---- io: the two instructions every driver lesson simulated ---- */
static inline void outb(uint16_t port, uint8_t val)
{
	__asm__ volatile("outb %0, %1" : : "a"(val), "Nd"(port));
}

static inline uint8_t inb(uint16_t port)
{
	uint8_t val;

	__asm__ volatile("inb %1, %0" : "=a"(val) : "Nd"(port));
	return val;
}

/* ---- console.c: the vga-console challenge, verbatim ---- */
#define VGA_COLS 80
#define VGA_ROWS 25

struct console {
	uint16_t buf[VGA_COLS * VGA_ROWS];
	int row, col;
	uint8_t attr;
};

void console_init(struct console *c, uint8_t attr);
void console_putc(struct console *c, char ch);
void console_puts(struct console *c, const char *s);

/* THE seam: the same struct, placed where the hardware looks. */
#define VGA_CONSOLE ((struct console *)0xB8000)

/* ---- printf.c: the kvsnprintf challenge, verbatim, plus kprintf ---- */
int kvsnprintf(char *dst, size_t size, const char *fmt, va_list ap);
int ksnprintf(char *dst, size_t size, const char *fmt, ...);
void kprintf(const char *fmt, ...);

/* ---- serial.c: COM1 mirror so `-serial stdio` shows the boot ---- */
void serial_init(void);
void serial_putc(char c);
void serial_puts(const char *s);

/* ---- gdt.c / idt.c: the encoders, loaded for real ---- */
void gdt_encode(uint8_t out[8], uint32_t base, uint32_t limit,
                uint8_t access, uint8_t flags);
void gdt_install(void);
void idt_gate(uint8_t out[8], uint32_t handler_offset, uint16_t selector,
              int dpl, int trap);
void idt_install(void);

/* ---- pic.c: the real 8259 pair (the course modeled one in bytes) ---- */
void pic_remap(void);
void pic_send_eoi(int irq);

/* ---- pit.c: the pit-divisor challenge, wired to port 0x40 ---- */
#define HZ 100
uint32_t pit_divisor(uint32_t hz);
void pit_program(uint32_t hz);

/* ---- kbd.c: the scancode-decode challenge, fed by IRQ 1 ---- */
#define KEY_NONE  0
#define KEY_UP    0x100
#define KEY_DOWN  0x101
#define KEY_LEFT  0x102
#define KEY_RIGHT 0x103

struct kbd {
	int shift;
	int ctrl;
	int caps;
	int e0;
};

void kbd_init(struct kbd *k);
int kbd_decode(struct kbd *k, uint8_t sc);

/* ---- isr.S / kernel.c ---- */
void irq0_stub(void);
void irq1_stub(void);
void timer_isr(void);
void kbd_isr(void);

#endif
