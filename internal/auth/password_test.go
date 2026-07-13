package auth

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestHashPassword_ProducesPHCFormat(t *testing.T) {
	got, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashPassword returned error: %v", err)
	}

	// hardcoded prefix to force testing expected values. Will need to be
	// adjusted if constants changed.
	phcPrefix := "$argon2id$v=19$m=65536,t=3,p=4"
	if !strings.HasPrefix(got, phcPrefix) {
		t.Errorf("hashPassword did not add PHC prefix. Received: %s Expecting prefix: %s", got, phcPrefix)
	}

	phcFields := len(strings.Split(got, "$")) - 1 // first str is empty
	if phcFields != 5 {
		t.Errorf("PHC string should have 5 fields, delimited by '$'. Received: %v.\nString: %s", phcFields, got)
	}
}

func TestHashPassword_IsNotPlaintext(t *testing.T) {
	pw := "plaintext password"
	got, err := hashPassword(pw)
	if err != nil {
		t.Fatalf("hashPassword returned error: %v", err)
	}

	if pw == got {
		t.Errorf("hashPassword returned plaintext password. Input: %s Output: %s", pw, got)
	}
}

func TestHashPassword_SaltIsRandomPerCall(t *testing.T) {
	pw := "same password twice"
	a, err := hashPassword(pw)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	b, err := hashPassword(pw)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if a == b {
		t.Error("hashing the same password twice gave identical output; salt is not random")
	}
}

func TestVerifyPassword_VerifiesPassword(t *testing.T) {
	plain := "correct horse battery staple"
	phc, err := hashPassword(plain)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	verified, err := verifyPassword(plain, phc)
	if err != nil {
		t.Fatalf("failed to verify password: %v", err)
	}
	if !verified {
		t.Errorf("same password not verified")
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	plain := "correct horse battery staple"
	phc, err := hashPassword(plain)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	wrongPw := "this is the wrong password"

	verified, err := verifyPassword(wrongPw, phc)
	if err != nil {
		t.Fatalf("failed to verify password: %v", err)
	}
	if verified {
		t.Errorf("the wrong password was verified")
	}
}

func TestVerifyPassword_RejectsBadPHC(t *testing.T) {
	const plain = "correct horse battery staple"

	tests := map[string]struct {
		mutate  func(t *testing.T, parts []string) []string
		wantErr bool
	}{
		"tampered hash": {
			mutate: func(t *testing.T, parts []string) []string {
				tampered, err := incBase64(parts[5])
				if err != nil {
					t.Fatalf("incBase64: %v", err) // setup failure, not a verify result
				}
				parts[5] = tampered
				return parts
			},
			wantErr: false, // still valid base64 -> decodes, just mismatches -> (false, nil)
		},
		"malformed segments": {
			mutate: func(t *testing.T, parts []string) []string {
				return slices.Delete(parts, 4, 5)
			},
			wantErr: true,
		},
		"tampered parameters": {
			mutate: func(t *testing.T, parts []string) []string {
				newMem := memory + 1
				newIter := iterations + 1
				newThr := threads - 1
				parts[3] = fmt.Sprintf("m=%d,t=%d,p=%d", newMem, newIter, newThr)
				return parts
			},
			wantErr: false,
		},
		"params exceed bounds": {
			mutate: func(t *testing.T, parts []string) []string {
				newMem := upperMem + 1
				newIter := upperIter + 1
				newThr := upperThr + 1
				parts[3] = fmt.Sprintf("m=%d,t=%d,p=%d", newMem, newIter, newThr)
				return parts
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			parts := mustPHCParts(t, plain) // fresh per subtest
			phc := strings.Join(tc.mutate(t, parts), "$")

			verified, err := verifyPassword(plain, phc)

			if tc.wantErr && err == nil {
				t.Errorf("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if verified {
				t.Errorf("verifyPassword accepted a bad PHC string")
			}
		})
	}
}

func mustPHCParts(t *testing.T, plain string) []string {
	t.Helper() // <- reports the caller's line number on failure, not this helper's
	phc, err := hashPassword(plain)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	// "$argon2id$v=19$m=..,t=..,p=..$salt$hash" -> ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	parts := strings.Split(phc, "$")
	if len(parts) != 6 {
		t.Fatalf("expected 6 PHC segments, got %d", len(parts))
	}
	return parts
}

func incBase64(b64 string) (string, error) {
	data, err := base64.RawStdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}

	for i := range data {
		data[i]++ // wraps naturally on uint8
	}

	return base64.RawStdEncoding.EncodeToString(data), nil
}
