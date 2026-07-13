/* DuckOS: link at 1 MiB, multiboot header first so GRUB/QEMU find it. */
ENTRY(_start)
SECTIONS
{
	. = 1M;

	.text : {
		*(.multiboot)
		*(.text*)
	}
	.rodata : {
		*(.rodata*)
	}
	.data : {
		*(.data*)
	}
	.bss : {
		*(COMMON)
		*(.bss*)
	}
}
