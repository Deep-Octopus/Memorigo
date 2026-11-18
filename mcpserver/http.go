package mcpserver

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPOptions configures the Streamable HTTP transport.
type HTTPOptions struct {
	// Token enables Bearer authentication. It is required unless
	// AllowAnonymous is true.
	Token string
	// AllowAnonymous starts an unauthenticated server. Only use this on a
	// trusted loopback interface.
	AllowAnonymous bool
	// Path is the MCP endpoint. Defaults to /mcp.
	Path string
	// Version is reported by /healthz.
	Version string
}

// NewHTTPHandler returns a stateless Streamable HTTP handler for the MCP
// server. /healthz is always unauthenticated; the MCP endpoint requires a
// Bearer token when Token is set.
func NewHTTPHandler(server *mcp.Server, opts HTTPOptions) (http.Handler, error) {
	if server == nil {
		return nil, errors.New("mcpserver: HTTP handler requires a non-nil server")
	}
	if opts.Path == "" {
		opts.Path = "/mcp"
	}
	if !strings.HasPrefix(opts.Path, "/") {
		return nil, errors.New("mcpserver: HTTP path must start with /")
	}
	if opts.Path == "/" || opts.Path == "/healthz" || strings.ContainsAny(opts.Path, " \t\r\n{}") {
		return nil, errors.New("mcpserver: HTTP path is reserved or invalid")
	}
	if opts.Token == "" && !opts.AllowAnonymous {
		return nil, errors.New("mcpserver: HTTP transport requires a token or AllowAnonymous")
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}

	rawHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:           true,
			JSONResponse:        true,
			MaxRequestBodyBytes: 1 << 20,
			// The SDK's localhost check rejects legitimate TLS reverse-proxy
			// requests (loopback local address, public Host header). We keep
			// DNS-rebinding/CSRF protection by wrapping with Go's cross-origin
			// middleware instead, which allows non-browser MCP clients and
			// rejects cross-site browser requests.
			DisableLocalhostProtection: true,
		},
	)
	mcpHandler := http.NewCrossOriginProtection().Handler(rawHandler)

	mux := http.NewServeMux()
	if opts.Token != "" {
		mux.Handle(opts.Path, bearerAuth(opts.Token, mcpHandler))
	} else {
		mux.Handle(opts.Path, mcpHandler)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": "memorigo",
			"version": opts.Version,
		})
	})
	return securityHeaders(mux), nil
}

func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) ||
			subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="memorigo"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
