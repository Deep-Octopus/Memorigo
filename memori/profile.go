package memori

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Deep-Octopus/Memorigo/storage"
	"github.com/Deep-Octopus/Memorigo/textutil"
)

const (
	defaultProfileBudget = 200
	defaultProfileLimit  = 40
	profileDynamicMaxAge = 90 * 24 * time.Hour
)

// ProfileOptions controls the always-on profile. The profile is built from
// active facts using the existing slot taxonomy; it does not require a query.
type ProfileOptions struct {
	// TokenBudget caps the rendered <memory_profile> block. Zero uses
	// MEMORI_PROFILE_BUDGET (default 200).
	TokenBudget int
	// MaxFacts caps the number of profile facts. Zero uses
	// MEMORI_PROFILE_LIMIT (default 40).
	MaxFacts int
	// IncludeDynamic adds recent non-static facts (current focus, opinions,
	// recent context). It is off by default for predictable prompt size.
	IncludeDynamic bool
}

// ProfileSection is one topic group in a profile (Identity, Preferences, ...).
type ProfileSection struct {
	Name  string `json:"name"`
	Facts []Fact `json:"facts"`
}

// Profile is a prompt-ready, query-independent view of an entity.
type Profile struct {
	EntityID  string           `json:"entity_id"`
	Sections  []ProfileSection `json:"sections,omitempty"`
	Facts     []Fact           `json:"facts,omitempty"`
	Text      string           `json:"text,omitempty"`
	Tokens    int              `json:"tokens"`
	Budget    int              `json:"budget"`
	Truncated bool             `json:"truncated,omitempty"`
}

type profileCandidate struct {
	fact     Fact
	section  string
	priority int
	slotRank int
	dynamic  bool
}

// Profile builds a small, stable, query-independent profile for the current
// entity. It is the "always-on" channel: callers can inject it on every turn
// while keeping query-specific recall separate.
func (m *Memori) Profile(ctx context.Context, opts ProfileOptions) (Profile, error) {
	p := Profile{}
	if m.closed.Load() {
		return p, ErrClosed
	}
	if err := m.Err(); err != nil {
		return p, err
	}
	repos, err := m.repos()
	if err != nil {
		return p, err
	}
	snap := m.Config.snapshot()
	budget, _, _ := profileSettings(snap, opts)
	p.EntityID = snap.EntityID
	p.Budget = budget
	if snap.EntityID == "" {
		return p, ErrNoAttribution
	}
	entityID, err := repos.Entity().GetByExternalID(ctx, entityKey(snap))
	if errors.Is(err, storage.ErrNotFound) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	return m.profileForEntity(ctx, repos, entityID, opts, snap)
}

func profileSettings(snap configSnapshot, opts ProfileOptions) (budget, limit int, includeDynamic bool) {
	budget = opts.TokenBudget
	if budget <= 0 {
		budget = snap.Profile.Budget
	}
	if budget <= 0 {
		budget = defaultProfileBudget
	}
	limit = opts.MaxFacts
	if limit <= 0 {
		limit = snap.Profile.Limit
	}
	if limit <= 0 {
		limit = defaultProfileLimit
	}
	includeDynamic = opts.IncludeDynamic || snap.Profile.IncludeDynamic
	return budget, limit, includeDynamic
}

func (m *Memori) profileForEntity(ctx context.Context, repos storage.Repos, entityID int64, opts ProfileOptions, snap configSnapshot) (Profile, error) {
	budget, limit, includeDynamic := profileSettings(snap, opts)
	p := Profile{EntityID: snap.EntityID, Budget: budget}

	scan := snap.Recall.ScanLimit
	if scan <= 0 {
		scan = 2000
	}
	rows, err := repos.EntityFact().Candidates(ctx, entityID, scan)
	if err != nil {
		return p, err
	}
	rows = visibleFacts(rows, snap.Recall.IncludeInferred)
	if len(rows) == 0 {
		return p, nil
	}

	now := time.Now()
	seenID := make(map[int64]struct{}, len(rows))
	seenKey := make(map[string]struct{}, len(rows))
	candidates := make([]profileCandidate, 0, len(rows))
	for _, row := range rows {
		section, priority, slotRank, dynamic := profileSectionFor(row.Slot)
		if dynamic {
			if !includeDynamic {
				continue
			}
			if !row.DateLastTime.IsZero() && now.Sub(row.DateLastTime) > profileDynamicMaxAge {
				continue
			}
		}
		content := strings.TrimSpace(row.Content)
		if content == "" {
			continue
		}
		if _, ok := seenID[row.ID]; ok {
			continue
		}
		key := textutil.Key(content)
		if key == "" {
			continue
		}
		if _, ok := seenKey[key]; ok {
			continue
		}
		seenID[row.ID] = struct{}{}
		seenKey[key] = struct{}{}
		candidates = append(candidates, profileCandidate{
			fact: Fact{
				ID:             row.ID,
				Content:        content,
				Slot:           row.Slot,
				NumTimes:       row.NumTimes,
				DateLastTime:   row.DateLastTime,
				ConversationID: row.SourceConversationID,
			},
			section:  section,
			priority: priority,
			slotRank: slotRank,
			dynamic:  dynamic,
		})
	}
	if len(candidates) == 0 {
		return p, nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		if a.slotRank != b.slotRank {
			return a.slotRank > b.slotRank
		}
		if !a.fact.DateLastTime.Equal(b.fact.DateLastTime) {
			return a.fact.DateLastTime.After(b.fact.DateLastTime)
		}
		if a.fact.NumTimes != b.fact.NumTimes {
			return a.fact.NumTimes > b.fact.NumTimes
		}
		return a.fact.ID < b.fact.ID
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	const header = "<memory_profile>\n"
	const footer = "</memory_profile>\n"
	remaining := budget - textutil.EstimateTokens(header) - textutil.EstimateTokens(footer)
	if remaining <= 0 {
		p.Truncated = true
		return p, nil
	}

	var b strings.Builder
	b.WriteString(header)
	currentSection := ""
	used := make([]profileCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.section != currentSection {
			heading := candidate.section + ":\n"
			n := textutil.EstimateTokens(heading)
			if n > remaining {
				p.Truncated = true
				break
			}
			b.WriteString(heading)
			remaining -= n
			currentSection = candidate.section
		}
		prefix := fmt.Sprintf("- [fact:%d] ", candidate.fact.ID)
		suffix := "\n"
		base := textutil.EstimateTokens(prefix) + textutil.EstimateTokens(suffix)
		if base >= remaining {
			p.Truncated = true
			break
		}
		avail := remaining - base
		content, cut := textutil.TruncateToTokens(candidate.fact.Content, avail)
		if strings.TrimSpace(content) == "" {
			p.Truncated = true
			break
		}
		line := prefix + content + suffix
		n := textutil.EstimateTokens(line)
		if n > remaining {
			over := n - remaining
			content, _ = textutil.TruncateToTokens(content, maxInt(0, textutil.EstimateTokens(content)-over))
			line = prefix + content + suffix
			n = textutil.EstimateTokens(line)
			if n > remaining || strings.TrimSpace(content) == "" {
				p.Truncated = true
				break
			}
			cut = true
		}
		b.WriteString(line)
		remaining -= n
		candidate.fact.Content = content
		used = append(used, candidate)
		if cut {
			p.Truncated = true
		}
	}
	if len(used) == 0 {
		return p, nil
	}
	b.WriteString(footer)
	p.Text = b.String()
	p.Tokens = textutil.EstimateTokens(p.Text)
	p.Facts = make([]Fact, 0, len(used))
	for _, candidate := range used {
		p.Facts = append(p.Facts, candidate.fact)
		if len(p.Sections) == 0 || p.Sections[len(p.Sections)-1].Name != candidate.section {
			p.Sections = append(p.Sections, ProfileSection{Name: candidate.section})
		}
		last := len(p.Sections) - 1
		p.Sections[last].Facts = append(p.Sections[last].Facts, candidate.fact)
	}
	return p, nil
}

// profileSectionFor maps the slot taxonomy to a compact profile section.
// Exact identity slots rank above generic ones so a small budget keeps the
// most useful facts.
func profileSectionFor(slot string) (section string, priority, slotRank int, dynamic bool) {
	s := strings.ToLower(strings.TrimSpace(slot))
	switch {
	case s == "profile.name":
		return "Identity", 70, 100, false
	case s == "profile.location":
		return "Identity", 70, 90, false
	case s == "profile.role":
		return "Identity", 70, 80, false
	case s == "profile.employer":
		return "Identity", 70, 70, false
	case s == "profile.birthday":
		return "Identity", 70, 60, false
	case s == "profile.origin":
		return "Identity", 70, 50, false
	case strings.HasPrefix(s, "profile."):
		return "Identity", 70, 40, false
	case s == "procedure" || strings.HasPrefix(s, "procedure."):
		return "Rules", 65, 80, false
	case strings.HasPrefix(s, "favorite."):
		return "Preferences", 60, 40, false
	case strings.HasPrefix(s, "health."):
		return "Health", 50, 40, false
	case strings.HasPrefix(s, "skill."):
		return "Skills", 40, 40, false
	case strings.HasPrefix(s, "goal."), strings.HasPrefix(s, "project."), strings.HasPrefix(s, "status."):
		return "Current focus", 30, 40, true
	case strings.HasPrefix(s, "sentiment."):
		return "Opinions", 20, 40, true
	default:
		return "Recent context", 10, 0, true
	}
}
