package testutil

import (
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func AccessToken(t testing.TB, key *rsa.PrivateKey, id, issuer, subject string, change func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{"cluster_id": id, "iss": issuer, "sub": subject, "exp": time.Now().Add(time.Hour).Unix(), "token_use": "access", "grant": []string{"guest:" + subject}}
	if change != nil {
		change(claims)
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
