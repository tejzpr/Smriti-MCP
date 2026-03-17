# Stage 1: Build
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev bash curl

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Download LadybugDB native library for the build platform
RUN LBUG_DIR=$(go env GOMODCACHE)/github.com/!ladybug!d!b/go-ladybug@$(grep 'go-ladybug' go.sum | head -1 | awk '{print $2}' | sed 's|/go.mod||') && \
    chmod +x "$LBUG_DIR/download_lbug.sh" && \
    cd "$LBUG_DIR" && bash download_lbug.sh

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
