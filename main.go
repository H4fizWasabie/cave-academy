package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/H4fizWasabie/cave-academy/internal/site"
)

func main() {
	cfg := site.ConfigFromEnv()
	if len(os.Args) > 1 {
		os.Exit(adminCommand(cfg, os.Args[1:]))
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// newHTTPServer is the production http.Server. WriteTimeout leaves room for
// the chat waiting up to 30s on OpenRouter.
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// run serves until SIGINT/SIGTERM, then drains in-flight requests (20s at
// most) and closes the database.
func run(cfg site.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	s, err := site.New(cfg)
	if err != nil {
		return err
	}
	defer s.Close()
	srv := newHTTPServer(cfg.Addr, s)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("listening on %s (APP_ENV=%s)", cfg.Addr, cfg.Env)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil // deferred s.Close() closes the DB after the server has drained
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
