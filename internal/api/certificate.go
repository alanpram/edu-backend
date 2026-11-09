package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/edumaster/backend-go/internal/httpx"
)

var opaqueCertificatePattern = regexp.MustCompile(`^EDU-[A-HJ-NP-Z2-9]{12}$`)
var legacyCertificatePattern = regexp.MustCompile(`^EDU-(\d+)-(\d+)-(\d+)$`)

type certificateData struct {
	SessionID     int
	AttemptNumber int
	FinalScore    *float64
	SubmittedAt   *time.Time
	CertificateJP int
	PaketID       int
	PaketNama     string
	PaketTipe     string
	UserNama      string
	AnswerCount   int
}

func (s *Server) registerCertificates(api chi.Router) {
	api.With(s.certLimit.Middleware).Get("/certificates/{kode}", httpx.Wrap(s.certificateVerify))
	api.With(s.certLimit.Middleware).Get("/certificates/{kode}/pdf", httpx.Wrap(s.certificatePDF))
}

func (s *Server) findCertificate(ctx context.Context, code string) (certificateData, error) {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	var item certificateData
	var paketJP int
	if opaqueCertificatePattern.MatchString(normalized) {
		err := s.Pool.QueryRow(ctx, `
			SELECT s.session_id, s.attempt_number, s.final_score, s.submitted_at,
			       COALESCE(s.certificate_jp, p.paket_jp, 8), p.paket_id, p.paket_nama, p.paket_tipe,
			       u.user_nama, (SELECT count(*) FROM exam_answer a WHERE a.session_id = s.session_id)
			FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id
			JOIN "user" u ON u.user_id = s.user_id
			WHERE s.certificate_code = $1 AND s.status = 'completed' AND s.is_passed = true`, normalized).
			Scan(&item.SessionID, &item.AttemptNumber, &item.FinalScore, &item.SubmittedAt, &item.CertificateJP,
				&item.PaketID, &item.PaketNama, &item.PaketTipe, &item.UserNama, &item.AnswerCount)
		if errors.Is(err, pgx.ErrNoRows) {
			return item, httpx.NotFound("Sertifikat tidak ditemukan atau belum diterbitkan", "CERTIFICATE_NOT_FOUND")
		}
		if err != nil {
			return item, err
		}
	} else {
		match := legacyCertificatePattern.FindStringSubmatch(normalized)
		if len(match) != 4 {
			return item, httpx.NotFound("Sertifikat tidak ditemukan", "CERTIFICATE_NOT_FOUND")
		}
		paketID, _ := strconv.Atoi(match[1])
		sessionID, _ := strconv.Atoi(match[2])
		attempt, _ := strconv.Atoi(match[3])
		err := s.Pool.QueryRow(ctx, `
			SELECT s.session_id, s.attempt_number, s.final_score, s.submitted_at,
			       COALESCE(s.certificate_jp, p.paket_jp, 8), p.paket_id, p.paket_nama, p.paket_tipe,
			       u.user_nama, (SELECT count(*) FROM exam_answer a WHERE a.session_id = s.session_id)
			FROM exam_session s JOIN paket_ujian p ON p.paket_id = s.paket_id
			JOIN "user" u ON u.user_id = s.user_id
			WHERE s.session_id = $1 AND s.paket_id = $2 AND s.attempt_number = $3
			  AND s.status = 'completed' AND s.is_passed = true`, sessionID, paketID, attempt).
			Scan(&item.SessionID, &item.AttemptNumber, &item.FinalScore, &item.SubmittedAt, &item.CertificateJP,
				&item.PaketID, &item.PaketNama, &item.PaketTipe, &item.UserNama, &item.AnswerCount)
		if errors.Is(err, pgx.ErrNoRows) {
			return item, httpx.NotFound("Sertifikat tidak ditemukan atau belum diterbitkan", "CERTIFICATE_NOT_FOUND")
		}
		if err != nil {
			return item, err
		}
	}
	_ = paketJP
	if item.CertificateJP == 0 {
		item.CertificateJP = 8
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE exam_session SET certificate_jp = $1 WHERE session_id = $2 AND certificate_jp IS NULL`,
		item.CertificateJP, item.SessionID)
	return item, nil
}

func (s *Server) certificateVerify(w http.ResponseWriter, r *http.Request) error {
	code := strings.TrimSpace(chi.URLParam(r, "kode"))
	item, err := s.findCertificate(r.Context(), code)
	if err != nil {
		return err
	}
	kode := strings.ToUpper(code)
	if item.SubmittedAt != nil {
		// Keep the exact opaque code for newer certificates.
		if opaqueCertificatePattern.MatchString(kode) {
			kode = strings.ToUpper(strings.TrimSpace(code))
		}
	}
	httpx.Success(w, map[string]any{
		"valid": true,
		"kode":  kode,
		"sertifikat": map[string]any{
			"kode": kode, "session_id": item.SessionID, "attempt_number": item.AttemptNumber,
			"nama": item.UserNama, "paket": item.PaketNama, "paket_tipe": item.PaketTipe,
			"skor": item.FinalScore, "jp": item.CertificateJP, "diterbitkan_at": item.SubmittedAt,
		},
	}, "")
	return nil
}

func (s *Server) certificatePDF(w http.ResponseWriter, r *http.Request) error {
	code := strings.TrimSpace(chi.URLParam(r, "kode"))
	item, err := s.findCertificate(r.Context(), code)
	if err != nil {
		return err
	}
	issued := time.Now()
	if item.SubmittedAt != nil {
		issued = *item.SubmittedAt
	}
	normalized := strings.ToUpper(code)
	pdfBytes, err := s.renderCertificatePDF(item, normalized, issued)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="Sertifikat-%s.pdf"`, normalized))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
	return nil
}

func certificateDate(value time.Time) string {
	months := []string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	jakarta := value.In(time.FixedZone("WIB", 7*60*60))
	return fmt.Sprintf("%d %s %d", jakarta.Day(), months[int(jakarta.Month())-1], jakarta.Year())
}

func (s *Server) renderCertificatePDF(item certificateData, code string, issued time.Time) ([]byte, error) {
	verifyURL := s.Config.CertificateVerifyBase + "/verify/" + code
	qrPNG, err := qrcode.Encode(verifyURL, qrcode.Medium, 360)
	if err != nil {
		return nil, err
	}

	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	// Background and borders.
	pdf.SetFillColor(255, 253, 247)
	pdf.Rect(0, 0, 297, 210, "F")
	pdf.SetDrawColor(11, 31, 58)
	pdf.SetLineWidth(0.8)
	pdf.Rect(14, 12, 269, 186, "D")
	pdf.SetDrawColor(180, 138, 44)
	pdf.SetLineWidth(0.3)
	pdf.Rect(20, 18, 257, 174, "D")

	// Header.
	pdf.SetFillColor(37, 99, 235)
	pdf.RoundedRect(32, 26, 46, 46, 5, "1234", "F")
	pdf.SetFont("Helvetica", "B", 24)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(32, 37)
	pdf.CellFormat(46, 12, "E", "", 0, "C", false, 0, "")
	pdf.SetTextColor(11, 31, 58)
	pdf.SetFont("Helvetica", "B", 19)
	pdf.SetXY(88, 30)
	pdf.CellFormat(90, 9, "EduMaster", "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(88, 43)
	pdf.CellFormat(120, 5, "PORTAL EDUKASI & SERTIFIKASI DIGITAL", "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "B", 6.5)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(177, 30)
	pdf.CellFormat(80, 4, "CERTIFICATE ID", "", 0, "R", false, 0, "")
	pdf.SetFont("Courier", "B", 9)
	pdf.SetTextColor(11, 31, 58)
	pdf.SetXY(167, 37)
	pdf.CellFormat(90, 5, code, "", 0, "R", false, 0, "")
	pdf.SetDrawColor(220, 227, 236)
	pdf.SetLineWidth(0.25)
	pdf.Line(32, 78, 265, 78)

	// Main title.
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetTextColor(180, 138, 44)
	pdf.SetXY(0, 85)
	pdf.CellFormat(297, 4, "SERTIFIKAT PENYELESAIAN", "", 0, "C", false, 0, "")
	pdf.SetFont("Times", "B", 22)
	pdf.SetTextColor(11, 31, 58)
	pdf.SetXY(0, 94)
	pdf.CellFormat(297, 10, "Certificate of Completion", "", 0, "C", false, 0, "")

	pdf.SetFont("Helvetica", "", 8.5)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(0, 116)
	pdf.CellFormat(297, 4, "Diberikan dengan bangga kepada", "", 0, "C", false, 0, "")
	pdf.SetFont("Times", "B", 22)
	pdf.SetTextColor(11, 31, 58)
	pdf.SetXY(0, 123)
	pdf.CellFormat(297, 11, tr(item.UserNama), "", 0, "C", false, 0, "")

	pdf.SetDrawColor(180, 138, 44)
	pdf.SetLineWidth(0.3)
	pdf.Line(82, 139, 215, 139)

	// Details.
	pdf.SetFont("Helvetica", "", 7.5)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(0, 143)
	pdf.CellFormat(297, 4, "atas keberhasilan menyelesaikan program pelatihan", "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "B", 12.5)
	pdf.SetTextColor(11, 31, 58)
	pdf.SetXY(60, 149)
	pdf.CellFormat(177, 7, tr(item.PaketNama), "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "B", 8.5)
	pdf.SetTextColor(37, 99, 235)
	pdf.SetXY(0, 159)
	pdf.CellFormat(297, 4.5, fmt.Sprintf("Total %d Jam Pelajaran (JP)", item.CertificateJP), "", 0, "C", false, 0, "")
	if item.FinalScore != nil {
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(100, 116, 139)
		pdf.SetXY(0, 165)
		pdf.CellFormat(297, 4, tr(fmt.Sprintf("Skor akhir: %.1f / 100", *item.FinalScore)), "", 0, "C", false, 0, "")
	}

	// Signature.
	pdf.SetFont("Helvetica", "B", 6)
	pdf.SetTextColor(11, 31, 58)
	pdf.SetXY(30, 168)
	pdf.CellFormat(72, 3.5, "DITERBITKAN PADA", "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(30, 172)
	pdf.CellFormat(72, 3.5, certificateDate(issued), "", 0, "C", false, 0, "")
	pdf.SetFont("Times", "I", 13)
	pdf.SetTextColor(11, 31, 58)
	pdf.SetXY(30, 177)
	pdf.CellFormat(72, 6, tr(s.Config.CertificateIssuerName), "", 0, "C", false, 0, "")
	pdf.SetDrawColor(203, 213, 225)
	pdf.SetLineWidth(0.25)
	pdf.Line(30, 185, 102, 185)
	pdf.SetFont("Helvetica", "B", 6.5)
	pdf.SetXY(30, 186)
	pdf.CellFormat(72, 3.5, "EduMaster Academy", "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 5.5)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(30, 190)
	pdf.CellFormat(72, 3.5, tr(s.Config.CertificateIssuerTitle), "", 0, "C", false, 0, "")

	// QR verification box.
	pdf.SetFillColor(255, 255, 255)
	pdf.SetDrawColor(226, 232, 240)
	pdf.SetLineWidth(0.2)
	pdf.RoundedRect(210, 161, 55, 38, 3, "1234", "DF")
	pdf.RegisterImageOptionsReader("qr", fpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}, bytes.NewReader(qrPNG))
	pdf.ImageOptions("qr", 222, 164, 26, 26, false, fpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}, 0, "")
	pdf.SetFont("Helvetica", "B", 5)
	pdf.SetTextColor(11, 31, 58)
	pdf.SetXY(210, 191)
	pdf.CellFormat(55, 2.5, "PINDAI UNTUK VERIFIKASI", "", 0, "C", false, 0, "")
	pdf.SetFont("Courier", "B", 5)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(210, 194)
	pdf.CellFormat(55, 2.5, code, "", 0, "C", false, 0, "")

	pdf.SetFont("Helvetica", "", 5)
	pdf.SetTextColor(100, 116, 139)
	pdf.SetXY(0, 202)
	pdf.CellFormat(297, 3, tr("Dokumen resmi EduMaster  |  "+code+"  |  "+verifyURL), "", 0, "C", false, 0, "")

	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
