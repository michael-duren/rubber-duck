#include <stdint.h>
#include <string.h>

#define NAME_LEN 14
#define NFILES 32
#define ENOENT 2
#define ENOTDIR 20
#define EINVAL 22
#define ROOT_INO 1

struct nfile {
	uint16_t ino;		/* 0 = free slot */
	uint16_t parent;	/* inode of the containing directory */
	int is_dir;
	char name[NAME_LEN + 1];	/* entry name within parent */
};

/* Record for inode `ino`, or NULL. (Provided.) */
const struct nfile *fs_find(const struct nfile *fs, uint16_t ino)
{
	for (int i = 0; i < NFILES; i++)
		if (fs[i].ino != 0 && fs[i].ino == ino)
			return &fs[i];
	return NULL;
}

uint16_t fs_lookup_in(const struct nfile *fs, uint16_t dir_ino,
                      const char *name)
{
	for (int i = 0; i < NFILES; i++)
		if (fs[i].ino != 0 && fs[i].parent == dir_ino &&
		    fs[i].ino != ROOT_INO &&
		    strcmp(fs[i].name, name) == 0)
			return fs[i].ino;
	return 0;
}

int path_resolve(const struct nfile *fs, const char *path)
{
	uint16_t cur = ROOT_INO;
	const char *p = path;

	if (p[0] != '/')
		return -EINVAL;
	for (;;) {
		char comp[NAME_LEN + 1];
		size_t len = 0;
		const struct nfile *f;
		uint16_t nxt;

		while (*p == '/')
			p++;
		if (*p == '\0')
			return cur;
		while (p[len] != '\0' && p[len] != '/')
			len++;
		if (len > NAME_LEN)
			return -ENOENT;
		memcpy(comp, p, len);
		comp[len] = '\0';
		p += len;

		if (strcmp(comp, ".") == 0) {
			nxt = cur;
		} else if (strcmp(comp, "..") == 0) {
			f = fs_find(fs, cur);
			if (f == NULL)
				return -ENOENT;
			nxt = f->parent;
		} else {
			nxt = fs_lookup_in(fs, cur, comp);
			if (nxt == 0)
				return -ENOENT;
		}
		if (*p == '/') {
			f = fs_find(fs, nxt);
			if (f == NULL || !f->is_dir)
				return -ENOTDIR;
		}
		cur = nxt;
	}
}
