#include <stdint.h>

#define MAX_FRAMES 1024

struct frame_alloc {
	uint32_t bitmap[MAX_FRAMES / 32];	/* bit set = frame in use */
	uint32_t nframes;			/* frames actually managed */
};

static int bit_get(const struct frame_alloc *fa, uint32_t frame)
{
	return (fa->bitmap[frame / 32] >> (frame % 32)) & 1u;
}

static void bit_set(struct frame_alloc *fa, uint32_t frame)
{
	fa->bitmap[frame / 32] |= 1u << (frame % 32);
}

static void bit_clear(struct frame_alloc *fa, uint32_t frame)
{
	fa->bitmap[frame / 32] &= ~(1u << (frame % 32));
}

/*
 * Initialize: frames [0, nframes) free, frames [nframes, MAX_FRAMES)
 * permanently used (guard bits). nframes <= MAX_FRAMES is promised.
 */
void fa_init(struct frame_alloc *fa, uint32_t nframes)
{
	for (int i = 0; i < MAX_FRAMES / 32; i++)
		fa->bitmap[i] = 0;
	fa->nframes = nframes;
	for (uint32_t f = nframes; f < MAX_FRAMES; f++)
		bit_set(fa, f);
}

/*
 * Allocate the lowest-numbered free frame; mark it used.
 * Returns the frame number, or -1 if nothing is free.
 */
int fa_alloc(struct frame_alloc *fa)
{
	for (uint32_t f = 0; f < fa->nframes; f++) {
		if (!bit_get(fa, f)) {
			bit_set(fa, f);
			return (int)f;
		}
	}
	return -1;
}

/*
 * Mark a frame free. Out-of-range frames (frame < 0 or
 * frame >= nframes) are ignored -- never clear a guard bit.
 */
void fa_free(struct frame_alloc *fa, int frame)
{
	if (frame < 0 || (uint32_t)frame >= fa->nframes)
		return;
	bit_clear(fa, (uint32_t)frame);
}

/*
 * Allocate the first run of `count` contiguous free frames
 * (count >= 1). Marks the whole run used and returns its first
 * frame number, or -1 -- with the bitmap unchanged -- if no run
 * of that length exists.
 */
int fa_alloc_run(struct frame_alloc *fa, int count)
{
	if (count < 1 || (uint32_t)count > fa->nframes)
		return -1;
	for (uint32_t start = 0; start + count <= fa->nframes; start++) {
		int free_run = 1;
		for (int i = 0; i < count; i++) {
			if (bit_get(fa, start + i)) {
				free_run = 0;
				break;
			}
		}
		if (free_run) {
			for (int i = 0; i < count; i++)
				bit_set(fa, start + i);
			return (int)start;
		}
	}
	return -1;
}

/*
 * How many managed frames (below nframes) are currently used?
 * Guard bits do not count.
 */
int fa_used(const struct frame_alloc *fa)
{
	int used = 0;

	for (uint32_t f = 0; f < fa->nframes; f++)
		used += bit_get(fa, f);
	return used;
}
