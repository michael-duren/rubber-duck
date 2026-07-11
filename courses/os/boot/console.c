#include "duckos.h"



static uint16_t blank_cell(const struct console *c)
{
	return (uint16_t)((c->attr << 8) | ' ');
}

/*
 * Set the console's attribute to attr, fill the whole screen with blank
 * cells (space, attr), and home the cursor to (0, 0).
 */
void console_init(struct console *c, uint8_t attr)
{
	int i;

	c->attr = attr;
	for (i = 0; i < VGA_COLS * VGA_ROWS; i++)
		c->buf[i] = blank_cell(c);
	c->row = 0;
	c->col = 0;
}

static void scroll(struct console *c)
{
	int i;

	memmove(c->buf, c->buf + VGA_COLS,
		(VGA_ROWS - 1) * VGA_COLS * sizeof(c->buf[0]));
	for (i = 0; i < VGA_COLS; i++)
		c->buf[(VGA_ROWS - 1) * VGA_COLS + i] = blank_cell(c);
	c->row = VGA_ROWS - 1;
}

/*
 * Write one byte to the console.
 *
 * '\n' -> column 0 of the next row.
 * '\r' -> column 0 of the same row.
 * '\b' -> back one column (not past column 0); erases nothing.
 * '\t' -> write blanks until the column is the next multiple of 8
 *         (always advances at least one column).
 * Other bytes -> store (attr << 8) | byte at the cursor, advance a column.
 *
 * Whenever the column reaches VGA_COLS, wrap to column 0 of the next row.
 * Whenever the row reaches VGA_ROWS, scroll: move rows 1..24 up one row
 * (overlapping copy!), blank the bottom row, and set row to VGA_ROWS - 1.
 */
void console_putc(struct console *c, char ch)
{
	switch (ch) {
	case '\n':
		c->col = 0;
		c->row++;
		break;
	case '\r':
		c->col = 0;
		return;
	case '\b':
		if (c->col > 0)
			c->col--;
		return;
	case '\t':
		do {
			c->buf[c->row * VGA_COLS + c->col] = blank_cell(c);
			c->col++;
		} while (c->col % 8 != 0);	/* VGA_COLS % 8 == 0: no overrun */
		break;
	default:
		c->buf[c->row * VGA_COLS + c->col] =
			(uint16_t)((c->attr << 8) | (uint8_t)ch);
		c->col++;
		break;
	}
	if (c->col >= VGA_COLS) {
		c->col = 0;
		c->row++;
	}
	if (c->row >= VGA_ROWS)
		scroll(c);
}

/* Write each byte of the NUL-terminated string s via console_putc. */
void console_puts(struct console *c, const char *s)
{
	while (*s != '\0')
		console_putc(c, *s++);
}
