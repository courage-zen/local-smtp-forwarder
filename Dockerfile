# Runtime stage — pin linux/arm64 to match the k8s arm64 node pool (same as bigdata-mcp).
# Uses the public alpine image from Docker Hub.
FROM --platform=linux/arm64 alpine:3.20

# ca-certificates.crt is already present in the base alpine image.
# Create a non-root user to run the mailer.
RUN adduser -D -u 10001 mailer

# Pre-built arm64 linux binary (cross-compiled on the dev machine, static, stripped).
COPY local-smtp-forwarder-linux-arm64 /usr/local/bin/local-smtp-forwarder
RUN chmod +x /usr/local/bin/local-smtp-forwarder

USER mailer
EXPOSE 2525

ENTRYPOINT ["/usr/local/bin/local-smtp-forwarder"]
