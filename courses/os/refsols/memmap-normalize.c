#include <stdint.h>

#define PAGE_SIZE  4096
#define MEMMAP_MAX 32		/* n_in never exceeds this */

struct mem_region {
	uint32_t base;
	uint32_t len;
};

static uint32_t round_up(uint32_t x)
{
	return (x + PAGE_SIZE - 1) & ~(uint32_t)(PAGE_SIZE - 1);
}

static uint32_t round_down(uint32_t x)
{
	return x & ~(uint32_t)(PAGE_SIZE - 1);
}

static int emit(struct mem_region *out, int max_out, int n,
		uint32_t start, uint32_t end)
{
	start = round_up(start);
	end = round_down(end);
	if (start >= end)
		return n;
	if (n >= max_out)
		return -1;
	out[n].base = start;
	out[n].len = end - start;
	return n + 1;
}

int memmap_normalize(const struct mem_region *in, int n_in,
                     uint32_t kernel_base, uint32_t kernel_end,
                     struct mem_region *out, int max_out)
{
	uint32_t start[MEMMAP_MAX], end[MEMMAP_MAX];
	int n = 0;

	for (int i = 0; i < n_in; i++) {
		if (in[i].len == 0)
			continue;
		start[n] = in[i].base;
		end[n] = in[i].base + in[i].len;
		n++;
	}

	for (int i = 1; i < n; i++) {
		uint32_t s = start[i], e = end[i];
		int j = i - 1;
		while (j >= 0 && start[j] > s) {
			start[j + 1] = start[j];
			end[j + 1] = end[j];
			j--;
		}
		start[j + 1] = s;
		end[j + 1] = e;
	}

	int m = 0;
	for (int i = 0; i < n; i++) {
		if (m > 0 && start[i] <= end[m - 1]) {
			if (end[i] > end[m - 1])
				end[m - 1] = end[i];
		} else {
			start[m] = start[i];
			end[m] = end[i];
			m++;
		}
	}

	int nout = 0;
	for (int i = 0; i < m; i++) {
		if (end[i] <= kernel_base || start[i] >= kernel_end) {
			nout = emit(out, max_out, nout, start[i], end[i]);
		} else {
			if (start[i] < kernel_base)
				nout = emit(out, max_out, nout, start[i],
					    kernel_base);
			if (nout >= 0 && end[i] > kernel_end)
				nout = emit(out, max_out, nout, kernel_end,
					    end[i]);
		}
		if (nout < 0)
			return -1;
	}
	return nout;
}
