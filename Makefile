BINARY     := bin/hellocalc
PKG        := github.com/Softbank-Hackathon-2026-Team-Daisy/sample-monolith/internal/buildinfo
IMAGE      ?= hellocalc
VERSION    ?= $(shell sed -n 's/^[[:space:]]*Version[[:space:]]*= "\(.*\)"/\1/p' internal/buildinfo/buildinfo.go)
COMMIT     ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BASE_URL   ?= http://localhost:8080

LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).BuildTime=$(BUILD_TIME)

.PHONY: run build test fmt fmt-check vet check docker-build docker-run smoke clean

## run: run the server locally (HOST/PORT/LOG_LEVEL from the environment)
run:
	go run ./cmd/hellocalc

## build: build a static binary into bin/
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/hellocalc

## test: run all tests
test:
	go test -count=1 ./...

## fmt: format all Go code
fmt:
	gofmt -w .

## fmt-check: fail if any Go file is not gofmt-clean
fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## vet: run go vet
vet:
	go vet ./...

## check: local quality gate (format, vet, tests, build)
check: fmt-check vet test build

## docker-build: build the container image tagged $(IMAGE):$(VERSION) and :latest
docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

## docker-run: run the container image on port 8080
docker-run:
	docker run --rm -p 8080:8080 $(IMAGE):$(VERSION)

## smoke: run the smoke test against $(BASE_URL)
smoke:
	BASE_URL=$(BASE_URL) ./scripts/smoke-test.sh

## clean: remove build output
clean:
	rm -rf bin
