// Package api is a small JSON HTTP API over a Memori instance. It is the
// non-MCP surface: same memory, Bearer auth, one entity switch at a time.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Deep-Octopus/Memorigo/memori"
	"github.com/Deep-Octopus/Memorigo/storage"
)

const maxBody = 1 << 20

// Options configures the HTTP API.
type Options struct {
	EntityID       string
	ProcessID      string
	ReadOnly       bool
	Token          string
	AllowAnonymous bool
}

// NewHandler serves /healthz and /v1/*. A token is required unless
// AllowAnonymous is set. The handler serializes calls because attribution is
// per Memori instance.
func NewHandler(m *memori.Memori, opts Options) (http.Handler, error) {
	if m == nil {
		return nil, errors.New("api: memori is nil")
	}
	if opts.Token == "" && !opts.AllowAnonymous {
		return nil, errors.New("api: token is required unless AllowAnonymous is set")
	}
	if opts.EntityID == "" {
		opts.EntityID = "api-user"
	}
	if opts.ProcessID == "" {
		opts.ProcessID = "memorigo-api"
	}
	s := &server{m: m, opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /v1/recall", s.recall)
	mux.HandleFunc("POST /v1/remember", s.remember)
	mux.HandleFunc("POST /v1/forget", s.forget)
	mux.HandleFunc("POST /v1/restore", s.restore)
	mux.HandleFunc("POST /v1/context", s.context)
	mux.HandleFunc("GET /v1/facts", s.facts)
	mux.HandleFunc("GET /v1/profile", s.profile)
	mux.HandleFunc("GET /v1/stats", s.stats)
	mux.HandleFunc("GET /v1/export", s.export)
	mux.HandleFunc("DELETE /v1/subject", s.deleteSubject)
	mux.HandleFunc("POST /v1/maintain", s.maintain)
	return s.wrap(mux), nil
}

type server struct {
	m    *memori.Memori
	opts Options
	mu   sync.Mutex
}

func (s *server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="memorigo"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) authorized(r *http.Request) bool {
	if s.opts.Token == "" {
		return s.opts.AllowAnonymous
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	got := header[len(prefix):]
	if len(got) != len(s.opts.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Token)) == 1
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) recall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
		EntityID string `json:"entity_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	s.withEntity(w, r, body.EntityID, func() error {
		limit := body.Limit
		if limit <= 0 {
			limit = 5
		}
		if limit > 50 {
			limit = 50
		}
		facts, err := s.m.Recall(body.Query, limit)
		if err != nil {
			return err
		}
		if facts == nil {
			facts = []memori.Fact{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"facts": facts})
		return nil
	})
}

func (s *server) remember(w http.ResponseWriter, r *http.Request) {
	if s.readOnly(w) {
		return
	}
	var body struct {
		Content  string `json:"content"`
		EntityID string `json:"entity_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	s.withEntity(w, r, body.EntityID, func() error {
		id, err := s.m.Remember(body.Content)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
		return nil
	})
}

func (s *server) forget(w http.ResponseWriter, r *http.Request) {
	if s.readOnly(w) {
		return
	}
	var body struct {
		ID       int64  `json:"id"`
		EntityID string `json:"entity_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	s.withEntity(w, r, body.EntityID, func() error {
		if err := s.m.Forget(body.ID); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	})
}

func (s *server) restore(w http.ResponseWriter, r *http.Request) {
	if s.readOnly(w) {
		return
	}
	var body struct {
		ID       int64  `json:"id"`
		EntityID string `json:"entity_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	s.withEntity(w, r, body.EntityID, func() error {
		if err := s.m.Restore(body.ID); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	})
}

func (s *server) context(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query          string `json:"query"`
		Budget         int    `json:"budget"`
		IncludeRaw     bool   `json:"include_raw"`
		IncludeProfile *bool  `json:"include_profile"`
		EntityID       string `json:"entity_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	includeProfile := true
	if body.IncludeProfile != nil {
		includeProfile = *body.IncludeProfile
	}
	s.withEntity(w, r, body.EntityID, func() error {
		pkg, err := s.m.BuildContext(r.Context(), body.Query, memori.ContextOptions{
			TokenBudget:    body.Budget,
			IncludeRaw:     body.IncludeRaw,
			IncludeProfile: includeProfile,
		})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, pkg)
		return nil
	})
}

func (s *server) facts(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	s.withEntity(w, r, r.URL.Query().Get("entity_id"), func() error {
		facts, err := s.m.Facts(limit)
		if err != nil {
			return err
		}
		if facts == nil {
			facts = []memori.Fact{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"facts": facts})
		return nil
	})
}

func (s *server) profile(w http.ResponseWriter, r *http.Request) {
	s.withEntity(w, r, r.URL.Query().Get("entity_id"), func() error {
		profile, err := s.m.Profile(r.Context(), memori.ProfileOptions{
			TokenBudget:    queryInt(r, "budget", 0),
			MaxFacts:       queryInt(r, "limit", 0),
			IncludeDynamic: queryBool(r, "include_dynamic"),
		})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, profile)
		return nil
	})
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	s.withEntity(w, r, r.URL.Query().Get("entity_id"), func() error {
		stats, err := s.m.Stats(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"entity_id":          stats.EntityID,
			"total_facts":        stats.TotalFacts,
			"active_facts":       stats.ActiveFacts,
			"pending_jobs":       stats.PendingJobs,
			"embedding_provider": stats.EmbeddingProvider,
			"embedding_model":    stats.EmbeddingModel,
			"embedding_dim":      stats.EmbeddingDim,
			"storage_dialect":    stats.StorageDialect,
		})
		return nil
	})
}

func (s *server) export(w http.ResponseWriter, r *http.Request) {
	s.withEntity(w, r, r.URL.Query().Get("entity_id"), func() error {
		doc, err := s.m.Export(memori.ExportOptions{Redact: queryBool(r, "redact")})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, doc)
		return nil
	})
}

func (s *server) deleteSubject(w http.ResponseWriter, r *http.Request) {
	if s.readOnly(w) {
		return
	}
	s.withEntity(w, r, r.URL.Query().Get("entity_id"), func() error {
		report, err := s.m.DeleteSubject()
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, report)
		return nil
	})
}

func (s *server) maintain(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TTL          string `json:"ttl"`
		ProtectTimes int64  `json:"protect_times"`
		DryRun       bool   `json:"dry_run"`
		EntityID     string `json:"entity_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	if s.opts.ReadOnly && !body.DryRun {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "read only"})
		return
	}
	ttl, err := time.ParseDuration(body.TTL)
	if err != nil || ttl <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ttl must be a positive duration such as 720h"})
		return
	}
	s.withEntity(w, r, body.EntityID, func() error {
		report, err := s.m.Maintain(memori.MaintainOptions{
			TTL:          ttl,
			ProtectTimes: body.ProtectTimes,
			DryRun:       body.DryRun,
		})
		if err != nil {
			return err
		}
		expired := report.Expired
		if expired == nil {
			expired = []int64{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"expired": expired, "dry_run": report.DryRun})
		return nil
	})
}

func (s *server) withEntity(w http.ResponseWriter, r *http.Request, entityID string, fn func() error) {
	if entityID == "" {
		entityID = s.opts.EntityID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.m.TryAttribution(entityID, s.opts.ProcessID); err != nil {
		writeAPIError(w, err)
		return
	}
	if err := fn(); err != nil {
		writeAPIError(w, err)
	}
	_ = r
}

func (s *server) readOnly(w http.ResponseWriter) bool {
	if !s.opts.ReadOnly {
		return false
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "read only"})
	return true
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return false
	}
	return true
}

func writeAPIError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, memori.ErrNoAttribution), errors.Is(err, memori.ErrEmptyContent):
		status = http.StatusBadRequest
	case errors.Is(err, memori.ErrClosed):
		status = http.StatusServiceUnavailable
	case errors.Is(err, storage.ErrNotFound):
		status = http.StatusNotFound
	}
	msg := "internal error"
	if status != http.StatusInternalServerError {
		msg = err.Error()
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func queryInt(r *http.Request, key string, fallback int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func queryBool(r *http.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
