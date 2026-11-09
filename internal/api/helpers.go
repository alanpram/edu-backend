package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
)

type validation struct {
	details []map[string]string
}

func (v *validation) add(field, message string) {
	v.details = append(v.details, map[string]string{"field": field, "message": message})
}

func (v *validation) err() error {
	if len(v.details) == 0 {
		return nil
	}
	return httpx.Unprocessable("Validasi request gagal", "VALIDATION_ERROR", v.details)
}

func (v *validation) stringField(body map[string]any, key, label string, min, max int, required bool) string {
	value, _ := body[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			v.add(key, label+" wajib diisi")
		}
		return ""
	}
	if len([]rune(value)) < min {
		v.add(key, fmt.Sprintf("%s minimal %d karakter", label, min))
	}
	if max > 0 && len([]rune(value)) > max {
		v.add(key, fmt.Sprintf("%s maksimal %d karakter", label, max))
	}
	return value
}

func (v *validation) email(body map[string]any, key string) string {
	value := v.stringField(body, key, "Email", 3, 255, true)
	if value == "" {
		return ""
	}
	value = strings.ToLower(value)
	if !emailPattern.MatchString(value) {
		v.add(key, "Format email tidak valid")
	}
	return value
}

func (v *validation) password(body map[string]any, key, label string) string {
	value, _ := body[key].(string)
	if value == "" {
		v.add(key, label+" wajib diisi")
		return ""
	}
	if len(value) < 8 {
		v.add(key, label+" minimal 8 karakter")
	}
	if len(value) > 72 {
		v.add(key, label+" maksimal 72 karakter")
	}
	return value
}

func (v *validation) phone(body map[string]any, key string, required bool) string {
	raw := v.stringField(body, key, "Nomor HP", 9, 20, required)
	if raw == "" {
		return ""
	}
	normalized := normalizePhone(raw)
	if !phonePattern.MatchString(normalized) {
		v.add(key, "Gunakan nomor HP Indonesia yang valid, mis. 081234567890")
	}
	return normalized
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
var phonePattern = regexp.MustCompile(`^\+628\d{8,11}$`)

func normalizePhone(input string) string {
	var digits strings.Builder
	for _, char := range input {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
		}
	}
	value := digits.String()
	switch {
	case strings.HasPrefix(value, "62"):
		return "+" + value
	case strings.HasPrefix(value, "0"):
		return "+62" + value[1:]
	case strings.HasPrefix(value, "8"):
		return "+62" + value
	default:
		return "+" + value
	}
}

func isRole(value string) bool {
	switch value {
	case "SUPER_ADMIN", "INSTRUCTOR", "ASESOR", "STUDENT":
		return true
	}
	return false
}

func jsonValue(value any) any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func rawJSON(value string) json.RawMessage {
	if value == "" {
		return nil
	}
	return json.RawMessage(value)
}

const publicUserColumns = `user_id, user_nama, user_email, user_phone, user_role,
	email_verified_at, is_active, last_login_at, created_at, updated_at`

func scanUser(row pgx.Row) (auth.User, error) {
	var user auth.User
	err := row.Scan(
		&user.UserID, &user.UserNama, &user.UserEmail, &user.UserPhone, &user.UserRole,
		&user.EmailVerifiedAt, &user.IsActive, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

func requireBody(r *http.Request) (map[string]any, error) {
	var body map[string]any
	if err := httpx.Decode(r, &body, 2<<20); err != nil {
		return nil, err
	}
	if body == nil {
		return map[string]any{}, nil
	}
	return body, nil
}

func bodyString(body map[string]any, key string) string {
	return httpx.BodyString(body, key)
}

func bodyHasKey(body map[string]any, key string) bool {
	_, exists := body[key]
	return exists
}

func bodyInt(body map[string]any, key string, fallback int) int {
	return httpx.BodyInt(body, key, fallback)
}

func bodyFloat(body map[string]any, key string, fallback float64) float64 {
	return httpx.BodyFloat(body, key, fallback)
}

func bodyBool(body map[string]any, key string, fallback bool) bool {
	return httpx.BodyBool(body, key, fallback)
}

func bodyStringPtr(body map[string]any, key string) *string {
	if value, exists := body[key]; exists {
		if value == nil {
			return nil
		}
		if text, ok := value.(string); ok {
			text = strings.TrimSpace(text)
			if text == "" {
				return nil
			}
			return &text
		}
	}
	return nil
}

func bodyFloatPtr(body map[string]any, key string) *float64 {
	if value, exists := body[key]; exists && value != nil {
		number := httpx.BodyFloat(body, key, 0)
		return &number
	}
	return nil
}

func bodyIntPtr(body map[string]any, key string) *int {
	if value, exists := body[key]; exists && value != nil {
		number := httpx.BodyInt(body, key, 0)
		return &number
	}
	return nil
}

func bodyMap(body map[string]any, key string) map[string]any {
	if value, ok := body[key].(map[string]any); ok {
		return value
	}
	return nil
}

func bodySlice(body map[string]any, key string) []any {
	if value, ok := body[key].([]any); ok {
		return value
	}
	return nil
}

func bodyStringSlice(body map[string]any, key string) []string {
	values := bodySlice(body, key)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

// withExtras menambahkan beberapa field tambahan pada envelope response.
func withExtras(values map[string]any) httpx.Option {
	return func(payload map[string]any, _ *int) {
		for key, value := range values {
			payload[key] = value
		}
	}
}
