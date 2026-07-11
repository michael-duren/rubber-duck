#include <stdint.h>

#define MINIX_MAGIC 0x137F
#define BLOCK_SIZE 1024
#define EINVAL 22

struct superblock {
	uint16_t ninodes;	/* inodes 1..ninodes */
	uint16_t nzones;	/* total zones incl. metadata region */
	uint16_t imap_blocks;	/* blocks of inode bitmap */
	uint16_t zmap_blocks;	/* blocks of zone bitmap */
	uint16_t firstdatazone;
	uint16_t log_zone_size;	/* must be 0 for us */
	uint32_t max_size;
	uint16_t magic;
};

static uint16_t le16(const uint8_t *b)
{
	return (uint16_t)(b[0] | (b[1] << 8));
}

static uint32_t le32(const uint8_t *b)
{
	return (uint32_t)b[0] | (uint32_t)b[1] << 8 |
	       (uint32_t)b[2] << 16 | (uint32_t)b[3] << 24;
}

int sb_parse(const uint8_t *block, struct superblock *out) {
	struct superblock sb;

	sb.ninodes = le16(block + 0);
	sb.nzones = le16(block + 2);
	sb.imap_blocks = le16(block + 4);
	sb.zmap_blocks = le16(block + 6);
	sb.firstdatazone = le16(block + 8);
	sb.log_zone_size = le16(block + 10);
	sb.max_size = le32(block + 12);
	sb.magic = le16(block + 16);

	if (sb.magic != MINIX_MAGIC)
		return -EINVAL;
	if (sb.log_zone_size != 0)
		return -EINVAL;
	if (sb.imap_blocks == 0 || sb.zmap_blocks == 0)
		return -EINVAL;
	if (sb.firstdatazone < 2u + sb.imap_blocks + sb.zmap_blocks ||
	    sb.firstdatazone >= sb.nzones)
		return -EINVAL;
	*out = sb;
	return 0;
}

/* First block of the inode bitmap. Fixed by the format. */
uint32_t sb_imap_start(const struct superblock *sb) {
	(void)sb;
	return 2;
}

/* First block of the zone bitmap: right after the inode bitmap. */
uint32_t sb_zmap_start(const struct superblock *sb) {
	return 2u + sb->imap_blocks;
}

/* First block of the inode table: right after both bitmaps. */
uint32_t sb_inode_table_start(const struct superblock *sb) {
	return 2u + sb->imap_blocks + sb->zmap_blocks;
}

/* Blocks the inode table occupies: ninodes 32-byte inodes, rounded
 * up to whole blocks. */
uint32_t sb_inode_table_blocks(const struct superblock *sb) {
	return (sb->ninodes * 32u + BLOCK_SIZE - 1) / BLOCK_SIZE;
}
