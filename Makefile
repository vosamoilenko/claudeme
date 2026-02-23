PREFIX ?= $(HOME)/.local
BINDIR = $(PREFIX)/bin
MANDIR = $(PREFIX)/share/man/man1

.PHONY: build install uninstall clean

build:
	go build -o claudeme .

install: build
	install -d $(BINDIR)
	install -m 755 claudeme $(BINDIR)/claudeme
	install -d $(MANDIR)
	install -m 644 claudeme.1 $(MANDIR)/claudeme.1

uninstall:
	rm -f $(BINDIR)/claudeme
	rm -f $(MANDIR)/claudeme.1

clean:
	rm -f claudeme
