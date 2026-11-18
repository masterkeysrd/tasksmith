package hash

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256Hex computes the lowercase 64-character hex SHA-256 digest of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA256String computes the lowercase 64-character hex SHA-256 digest of s.
func SHA256String(s string) string {
	return SHA256Hex([]byte(s))
}

// IsValidSHA256 reports whether s is a canonical lowercase 64-character hex SHA-256 digest.
func IsValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
