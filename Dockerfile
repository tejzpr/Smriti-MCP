# Stage 1: Build
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc g++ musl-dev bash curl tar

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Download LadybugDB native library and place where CGO expects it
RUN set -e && \
    LBUG_DIR=$(go mod download -json github.com/LadybugDB/go-ladybug@v0.13.1 | grep '"Dir"' | cut -d'"' -f4) && \
    chmod -R u+w "$LBUG_DIR" && \
    ARCH=$(uname -m) && \
    case "$ARCH" in x86_64) ARCH_LABEL="x86_64" ;; aarch64|arm64) ARCH_LABEL="aarch64" ;; esac && \
    curl -L -o /tmp/liblbug.tar.gz "https://github.com/LadybugDB/ladybug/releases/latest/download/liblbug-linux-${ARCH_LABEL}.tar.gz" && \
    mkdir -p "$LBUG_DIR/lib/dynamic/linux-$([ "$ARCH_LABEL" = "x86_64" ] && echo amd64 || echo arm64)" && \
    tar -xzf /tmp/liblbug.tar.gz -C /tmp && \
    find /tmp -name 'liblbug*' -exec cp {} "$LBUG_DIR/lib/dynamic/linux-$([ "$ARCH_LABEL" = "x86_64" ] && echo amd64 || echo arm64)/" \; && \
    ls -la "$LBUG_DIR/lib/dynamic/linux-$([ "$ARCH_LABEL" = "x86_64" ] && echo amd64 || echo arm64)/"

COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /smriti-mcp .

# Stage 2: Runtime
FROM alpine:3.23

RUN apk add --no-cache ca-certificates git

COPY --from=builder /smriti-mcp /usr/local/bin/smriti-mcp

RUN adduser -D -h /home/smriti smriti
USER smriti
WORKDIR /home/smriti

ENV STORAGE_LOCATION=/home/smriti/.smriti

ENTRYPOINT ["smriti-mcp"]
