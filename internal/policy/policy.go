// Package policy decides whether this participant authorizes a hosting proposal.
package policy

import (
	"fmt"
	"strings"

	"github.com/metatarzxyz/canton-decentralized-parties/internal/admin"
	"github.com/metatarzxyz/canton-decentralized-parties/internal/config"
	"github.com/metatarzxyz/canton-decentralized-parties/internal/uid"
)

// Decision is the outcome for one proposal.
type Decision int

const (
	// Approve: authorize the proposal.
	Approve Decision = iota
	// Reject: never authorize it (logged once).
	Reject
	// AlreadySigned: this participant signed it before.
	AlreadySigned
)

// Policy evaluates proposals for participant Self.
type Policy struct {
	Self string // normalized participant UID
	Cfg  config.Policy

	allowedParticipants map[string]bool // empty: every co-host is allowed
	allowedPermissions  map[string]bool
}

// New builds the policy.
func New(self string, cfg config.Policy) *Policy {
	p := &Policy{Self: uid.Normalize(self), Cfg: cfg, allowedParticipants: map[string]bool{}, allowedPermissions: map[string]bool{}}
	for _, u := range cfg.AllowedParticipantsUID {
		p.allowedParticipants[uid.Normalize(u)] = true
	}
	for _, perm := range cfg.AllowedPermissions {
		p.allowedPermissions[strings.ToLower(perm)] = true
	}
	return p
}

// signedBy reports whether a participant's namespace key signed the proposal.
func signedBy(signatures []string, participantUID string) bool {
	ns := uid.Namespace(participantUID)
	for _, s := range signatures {
		if ns != "" && s == ns {
			return true
		}
	}
	return false
}

// isAllowed: allowed_participants_uid is empty (all allowed) or lists uid.
func (p *Policy) isAllowed(participantUID string) bool {
	return len(p.allowedParticipants) == 0 || p.allowedParticipants[participantUID]
}

// Evaluate returns the decision and, for Approve, the initiator UID; for
// Reject, the reason.
func (p *Policy) Evaluate(pr *admin.Proposal) (Decision, string) {
	if signedBy(pr.SignedBy, p.Self) {
		return AlreadySigned, ""
	}
	if !pr.Replace {
		return Reject, "not a hosting addition (REMOVE)"
	}
	var self *admin.Host
	confirming := 0
	for i := range pr.Hosts {
		h := &pr.Hosts[i]
		if h.UID == p.Self {
			self = h
		}
		if !p.allowedPermissions[h.Permission] {
			return Reject, fmt.Sprintf("host %s has permission %q", h.UID, h.Permission)
		}
		if h.Permission == "confirmation" {
			confirming++
		}
	}
	if self == nil {
		return Reject, "this participant is not a host"
	}
	if len(pr.Hosts) < 2 {
		return Reject, "not multi-hosted"
	}
	if len(pr.Hosts) > p.Cfg.MaxHosts {
		return Reject, fmt.Sprintf("%d hosts exceed max_hosts %d", len(pr.Hosts), p.Cfg.MaxHosts)
	}
	if pr.Threshold < 1 || int(pr.Threshold) > confirming {
		return Reject, fmt.Sprintf("threshold %d invalid for %d confirming hosts", pr.Threshold, confirming)
	}
	// Only a party's first hosting mapping: adding a host to an existing party
	// needs its ACS to be copied first, which is never automated.
	if pr.Serial != 1 {
		return Reject, fmt.Sprintf("serial %d: changes to existing parties need an ACS copy and are not automated", pr.Serial)
	}
	// Only external parties (own signing keys), so the party's consent below is meaningful.
	if !pr.External {
		return Reject, "party has no signing keys (not an external party)"
	}
	// The party itself consented: its namespace key signed the proposal.
	if !signedBy(pr.SignedBy, pr.Party) {
		return Reject, "not signed by the party"
	}
	// An allowed co-host started it: it already signed the proposal.
	for _, h := range pr.Hosts {
		if h.UID != p.Self && p.isAllowed(h.UID) && signedBy(pr.SignedBy, h.UID) {
			return Approve, h.UID
		}
	}
	if len(p.allowedParticipants) > 0 {
		return Reject, "not signed by a co-host in allowed_participants_uid"
	}
	return Reject, "not signed by any co-host"
}
