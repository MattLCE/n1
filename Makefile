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
	# Changed docker-compose to docker compose
	docker compose -f test/sync/docker-compose.yml build

test-net-clean:
	# Changed docker-compose to docker compose
	docker compose -f test/sync/docker-compose.yml down -v
	rm -rf test/sync/data

test-net: test-net-build
	# Changed docker-compose to docker compose
	docker compose -f test/sync/docker-compose.yml up --abort-on-container-exit test-runner
	@echo "Network tests completed"

# Run a specific network test
test-net-%: test-net-build
	# Start services in detached mode
	@echo "Starting background services (toxiproxy, vault1, vault2)..."
	docker compose -f test/sync/docker-compose.yml up -d --build toxiproxy vault1 vault2
	# Wait for services to initialize (adjust sleep time if needed)
	@echo "Waiting 5 seconds for services to initialize..."
	@sleep 5
	# Run the test runner in the foreground
	@echo "Running test: $*"
	docker compose -f test/sync/docker-compose.yml run --rm test-runner /app/bin/sync.test -test.v -test.run $*
	# Cleanup services and volumes
	@echo "Cleaning up services and volumes..."
	docker compose -f test/sync/docker-compose.yml down -v