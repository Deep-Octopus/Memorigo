package storage

import "strings"

// scopeMark prefixes an external id that belongs to an org/app pair.
// The user-facing entity id is stored after the marker so two tenants can
// reuse the same entity id without sharing rows.
const scopeMark = "memorigo-scope\x1e"

// ScopedExternalID returns the storage key for an entity. An empty org and
// app keep the historical key, so existing databases stay addressable.
func ScopedExternalID(org, app, entity string) string {
	if org == "" && app == "" {
		return entity
	}
	return scopeMark + org + "\x1e" + app + "\x1e" + entity
}

// ScopeParts splits a storage key into org, app and the user-facing entity id.
func ScopeParts(externalID string) (org, app, entity string) {
	if !strings.HasPrefix(externalID, scopeMark) {
		return "", "", externalID
	}
	parts := strings.SplitN(strings.TrimPrefix(externalID, scopeMark), "\x1e", 3)
	if len(parts) != 3 {
		return "", "", externalID
	}
	return parts[0], parts[1], parts[2]
}
