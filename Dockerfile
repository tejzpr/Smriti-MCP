# Stage 1: Build
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev bash curl

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# Download LadybugDB native library for the build platform
RUN LBUG_DIR=$(go mod download -json github.com/LadybugDB/go-ladybug@v0.13.1 | grep '"Dir"' | cut -d'"' -f4) && \
    chmod -R u+w "$LBUG_DIR" && \
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
