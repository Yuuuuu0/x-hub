# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ENV CGO_ENABLED=0
RUN LDFLAGS="-s -w -X github.com/Yuuuuu0/x-hub/internal/version.Version=${VERSION}" \
 && GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "$LDFLAGS" -o /out/xhub-master ./cmd/master \
 && for a in amd64 arm64; do \
      GOOS=linux GOARCH=$a go build -trimpath -ldflags "$LDFLAGS" -o /out/agents/xhub-agent-linux-$a ./cmd/agent; \
    done

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/xhub-master /usr/local/bin/xhub-master
COPY --from=build /out/agents /app/agents
VOLUME /data
ENTRYPOINT ["xhub-master"]
CMD ["run", "--data", "/data", "--agents", "/app/agents"]
