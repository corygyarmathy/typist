package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// Hash + salt constants
	memory     = 64 * 1024 // 64 MB
	iterations = 3         // time
	threads    = 4         // parallelisation
	keyLen     = 32
	saltLen    = 16

	// Upper limits (beyond this will error)
	upperMem  = 256 * 1024 // 256 MB
	upperIter = 5
	upperThr  = 8
)

// hashPassword hashes and salts plan using argon2id and the constants defined
// above. It constructs and returns a PHC string. Errors are theoretically
// impossible, but handled rather than returning an improper hash.
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

// verifyPassword reports whether plain, when hashed with the parameters and
// salt encoded in phc, matches the hash encoded in phc. A non-matching password
// returns (false, nil); only a malformed phc string returns an error.
func verifyPassword(plain, phc string) (bool, error) {
	parts := strings.Split(phc, "$")
	// "$argon2id$v=19$m=..,t=..,p=..$salt$hash" -> ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 {
		return false, fmt.Errorf(
			"invalid PHC string: expected 6 segments, got %d",
			len(parts),
		)
	}

	if parts[1] != "argon2id" {
		return false, fmt.Errorf(
			"invalid algorithm: expected 'argon2id', got %s",
			parts[1],
		)
	}

	derVer := argon2.Version
	n, err := fmt.Sscanf(parts[2], "v=%d", &derVer)
	if err != nil {
		return false, fmt.Errorf("scanning string '%s': %w", parts[2], err)
	}
	if n != 1 || derVer != argon2.Version {
		return false, fmt.Errorf(
			"incorrect argon2 version: expected 'v=%d', got '%s'",
			argon2.Version, parts[2],
		)
	}

	derMem := memory
	derIter := iterations
	derThr := threads
	n, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &derMem, &derIter, &derThr)
	if err != nil {
		return false, fmt.Errorf("scanning string '%s': %w", parts[3], err)
	}
	if n != 3 {
		return false, fmt.Errorf(
			"incorrect argon2 parameters: expected 'm=%d,t=%d,p=%d', got '%s'",
			memory, iterations, threads, parts[3],
		)
	}
	if derMem > upperMem || derIter > upperIter || derThr > upperThr {
		return false, fmt.Errorf(
			"given argon2 parameters exceed upper bounds: 'm=%d,t=%d,p=%d', got '%s'",
			upperMem, upperIter, upperThr, parts[3],
		)
	}

	encSalt := parts[4]
	encHash := parts[5]
	decSalt, err := base64.RawStdEncoding.DecodeString(encSalt)
	if err != nil {
		return false, fmt.Errorf("decoding salt: %w", err)
	}
	decHash, err := base64.RawStdEncoding.DecodeString(encHash)
	if err != nil {
		return false, fmt.Errorf("decoding hash: %w", err)
	}

	derKeyLen := uint32(len(decHash)) // verify stays correct even if a future hash used a different keyLen
	derHash := argon2.IDKey(
		[]byte(plain),
		decSalt,
		uint32(derIter),
		uint32(derMem),
		uint8(derThr),
		derKeyLen,
	)

	if subtle.ConstantTimeCompare(derHash, decHash) == 1 {
		return true, nil
	}
	return false, nil
}
