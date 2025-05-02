.DEFAULT_GOAL := vet

.PHONY: build test vet lint clean test-net test-net-clean test-net-build

build:
	go build -o bin/bosr ./cmd/bosr
	go build -o bin/mirord ./cmd/mirord

test:
	go test -v ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

# Network testing targets
test-net-build:
	mkdir -p test/sync/data/vault1 test/sync/data/vault2
	docker-compose -f test/sync/docker-compose.yml build

test-net-clean:
	docker-compose -f test/sync/docker-compose.yml down -v
	rm -rf test/sync/data

test-net: test-net-build
	docker-compose -f test/sync/docker-compose.yml up --abort-on-container-exit test-runner
	@echo "Network tests completed"

# Run a specific network test
test-net-%: test-net-build
	docker-compose -f test/sync/docker-compose.yml up -d toxiproxy vault1 vault2
	docker-compose -f test/sync/docker-compose.yml run test-runner /app/bin/sync.test -test.v -test.run $*
	docker-compose -f test/sync/docker-compose.yml down
