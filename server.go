package main

import (
	"fmt"
	"io"
	"log"

	"github.com/emersion/go-smtp"
)

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

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

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

func (s *Session) Reset() {
	s.from = ""
	s.to = nil
}

func (s *Session) Logout() error {
	return nil
}
