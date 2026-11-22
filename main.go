package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/H4fizWasabie/cave-academy/internal/site"
)

func main() {
	cfg := site.ConfigFromEnv()
	if len(os.Args) > 1 {
		os.Exit(adminCommand(cfg, os.Args[1:]))
	}
	s, err := site.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	log.Printf("listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, s))
}

// adminCommand runs `make-admin <email>` or `revoke-admin <email>` against
// DB_PATH and returns the exit code: 0 on success, 1 when the account does
// not exist, 2 on bad usage.
func adminCommand(cfg site.Config, args []string) int {
	if (args[0] != "make-admin" && args[0] != "revoke-admin") || len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cave-academy [make-admin|revoke-admin <email>]")
		return 2
	}
	db, err := site.OpenDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()
	on := args[0] == "make-admin"
	if err := site.SetAdmin(db, args[1], on); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if on {
		fmt.Printf("%s is now an admin\n", args[1])
	} else {
		fmt.Printf("%s is no longer an admin\n", args[1])
	}
	return 0
}
