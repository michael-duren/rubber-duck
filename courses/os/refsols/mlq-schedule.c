#define NPROC 16
#define NQ 4
#define QUANTUM(q) (1 << (q))	/* queue 0: 1 tick ... queue 3: 8 ticks */

struct sched {
	int queue[NQ][NPROC];	/* circular FIFO per level */
	int head[NQ];
	int count[NQ];
	int prio[NPROC];	/* current queue of each slot; -1 = not present */
	int current;		/* running slot, -1 = none */
	int quantum_left;	/* ticks left for current */
};

static void q_push(struct sched *s, int level, int slot)
{
	s->queue[level][(s->head[level] + s->count[level]) % NPROC] = slot;
	s->count[level]++;
}

static int q_pop(struct sched *s, int level)
{
	int slot = s->queue[level][s->head[level]];

	s->head[level] = (s->head[level] + 1) % NPROC;
	s->count[level]--;
	return slot;
}

static int q_contains(const struct sched *s, int slot)
{
	for (int level = 0; level < NQ; level++)
		for (int i = 0; i < s->count[level]; i++)
			if (s->queue[level][(s->head[level] + i) % NPROC]
			    == slot)
				return 1;
	return 0;
}

/* Empty scheduler: no queued slots, nothing running, all prio[] -1. */
void sched_init(struct sched *s) {
	for (int level = 0; level < NQ; level++) {
		s->head[level] = 0;
		s->count[level] = 0;
	}
	for (int slot = 0; slot < NPROC; slot++)
		s->prio[slot] = -1;
	s->current = -1;
	s->quantum_left = 0;
}

/* Append slot to queue prio; record prio[slot].
 * -1 if prio not in 0..NQ-1, slot not in 0..NPROC-1, queue full, or
 * slot already present (queued anywhere, or running). 0 on success. */
int sched_enqueue(struct sched *s, int slot, int prio) {
	if (prio < 0 || prio >= NQ || slot < 0 || slot >= NPROC)
		return -1;
	if (s->count[prio] == NPROC)
		return -1;
	if (slot == s->current || q_contains(s, slot))
		return -1;
	q_push(s, prio, slot);
	s->prio[slot] = prio;
	return 0;
}

/* Pop the lowest-numbered non-empty queue; set current and
 * quantum_left = QUANTUM(level); prio[slot] keeps its level.
 * Returns the slot, or -1 if all queues are empty (current stays -1).
 * Only called when current == -1. */
int sched_pick(struct sched *s) {
	for (int level = 0; level < NQ; level++) {
		if (s->count[level] == 0)
			continue;
		s->current = q_pop(s, level);
		s->quantum_left = QUANTUM(level);
		return s->current;
	}
	return -1;
}

/* One timer tick. No current: return 0. Otherwise decrement
 * quantum_left; at 0, demote current one level (cap NQ-1),
 * re-enqueue it there, clear current, return 1. Else return 0. */
int sched_tick(struct sched *s) {
	int level;

	if (s->current == -1)
		return 0;
	s->quantum_left--;
	if (s->quantum_left > 0)
		return 0;
	level = s->prio[s->current] + 1;
	if (level > NQ - 1)
		level = NQ - 1;
	q_push(s, level, s->current);
	s->prio[s->current] = level;
	s->current = -1;
	return 1;
}

/* Current blocked voluntarily: current = -1, prio[slot] unchanged,
 * slot NOT re-enqueued (its waker re-enqueues it). No demotion —
 * blocking early is what interactive processes do. */
void sched_block(struct sched *s) {
	s->current = -1;
	s->quantum_left = 0;
}

/* Starvation fix: drain queues 1..NQ-1 in order into the tail of
 * queue 0, preserving pop order, setting each prio[] to 0. */
void sched_boost(struct sched *s) {
	for (int level = 1; level < NQ; level++) {
		while (s->count[level] > 0) {
			int slot = q_pop(s, level);

			q_push(s, 0, slot);
			s->prio[slot] = 0;
		}
	}
}
