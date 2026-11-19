package memori

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Deep-Octopus/Memorigo/textutil"
)

// CandidateFact is a fact produced by an extractor before embedding and
// reconciliation.
type CandidateFact struct {
	Content    string
	Slot       string
	Exclusive  bool
	AssertedBy string
	Confidence float64
}

// ExtractionInput is the conversation slice considered for fact extraction.
type ExtractionInput struct {
	Messages []Message
	Response *Message
}

// ExtractionResult is the output of an extractor.
type ExtractionResult struct {
	Facts   []CandidateFact
	Summary string
}

// Extractor derives durable facts and a conversation summary from a
// conversation. Implementations must be safe for concurrent use.
type Extractor interface {
	Extract(ctx context.Context, in ExtractionInput) (ExtractionResult, error)
}

func NewExtractor(m *Memori) Extractor {
	provider := strings.ToLower(strings.TrimSpace(m.Config.Extraction.Provider))
	switch provider {
	case "off", "none", "disabled":
		return noopExtractor{}
	case "llm":
		return &LLMExtractor{m: m}
	default:
		return &HeuristicExtractor{maxFacts: m.Config.Extraction.MaxFacts}
	}
}

type noopExtractor struct{}

func (noopExtractor) Extract(context.Context, ExtractionInput) (ExtractionResult, error) {
	return ExtractionResult{}, nil
}

// HeuristicExtractor is a deterministic, LLM-free extractor. It recognises
// common self-disclosure patterns in English and Chinese and falls back to
// first-person declarative sentences. It is intentionally conservative: LLM
// extraction is the higher-recall mode.
type HeuristicExtractor struct {
	maxFacts int
}

func (e *HeuristicExtractor) Extract(_ context.Context, in ExtractionInput) (ExtractionResult, error) {
	maxFacts := e.maxFacts
	if maxFacts <= 0 {
		maxFacts = 20
	}

	var facts []CandidateFact
	seen := make(map[string]struct{})
	var userTexts []string

loop:
	for _, msg := range in.Messages {
		if msg.Role != "user" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		userTexts = append(userTexts, content)
		for _, sentence := range textutil.SplitSentences(content) {
			for _, c := range extractCandidatesFromSentence(sentence) {
				key := textutil.Key(c.Content)
				if key == "" {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				facts = append(facts, c)
				if len(facts) >= maxFacts {
					break loop
				}
			}
		}
	}

	return ExtractionResult{
		Facts:   facts,
		Summary: buildHeuristicSummary(facts, userTexts),
	}, nil
}

func extractCandidatesFromText(text string) []CandidateFact {
	var out []CandidateFact
	seen := make(map[string]struct{})
	for _, sentence := range textutil.SplitSentences(text) {
		for _, c := range extractCandidatesFromSentence(sentence) {
			key := textutil.Key(c.Content)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, c)
		}
	}
	return out
}

var (
	reName      = regexp.MustCompile(`(?i)^(?:my name is|i am called|i'm called|call me)\s+(.{1,60})$`)
	reFavorite  = regexp.MustCompile(`(?i)^my favou?rite\s+([\p{L}][\p{L}\s]{0,30}?)\s+is\s+(.{1,100})$`)
	reBirthday  = regexp.MustCompile(`(?i)^my birthday is\s+(.{1,60})$`)
	reOrigin    = regexp.MustCompile(`(?i)^i(?:'m| am) from\s+(.{1,80})$`)
	reLive      = regexp.MustCompile(`(?i)^i live in\s+(.{1,80})$`)
	reWorkAt    = regexp.MustCompile(`(?i)^i work (?:at|for)\s+(.{1,80})$`)
	reWorkIn    = regexp.MustCompile(`(?i)^i work in\s+(.{1,80})$`)
	reRole      = regexp.MustCompile(`(?i)^i(?:'m| am) (?:a|an)\s+(.{1,80})$`)
	reSentiment = regexp.MustCompile(`(?i)^i (like|love|enjoy|prefer|hate|dislike)\s+(.{1,100})$`)
	reSkill     = regexp.MustCompile(`(?i)^i (use|study|learn|know|speak)\s+(.{1,100})$`)
	reLearning  = regexp.MustCompile(`(?i)^i(?:'m| am) learning\s+(.{1,100})$`)
	reRemember  = regexp.MustCompile(`(?i)^(?:please\s+)?remember(?:\s+that)?\s+(.{2,300})$`)
	reAllergy   = regexp.MustCompile(`(?i)^i(?:'m| am) allergic to\s+(.{1,100})$`)

	reZhName      = regexp.MustCompile(`^我(?:叫|的名字是)(.{1,30})$`)
	reZhFavorite  = regexp.MustCompile(`^我(?:最)?(?:喜欢|爱|讨厌|不喜欢)的?(.{1,24}?)(?:是|为)(.{1,80})$`)
	reZhSentiment = regexp.MustCompile(`^我(?:很|挺|特别|最)?(喜欢|爱|讨厌|不喜欢)(.{1,100})$`)
	reZhLive      = regexp.MustCompile(`^我(?:现在)?住在(.{1,80})$`)
	reZhOrigin    = regexp.MustCompile(`^我来自(.{1,80})$`)
	reZhWork      = regexp.MustCompile(`^我在(.{1,60})(?:工作|上班)$`)
	reZhBirthday  = regexp.MustCompile(`^我的生日是(.{1,40})$`)
	reZhRole      = regexp.MustCompile(`^我(?:的)?(?:职业|工作)是(.{1,60})$`)
	reZhRole2     = regexp.MustCompile(`^我是(?:一[个名位])?(.{2,40})$`)
	reZhSkill     = regexp.MustCompile(`^我(?:会|学过|正在学|在学习)(.{1,60})$`)
	reZhRemember  = regexp.MustCompile(`^请?记住[：:，,]?(.{2,300})$`)
	reZhAllergy   = regexp.MustCompile(`^我对(.{1,60})过敏$`)
)

var occupationSuffixes = []string{
	"程序员", "工程师", "设计师", "分析师", "研究员", "产品经理", "项目经理",
	"老师", "教师", "学生", "医生", "护士", "律师", "会计", "作家", "记者",
	"导演", "顾问", "销售", "运营", "创始人", "经理", "总监", "老板",
	"员", "师", "生", "家", "工", "长",
	"engineer", "developer", "designer", "manager", "doctor", "nurse",
	"teacher", "student", "lawyer", "founder", "analyst", "consultant",
	"ceo", "cto", "cfo", "cmo",
}

func extractCandidatesFromSentence(sentence string) []CandidateFact {
	s := cleanSentence(sentence)
	if s == "" || utf8.RuneCountInString(s) < 4 || utf8.RuneCountInString(s) > 500 {
		return nil
	}
	if textutil.IsQuestion(s) {
		return nil
	}

	if c, ok := matchSpecificFact(s); ok {
		return []CandidateFact{c}
	}
	if c, ok := generalFact(s); ok {
		return []CandidateFact{c}
	}
	return nil
}

func matchSpecificFact(s string) (CandidateFact, bool) {
	if m := reName.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.name", true), true
	}
	if m := reFavorite.FindStringSubmatch(s); m != nil {
		topic := slotKey(m[1])
		if topic != "" {
			return newCandidate(s, "favorite."+topic, true), true
		}
	}
	if m := reBirthday.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.birthday", true), true
	}
	if m := reOrigin.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.origin", true), true
	}
	if m := reLive.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.location", true), true
	}
	if m := reWorkAt.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.employer", true), true
	}
	if m := reWorkIn.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.workplace", true), true
	}
	if m := reRole.FindStringSubmatch(s); m != nil && plausibleRole(m[1]) {
		return newCandidate(s, "profile.role", true), true
	}
	if m := reSentiment.FindStringSubmatch(s); m != nil {
		obj := slotKey(m[2])
		if obj != "" {
			return newCandidate(s, "sentiment."+obj, true), true
		}
	}
	if m := reLearning.FindStringSubmatch(s); m != nil {
		obj := slotKey(m[1])
		if obj != "" {
			return newCandidate(s, "skill."+obj, false), true
		}
	}
	if m := reSkill.FindStringSubmatch(s); m != nil {
		obj := slotKey(m[2])
		if obj != "" {
			return newCandidate(s, "skill."+obj, false), true
		}
	}
	if m := reAllergy.FindStringSubmatch(s); m != nil {
		obj := slotKey(m[1])
		if obj != "" {
			return newCandidate(s, "health.allergy."+obj, true), true
		}
	}
	if m := reRemember.FindStringSubmatch(s); m != nil {
		content := cleanSentence(m[1])
		if content != "" {
			return CandidateFact{Content: content}, true
		}
	}

	// Chinese patterns.
	if m := reZhName.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.name", true), true
	}
	if m := reZhFavorite.FindStringSubmatch(s); m != nil {
		topic := slotKey(m[1])
		if topic != "" {
			return newCandidate(s, "favorite."+topic, true), true
		}
	}
	if m := reZhBirthday.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.birthday", true), true
	}
	if m := reZhOrigin.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.origin", true), true
	}
	if m := reZhLive.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.location", true), true
	}
	if m := reZhWork.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.workplace", true), true
	}
	if m := reZhRole.FindStringSubmatch(s); m != nil {
		return newCandidate(s, "profile.role", true), true
	}
	if m := reZhRole2.FindStringSubmatch(s); m != nil && plausibleRole(m[1]) {
		return newCandidate(s, "profile.role", true), true
	}
	if m := reZhSentiment.FindStringSubmatch(s); m != nil {
		obj := slotKey(m[2])
		if obj != "" {
			return newCandidate(s, "sentiment."+obj, true), true
		}
	}
	if m := reZhSkill.FindStringSubmatch(s); m != nil {
		obj := slotKey(m[1])
		if obj != "" {
			return newCandidate(s, "skill."+obj, false), true
		}
	}
	if m := reZhAllergy.FindStringSubmatch(s); m != nil {
		obj := slotKey(m[1])
		if obj != "" {
			return newCandidate(s, "health.allergy."+obj, true), true
		}
	}
	if m := reZhRemember.FindStringSubmatch(s); m != nil {
		content := cleanSentence(m[1])
		if content != "" {
			return CandidateFact{Content: content}, true
		}
	}
	return CandidateFact{}, false
}

func newCandidate(s, slot string, exclusive bool) CandidateFact {
	return CandidateFact{Content: cleanSentence(s), Slot: slot, Exclusive: exclusive}
}

func plausibleRole(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return false
	}
	// Reject "I am a bit tired", "I am a little sad", etc.
	for _, prefix := range []string{"bit ", "little ", "very ", "so ", "too ", "really ", "quite "} {
		if strings.HasPrefix(v, prefix) {
			return false
		}
	}
	// English roles are long-tailed; an article plus a noun phrase is a
	// reasonable heuristic. Chinese roles are validated by suffix instead.
	hasLatin := false
	for _, r := range v {
		if r < unicode.MaxASCII && unicode.IsLetter(r) {
			hasLatin = true
			break
		}
	}
	if hasLatin {
		return len(v) <= 60
	}
	for _, suffix := range occupationSuffixes {
		if strings.HasSuffix(v, suffix) {
			return true
		}
	}
	return false
}

func generalFact(s string) (CandidateFact, bool) {
	lower := strings.ToLower(s)
	normalized := textutil.Normalize(s)
	if _, bad := conversationalNoise[normalized]; bad {
		return CandidateFact{}, false
	}

	looksEnglishFirstPerson := strings.HasPrefix(lower, "i ") ||
		strings.HasPrefix(lower, "i'm ") ||
		strings.HasPrefix(lower, "i am ") ||
		strings.HasPrefix(lower, "my ")
	looksChineseFirstPerson := strings.HasPrefix(s, "我")

	if !looksEnglishFirstPerson && !looksChineseFirstPerson {
		return CandidateFact{}, false
	}
	for _, bad := range []string{
		"i want you to", "i need you to", "i asked you", "i will ask",
		"我想让你", "我需要你", "你能不能", "帮我", "请帮我",
	} {
		if strings.HasPrefix(lower, bad) {
			return CandidateFact{}, false
		}
	}
	if utf8.RuneCountInString(s) < 6 {
		return CandidateFact{}, false
	}
	return CandidateFact{Content: s}, true
}

var conversationalNoise = map[string]struct{}{
	"i am fine": {}, "i'm fine": {}, "i am good": {}, "i'm good": {},
	"i am ok": {}, "i'm ok": {}, "i am okay": {}, "i'm okay": {},
	"i am sorry": {}, "i'm sorry": {}, "i think": {}, "i guess": {},
	"i mean": {}, "i see": {}, "i know": {}, "i am here": {}, "i'm here": {},
	"i am back": {}, "i'm back": {},
	"我很好": {}, "我没事": {}, "我知道了": {}, "我明白了": {},
}

func cleanSentence(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	s = strings.Trim(s, "\"'“”‘’` ")
	s = strings.TrimRight(s, ".!?。！？;；,，")
	return strings.TrimSpace(s)
}

func slotKey(s string) string {
	var b strings.Builder
	lastUnderscore := false
	count := 0
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if count >= 60 {
			break
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
			count++
		case r == ' ' || r == '_' || r == '-' || r == '.':
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
				count++
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func isExclusiveSlot(slot string) bool {
	for _, prefix := range []string{"profile.", "favorite.", "sentiment.", "health.allergy."} {
		if strings.HasPrefix(slot, prefix) {
			return true
		}
	}
	return false
}

func buildHeuristicSummary(facts []CandidateFact, userTexts []string) string {
	var parts []string
	for _, f := range facts {
		if len(parts) >= 3 {
			break
		}
		parts = append(parts, f.Content)
	}
	if len(parts) == 0 && len(userTexts) > 0 {
		return textutil.Truncate(userTexts[0], 160)
	}
	if len(parts) == 0 {
		return ""
	}
	return textutil.Truncate("用户提到："+strings.Join(parts, "；"), 400)
}

// LLMExtractor uses the registered OpenAI-compatible client with a strict JSON
// prompt. Calls go through the raw client, so extraction requests are never
// persisted as conversation messages or auto-injected.
type LLMExtractor struct {
	m *Memori
}

func (e *LLMExtractor) Extract(ctx context.Context, in ExtractionInput) (ExtractionResult, error) {
	client := e.m.getOpenAIClient()
	if client == nil {
		return (&HeuristicExtractor{maxFacts: e.m.Config.Extraction.MaxFacts}).Extract(ctx, in)
	}

	model := strings.TrimSpace(e.m.Config.Extraction.Model)
	if model == "" {
		model = e.m.lastUsedModel()
	}
	if model == "" {
		e.m.logger.Warn("llm extraction requested but no model configured; falling back to heuristic")
		return (&HeuristicExtractor{maxFacts: e.m.Config.Extraction.MaxFacts}).Extract(ctx, in)
	}

	transcript := buildTranscript(in, 24, 2000)
	if strings.TrimSpace(transcript) == "" {
		return ExtractionResult{}, nil
	}

	maxFacts := e.m.Config.Extraction.MaxFacts
	if maxFacts <= 0 {
		maxFacts = 20
	}
	system := fmt.Sprintf(`You are a long-term memory extraction engine.
Extract durable facts explicitly stated by the USER about themselves.
Return ONLY a JSON object with this exact shape:
{"facts":[{"content":"self-contained sentence in the user's language","slot":"","exclusive":false}],"summary":"1-3 sentence summary"}
Rules:
- Extract at most %d facts. Prefer quality over quantity.
- Only user-stated durable facts: identity, preferences, relationships, skills, goals, constraints, important events.
- Never extract questions, requests, greetings, temporary states, assistant claims, or anything the user did not state.
- slot must be one of: "", "profile.name", "profile.location", "profile.origin", "profile.birthday", "profile.employer", "profile.workplace", "profile.role", "favorite.<topic>", "sentiment.<object>", "skill.<object>", "health.allergy.<object>".
- exclusive=true when a newer fact in the same slot should replace the old one (profile attributes, favorites, sentiments). Otherwise false.
- Never store passwords, API keys, tokens, full payment card numbers, or other secrets.
- summary should describe the conversation's key points, not the extraction process.`, maxFacts)

	temperature := 0.0
	req := ChatCompletionsRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: transcript},
		},
		Temperature: &temperature,
	}

	resp, err := client.ChatCompletionsCreate(ctx, req)
	if err != nil {
		return ExtractionResult{}, fmt.Errorf("llm extraction call failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return ExtractionResult{}, errors.New("llm extraction returned no choices")
	}
	raw := resp.Choices[0].Message.Content

	var parsed struct {
		Facts []struct {
			Content   string `json:"content"`
			Slot      string `json:"slot"`
			Exclusive bool   `json:"exclusive"`
		} `json:"facts"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(raw)), &parsed); err != nil {
		return ExtractionResult{}, fmt.Errorf("parse llm extraction json: %w", err)
	}

	res := ExtractionResult{Summary: textutil.Truncate(strings.TrimSpace(parsed.Summary), 500)}
	seen := make(map[string]struct{})
	for _, f := range parsed.Facts {
		content := cleanSentence(f.Content)
		if content == "" || utf8.RuneCountInString(content) > 400 {
			continue
		}
		key := textutil.Key(content)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		slot := sanitizeSlot(f.Slot)
		res.Facts = append(res.Facts, CandidateFact{
			Content:   content,
			Slot:      slot,
			Exclusive: f.Exclusive || isExclusiveSlot(slot),
		})
		if len(res.Facts) >= maxFacts {
			break
		}
	}
	return res, nil
}

func buildTranscript(in ExtractionInput, maxMessages, maxChars int) string {
	var b strings.Builder
	msgs := in.Messages
	if len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "user"
		}
		content := textutil.Truncate(strings.TrimSpace(m.Content), maxChars)
		if content == "" {
			continue
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}
	if in.Response != nil {
		content := textutil.Truncate(strings.TrimSpace(in.Response.Content), maxChars)
		if content != "" {
			b.WriteString("assistant: ")
			b.WriteString(content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func sanitizeSlot(slot string) string {
	slot = strings.ToLower(strings.TrimSpace(slot))
	if slot == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range slot {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' || r == ':':
			b.WriteRune(r)
		default:
			return ""
		}
	}
	out := b.String()
	if utf8.RuneCountInString(out) > 100 {
		return ""
	}
	allowedPrefixes := []string{
		"profile.", "favorite.", "sentiment.", "skill.", "health.allergy.",
	}
	for _, p := range allowedPrefixes {
		if strings.HasPrefix(out, p) {
			return out
		}
	}
	return ""
}
