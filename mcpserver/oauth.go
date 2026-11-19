package mcpserver

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"context"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	oauthClockSkew    = 30 * time.Second
	oauthHMACMinBytes = 16
	oauthRSAMinBits   = 2048
	oauthStaticExtra  = "memorigo_auth"
	oauthStaticValue  = "static"
)

// OAuthConfig turns the HTTP MCP endpoint into an OAuth resource server.
// The zero value leaves authentication on the static Bearer token.
//
// This does not issue tokens and it does not replace MEMORI_MCP_TOKEN.
// A valid static token keeps the process allow-list. A JWT is accepted
// only when iss, aud, exp and the signature match, and its scope cannot
// exceed that allow-list or read-only mode.
type OAuthConfig struct {
	// Issuer is the required iss claim. It must be an absolute http(s) URL.
	Issuer string
	// Audience is the required aud claim and the protected-resource identifier.
	// It must be an absolute http(s) URL, usually the public MCP endpoint.
	Audience string
	// HMACSecret verifies HS256 tokens. It is mutually exclusive with
	// RSAPublicKeyPEM and must be at least 16 bytes.
	HMACSecret string
	// RSAPublicKeyPEM is a PKIX or PKCS#1 RSA public key. It verifies RS256
	// tokens. The key must be at least 2048 bits.
	RSAPublicKeyPEM string
	// Now overrides the clock used for exp and nbf. Nil uses time.Now.
	Now func() time.Time
}

type oauthSettings struct {
	issuer      string
	audience    string
	hmac        []byte
	rsa         *rsa.PublicKey
	metadataURL string
	paths       []string
	metadata    *oauthex.ProtectedResourceMetadata
	now         func() time.Time
}

func prepareOAuth(cfg OAuthConfig) (*oauthSettings, error) {
	secret := cfg.HMACSecret
	pemText := strings.TrimSpace(cfg.RSAPublicKeyPEM)
	if strings.TrimSpace(cfg.Issuer) == "" && strings.TrimSpace(cfg.Audience) == "" && secret == "" && pemText == "" {
		return nil, nil
	}
	issuer, err := canonicalHTTPURL("issuer", cfg.Issuer)
	if err != nil {
		return nil, err
	}
	audience, err := canonicalHTTPURL("audience", cfg.Audience)
	if err != nil {
		return nil, err
	}
	if secret != "" && pemText != "" {
		return nil, errors.New("mcpserver: OAuth accepts either an HMAC secret or an RSA public key")
	}
	settings := &oauthSettings{issuer: issuer, audience: audience, now: cfg.Now}
	if settings.now == nil {
		settings.now = time.Now
	}
	switch {
	case secret != "":
		if len(secret) < oauthHMACMinBytes {
			return nil, fmt.Errorf("mcpserver: OAuth HMAC secret must be at least %d bytes", oauthHMACMinBytes)
		}
		settings.hmac = []byte(secret)
	case pemText != "":
		key, err := parseRSAPublicKey(pemText)
		if err != nil {
			return nil, err
		}
		settings.rsa = key
	default:
		return nil, errors.New("mcpserver: OAuth requires an HMAC secret or an RSA public key")
	}
	metaURL, paths, err := protectedResourcePaths(audience)
	if err != nil {
		return nil, err
	}
	settings.metadataURL = metaURL
	settings.paths = paths
	settings.metadata = &oauthex.ProtectedResourceMetadata{
		Resource:               audience,
		AuthorizationServers:   []string{issuer},
		ScopesSupported:        oauthAdvertisedScopes(),
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "memorigo",
	}
	return settings, nil
}

func canonicalHTTPURL(name, raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("mcpserver: OAuth %s must be an absolute http(s) URL", name)
	}
	return u.String(), nil
}

func protectedResourcePaths(audience string) (string, []string, error) {
	u, err := url.Parse(audience)
	if err != nil {
		return "", nil, err
	}
	root := "/.well-known/oauth-protected-resource"
	metaPath := root
	paths := []string{root}
	if trimmed := strings.Trim(u.Path, "/"); trimmed != "" {
		metaPath = root + "/" + trimmed
		paths = append(paths, metaPath)
	}
	abs := url.URL{Scheme: u.Scheme, Host: u.Host, Path: metaPath}
	return abs.String(), paths, nil
}

func oauthAdvertisedScopes() []string {
	out := []string{"read", "write"}
	out = append(out, allTools...)
	return out
}

func parseRSAPublicKey(pemText string) (*rsa.PublicKey, error) {
	block, rest := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("mcpserver: OAuth RSA public key is not PEM")
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("mcpserver: OAuth RSA public key PEM must contain one key")
	}
	var (
		key *rsa.PublicKey
		err error
	)
	switch block.Type {
	case "PUBLIC KEY":
		parsed, parseErr := x509.ParsePKIXPublicKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("mcpserver: parse OAuth RSA public key: %w", parseErr)
		}
		var ok bool
		key, ok = parsed.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("mcpserver: OAuth public key is not RSA")
		}
	case "RSA PUBLIC KEY":
		key, err = x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("mcpserver: parse OAuth RSA public key: %w", err)
		}
	default:
		return nil, errors.New("mcpserver: OAuth public key PEM must be PUBLIC KEY or RSA PUBLIC KEY")
	}
	if key.N.BitLen() < oauthRSAMinBits {
		return nil, fmt.Errorf("mcpserver: OAuth RSA public key must be at least %d bits", oauthRSAMinBits)
	}
	return key, nil
}

func (s *oauthSettings) middleware(staticToken string, next http.Handler) http.Handler {
	return auth.RequireBearerToken(s.verifier(staticToken), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL:    s.metadataURL,
		AllowMissingExpiration: true,
		ClockSkew:              oauthClockSkew,
	})(next)
}

func (s *oauthSettings) verifier(staticToken string) auth.TokenVerifier {
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if staticToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(staticToken)) == 1 {
			return &auth.TokenInfo{Extra: map[string]any{oauthStaticExtra: oauthStaticValue}}, nil
		}
		return verifyOAuthJWT(token, s)
	}
}

func verifyOAuthJWT(token string, s *oauthSettings) (*auth.TokenInfo, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return nil, oauthInvalid("malformed token")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, oauthInvalid("malformed token")
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, oauthInvalid("malformed token")
	}
	if len(header.Crit) > 0 {
		return nil, oauthInvalid("unsupported token header")
	}
	if header.Typ != "" && !acceptedJWTType(header.Typ) {
		return nil, oauthInvalid("unsupported token type")
	}
	wantAlg := "RS256"
	if s.hmac != nil {
		wantAlg = "HS256"
	}
	if header.Alg != wantAlg {
		return nil, oauthInvalid("unsupported token algorithm")
	}
	signed := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, oauthInvalid("malformed token")
	}
	if s.hmac != nil {
		mac := hmac.New(sha256.New, s.hmac)
		_, _ = mac.Write([]byte(signed))
		if !hmac.Equal(mac.Sum(nil), sig) {
			return nil, oauthInvalid("token signature mismatch")
		}
	} else {
		sum := sha256.Sum256([]byte(signed))
		if err := rsa.VerifyPKCS1v15(s.rsa, crypto.SHA256, sum[:], sig); err != nil {
			return nil, oauthInvalid("token signature mismatch")
		}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, oauthInvalid("malformed token")
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, oauthInvalid("malformed token")
	}
	if claims.Iss != s.issuer {
		return nil, oauthInvalid("token issuer mismatch")
	}
	if !claims.Aud.contains(s.audience) {
		return nil, oauthInvalid("token audience mismatch")
	}
	if claims.Exp == nil || !validUnix(*claims.Exp) {
		return nil, oauthInvalid("token missing expiration")
	}
	now := s.now()
	exp := time.Unix(int64(*claims.Exp), 0)
	if exp.Add(oauthClockSkew).Before(now) {
		return nil, oauthInvalid("token expired")
	}
	if claims.Nbf != nil {
		if !validUnix(*claims.Nbf) {
			return nil, oauthInvalid("token not yet valid")
		}
		nbf := time.Unix(int64(*claims.Nbf), 0)
		if nbf.Add(-oauthClockSkew).After(now) {
			return nil, oauthInvalid("token not yet valid")
		}
	}
	rawScope := strings.TrimSpace(claims.Scope)
	if claims.Scp != nil && claims.Scp.raw != "" {
		if rawScope != "" {
			return nil, oauthInvalid("token scope is ambiguous")
		}
		rawScope = claims.Scp.raw
	}
	names, err := ParseScopes(strings.Join(strings.Fields(rawScope), ","))
	if err != nil || len(names) == 0 {
		return nil, oauthInvalid("token scope is not accepted")
	}
	return &auth.TokenInfo{Scopes: names, Expiration: exp}, nil
}

func oauthInvalid(reason string) error {
	return fmt.Errorf("%w: %s", auth.ErrInvalidToken, reason)
}

func acceptedJWTType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "jwt", "at+jwt", "application/at+jwt":
		return true
	default:
		return false
	}
}

func validUnix(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 && v < math.MaxInt64
}

type jwtHeader struct {
	Alg  string   `json:"alg"`
	Typ  string   `json:"typ"`
	Crit []string `json:"crit"`
}

type jwtClaims struct {
	Iss   string    `json:"iss"`
	Aud   audience  `json:"aud"`
	Exp   *float64  `json:"exp"`
	Nbf   *float64  `json:"nbf"`
	Scope string    `json:"scope"`
	Scp   *scpClaim `json:"scp"`
}

type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return errors.New("missing aud")
	}
	if data[0] == '"' {
		var one string
		if err := json.Unmarshal(data, &one); err != nil {
			return err
		}
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a audience) contains(want string) bool {
	for _, item := range a {
		if item == want {
			return true
		}
	}
	return false
}

type scpClaim struct {
	raw string
}

func (s *scpClaim) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &s.raw)
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	s.raw = strings.Join(list, " ")
	return nil
}

// oauthToolScopes returns the tools a JWT may call. A static Bearer token and
// a request without token info return false so the process allow-list remains
// the only restriction.
func oauthToolScopes(req *mcp.CallToolRequest) (map[string]struct{}, bool) {
	if req == nil || req.Extra == nil || req.Extra.TokenInfo == nil {
		return nil, false
	}
	info := req.Extra.TokenInfo
	if info.Extra != nil && info.Extra[oauthStaticExtra] == oauthStaticValue {
		return nil, false
	}
	set := make(map[string]struct{}, len(info.Scopes))
	for _, name := range info.Scopes {
		set[name] = struct{}{}
	}
	return set, true
}
