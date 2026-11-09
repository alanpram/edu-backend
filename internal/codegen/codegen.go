// Package codegen membuat kode akses/acak yang aman untuk fitur bisnis.
package codegen

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randomBlock(length int) string {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	output := make([]byte, length)
	for index, value := range buf {
		output[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(output)
}

func OrganizationCode() string { return "ORG-" + randomBlock(6) }
func PackageCode() string      { return "PKG-" + randomBlock(6) }
func ParticipantCode() string  { return "PSN-" + randomBlock(8) }

const certificateAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// CertificateCode mengembalikan kode opaque EDU-<12 karakter CSPRNG>.
func CertificateCode() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	output := make([]byte, 12)
	for index, value := range buf {
		output[index] = certificateAlphabet[int(value)%len(certificateAlphabet)]
	}
	return "EDU-" + string(output)
}

// RandomToken membuat token acak 32 byte dalam bentuk hex.
func RandomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

// HashToken menghasilkan SHA-256 hex dari token (kompatibel dengan Node).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NormalizeAccessCode(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	return strings.ToUpper(strings.Join(fields, ""))
}

func Slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastDash = false
		} else if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
		if builder.Len() >= 60 {
			break
		}
	}
	return strings.Trim(builder.String(), "-")
}
