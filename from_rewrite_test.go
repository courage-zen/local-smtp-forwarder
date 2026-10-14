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
