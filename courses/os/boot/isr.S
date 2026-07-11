/*
 * Interrupt stubs: the glue *Interrupts and the IDT* described.
 * The CPU pushed EFLAGS/CS/EIP; we save the caller-saved world,
 * call the C handler, restore, and iret. cld because the interrupt
 * may have landed mid-`rep movs` with DF set.
 */

	.section .text

	.global irq0_stub
irq0_stub:
	pusha
	cld
	call timer_isr
	popa
	iret

	.global irq1_stub
irq1_stub:
	pusha
	cld
	call kbd_isr
	popa
	iret
