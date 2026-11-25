package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/metatarzxyz/canton-decentralized-parties/internal/admin"
	"github.com/metatarzxyz/canton-decentralized-parties/internal/config"
	"github.com/metatarzxyz/canton-decentralized-parties/internal/metrics"
	"github.com/metatarzxyz/canton-decentralized-parties/internal/policy"
)

// runApprove authorizes this participant's hosting of new multi-hosted
// external parties (see the package documentation).
func runApprove(args []string) int {
	fs := flag.NewFlagSet("approve", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to the configuration file")
	_ = fs.Parse(args)
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("load config", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := admin.Dial(cfg.AdminAPI, cfg.SynchronizerID)
	if err != nil {
		log.Error("connect admin api", "error", err)
		return 1
	}
	defer func() { _ = client.Close() }()

	idCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	self, err := client.ParticipantUID(idCtx)
	cancel()
	if err != nil {
		log.Error("get participant id", "error", err)
		return 1
	}

	pol := policy.New(self, cfg.Policy)
	allowed := "all co-hosts"
	if n := len(cfg.Policy.AllowedParticipantsUID); n > 0 {
		allowed = fmt.Sprintf("%d participants", n)
	}
	log.Info("approver started",
		"participant_uid", self, "synchronizer", cfg.SynchronizerID,
		"allowed_initiators", allowed, "poll_interval", cfg.PollInterval.String())

	a := &approver{log: log, client: client, policy: pol, cfg: cfg, self: self,
		seen: map[string]time.Time{}, metrics: &metrics.Metrics{}}
	if cfg.MetricsAddress != "" {
		if err := a.metrics.Serve(ctx, cfg.MetricsAddress); err != nil {
			log.Error("start metrics server", "error", err)
			return 1
		}
		log.Info("metrics enabled", "address", cfg.MetricsAddress, "path", "/metrics")
	}
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		a.scan(ctx)
		select {
		case <-ctx.Done():
			log.Info("approver stopped")
			return 0
		case <-ticker.C:
		}
	}
}

type approver struct {
	log     *slog.Logger
	client  *admin.Client
	policy  *policy.Policy
	cfg     *config.Config
	self    string
	metrics *metrics.Metrics

	// seen holds proposals already decided (hash -> time), so rejections are
	// logged once and approvals are not repeated while they propagate.
	seen     map[string]time.Time
	approved []time.Time // approval times, for the per-minute cap
}

func (a *approver) scan(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	proposals, err := a.client.PendingProposals(listCtx, a.self)
	cancel()
	if err != nil {
		a.metrics.SetUp(false)
		if ctx.Err() == nil {
			a.log.Warn("list pending proposals failed", "error", err)
		}
		return
	}
	a.metrics.SetUp(true)

	now := time.Now()
	for h, t := range a.seen {
		if now.Sub(t) > time.Hour {
			delete(a.seen, h)
		}
	}
	for _, p := range proposals {
		hash := p.HashHex()
		if _, done := a.seen[hash]; done {
			continue
		}
		decision, info := a.policy.Evaluate(p)
		switch decision {
		case policy.AlreadySigned:
			a.seen[hash] = now
		case policy.Reject:
			a.seen[hash] = now
			a.metrics.IncRejections()
			a.log.Info("proposal rejected", "party", p.Party, "hash", hash, "reason", info)
		case policy.Approve:
			if !a.allowApproval(now) {
				a.log.Warn("approval rate limit reached, retrying next scan", "party", p.Party)
				continue
			}
			authCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := a.client.Authorize(authCtx, p)
			cancel()
			if err != nil {
				a.log.Warn("authorize failed, retrying next scan", "party", p.Party, "hash", hash, "error", err)
				continue
			}
			a.seen[hash] = now
			a.approved = append(a.approved, now)
			a.metrics.IncApprovals()
			a.log.Info("authorized hosting", "party", p.Party, "hash", hash, "initiator", info,
				"hosts", len(p.Hosts), "threshold", p.Threshold)
		}
	}
}

func (a *approver) allowApproval(now time.Time) bool {
	kept := a.approved[:0]
	for _, t := range a.approved {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	a.approved = kept
	return len(a.approved) < a.cfg.Policy.MaxApprovalsPerMinute
}
