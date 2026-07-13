#include "duckos.h"

struct out {
	char *dst;
	size_t size;
	size_t pos;
};

static inline void out_char(struct out *o, char c)
{
	if (o->pos + 1 < o->size)
		o->dst[o->pos] = c;
	o->pos++;
}

static void out_str(struct out *o, const char *s, int width)
{
	int len = 0;

	if (s == NULL)
		s = "(null)";
	while (s[len] != '\0')
		len++;
	for (int i = len; i < width; i++)
		out_char(o, ' ');
	for (int i = 0; i < len; i++)
		out_char(o, s[i]);
}

static void out_num(struct out *o, unsigned int val, unsigned int base,
		    int negative, int width, int zero)
{
	static const char digits[] = "0123456789abcdef";
	char tmp[12];
	int ndigits = 0;
	int total;

	do {
		tmp[ndigits++] = digits[val % base];
		val /= base;
	} while (val != 0);
	total = ndigits + (negative ? 1 : 0);

	if (zero) {
		if (negative)
			out_char(o, '-');
		for (int i = total; i < width; i++)
			out_char(o, '0');
	} else {
		for (int i = total; i < width; i++)
			out_char(o, ' ');
		if (negative)
			out_char(o, '-');
	}
	while (ndigits > 0)
		out_char(o, tmp[--ndigits]);
}

int kvsnprintf(char *dst, size_t size, const char *fmt, va_list ap)
{
	struct out o = { dst, size, 0 };

	for (const char *f = fmt; *f != '\0'; f++) {
		int zero = 0;
		int width = 0;
		int v;
		unsigned int mag;

		if (*f != '%') {
			out_char(&o, *f);
			continue;
		}
		f++;
		while (*f == '0') {
			zero = 1;
			f++;
		}
		while (*f >= '0' && *f <= '9') {
			width = width * 10 + (*f - '0');
			f++;
		}
		switch (*f) {
		case 'c':
			out_char(&o, (char)va_arg(ap, int));
			break;
		case 's':
			out_str(&o, va_arg(ap, const char *), width);
			break;
		case 'd':
			v = va_arg(ap, int);
			mag = v < 0 ? 0u - (unsigned int)v : (unsigned int)v;
			out_num(&o, mag, 10, v < 0, width, zero);
			break;
		case 'u':
			out_num(&o, va_arg(ap, unsigned int), 10, 0, width,
				zero);
			break;
		case 'x':
			out_num(&o, va_arg(ap, unsigned int), 16, 0, width,
				zero);
			break;
		case '%':
			out_char(&o, '%');
			break;
		case '\0':
			out_char(&o, '%');
			goto done;
		default:
			out_char(&o, '%');
			out_char(&o, *f);
			break;
		}
	}
done:
	if (size > 0)
		dst[o.pos + 1 < size ? o.pos : size - 1] = '\0';
	return (int)o.pos;
}

int ksnprintf(char *dst, size_t size, const char *fmt, ...)
{
	va_list ap;
	int n;

	va_start(ap, fmt);
	n = kvsnprintf(dst, size, fmt, ap);
	va_end(ap);
	return n;
}
