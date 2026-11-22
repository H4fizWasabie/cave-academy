package site

import (
	"bytes"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
)

// Brand colours (base.css): navy #0f1a3c, gold #f5c518, dark gold text #8a6100.
var (
	pdfNavy     = [3]int{15, 26, 60}
	pdfGold     = [3]int{245, 197, 24}
	pdfGoldText = [3]int{138, 97, 0}
)

// trainerPDF is GET /trainer/{slug}/pdf: the profile as one A4 page, under the
// same visibility rules as the profile page.
func (s *Server) trainerPDF(w http.ResponseWriter, r *http.Request) {
	p, ok := s.profileBySlug(r.PathValue("slug"))
	if !ok || !canSee(p, s.currentUser(r)) {
		s.notFound(w, r)
		return
	}
	var buf bytes.Buffer
	if err := s.renderProfilePDF(&buf, p); err != nil {
		log.Printf("trainer pdf %s: %v", p.Slug, err)
		http.Error(w, "Ralat dalaman", http.StatusInternalServerError)
		return
	}
	if !p.Live() {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+p.Slug+`.pdf"`) // slug is [a-z0-9-] only
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	buf.WriteTo(w)
}

// renderProfilePDF draws the profile with fpdf's built-in Helvetica. Core fonts
// are cp1252, so every string goes through the translator: Latin text (Malay
// included) is kept and other characters become "?".
func (s *Server) renderProfilePDF(out *bytes.Buffer, p trainerProfile) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.SetAutoPageBreak(false, 0) // one page only: the bio is trimmed to fit instead
	pdf.SetMargins(18, 18, 18)
	pdf.SetTitle(tr(p.DisplayName+" | The Cave Academy"), false)
	pdf.SetAuthor("The Cave Academy", false)
	pdf.AddPage()
	const left, width = 18.0, 174.0
	color := func(c [3]int) { pdf.SetTextColor(c[0], c[1], c[2]) }

	// Header band.
	pdf.SetFillColor(pdfNavy[0], pdfNavy[1], pdfNavy[2])
	pdf.Rect(0, 0, 210, 70, "F")
	pdf.SetFillColor(pdfGold[0], pdfGold[1], pdfGold[2])
	pdf.Rect(0, 70, 210, 2.5, "F")
	pdf.SetFont("Helvetica", "B", 11)
	color(pdfGold)
	pdf.Text(left, 15, "THE CAVE ACADEMY")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetTextColor(255, 255, 255)
	pdf.Text(left, 19.5, "EMPOWER MINDS. SHAPE LEADERS.")

	pdf.Circle(left+12, 44, 12, "F")
	pdf.SetFont("Helvetica", "B", 16)
	color(pdfNavy)
	pdf.SetXY(left, 40.5)
	pdf.CellFormat(24, 7, tr(p.Initials()), "", 0, "C", false, 0, "")

	nameSize := 20.0
	switch n := len([]rune(p.DisplayName)); {
	case n > 50:
		nameSize = 12
	case n > 28:
		nameSize = 16
	}
	pdf.SetXY(48, 28)
	pdf.SetFont("Helvetica", "B", nameSize)
	pdf.SetTextColor(255, 255, 255)
	pdf.MultiCell(144, nameSize*0.5, tr(p.DisplayName), "", "L", false)
	if p.Headline != "" {
		pdf.SetX(48)
		pdf.SetFont("Helvetica", "", 11)
		color(pdfGold)
		pdf.MultiCell(144, 5.5, tr(p.Headline), "", "L", false)
	}

	// Body sections.
	y := 84.0
	heading := func(t string) {
		pdf.SetXY(left, y)
		pdf.SetFont("Helvetica", "B", 9)
		color(pdfGoldText)
		pdf.SetDrawColor(pdfGold[0], pdfGold[1], pdfGold[2])
		pdf.CellFormat(width, 5, t, "B", 1, "L", false, 0, "")
		y += 8
	}
	line := func(t string) {
		pdf.SetXY(left, y)
		pdf.SetFont("Helvetica", "", 10.5)
		color(pdfNavy)
		pdf.MultiCell(width, 5.5, tr(t), "", "L", false)
		y = pdf.GetY() + 1
	}
	heading("KEPAKARAN")
	line(strings.Join(p.Tags(), ", "))
	y += 4
	heading("PENGALAMAN")
	line(strconv.Itoa(p.Years) + " tahun")
	y += 4

	var contact []string
	if p.Phone != "" {
		contact = append(contact, "Telefon: "+p.Phone)
	}
	if p.Location != "" {
		contact = append(contact, "Lokasi: "+p.Location)
	}
	if p.LinkedIn != "" {
		contact = append(contact, "LinkedIn: "+p.LinkedIn)
	}
	if p.Live() {
		contact = append(contact, "Profil: "+s.baseURL()+"/trainer/"+p.Slug)
	}
	if len(contact) > 0 {
		heading("HUBUNGI")
		for _, c := range contact {
			line(c)
		}
		y += 4
	}

	if p.Bio != "" {
		heading("TENTANG")
		pdf.SetFont("Helvetica", "", 10.5)
		lines := pdf.SplitText(tr(p.Bio), width)
		if max := int((278 - y) / 5.5); len(lines) > max && max > 0 {
			lines = append(lines[:max-1], "...")
		}
		line(strings.Join(lines, "\n"))
	}

	// Footer.
	pdf.SetFillColor(pdfNavy[0], pdfNavy[1], pdfNavy[2])
	pdf.Rect(0, 285, 210, 12, "F")
	pdf.SetFont("Helvetica", "", 8)
	color(pdfGold)
	pdf.SetXY(left, 289)
	pdf.CellFormat(width, 4, "The Cave Academy  |  "+strings.TrimPrefix(strings.TrimPrefix(s.baseURL(), "https://"), "http://"), "", 0, "L", false, 0, "")
	return pdf.Output(out)
}
