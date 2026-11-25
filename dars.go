package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/metatarzxyz/canton-decentralized-parties/internal/ledgerapi"
)

// runDarsList lists the packages vetted by the participant serving the given
// JSON Ledger API (ListVettedPackages, filtered to that participant).
// Exit code 0: ok, 2: error.
func runDarsList(args []string) int {
	fs := flag.NewFlagSet("dars list", flag.ExitOnError)
	ledgerURL := fs.String("ledger-api-url", "", "JSON Ledger API base URL, e.g. http://localhost:7575 or https://host/api/json-api (required)")
	token := fs.String("token", "", "Ledger API bearer token (any user with ledger:read)")
	prefix := fs.String("prefix", "", `optional comma-separated package-name prefixes, e.g. "utility-registry-,metatarz-" (default: all packages)`)
	synchronizerID := fs.String("synchronizer-id", "", "only list packages vetted on this synchronizer (default: all)")
	_ = fs.Parse(args)

	if *ledgerURL == "" {
		fmt.Fprintln(os.Stderr, "-ledger-api-url is required")
		return 2
	}
	var prefixes []string
	for _, p := range strings.Split(*prefix, ",") {
		if p = strings.TrimSpace(p); p != "" {
			prefixes = append(prefixes, p)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := ledgerapi.New(*ledgerURL, *token)

	participantID, err := client.GetParticipantID(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get participant ID: %v\n", err)
		return 2
	}
	results, err := client.ListVettedPackages(ctx, []string{participantID}, prefixes, *synchronizerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list vetted packages: %v\n", err)
		return 2
	}
	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "%s vets no matching packages\n", participantID)
		return 2
	}

	fmt.Printf("Participant: %s\n", participantID)
	sort.Slice(results, func(i, j int) bool { return results[i].SynchronizerID < results[j].SynchronizerID })
	for _, r := range results {
		pkgs := r.Packages
		sort.Slice(pkgs, func(i, j int) bool {
			if pkgs[i].PackageName != pkgs[j].PackageName {
				return pkgs[i].PackageName < pkgs[j].PackageName
			}
			if pkgs[i].PackageVersion != pkgs[j].PackageVersion {
				return pkgs[i].PackageVersion < pkgs[j].PackageVersion
			}
			return pkgs[i].PackageID < pkgs[j].PackageID
		})
		fmt.Printf("\nSynchronizer: %s (%d packages)\n", r.SynchronizerID, len(pkgs))
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PACKAGE\tVERSION\tPACKAGE ID\tVALID")
		for _, p := range pkgs {
			name, version := p.PackageName, p.PackageVersion
			if name == "" {
				name, version = "(unknown)", "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, version, p.PackageID, validity(p))
		}
		_ = tw.Flush()
	}
	return 0
}

// validity describes when a vetting is in effect.
func validity(p ledgerapi.VettedPackage) string {
	switch {
	case p.ValidFromInclusive == "" && p.ValidUntilExclusive == "":
		return "always"
	case p.ValidUntilExclusive == "":
		return "from " + p.ValidFromInclusive
	case p.ValidFromInclusive == "":
		return "until " + p.ValidUntilExclusive
	}
	return p.ValidFromInclusive + " .. " + p.ValidUntilExclusive
}
