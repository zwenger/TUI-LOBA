BINARY   := loba
MODULE   := github.com/zwenger/TUI-LOBA
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w

.PHONY: all build darwin-arm64 darwin-amd64 linux-amd64 windows-amd64 test vet clean

## Default: build for the current platform.
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## Run all tests.
test:
	go test ./...

## Run go vet.
vet:
	go vet ./...

## Cross-compile all targets.
all: darwin-arm64 darwin-amd64 linux-amd64 windows-amd64

darwin-arm64:
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 .

darwin-amd64:
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 .

linux-amd64:
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .

windows-amd64:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe .

clean:
	rm -f $(BINARY)
	rm -rf dist/
