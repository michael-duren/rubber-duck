#include <stddef.h>
#include <stdint.h>

#define HEAP_SIZE 65536
#define ALIGN 8

struct block {
	uint32_t size;	/* payload bytes in this block (multiple of ALIGN) */
	uint32_t free;	/* 1 = on the free path */
};

static _Alignas(ALIGN) uint8_t heap[HEAP_SIZE];

static struct block *next_block(struct block *b)
{
	uint8_t *p = (uint8_t *)b + sizeof(struct block) + b->size;

	if (p >= heap + HEAP_SIZE)
		return NULL;
	return (struct block *)p;
}

/* Reset the heap: one free block spanning the whole arena. */
void kheap_init(void) {
	struct block *b = (struct block *)heap;

	b->size = HEAP_SIZE - sizeof(struct block);
	b->free = 1;
}

void *kmalloc(uint32_t n) {
	if (n == 0 || n > HEAP_SIZE)
		return NULL;
	n = (n + ALIGN - 1) & ~(uint32_t)(ALIGN - 1);

	for (struct block *b = (struct block *)heap; b != NULL;
	     b = next_block(b)) {
		if (!b->free || b->size < n)
			continue;
		if (b->size - n >= sizeof(struct block) + ALIGN) {
			struct block *tail = (struct block *)
				((uint8_t *)b + sizeof(struct block) + n);
			tail->size = b->size - n - sizeof(struct block);
			tail->free = 1;
			b->size = n;
		}
		b->free = 0;
		return (uint8_t *)b + sizeof(struct block);
	}
	return NULL;
}

void kfree(void *p) {
	if (p == NULL)
		return;
	((struct block *)((uint8_t *)p - sizeof(struct block)))->free = 1;

	for (struct block *b = (struct block *)heap; b != NULL;
	     b = next_block(b)) {
		if (!b->free)
			continue;
		for (struct block *nb = next_block(b);
		     nb != NULL && nb->free; nb = next_block(b))
			b->size += sizeof(struct block) + nb->size;
	}
}

/* Sum of all free blocks' payload bytes (headers don't count). */
uint32_t kheap_free_bytes(void) {
	uint32_t total = 0;

	for (struct block *b = (struct block *)heap; b != NULL;
	     b = next_block(b))
		if (b->free)
			total += b->size;
	return total;
}

/* Payload size of the largest single free block (0 if none). */
uint32_t kheap_largest_free(void) {
	uint32_t best = 0;

	for (struct block *b = (struct block *)heap; b != NULL;
	     b = next_block(b))
		if (b->free && b->size > best)
			best = b->size;
	return best;
}
