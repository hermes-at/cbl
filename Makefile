BINARY=cbl

.PHONY: build build-tray build-all test tidy run install uninstall package-extension package-release

build:
	go build -o bin/$(BINARY) ./cmd/cbl

build-tray:
	go build -o bin/cbl-tray ./cmd/cbl-tray

build-all: build build-tray

test:
	go test ./...

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
