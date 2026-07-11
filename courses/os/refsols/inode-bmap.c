#include <stdint.h>
#include <string.h>

#define BLOCK_SIZE 1024
#define NDIRECT 7
#define PTRS_PER_BLOCK (BLOCK_SIZE / 2)	/* 512 u16 zone numbers */
#define NDISK 4096	/* zones on our toy disk */
#define EINVAL 22
#define EIO 5

struct inode {
	uint32_t size;		/* file length in bytes */
	uint16_t zones[9];	/* [0..6] direct, [7] indirect, [8] double */
};

/* One little-endian u16: indirect zones are arrays of these. */
static inline uint16_t get_le16(const uint8_t *p)
{
	return (uint16_t)(p[0] | ((uint16_t)p[1] << 8));
}

/* Entry `idx` of the indirect block living in zone `zone`. */
static uint16_t indirect_entry(const uint8_t *disk, uint16_t zone,
			       uint32_t idx)
{
	return get_le16(disk + (uint32_t)zone * BLOCK_SIZE + idx * 2);
}

int inode_bmap(const struct inode *ino, const uint8_t *disk,
	       uint32_t fileblock)
{
	uint16_t zone, l1;

	if (fileblock < NDIRECT) {
		zone = ino->zones[fileblock];
		if (zone >= NDISK)
			return -EIO;
		return zone;
	}
	fileblock -= NDIRECT;
	if (fileblock < PTRS_PER_BLOCK) {
		zone = ino->zones[7];
		if (zone == 0)
			return 0;
		if (zone >= NDISK)
			return -EIO;
		zone = indirect_entry(disk, zone, fileblock);
		if (zone >= NDISK)
			return -EIO;
		return zone;
	}
	fileblock -= PTRS_PER_BLOCK;
	if (fileblock < (uint32_t)PTRS_PER_BLOCK * PTRS_PER_BLOCK) {
		zone = ino->zones[8];
		if (zone == 0)
			return 0;
		if (zone >= NDISK)
			return -EIO;
		l1 = indirect_entry(disk, zone, fileblock / PTRS_PER_BLOCK);
		if (l1 == 0)
			return 0;
		if (l1 >= NDISK)
			return -EIO;
		zone = indirect_entry(disk, l1, fileblock % PTRS_PER_BLOCK);
		if (zone >= NDISK)
			return -EIO;
		return zone;
	}
	return -EINVAL;
}

int inode_read(const struct inode *ino, const uint8_t *disk,
	       uint32_t off, uint8_t *dst, uint32_t n)
{
	uint32_t done = 0;

	if (off >= ino->size)
		return 0;
	if (n > ino->size - off)
		n = ino->size - off;
	while (done < n) {
		uint32_t fileblock = (off + done) / BLOCK_SIZE;
		uint32_t boff = (off + done) % BLOCK_SIZE;
		uint32_t chunk = BLOCK_SIZE - boff;
		int zone = inode_bmap(ino, disk, fileblock);

		if (zone < 0)
			return zone;
		if (chunk > n - done)
			chunk = n - done;
		if (zone == 0)
			memset(dst + done, 0, chunk);
		else
			memcpy(dst + done,
			       disk + (uint32_t)zone * BLOCK_SIZE + boff,
			       chunk);
		done += chunk;
	}
	return (int)done;
}
