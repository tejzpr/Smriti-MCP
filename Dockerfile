# Stage 1: Build
FROM golang:1.26-trixie AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ libc6-dev curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Download LadybugDB native library and place where CGO expects it
RUN set -e && \
    LBUG_DIR=$(go mod download -json github.com/LadybugDB/go-ladybug@v0.13.1 | grep '"Dir"' | cut -d'"' -f4) && \
    chmod -R u+w "$LBUG_DIR" && \
    ARCH=$(uname -m) && \
    case "$ARCH" in x86_64) GOARCH="amd64" ;; aarch64|arm64) GOARCH="arm64" ;; esac && \
    curl -L -o /tmp/liblbug.tar.gz "https://github.com/LadybugDB/ladybug/releases/latest/download/liblbug-linux-${ARCH}.tar.gz" && \
    mkdir -p /tmp/liblbug && tar -xzf /tmp/liblbug.tar.gz -C /tmp/liblbug && \
    mkdir -p "$LBUG_DIR/lib/dynamic/linux-${GOARCH}" && \
    find /tmp/liblbug -name '*.so*' -exec cp {} "$LBUG_DIR/lib/dynamic/linux-${GOARCH}/" \; && \
    ls -la "$LBUG_DIR/lib/dynamic/linux-${GOARCH}/" && \
    rm -rf /tmp/liblbug /tmp/liblbug.tar.gz

COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /smriti-mcp .

# Stage 2: Runtime
FROM debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git libc6 libstdc++6 && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /smriti-mcp /usr/local/bin/smriti-mcp
COPY --from=builder /go/pkg/mod/github.com/!ladybug!d!b/go-ladybug@v0.13.1/lib/dynamic /usr/local/lib/lbug

ENV LD_LIBRARY_PATH=/usr/local/lib/lbug/linux-amd64:/usr/local/lib/lbug/linux-arm64

RUN useradd -m smriti
USER smriti
WORKDIR /home/smriti

ENV STORAGE_LOCATION=/home/smriti/.smriti

ENTRYPOINT ["smriti-mcp"]
