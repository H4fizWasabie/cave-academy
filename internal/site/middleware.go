package site

import (
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// maxBody caps every non-GET request body. Handlers that need less (chat,
// admin) set a smaller cap of their own on top.
const maxBody = 64 << 10

// securityHeaders are set on every response. HSTS is Caddy's job. The CSP has
// no 'unsafe-inline': scripts are files and SVG icons are styled by icons.css.
var securityHeaders = map[string]string{
	"Content-Security-Policy": "default-src 'self'; img-src 'self' data:; style-src 'self' https://fonts.googleapis.com; " +
		"font-src https://fonts.gstatic.com; script-src 'self'; connect-src 'self'; form-action 'self' https://wa.me; " +
		"frame-ancestors 'none'; base-uri 'self'; object-src 'none'",
	"X-Content-Type-Options": "nosniff",
	"Referrer-Policy":        "strict-origin-when-cross-origin",
	"X-Frame-Options":        "DENY",
	"Permissions-Policy":     "camera=(), microphone=(), geolocation=(), payment=(), usb=(), accelerometer=(), gyroscope=(), magnetometer=()",
}

// statusWriter remembers the status code and whether the header went out.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ServeHTTP is the middleware chain: security headers, body cap, panic
// recovery and an access log line, around the mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	defer func() {
		if rec := recover(); rec != nil {
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			log.Printf("panic: %s %q: %v\n%s", r.Method, logPath(r), rec, debug.Stack())
			if !sw.wrote {
				s.serverError(sw, r)
			}
		}
		// Method, path and status only: never the query string, cookies or body.
		log.Printf("%s %q %d %s ip=%s", r.Method, logPath(r), sw.status, time.Since(start).Round(time.Millisecond), clientIP(r))
	}()
	h := sw.Header()
	for k, v := range securityHeaders {
		h.Set(k, v)
	}
	if strings.HasPrefix(r.URL.Path, "/admin") {
		h.Set("Cache-Control", "no-store") // PII pages and CSV exports
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		r.Body = http.MaxBytesReader(sw, r.Body, maxBody)
	}
	s.mux.ServeHTTP(&reqWriter{sw, r}, r) // reqWriter lets render find the logged-in user
}

// logPath is the request path for the log: the BCL webhook's secret key is
// part of its path, so that tail never reaches the log.
func logPath(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/bcl/webhook/") {
		return "/bcl/webhook/…"
	}
	return r.URL.Path
}

// notFound renders the styled 404 page. /static/ misses stay plain.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/static/") {
		http.NotFound(w, r)
		return
	}
	s.errorPage(w, r, http.StatusNotFound, "Halaman tidak dijumpai", "Maaf, halaman yang anda cari tiada atau telah dialihkan.")
}

// serverError renders the styled 500 page.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request) {
	s.errorPage(w, r, http.StatusInternalServerError, "Sesuatu tidak kena",
		"Maaf, berlaku ralat di pihak kami. Sila cuba lagi sebentar lagi, atau hubungi kami melalui WhatsApp.")
}

type errorData struct {
	Code          int
	Heading, Text string
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, code int, heading, text string) {
	// Wrapped so renderStatus can look up the user; a bare writer would just skip that.
	if _, ok := w.(*reqWriter); !ok {
		w = &reqWriter{w, r}
	}
	s.renderStatus(w, code, "error", Page{Title: heading, CSS: []string{"pages"}, Data: errorData{code, heading, text}})
}

// healthz answers 200 "ok" when the database responds, else 503. No auth.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := s.db.PingContext(r.Context()); err != nil {
		log.Printf("healthz: %v", err)
		http.Error(w, "db down", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

// abuseLimits are the cost and spam guards held by Server (see chat.go and
// formAllowed). In memory, like the auth limiters.
type abuseLimits struct {
	chatIP   *limiter // chat POSTs per client IP
	formsIP  *limiter // saved contact/RFQ/trainer forms per client IP
	formsDay *limiter // saved forms overall, so spam cannot drain the shared Brevo quota
}

func newAbuseLimits() *abuseLimits {
	return &abuseLimits{
		chatIP:   newLimiter(30, 10*time.Minute),
		formsIP:  newLimiter(5, time.Hour),
		formsDay: newLimiter(100, 24*time.Hour),
	}
}

// useClock points every limiter at now (tests inject a fake clock).
func (a *abuseLimits) useClock(now func() time.Time) {
	for _, l := range []*limiter{a.chatIP, a.formsIP, a.formsDay} {
		l.now = now
	}
}

// formAllowed refuses a form POST when the client or the site is over its
// quota. It answers 429 itself and returns false. Callers count a form with
// formSaved once it is stored.
func (s *Server) formAllowed(w http.ResponseWriter, r *http.Request) bool {
	if s.abuse.formsIP.over(clientIP(r)) || s.abuse.formsDay.over("all") {
		log.Printf("forms: limit hit, ip=%s", clientIP(r))
		s.errorPage(w, r, http.StatusTooManyRequests, "Terlalu banyak penghantaran borang",
			"Terlalu banyak penghantaran borang. Sila cuba lagi dalam sejam, atau hubungi kami di WhatsApp.")
		return false
	}
	return true
}

func (s *Server) formSaved(r *http.Request) {
	s.abuse.formsIP.add(clientIP(r))
	s.abuse.formsDay.add("all")
}

// fromLoopback reports whether the peer is on this machine, i.e. the reverse
// proxy, the only peer whose X-Forwarded-* headers are believed.
func fromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
