.PHONY: build test lint install deps

deps:
	go mod tidy && go mod download

build:
	CGO_ENABLED=0 go build -o bin/saturn .

test:
	go test ./...

lint:
	golangci-lint run

install: build
	cp bin/saturn /usr/local/bin/saturn
