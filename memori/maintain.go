package memori

import (
	"context"
	"errors"
	"time"

	"github.com/Deep-Octopus/Memorigo/storage"
)

// MaintainOptions controls a reversible forgetting pass. TTL is measured from
// date_last_time. Facts mentioned at least ProtectTimes times are kept.
// A non-positive TTL does nothing, so forgetting stays opt-in.
type MaintainOptions struct {
	DryRun       bool
	TTL          time.Duration
	ProtectTimes int64
	Now          time.Time
}

// MaintainReport lists facts a pass expired. DryRun reports the same ids
// without writing.
type MaintainReport struct {
	Expired []int64
	DryRun  bool
}

// Maintain expires stale, rarely repeated facts. Expired rows stay in the
// database and can be restored. Automatic extraction will not resurrect them;
// explicit Remember and Restore will.
func (m *Memori) Maintain(opts MaintainOptions) (MaintainReport, error) {
	report := MaintainReport{DryRun: opts.DryRun}
	if opts.TTL <= 0 {
		return report, nil
	}
	if m.closed.Load() {
		return report, ErrClosed
	}
	if err := m.Err(); err != nil {
		return report, err
	}
	repos, err := m.repos()
	if err != nil {
		return report, err
	}
	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return report, ErrNoAttribution
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()
	entityID, err := repos.Entity().GetByExternalID(ctx, entityKey(snap))
	if errors.Is(err, storage.ErrNotFound) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	ids, err := repos.EntityFact().Expirable(ctx, entityID, now.Add(-opts.TTL), opts.ProtectTimes)
	if err != nil {
		return report, err
	}
	report.Expired = ids
	if opts.DryRun || len(ids) == 0 {
		return report, nil
	}
	unlock := m.Augmentation.lockEntity(entityID)
	defer unlock()
	if err := repos.EntityFact().Expire(ctx, entityID, ids, now); err != nil {
		return report, err
	}
	return report, nil
}

// Restore reactivates one expired fact. It does not reopen a fact that was
// superseded by a newer slot value or deleted by Forget.
func (m *Memori) Restore(factID int64) error {
	if m.closed.Load() {
		return ErrClosed
	}
	if err := m.Err(); err != nil {
		return err
	}
	repos, err := m.repos()
	if err != nil {
		return err
	}
	snap := m.Config.snapshot()
	if snap.EntityID == "" {
		return ErrNoAttribution
	}
	ctx, cancel := context.WithTimeout(context.Background(), snap.OpTimeout)
	defer cancel()
	entityID, err := repos.Entity().GetByExternalID(ctx, entityKey(snap))
	if err != nil {
		return err
	}
	unlock := m.Augmentation.lockEntity(entityID)
	defer unlock()
	return repos.EntityFact().Restore(ctx, entityID, factID)
}
