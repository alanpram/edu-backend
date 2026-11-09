package api

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
	"github.com/edumaster/backend-go/internal/middleware"
)

type MediaAsset struct {
	MediaID     int       `json:"media_id"`
	MediaURL    string    `json:"url"`
	MediaMime   string    `json:"mime"`
	MediaSize   int       `json:"size"`
	MediaWidth  *int      `json:"width"`
	MediaHeight *int      `json:"height"`
	MediaAlt    *string   `json:"alt"`
	UploadedBy  *int      `json:"uploaded_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Server) registerUploads(api chi.Router) {
	api.With(s.Auth.Authenticate, middleware.RequirePermission(s.Permissions, "admin.media.manage")).
		Post("/uploads/images", httpx.Wrap(s.uploadImage))
}

func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFrom(r.Context())
	maxBytes := s.Config.UploadMaxMB * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1*1024*1024))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return httpx.NewError(http.StatusRequestEntityTooLarge,
				fmt.Sprintf("Ukuran file maksimal %d MB.", s.Config.UploadMaxMB), "FILE_TOO_LARGE", nil)
		}
		return httpx.BadRequest("Request harus berupa multipart/form-data yang valid dengan field file.", "INVALID_UPLOAD")
	}
	alt := bodyString(map[string]any{"alt": r.FormValue("alt")}, "alt")
	if len([]rune(alt)) > 200 {
		return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", []map[string]string{{"field": "alt", "message": "Teks alternatif maksimal 200 karakter"}})
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("File gambar wajib diunggah pada field file.", "FILE_REQUIRED")
	}
	defer file.Close()
	if header.Size > maxBytes {
		return httpx.NewError(http.StatusRequestEntityTooLarge,
			fmt.Sprintf("Ukuran file maksimal %d MB.", s.Config.UploadMaxMB), "FILE_TOO_LARGE", nil)
	}
	if header.Size == 0 {
		return httpx.BadRequest("File gambar kosong.", "EMPTY_FILE")
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(payload)) > maxBytes {
		return httpx.NewError(http.StatusRequestEntityTooLarge,
			fmt.Sprintf("Ukuran file maksimal %d MB.", s.Config.UploadMaxMB), "FILE_TOO_LARGE", nil)
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return httpx.BadRequest("File gambar tidak valid atau rusak.", "INVALID_IMAGE")
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return httpx.BadRequest("Format gambar harus JPG, PNG, atau WebP.", "UNSUPPORTED_IMAGE_FORMAT")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return httpx.BadRequest("Dimensi gambar terlalu besar (maksimal 40 megapiksel).", "IMAGE_TOO_LARGE")
	}
	source, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		return httpx.BadRequest("File gambar tidak valid atau rusak.", "INVALID_IMAGE")
	}
	source = normalizeImage(source)
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return httpx.BadRequest("File gambar tidak valid atau rusak.", "INVALID_IMAGE")
	}
	targetWidth, targetHeight := width, height
	if width > 1920 || height > 1920 {
		if width >= height {
			targetWidth = 1920
			targetHeight = maxInt(1, int(float64(height)*1920/float64(width)))
		} else {
			targetHeight = 1920
			targetWidth = maxInt(1, int(float64(width)*1920/float64(height)))
		}
	}
	if targetWidth != width || targetHeight != height {
		target := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Over, nil)
		source = target
	}

	var output bytes.Buffer
	if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: 82}); err != nil {
		return err
	}

	now := time.Now()
	relativeDir := filepath.Join(fmt.Sprintf("%d", now.Year()), fmt.Sprintf("%02d", int(now.Month())))
	filename := uuid.NewString() + ".jpg"
	absoluteDir := filepath.Join(s.UploadDir, relativeDir)
	if err := os.MkdirAll(absoluteDir, 0o755); err != nil {
		return err
	}
	absolutePath := filepath.Join(absoluteDir, filename)
	if err := os.WriteFile(absolutePath, output.Bytes(), 0o644); err != nil {
		return err
	}

	relativePath := "/uploads/" + filepath.ToSlash(filepath.Join(relativeDir, filename))
	var mediaID int
	var widthPtr, heightPtr *int
	widthValue, heightValue := targetWidth, targetHeight
	widthPtr = &widthValue
	heightPtr = &heightValue
	var altPtr *string
	if alt != "" {
		altPtr = &alt
	}
	createdAt := time.Now()
	err = s.Pool.QueryRow(r.Context(), `
		INSERT INTO media_asset (media_url, media_path, media_mime, media_size, media_width, media_height,
			media_alt, uploaded_by, created_at)
		VALUES ($1,$2,'image/jpeg',$3,$4,$5,$6,$7,now())
		RETURNING media_id, created_at`,
		relativePath, relativePath, output.Len(), targetWidth, targetHeight, altPtr, principal.UserID).
		Scan(&mediaID, &createdAt)
	if err != nil {
		_ = os.Remove(absolutePath)
		return err
	}
	s.Activity.Record(r.Context(), principal.UserID, "MEDIA_UPLOAD", auth.RequestIP(r), map[string]any{
		"media_id": mediaID, "url": relativePath, "size": output.Len(),
	})
	httpx.Created(w, MediaAsset{
		MediaID: mediaID, MediaURL: relativePath, MediaMime: "image/jpeg", MediaSize: output.Len(),
		MediaWidth: widthPtr, MediaHeight: heightPtr, MediaAlt: altPtr, UploadedBy: &principal.UserID,
		CreatedAt: createdAt,
	}, "Gambar berhasil diunggah")
	return nil
}

// normalizeImage memastikan gambar dikonversi ke RGBA agar encoder JPEG tidak
// gagal pada gambar dengan kanal warna khusus (mis. palet atau alpha).
func normalizeImage(source image.Image) image.Image {
	if _, ok := source.(*image.RGBA); ok {
		return source
	}
	bounds := source.Bounds()
	target := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(target, target.Bounds(), source, bounds.Min, draw.Src)
	return target
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
