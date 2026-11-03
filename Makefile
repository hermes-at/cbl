BINARY=cbl

.PHONY: build build-tray build-all test tidy run install uninstall package-release

build:
	go build -o bin/$(BINARY) ./cmd/cbl

build-tray:
	go build -o bin/cbl-tray ./cmd/cbl-tray

build-all: build build-tray

test:
	go test -race ./...
	/usr/bin/python3 -m unittest discover -s desktop/app -p 'test_*.py'
	/usr/bin/python3 -m unittest discover -s install/ubuntu -p 'test_*.py'

tidy:
	go mod tidy

run:
	go run ./cmd/cbl

install:
	./install/ubuntu/install.sh

uninstall:
	./install/ubuntu/uninstall.sh

package-release:
	./release/package-release.sh
