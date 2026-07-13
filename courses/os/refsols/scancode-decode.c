#include <stdint.h>

#define KEY_NONE  0
#define KEY_UP    0x100
#define KEY_DOWN  0x101
#define KEY_LEFT  0x102
#define KEY_RIGHT 0x103

#define SC_LSHIFT 0x2A
#define SC_RSHIFT 0x36
#define SC_LCTRL  0x1D
#define SC_CAPS   0x3A

#define SC_E0_UP    0x48
#define SC_E0_DOWN  0x50
#define SC_E0_LEFT  0x4B
#define SC_E0_RIGHT 0x4D

struct kbd {
	int shift;	/* count of shift keys held (L+R) */
	int ctrl;	/* left ctrl held */
	int caps;	/* caps lock toggle */
	int e0;		/* saw 0xE0 prefix, awaiting second byte */
};

/* Scancode set 1 -> ASCII, unshifted. 0 = no character. */
static const char keymap[128] = {
	[0x02] = '1',  [0x03] = '2', [0x04] = '3',  [0x05] = '4',
	[0x06] = '5',  [0x07] = '6', [0x08] = '7',  [0x09] = '8',
	[0x0A] = '9',  [0x0B] = '0', [0x0C] = '-',  [0x0D] = '=',
	[0x0E] = '\b', [0x0F] = '\t',
	[0x10] = 'q',  [0x11] = 'w', [0x12] = 'e',  [0x13] = 'r',
	[0x14] = 't',  [0x15] = 'y', [0x16] = 'u',  [0x17] = 'i',
	[0x18] = 'o',  [0x19] = 'p', [0x1A] = '[',  [0x1B] = ']',
	[0x1C] = '\n',
	[0x1E] = 'a',  [0x1F] = 's', [0x20] = 'd',  [0x21] = 'f',
	[0x22] = 'g',  [0x23] = 'h', [0x24] = 'j',  [0x25] = 'k',
	[0x26] = 'l',  [0x27] = ';', [0x28] = '\'', [0x29] = '`',
	[0x2B] = '\\',
	[0x2C] = 'z',  [0x2D] = 'x', [0x2E] = 'c',  [0x2F] = 'v',
	[0x30] = 'b',  [0x31] = 'n', [0x32] = 'm',  [0x33] = ',',
	[0x34] = '.',  [0x35] = '/',
	[0x39] = ' ',
};

/* Scancode set 1 -> ASCII with shift held. 0 = no character. */
static const char keymap_shift[128] = {
	[0x02] = '!',  [0x03] = '@', [0x04] = '#',  [0x05] = '$',
	[0x06] = '%',  [0x07] = '^', [0x08] = '&',  [0x09] = '*',
	[0x0A] = '(',  [0x0B] = ')', [0x0C] = '_',  [0x0D] = '+',
	[0x0E] = '\b', [0x0F] = '\t',
	[0x10] = 'Q',  [0x11] = 'W', [0x12] = 'E',  [0x13] = 'R',
	[0x14] = 'T',  [0x15] = 'Y', [0x16] = 'U',  [0x17] = 'I',
	[0x18] = 'O',  [0x19] = 'P', [0x1A] = '{',  [0x1B] = '}',
	[0x1C] = '\n',
	[0x1E] = 'A',  [0x1F] = 'S', [0x20] = 'D',  [0x21] = 'F',
	[0x22] = 'G',  [0x23] = 'H', [0x24] = 'J',  [0x25] = 'K',
	[0x26] = 'L',  [0x27] = ':', [0x28] = '"',  [0x29] = '~',
	[0x2B] = '|',
	[0x2C] = 'Z',  [0x2D] = 'X', [0x2E] = 'C',  [0x2F] = 'V',
	[0x30] = 'B',  [0x31] = 'N', [0x32] = 'M',  [0x33] = '<',
	[0x34] = '>',  [0x35] = '?',
	[0x39] = ' ',
};

/* Reset the decoder: no modifiers held, caps off, no pending prefix. */
void kbd_init(struct kbd *k)
{
	k->shift = 0;
	k->ctrl = 0;
	k->caps = 0;
	k->e0 = 0;
}

/* Consume one scancode byte; return an ASCII char (> 0), a KEY_*
 * code, or KEY_NONE. Updates *k as a side effect. Decode order:
 * pending 0xE0, then 0xE0 itself, then breaks, then modifier makes,
 * then character lookup (ctrl beats shift beats caps). */
int kbd_decode(struct kbd *k, uint8_t sc)
{
	char c;

	if (k->e0) {
		k->e0 = 0;
		switch (sc) {
		case SC_E0_UP:    return KEY_UP;
		case SC_E0_DOWN:  return KEY_DOWN;
		case SC_E0_LEFT:  return KEY_LEFT;
		case SC_E0_RIGHT: return KEY_RIGHT;
		default:          return KEY_NONE;
		}
	}
	if (sc == 0xE0) {
		k->e0 = 1;
		return KEY_NONE;
	}
	if (sc & 0x80) {
		sc &= 0x7F;
		if (sc == SC_LSHIFT || sc == SC_RSHIFT) {
			if (k->shift > 0)
				k->shift--;
		} else if (sc == SC_LCTRL) {
			k->ctrl = 0;
		}
		return KEY_NONE;
	}
	if (sc == SC_LSHIFT || sc == SC_RSHIFT) {
		k->shift++;
		return KEY_NONE;
	}
	if (sc == SC_LCTRL) {
		k->ctrl = 1;
		return KEY_NONE;
	}
	if (sc == SC_CAPS) {
		k->caps ^= 1;
		return KEY_NONE;
	}
	c = keymap[sc];
	if (c == 0)
		return KEY_NONE;
	if (k->ctrl && c >= 'a' && c <= 'z')
		return c - 'a' + 1;
	if (k->shift)
		c = keymap_shift[sc];
	if (k->caps) {
		if (c >= 'a' && c <= 'z')
			c = c - 'a' + 'A';
		else if (c >= 'A' && c <= 'Z')
			c = c - 'A' + 'a';
	}
	return c;
}
