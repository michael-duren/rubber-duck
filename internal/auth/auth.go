// Package auth holds password hashing and token generation. Tokens are
// returned to the caller exactly once; only their SHA-256 hash is stored.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewSessionToken returns a random token and its storage hash.
func NewSessionToken() (token string, hash []byte) {
	return newToken("")
}

// UserTokenPrefix marks a human CLI token. It lives here next to
// NewUserToken — rather than as a duplicated literal in consumers like
// httpapi's requireUser — so the minted prefix and the dispatch check can
// never drift apart.
const UserTokenPrefix = "gc_u_"

// NewUserToken returns a user CLI token (UserTokenPrefix) and its storage hash.
func NewUserToken() (token string, hash []byte) {
	return newToken(UserTokenPrefix)
}

func newToken(prefix string) (string, []byte) {
	b := make([]byte, 20)
	rand.Read(b) // never fails; see crypto/rand docs
	t := prefix + hex.EncodeToString(b)
	return t, HashToken(t)
}

func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
