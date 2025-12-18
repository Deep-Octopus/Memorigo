package memori

import (
	"fmt"

	"memorigo/storage"
)

type Recall struct {
	m *Memori
}

func NewRecall(m *Memori) *Recall { return &Recall{m: m} }

func (r *Recall) SearchFacts(query string, limit int) ([]Fact, error) {
	if r.m.Storage == nil || r.m.Storage.Driver() == nil {
		return nil, nil
	}
	if r.m.Config.EntityID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = r.m.Config.RecallLimit
	}

	driver := r.m.Storage.Driver()
	repos, ok := driver.(storage.Repos)
	if !ok {
		return nil, fmt.Errorf("driver does not implement Repos")
	}

	// Resolve internal entity ID
	entityID, err := repos.Entity().GetByExternalID(r.m.Config.EntityID)
	if err != nil {
		// No entity yet -> no facts
		return nil, nil
	}

	queryEmbedding := embedText(query)
	embLimit := limit * 10
	if embLimit < limit {
		embLimit = limit
	}

	facts, err := repos.EntityFact().SearchByEmbedding(entityID, queryEmbedding, limit, embLimit)
	if err != nil {
		return nil, err
	}

	out := make([]Fact, 0, len(facts))
	for _, f := range facts {
		out = append(out, Fact{
			Content:      f.Content,
			Score:        f.Score,
			NumTimes:     f.NumTimes,
			DateLastTime: f.DateLastTime,
		})
	}
	return out, nil
}

