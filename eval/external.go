package eval

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Deep-Octopus/Memorigo/memori"
)

// DefaultExternalPath is where download.sh writes the LongMemEval oracle file.
// The file is not part of the repository.
const DefaultExternalPath = "eval/datasets/longmemeval_oracle.json"

// ErrDatasetAbsent means the external file is not on disk. The builtin suite
// still runs.
var ErrDatasetAbsent = errors.New("eval: external dataset not present")

// ExternalTurn is one LongMemEval haystack message.
type ExternalTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ExternalQuestion is the subset of a LongMemEval row the sampler reads.
type ExternalQuestion struct {
	ID       string           `json:"question_id"`
	Type     string           `json:"question_type"`
	Question string           `json:"question"`
	Answer   string           `json:"answer"`
	Sessions [][]ExternalTurn `json:"haystack_sessions"`
}

// ExternalReport describes a file that may be absent. AnswerHits is a
// case-insensitive containment check on packed context. It is not a
// LongMemEval score and it does not affect the builtin pass/fail counts.
type ExternalReport struct {
	Questions   int    `json:"questions,omitempty"`
	Ran         int    `json:"ran,omitempty"`
	Scored      int    `json:"scored,omitempty"`
	AnswerHits  int    `json:"answer_hits,omitempty"`
	Abstentions int    `json:"abstentions,omitempty"`
	JudgeScored int    `json:"judge_scored,omitempty"`
	JudgeErrors int    `json:"judge_errors,omitempty"`
	Note        string `json:"note,omitempty"`
}

const externalNote = "not an official LongMemEval score; heuristic context containment only; the dataset is not in git; the judge does not change pass/fail"

// LoadLongMemEval reads a LongMemEval JSON array. A missing file returns
// ErrDatasetAbsent. Answers may be strings or numbers.
func LoadLongMemEval(path string) ([]ExternalQuestion, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrDatasetAbsent, path)
		}
		return nil, err
	}
	defer f.Close()
	return decodeLongMemEval(f)
}

func decodeLongMemEval(r io.Reader) ([]ExternalQuestion, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 1<<32))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("eval: external dataset is empty")
	}
	var rows []struct {
		QuestionID       string           `json:"question_id"`
		QuestionType     string           `json:"question_type"`
		Question         string           `json:"question"`
		Answer           json.RawMessage  `json:"answer"`
		HaystackSessions [][]ExternalTurn `json:"haystack_sessions"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("eval: external dataset: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("eval: external dataset has no questions")
	}
	out := make([]ExternalQuestion, 0, len(rows))
	for i, row := range rows {
		if strings.TrimSpace(row.QuestionID) == "" || strings.TrimSpace(row.Question) == "" {
			return nil, fmt.Errorf("eval: external question %d is missing id or text", i)
		}
		answer, err := answerString(row.Answer)
		if err != nil {
			return nil, fmt.Errorf("eval: external question %s: %w", row.QuestionID, err)
		}
		out = append(out, ExternalQuestion{
			ID:       row.QuestionID,
			Type:     row.QuestionType,
			Question: row.Question,
			Answer:   answer,
			Sessions: row.HaystackSessions,
		})
	}
	return out, nil
}

func answerString(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", fmt.Errorf("missing answer")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return "", fmt.Errorf("missing answer")
		}
		return text, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var number json.Number
	if err := dec.Decode(&number); err != nil {
		return "", fmt.Errorf("answer must be a string or number")
	}
	return number.String(), nil
}

// RunExternal samples questions in file order. limit <= 0 loads nothing into
// the sample and returns the question count. IDs ending in _abs are counted
// as abstentions and are not containment hits. judge may be nil.
func RunExternal(ctx context.Context, questions []ExternalQuestion, limit int, judge Judge) (ExternalReport, error) {
	report := ExternalReport{Questions: len(questions), Note: externalNote}
	if limit <= 0 || len(questions) == 0 {
		return report, nil
	}
	if limit > len(questions) {
		limit = len(questions)
	}
	for _, q := range questions[:limit] {
		report.Ran++
		if strings.HasSuffix(q.ID, "_abs") {
			report.Abstentions++
			continue
		}
		hit, observed, err := sampleExternal(ctx, q)
		if err != nil {
			return report, err
		}
		report.Scored++
		if hit {
			report.AnswerHits++
		}
		if judge != nil {
			result := QueryResult{Scenario: q.ID, Query: q.Question}
			applyJudge(ctx, judge, QueryCase{Query: q.Question, Want: []string{q.Answer}}, observed, &result)
			if result.JudgeScore != nil {
				report.JudgeScored++
			}
			if result.JudgeError != "" {
				report.JudgeErrors++
			}
		}
	}
	return report, nil
}

func sampleExternal(ctx context.Context, q ExternalQuestion) (bool, string, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:eval_ext_%s_%d?mode=memory&cache=shared",
		sanitize(q.ID), runCounter.Add(1)))
	if err != nil {
		return false, "", err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	m := memori.New(
		memori.WithStorageConn(db),
		memori.WithConfig(func(c *memori.Config) {
			c.LogLevel = "off"
			c.AutoInject = false
			c.Embedding.Provider = "hash"
			c.Extraction.Provider = "heuristic"
			c.Profile.Budget = 200
			c.Profile.Limit = 40
			c.Profile.IncludeDynamic = false
		}),
	)
	defer m.Close()
	if err := m.Err(); err != nil {
		return false, "", err
	}
	if err := m.TryAttribution("eval-external", "eval-process"); err != nil {
		return false, "", err
	}
	if err := seedSessions(ctx, m, q.Sessions); err != nil {
		return false, "", err
	}
	pkg, err := m.BuildContext(ctx, q.Question, memori.ContextOptions{
		TokenBudget:    800,
		FactLimit:      10,
		IncludeRaw:     true,
		IncludeProfile: true,
	})
	if err != nil {
		return false, "", err
	}
	hit := q.Answer != "" && strings.Contains(strings.ToLower(pkg.Text), strings.ToLower(q.Answer))
	return hit, pkg.Text, nil
}

func seedSessions(ctx context.Context, m *memori.Memori, sessions [][]ExternalTurn) error {
	for _, session := range sessions {
		var pending string
		hasPending := false
		flushUser := func() error {
			if !hasPending {
				return nil
			}
			hasPending = false
			return writeExchange(ctx, m, pending, "")
		}
		for _, turn := range session {
			content := strings.TrimSpace(turn.Content)
			if content == "" {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(turn.Role), "assistant") {
				user := ""
				if hasPending {
					user = pending
					hasPending = false
				}
				if err := writeExchange(ctx, m, user, content); err != nil {
					return err
				}
				continue
			}
			if err := flushUser(); err != nil {
				return err
			}
			pending = content
			hasPending = true
		}
		if err := flushUser(); err != nil {
			return err
		}
	}
	return m.Flush(ctx)
}

func writeExchange(ctx context.Context, m *memori.Memori, user, assistant string) error {
	user = strings.TrimSpace(user)
	assistant = strings.TrimSpace(assistant)
	if user == "" && assistant == "" {
		return nil
	}
	if user == "" {
		return memori.NewWriter(m).Execute(ctx, memori.ConversationPayload{
			Messages: []memori.Message{{Role: "assistant", Content: assistant}},
		})
	}
	return writeTurn(ctx, m, user, assistant)
}

func (r ExternalReport) String() string {
	return fmt.Sprintf("external sample: questions %d, ran %d, scored %d, answer hits %d, abstentions %d, judge scored %d, judge errors %d (%s)",
		r.Questions, r.Ran, r.Scored, r.AnswerHits, r.Abstentions, r.JudgeScored, r.JudgeErrors, r.Note)
}
