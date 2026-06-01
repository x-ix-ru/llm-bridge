FROM golang:1.26.3-alpine AS builder

WORKDIR /build
COPY . .
RUN go mod download
RUN go version
RUN pwd
RUN find . -type f | sort

RUN CGO_ENABLED=0 go build -o /build/llm-bridge ./cmd/llm-bridge

FROM alpine:3.21

RUN apk add --no-cache bash curl net-tools vim

COPY --from=builder /build/llm-bridge /llm-bridge
COPY docs/release-notes.md /app/release-notes.md
COPY config.yaml.example /config.yaml

ARG BUILD_VERSION
ARG BUILD_DATE

LABEL org.opencontainers.image.version="${BUILD_VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.title="LLM Bridge"
LABEL org.opencontainers.image.description="HTTP proxy/bridge for LLM cluster routing with auto-discovery, metrics, and admin UI"
LABEL release-notes="/app/release-notes.md"

EXPOSE 8080
ENTRYPOINT ["/llm-bridge"]
