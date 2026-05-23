.PHONY: build run fmt tidy

build:
	go build -o bin/server ./cmd/server

run: build
	./bin/server

fmt:
	gofmt -w .

tidy:
	go mod tidy
