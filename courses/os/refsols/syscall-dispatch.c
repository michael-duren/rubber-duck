#include <stdint.h>

#define NSYSCALLS 8
#define ENOSYS 38
#define EFAULT 14
#define EINVAL 22

#define USER_BASE 0x08048000u	/* classic i386 ELF load address */
#define USER_TOP  0xC0000000u	/* kernel owns the top gigabyte */

struct kernel {
	char log[64];
	int log_len;
};

typedef int (*syscall_fn)(struct kernel *k, uint32_t a1, uint32_t a2, uint32_t a3);

struct syscall_table {
	syscall_fn fn[NSYSCALLS];
};

/*
 * Return 1 iff the whole range [addr, addr+len) lies in user space,
 * [USER_BASE, USER_TOP); 0 otherwise. len == 0 is fine when addr is
 * itself a user address. Must be wrap-safe: validate addr first, then
 * compare len against the room remaining (USER_TOP - addr). Never
 * compute addr + len.
 */
int user_range_ok(uint32_t addr, uint32_t len)
{
	if (addr < USER_BASE || addr >= USER_TOP)
		return 0;
	return len <= USER_TOP - addr;
}

/* Empty the table: every slot NULL. */
void st_init(struct syscall_table *t)
{
	for (int i = 0; i < NSYSCALLS; i++)
		t->fn[i] = 0;
}

int st_register(struct syscall_table *t, uint32_t nr, syscall_fn fn)
{
	if (nr >= NSYSCALLS)
		return -EINVAL;
	t->fn[nr] = fn;
	return 0;
}

int syscall_dispatch(struct kernel *k, const struct syscall_table *t,
                     uint32_t nr, uint32_t a1, uint32_t a2, uint32_t a3)
{
	if (nr >= NSYSCALLS || t->fn[nr] == 0)
		return -ENOSYS;
	return t->fn[nr](k, a1, a2, a3);
}

int sys_klog(struct kernel *k, uint32_t addr, uint32_t len, uint32_t unused)
{
	(void)unused;
	if (!user_range_ok(addr, len))
		return -EFAULT;
	if (k->log_len < (int)sizeof(k->log))
		k->log[k->log_len++] = 'W';
	return (int)len;
}
