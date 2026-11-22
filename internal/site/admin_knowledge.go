package site

import (
	"errors"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Admin page /admin/pengetahuan: the client's own material for the bots (RAG,
// knowledge.go). Pasted text or a .txt/.md file; admins only.

const (
	knowledgePath     = "/admin/pengetahuan" // middleware.go lets this one path exceed maxBody
	maxKnowledgeText  = 200 << 10            // pasted text, bytes
	maxKnowledgeFile  = 1 << 20              // uploaded file, bytes
	maxKnowledgeBody  = maxKnowledgeFile + maxKnowledgeText + 64<<10
	maxKnowledgeTitle = 150 // characters
	botAll            = "all"
)

type knowledgeDoc struct {
	ID      int64
	Title   string
	Chunks  int
	Size    int // characters
	Created string
}

type knowledgeGroup struct {
	Label string
	Docs  []knowledgeDoc
}

// knowledgeView is Page.Data for admin_knowledge.html.
type knowledgeView struct {
	Groups []knowledgeGroup
	Bots   []Bot
	Docs   int
	Chunks int
	Error  string
	Title  string // form values kept after an error
	Bot    string
	Text   string
}

func (s *Server) adminKnowledge(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.renderKnowledge(w, u, http.StatusOK, knowledgeView{})
	case http.MethodPost:
		s.postKnowledge(w, r, u)
	default:
		http.Error(w, "Kaedah tidak dibenarkan", http.StatusMethodNotAllowed)
	}
}

func (s *Server) renderKnowledge(w http.ResponseWriter, u *User, status int, v knowledgeView) {
	v.Bots = bots
	if v.Bot == "" {
		v.Bot = botAll
	}
	rows, err := s.db.Query(`SELECT d.id, d.bot, d.title, length(d.content), d.created_at,
		(SELECT COUNT(*) FROM knowledge_chunks c WHERE c.doc_id = d.id) FROM knowledge_docs d ORDER BY d.id DESC`)
	if err != nil {
		log.Printf("admin knowledge: %v", err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	byBot := map[string][]knowledgeDoc{}
	for rows.Next() {
		var d knowledgeDoc
		var bot, at string
		if rows.Scan(&d.ID, &bot, &d.Title, &d.Size, &at, &d.Chunks) == nil {
			d.Created = makeCell("date", at).V
			byBot[bot] = append(byBot[bot], d)
			v.Docs++
			v.Chunks += d.Chunks
		}
	}
	rows.Close()
	if docs := byBot[botAll]; len(docs) > 0 {
		v.Groups = append(v.Groups, knowledgeGroup{Label: "Semua bot", Docs: docs})
	}
	for _, b := range bots {
		if docs := byBot[b.Slug]; len(docs) > 0 {
			v.Groups = append(v.Groups, knowledgeGroup{Label: "NLP Bot " + b.Name, Docs: docs})
		}
	}
	s.renderStatus(w, status, "admin_knowledge", Page{Title: "Admin: Pengetahuan", CSS: []string{"admin", "admin_knowledge"}, User: u, Data: v})
}

// postKnowledge adds (default) or deletes (action=delete) a document.
func (s *Server) postKnowledge(w http.ResponseWriter, r *http.Request, u *User) {
	if !sameOrigin(r) {
		http.Error(w, "Permintaan ditolak", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxKnowledgeBody)
	if err := r.ParseMultipartForm(2 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "Permintaan terlalu besar", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
		}
		return
	}
	if r.PostFormValue("action") == "delete" {
		id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "Permintaan tidak sah", http.StatusBadRequest)
			return
		}
		if err := s.deleteKnowledge(id); err != nil {
			log.Printf("admin knowledge delete: %v", err)
			http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, knowledgePath, http.StatusSeeOther)
		return
	}

	v := knowledgeView{Title: strings.TrimSpace(r.PostFormValue("title")), Bot: r.PostFormValue("bot"), Text: r.PostFormValue("content")}
	fail := func(msg string) {
		v.Error = msg
		s.renderKnowledge(w, u, http.StatusBadRequest, v)
	}
	if _, isBot := findBot(v.Bot); !isBot && v.Bot != botAll {
		fail("Pilih bot yang sah.")
		return
	}
	if v.Title == "" || utf8.RuneCountInString(v.Title) > maxKnowledgeTitle {
		fail("Tajuk diperlukan (maksimum 150 aksara).")
		return
	}
	content := v.Text
	if len(content) > maxKnowledgeText {
		fail("Teks terlalu panjang (maksimum 200 KB). Muat naik fail .txt atau .md (maksimum 1 MB) untuk bahan yang lebih besar.")
		return
	}
	if f, hdr, err := r.FormFile("file"); err == nil {
		defer f.Close()
		if ext := strings.ToLower(path.Ext(hdr.Filename)); ext != ".txt" && ext != ".md" {
			fail("Hanya fail .txt atau .md diterima.")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(f, maxKnowledgeFile+1))
		if err != nil || len(raw) > maxKnowledgeFile {
			fail("Fail terlalu besar (maksimum 1 MB).")
			return
		}
		content = string(raw)
		v.Text = ""
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		fail("Kandungan mesti teks UTF-8 biasa.")
		return
	}
	if _, err := s.addKnowledge(v.Bot, v.Title, content); err != nil {
		if strings.TrimSpace(content) == "" {
			fail("Kandungan kosong: tampal teks atau pilih fail.")
			return
		}
		log.Printf("admin knowledge add: %v", err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, knowledgePath, http.StatusSeeOther)
}
