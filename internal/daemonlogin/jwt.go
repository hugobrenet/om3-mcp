package daemonlogin

import (
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

type jwtClaims struct {
	Grant    []string `json:"grant"`
	TokenUse string   `json:"token_use"`
	jwt.RegisteredClaims
}

// jwtVerifier checks daemon-issued access tokens against the selected cluster's CA.
type jwtVerifier struct{ publicKeys jwt.VerificationKeySet }

// newJWTVerifier uses the already loaded public trust snapshot. All RSA
// keys in the bundle are considered, including during a CA rollover.
func newJWTVerifier(keyPEM []byte) (*jwtVerifier, error) {
	v := &jwtVerifier{}
	for len(keyPEM) > 0 {
		block, rest := pem.Decode(keyPEM)
		if block == nil {
			break
		}
		keyPEM = rest
		key, err := jwt.ParseRSAPublicKeyFromPEM(pem.EncodeToMemory(block))
		if err == nil {
			v.publicKeys.Keys = append(v.publicKeys.Keys, key)
		}
	}
	if len(v.publicKeys.Keys) == 0 {
		return nil, fmt.Errorf("OpenSVC JWT trust material contains no RSA verification key")
	}
	return v, nil
}

// verify validates the daemon token before creating a server-side session.
func (v *jwtVerifier) verify(rawToken string) (*jwtClaims, error) {
	claims := &jwtClaims{}
	token, err := jwt.ParseWithClaims(
		rawToken,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodRS256 {
				return nil, fmt.Errorf("unexpected signing method %q", token.Method.Alg())
			}
			return v.publicKeys, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, invalidToken("signature or registered claims validation failed")
	}
	if claims.Subject == "" {
		return nil, invalidToken("subject claim is missing")
	}
	if claims.Issuer == "" {
		return nil, invalidToken("issuer claim is missing")
	}
	if claims.TokenUse != "access" {
		return nil, invalidToken("token_use claim is not access")
	}
	if claims.ExpiresAt == nil {
		return nil, invalidToken("expiration claim is missing")
	}

	return claims, nil
}

func invalidToken(reason string) error {
	return errors.New("invalid OpenSVC access JWT: " + reason)
}
