# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=docker
ARG COMMIT=none
ARG DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/slime ./cmd/slime

FROM alpine:3.19
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 slime \
    && mkdir -p /data /games \
    && chown slime:slime /data /games
COPY --from=build /out/slime /usr/local/bin/slime
USER slime
ENV SLIME_DATA_DIR=/data \
    SLIME_MEMORY_DB=/data/memory.db
WORKDIR /games
VOLUME ["/data", "/games"]
ENTRYPOINT ["slime"]
CMD ["--help"]
