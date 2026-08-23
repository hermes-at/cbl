BINARY=cbl

.PHONY: build test tidy run install uninstall package-extension

build:
	go build -o bin/$(BINARY) ./cmd/cbl

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
