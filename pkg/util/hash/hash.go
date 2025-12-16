package hash

import (
	"crypto/sha256"
	"fmt"
)

// ComputeSHA256 computes the SHA256 hash of the given string
func ComputeSHA256(data string) string {
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)
}
