obj-m := jz08c_bl.o

KDIR ?= /usr/src/linux-6.6

all:
	$(MAKE) -C $(KDIR) M=$(CURDIR) modules

clean:
	$(MAKE) -C $(KDIR) M=$(CURDIR) clean
