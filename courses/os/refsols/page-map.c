#include <stdint.h>
#include <string.h>

#define PAGE_SIZE 4096
#define NFRAMES   64

#define PTE_P  0x001	/* present */
#define PTE_W  0x002	/* writable */
#define PTE_U  0x004	/* user-accessible */

struct mmu {
	uint8_t phys[NFRAMES * PAGE_SIZE];
	uint32_t cr3;		/* frame number of the page directory */
	uint32_t next_free;	/* bump allocator: next never-used frame */
};

/* Read entry `index` of the table living in frame `frame`. */
uint32_t pte_read(const uint8_t *phys, uint32_t frame, uint32_t index)
{
	const uint8_t *p = phys + frame * PAGE_SIZE + index * 4;

	return (uint32_t)p[0] | (uint32_t)p[1] << 8 |
	       (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}

/* Write entry `index` of the table living in frame `frame`. */
void pte_write(uint8_t *phys, uint32_t frame, uint32_t index,
               uint32_t value)
{
	uint8_t *p = phys + frame * PAGE_SIZE + index * 4;

	p[0] = (uint8_t)(value & 0xff);
	p[1] = (uint8_t)((value >> 8) & 0xff);
	p[2] = (uint8_t)((value >> 16) & 0xff);
	p[3] = (uint8_t)(value >> 24);
}

static uint32_t alloc_frame(struct mmu *m)
{
	return m->next_free++;
}

/* Zero physical memory; allocate frame 0 as the empty page directory. */
void mmu_init(struct mmu *m)
{
	memset(m->phys, 0, sizeof(m->phys));
	m->next_free = 0;
	m->cr3 = alloc_frame(m);
}

int mmu_map(struct mmu *m, uint32_t va, uint32_t frame, uint32_t flags)
{
	uint32_t pd = va >> 22;
	uint32_t pt = (va >> 12) & 0x3ffu;
	uint32_t pde = pte_read(m->phys, m->cr3, pd);
	uint32_t table;

	if (!(pde & PTE_P)) {
		table = alloc_frame(m);
		pte_write(m->phys, m->cr3, pd,
			  table << 12 | PTE_P | PTE_W | PTE_U);
	} else {
		table = pde >> 12;
	}
	if (pte_read(m->phys, table, pt) & PTE_P)
		return -1;
	pte_write(m->phys, table, pt, frame << 12 | flags | PTE_P);
	return 0;
}

int mmu_translate(const struct mmu *m, uint32_t va, uint32_t *pa_out)
{
	uint32_t pde = pte_read(m->phys, m->cr3, va >> 22);
	uint32_t pte;

	if (!(pde & PTE_P))
		return -1;
	pte = pte_read(m->phys, pde >> 12, (va >> 12) & 0x3ffu);
	if (!(pte & PTE_P))
		return -1;
	*pa_out = (pte & ~0xfffu) | (va & 0xfffu);
	return 0;
}

int mmu_unmap(struct mmu *m, uint32_t va)
{
	uint32_t pde = pte_read(m->phys, m->cr3, va >> 22);
	uint32_t pt = (va >> 12) & 0x3ffu;

	if (!(pde & PTE_P))
		return -1;
	if (!(pte_read(m->phys, pde >> 12, pt) & PTE_P))
		return -1;
	pte_write(m->phys, pde >> 12, pt, 0);
	return 0;
}

int mmu_fault_kind(const struct mmu *m, uint32_t va, int is_write,
                   int is_user)
{
	int code = (is_write ? 2 : 0) | (is_user ? 4 : 0);
	uint32_t pde = pte_read(m->phys, m->cr3, va >> 22);
	uint32_t pte;

	if (!(pde & PTE_P))
		return code;
	pte = pte_read(m->phys, pde >> 12, (va >> 12) & 0x3ffu);
	if (!(pte & PTE_P))
		return code;
	if ((is_write && !(pte & PTE_W)) || (is_user && !(pte & PTE_U)))
		return code | 1;
	return 0;
}
