// cdp (canton-decentralized-parties) helps participants host decentralized,
// multi-hosted external parties.
//
//	cdp approve -config config.yaml
//	    Authorizes this participant's hosting of new multi-hosted external
//	    parties, using only its own Canton Admin API. When a party is onboarded
//	    on several participants, the initiating participant publishes a
//	    PartyToParticipant proposal (signed by the party and by itself) to the
//	    synchronizer; every participant receives it in its topology store. The
//	    approver polls the local store for pending proposals that name this
//	    participant, checks them against its policy (allowed initiator,
//	    permissions, threshold, new external party, party consent) and
//	    authorizes them. The party becomes active once every host authorized.
//
//	cdp dars list -ledger-api-url URL [-token T] [-prefix …] [-synchronizer-id …]
//	    Lists the packages vetted by the participant serving the JSON Ledger
//	    API (ListVettedPackages for the current participant).
package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprint(os.Stderr, `Usage: cdp <command> [flags]

Commands:
  approve      authorize hosting of new multi-hosted external parties
  dars list    list the packages this participant vets

Run "cdp <command> -h" for the command's flags.
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "approve":
		os.Exit(runApprove(os.Args[2:]))
	case "dars":
		if len(os.Args) < 3 || os.Args[2] != "list" {
			fmt.Fprintln(os.Stderr, "Usage: cdp dars list -ledger-api-url URL [-token T] [-prefix …] [-synchronizer-id …]")
			os.Exit(2)
		}
		os.Exit(runDarsList(os.Args[3:]))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}
