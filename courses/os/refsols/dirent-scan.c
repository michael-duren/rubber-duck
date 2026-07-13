#include <stdint.h>
#include <string.h>

#define DIRENT_SIZE 16
#define NAME_LEN 14

/*
 * 1 if the C string `name` names the fixed field at `entry_name`.
 */
int dir_name_eq(const uint8_t *entry_name, const char *name)
{
	size_t n = strlen(name);

	if (n > NAME_LEN)
		return 0;
	for (size_t i = 0; i < n; i++)
		if (entry_name[i] != (uint8_t)name[i])
			return 0;
	if (n < NAME_LEN && entry_name[n] != 0)
		return 0;
	return 1;
}

uint16_t dir_lookup(const uint8_t *data, uint32_t size, const char *name)
{
	for (uint32_t off = 0; off + DIRENT_SIZE <= size;
	     off += DIRENT_SIZE) {
		uint16_t ino = (uint16_t)(data[off] | (data[off + 1] << 8));

		if (ino == 0)
			continue;
		if (dir_name_eq(data + off + 2, name))
			return ino;
	}
	return 0;
}

/* Live (inode != 0) entries in the table. */
int dir_count(const uint8_t *data, uint32_t size)
{
	int n = 0;

	for (uint32_t off = 0; off + DIRENT_SIZE <= size;
	     off += DIRENT_SIZE)
		if ((data[off] | (data[off + 1] << 8)) != 0)
			n++;
	return n;
}

int dir_entry_name(const uint8_t *data, uint32_t size, int idx, char *out)
{
	int live = 0;

	for (uint32_t off = 0; off + DIRENT_SIZE <= size;
	     off += DIRENT_SIZE) {
		uint16_t ino = (uint16_t)(data[off] | (data[off + 1] << 8));

		if (ino == 0)
			continue;
		if (live == idx) {
			memcpy(out, data + off + 2, NAME_LEN);
			out[NAME_LEN] = '\0';
			return ino;
		}
		live++;
	}
	return 0;
}
