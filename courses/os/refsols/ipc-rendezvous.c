#include <stddef.h>

/*
 * DuckOS rendezvous IPC.  In a real kernel these functions run in the
 * trap handler with interrupts off and the proc table lives in ring 0;
 * here the "kernel" is a struct the tests can read.
 */

#define NPROC 16
#define ANY (-1)
#define ESRCH 3
#define EDEADLK 35
#define EINVAL 22

enum proc_state { PROC_UNUSED, PROC_EMBRYO, PROC_RUNNABLE, PROC_RUNNING,
                  PROC_SENDING, PROC_RECEIVING, PROC_SLEEPING, PROC_ZOMBIE };

struct message {
	int m_source;	/* stamped by the kernel on delivery */
	int m_type;
	int m_i1;
	int m_i2;
};

struct proc {
	int pid;
	enum proc_state state;
	int send_to;		/* slot we are blocked sending to, -1 */
	int recv_from;		/* slot (or ANY) we are blocked receiving from, -1 = not receiving */
	struct message buf;	/* out: message being sent; in: landing pad */
	struct message *user_out; /* where a blocked receiver wants delivery (points into test memory) */
};

struct kernel {
	struct proc procs[NPROC];
};

/* Reset every slot: PROC_UNUSED, links cleared.  (Provided.) */
void k_init(struct kernel *k) {
	for (int i = 0; i < NPROC; i++) {
		k->procs[i].pid = 0;
		k->procs[i].state = PROC_UNUSED;
		k->procs[i].send_to = -1;
		k->procs[i].recv_from = -1;
		k->procs[i].user_out = NULL;
	}
}

/* Bring a slot to life as PROC_RUNNABLE.  (Provided; tests use it.) */
void k_mkproc(struct kernel *k, int slot, int pid) {
	k->procs[slot].pid = pid;
	k->procs[slot].state = PROC_RUNNABLE;
	k->procs[slot].send_to = -1;
	k->procs[slot].recv_from = -1;
	k->procs[slot].user_out = NULL;
}

/*
 * src sends *m to dst.
 *
 * Returns -ESRCH if dst is out of range or PROC_UNUSED, -EDEADLK if
 * src == dst or if blocking src would close a cycle of senders.
 * If dst is PROC_RECEIVING from ANY or from src: deliver *m into dst's
 * user_out (or its buf if user_out is NULL), stamping m_source = src,
 * wake dst (PROC_RUNNABLE, recv_from = -1), return 0 without blocking.
 * Otherwise walk send_to links from dst while they point at PROC_SENDING
 * processes; if the walk reaches src, refuse with -EDEADLK.  Else block
 * src: PROC_SENDING, send_to = dst, *m parked in src's buf; return 0.
 */
int k_send(struct kernel *k, int src, int dst, const struct message *m) {
	struct proc *d;

	if (dst < 0 || dst >= NPROC || k->procs[dst].state == PROC_UNUSED)
		return -ESRCH;
	if (src == dst)
		return -EDEADLK;
	d = &k->procs[dst];
	if (d->state == PROC_RECEIVING &&
	    (d->recv_from == ANY || d->recv_from == src)) {
		struct message *land = d->user_out ? d->user_out : &d->buf;

		*land = *m;
		land->m_source = src;
		d->state = PROC_RUNNABLE;
		d->recv_from = -1;
		d->user_out = NULL;
		return 0;
	}
	for (int hop = dst; k->procs[hop].state == PROC_SENDING;
	     hop = k->procs[hop].send_to)
		if (k->procs[hop].send_to == src)
			return -EDEADLK;
	k->procs[src].state = PROC_SENDING;
	k->procs[src].send_to = dst;
	k->procs[src].buf = *m;
	return 0;
}

/*
 * rcv receives into *out from `from` (a slot, or ANY).
 *
 * Returns -ESRCH if from is neither ANY nor a live slot.  Scans slots
 * 0..NPROC-1 in order for a PROC_SENDING process with send_to == rcv
 * (and slot == from unless from is ANY): copies its buf into *out with
 * m_source = sender's slot, wakes the sender (PROC_RUNNABLE,
 * send_to = -1), returns 0.  If no sender matches, blocks rcv:
 * PROC_RECEIVING, recv_from = from, user_out = out; returns 0.
 */
int k_receive(struct kernel *k, int rcv, int from, struct message *out) {
	if (from != ANY &&
	    (from < 0 || from >= NPROC ||
	     k->procs[from].state == PROC_UNUSED))
		return -ESRCH;
	for (int s = 0; s < NPROC; s++) {
		struct proc *p = &k->procs[s];

		if (p->state != PROC_SENDING || p->send_to != rcv)
			continue;
		if (from != ANY && s != from)
			continue;
		*out = p->buf;
		out->m_source = s;
		p->state = PROC_RUNNABLE;
		p->send_to = -1;
		return 0;
	}
	k->procs[rcv].state = PROC_RECEIVING;
	k->procs[rcv].recv_from = from;
	k->procs[rcv].user_out = out;
	return 0;
}
