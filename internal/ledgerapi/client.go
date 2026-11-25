// Package ledgerapi calls the Canton JSON Ledger API endpoints used by
// "cdp dars list": participant ID and package vetting listing.
package ledgerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a JSON Ledger API client.
type Client struct {
	BaseURL string // e.g. http://localhost:7575 or https://host/api/json-api
	Token   string // bearer token (optional)
	HTTP    *http.Client
}

// New returns a client for baseURL.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// APIError is a non-2xx answer.
type APIError struct {
	Status int
	Code   string // Canton error code, e.g. "NO_PREFERRED_PACKAGES_FOUND"
	Cause  string
	Body   string
}

func (e *APIError) Error() string {
	if e.Code != "" || e.Cause != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Cause)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		e := &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
		var ce struct {
			Code  string `json:"code"`
			Cause string `json:"cause"`
		}
		if json.Unmarshal(raw, &ce) == nil {
			e.Code, e.Cause = ce.Code, ce.Cause
		}
		if len(e.Body) > 500 {
			e.Body = e.Body[:500] + "…"
		}
		return e
	}
	return json.Unmarshal(raw, out)
}

// VettedPackage is one package vetted by a participant.
type VettedPackage struct {
	PackageID           string `json:"packageId"`
	PackageName         string `json:"packageName"`    // empty unless uploaded on the queried participant
	PackageVersion      string `json:"packageVersion"` // idem
	ValidFromInclusive  string `json:"validFromInclusive"`
	ValidUntilExclusive string `json:"validUntilExclusive"`
}

// ActiveAt reports whether the vetting is in effect at t.
func (v VettedPackage) ActiveAt(t time.Time) bool {
	if v.ValidFromInclusive != "" {
		if from, err := time.Parse(time.RFC3339Nano, v.ValidFromInclusive); err == nil && t.Before(from) {
			return false
		}
	}
	if v.ValidUntilExclusive != "" {
		if until, err := time.Parse(time.RFC3339Nano, v.ValidUntilExclusive); err == nil && !t.Before(until) {
			return false
		}
	}
	return true
}

// VettedPackages is one participant's vetting on one synchronizer.
type VettedPackages struct {
	Packages       []VettedPackage `json:"packages"`
	ParticipantID  string          `json:"participantId"`
	SynchronizerID string          `json:"synchronizerId"`
	TopologySerial int             `json:"topologySerial"`
}

// ListVettedPackages lists the vetting of participantIDs (all pages), filtered
// by package-name prefixes and synchronizer when given.
func (c *Client) ListVettedPackages(ctx context.Context, participantIDs, prefixes []string, synchronizerID string) ([]VettedPackages, error) {
	var synchronizerIDs []string
	if synchronizerID != "" {
		synchronizerIDs = []string{synchronizerID}
	}

	// Canton 3.7 accepts a request with only the fields that are set (even "{}"),
	// and rejects some empty ones. Some older versions fail with
	// PROTO_DESERIALIZATION_FAILURE when a filter or page field is missing, so on
	// that error we retry once with every field present.
	all, err := c.listVettedPackages(ctx, participantIDs, prefixes, synchronizerIDs, false)
	if ae, ok := err.(*APIError); ok && ae.Code == "PROTO_DESERIALIZATION_FAILURE" {
		if all2, err2 := c.listVettedPackages(ctx, participantIDs, prefixes, synchronizerIDs, true); err2 == nil {
			return all2, nil
		}
	}
	return all, err
}

func (c *Client) listVettedPackages(ctx context.Context, participantIDs, prefixes, synchronizerIDs []string, full bool) ([]VettedPackages, error) {
	nonNil := func(v []string) []string {
		if v == nil {
			return []string{}
		}
		return v
	}
	build := func(pageToken string) map[string]any {
		req := map[string]any{}
		meta := map[string]any{}
		topo := map[string]any{}
		if full {
			meta["packageIds"] = []string{}
			meta["packageNamePrefixes"] = nonNil(prefixes)
			topo["participantIds"] = nonNil(participantIDs)
			topo["synchronizerIds"] = nonNil(synchronizerIDs)
			req["packageMetadataFilter"] = meta
			req["topologyStateFilter"] = topo
			req["pageToken"] = pageToken
			req["pageSize"] = 0
			return req
		}
		if len(prefixes) > 0 {
			meta["packageNamePrefixes"] = prefixes
			req["packageMetadataFilter"] = meta
		}
		if len(participantIDs) > 0 {
			topo["participantIds"] = participantIDs
		}
		if len(synchronizerIDs) > 0 {
			topo["synchronizerIds"] = synchronizerIDs
		}
		if len(topo) > 0 {
			req["topologyStateFilter"] = topo
		}
		if pageToken != "" {
			req["pageToken"] = pageToken
		}
		// pageSize is left to the server: an explicit value above its maximum
		// (packageFeature.maxVettedPackagesPageSize in /v2/version, 100 on 3.7) is rejected.
		return req
	}

	var all []VettedPackages
	pageToken := ""
	for {
		var resp struct {
			VettedPackages []VettedPackages `json:"vettedPackages"`
			NextPageToken  string           `json:"nextPageToken"`
		}
		if err := c.post(ctx, "/v2/package-vetting/list", build(pageToken), &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.VettedPackages...)
		if resp.NextPageToken == "" {
			return all, nil
		}
		pageToken = resp.NextPageToken
	}
}

// GetParticipantID returns the ID of the participant serving this Ledger API,
// in Canton's "PAR::name::1220…" form.
func (c *Client) GetParticipantID(ctx context.Context) (string, error) {
	var resp struct {
		ParticipantID string `json:"participantId"`
	}
	if err := c.do(ctx, http.MethodGet, "/v2/parties/participant-id", nil, &resp); err != nil {
		return "", err
	}
	if resp.ParticipantID == "" {
		return "", fmt.Errorf("the Ledger API returned no participant ID")
	}
	return resp.ParticipantID, nil
}
