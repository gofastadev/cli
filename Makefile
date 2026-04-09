.PHONY: lint test coverage build integration clean ci

## Run golangci-lint
lint:
	golangci-lint run

## Run tests
test:
	go test -race ./...

## Run tests with coverage report
coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

## Build the CLI binary
build:
	go build -o bin/gofasta ./cmd/gofasta/

## Run integration test (build + scaffold + compile)
integration: build
	rm -rf /tmp/gofasta-integration-test
	./bin/gofasta new /tmp/gofasta-integration-test
	cd /tmp/gofasta-integration-test && go build ./...

## Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html

## Run all checks (what CI runs)
ci: lint test build
