# DuckOS bare-metal build. Needs: gcc with -m32 support, qemu-system-i386.

CC      = gcc
CFLAGS  = -m32 -std=c17 -ffreestanding -fno-pie -fno-stack-protector \
          -fno-builtin -fno-asynchronous-unwind-tables -Wall -Wextra -O1
ASFLAGS = -m32
LDFLAGS = -m32 -nostdlib -static -Wl,--build-id=none -T linker.ld

OBJS = boot.o isr.o klib.o console.o printf.o serial.o gdt.o idt.o \
       pic.o pit.o kbd.o kernel.o

duckos.elf: $(OBJS) linker.ld
	$(CC) $(LDFLAGS) -o $@ $(OBJS)

%.o: %.c duckos.h
	$(CC) $(CFLAGS) -c -o $@ $<

%.o: %.S
	$(CC) $(ASFLAGS) -c -o $@ $<

run: duckos.elf
	qemu-system-i386 -kernel duckos.elf

# Headless: the console mirrors to COM1, so the boot shows up here.
run-serial: duckos.elf
	qemu-system-i386 -kernel duckos.elf -display none -serial stdio

clean:
	rm -f $(OBJS) duckos.elf

.PHONY: run run-serial clean
