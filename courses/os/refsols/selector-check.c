#include <stdint.h>

/*
 * A segment selector, as loaded into CS/DS/SS/ES:
 *
 *   bit 15                          3    2    1  0
 *      +----------------------------+----+-------+
 *      |          index             | TI |  RPL  |
 *      +----------------------------+----+-------+
 *
 *   raw = index << 3 | ti << 2 | rpl
 */
struct selector {
	uint16_t raw;
};

/* Descriptor table index: bits 3:15. */
int sel_index(uint16_t raw)
{
	return raw >> 3;
}

/* Requested privilege level: bits 0:1. */
int sel_rpl(uint16_t raw)
{
	return raw & 0x3;
}

/* Table indicator (bit 2): 1 = LDT, 0 = GDT. */
int sel_is_ldt(uint16_t raw)
{
	return (raw >> 2) & 0x1;
}

/*
 * The CPU's data-segment load check: code running at ring `cpl`,
 * presenting `selector_raw`, wants a segment whose descriptor has
 * privilege `descriptor_dpl`.
 *
 * Returns 1 iff max(cpl, rpl) <= descriptor_dpl, else 0.
 */
int can_load_data_segment(int cpl, uint16_t selector_raw,
                          int descriptor_dpl)
{
	int rpl = sel_rpl(selector_raw);
	int effective = cpl > rpl ? cpl : rpl;

	return effective <= descriptor_dpl;
}
