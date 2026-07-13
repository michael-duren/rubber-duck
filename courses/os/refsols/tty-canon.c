#include <string.h>	/* memcpy, memmove — for commit and tty_read */

#define TTY_BUF 256
#define EAGAIN 11

#define CH_ERASE 0x7F	/* DEL — what modern terminals send for backspace */
#define CH_KILL  0x15	/* ^U */
#define CH_EOF   0x04	/* ^D */

struct tty {
	char line[TTY_BUF];	/* edit buffer: current uncommitted line */
	int  len;
	char cooked[TTY_BUF];	/* committed bytes readable by tty_read */
	int  cooked_len;
	int  eof;		/* ^D at line start seen; sticky */
	char echo[TTY_BUF];	/* transcript of everything echoed */
	int  echo_len;
};

/* Append one byte to the echo transcript; silently drop when full. */
static void echo_c(struct tty *t, char c)
{
	if (t->echo_len < TTY_BUF)
		t->echo[t->echo_len++] = c;
}

/* Append a string to the echo transcript. */
static void echo_s(struct tty *t, const char *s)
{
	while (*s)
		echo_c(t, *s++);
}

/* Reset t to an empty tty: no edit line, no cooked bytes, no echo, no EOF. */
void tty_init(struct tty *t)
{
	t->len = 0;
	t->cooked_len = 0;
	t->echo_len = 0;
	t->eof = 0;
}

/* Move the edit line into the cooked buffer (as much as fits). */
static void commit(struct tty *t)
{
	int n = t->len;

	if (n > TTY_BUF - t->cooked_len)
		n = TTY_BUF - t->cooked_len;
	memcpy(t->cooked + t->cooked_len, t->line, (size_t)n);
	t->cooked_len += n;
	t->len = 0;
}

void tty_input(struct tty *t, char c)
{
	if (c == '\b' || c == CH_ERASE) {
		if (t->len > 0) {
			t->len--;
			echo_s(t, "\b \b");
		}
		return;
	}
	if (c == CH_KILL) {
		while (t->len > 0) {
			t->len--;
			echo_s(t, "\b \b");
		}
		return;
	}
	if (c == CH_EOF) {
		if (t->len == 0)
			t->eof = 1;
		else
			commit(t);	/* partial line, no newline, no echo */
		return;
	}
	if (c == '\n') {
		if (t->len < TTY_BUF)
			t->line[t->len++] = '\n';
		echo_c(t, '\n');
		commit(t);
		return;
	}
	if (c >= 0x20 && c <= 0x7E) {
		if (t->len < TTY_BUF) {
			t->line[t->len++] = c;
			echo_c(t, c);
		}
		return;
	}
	/* everything else: ignored */
}

int tty_read(struct tty *t, char *buf, int n)
{
	if (t->cooked_len > 0) {
		if (n > t->cooked_len)
			n = t->cooked_len;
		memcpy(buf, t->cooked, (size_t)n);
		memmove(t->cooked, t->cooked + n,
			(size_t)(t->cooked_len - n));
		t->cooked_len -= n;
		return n;
	}
	if (t->eof)
		return 0;
	return -EAGAIN;
}
