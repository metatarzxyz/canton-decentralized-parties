// Package admin talks to a Canton participant's Admin API (topology services).
package admin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	protov30 "github.com/metatarzxyz/canton-decentralized-parties/gen/com/digitalasset/canton/protocol/v30"
	adminv30 "github.com/metatarzxyz/canton-decentralized-parties/gen/com/digitalasset/canton/topology/admin/v30"
	"github.com/metatarzxyz/canton-decentralized-parties/internal/config"
	"github.com/metatarzxyz/canton-decentralized-parties/internal/uid"
)

// Host is one hosting participant of a proposal.
type Host struct {
	UID        string // normalized "name::1220…"
	Permission string // "submission", "confirmation", "observation"
}

// Proposal is a pending PartyToParticipant proposal.
type Proposal struct {
	Party       string
	Threshold   uint32
	Hosts       []Host
	External    bool // the party has its own signing keys
	Serial      int32
	Replace     bool // ADD_REPLACE (not REMOVE)
	TxHash      []byte
	SignedBy    []string // key fingerprints that already signed
	ValidFrom   string
	SequencedAt string
}

// HashHex is the transaction hash as Authorize expects it.
func (p *Proposal) HashHex() string { return hex.EncodeToString(p.TxHash) }

// Client wraps the Admin API services.
type Client struct {
	conn  *grpc.ClientConn
	token string
	read  adminv30.TopologyManagerReadServiceClient
	write adminv30.TopologyManagerWriteServiceClient
	ident adminv30.IdentityInitializationServiceClient
	store *adminv30.StoreId
}

// checkAddress fails early with a clear message when the Admin API address
// is malformed or its host does not resolve (gRPC dials lazily and would only
// report "name resolver error: produced zero addresses" on the first call).
func checkAddress(address string) error {
	if strings.Contains(address, "://") {
		return nil // explicit gRPC target scheme, resolved by gRPC
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("admin api address %q must be host:port: %w", address, err)
	}
	if net.ParseIP(host) != nil || isLocalhost(host) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if addrs, err := net.DefaultResolver.LookupHost(ctx, host); err != nil || len(addrs) == 0 {
		return fmt.Errorf("cannot resolve admin api host %q (check the address, or use an IP such as 127.0.0.1:%s)", host, portOf(address))
	}
	return nil
}

// isLocalhost reports whether host is "localhost" or a "*.localhost" name,
// which always means the loopback interface (RFC 6761).
func isLocalhost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// dialTarget returns the gRPC target and options for address. "*.localhost"
// names are dialed on 127.0.0.1 (Go's own resolver does not map them to
// loopback) while keeping the name as the gRPC authority, so a reverse proxy
// routing by host name (e.g. nginx in the Splice validator deployment) still
// routes the request.
func dialTarget(address string) (string, []grpc.DialOption) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.Contains(address, "://") || !isLocalhost(host) || strings.EqualFold(host, "localhost") {
		return address, nil
	}
	return "passthrough:///" + net.JoinHostPort("127.0.0.1", port), []grpc.DialOption{grpc.WithAuthority(address)}
}

func portOf(address string) string {
	_, port, _ := net.SplitHostPort(address)
	return port
}

// Dial connects to the Admin API.
func Dial(cfg config.AdminAPI, synchronizerID string) (*Client, error) {
	if err := checkAddress(cfg.Address); err != nil {
		return nil, err
	}
	creds := insecure.NewCredentials()
	if cfg.TLS.Enabled {
		tc := &tls.Config{ServerName: cfg.TLS.ServerName, InsecureSkipVerify: cfg.TLS.InsecureSkipVerify, MinVersion: tls.VersionTLS12} //nolint:gosec // opt-in
		if cfg.TLS.CAFile != "" {
			pem, err := os.ReadFile(cfg.TLS.CAFile)
			if err != nil {
				return nil, fmt.Errorf("read ca_file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("ca_file contains no certificates")
			}
			tc.RootCAs = pool
		}
		if cfg.TLS.CertFile != "" {
			cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("load client certificate: %w", err)
			}
			tc.Certificates = []tls.Certificate{cert}
		}
		creds = credentials.NewTLS(tc)
	}
	target, extra := dialTarget(cfg.Address)
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64 << 20))}, extra...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:  conn,
		token: cfg.Token,
		read:  adminv30.NewTopologyManagerReadServiceClient(conn),
		write: adminv30.NewTopologyManagerWriteServiceClient(conn),
		ident: adminv30.NewIdentityInitializationServiceClient(conn),
		store: &adminv30.StoreId{Store: &adminv30.StoreId_Synchronizer{
			Synchronizer: &adminv30.Synchronizer{Kind: &adminv30.Synchronizer_Id{Id: synchronizerID}},
		}},
	}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) ctx(ctx context.Context) context.Context {
	if c.token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
}

// ParticipantUID returns this node's unique identifier ("name::1220…").
func (c *Client) ParticipantUID(ctx context.Context) (string, error) {
	resp, err := c.ident.GetId(c.ctx(ctx), &adminv30.GetIdRequest{})
	if err != nil {
		return "", err
	}
	if !resp.GetInitialized() || resp.GetUniqueIdentifier() == "" {
		return "", errors.New("participant is not initialized")
	}
	return uid.Normalize(resp.GetUniqueIdentifier()), nil
}

// PendingProposals lists pending PartyToParticipant proposals that include
// participantUID as a host, in the synchronizer store.
func (c *Client) PendingProposals(ctx context.Context, participantUID string) ([]*Proposal, error) {
	resp, err := c.read.ListPartyToParticipant(c.ctx(ctx), &adminv30.ListPartyToParticipantRequest{
		BaseQuery: &adminv30.BaseQuery{
			Store:     c.store,
			Proposals: true,
			TimeQuery: &adminv30.BaseQuery_HeadState{HeadState: &emptypb.Empty{}},
		},
		FilterParticipant: participantUID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*Proposal, 0, len(resp.GetResults()))
	for _, r := range resp.GetResults() {
		ctxR := r.GetContext()
		p := &Proposal{
			Serial:   ctxR.GetSerial(),
			Replace:  ctxR.GetOperation() == protov30.Enums_TOPOLOGY_CHANGE_OP_ADD_REPLACE,
			TxHash:   ctxR.GetTransactionHash(),
			SignedBy: ctxR.GetSignedByFingerprints(),
		}
		if ts := ctxR.GetValidFrom(); ts != nil {
			p.ValidFrom = ts.AsTime().UTC().String()
		}
		if ts := ctxR.GetSequenced(); ts != nil {
			p.SequencedAt = ts.AsTime().UTC().String()
		}
		switch {
		case r.GetV31() != nil:
			m := r.GetV31()
			p.Party, p.Threshold, p.External = m.GetParty(), m.GetThreshold(), m.PartySigningKeys != nil
			for _, h := range m.GetParticipants() {
				p.Hosts = append(p.Hosts, Host{UID: uid.Normalize(h.GetParticipantUid()), Permission: permission(h.GetPermission())})
			}
		case r.GetV30() != nil:
			m := r.GetV30()
			p.Party, p.Threshold, p.External = m.GetParty(), m.GetThreshold(), m.PartySigningKeys != nil
			for _, h := range m.GetParticipants() {
				p.Hosts = append(p.Hosts, Host{UID: uid.Normalize(h.GetParticipantUid()), Permission: permission(h.GetPermission())})
			}
		default:
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// Authorize signs the proposal with this participant's keys and distributes
// the signature. The mapping becomes effective once every host signed.
func (c *Client) Authorize(ctx context.Context, p *Proposal) error {
	_, err := c.write.Authorize(c.ctx(ctx), &adminv30.AuthorizeRequest{
		Type:               &adminv30.AuthorizeRequest_TransactionHash{TransactionHash: p.HashHex()},
		MustFullyAuthorize: false,
		Store:              c.store,
	})
	return err
}

func permission(p protov30.Enums_ParticipantPermission) string {
	switch p {
	case protov30.Enums_PARTICIPANT_PERMISSION_SUBMISSION:
		return "submission"
	case protov30.Enums_PARTICIPANT_PERMISSION_CONFIRMATION:
		return "confirmation"
	case protov30.Enums_PARTICIPANT_PERMISSION_OBSERVATION:
		return "observation"
	default:
		return "unspecified"
	}
}
