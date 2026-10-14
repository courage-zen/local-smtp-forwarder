# From Header Display Name Rewrite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the `From:` header of forwarded emails to add a display name when the current display name is empty, missing, or equals the email address itself — but only for sender addresses listed in the new `from_display_names` config mapping. All other messages are forwarded byte-for-byte.

**Architecture:** Byte-level header surgery in a new pure function (`from_rewrite.go`): split raw message at the first blank line, locate and parse the `From:` header with stdlib `net/mail` (parse-only), and splice in a freshly generated From header when a rewrite is needed. `net/mail` is never used to re-serialize the whole message. The mapping flows Config → Backend → Session, and `Session.Data` calls the rewriter before handing bytes to Relay (Relay stays untouched). Spec: `docs/superpowers/specs/2026-10-14-from-display-name-design.md`.

**Tech Stack:** Go 1.23, stdlib only for the new code (`net/mail`, `strings`, `encoding/base64`, `unicode`), `gopkg.in/yaml.v3` for config (already a dependency). Tests with `go test` (the project's first tests).

**Core premise (from spec):** no message is ever modified unless its sender email matches a `from_display_names` entry. Every code path begins with this lookup; any miss (or an empty/absent mapping) returns the message untouched, byte-for-byte.

**Verified behavior of `net/mail` (checked with go run on 2026-10-14, Go 1.23):**

- `mail.ParseAddress("alice@example.com <alice@example.com>")` → **ERROR** ("expected single address"). The bare-unquoted-name form is NOT parsed. Only the quoted form works.
- `mail.ParseAddress("\"alice@example.com\" <alice@example.com>")` → Name=`alice@example.com`, Address=`alice@example.com`. This is the "display name equals email" case that must be rewritten.
- `mail.ParseAddressList` handles comma lists; a multi-address header must use the LIST parser first.
- `mail.ParseAddress("=?utf-8?B?5byg5LiJ?= <alice@example.com>")` → Name=`张三` (encoded-words are decoded by the parser).
- `base64(张三)` = `5byg5LiJ`.
- `mail.Address.String()` always quotes display names (`"Bob" <bob@...>`), so `formatFromHeader` writes the name raw (or RFC 2047-encoded) instead of using `String()` — to keep minimal-change output readable.

Consequences baked into the plan:

1. Parse order: `ParseAddressList` first, fall back to `ParseAddress` only if the list parse fails.
2. "Display name equals email" is only detectable in the quoted form; the unquoted form is unparseable → message unchanged (fail open).
3. ASCII display names are written unquoted and unencoded; non-ASCII names are RFC 2047 B-encoded.

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `from_rewrite.go` | Create | Pure From-header rewriting: `rewriteFromHeader`, `splitMessage`, `splitHeaderLines`, `findFromHeader`, `headerValue`, `parseFromHeader`, `needsDisplayName`, `formatFromHeader`, `encodeDisplayName`, `isASCII`, `addMissingFrom` |
| `from_rewrite_test.go` | Create | Table-driven tests for the rewriter |
| `config.go` | Modify | Add `FromDisplayNames map[string]string` to `Config`; lowercase keys at load |
| `config_test.go` | Create | Test that `from_display_names` parses from YAML with keys lowercased |
| `server.go` | Modify | `Backend`/`Session` gain `fromDisplayNames`; `Session.Data` calls the rewriter |
| `main.go` | Modify | Pass `cfg.FromDisplayNames` into `Backend` |
| `config.example.yaml` | Modify | Document the new key |
| `README.md` | Modify | New "From display names" subsection |
| `deploy/configmap.yaml` | Modify | Comment noting the key is file-only |
| `deploy/README.md` | Modify | Mention the key in the config notes |

Conventions: single `package main`, comments in English, tabs (gofmt default). Run `gofmt -w` on changed files before each commit.

---

### Task 1: Rewriter — core gate and existing-From rewrite

**Files:**
- Create: `from_rewrite.go`
- Create: `from_rewrite_test.go`

- [ ] **Step 1: Write the failing test**

Create `from_rewrite_test.go`:

```go
package main

import (
	"bytes"
	"testing"
)

func TestRewriteFromHeader(t *testing.T) {
	mapping := map[string]string{
		"alice@example.com": "张三",
		"bob@example.com":   "Bob",
	}

	tests := []struct {
		name         string
		data         string
		envelopeFrom string
		mapping      map[string]string
		want         string
	}{
		{
			name:         "bare address, mapped -> display name added",
			data:         "From: alice@example.com\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "bare address, unmapped -> unchanged (core premise)",
			data:         "From: carol@example.com\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "carol@example.com",
			mapping:      mapping,
			want:         "From: carol@example.com\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "nil mapping -> unchanged (core premise)",
			data:         "From: alice@example.com\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      nil,
			want:         "From: alice@example.com\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "address in header mixed case, mapping lowercase -> matched",
			data:         "From: <ALICE@EXAMPLE.COM>\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <ALICE@EXAMPLE.COM>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "quoted display name equals email -> rewritten",
			data:         "From: \"alice@example.com\" <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "quoted display name equals email, different case -> rewritten",
			data:         "From: \"ALICE@EXAMPLE.COM\" <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "existing different display name -> unchanged",
			data:         "From: Alice A <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: Alice A <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "ASCII display name -> written as-is",
			data:         "From: bob@example.com\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "bob@example.com",
			mapping:      mapping,
			want:         "From: Bob <bob@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "LF line endings -> preserved",
			data:         "From: bob@example.com\nSubject: hi\n\nbody",
			envelopeFrom: "bob@example.com",
			mapping:      mapping,
			want:         "From: Bob <bob@example.com>\nSubject: hi\n\nbody",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := rewriteFromHeader([]byte(tt.data), tt.envelopeFrom, tt.mapping)
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./...`
Expected: compile FAIL — `undefined: rewriteFromHeader`.

- [ ] **Step 3: Write the implementation**

Create `from_rewrite.go`:

```go
package main

import (
	"encoding/base64"
	"net/mail"
	"strings"
	"unicode"
)

// rewriteFromHeader returns data with the From header's display name set
// according to the mapping, and whether a rewrite happened. The envelope
// from is used only to synthesize a missing From header. Messages whose
// sender is not in the mapping — and messages that don't need a change —
// are returned unchanged with rewritten=false.
func rewriteFromHeader(data []byte, envelopeFrom string, mapping map[string]string) (out []byte, rewritten bool) {
	if len(mapping) == 0 {
		return data, false
	}

	head, sep, body, ok := splitMessage(data)
	if !ok {
		return data, false
	}

	nl := detectNewline(head)
	lines := splitHeaderLines(head, nl)
	fromIdx := findFromHeader(lines)
	if fromIdx == -1 {
		return addMissingFrom(data, head, sep, body, nl, envelopeFrom, mapping)
	}

	value := headerValue(lines, fromIdx)
	addr, others, ok := parseFromHeader(value)
	if !ok {
		return data, false
	}

	display, mapped := mapping[strings.ToLower(addr.Address)]
	if !mapped {
		return data, false // core premise: sender not configured, forward as-is
	}
	if !needsDisplayName(addr.Name, addr.Address) {
		return data, false
	}

	newFrom := formatFromHeader(display, addr.Address, others)

	// Replace the From header line and its folded continuation lines with
	// the single new line.
	end := fromIdx + 1
	for end < len(lines) && (strings.HasPrefix(lines[end], " ") || strings.HasPrefix(lines[end], "\t")) {
		end++
	}
	var b []byte
	for i, line := range lines {
		if i == fromIdx {
			b = append(b, newFrom...)
		} else if i > fromIdx && i < end {
			continue // folded continuation of the old From header — dropped
		} else {
			b = append(b, line...)
		}
		if i != len(lines)-1 {
			b = append(b, nl...)
		}
	}
	b = append(b, sep...)
	b = append(b, body...)
	return b, true
}

// splitMessage splits data into header block, separator ("\r\n\r\n" or
// "\n\n"), and body at the first blank line. ok is false when there is no
// header/body split (treat as opaque, forward as-is).
func splitMessage(data []byte) (head, sep, body []byte, ok bool) {
	if i := strings.Index(string(data), "\r\n\r\n"); i != -1 {
		return data[:i+2], data[i : i+4], data[i+4:], true
	}
	if i := strings.Index(string(data), "\n\n"); i != -1 {
		return data[:i+1], data[i : i+2], data[i+2:], true
	}
	return nil, nil, nil, false
}

// detectNewline returns the line ending used in the header block.
func detectNewline(head []byte) string {
	if strings.Index(string(head), "\r\n") != -1 {
		return "\r\n"
	}
	return "\n"
}

// splitHeaderLines splits the header block into lines, each without its line
// ending, using nl as the line separator.
func splitHeaderLines(head []byte, nl string) []string {
	s := string(head)
	s = strings.TrimSuffix(s, nl)
	return strings.Split(s, nl)
}

// findFromHeader returns the index of the line starting the From header
// (case-insensitive), or -1.
func findFromHeader(lines []string) int {
	for i, line := range lines {
		if len(line) >= 5 && strings.EqualFold(line[:5], "From:") {
			return i
		}
	}
	return -1
}

// headerValue returns the full unfolded value of the header at index idx,
// with continuation lines joined by a single space (RFC 5322 unfolding).
func headerValue(lines []string, idx int) string {
	v := strings.TrimSpace(lines[idx][5:])
	for i := idx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			v += " " + strings.TrimSpace(line)
		} else {
			break
		}
	}
	return v
}

// parseFromHeader parses the From header value. It returns the first
// address and the re-serialized remaining addresses when there are several;
// ok is false when the value is unparseable (fail open).
func parseFromHeader(value string) (addr *mail.Address, others string, ok bool) {
	list, err := mail.ParseAddressList(value)
	if err == nil && len(list) > 0 {
		first := list[0]
		if len(list) == 1 {
			return first, "", true
		}
		var strs []string
		for _, a := range list[1:] {
			strs = append(strs, a.String())
		}
		return first, strings.Join(strs, ", "), true
	}
	a, err1 := mail.ParseAddress(value)
	if err1 != nil {
		return nil, "", false
	}
	return a, "", true
}

// needsDisplayName reports whether the current display name should be
// replaced: it is empty, or it equals the email address (case-insensitive).
func needsDisplayName(name, email string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	return strings.EqualFold(name, email)
}

// formatFromHeader builds the replacement From header line (without the
// line ending). others carries the re-serialized remaining addresses when
// the original header had several, empty otherwise.
func formatFromHeader(display, email, others string) string {
	var b strings.Builder
	b.WriteString("From: ")
	if isASCII(display) {
		b.WriteString(display)
	} else {
		b.WriteString(encodeDisplayName(display))
	}
	b.WriteString(" <")
	b.WriteString(email)
	b.WriteString(">")
	if others != "" {
		b.WriteString(", ")
		b.WriteString(others)
	}
	return b.String()
}

// encodeDisplayName RFC 2047 B-encodes a UTF-8 display name.
func encodeDisplayName(name string) string {
	return "=?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte(name)) + "?="
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// addMissingFrom prepends a synthesized From header when the envelope from
// address is in the mapping; otherwise the message is returned unchanged
// (data carries the full original message for the no-op path).
func addMissingFrom(data, head, sep, body []byte, nl, envelopeFrom string, mapping map[string]string) (out []byte, rewritten bool) {
	addr, err := mail.ParseAddress(envelopeFrom)
	if err != nil {
		return data, false
	}
	display, mapped := mapping[strings.ToLower(addr.Address)]
	if !mapped {
		return data, false
	}
	newFrom := formatFromHeader(display, addr.Address, "")
	b := make([]byte, 0, len(head)+len(sep)+len(body)+len(newFrom)+len(nl))
	b = append(b, newFrom...)
	b = append(b, nl...)
	b = append(b, head...)
	b = append(b, sep...)
	b = append(b, body...)
	return b, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestRewriteFromHeader -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
gofmt -w from_rewrite.go from_rewrite_test.go
git add from_rewrite.go from_rewrite_test.go
git commit -m "feat: rewrite From header display name for configured senders"
```

---

### Task 2: Rewriter — edge cases (folded, multi-address, missing From, fail-open)

**Files:**
- Modify: `from_rewrite_test.go` (extend the test table)

- [ ] **Step 1: Extend the test table**

Add these cases to the `tests` slice in `TestRewriteFromHeader` (same `mapping`; `=?utf-8?B?5byg5LiJ?=` is 张三):

```go
		{
			name:         "missing From header + envelope mapped -> synthesized",
			data:         "Subject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "missing From header + envelope unmapped -> unchanged (core premise)",
			data:         "Subject: hi\r\n\r\nbody",
			envelopeFrom: "carol@example.com",
			mapping:      mapping,
			want:         "Subject: hi\r\n\r\nbody",
		},
		{
			name:         "missing From header + unparseable envelope -> unchanged",
			data:         "Subject: hi\r\n\r\nbody",
			envelopeFrom: "not an address",
			mapping:      mapping,
			want:         "Subject: hi\r\n\r\nbody",
		},
		{
			name:         "missing From header + empty envelope -> unchanged",
			data:         "Subject: hi\r\n\r\nbody",
			envelopeFrom: "",
			mapping:      mapping,
			want:         "Subject: hi\r\n\r\nbody",
		},
		{
			name:         "folded From header -> rewritten, other headers/body untouched",
			data:         "From:\r\n alice@example.com\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "multi-address From, first matches -> first rewritten, rest preserved",
			data:         "From: <alice@example.com>, Carol <carol@example.com>\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <alice@example.com>, \"Carol\" <carol@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "multi-address From, first does not match -> whole header unchanged (core premise)",
			data:         "From: Carol <carol@example.com>, <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: Carol <carol@example.com>, <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "malformed From -> unchanged, no panic (fail open)",
			data:         "From: \"unclosed quote <broken\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "",
			mapping:      mapping,
			want:         "From: \"unclosed quote <broken\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "unquoted display name equal to email -> unparseable, unchanged (fail open)",
			data:         "From: alice@example.com <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
			envelopeFrom: "",
			mapping:      mapping,
			want:         "From: alice@example.com <alice@example.com>\r\nSubject: hi\r\n\r\nbody",
		},
		{
			name:         "no header/body split -> unchanged",
			data:         "From: alice@example.com",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: alice@example.com",
		},
		{
			name:         "body containing From: line -> untouched (scan stops at blank line)",
			data:         "Subject: hi\r\n\r\nFrom: alice@example.com\r\nnot a header",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "Subject: hi\r\n\r\nFrom: alice@example.com\r\nnot a header",
		},
```

- [ ] **Step 2: Run tests**

Run: `go test ./... -run TestRewriteFromHeader -v`
Expected: all PASS with the Task 1 implementation (add the cases one at a time; if any fails, investigate before adjusting — the multi-address first-does-not-match case must miss the mapping lookup before any other check).

- [ ] **Step 3: Fix implementation if needed and re-run**

Run: `go test ./... -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
gofmt -w from_rewrite_test.go
git add from_rewrite_test.go
git commit -m "test: cover folded/multi-address/missing-From edge cases"
```

---

### Task 3: Config — add `from_display_names` mapping

**Files:**
- Modify: `config.go:12-15` (Config struct), `config.go:49-53` (LoadConfig)
- Create: `config_test.go`

- [ ] **Step 1: Write the failing test**

Create `config_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFromDisplayNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
listen: "127.0.0.1:9999"
upstream:
  host: "smtp.example.com"
  port: 465
  username: "u"
  password: "p"
  tls: "implicit"
from_display_names:
  Alice@Example.com: "张三"
  BOB@example.com: "Bob"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILER_CONFIG", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got := cfg.FromDisplayNames["alice@example.com"]; got != "张三" {
		t.Errorf("alice key not lowercased or missing: got %q", got)
	}
	if got := cfg.FromDisplayNames["bob@example.com"]; got != "Bob" {
		t.Errorf("bob key not lowercased or missing: got %q", got)
	}
	if len(cfg.FromDisplayNames) != 2 {
		t.Errorf("want 2 entries, got %d", len(cfg.FromDisplayNames))
	}
}

func TestLoadConfigNoFromDisplayNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
listen: "127.0.0.1:9999"
upstream:
  host: "smtp.example.com"
  username: "u"
  password: "p"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILER_CONFIG", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.FromDisplayNames != nil {
		t.Errorf("want nil FromDisplayNames, got %v", cfg.FromDisplayNames)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestLoadConfigFromDisplayNames -v`
Expected: compile FAIL — `cfg.FromDisplayNames undefined`.

- [ ] **Step 3: Write the implementation**

In `config.go`, change the Config struct:

```go
// Config holds all application configuration.
type Config struct {
	Listen   string         `yaml:"listen"`
	Upstream UpstreamConfig `yaml:"upstream"`
	// FromDisplayNames maps sender email addresses (lowercased at load) to
	// the display name to set on the From header when it is missing or bare.
	// File-only: there is no env override for this mapping.
	FromDisplayNames map[string]string `yaml:"from_display_names"`
}
```

In `LoadConfig`, change the else branch to:

```go
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
		// Lowercase mapping keys so lookups are case-insensitive.
		cfg.FromDisplayNames = lowerKeyMap(cfg.FromDisplayNames)
	}
```

Add at the end of `config.go`:

```go
// lowerKeyMap returns a copy of m with all keys lowercased. A nil map
// returns nil.
func lowerKeyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}
```

Add `"strings"` to the imports of `config.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -v`
Expected: PASS (all test functions).

- [ ] **Step 5: Commit**

```bash
gofmt -w config.go config_test.go
git add config.go config_test.go
git commit -m "feat: from_display_names config mapping with lowercased keys"
```

---

### Task 4: Server wiring — Backend/Session carry the mapping

**Files:**
- Modify: `server.go`
- Modify: `main.go:16`

- [ ] **Step 1: Wire the mapping through Backend and Session**

In `server.go`, update the structs and `NewSession`:

```go
// Backend implements go-smtp's Backend interface. It accepts all connections
// and forwards received emails to the upstream relay.
type Backend struct {
	relay            *Relay
	fromDisplayNames map[string]string
}

func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{relay: b.relay, fromDisplayNames: b.fromDisplayNames}, nil
}

// Session implements go-smtp's Session interface for a single SMTP transaction.
type Session struct {
	relay            *Relay
	fromDisplayNames map[string]string
	from             string
	to               []string
}
```

Update `Session.Data` (insert the rewrite between ReadAll and Send):

```go
func (s *Session) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read email data: %w", err)
	}

	if len(s.fromDisplayNames) > 0 {
		if rewritten, ok := rewriteFromHeader(data, s.from, s.fromDisplayNames); ok {
			log.Printf("from rewritten: %s", s.from)
			data = rewritten
		}
	}

	if err := s.relay.Send(s.from, s.to, data); err != nil {
		log.Printf("relay failed: from=%s to=%v err=%v", s.from, s.to, err)
		return err
	}

	log.Printf("relayed: from=%s to=%v (%d bytes)", s.from, s.to, len(data))
	return nil
}
```

In `main.go`, line 16 becomes:

```go
	backend := &Backend{relay: relay, fromDisplayNames: cfg.FromDisplayNames}
```

- [ ] **Step 2: Run tests and build to verify nothing broke**

Run: `go test ./... -v && go build ./...`
Expected: PASS + build succeeds.

- [ ] **Step 3: Commit**

```bash
gofmt -w server.go main.go
git add server.go main.go
git commit -m "feat: wire from_display_names through Backend/Session, rewrite before relay"
```

---

### Task 5: Docs — README, example config, deploy notes

**Files:**
- Modify: `README.md` (after the env table, before "## Run")
- Modify: `config.example.yaml` (append section)
- Modify: `deploy/configmap.yaml`
- Modify: `deploy/README.md:31`

- [ ] **Step 1: README.md — add "From display names" subsection**

After the env-var table (line 59) add:

```markdown
### From display names

Optional mapping of sender email addresses to display names. When a message's
`From:` header has no display name (or the display name is just the bare
address), the forwarder rewrites it to `From: Name <address>`. Senders not in
the mapping are forwarded untouched, byte-for-byte.

```yaml
from_display_names:
  alice@example.com: "张三"
  noreply@svc.example.com: "服务通知"
```

Matching is case-insensitive. Non-ASCII display names are written as
RFC 2047 encoded words (`=?utf-8?B?...?=`). This key is config-file-only:
there is no environment-variable override for it. If the `From:` header is
missing entirely, one is synthesized from the SMTP envelope sender when that
address is in the mapping.
```

- [ ] **Step 2: config.example.yaml — append the key**

Append to `config.example.yaml`:

```yaml

# 可选：From 头显示名映射（发件人 email -> 显示名）。
# 仅当 From 头没有显示名、或显示名就是裸地址时改写。
# 不在映射中的发件人原样转发。
from_display_names:
  # alice@example.com: "张三"
  # noreply@svc.example.com: "服务通知"
```

- [ ] **Step 3: deploy/configmap.yaml — note the key placement**

In `deploy/configmap.yaml`, inside `data:`, add the comment lines:

```yaml
  # Optional: From display names are file-only config. To use them in k8s,
  # mount a config file and set MAILER_CONFIG to its path — env overrides
  # do not exist for this mapping.
```

- [ ] **Step 4: deploy/README.md — update config notes**

After the line

```markdown
- ConfigMap `multica-mailer-config`：非敏感配置（listen、upstream host/port/tls）
```

add:

```markdown
- From 显示名映射（`from_display_names`）只能通过配置文件设置（`MAILER_CONFIG` 指向挂载的文件），无环境变量覆盖
```

- [ ] **Step 5: Run full test suite and build**

Run: `go test ./... -v && go vet ./... && go build ./...`
Expected: PASS + clean + build succeeds.

- [ ] **Step 6: Commit**

```bash
git add README.md config.example.yaml deploy/configmap.yaml deploy/README.md
git commit -m "docs: document from_display_names configuration"
```

---

## Verification (final)

- [ ] Run: `go test ./... -v` — all PASS
- [ ] Run: `go vet ./...` — clean
- [ ] Run: `go build ./...` — succeeds
- [ ] Manual smoke test (optional; needs a reachable upstream):

```bash
# Run the forwarder with a config containing:
#   from_display_names: { alice@example.com: "张三" }
go run .
printf 'From: alice@example.com\r\nSubject: t\r\n\r\nhi' | \
  swaks --to you@example.com --from alice@example.com --server 127.0.0.1:2525
```

Expected log line: `from rewritten: alice@example.com`.
