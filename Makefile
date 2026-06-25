.PHONY: all build test lint vet fmt tidy check clean

all: check

build:
	go build ./...
	go build ./examples/...

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

check: fmt tidy vet build test

clean:
	go clean -cache -testcache
