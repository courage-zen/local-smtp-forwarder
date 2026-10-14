# From Header Display Name Rewrite — Design

Date: 2026-10-14
Status: Approved

## Summary

Add a feature to local-smtp-forwarder that rewrites the `From:` header of
forwarded emails to add a display name, when the current display name is empty
or equals the email address itself. The address-to-display-name mapping is
configured in the YAML config file.

## Motivation

Some local services send emails whose `From:` header is a bare address (e.g.
`From: noreply@svc.example.com`). Recipients see a bare email address instead
of a friendly name. The forwarder is the natural chokepoint to fix this before
mail reaches the upstream relay.

## Requirements

1. Exact-match mapping table in config: `from_display_names`, keyed by email
   address (case-insensitive match), value is the display name.
2. Only rewrite when the email address is in the mapping AND the current
   display name is empty, missing, or identical to the email address
   (case-insensitive). Otherwise forward untouched.
3. Support non-ASCII display names via RFC 2047 B-encoding
   (`From: =?utf-8?B?...?= <addr>`). Pure ASCII names are written as-is.
4. If the `From:` header is missing entirely, synthesize one from the envelope
   sender (`Session.from`) when the envelope address is in the mapping.
5. Minimal-change rewriting: only the From header line(s) are touched; all
   other headers and the body are forwarded byte-for-byte.
6. Emails whose From address is not in the mapping are forwarded as-is
   (current behavior preserved).

## Non-Goals

- Wildcard / pattern matching of addresses.
- A default display name applied to all unmapped addresses.
- Rewriting any other headers (To, Cc, Reply-To, ...).
- Env-var overrides for the mapping (map-shaped config, file-only; documented).
- Multi-address From headers: when the From header contains multiple
  addresses, the rewrite applies only if the FIRST address matches the
  mapping; the whole header is left intact otherwise.

## Design

### Approach chosen

Manual, byte-level header surgery on the raw message. The header block and
body are separated by the first blank line. The `From:` header is located and
parsed with stdlib `net/mail` (parse-only, used to decide whether a rewrite is
needed), then the original From header line(s) are replaced in the raw bytes
by a freshly generated one. Nothing else in the message changes.

Rejected alternatives:

- Full `mail.ReadMessage` parse + re-serialize: header order/folding is lost,
  body can be subtly altered — violates the minimal-change requirement.
- `emersion/go-message` dependency: adds a dependency for a single-header edit
  with no real benefit.

### Components

**Config (`config.go`)**

```go
type Config struct {
    Listen            string              `yaml:"listen"`
    Upstream          UpstreamConfig      `yaml:"upstream"`
    FromDisplayNames  map[string]string   `yaml:"from_display_names"`
}
```

Loaded by the existing `yaml.Unmarshal` path. Keys are lowercased at load time
so lookup is a single map access on a lowercased address. No env override.

**Rewriter (`from_rewrite.go`, new file)**

Pure function, no I/O:

```go
// rewriteFromHeader returns data with the From header's display name set
// per the mapping. Messages not needing a change are returned unchanged
// (same backing array).
func rewriteFromHeader(data []byte, mapping map[string]string) []byte
```

Behavior:

1. Split header block / body at the first `\r\n\r\n` (fallback `\n\n` for
   bare-LF messages; the chosen separator is reused on reassembly).
2. Scan header lines for a line starting with `From:` (case-insensitive),
   handling folded continuation lines (lines starting with space/tab belong to
   the previous header).
3. Collect the full From header value (unfolded), parse with
   `mail.ParseAddress` / `mail.ParseAddressList`:
   - If the first address's email (lowercased) is in the mapping AND its
     display name is empty or case-insensitively equals the email — rewrite.
   - Otherwise return data unchanged.
4. Replacement header: `From: <display> <addr>` where `<display>` is RFC 2047
   B-encoded when it contains non-ASCII, raw otherwise. Format:
   `From: 张三 <alice@example.com>` (ASCII) or
   `From: =?utf-8?B?...?= <alice@example.com>` (non-ASCII). Followed by the
   line ending style of the original message.
   When the original From had multiple addresses, the rewrite preserves the
   remaining addresses verbatim after the rewritten first one (rare case;
   acceptable to re-serialize them via net/mail).
5. Missing `From:` header: if the envelope from address (lowercased) is in the
   mapping, prepend `From: <display> <envelope-from>` as the first header.
   The envelope address is passed in by the caller.
   Signature becomes:
   `rewriteFromHeader(data []byte, envelopeFrom string, mapping map[string]string) []byte`
6. On any parse failure or ambiguity, return data unchanged (fail open —
   delivery is more important than cosmetics).

**Server wiring (`server.go`)**

- `Backend` gains a `fromDisplayNames map[string]string` field, set in
  `main.go` from `cfg.FromDisplayNames`.
- `Session.Data` calls `data = rewriteFromHeader(data, s.from, b.fromDisplayNames)`
  before `relay.Send`. (Session gains a reference to the mapping, or Backend
  passes it to the session at construction — Backend field on Session.)

### Data flow

```
client DATA -> Session.Data
             -> io.ReadAll
             -> rewriteFromHeader(data, s.from, mapping)   [new]
             -> relay.Send(from, to, data)                 [unchanged]
```

Relay stays purely transport-focused.

### Error handling

- Rewrite failures (unparseable From, weird folding): log a line
  (`from rewrite skipped: ...`), forward the message unchanged. Never reject
  or drop mail because of this feature.
- Config: empty mapping (nil map) short-circuits — zero overhead, messages
  forwarded byte-for-byte as today.

### Logging

When a rewrite happens, log `from rewritten: alice@example.com -> "张三"` at
the same level/place as the existing `relayed:` line. No logging for
non-rewritten messages (avoid noise).

## Testing

Table-driven unit tests for `rewriteFromHeader` in `from_rewrite_test.go`
(project's first tests):

- bare address, mapped → display name added
- bare address, unmapped → unchanged
- existing different display name → unchanged
- display name equals email address → rewritten
- display name equals email, different case → rewritten
- non-ASCII display name → RFC 2047 B-encoded output
- missing From header + envelope from mapped → header synthesized
- missing From header + envelope from unmapped → unchanged
- folded From header → handled, other headers/body untouched
- multiple addresses in From, first matches → first rewritten, rest preserved
- CRLF vs LF line endings → preserved
- nil mapping → unchanged
- malformed From (unparseable) → unchanged, no panic
- body containing lines starting with "From:" (e.g. multipart) → body untouched
  (header scan must stop at the blank line)

Config loading test: `from_display_names` parses from YAML, keys lowercased.

## Documentation

- `README.md`: new "From display names" subsection under Configure with YAML
  example and note that there is no env override for this key.
- `config.example.yaml`: annotated example entry.
- `deploy/README.md` / `deploy/configmap.yaml`: mention the key belongs in
  the ConfigMap (non-secret).

## Rollout

Single feature, backward compatible: absent config key → nil map → feature
off, behavior identical to today.
