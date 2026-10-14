package main

import (
	"bytes"
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
	// head ends with the last header line's line ending; sep is the rest of
	// the blank line, so head+sep+body reassembles the original message.
	b = append(b, nl...)
	b = append(b, sep...)
	b = append(b, body...)
	return b, true
}

// splitMessage splits data into header block, separator ("\r\n\r\n" or
// "\n\n"), and body at the first blank line. The header block includes the
// line ending of the last header line; the separator is the remaining part
// of the blank line, so head+sep+body == data. ok is false when there is no
// header/body split (treat as opaque, forward as-is).
func splitMessage(data []byte) (head, sep, body []byte, ok bool) {
	crlf := bytes.Index(data, []byte("\r\n\r\n"))
	lf := bytes.Index(data, []byte("\n\n"))
	// Take whichever blank line comes first: a bare-LF message whose body
	// contains \r\n\r\n (e.g. a forwarded MIME part) must still split at the
	// real header/body separator.
	if crlf != -1 && (lf == -1 || crlf < lf) {
		return data[:crlf+2], data[crlf+2 : crlf+4], data[crlf+4:], true
	}
	if lf != -1 {
		return data[:lf+1], data[lf+1 : lf+2], data[lf+2:], true
	}
	return nil, nil, nil, false
}

// detectNewline returns the line ending used in the header block.
func detectNewline(head []byte) string {
	if bytes.Index(head, []byte("\r\n")) != -1 {
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
	// head ends with the last header line's line ending; inserting the new
	// From header before head requires putting the line ending after the new
	// line instead. A fresh buffer is required: head/sep/body alias data, and
	// the prepend would overwrite head before it is copied.
	need := len(newFrom) + len(nl) + len(head) + len(sep) + len(body)
	b := make([]byte, 0, need)
	b = append(b, newFrom...)
	b = append(b, nl...)
	b = append(b, head...)
	b = append(b, sep...)
	b = append(b, body...)
	return b, true
}
