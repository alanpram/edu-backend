package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/edumaster/backend-go/internal/httpx"
)

const (
	Issuer   = "edumaster-api"
	Audience = "edumaster-client"
)

type Claims struct {
	Sub  string
	Role string
	JTI  string
}

func Sign(secret string, expiresIn time.Duration, userID int, role string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":  itoa(userID),
		"role": role,
		"jti":  uuid.NewString(),
		"iat":  now.Unix(),
		"exp":  now.Add(expiresIn).Unix(),
		"aud":  Audience,
		"iss":  Issuer,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func Verify(secret, tokenString string) (*Claims, error) {
	parsed, err := jwt.Parse(
		tokenString,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		},
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil || !parsed.Valid {
		return nil, httpx.Unauthorized("Token tidak valid atau sudah kedaluwarsa", "INVALID_TOKEN")
	}
	mapClaims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, httpx.Unauthorized("Token tidak valid", "INVALID_TOKEN")
	}
	sub, _ := mapClaims["sub"].(string)
	jti, _ := mapClaims["jti"].(string)
	role, _ := mapClaims["role"].(string)
	if sub == "" || jti == "" {
		return nil, httpx.Unauthorized("Token tidak valid", "INVALID_TOKEN")
	}
	return &Claims{Sub: sub, Role: role, JTI: jti}, nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func ExtractBearer(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", httpx.Unauthorized("Header Authorization tidak ditemukan", "MISSING_TOKEN")
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || parts[1] == "" {
		return "", httpx.Unauthorized("Format Authorization harus: Bearer <token>", "MALFORMED_TOKEN")
	}
	return parts[1], nil
}

func RandomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	negative := value < 0
	if negative {
		value = -value
	}
	for value > 0 {
		pos--
		digits[pos] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		pos--
		digits[pos] = '-'
	}
	return string(digits[pos:])
}
