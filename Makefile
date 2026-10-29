VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# 被控端版本：最后一次改动被控端相关代码的提交，只改主控或界面时不变。
AGENT_PATHS := cmd/agent internal/agent internal/shared internal/version go.mod go.sum
AGENT_VERSION ?= $(shell git log -1 --format=%h --abbrev=7 -- $(AGENT_PATHS) 2>/dev/null || echo dev)
PKG := github.com/Yuuuuu0/x-hub/internal/version
GOBUILD := CGO_ENABLED=0 go build -trimpath

.PHONY: all web master agents test docker clean print-agent-version

all: web agents master

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

master:
	$(GOBUILD) -ldflags "-s -w -X $(PKG).Version=$(VERSION) -X $(PKG).AgentVersion=$(AGENT_VERSION)" -o dist/xhub-master ./cmd/master

agents:
	for a in amd64 arm64; do GOOS=linux GOARCH=$$a $(GOBUILD) -ldflags "-s -w -X $(PKG).Version=$(AGENT_VERSION)" -o dist/agents/xhub-agent-linux-$$a ./cmd/agent; done

test:
	go vet ./...
	go test ./...
	cd web && pnpm typecheck

docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg AGENT_VERSION=$(AGENT_VERSION) -t xhub-master:$(VERSION) .

clean:
	rm -rf dist

print-agent-version:
	@echo $(AGENT_VERSION)
