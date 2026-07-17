# syntax=docker/dockerfile:1
#
# Build context is ./backend. SERVICE picks which services/<name> to compile,
# so all five processes share one image recipe.

ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.22

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

COPY . .
ARG SERVICE
RUN test -d "./services/${SERVICE}" || { echo "unknown SERVICE=${SERVICE}"; exit 1; }

# The module and build caches are mounted, not baked into a layer: all five
# images share them, and an edit to one file recompiles one package instead of
# the whole dependency tree.
#
# There is deliberately no `go mod download` step. It would fetch the entire
# module graph, including goose's — a Go C toolchain that no service imports —
# for every image. `go build` fetches only what it actually needs.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/service "./services/${SERVICE}"

FROM alpine:${ALPINE_VERSION} AS runtime
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 hookary
COPY --from=build /out/service /usr/local/bin/service
USER hookary
ENTRYPOINT ["/usr/local/bin/service"]
