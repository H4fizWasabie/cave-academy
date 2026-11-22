package main

import (
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	for name, c := range map[string]struct{ got, want time.Duration }{
		"ReadHeaderTimeout": {srv.ReadHeaderTimeout, 5 * time.Second},
		"ReadTimeout":       {srv.ReadTimeout, 60 * time.Second},  // room for a 1.3MB knowledge upload on a slow link
		"WriteTimeout":      {srv.WriteTimeout, 45 * time.Second}, // the chat waits up to 30s on OpenRouter
		"IdleTimeout":       {srv.IdleTimeout, 120 * time.Second},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
	if srv.MaxHeaderBytes != 64<<10 {
		t.Errorf("MaxHeaderBytes = %d", srv.MaxHeaderBytes)
	}
}
