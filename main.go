package main

import (
	"log"

	"github.com/emersion/go-smtp"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	relay := NewRelay(cfg.Upstream)
	backend := &Backend{relay: relay}

	s := smtp.NewServer(backend)
	s.Addr = cfg.Listen
	s.Domain = "localhost"
	s.AllowInsecureAuth = true
	s.Debug = nil

	log.Printf("local-smtp-forwarder listening on %s → upstream %s", cfg.Listen, relay)

	if err := s.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
