package memori

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/Deep-Octopus/Memorigo/storage"
)

// idCache memoizes immutable external-id -> internal-id mappings. The cache is
// keyed by the external value, so switching attribution or sessions can never
// reuse a stale internal id (the original implementation cached a single id on
// Config and returned it after Attribution/SetSession changed).
type idCache struct {
	mu        sync.Mutex
	entities  map[string]int64
	processes map[string]int64
	sessions  map[uuid.UUID]int64
}

func newIDCache() *idCache {
	return &idCache{
		entities:  make(map[string]int64),
		processes: make(map[string]int64),
		sessions:  make(map[uuid.UUID]int64),
	}
}

func (c *idCache) entity(ctx context.Context, repos storage.Repos, externalID string) (int64, error) {
	c.mu.Lock()
	if id, ok := c.entities[externalID]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	id, err := repos.Entity().Create(ctx, externalID)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.entities[externalID] = id
	c.mu.Unlock()
	return id, nil
}

func (c *idCache) process(ctx context.Context, repos storage.Repos, externalID string) (int64, error) {
	c.mu.Lock()
	if id, ok := c.processes[externalID]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	id, err := repos.Process().Create(ctx, externalID)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.processes[externalID] = id
	c.mu.Unlock()
	return id, nil
}

func (c *idCache) session(ctx context.Context, repos storage.Repos, sessionUUID uuid.UUID, entityID, processID *int64) (int64, error) {
	c.mu.Lock()
	if id, ok := c.sessions[sessionUUID]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	id, err := repos.Session().Create(ctx, entityID, processID, sessionUUID)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.sessions[sessionUUID] = id
	c.mu.Unlock()
	return id, nil
}
