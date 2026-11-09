// Package httpx berisi helper HTTP, format respons, dan error aplikasi.
package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Error adalah error aplikasi yang aman dikirim ke klien.
type Error struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *Error) Error() string { return e.Message }

func NewError(status int, message, code string, details any) *Error {
	return &Error{Status: status, Code: code, Message: message, Details: details}
}

func BadRequest(message string, code ...string) *Error {
	return NewError(http.StatusBadRequest, message, orDefault(code, "BAD_REQUEST"), nil)
}

func Unauthorized(message string, code ...string) *Error {
	if message == "" {
		message = "Autentikasi diperlukan"
	}
	return NewError(http.StatusUnauthorized, message, orDefault(code, "UNAUTHORIZED"), nil)
}

func Forbidden(message string, code ...string) *Error {
	if message == "" {
		message = "Anda tidak memiliki akses"
	}
	return NewError(http.StatusForbidden, message, orDefault(code, "FORBIDDEN"), nil)
}

func NotFound(message string, code ...string) *Error {
	if message == "" {
		message = "Data tidak ditemukan"
	}
	return NewError(http.StatusNotFound, message, orDefault(code, "NOT_FOUND"), nil)
}

func Conflict(message string, code ...string) *Error {
	return NewError(http.StatusConflict, message, orDefault(code, "CONFLICT"), nil)
}

func Unprocessable(message string, code string, details any) *Error {
	return NewError(http.StatusUnprocessableEntity, message, code, details)
}

func TooManyRequests(message string) *Error {
	return NewError(http.StatusTooManyRequests, message, "RATE_LIMITED", nil)
}

func orDefault(values []string, fallback string) string {
	if len(values) > 0 && values[0] != "" {
		return values[0]
	}
	return fallback
}

// Handler adalah handler yang dapat mengembalikan error.
type Handler func(http.ResponseWriter, *http.Request) error

// Wrap mengubah Handler menjadi http.HandlerFunc dan memetakan error ke format API.
func Wrap(handler Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			WriteError(w, err)
		}
	}
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func Success(w http.ResponseWriter, data any, message string, opts ...Option) {
	if message == "" {
		message = "Berhasil"
	}
	payload := map[string]any{
		"success": true,
		"message": message,
		"data":    data,
	}
	status := http.StatusOK
	for _, opt := range opts {
		opt(payload, &status)
	}
	WriteJSON(w, status, payload)
}

func Created(w http.ResponseWriter, data any, message string) {
	if message == "" {
		message = "Data berhasil dibuat"
	}
	Success(w, data, message, WithStatus(http.StatusCreated))
}

type Option func(map[string]any, *int)

func WithStatus(status int) Option {
	return func(_ map[string]any, current *int) { *current = status }
}

func WithMeta(meta any) Option {
	return func(payload map[string]any, _ *int) { payload["meta"] = meta }
}

func WithExtra(key string, value any) Option {
	return func(payload map[string]any, _ *int) { payload[key] = value }
}

func WriteError(w http.ResponseWriter, err error) {
	var appErr *Error
	if !errors.As(err, &appErr) {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				appErr = Conflict("Data sudah digunakan", "DUPLICATE_DATA")
			case "23503":
				appErr = Conflict("Data masih direferensikan oleh data lain sehingga tidak dapat diubah/dihapus", "RELATION_CONSTRAINT")
			case "23502":
				appErr = BadRequest("Data wajib belum diisi", "VALIDATION_ERROR")
			default:
				appErr = NewError(http.StatusInternalServerError, "Terjadi kesalahan internal pada server", "INTERNAL_SERVER_ERROR", nil)
			}
		}
	}
	if appErr == nil {
		appErr = NewError(http.StatusInternalServerError, "Terjadi kesalahan internal pada server", "INTERNAL_SERVER_ERROR", nil)
	}
	body := map[string]any{
		"success": false,
		"message": appErr.Message,
		"error": map[string]any{
			"code": appErr.Code,
		},
	}
	if appErr.Details != nil {
		body["error"].(map[string]any)["details"] = appErr.Details
	}
	WriteJSON(w, appErr.Status, body)
}

// Decode membaca JSON body dengan batas ukuran yang ketat. Unknown fields
// tetap diperbolehkan agar kompatibel dengan payload frontend lama, tetapi
// body yang melebihi batas atau berisi lebih dari satu nilai JSON ditolak.
func Decode(r *http.Request, target any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	if r.Body == nil {
		return BadRequest("Body request wajib diisi", "INVALID_BODY")
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return BadRequest("Body request tidak dapat dibaca", "INVALID_BODY")
	}
	if int64(len(payload)) > maxBytes {
		return NewError(http.StatusRequestEntityTooLarge, "Body request terlalu besar", "PAYLOAD_TOO_LARGE", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return BadRequest("Body request wajib diisi", "INVALID_BODY")
		}
		return BadRequest("Body JSON tidak valid", "INVALID_JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return BadRequest("Body JSON tidak valid", "INVALID_JSON")
	}
	return nil
}

func Pagination(page, limit int) (int, int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit, (page - 1) * limit
}

func Meta(total, page, limit int) map[string]any {
	totalPages := 0
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}
	return map[string]any{
		"page":        page,
		"limit":       limit,
		"total":       total,
		"total_pages": totalPages,
	}
}

func QueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func QueryBoolPtr(r *http.Request, key string) *bool {
	raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	if raw == "" {
		return nil
	}
	value := raw == "true" || raw == "1"
	return &value
}

func QueryString(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

func IDParam(r *http.Request, key string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(pathParam(r, key)))
	if err != nil || value <= 0 {
		return 0, BadRequest("ID tidak valid", "INVALID_ID")
	}
	return value, nil
}

// pathParam diisi oleh router (lihat router.SetPathParam) agar paket httpx
// tidak bergantung langsung pada chi.
var pathParam = func(_ *http.Request, _ string) string { return "" }

func SetPathParam(fn func(*http.Request, string) string) { pathParam = fn }

func BodyString(body map[string]any, key string) string {
	if value, ok := body[key]; ok {
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func BodyInt(body map[string]any, key string, fallback int) int {
	switch value := body[key].(type) {
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	case int:
		return value
	case int64:
		return int(value)
	}
	return fallback
}

func BodyFloat(body map[string]any, key string, fallback float64) float64 {
	switch value := body[key].(type) {
	case float64:
		return value
	case json.Number:
		if parsed, err := value.Float64(); err == nil {
			return parsed
		}
	}
	return fallback
}

func BodyBool(body map[string]any, key string, fallback bool) bool {
	if value, ok := body[key].(bool); ok {
		return value
	}
	return fallback
}
