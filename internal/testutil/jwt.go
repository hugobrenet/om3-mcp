package testutil

import (
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// OpenIDToken represents an IdP credential whose opaque subject is distinct
// from the daemon username. The target cluster is not embedded in the token.
func OpenIDToken(t testing.TB, key *rsa.PrivateKey, issuer, audience, subject string, change func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{"iss": issuer, "aud": audience, "sub": subject, "exp": time.Now().Add(time.Hour).Unix(), "preferred_username": "alice", "email": "alice@example.test"}
	if change != nil {
		change(claims)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "example-idp-key"
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
