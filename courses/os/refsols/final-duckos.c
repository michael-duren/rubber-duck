#include <stdint.h>

#define NPROC 16
#define NQ 4
#define ANY (-1)
#define QUANTUM(q) (1 << (q))	/* queue 0: 1 tick ... queue 3: 8 */
#define INIT_PID 1
#define ESRCH 3
#define EDEADLK 35
#define ECHILD 10

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct message {
	int m_source;
	int m_type;
	int m_i1;
	int m_i2;
};

enum ev_type { EV_SPAWN, EV_RUN, EV_PREEMPT, EV_BLOCK, EV_WAKE,
               EV_EXIT, EV_REAP };

#define BLK_SEND  0
#define BLK_RECV  1
#define BLK_SLEEP 2
#define BLK_WAIT  3

struct kevent {
	enum ev_type type;
	int slot;
	int arg;
};

#define NTRACE 128

struct proc {
	int pid;
	enum proc_state state;
	int parent;
	int prio;
	int send_to;
	int recv_from;
	struct message buf;
	struct message *user_out;
	uint32_t wake_at;
	int waiting;
	int exit_status;
};

struct kernel {
	struct proc procs[NPROC];
	int next_pid;
	int rq[NQ][NPROC];
	int rq_head[NQ];
	int rq_count[NQ];
	int current;
	int quantum_left;
	uint32_t ticks;
	struct kevent trace[NTRACE];
	int ntrace;
};

/* ---- Provided plumbing. ---- */

void trace_emit(struct kernel *k, enum ev_type type, int slot, int arg)
{
	if (k->ntrace < NTRACE) {
		k->trace[k->ntrace].type = type;
		k->trace[k->ntrace].slot = slot;
		k->trace[k->ntrace].arg = arg;
		k->ntrace++;
	}
}

void rq_push(struct kernel *k, int level, int slot)
{
	k->rq[level][(k->rq_head[level] + k->rq_count[level]) % NPROC]
		= slot;
	k->rq_count[level]++;
}

int rq_pop(struct kernel *k, int level)
{
	int slot = k->rq[level][k->rq_head[level]];

	k->rq_head[level] = (k->rq_head[level] + 1) % NPROC;
	k->rq_count[level]--;
	return slot;
}

int rq_empty(const struct kernel *k, int level)
{
	return k->rq_count[level] == 0;
}

void k_init(struct kernel *k)
{
	for (int i = 0; i < NPROC; i++) {
		k->procs[i].pid = 0;
		k->procs[i].state = PROC_UNUSED;
		k->procs[i].parent = -1;
		k->procs[i].prio = 0;
		k->procs[i].send_to = -1;
		k->procs[i].recv_from = -1;
		k->procs[i].user_out = 0;
		k->procs[i].wake_at = 0;
		k->procs[i].waiting = 0;
		k->procs[i].exit_status = 0;
	}
	k->next_pid = 1;
	for (int q = 0; q < NQ; q++) {
		k->rq_head[q] = 0;
		k->rq_count[q] = 0;
	}
	k->current = -1;
	k->quantum_left = 0;
	k->ticks = 0;
	k->ntrace = 0;
}

/* ---- The eight entry points. ---- */

/* Make slot runnable again and put it in line at its own level. */
static void wake_into_queue(struct kernel *k, int slot, int waker)
{
	k->procs[slot].state = PROC_RUNNABLE;
	k->procs[slot].waiting = 0;
	rq_push(k, k->procs[slot].prio, slot);
	trace_emit(k, EV_WAKE, slot, waker);
}

int k_spawn(struct kernel *k, int parent_slot, int prio)
{
	int slot = -1;

	for (int i = 0; i < NPROC; i++) {
		if (k->procs[i].state == PROC_UNUSED) {
			slot = i;
			break;
		}
	}
	if (slot == -1)
		return -1;
	if (prio < 0)
		prio = 0;
	if (prio > NQ - 1)
		prio = NQ - 1;
	k->procs[slot].pid = k->next_pid++;
	k->procs[slot].state = PROC_RUNNABLE;
	k->procs[slot].parent = parent_slot;
	k->procs[slot].prio = prio;
	k->procs[slot].send_to = -1;
	k->procs[slot].recv_from = -1;
	k->procs[slot].user_out = 0;
	k->procs[slot].wake_at = 0;
	k->procs[slot].waiting = 0;
	k->procs[slot].exit_status = 0;
	rq_push(k, prio, slot);
	trace_emit(k, EV_SPAWN, slot, k->procs[slot].pid);
	return slot;
}

int k_schedule(struct kernel *k)
{
	if (k->current != -1)
		return k->current;
	for (int q = 0; q < NQ; q++) {
		if (rq_empty(k, q))
			continue;
		k->current = rq_pop(k, q);
		k->procs[k->current].state = PROC_RUNNING;
		k->quantum_left = QUANTUM(q);
		trace_emit(k, EV_RUN, k->current, q);
		return k->current;
	}
	return -1;
}

void k_tick(struct kernel *k)
{
	k->ticks++;
	for (int i = 0; i < NPROC; i++) {
		struct proc *p = &k->procs[i];

		if (p->state == PROC_SLEEPING && !p->waiting &&
		    p->wake_at <= k->ticks) {
			p->state = PROC_RUNNABLE;
			rq_push(k, p->prio, i);
			trace_emit(k, EV_WAKE, i, 0);
		}
	}
	if (k->current == -1)
		return;
	k->quantum_left--;
	if (k->quantum_left > 0)
		return;
	{
		int slot = k->current;
		int newprio = k->procs[slot].prio + 1;

		if (newprio > NQ - 1)
			newprio = NQ - 1;
		k->procs[slot].prio = newprio;
		k->procs[slot].state = PROC_RUNNABLE;
		rq_push(k, newprio, slot);
		trace_emit(k, EV_PREEMPT, slot, newprio);
		k->current = -1;
	}
}

int k_send(struct kernel *k, int dst, const struct message *m)
{
	int cur = k->current;
	struct proc *d;

	if (cur == -1)
		return -ESRCH;
	if (dst < 0 || dst >= NPROC ||
	    k->procs[dst].state == PROC_UNUSED)
		return -ESRCH;
	if (dst == cur)
		return -EDEADLK;
	d = &k->procs[dst];
	if (d->state == PROC_RECEIVING &&
	    (d->recv_from == ANY || d->recv_from == cur)) {
		struct message *land = d->user_out ? d->user_out : &d->buf;

		*land = *m;
		land->m_source = cur;
		d->recv_from = -1;
		d->user_out = 0;
		wake_into_queue(k, dst, cur);
		return 0;	/* the sender keeps running */
	}
	for (int hop = dst; k->procs[hop].state == PROC_SENDING;
	     hop = k->procs[hop].send_to)
		if (k->procs[hop].send_to == cur)
			return -EDEADLK;
	k->procs[cur].state = PROC_SENDING;
	k->procs[cur].send_to = dst;
	k->procs[cur].buf = *m;
	trace_emit(k, EV_BLOCK, cur, BLK_SEND);
	k->current = -1;
	return 0;
}

int k_receive(struct kernel *k, int from, struct message *out)
{
	int cur = k->current;

	if (cur == -1)
		return -ESRCH;
	if (from != ANY &&
	    (from < 0 || from >= NPROC ||
	     k->procs[from].state == PROC_UNUSED))
		return -ESRCH;
	for (int s = 0; s < NPROC; s++) {
		struct proc *p = &k->procs[s];

		if (p->state != PROC_SENDING || p->send_to != cur)
			continue;
		if (from != ANY && s != from)
			continue;
		*out = p->buf;
		out->m_source = s;
		p->send_to = -1;
		wake_into_queue(k, s, cur);
		return 0;	/* the receiver keeps running */
	}
	k->procs[cur].state = PROC_RECEIVING;
	k->procs[cur].recv_from = from;
	k->procs[cur].user_out = out;
	trace_emit(k, EV_BLOCK, cur, BLK_RECV);
	k->current = -1;
	return 0;
}

int k_sleep(struct kernel *k, uint32_t nticks)
{
	int cur = k->current;

	if (cur == -1)
		return -ESRCH;
	if (nticks == 0)
		return 0;
	k->procs[cur].state = PROC_SLEEPING;
	k->procs[cur].wake_at = k->ticks + nticks;
	k->procs[cur].waiting = 0;
	trace_emit(k, EV_BLOCK, cur, BLK_SLEEP);
	k->current = -1;
	return 0;
}

int k_exit(struct kernel *k, int status)
{
	int cur = k->current;
	int init_slot = -1, inherited_zombie = 0, parent;

	if (cur == -1)
		return -ESRCH;

	/* Mark the corpse before anyone is woken. */
	k->procs[cur].state = PROC_ZOMBIE;
	k->procs[cur].exit_status = (status & 0xff) << 8;
	k->procs[cur].waiting = 0;
	trace_emit(k, EV_EXIT, cur, k->procs[cur].exit_status);

	for (int i = 0; i < NPROC; i++)
		if (k->procs[i].state != PROC_UNUSED &&
		    k->procs[i].pid == INIT_PID) {
			init_slot = i;
			break;
		}
	for (int i = 0; i < NPROC; i++) {
		if (k->procs[i].state == PROC_UNUSED ||
		    k->procs[i].parent != cur)
			continue;
		k->procs[i].parent = init_slot;	/* -1 if no init */
		if (k->procs[i].state == PROC_ZOMBIE)
			inherited_zombie = 1;
	}

	parent = k->procs[cur].parent;
	if (parent >= 0 && k->procs[parent].waiting)
		wake_into_queue(k, parent, cur);
	if (inherited_zombie && init_slot >= 0 &&
	    k->procs[init_slot].waiting)
		wake_into_queue(k, init_slot, cur);

	k->current = -1;
	return 0;
}

int k_wait(struct kernel *k, int *status)
{
	int cur = k->current;
	int zombie = -1, have_child = 0;

	if (cur == -1)
		return -ESRCH;
	for (int i = 0; i < NPROC; i++) {
		if (k->procs[i].state == PROC_UNUSED ||
		    k->procs[i].parent != cur)
			continue;
		have_child = 1;
		if (k->procs[i].state == PROC_ZOMBIE && zombie == -1)
			zombie = i;
	}
	if (zombie != -1) {
		int pid = k->procs[zombie].pid;

		*status = k->procs[zombie].exit_status;
		k->procs[zombie].state = PROC_UNUSED;
		k->procs[zombie].pid = 0;
		trace_emit(k, EV_REAP, cur, pid);
		return pid;	/* the caller keeps running */
	}
	if (have_child) {
		k->procs[cur].waiting = 1;
		k->procs[cur].state = PROC_SLEEPING;
		trace_emit(k, EV_BLOCK, cur, BLK_WAIT);
		k->current = -1;
		return 0;
	}
	return -ECHILD;
}
