#include <stdint.h>
#include <string.h>

#define NBUF 8			/* buffers in the cache */
#define BLOCK_SIZE 1024		/* Minix v1: two 512-byte sectors */
#define NDISK 64		/* blocks on our toy disk */

struct disk {
	uint8_t blocks[NDISK][BLOCK_SIZE];
	int nreads;
	int nwrites;
};

struct buf {
	int valid;		/* 1 if this buffer holds a copy of block `blockno` */
	int dirty;		/* 1 if data is newer than the disk's copy */
	int refcnt;		/* pins; >0 means in use, never evict */
	uint32_t blockno;	/* which disk block this buffer caches */
	uint32_t lastuse;	/* stamp from cache->clock at each get */
	uint8_t data[BLOCK_SIZE];
};

struct bcache {
	struct buf bufs[NBUF];
	uint32_t clock;		/* monotonic, ++ on every bc_get */
};

/* ---- Provided driver: each call below is one real disk I/O. ---- */

void disk_read(struct disk *d, uint32_t blockno, uint8_t *dst) {
	memcpy(dst, d->blocks[blockno], BLOCK_SIZE);
	d->nreads++;
}

void disk_write(struct disk *d, uint32_t blockno, const uint8_t *src) {
	memcpy(d->blocks[blockno], src, BLOCK_SIZE);
	d->nwrites++;
}

/* ---- The cache. ---- */

/* Empty cache: all buffers invalid/clean/unpinned, clock = 0. */
void bc_init(struct bcache *c) {
	for (int i = 0; i < NBUF; i++) {
		c->bufs[i].valid = 0;
		c->bufs[i].dirty = 0;
		c->bufs[i].refcnt = 0;
		c->bufs[i].blockno = 0;
		c->bufs[i].lastuse = 0;
	}
	c->clock = 0;
}

struct buf *bc_get(struct bcache *c, struct disk *d, uint32_t blockno) {
	struct buf *victim = NULL;

	if (blockno >= NDISK)
		return NULL;
	for (int i = 0; i < NBUF; i++) {
		struct buf *b = &c->bufs[i];

		if (b->valid && b->blockno == blockno) {
			b->refcnt++;
			b->lastuse = ++c->clock;
			return b;
		}
	}
	for (int i = 0; i < NBUF; i++) {
		struct buf *b = &c->bufs[i];

		if (!b->valid) {
			victim = b;
			break;
		}
		if (b->refcnt == 0 &&
		    (victim == NULL || b->lastuse < victim->lastuse))
			victim = b;
	}
	if (victim == NULL)
		return NULL;
	if (victim->valid && victim->dirty)
		disk_write(d, victim->blockno, victim->data);
	disk_read(d, blockno, victim->data);
	victim->valid = 1;
	victim->dirty = 0;
	victim->refcnt = 1;
	victim->blockno = blockno;
	victim->lastuse = ++c->clock;
	return victim;
}

/* Drop one pin (refcnt floors at 0). Data stays cached and valid. */
void bc_release(struct bcache *c, struct buf *b) {
	(void)c;
	if (b->refcnt > 0)
		b->refcnt--;
}

/* The caller modified b->data; remember that the disk is now stale. */
void bc_mark_dirty(struct buf *b) {
	b->dirty = 1;
}

/*
 * Flush every valid dirty buffer to disk and clear its dirty flag.
 * Returns the number of buffers flushed.
 */
int bc_sync(struct bcache *c, struct disk *d) {
	int n = 0;

	for (int i = 0; i < NBUF; i++) {
		struct buf *b = &c->bufs[i];

		if (b->valid && b->dirty) {
			disk_write(d, b->blockno, b->data);
			b->dirty = 0;
			n++;
		}
	}
	return n;
}
