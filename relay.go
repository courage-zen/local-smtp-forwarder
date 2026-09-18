package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Relay connects to an upstream SMTP server and forwards received emails.
type Relay struct {
	host     string
	port     int
	username string
	password string
	tlsMode  string // "starttls", "implicit", "none"
}

// NewRelay creates a Relay from upstream config.
func NewRelay(cfg UpstreamConfig) *Relay {
	return &Relay{
		host:     cfg.Host,
		port:     cfg.Port,
		username: cfg.Username,
		password: cfg.Password,
		tlsMode:  cfg.TLS,
	}
}

// loginAuth implements SMTP AUTH LOGIN (RFC 4954). Used as a fallback when the
// upstream server does not support AUTH PLAIN (e.g. some corporate SMTP relays).
type loginAuth struct {
	username string
	password string
	host     string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, fmt.Errorf("unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, fmt.Errorf("wrong host name: %q does not match expected %q", server.Name, a.host)
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}

	raw := strings.TrimSpace(string(fromServer))
	challenge := strings.ToLower(raw)
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		challenge = strings.ToLower(strings.TrimSpace(string(decoded)))
	}

	switch {
	case strings.Contains(challenge, "username"):
		return []byte(a.username), nil
	case strings.Contains(challenge, "password"):
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("unexpected LOGIN challenge %q", raw)
	}
}

// openClient dials and returns a ready-to-use SMTP client (after TLS upgrade).
func (r *Relay) openClient() (*smtp.Client, error) {
	addr := net.JoinHostPort(r.host, strconv.Itoa(r.port))
	tlsCfg := &tls.Config{ServerName: r.host}

	var conn net.Conn
	var err error

	if r.tlsMode == "implicit" {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 10 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("dial upstream %s: %w", addr, err)
	}
	if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, r.host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp client: %w", err)
	}

	// STARTTLS upgrade for non-implicit connections.
	if r.tlsMode != "none" && r.tlsMode != "implicit" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(tlsCfg); err != nil {
				c.Close()
				return nil, fmt.Errorf("starttls: %w", err)
			}
		}
	}

	return c, nil
}

// shouldFallbackToLogin checks if a PLAIN auth error indicates the server
// doesn't support PLAIN and we should try LOGIN instead.
func shouldFallbackToLogin(plainErr error, c *smtp.Client) bool {
	msg := strings.ToLower(plainErr.Error())
	if !strings.Contains(msg, "unrecognized authentication type") && !strings.Contains(msg, "504") {
		return false
	}
	ok, authLine := c.Extension("AUTH")
	return ok && strings.Contains(strings.ToUpper(authLine), "LOGIN")
}

// Send forwards a raw email to the upstream SMTP server.
// from is the envelope sender, to is the envelope recipients, data is the raw
// email bytes (headers + body) as received from the inbound SMTP session.
func (r *Relay) Send(from string, to []string, data []byte) error {
	c, err := r.openClient()
	if err != nil {
		return err
	}
	defer c.Close()

	// Authenticate — try PLAIN first, fall back to LOGIN if the server rejects
	// PLAIN (e.g. some corporate SMTP relays only support AUTH LOGIN).
	if r.username != "" {
		plainErr := c.Auth(smtp.PlainAuth("", r.username, r.password, r.host))
		if plainErr != nil {
			if !shouldFallbackToLogin(plainErr, c) {
				return fmt.Errorf("auth: %w", plainErr)
			}
			// Reconnect for a clean session, then try LOGIN.
			c.Close()
			c, err = r.openClient()
			if err != nil {
				return fmt.Errorf("auth: plain failed (%v); login reconnect failed: %w", plainErr, err)
			}
			defer c.Close()
			if err = c.Auth(&loginAuth{username: r.username, password: r.password, host: r.host}); err != nil {
				return fmt.Errorf("auth: plain failed (%v); login failed: %w", plainErr, err)
			}
		}
	}

	// Envelope.
	if err = c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, recipient := range to {
		if err = c.Rcpt(recipient); err != nil {
			return fmt.Errorf("RCPT TO <%s>: %w", recipient, err)
		}
	}

	// Data — forward the raw email bytes as-is.
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err = w.Write(data); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	_ = c.Quit()
	return nil
}

func (r *Relay) String() string {
	return fmt.Sprintf("%s:%d (%s)", r.host, r.port, r.tlsMode)
}
