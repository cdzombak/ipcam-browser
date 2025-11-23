ARG BIN_NAME=ipcam-browser
ARG BIN_VERSION=<unknown>

# Build stage
FROM --platform=$BUILDPLATFORM golang:1-alpine AS builder
ARG BIN_NAME
ARG BIN_VERSION
RUN apk add --no-cache gcc musl-dev
RUN update-ca-certificates
WORKDIR /src/${BIN_NAME}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-X main.version=${BIN_VERSION}" -o ./out/${BIN_NAME} .

# Final stage
FROM alpine:latest
ARG BIN_NAME
ARG BIN_VERSION
RUN apk add --no-cache ca-certificates ffmpeg
COPY --from=builder /src/${BIN_NAME}/out/${BIN_NAME} /usr/bin/${BIN_NAME}
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Create a non-root user
RUN addgroup -g 1000 ipcam && \
    adduser -D -s /bin/sh -u 1000 -G ipcam ipcam

# Create cache directory
RUN mkdir -p /var/cache/ipcam-browser && \
    chown ipcam:ipcam /var/cache/ipcam-browser

USER ipcam
WORKDIR /home/ipcam

ENV CACHE_DIR=/var/cache/ipcam-browser

EXPOSE 8080

ENTRYPOINT ["/usr/bin/ipcam-browser"]

LABEL license="MIT"
LABEL maintainer="Chris Dzombak <https://www.dzombak.com>"
LABEL org.opencontainers.image.authors="Chris Dzombak <https://www.dzombak.com>"
LABEL org.opencontainers.image.url="https://github.com/cdzombak/${BIN_NAME}"
LABEL org.opencontainers.image.documentation="https://github.com/cdzombak/${BIN_NAME}/blob/main/README.md"
LABEL org.opencontainers.image.source="https://github.com/cdzombak/${BIN_NAME}.git"
LABEL org.opencontainers.image.version="${BIN_VERSION}"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.title="${BIN_NAME}"
LABEL org.opencontainers.image.description="Browse and view recordings from IP cameras with web UI"
