package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	toolSearch   = "memory_search"
	toolContext  = "memory_context"
	toolRemember = "memory_remember"
	toolForget   = "memory_forget"
	toolRule     = "memory_rule"
	toolProfile  = "memory_profile"
	toolStats    = "memory_stats"

	// MaxQuotaPerMinute is the largest accepted per-process tool quota.
	// Zero means unlimited. The window is not shared across processes.
	MaxQuotaPerMinute = 100000
)

var (
	errReadOnly  = errors.New("memorigo MCP server is read-only")
	errForbidden = errors.New("memorigo MCP tool is not allowed")
	errQuota     = errors.New("memorigo MCP quota exceeded")
)

var allTools = []string{toolSearch, toolContext, toolRemember, toolForget, toolRule, toolProfile, toolStats}

var scopeAliases = map[string][]string{
	"memory_search":   {toolSearch},
	"search":          {toolSearch},
	"memory_context":  {toolContext},
	"context":         {toolContext},
	"memory_remember": {toolRemember},
	"remember":        {toolRemember},
	"memory_forget":   {toolForget},
	"forget":          {toolForget},
	"memory_rule":     {toolRule},
	"rule":            {toolRule},
	"memory_profile":  {toolProfile},
	"profile":         {toolProfile},
	"memory_stats":    {toolStats},
	"stats":           {toolStats},
	"read":            {toolSearch, toolContext, toolProfile, toolStats},
	"write":           {toolRemember, toolForget, toolRule},
	"all":             allTools,
}

// ParseScopes expands a comma-separated allow-list into canonical tool names.
// An empty string allows every tool. Aliases: read, write, all, and the short
// tool names (search, context, remember, forget, profile, stats).
func ParseScopes(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}
		names, ok := scopeAliases[part]
		if !ok {
			return nil, fmt.Errorf("unknown MCP scope %q", part)
		}
		for _, name := range names {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("MCP scope list is empty")
	}
	return out, nil
}

func (s *state) configure() {
	limit := s.opts.QuotaPerMinute
	if limit < 0 {
		limit = 0
	}
	if limit > MaxQuotaPerMinute {
		limit = MaxQuotaPerMinute
	}
	s.quotaLimit = limit
	if len(s.opts.Scopes) == 0 {
		return
	}
	names, err := ParseScopes(strings.Join(s.opts.Scopes, ","))
	if err != nil {
		s.scopeErr = err
		s.allowed = map[string]struct{}{}
		return
	}
	s.allowed = make(map[string]struct{}, len(names))
	for _, name := range names {
		s.allowed[name] = struct{}{}
	}
}

func (s *state) enabled(name string) bool {
	if s.scopeErr != nil {
		return false
	}
	if s.allowed == nil {
		return true
	}
	_, ok := s.allowed[name]
	return ok
}

func (s *state) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

func (s *state) resolveEntity(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return s.opts.EntityID
	}
	return id
}

// guard checks the static allow-list and read-only mode. It does not consume
// quota. A denied call is still audited.
func (s *state) guard(req *mcp.CallToolRequest, tool, entity string) (string, time.Time, error) {
	started := s.now()
	entity = s.resolveEntity(entity)
	if err := s.permit(req, tool); err != nil {
		s.record(tool, entity, outcomeOf(err), started)
		return entity, started, err
	}
	return entity, started, nil
}

func (s *state) permit(req *mcp.CallToolRequest, tool string) error {
	if s.scopeErr != nil {
		return s.scopeErr
	}
	if s.allowed != nil {
		if _, ok := s.allowed[tool]; !ok {
			return errForbidden
		}
	}
	if s.opts.ReadOnly && isWriteTool(tool) {
		return errReadOnly
	}
	if scopes, ok := oauthToolScopes(req); ok {
		if _, allowed := scopes[tool]; !allowed {
			return errForbidden
		}
	}
	return nil
}

func isWriteTool(name string) bool {
	return name == toolRemember || name == toolForget || name == toolRule
}

// charge consumes one unit of the per-minute quota. Unlimited when the limit
// is zero. The call is counted even if the memory operation later fails.
func (s *state) charge(tool, entity string, started time.Time) error {
	if !s.takeQuota(s.now()) {
		s.record(tool, entity, "quota", started)
		return errQuota
	}
	return nil
}

func (s *state) takeQuota(now time.Time) bool {
	if s.quotaLimit <= 0 {
		return true
	}
	cutoff := now.Add(-time.Minute)
	kept := s.quotaTimes[:0]
	for _, ts := range s.quotaTimes {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	s.quotaTimes = kept
	if len(s.quotaTimes) >= s.quotaLimit {
		return false
	}
	s.quotaTimes = append(s.quotaTimes, now)
	return true
}

func (s *state) done(tool, entity, outcome string, started time.Time) {
	s.record(tool, entity, outcome, started)
}

func outcomeOf(err error) string {
	switch {
	case errors.Is(err, errReadOnly):
		return "readonly"
	case errors.Is(err, errForbidden):
		return "forbidden"
	case errors.Is(err, errQuota):
		return "quota"
	default:
		return "error"
	}
}

type auditRecord struct {
	Time       string `json:"time"`
	Tool       string `json:"tool"`
	EntityID   string `json:"entity_id"`
	Outcome    string `json:"outcome"`
	DurationUS int64  `json:"duration_us"`
}

// record appends one JSON line. The line has no query and no memory text.
// A write failure is logged and does not undo the memory operation.
func (s *state) record(tool, entity, outcome string, started time.Time) {
	if s.opts.Audit == nil {
		return
	}
	end := s.now()
	rec := auditRecord{
		Time:       end.UTC().Format(time.RFC3339Nano),
		Tool:       tool,
		EntityID:   cleanAuditEntity(entity),
		Outcome:    outcome,
		DurationUS: end.Sub(started).Microseconds(),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		log.Printf("memorigo MCP audit encode: %v", err)
		return
	}
	if _, err := s.opts.Audit.Write(buf.Bytes()); err != nil {
		log.Printf("memorigo MCP audit write: %v", err)
	}
}

func cleanAuditEntity(id string) string {
	id = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, id)
	runes := []rune(id)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}
