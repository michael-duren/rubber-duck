#define NPROC 16

struct runqueue {
	int items[NPROC];	/* proc slots, FIFO */
	int head;		/* index of oldest entry */
	int count;
};

/* Make the queue empty. */
void rq_init(struct runqueue *q) {
	q->head = 0;
	q->count = 0;
}

/* Append slot at the tail: index (head + count) % NPROC.
 * Returns 0, or -1 if the queue is already full. */
int rq_push(struct runqueue *q, int slot) {
	if (q->count == NPROC)
		return -1;
	q->items[(q->head + q->count) % NPROC] = slot;
	q->count++;
	return 0;
}

/* Remove and return the oldest entry (at head), advancing head with
 * wraparound. Returns -1 if the queue is empty. */
int rq_pop(struct runqueue *q) {
	int slot;

	if (q->count == 0)
		return -1;
	slot = q->items[q->head];
	q->head = (q->head + 1) % NPROC;
	q->count--;
	return slot;
}

/* 1 if slot is somewhere in the queue, else 0. Walk the live region:
 * count entries starting at head, indexes taken modulo NPROC. */
int rq_contains(const struct runqueue *q, int slot) {
	for (int i = 0; i < q->count; i++)
		if (q->items[(q->head + i) % NPROC] == slot)
			return 1;
	return 0;
}
