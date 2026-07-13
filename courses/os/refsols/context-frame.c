#include <stdint.h>

struct cpu_context {          /* what the switch code saves/restores */
	uint32_t edi, esi, ebx, ebp;   /* callee-saved (cdecl) */
	uint32_t eip;                  /* return address the switch "returns" to */
};

struct trap_frame {           /* what iret pops, innermost last */
	uint32_t eip; uint32_t cs; uint32_t eflags; uint32_t esp; uint32_t ss;
};

#define EFLAGS_IF 0x200		/* bit 9: interrupts enabled */
#define KCODE_SEL 0x08		/* kernel code selector (ring 0) */
#define UCODE_SEL 0x1B		/* user code selector (ring 3) */
#define UDATA_SEL 0x23		/* user data/stack selector (ring 3) */

/* Forge the kernel switch context for a never-run process: zero the
 * callee-saved registers, point eip at the entry function. */
void context_init(struct cpu_context *c, uint32_t entry)
{
	c->edi = 0;
	c->esi = 0;
	c->ebx = 0;
	c->ebp = 0;
	c->eip = entry;
}

/* Forge the trap frame that iret pops to enter user mode for the
 * first time: entry point, user code/stack selectors, the given user
 * stack top — and interrupts ENABLED, or the first tick never comes. */
void trap_frame_init_user(struct trap_frame *f, uint32_t entry,
                          uint32_t user_stack_top)
{
	f->eip = entry;
	f->cs = UCODE_SEL;
	f->eflags = EFLAGS_IF;
	f->esp = user_stack_top;
	f->ss = UDATA_SEL;
}
