# Build stage — compile a static binary from source.
# TARGETOS / TARGETARCH are injected by buildx for multi-platform builds.
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' -o /out/local-smtp-forwarder .

# Runtime stage — minimal Alpine, non-root user, no shell useful for the app.
FROM alpine:3.20

RUN adduser -D -u 10001 mailer

COPY --from=builder /out/local-smtp-forwarder /usr/local/bin/local-smtp-forwarder

USER mailer
EXPOSE 2525

ENTRYPOINT ["/usr/local/bin/local-smtp-forwarder"]
