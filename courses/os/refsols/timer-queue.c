#include <stdint.h>

#define NTIMERS 16

struct timer {
	int in_use;
	int owner;		/* who to wake; opaque to the queue */
	uint32_t delta;		/* ticks AFTER the previous list entry */
	int next;		/* pool index of next timer, -1 = end */
};

struct timerq {
	struct timer t[NTIMERS];
	int head;		/* pool index of first timer, -1 = empty */
};

/* Empty queue: every pool slot free, no list. */
void tq_init(struct timerq *q)
{
	for (int i = 0; i < NTIMERS; i++)
		q->t[i].in_use = 0;
	q->head = -1;
}

int tq_arm(struct timerq *q, int owner, uint32_t ticks)
{
	int id = -1, cur, prev = -1;

	if (ticks == 0)
		return -1;
	for (int i = 0; i < NTIMERS; i++) {
		if (!q->t[i].in_use) {
			id = i;
			break;
		}
	}
	if (id == -1)
		return -1;

	cur = q->head;
	while (cur != -1 && q->t[cur].delta <= ticks) {
		ticks -= q->t[cur].delta;
		prev = cur;
		cur = q->t[cur].next;
	}
	q->t[id].in_use = 1;
	q->t[id].owner = owner;
	q->t[id].delta = ticks;
	q->t[id].next = cur;
	if (cur != -1)
		q->t[cur].delta -= ticks;
	if (prev == -1)
		q->head = id;
	else
		q->t[prev].next = id;
	return id;
}

int tq_tick(struct timerq *q, int *expired, int max)
{
	int n = 0;

	if (q->head == -1)
		return 0;
	q->t[q->head].delta--;
	while (q->head != -1 && q->t[q->head].delta == 0) {
		int id = q->head;

		if (n < max)
			expired[n] = q->t[id].owner;
		n++;
		q->head = q->t[id].next;
		q->t[id].in_use = 0;
	}
	return n;
}

int tq_cancel(struct timerq *q, int id)
{
	int cur, prev = -1;

	if (id < 0 || id >= NTIMERS || !q->t[id].in_use)
		return -1;
	for (cur = q->head; cur != id; cur = q->t[cur].next)
		prev = cur;
	if (q->t[id].next != -1)
		q->t[q->t[id].next].delta += q->t[id].delta;
	if (prev == -1)
		q->head = q->t[id].next;
	else
		q->t[prev].next = q->t[id].next;
	q->t[id].in_use = 0;
	return 0;
}

/* Ticks until timer id fires: sum of deltas from head through id.
 * 0 if id is invalid or not pending. */
uint32_t tq_remaining(const struct timerq *q, int id)
{
	uint32_t sum = 0;

	if (id < 0 || id >= NTIMERS || !q->t[id].in_use)
		return 0;
	for (int cur = q->head; cur != -1; cur = q->t[cur].next) {
		sum += q->t[cur].delta;
		if (cur == id)
			return sum;
	}
	return 0;
}
