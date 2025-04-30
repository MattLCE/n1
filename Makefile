.DEFAULT_GOAL := vet

.PHONY: build test vet lint clean

build:
	go build -o bin/bosr ./cmd/bosr

test:
	go test -v ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
