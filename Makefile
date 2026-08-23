BINARY=cbl

.PHONY: build test tidy run

build:
	go build -o bin/$(BINARY) ./cmd/cbl

test:
	go test ./...

tidy:
	go mod tidy

run:
	go run ./cmd/cbl
