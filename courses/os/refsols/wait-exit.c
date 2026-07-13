#define NPROC 16
#define ECHILD 10
#define ESRCH 3
#define EINVAL 22
#define INIT_PID 1

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;
	enum proc_state state;
	int parent;		/* slot index of parent; -1 for init */
	int exit_status;	/* valid once ZOMBIE */
	int waiting;		/* 1 = blocked in k_wait (state SLEEPING) */
};

struct proc_table {
	struct proc procs[NPROC];
};

/* Clear every slot to PROC_UNUSED. (Provided.) */
void pt_init(struct proc_table *pt)
{
	for (int i = 0; i < NPROC; i++) {
		pt->procs[i].pid = 0;
		pt->procs[i].state = PROC_UNUSED;
		pt->procs[i].parent = -1;
		pt->procs[i].exit_status = 0;
		pt->procs[i].waiting = 0;
	}
}

/*
 * Test scaffolding: place a RUNNABLE process in a slot. (Provided.)
 * parent_slot is a slot index, or -1 for init itself.
 */
void pt_spawn(struct proc_table *pt, int slot, int pid, int parent_slot)
{
	pt->procs[slot].pid = pid;
	pt->procs[slot].state = PROC_RUNNABLE;
	pt->procs[slot].parent = parent_slot;
	pt->procs[slot].exit_status = 0;
	pt->procs[slot].waiting = 0;
}

/*
 * Slot index of the live (non-UNUSED) process with this pid, or -1.
 * Zombies are live for this purpose: they still own their pid.
 */
int pt_find_pid(const struct proc_table *pt, int pid)
{
	for (int i = 0; i < NPROC; i++)
		if (pt->procs[i].state != PROC_UNUSED &&
		    pt->procs[i].pid == pid)
			return i;
	return -1;
}

static void wake(struct proc *p)
{
	p->state = PROC_RUNNABLE;
	p->waiting = 0;
}

int k_exit(struct proc_table *pt, int slot, int status)
{
	int init_slot, inherited_zombie = 0;

	if (slot < 0 || slot >= NPROC ||
	    pt->procs[slot].state == PROC_UNUSED ||
	    pt->procs[slot].state == PROC_ZOMBIE)
		return -ESRCH;

	/* Mark the corpse BEFORE any wakeup — lost-wakeup ordering. */
	pt->procs[slot].exit_status = (status & 0xff) << 8;
	pt->procs[slot].state = PROC_ZOMBIE;
	pt->procs[slot].waiting = 0;

	init_slot = pt_find_pid(pt, INIT_PID);
	for (int i = 0; i < NPROC; i++) {
		if (pt->procs[i].state == PROC_UNUSED ||
		    pt->procs[i].parent != slot)
			continue;
		pt->procs[i].parent = init_slot;	/* -1 if no init */
		if (pt->procs[i].state == PROC_ZOMBIE)
			inherited_zombie = 1;
	}

	if (pt->procs[slot].parent >= 0 &&
	    pt->procs[pt->procs[slot].parent].waiting)
		wake(&pt->procs[pt->procs[slot].parent]);
	if (inherited_zombie && init_slot >= 0 &&
	    pt->procs[init_slot].waiting)
		wake(&pt->procs[init_slot]);
	return 0;
}

int k_wait(struct proc_table *pt, int slot, int *status)
{
	int zombie = -1, have_child = 0;

	if (slot < 0 || slot >= NPROC ||
	    pt->procs[slot].state == PROC_UNUSED ||
	    pt->procs[slot].state == PROC_ZOMBIE)
		return -EINVAL;

	for (int i = 0; i < NPROC; i++) {
		if (pt->procs[i].state == PROC_UNUSED ||
		    pt->procs[i].parent != slot)
			continue;
		have_child = 1;
		if (pt->procs[i].state == PROC_ZOMBIE && zombie == -1)
			zombie = i;
	}
	if (zombie != -1) {
		int pid = pt->procs[zombie].pid;

		*status = pt->procs[zombie].exit_status;
		pt->procs[zombie].state = PROC_UNUSED;
		pt->procs[zombie].pid = 0;
		return pid;
	}
	if (have_child) {
		pt->procs[slot].waiting = 1;
		pt->procs[slot].state = PROC_SLEEPING;
		return 0;
	}
	return -ECHILD;
}
