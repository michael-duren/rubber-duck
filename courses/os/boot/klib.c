#include "duckos.h"

/*
 * DuckOS klib: freestanding memory and string routines.
 *
 * These are the functions gcc assumes exist even with -ffreestanding
 * (struct assignment and initializers may compile into calls to them),
 * plus the two string routines everything else in the kernel wants.
 * All loops must work on bytes as unsigned char.
 */

/* Fill n bytes of dst with (unsigned char)c; return dst. */
void *memset(void *dst, int c, size_t n) {
	unsigned char *d = dst;
	unsigned char v = (unsigned char)c;
	for (size_t i = 0; i < n; i++)
		d[i] = v;
	return dst;
}

/* Copy n bytes from src to dst; regions must not overlap; return dst. */
void *memcpy(void *dst, const void *src, size_t n) {
	unsigned char *d = dst;
	const unsigned char *s = src;
	for (size_t i = 0; i < n; i++)
		d[i] = s[i];
	return dst;
}

/*
 * Copy n bytes from src to dst, correct for overlapping regions in
 * either direction; return dst.  dst < src: copy forward.
 * dst > src: copy backward so no byte is overwritten before it is read.
 */
void *memmove(void *dst, const void *src, size_t n) {
	unsigned char *d = dst;
	const unsigned char *s = src;
	if (d < s) {
		for (size_t i = 0; i < n; i++)
			d[i] = s[i];
	} else if (d > s) {
		for (size_t i = n; i > 0; i--)
			d[i - 1] = s[i - 1];
	}
	return dst;
}

/* Length of NUL-terminated s, not counting the NUL. */
size_t strlen(const char *s) {
	size_t n = 0;
	while (s[n] != '\0')
		n++;
	return n;
}

/*
 * Compare NUL-terminated strings byte by byte AS UNSIGNED CHAR.
 * Return <0, 0, >0 as a sorts before, equal to, after b.
 */
int kstrcmp(const char *a, const char *b) {
	const unsigned char *ua = (const unsigned char *)a;
	const unsigned char *ub = (const unsigned char *)b;
	while (*ua && *ua == *ub) {
		ua++;
		ub++;
	}
	return (int)*ua - (int)*ub;
}
