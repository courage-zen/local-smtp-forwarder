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
			// Bare-LF headers; the body itself contains \r\n\r\n (e.g. a
			// forwarded MIME part). The split must happen at the FIRST blank
			// line (the real header/body separator), not inside the body.
			name:         "LF headers + CRLF blank line inside body -> split at first blank line",
			data:         "From: alice@example.com\nSubject: s\n\nbody-with\r\n\r\ncrlf-inside",
			envelopeFrom: "alice@example.com",
			mapping:      mapping,
			want:         "From: =?utf-8?B?5byg5LiJ?= <alice@example.com>\nSubject: s\n\nbody-with\r\n\r\ncrlf-inside",
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
