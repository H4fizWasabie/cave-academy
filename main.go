package main

import (
	"log"
	"net/http"

	"github.com/H4fizWasabie/cave-academy/internal/site"
)

func main() {
	cfg := site.ConfigFromEnv()
	s, err := site.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	log.Printf("listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, s))
}
