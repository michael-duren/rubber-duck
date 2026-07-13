#include <stdint.h>

/*
 * Master Boot Record parsing for DuckOS's boot path.
 *
 * Layout of the 512-byte sector:
 *   bytes   0..445  bootstrap code
 *   bytes 446..509  partition table, 4 entries x 16 bytes
 *   bytes 510..511  signature 0x55, 0xAA
 *
 * Within each 16-byte entry:
 *   +0  status (0x80 = active)      +4  type (0 = unused)
 *   +8  LBA of first sector, LE32   +12 sector count, LE32
 * (Bytes +1..3 and +5..7 are obsolete CHS fields; ignore them.)
 */

#define MBR_TABLE_OFFSET 446
#define MBR_ENTRY_SIZE   16
#define MBR_ENTRIES      4

struct mbr_partition {
	uint8_t  bootable;	/* 1 iff status byte == 0x80 */
	uint8_t  type;		/* partition type ID; 0 = unused */
	uint32_t lba_start;	/* first sector of the partition */
	uint32_t sector_count;	/* length in 512-byte sectors */
};

/* 1 if sector[510..511] is the 0x55 0xAA boot signature, else 0. */
int mbr_valid(const uint8_t *sector)
{
	return sector[510] == 0x55 && sector[511] == 0xAA;
}

static uint32_t read_le32(const uint8_t *p)
{
	return (uint32_t)p[0]
	     | (uint32_t)p[1] << 8
	     | (uint32_t)p[2] << 16
	     | (uint32_t)p[3] << 24;
}

/* Decode all four partition entries into out[0..3].
   Returns 0 on success; -1 (leaving out untouched) if the boot
   signature is invalid. */
int mbr_parse(const uint8_t *sector, struct mbr_partition out[4])
{
	if (!mbr_valid(sector))
		return -1;
	for (int i = 0; i < MBR_ENTRIES; i++) {
		const uint8_t *e = sector + MBR_TABLE_OFFSET + i * MBR_ENTRY_SIZE;

		out[i].bootable = (e[0] == 0x80);
		out[i].type = e[4];
		out[i].lba_start = read_le32(e + 8);
		out[i].sector_count = read_le32(e + 12);
	}
	return 0;
}

/* Index (0-3) of the first entry with status exactly 0x80 and a
   nonzero type byte; -1 if no such entry. */
int mbr_active_partition(const uint8_t *sector)
{
	for (int i = 0; i < MBR_ENTRIES; i++) {
		const uint8_t *e = sector + MBR_TABLE_OFFSET + i * MBR_ENTRY_SIZE;

		if (e[0] == 0x80 && e[4] != 0)
			return i;
	}
	return -1;
}
