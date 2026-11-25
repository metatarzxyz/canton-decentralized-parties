// Package uid handles Canton unique identifiers ("name::1220…").
package uid

import "strings"

// Normalize strips an optional "PAR::" prefix.
func Normalize(uid string) string {
	return strings.TrimPrefix(strings.TrimSpace(uid), "PAR::")
}

// Namespace returns the namespace fingerprint of a UID ("name::1220…" -> "1220…").
func Namespace(uid string) string {
	uid = Normalize(uid)
	if i := strings.LastIndex(uid, "::"); i >= 0 {
		return uid[i+2:]
	}
	return ""
}
