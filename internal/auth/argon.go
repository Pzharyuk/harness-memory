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
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
	argonVersion = argon2.Version // 19
)

// Hash returns an argon2id encoding:
// argon2id$v=19$m=65536,t=1,p=4$<salt>$<key>
func Hash(plaintext string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(plaintext), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf(
		"argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion, argonMemory, argonTime, argonThreads,
		enc.EncodeToString(salt),
		enc.EncodeToString(key),
	), nil
}

// Verify checks plaintext against an argon2id hash from Hash.
// Comparison of the derived key is constant-time.
func Verify(hash, plaintext string) bool {
	salt, want, time, memory, threads, keyLen, ok := parseArgonHash(hash)
	if !ok {
		return false
	}
	got := argon2.IDKey([]byte(plaintext), salt, time, memory, threads, keyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}

func parseArgonHash(hash string) (salt, key []byte, time, memory uint32, threads uint8, keyLen uint32, ok bool) {
	// argon2id$v=19$m=65536,t=1,p=4$<salt>$<key>
	parts := strings.Split(hash, "$")
	if len(parts) != 5 {
		return
	}
	if parts[0] != "argon2id" {
		return
	}
	var version int
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil || version != argonVersion {
		return
	}
	var m, t, p int
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return
	}
	if m <= 0 || t <= 0 || p <= 0 {
		return
	}
	enc := base64.RawStdEncoding
	var err error
	salt, err = enc.DecodeString(parts[3])
	if err != nil || len(salt) == 0 {
		return
	}
	key, err = enc.DecodeString(parts[4])
	if err != nil || len(key) == 0 {
		return
	}
	return salt, key, uint32(t), uint32(m), uint8(p), uint32(len(key)), true
}
