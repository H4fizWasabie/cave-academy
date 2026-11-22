package site

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// knowledgePost sends a multipart POST to /admin/pengetahuan. file is
// {filename, content} or nil.
func knowledgePost(s *Server, c *http.Cookie, fields map[string]string, file []string, hdr ...string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if file != nil {
		fw, _ := mw.CreateFormFile("file", file[0])
		fw.Write([]byte(file[1]))
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, knowledgePath, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func docCount(s *Server) (n int) {
	s.db.QueryRow(`SELECT COUNT(*) FROM knowledge_docs`).Scan(&n)
	return
}

func TestKnowledgeAdminAccess(t *testing.T) {
	s := newTestServer(t)
	if rec := request(s, knowledgePath); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/log-masuk") {
		t.Errorf("logged out GET = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	user := signUp(t, s, "biasa@example.com")
	if rec := request(s, knowledgePath, user); rec.Code != http.StatusNotFound {
		t.Errorf("non-admin GET = %d, want 404", rec.Code)
	}
	if rec := knowledgePost(s, user, map[string]string{"title": "X", "bot": "all", "content": "isi"}, nil); rec.Code != http.StatusNotFound || docCount(s) != 0 {
		t.Errorf("non-admin POST = %d, docs %d", rec.Code, docCount(s))
	}
	if rec := knowledgePost(s, nil, map[string]string{"title": "X", "bot": "all", "content": "isi"}, nil); rec.Code != http.StatusSeeOther || docCount(s) != 0 {
		t.Errorf("logged out POST = %d, docs %d", rec.Code, docCount(s))
	}
	admin := adminCookie(t, s, "admin@example.com")
	rec := request(s, knowledgePath, admin)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Tambah bahan") {
		t.Errorf("admin GET = %d", rec.Code)
	}
}

func TestKnowledgeAddListDelete(t *testing.T) {
	s := newTestServer(t)
	admin := adminCookie(t, s, "admin@example.com")
	rec := knowledgePost(s, admin, map[string]string{"title": "Bengkel <b>Guru</b>", "bot": "educators", "content": "Yuran bengkel RM450.\n\nSijil diberi."}, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != knowledgePath {
		t.Fatalf("add = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	page := request(s, knowledgePath, admin).Body.String()
	if !strings.Contains(page, "Bengkel &lt;b&gt;Guru&lt;/b&gt;") || !strings.Contains(page, "NLP Bot Educators") {
		t.Errorf("list does not show the doc escaped under its bot:\n%s", page)
	}
	if !strings.Contains(request(s, "/admin", admin).Body.String(), "Pengetahuan") {
		t.Error("overview lacks the Pengetahuan card")
	}
	var id string
	s.db.QueryRow(`SELECT id FROM knowledge_docs`).Scan(&id)

	// Delete needs same origin.
	rec = authPost(t, s, knowledgePath, url.Values{"action": {"delete"}, "id": {id}}, []*http.Cookie{admin}, "Origin", "https://evil.example")
	if rec.Code != http.StatusForbidden || docCount(s) != 1 {
		t.Errorf("cross-origin delete = %d, docs %d", rec.Code, docCount(s))
	}
	rec = authPost(t, s, knowledgePath, url.Values{"action": {"delete"}, "id": {id}}, []*http.Cookie{admin})
	var chunks int
	s.db.QueryRow(`SELECT COUNT(*) FROM knowledge_chunks`).Scan(&chunks)
	if rec.Code != http.StatusSeeOther || docCount(s) != 0 || chunks != 0 {
		t.Errorf("delete = %d, docs %d, chunks %d", rec.Code, docCount(s), chunks)
	}
	if rec := authPost(t, s, knowledgePath, url.Values{"action": {"delete"}, "id": {"abc"}}, []*http.Cookie{admin}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d", rec.Code)
	}
}

func TestKnowledgeAddCrossOriginRefused(t *testing.T) {
	s := newTestServer(t)
	admin := adminCookie(t, s, "admin@example.com")
	rec := knowledgePost(s, admin, map[string]string{"title": "X", "bot": "all", "content": "isi"}, nil, "Origin", "https://evil.example")
	if rec.Code != http.StatusForbidden || docCount(s) != 0 {
		t.Errorf("cross-origin add = %d, docs %d", rec.Code, docCount(s))
	}
}

func TestKnowledgeValidation(t *testing.T) {
	s := newTestServer(t)
	admin := adminCookie(t, s, "admin@example.com")
	ok := map[string]string{"title": "T", "bot": "all", "content": "isi"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for a, b := range ok {
			m[a] = b
		}
		m[k] = v
		return m
	}
	for name, c := range map[string]struct {
		fields map[string]string
		file   []string
	}{
		"bad bot":       {with("bot", "nope"), nil},
		"no title":      {with("title", "  "), nil},
		"long title":    {with("title", strings.Repeat("a", 151)), nil},
		"empty content": {with("content", " \n "), nil},
		"text too big":  {with("content", strings.Repeat("a", maxKnowledgeText+1)), nil},
		"bad ext":       {with("content", ""), []string{"x.pdf", "isi"}},
		"double ext":    {with("content", ""), []string{"x.txt.exe", "isi"}},
		"file too big":  {with("content", ""), []string{"x.txt", strings.Repeat("a", maxKnowledgeFile+1)}},
		"not utf8":      {with("content", ""), []string{"x.txt", "\xff\xfe\x00bad"}},
		"empty file":    {with("content", ""), []string{"x.md", ""}},
	} {
		rec := knowledgePost(s, admin, c.fields, c.file)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge || docCount(s) != 0 {
			t.Errorf("%s: status %d, docs %d", name, rec.Code, docCount(s))
		}
	}
}

func TestKnowledgeUploadAndLargeBody(t *testing.T) {
	s := newTestServer(t)
	admin := adminCookie(t, s, "admin@example.com")
	// 200 KB of pasted text is far over the global 64 KB body cap.
	big := strings.Repeat("Ayat ujian tentang bengkel. ", 7000) // ~196 KB
	if len(big) <= maxBody || len(big) > maxKnowledgeText {
		t.Fatalf("test text is %d bytes", len(big))
	}
	if rec := knowledgePost(s, admin, map[string]string{"title": "Besar", "bot": "all", "content": big}, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("200 KB text = %d", rec.Code)
	}
	// A 1 MB .md upload is accepted and takes precedence over the text box.
	md := strings.Repeat("Baris markdown tentang latihan.\n\n", 30000)
	if len(md) > maxKnowledgeFile || len(md) < 900<<10 {
		t.Fatalf("md is %d bytes", len(md))
	}
	if rec := knowledgePost(s, admin, map[string]string{"title": "Fail", "bot": "hr", "content": "diabaikan"}, []string{"Nota.MD", md}); rec.Code != http.StatusSeeOther {
		t.Fatalf("1 MB upload = %d", rec.Code)
	}
	var content string
	s.db.QueryRow(`SELECT content FROM knowledge_docs WHERE title = 'Fail'`).Scan(&content)
	if content != md {
		t.Errorf("stored %d bytes, want the file's %d", len(content), len(md))
	}
	// A body over the cap is refused outright.
	if rec := knowledgePost(s, admin, map[string]string{"title": "X", "bot": "all"}, []string{"x.txt", strings.Repeat("a", maxKnowledgeBody)}); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body = %d, want 413", rec.Code)
	}
}

// Every other POST route still has the 64 KB cap; only the knowledge page is raised.
func TestBodyCapStillAppliesElsewhere(t *testing.T) {
	s := newTestServer(t)
	admin := adminCookie(t, s, "admin@example.com")
	rec := authPost(t, s, "/admin/pengguna", url.Values{"id": {"1"}, "plan": {"paid"}, "pad": {strings.Repeat("a", 70<<10)}}, []*http.Cookie{admin})
	if rec.Code == http.StatusSeeOther {
		t.Error("a 70 KB body got through the global cap")
	}
}
