/*
 * DuckOS entry: the 30 lines of assembly the course promised you'd
 * never need until now.
 *
 * The multiboot header lets QEMU (or GRUB) load this ELF directly:
 * `qemu-system-i386 -kernel duckos.elf`. The loader drops us in
 * 32-bit protected mode with a flat 4 GiB segment model -- past the
 * real-mode dance of *The Machine Wakes Up* -- but with NO stack and
 * NO GDT of our own (we borrow the loader's until kmain builds one
 * with gdt_encode). So: point ESP at our stack, call C, and if C
 * ever returns, halt forever.
 */

	.section .multiboot
	.align 4
	.long 0x1BADB002		/* multiboot v1 magic */
	.long 0				/* flags: nothing fancy */
	.long -(0x1BADB002)		/* checksum: magic+flags+this = 0 */

	.section .bss
	.align 16
stack_bottom:
	.skip 16384			/* 16 KiB kernel stack */
stack_top:

	.section .text
	.global _start
_start:
	mov $stack_top, %esp
	cld				/* SysV ABI: DF clear before C */
	call kmain
1:	cli
	hlt
	jmp 1b
