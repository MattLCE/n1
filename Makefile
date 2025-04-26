.PHONY: test lint vet

test:
	go test ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...
