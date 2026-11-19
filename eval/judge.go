package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Judge scores one question against text the harness already produced.
// A judge must not be used to decide Passed, and memori's write path does not
// call this package.
type Judge interface {
	Score(ctx context.Context, in JudgeInput) (JudgeResult, error)
}

// JudgeInput is the text sent to an opt-in judge.
type JudgeInput struct {
	Question   string
	Expected   string
	Hypothesis string
}

// JudgeResult is a score in [0, 1]. The harness clamps values outside that range.
type JudgeResult struct {
	Score float64
}

type httpJudge struct {
	url    string
	apiKey string
	model  string
	client *http.Client
}

// NewHTTPJudge posts a JSON body and expects {"score": <number>}.
// Extra response fields are ignored. A non-200 status is an error.
func NewHTTPJudge(url, apiKey, model string) Judge {
	return &httpJudge{
		url:    strings.TrimSpace(url),
		apiKey: strings.TrimSpace(apiKey),
		model:  strings.TrimSpace(model),
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (j *httpJudge) Score(ctx context.Context, in JudgeInput) (JudgeResult, error) {
	payload := struct {
		Question   string `json:"question"`
		Expected   string `json:"expected"`
		Hypothesis string `json:"hypothesis"`
		Model      string `json:"model,omitempty"`
	}{
		Question:   in.Question,
		Expected:   in.Expected,
		Hypothesis: in.Hypothesis,
		Model:      j.model,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return JudgeResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.url, bytes.NewReader(body))
	if err != nil {
		return JudgeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if j.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.apiKey)
	}
	resp, err := j.client.Do(req)
	if err != nil {
		return JudgeResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return JudgeResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return JudgeResult{}, fmt.Errorf("eval: judge HTTP %d", resp.StatusCode)
	}
	var out struct {
		Score *float64 `json:"score"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return JudgeResult{}, fmt.Errorf("eval: judge response: %w", err)
	}
	if out.Score == nil {
		return JudgeResult{}, fmt.Errorf("eval: judge response missing score")
	}
	return JudgeResult{Score: *out.Score}, nil
}

// JudgeFromEnv builds the opt-in judge. Unset, empty, or "off" returns nil.
// "http" uses MEMORI_EVAL_JUDGE_URL. Any other http(s) value is the URL.
// MEMORI_EVAL_JUDGE_API_KEY and MEMORI_EVAL_JUDGE_MODEL are optional.
func JudgeFromEnv() (Judge, error) {
	mode := strings.TrimSpace(os.Getenv("MEMORI_EVAL_JUDGE"))
	switch strings.ToLower(mode) {
	case "", "0", "off", "false", "no":
		return nil, nil
	}
	url := mode
	if strings.EqualFold(mode, "http") {
		url = strings.TrimSpace(os.Getenv("MEMORI_EVAL_JUDGE_URL"))
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("eval: MEMORI_EVAL_JUDGE must be off, http, or an http(s) URL")
	}
	return NewHTTPJudge(url, os.Getenv("MEMORI_EVAL_JUDGE_API_KEY"), os.Getenv("MEMORI_EVAL_JUDGE_MODEL")), nil
}

func applyJudge(ctx context.Context, judge Judge, q QueryCase, observed string, result *QueryResult) {
	if judge == nil || result == nil {
		return
	}
	expected := strings.Join(q.Want, "\n")
	if q.Context != nil && expected == "" {
		expected = strings.Join(q.Context.WantText, "\n")
	}
	scored, err := judge.Score(ctx, JudgeInput{
		Question:   q.Query,
		Expected:   expected,
		Hypothesis: observed,
	})
	if err != nil {
		result.JudgeError = err.Error()
		return
	}
	score := scored.Score
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	result.JudgeScore = &score
}
