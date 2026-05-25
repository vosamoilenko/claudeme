PREFIX ?= $(HOME)/.local
BINDIR = $(PREFIX)/bin
MANDIR = $(PREFIX)/share/man/man1

VERSION := $(shell cat VERSION)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/vosamoilenko/claudeme/internal/version.Version=$(VERSION) \
	-X github.com/vosamoilenko/claudeme/internal/version.Commit=$(COMMIT) \
	-X github.com/vosamoilenko/claudeme/internal/version.Date=$(DATE)

.PHONY: build install uninstall clean

build:
	go build -ldflags "$(LDFLAGS)" -o claudeme .

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
