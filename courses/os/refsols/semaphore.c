#define NPROC 16

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct proc {
	int pid;
	enum proc_state state;
};

struct semaphore {
	int value;		/* > 0: that many free units; never negative */
	int waiters[NPROC];	/* FIFO of blocked slots (circular buffer) */
	int head;		/* index of the longest-waiting slot */
	int count;		/* number of queued waiters */
};

/* Set the counter (negative treated as 0) and empty the wait queue. */
void sem_init(struct semaphore *s, int value) {
	s->value = value < 0 ? 0 : value;
	s->head = 0;
	s->count = 0;
}

/*
 * procs[slot] tries to acquire one unit.
 * Returns 0 if acquired without blocking (value was > 0),
 *         1 if the caller blocked (queued FIFO, state -> PROC_SLEEPING),
 *        -1 if the wait queue is somehow already full (cannot happen
 *           with NPROC processes, but guard anyway).
 */
int sem_down(struct semaphore *s, struct proc *procs, int slot) {
	if (s->value > 0) {
		s->value--;
		return 0;
	}
	if (s->count == NPROC)
		return -1;
	s->waiters[(s->head + s->count) % NPROC] = slot;
	s->count++;
	procs[slot].state = PROC_SLEEPING;
	return 1;
}

/*
 * Release one unit. If waiters are queued, hand the unit directly to
 * the FIFO head: pop it, mark it PROC_RUNNABLE, return its slot number
 * (the value stays 0 — do NOT increment). With no waiters, increment
 * the value and return -1.
 */
int sem_up(struct semaphore *s, struct proc *procs) {
	if (s->count > 0) {
		int slot = s->waiters[s->head];

		s->head = (s->head + 1) % NPROC;
		s->count--;
		procs[slot].state = PROC_RUNNABLE;
		return slot;
	}
	s->value++;
	return -1;
}

/* Current counter value. */
int sem_value(const struct semaphore *s) {
	return s->value;
}

/* Number of processes blocked on the semaphore. */
int sem_waiting(const struct semaphore *s) {
	return s->count;
}
