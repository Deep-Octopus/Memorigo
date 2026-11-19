package memori

import "github.com/Deep-Octopus/Memorigo/storage"

func entityKey(snap configSnapshot) string {
	return storage.ScopedExternalID(snap.OrgID, snap.AppID, snap.EntityID)
}
