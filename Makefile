VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/Yuuuuu0/x-hub/internal/version.Version=$(VERSION)
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: all web master agents test docker clean

all: web agents master

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

master:
	$(GOBUILD) -o dist/xhub-master ./cmd/master

agents:
	for a in amd64 arm64; do GOOS=linux GOARCH=$$a $(GOBUILD) -o dist/agents/xhub-agent-linux-$$a ./cmd/agent; done

test:
	go vet ./...
	go test ./...
	cd web && pnpm typecheck

docker:
	docker build --build-arg VERSION=$(VERSION) -t xhub-master:$(VERSION) .

clean:
	rm -rf dist
