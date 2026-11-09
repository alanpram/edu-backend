package auth

import "golang.org/x/crypto/bcrypt"

func HashPassword(plain string, cost int) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	return string(hash), err
}

func VerifyPassword(plain, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
