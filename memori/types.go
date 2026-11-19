package memori

import "time"

// Fact is a recalled long-term memory fact.
type Fact struct {
	ID             int64      `json:"id"`
	Content        string     `json:"content"`
	Slot           string     `json:"slot,omitempty"`
	Score          float64    `json:"score"`
	Similarity     float64    `json:"similarity,omitempty"`
	LexicalScore   float64    `json:"lexical_score,omitempty"`
	NumTimes       int64      `json:"num_times"`
	DateLastTime   time.Time  `json:"date_last_time,omitempty"`
	ConversationID *int64     `json:"conversation_id,omitempty"`
	ValidFrom      time.Time  `json:"valid_from,omitempty"`
	ValidTo        *time.Time `json:"valid_to,omitempty"`
	MessageID      *int64     `json:"message_id,omitempty"`
	Confidence     float64    `json:"confidence,omitempty"`
	AssertedBy     string     `json:"asserted_by,omitempty"`
}

// MemoryContext is the formatted memory block plus the underlying facts.
type MemoryContext struct {
	Facts []Fact
	Text  string
}

// Stats is a point-in-time snapshot of memory state.
type Stats struct {
	EntityID          string
	TotalFacts        int64
	ActiveFacts       int64
	PendingJobs       int64
	EmbeddingProvider string
	EmbeddingModel    string
	EmbeddingDim      int
	StorageDialect    string
}
