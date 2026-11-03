BINARY=cbl

.PHONY: build build-tray build-all test tidy run install uninstall package-extension package-release

build:
	go build -o bin/$(BINARY) ./cmd/cbl

build-tray:
	go build -o bin/cbl-tray ./cmd/cbl-tray

build-all: build build-tray

test:
	go test -race ./...
	/usr/bin/python3 -m unittest discover -s install/ubuntu -p 'test_*.py'
	node --test desktop/gnome-extension/cbl@codex-limits/ui-data.test.js

tidy:
	go mod tidy

run:
	go run ./cmd/cbl

install:
	./install/ubuntu/install.sh

uninstall:
	./install/ubuntu/uninstall.sh

package-extension:
	./install/ubuntu/package-gnome-extension.sh

package-release:
	./release/package-release.sh
