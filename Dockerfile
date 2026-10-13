# Build stage — compile a static binary from source.
# buildx runs this stage on each target platform (arm64 via QEMU), so
# the Go toolchain natively targets the correct arch. No GOOS/GOARG
# overrides needed — Go uses the host platform by default.
FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/local-smtp-forwarder .

# Runtime stage — minimal Alpine, non-root user, no shell useful for the app.
FROM alpine:3.20

RUN adduser -D -u 10001 mailer

COPY --from=builder /out/local-smtp-forwarder /usr/local/bin/local-smtp-forwarder

USER mailer
EXPOSE 2525

ENTRYPOINT ["/usr/local/bin/local-smtp-forwarder"]
