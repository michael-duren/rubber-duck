#include <stdint.h>

/*
 * One 8259A, modeled in software. In the real chip these three
 * registers ARE the hardware state the INTA handshake reads and
 * writes; here they are bytes the tests can inspect. Bit n of each
 * register corresponds to IRQ n; IRQ 0 is the highest priority.
 */
struct pic {
	uint8_t irr, isr, imr;	/* pending, in-service, masked */
};

/* A device raised line irq (0..7): mark it pending. Idempotent. */
void pic_raise(struct pic *p, int irq)
{
	p->irr |= (uint8_t)(1u << irq);
}

/*
 * The INTA handshake: deliver the best pending request, if any.
 * Eligible = pending in IRR, not masked in IMR, and strictly
 * higher priority (lower number) than every in-service bit in ISR.
 * Deliver the highest-priority eligible IRQ: clear IRR bit, set ISR
 * bit, return its number. If none is eligible, return -1 and leave
 * all registers untouched.
 */
int pic_next(struct pic *p)
{
	int limit = 8;

	for (int i = 0; i < 8; i++) {
		if (p->isr & (1u << i)) {
			limit = i;	/* nothing at or below this wins */
			break;
		}
	}
	for (int i = 0; i < limit; i++) {
		if ((p->irr & (1u << i)) && !(p->imr & (1u << i))) {
			p->irr &= (uint8_t)~(1u << i);
			p->isr |= (uint8_t)(1u << i);
			return i;
		}
	}
	return -1;
}

/* Non-specific EOI: clear the lowest-numbered set bit in ISR. */
void pic_eoi(struct pic *p)
{
	for (int i = 0; i < 8; i++) {
		if (p->isr & (1u << i)) {
			p->isr &= (uint8_t)~(1u << i);
			return;
		}
	}
}

/* OCW1: replace the interrupt mask. Set bit = line ignored. */
void pic_set_mask(struct pic *p, uint8_t mask)
{
	p->imr = mask;
}
