# local-smtp-forwarder

A minimal SMTP relay written in Go. It listens on a local port and forwards
every received email to an upstream SMTP server (e.g. Gmail, your corporate
relay). Useful when an application can only speak SMTP to `localhost` but the
real mail server requires authenticated TLS connections that the app doesn't
want to handle.

## How it works

```
your app  --SMTP-->  local-smtp-forwarder (127.0.0.1:2525)  --SMTP+TLS+AUTH-->  upstream relay (smtp.gmail.com:587)
```

- Accepts any message on the local listen address (no auth, no TLS — it's local).
- Connects to the upstream relay with STARTTLS / implicit TLS / plaintext.
- Authenticates with AUTH PLAIN, falling back to AUTH LOGIN for servers that
  don't support PLAIN.
- Forwards the raw message bytes as-is.

## Build

```bash
make build          # builds ./local-smtp-forwarder for your host
```

Cross-compile for Linux:

```bash
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o local-smtp-forwarder-linux-arm64 .
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o local-smtp-forwarder-linux-amd64 .
```

## Configure

Copy `config.example.yaml` to `config.yaml` and edit:

```yaml
listen: "127.0.0.1:2525"

upstream:
  host: "smtp.gmail.com"
  port: 587
  username: "user@gmail.com"
  password: "app-password"   # Gmail: use an App Password, not your account password
  tls: "starttls"            # "starttls" | "implicit" | "none"
```

All fields can also be set via environment variables, which override the file:

| Env | Field |
|-----|-------|
| `MAILER_CONFIG` | Path to config file (default `config.yaml`) |
| `MAILER_LISTEN` | `listen` |
| `MAILER_UPSTREAM_HOST` | `upstream.host` |
| `MAILER_UPSTREAM_PORT` | `upstream.port` |
| `MAILER_UPSTREAM_USERNAME` | `upstream.username` |
| `MAILER_UPSTREAM_PASSWORD` | `upstream.password` |
| `MAILER_UPSTREAM_TLS` | `upstream.tls` |

## Run

```bash
make run
# or
./local-smtp-forwarder
```

Send a test email:

```bash
(echo "Subject: test"; echo "hello") | sendmail -t user@example.com
# or with swaks:
swaks --to user@example.com --from me@local --server 127.0.0.1:2525 --body "hello"
```

## TLS modes

- `starttls` (default) — plain TCP, then upgrades with STARTTLS. Use port 587.
- `implicit` — TLS from the start. Use port 465.
- `none` — plaintext, no TLS. **Not recommended**; only for testing.

## Docker

The included `Dockerfile` is a runtime-only stage that copies a pre-built
`linux/arm64` binary into an Alpine image. Build the binary first, then:

```bash
docker build -t local-smtp-forwarder .
docker run --rm -p 2525:2525 \
  -e MAILER_LISTEN=0.0.0.0:2525 \
  -e MAILER_UPSTREAM_HOST=smtp.gmail.com \
  -e MAILER_UPSTREAM_PORT=587 \
  -e MAILER_UPSTREAM_TLS=starttls \
  -e MAILER_UPSTREAM_USERNAME=user@gmail.com \
  -e MAILER_UPSTREAM_PASSWORD=app-password \
  local-smtp-forwarder
```

## Project layout

```
main.go          entrypoint: loads config, starts SMTP server
config.go        config loading (YAML file + env overrides)
server.go        SMTP backend/session: accepts mail, hands to Relay
relay.go         upstream SMTP client: TLS, AUTH PLAIN/LOGIN, forward
Dockerfile       runtime image (Alpine + pre-built binary)
```

## License

MIT
