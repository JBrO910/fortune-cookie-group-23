FRONTEND := frontend
BACKEND  := backend

.PHONY: all build build-frontend build-backend \
        test test-frontend test-backend \
        lint lint-frontend lint-backend \
        scan

all: build test lint scan

build: build-frontend build-backend

build-frontend:
	cd $(FRONTEND) && go build ./...

build-backend:
	cd $(BACKEND) && go build ./...

test: test-frontend test-backend

test-frontend:
	cd $(FRONTEND) && go test ./...

test-backend:
	cd $(BACKEND) && go test ./...

lint: lint-frontend lint-backend

lint-frontend:
	cd $(FRONTEND) && golangci-lint run --timeout=5m ./...

lint-backend:
	cd $(BACKEND) && golangci-lint run --timeout=5m ./...

scan:
	trivy fs --scanners vuln,secret,config --severity CRITICAL,HIGH --ignore-unfixed --exit-code 1 .
