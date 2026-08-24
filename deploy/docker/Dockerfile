# syntax=docker/dockerfile:1
#
# Multi-stage build:
#   builder  - compile static binary with CGO disabled
#   runtime  - distroless, ~10-20MB final image
#
# Build with:   docker build -f deploy/docker/Dockerfile -t typist .
# (built from repo root so the build context contains go.mod and migrations)

FROM golang:1.27-alpine AS builder
WORKDIR /src

# The base image's Go is the only toolchain the build may use. The official
# golang images already default to this; setting it explicitly keeps the
# guarantee if the base image is ever swapped, and matches flake.nix and CI.
ENV GOTOOLCHAIN=local

# Cache dependencies separately from source for faster rebuilds.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/server \
    ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
