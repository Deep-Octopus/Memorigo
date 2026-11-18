package eval

import (
	"context"
	"fmt"

	"github.com/Deep-Octopus/Memorigo/memori"
)

// Builtin returns a deterministic suite that covers the core memory loop:
// extraction, supersession, isolation, forgetting, bilingual recall, token
// budgeting and raw-evidence fallback.
func Builtin() []Scenario {
	return []Scenario{
		{
			Name:        "basic-preference",
			Description: "A single stated preference is recallable.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				_, err := m.Remember("My favorite color is blue")
				return err
			},
			Queries: []QueryCase{{Query: "favorite color", Want: []string{"blue"}}},
		},
		{
			Name:        "preference-update",
			Description: "A newer favorite replaces the old active fact.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				if _, err := m.Remember("My favorite color is blue"); err != nil {
					return err
				}
				_, err := m.Remember("My favorite color is red")
				return err
			},
			Queries: []QueryCase{{
				Query: "favorite color",
				Want:  []string{"red"},
				Avoid: []string{"blue"},
			}},
		},
		{
			Name:        "relocation",
			Description: "A new home city supersedes the previous profile.location.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				if _, err := m.Remember("I live in Shanghai"); err != nil {
					return err
				}
				_, err := m.Remember("I live in Beijing")
				return err
			},
			Queries: []QueryCase{{
				Query: "where do I live",
				Want:  []string{"Beijing"},
				Avoid: []string{"Shanghai"},
			}},
		},
		{
			Name:        "session-continuity",
			Description: "Facts survive a new session for the same entity.",
			Seed: func(ctx context.Context, m *memori.Memori) error {
				if err := writeTurn(ctx, m, "I live in Shanghai", "Got it."); err != nil {
					return err
				}
				m.NewSession()
				return writeTurn(ctx, m, "I also like tea", "Noted.")
			},
			Queries: []QueryCase{{
				Query: "where do I live",
				Want:  []string{"Shanghai"},
			}},
		},
		{
			Name:        "forget",
			Description: "Forget soft-deletes a fact and excludes it from recall.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				id, err := m.Remember("I live in Shanghai")
				if err != nil {
					return err
				}
				return m.Forget(id)
			},
			Queries: []QueryCase{{
				Query:       "where do I live",
				ExpectEmpty: true,
			}},
		},
		{
			Name:        "noise-resistance",
			Description: "Frequent unrelated facts do not outrank the relevant one.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				for _, fact := range []string{
					"I like coffee",
					"The weather is nice today",
					"I enjoy hiking on weekends",
					"My favorite movie is Arrival",
					"I live in Shanghai",
				} {
					if _, err := m.Remember(fact); err != nil {
						return err
					}
				}
				return nil
			},
			Queries: []QueryCase{{
				Query: "where do I live",
				Want:  []string{"Shanghai"},
			}},
		},
		{
			Name:        "chinese-recall",
			Description: "Chinese facts are extracted and recalled.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				_, err := m.Remember("我最喜欢的颜色是蓝色")
				return err
			},
			Queries: []QueryCase{{
				Query: "最喜欢的颜色",
				Want:  []string{"蓝色"},
			}},
		},
		{
			Name:        "entity-isolation",
			Description: "Switching attribution never leaks another entity's facts.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				if _, err := m.Remember("My favorite color is blue"); err != nil {
					return err
				}
				if err := m.TryAttribution("other-user", "eval-process"); err != nil {
					return err
				}
				if _, err := m.Remember("My favorite color is green"); err != nil {
					return err
				}
				return m.TryAttribution("eval-user", "eval-process")
			},
			Queries: []QueryCase{{
				Query: "favorite color",
				Want:  []string{"blue"},
				Avoid: []string{"green"},
			}},
		},
		{
			Name:        "token-budget",
			Description: "Context assembly respects the token budget and emits citations.",
			Seed: func(_ context.Context, m *memori.Memori) error {
				if _, err := m.Remember("My favorite color is blue"); err != nil {
					return err
				}
				_, err := m.Remember("I live in Shanghai")
				return err
			},
			Queries: []QueryCase{{
				Query: "favorite color",
				Context: &ContextCheck{
					Budget:    100,
					WantText:  []string{"blue", "[fact:"},
					WantKinds: []string{"fact"},
					MaxTokens: 100,
				},
			}},
		},
		{
			Name:         "raw-evidence",
			Description:  "When extraction is off, context can fall back to raw messages.",
			NoExtraction: true,
			Seed: func(ctx context.Context, m *memori.Memori) error {
				return writeTurn(ctx, m,
					"My sister's name is Alice and she lives in Paris",
					"Thanks for sharing.")
			},
			Queries: []QueryCase{{
				Query: "sister name",
				Context: &ContextCheck{
					Budget:     300,
					IncludeRaw: true,
					WantText:   []string{"Alice", "[msg:"},
					WantKinds:  []string{"message"},
					MaxTokens:  300,
				},
			}},
		},
		{
			Name:         "cross-session-evidence",
			Description:  "Raw evidence is retrievable across sessions for the same entity.",
			NoExtraction: true,
			Seed: func(ctx context.Context, m *memori.Memori) error {
				if err := writeTurn(ctx, m,
					"My sister's name is Alice and she lives in Paris",
					"Thanks for sharing."); err != nil {
					return err
				}
				m.NewSession()
				return writeTurn(ctx, m, "My brother's name is Bob", "Noted.")
			},
			Queries: []QueryCase{{
				Query: "sister name",
				Context: &ContextCheck{
					Budget:     300,
					IncludeRaw: true,
					WantText:   []string{"Alice", "[msg:"},
					WantKinds:  []string{"message"},
					MaxTokens:  300,
				},
			}},
		},
	}
}

func writeTurn(ctx context.Context, m *memori.Memori, user, assistant string) error {
	payload := memori.ConversationPayload{
		Messages: []memori.Message{{Role: "user", Content: user}},
	}
	if assistant != "" {
		payload.Response = &memori.Message{Role: "assistant", Content: assistant}
	}
	if err := memori.NewWriter(m).Execute(ctx, payload); err != nil {
		return fmt.Errorf("write turn: %w", err)
	}
	return nil
}
