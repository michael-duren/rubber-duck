#include <stdint.h>

#define ENOSPC 28

/* 1 if `bit` is set in `map`, else 0. */
int bm_isset(const uint8_t *map, uint32_t bit) {
	return (map[bit / 8] >> (bit % 8)) & 1;
}

/* Set `bit` in `map`, leaving every other bit untouched. */
void bm_set(uint8_t *map, uint32_t bit) {
	map[bit / 8] |= (uint8_t)(1u << (bit % 8));
}

/* Clear `bit` in `map`, leaving every other bit untouched. */
void bm_clear(uint8_t *map, uint32_t bit) {
	map[bit / 8] &= (uint8_t)~(1u << (bit % 8));
}

/*
 * Allocate: find the lowest clear bit in [1, nbits), set it, return
 * its number. Never return 0, even if bit 0 is (wrongly) clear.
 * Return -ENOSPC if no bit in [1, nbits) is clear.
 */
int bm_alloc(uint8_t *map, uint32_t nbits) {
	for (uint32_t bit = 1; bit < nbits; bit++) {
		if (!bm_isset(map, bit)) {
			bm_set(map, bit);
			return (int)bit;
		}
	}
	return -ENOSPC;
}

/* Count of clear (free) bits in [1, nbits) — bit 0 never counts. */
uint32_t bm_count_free(const uint8_t *map, uint32_t nbits) {
	uint32_t n = 0;

	for (uint32_t bit = 1; bit < nbits; bit++)
		if (!bm_isset(map, bit))
			n++;
	return n;
}
