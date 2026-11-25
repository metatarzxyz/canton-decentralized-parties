// Package config loads the host-approver configuration (YAML, ${VAR} expanded).
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config of the host-approver.
type Config struct {
	// AdminAPI is this participant's Canton Admin API (gRPC).
	AdminAPI AdminAPI `yaml:"admin_api"`
	// SynchronizerID is the synchronizer whose topology store is watched,
	// e.g. "global-domain::1220…".
	SynchronizerID string `yaml:"synchronizer_id"`
	// PollInterval between scans of pending proposals (default 2s).
	PollInterval time.Duration `yaml:"poll_interval"`
	// MetricsAddress, e.g. ":9090", serves Prometheus metrics on /metrics.
	// Empty (default): no metrics endpoint.
	MetricsAddress string `yaml:"metrics_address"`

	Policy Policy `yaml:"policy"`
}

// AdminAPI is a gRPC endpoint with optional TLS and bearer token.
type AdminAPI struct {
	Address string `yaml:"address"` // host:port
	TLS     TLS    `yaml:"tls"`
	// Token is an optional static bearer token sent with every call.
	Token string `yaml:"token"`
}

// TLS settings of the Admin API connection.
type TLS struct {
	Enabled            bool   `yaml:"enabled"`
	CAFile             string `yaml:"ca_file"`
	CertFile           string `yaml:"cert_file"` // client certificate (mTLS)
	KeyFile            string `yaml:"key_file"`
	ServerName         string `yaml:"server_name"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// Policy decides which hosting proposals this participant authorizes.
type Policy struct {
	// AllowedParticipantsUID lists the participant UIDs whose hosting proposals
	// are approved: the initiator, a co-host that already signed the proposal,
	// must be one of them. Empty: proposals initiated by any co-host are approved.
	AllowedParticipantsUID []string `yaml:"allowed_participants_uid"`
	// AllowedPermissions for every host: "confirmation", "observation"
	// (default both). "submission" is never allowed for multi-hosted parties.
	AllowedPermissions []string `yaml:"allowed_permissions"`
	// MaxHosts limits the number of hosting participants (default 10).
	MaxHosts int `yaml:"max_hosts"`
	// MaxApprovalsPerMinute caps authorizations (default 60).
	MaxApprovalsPerMinute int `yaml:"max_approvals_per_minute"`
}

// uidPattern matches a participant UID, optionally prefixed with "PAR::".
var uidPattern = regexp.MustCompile(`^(PAR::)?[^:\s]+::1220[0-9a-f]{64}$`)

// Load reads and validates the configuration file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(os.ExpandEnv(string(raw))))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.defaultsAndValidate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) defaultsAndValidate() error {
	if c.AdminAPI.Address == "" {
		return errors.New("admin_api.address is required")
	}
	if c.SynchronizerID == "" {
		return errors.New("synchronizer_id is required")
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	p := &c.Policy
	for _, u := range p.AllowedParticipantsUID {
		if !uidPattern.MatchString(strings.TrimSpace(u)) {
			return fmt.Errorf("policy.allowed_participants_uid: %q is not a participant UID (name::1220…)", u)
		}
	}
	if len(p.AllowedPermissions) == 0 {
		p.AllowedPermissions = []string{"confirmation", "observation"}
	}
	for _, perm := range p.AllowedPermissions {
		switch strings.ToLower(perm) {
		case "confirmation", "observation":
		default:
			return fmt.Errorf("policy.allowed_permissions: %q is not allowed (confirmation, observation)", perm)
		}
	}
	if p.MaxHosts <= 0 {
		p.MaxHosts = 10
	}
	if p.MaxApprovalsPerMinute <= 0 {
		p.MaxApprovalsPerMinute = 60
	}
	return nil
}
