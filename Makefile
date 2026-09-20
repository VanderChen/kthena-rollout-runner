.PHONY: test build image build-fault-proxy fault-proxy-image
GOARCH ?= arm64
IMAGE ?= kthena-rollout-runner:dev
FAULT_PROXY_IMAGE ?= kthena-fault-proxy:dev

test:
	go test ./...
	go test -race ./...
	go vet ./...

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o bin/rollout-runner ./cmd/rollout-runner

image: build
	docker build --platform linux/$(GOARCH) -t $(IMAGE) .

build-fault-proxy:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o bin/fault-proxy ./cmd/fault-proxy

fault-proxy-image: build-fault-proxy
	docker build --platform linux/$(GOARCH) -f Dockerfile.fault-proxy -t $(FAULT_PROXY_IMAGE) .
