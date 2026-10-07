package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.yaml.in/yaml/v2"
)

const accessTokenType = "urn:ietf:params:oauth:token-type:access_token"

// ExchangeProfiles holds only administrator-owned credentials and public
// discovery metadata. User tokens are never cached here.
type ExchangeProfiles struct{ profiles map[string]*exchangeProfile }

type exchangeSettings struct {
	Issuer     string   `yaml:"issuer"`
	ClientID   string   `yaml:"client_id"`
	SecretFile string   `yaml:"client_secret_file"`
	Method     string   `yaml:"token_endpoint_auth_method"`
	Scopes     []string `yaml:"scopes"`
	Timeout    string   `yaml:"request_timeout"`
	TLS        struct {
		CAFile string `yaml:"ca_file"`
	} `yaml:"tls"`
}

type exchangeProfile struct {
	settings exchangeSettings
	secret   string
	client   *http.Client
	mu       sync.Mutex
	endpoint string
	expires  time.Time
}

// LoadExchangeProfiles loads a local snapshot, without contacting the SSO.
func LoadExchangeProfiles(path string) (*ExchangeProfiles, error) {
	data, err := exchangeFile(path, 1<<20, false)
	if err != nil {
		return nil, fmt.Errorf("exchange profiles: %w", err)
	}
	var doc struct {
		Version  int                         `yaml:"version"`
		Profiles map[string]exchangeSettings `yaml:"profiles"`
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.SetStrict(true)
	if d.Decode(&doc) != nil {
		return nil, errors.New("exchange profiles: invalid YAML, unknown or duplicate fields")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || doc.Version != 1 || len(doc.Profiles) == 0 || len(doc.Profiles) > 64 {
		return nil, errors.New("exchange profiles: expected one version 1 document with 1 to 64 profiles")
	}
	result := &ExchangeProfiles{profiles: make(map[string]*exchangeProfile)}
	for name, s := range doc.Profiles {
		if !validClaimText(name) || !validClaimText(s.ClientID) {
			return nil, errors.New("exchange profiles: invalid name or client_id")
		}
		if _, err := httpsURL(s.Issuer); err != nil {
			return nil, errors.New("exchange profiles: issuer must be an absolute HTTPS URL")
		}
		if s.Method == "" {
			s.Method = "client_secret_basic"
		}
		if s.Method != "client_secret_basic" && s.Method != "client_secret_post" {
			return nil, errors.New("exchange profiles: unsupported token_endpoint_auth_method")
		}
		if len(s.Scopes) > 32 {
			return nil, errors.New("exchange profiles: at most 32 technical scopes")
		}
		seen := make(map[string]bool)
		for _, scope := range s.Scopes {
			if len(scope) == 0 || len(scope) > 256 || seen[scope] {
				return nil, errors.New("exchange profiles: invalid or duplicate scope")
			}
			for _, c := range scope {
				if c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
					return nil, errors.New("exchange profiles: invalid scope character")
				}
			}
			seen[scope] = true
		}
		if s.Timeout == "" {
			s.Timeout = "10s"
		}
		timeout, err := time.ParseDuration(s.Timeout)
		if err != nil || timeout < time.Second || timeout > time.Minute {
			return nil, errors.New("exchange profiles: request_timeout must be between 1s and 1m")
		}
		if s.TLS.CAFile != "" && !filepath.IsAbs(s.TLS.CAFile) {
			return nil, errors.New("exchange profiles: tls.ca_file must be absolute")
		}
		secret, err := exchangeFile(s.SecretFile, 16<<10, true)
		if err != nil {
			return nil, fmt.Errorf("exchange client secret: %w", err)
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(secret), "\n"), "\r")
		if len(value) == 0 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errors.New("exchange client secret must contain one nonempty line")
		}
		client, err := oauthHTTPClient(s.TLS.CAFile, timeout)
		if err != nil {
			return nil, errors.New("exchange profiles: invalid SSO TLS trust")
		}
		result.profiles[name] = &exchangeProfile{settings: s, secret: value, client: client}
	}
	return result, nil
}

func exchangeFile(path string, limit int64, secret bool) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("file path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("file must be a readable bounded regular file")
	}
	if secret && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("secret file must not be accessible to group or others (use 0600 or 0400)")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %q: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("file exceeds size limit or is unreadable")
	}
	return data, nil
}

func (p *ExchangeProfiles) Has(name string) bool {
	if p == nil {
		return false
	}
	_, ok := p.profiles[name]
	return ok
}

func (p *exchangeProfile) tokenEndpoint(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Before(p.expires) {
		return p.endpoint, nil
	}
	var metadata struct {
		Issuer   string   `json:"issuer"`
		Endpoint string   `json:"token_endpoint"`
		Methods  []string `json:"token_endpoint_auth_methods_supported"`
	}
	// Use the same bounded, redirect-free fetcher as incoming JWT discovery.
	fetcher := oauthKeys{client: p.client}
	u, _ := url.Parse(p.settings.Issuer)
	u.Path = "/.well-known/oauth-authorization-server" + strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	for i, endpoint := range []string{strings.TrimRight(p.settings.Issuer, "/") + "/.well-known/openid-configuration", u.String()} {
		status, err := fetcher.getJSON(ctx, endpoint, &metadata)
		if status == 404 && i == 0 {
			continue
		}
		if err != nil {
			return "", errors.New("Token exchange discovery unavailable.")
		}
		break
	}
	if metadata.Issuer != p.settings.Issuer {
		return "", errors.New("Token exchange discovery issuer mismatch.")
	}
	if _, err := httpsURL(metadata.Endpoint); err != nil {
		return "", errors.New("Token exchange discovery has no valid HTTPS token endpoint.")
	}
	if len(metadata.Methods) > 0 && !slices.Contains(metadata.Methods, p.settings.Method) {
		return "", errors.New("Token exchange client authentication method is not supported by the SSO.")
	}
	p.endpoint, p.expires = metadata.Endpoint, time.Now().Add(5*time.Minute)
	return p.endpoint, nil
}

type exchangedContextKey struct{}
type exchangedCredential struct {
	clusterID, token string
	expires          time.Time
}

// ExchangedFromContext is consumed only by the origin-bound daemon transport.
func ExchangedFromContext(ctx context.Context) (clusterID, token string, ok bool) {
	c, ok := ctx.Value(exchangedContextKey{}).(exchangedCredential)
	if !ok || ctx.Err() != nil || !time.Now().Before(c.expires) {
		return "", "", false
	}
	return c.clusterID, c.token, true
}

// Prepare exchanges the authenticated MCP token once for one tool call. Target
// values must come from the administrator catalogue, never raw tool arguments.
func (p *ExchangeProfiles) Prepare(ctx context.Context, profile, audience, clusterID string) (context.Context, context.CancelFunc, error) {
	identity, subject, ok := OAuthFromContext(ctx)
	if !ok {
		return nil, nil, errors.New("Token exchange requires authenticated MCP credentials.")
	}
	if !p.Has(profile) || !validClaimText(audience) || !validClaimText(clusterID) {
		return nil, nil, errors.New("Token exchange target is not configured.")
	}
	s := p.profiles[profile]
	endpoint, err := s.tokenEndpoint(ctx)
	if err != nil {
		return nil, nil, err
	}
	form := url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {subject}, "subject_token_type": {accessTokenType},
		"requested_token_type": {accessTokenType}, "audience": {audience},
	}
	if len(s.settings.Scopes) > 0 {
		form.Set("scope", strings.Join(s.settings.Scopes, " "))
	}
	if s.settings.Method == "client_secret_post" {
		form.Set("client_id", s.settings.ClientID)
		form.Set("client_secret", s.secret)
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, errors.New("Token exchange request could not be constructed.")
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	if s.settings.Method == "client_secret_basic" {
		r.SetBasicAuth(url.QueryEscape(s.settings.ClientID), url.QueryEscape(s.secret))
	}
	resp, err := s.client.Do(r)
	if err != nil {
		return nil, nil, errors.New("Token exchange request failed or timed out.")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthDocumentBytes+1))
	if err != nil || len(data) > maxOAuthDocumentBytes {
		return nil, nil, errors.New("Token exchange response is unreadable or too large.")
	}
	if resp.StatusCode != http.StatusOK {
		// Never echo error_description, upstream body, endpoint or credentials.
		var problem struct {
			Code string `json:"error"`
		}
		_ = json.Unmarshal(data, &problem)
		switch problem.Code {
		case "invalid_grant", "invalid_target", "invalid_scope", "invalid_client", "unauthorized_client", "unsupported_grant_type", "invalid_request", "access_denied":
			return nil, nil, fmt.Errorf("Token exchange refused by SSO (%s).", problem.Code)
		default:
			return nil, nil, errors.New("Token exchange failed at the SSO.")
		}
	}
	var result struct {
		Token      string `json:"access_token"`
		Type       string `json:"token_type"`
		IssuedType string `json:"issued_token_type"`
		Expires    *int64 `json:"expires_in"`
	}
	if json.Unmarshal(data, &result) != nil || !strings.EqualFold(result.Type, "Bearer") || result.IssuedType != accessTokenType || result.Token == subject || len(result.Token) > maxTokenBytes {
		return nil, nil, errors.New("Token exchange returned an invalid access-token response.")
	}
	// ParseUnverified does not decode the signature segment. Validate the compact
	// token alphabet too, so an invalid Authorization header cannot cause an HTTP
	// transport error that quotes the credential back into a tool result.
	for _, c := range result.Token {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return nil, nil, errors.New("Token exchange returned an invalid compact JWT.")
		}
	}
	// The authenticated SSO response is checked for target and lifetime before
	// forwarding. The daemon verifies signature, issuer and grants independently.
	var claims jwt.RegisteredClaims
	token, _, err := jwt.NewParser().ParseUnverified(result.Token, &claims)
	if err != nil || token == nil || !openIDSigningMethod(token.Method.Alg()) || !validClaimText(claims.Subject) || !validClaimText(claims.Issuer) ||
		jwt.NewValidator(jwt.WithAudience(audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt()).Validate(&claims) != nil {
		return nil, nil, errors.New("Token exchange returned a JWT with invalid target or lifetime.")
	}
	// Refuse a token that would still be usable as the MCP credential.
	if identity.Resource == "" || slices.Contains(claims.Audience, identity.Resource) {
		return nil, nil, errors.New("Token exchange did not separate MCP and daemon audiences.")
	}
	deadline := claims.ExpiresAt.Time
	if identity.ExpiresAt.Before(deadline) {
		deadline = identity.ExpiresAt
	}
	if result.Expires != nil {
		if *result.Expires <= 0 || *result.Expires > int64((365*24*time.Hour)/time.Second) {
			return nil, nil, errors.New("Token exchange returned an invalid lifetime.")
		}
		if end := time.Now().Add(time.Duration(*result.Expires) * time.Second); end.Before(deadline) {
			deadline = end
		}
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	ctx = context.WithValue(ctx, exchangedContextKey{}, exchangedCredential{clusterID: clusterID, token: result.Token, expires: deadline})
	return ctx, cancel, nil
}
