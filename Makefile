.PHONY: test build image
GOARCH ?= arm64
IMAGE ?= kthena-rollout-runner:dev-021-r3

test:
	go test ./...
	go test -race ./...
	go vet ./...

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o bin/rollout-runner ./cmd/rollout-runner

image: build
	docker build -t $(IMAGE) .
