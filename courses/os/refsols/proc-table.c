#include <stddef.h>

#define NPROC   16
#define PID_MAX 30000

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;		/* public identity; issued by pt_alloc */
	enum proc_state state;	/* PROC_UNUSED means the slot is free */
	int parent;		/* slot index of parent, -1 = none */
};

struct proc_table {
	struct proc procs[NPROC];
	int next_pid;		/* next candidate pid, 1..PID_MAX */
};

/* Mark every slot PROC_UNUSED; pid numbering starts at 1. */
void pt_init(struct proc_table *pt)
{
	for (int i = 0; i < NPROC; i++) {
		pt->procs[i].pid = 0;
		pt->procs[i].state = PROC_UNUSED;
		pt->procs[i].parent = -1;
	}
	pt->next_pid = 1;
}

static int pid_in_use(const struct proc_table *pt, int pid)
{
	for (int i = 0; i < NPROC; i++) {
		if (pt->procs[i].state != PROC_UNUSED && pt->procs[i].pid == pid)
			return 1;
	}
	return 0;
}

/*
 * Claim the lowest UNUSED slot for a new process: assign the next free
 * pid (skipping pids held by live slots, wrapping PID_MAX -> 1), set
 * state PROC_EMBRYO and parent, and return the slot index.
 * Returns -1 if the table is full.
 */
int pt_alloc(struct proc_table *pt, int parent_slot)
{
	int slot = -1;
	for (int i = 0; i < NPROC; i++) {
		if (pt->procs[i].state == PROC_UNUSED) {
			slot = i;
			break;
		}
	}
	if (slot < 0)
		return -1;

	int pid = pt->next_pid;
	while (pid_in_use(pt, pid))
		pid = (pid >= PID_MAX) ? 1 : pid + 1;
	pt->next_pid = (pid >= PID_MAX) ? 1 : pid + 1;

	pt->procs[slot].pid = pid;
	pt->procs[slot].state = PROC_EMBRYO;
	pt->procs[slot].parent = parent_slot;
	return slot;
}

/* Slot index of the live process with this pid, or -1 if none.
 * UNUSED slots never match. */
int pt_find_pid(const struct proc_table *pt, int pid)
{
	for (int i = 0; i < NPROC; i++) {
		if (pt->procs[i].state != PROC_UNUSED && pt->procs[i].pid == pid)
			return i;
	}
	return -1;
}

/* Number of slots currently in state s. */
int pt_count(const struct proc_table *pt, enum proc_state s)
{
	int n = 0;
	for (int i = 0; i < NPROC; i++) {
		if (pt->procs[i].state == s)
			n++;
	}
	return n;
}
