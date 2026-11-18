# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/Deep-Octopus/Memorigo/internal/version.Version=${VERSION} \
      -X github.com/Deep-Octopus/Memorigo/internal/version.Commit=${COMMIT} \
      -X github.com/Deep-Octopus/Memorigo/internal/version.Date=${BUILD_DATE}" \
    -o /out/memorigo-mcp ./cmd/memorigo-mcp

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S memorigo \
    && adduser -S -G memorigo memorigo \
    && mkdir -p /data \
    && chown memorigo:memorigo /data
COPY --from=build /out/memorigo-mcp /usr/local/bin/memorigo-mcp
USER memorigo
EXPOSE 8787
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8787/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/memorigo-mcp"]
CMD ["-http", "-addr", "0.0.0.0:8787", "-db", "/data/memorigo.db"]
