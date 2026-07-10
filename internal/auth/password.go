package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	// Hash constants
	memory     = 64 * 1024
	iterations = 3
	threads    = 4
	keyLen     = 32
	saltLen    = 16
)

func hashPassword(plain string) (string, error) {
	salt := make([]byte, saltLen)
	_, err := rand.Read(salt)
	if err != nil {
		// Theoretically impossible, but the alt. is to return invalid hash
		return "", fmt.Errorf("generating random salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plain), salt, iterations, memory, threads, keyLen)

	// Encode to base64 string w/o padding as per PHC string format
	encSalt := base64.RawStdEncoding.EncodeToString(salt)
	encHash := base64.RawStdEncoding.EncodeToString(hash)

	phcString := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memory, iterations, threads, encSalt, encHash)

	return phcString, nil
}
