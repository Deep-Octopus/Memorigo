package mcpserver

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

func TestPrepareOAuthRejectsPartialConfig(t *testing.T) {
	if settings, err := prepareOAuth(OAuthConfig{}); err != nil || settings != nil {
		t.Fatalf("zero config = %#v, %v", settings, err)
	}
	if _, err := prepareOAuth(OAuthConfig{Issuer: "http://issuer.example"}); err == nil {
		t.Fatal("partial config accepted")
	}
	if _, err := prepareOAuth(OAuthConfig{
		Issuer:     "http://issuer.example",
		Audience:   "http://memory.example/mcp",
		HMACSecret: "short",
	}); err == nil {
		t.Fatal("short HMAC secret accepted")
	}
	if _, err := prepareOAuth(OAuthConfig{
		Issuer:          "http://issuer.example",
		Audience:        "http://memory.example/mcp",
		HMACSecret:      strings.Repeat("k", 16),
		RSAPublicKeyPEM: "-----BEGIN PUBLIC KEY-----\nZm9v\n-----END PUBLIC KEY-----",
	}); err == nil {
		t.Fatal("both key types accepted")
	}
	if _, err := prepareOAuth(OAuthConfig{
		Issuer:     "issuer.example",
		Audience:   "http://memory.example/mcp",
		HMACSecret: strings.Repeat("k", 16),
	}); err == nil {
		t.Fatal("relative issuer accepted")
	}
}

func TestVerifyOAuthJWT(t *testing.T) {
	secret := strings.Repeat("s", 16)
	settings, err := prepareOAuth(OAuthConfig{
		Issuer:     "http://issuer.example",
		Audience:   "http://memory.example/mcp",
		HMACSecret: secret,
		Now:        func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	good := signHS256(t, secret, map[string]any{
		"iss":   "http://issuer.example",
		"aud":   []string{"http://other.example", "http://memory.example/mcp"},
		"exp":   1_700_000_100,
		"scope": "read",
	})
	info, err := verifyOAuthJWT(good, settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Scopes) != 4 || info.Scopes[0] != toolSearch {
		t.Fatalf("scopes = %#v", info.Scopes)
	}

	scp := signHS256(t, secret, map[string]any{
		"iss": "http://issuer.example",
		"aud": "http://memory.example/mcp",
		"exp": 1_700_000_100,
		"scp": []string{"memory_stats"},
	})
	info, err = verifyOAuthJWT(scp, settings)
	if err != nil || len(info.Scopes) != 1 || info.Scopes[0] != toolStats {
		t.Fatalf("scp = %#v, %v", info, err)
	}

	cases := []struct {
		name   string
		token  string
		reason string
	}{
		{name: "expired", token: signHS256(t, secret, map[string]any{
			"iss": "http://issuer.example", "aud": "http://memory.example/mcp", "exp": 1_699_999_000, "scope": "read",
		}), reason: "expired"},
		{name: "not yet valid", token: signHS256(t, secret, map[string]any{
			"iss": "http://issuer.example", "aud": "http://memory.example/mcp", "exp": 1_700_000_100, "nbf": 1_700_000_100, "scope": "read",
		}), reason: "not yet valid"},
		{name: "issuer", token: signHS256(t, secret, map[string]any{
			"iss": "http://other.example", "aud": "http://memory.example/mcp", "exp": 1_700_000_100, "scope": "read",
		}), reason: "issuer"},
		{name: "audience", token: signHS256(t, secret, map[string]any{
			"iss": "http://issuer.example", "aud": "http://other.example", "exp": 1_700_000_100, "scope": "read",
		}), reason: "audience"},
		{name: "scope", token: signHS256(t, secret, map[string]any{
			"iss": "http://issuer.example", "aud": "http://memory.example/mcp", "exp": 1_700_000_100, "scope": "drop",
		}), reason: "scope"},
		{name: "ambiguous", token: signHS256(t, secret, map[string]any{
			"iss": "http://issuer.example", "aud": "http://memory.example/mcp", "exp": 1_700_000_100, "scope": "read", "scp": "write",
		}), reason: "ambiguous"},
		{name: "none", token: signAlg(t, "none", "", map[string]any{
			"iss": "http://issuer.example", "aud": "http://memory.example/mcp", "exp": 1_700_000_100, "scope": "read",
		}), reason: "algorithm"},
		{name: "bad signature", token: signHS256(t, "other-secret-value", map[string]any{
			"iss": "http://issuer.example", "aud": "http://memory.example/mcp", "exp": 1_700_000_100, "scope": "read",
		}), reason: "signature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verifyOAuthJWT(tc.token, settings)
			if !errors.Is(err, auth.ErrInvalidToken) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestVerifyOAuthRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := encodeRSAPublicPEM(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := prepareOAuth(OAuthConfig{
		Issuer:          "https://issuer.example",
		Audience:        "https://memory.example/mcp",
		RSAPublicKeyPEM: der,
		Now:             func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	token := signRS256(t, key, map[string]any{
		"iss":   "https://issuer.example",
		"aud":   "https://memory.example/mcp",
		"exp":   1_700_000_100,
		"scope": "write",
	})
	info, err := verifyOAuthJWT(token, settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Scopes) != 3 || info.Scopes[0] != toolRemember {
		t.Fatalf("scopes = %#v", info.Scopes)
	}
	if _, err := verifyOAuthJWT(token+"x", settings); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("tampered token error = %v", err)
	}
}

func signHS256(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	return signAlg(t, "HS256", secret, claims)
}

func signAlg(t *testing.T, alg, secret string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": alg, "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig := ""
	if alg == "HS256" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(encoded))
		sig = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	return encoded + "." + sig
}

func signRS256(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "at+jwt"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(encoded))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func encodeRSAPublicPEM(key *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}
