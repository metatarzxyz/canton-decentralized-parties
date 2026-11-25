# cdp — canton-decentralized-parties

Tools for Canton participants that host **decentralized, multi-hosted external
parties**:

- `cdp approve`: automatically authorize your participant's hosting of new
  multi-hosted parties (below).
- `cdp dars list`: list the packages your participant vets
  ([DAR list](#dar-list)).

## cdp approve

Automatically authorize your Canton participant's hosting of new **multi-hosted
external parties**, using only your participant's own **Admin API**.

When an app onboards a user hosted on several participants (for example 3 hosts
with a 2-of-3 confirmation threshold), the initiating participant publishes a
`PartyToParticipant` proposal, signed by the party and by itself, to the
synchronizer. Every participant receives it in its topology store, but each host
must authorize its own hosting before the party becomes active. `cdp approve`
watches your participant's topology store and authorizes the proposals that pass
your policy. No connection to the app or its backend is needed.

## How it works

Every `poll_interval`, `cdp approve`:

1. lists pending `PartyToParticipant` proposals in the synchronizer store that
   name this participant (`TopologyManagerReadService.ListPartyToParticipant`);
2. evaluates each one against the policy;
3. authorizes approved proposals (`TopologyManagerWriteService.Authorize` by
   transaction hash). The party becomes active once every host has authorized.

Its own participant UID is read at startup (`IdentityInitializationService.GetId`)
and logged.

## Policy (defaults)

A proposal is authorized only if all of the following hold:

| Check | Default |
|---|---|
| This participant is a host, and the proposal adds hosting (not REMOVE) | always |
| The party is multi-hosted, with at most `max_hosts` hosts | 10 |
| Every host has an allowed permission (`submission` is never allowed) | confirmation, observation |
| `1 ≤ threshold ≤` number of confirming hosts | always |
| It is the party's first hosting mapping (serial 1). Adding a host to an existing party needs its ACS to be copied first, so it is not automated | always |
| The party has its own signing keys (external party) | always |
| The party itself signed the proposal (consent) | always |
| A co-host already signed it (the initiator) | any co-host; with `allowed_participants_uid` set, only the listed participants |
| At most `max_approvals_per_minute` authorizations | 60 |

Rejections are logged once per proposal with the reason.

## Operators registry

`registry/{network}/operators.json` (e.g.
[`registry/mainnet/operators.json`](registry/mainnet/operators.json)) lists the
operators that run `cdp approve` on each network, their participants and the
packages they vet ([schema](registry/schema.json)). It is reference data for apps
choosing hosts for their users.

```json
[
  {
    "name": "My Operator",
    "logo": "https://example.com/logo.png",
    "website": "https://example.com",
    "participant_uids": ["my-validator-1::1220…", "my-validator-2::1220…"],
    "dars": [
      {
        "name": "metatarz-wallet",
        "version": "0.3.4",
        "package_id": "f6e40e15…",
        "download_link": "https://example.com/metatarz-wallet-0.3.4.dar"
      }
    ]
  }
]
```

- `participant_uids` are the UIDs `cdp approve` logs at startup (not party IDs).
- `dars` lists the packages your participants vet. Every host of a party must vet
  the packages its transactions use, otherwise Canton fails with "No package … is
  consistently vetted by all hosting participants" or "failed package name
  resolution". Apps use this list to pick compatible hosts, and `download_link`
  to get the DARs to vet.

To join, open a pull request adding your operator to `registry/{network}/operators.json`.

## Configuration

See [`config.example.yaml`](config.example.yaml). `${VAR}` references are expanded
from the environment. The Admin API is usually internal to the participant's
deployment (no auth); TLS, mTLS and a static bearer token are supported.

## Metrics

Set `metrics_address` (e.g. `":9090"`) to serve Prometheus metrics on
`GET /metrics`. It is off by default.

| Metric | Type | |
|---|---|---|
| `cdp_up` | gauge | `1` if the last scan of the participant's Admin API succeeded, `0` if it failed (or before the first scan) |
| `cdp_approvals_total` | counter | hosting proposals authorized since the process started |
| `cdp_rejections_total` | counter | hosting proposals rejected by the policy since the process started (each proposal counted once) |
| `cdp_last_successful_scan_timestamp_seconds` | gauge | Unix time of the last successful scan; `0` before the first one. Alert on `time() - cdp_last_successful_scan_timestamp_seconds > 60` |

If `cdp` is down the scrape fails, so alert on
`up{job="cdp"} == 0 or cdp_up == 0`.

## Run

```bash
go build -o cdp .
./cdp approve -config config.yaml
```

or with Docker:

```bash
docker run -d --restart unless-stopped \
  -v $PWD/config.yaml:/etc/cdp/config.yaml:ro ghcr.io/metatarzxyz/canton-decentralized-parties:latest
```

## DAR list

Every host of a party must vet the packages its transactions use, otherwise
Canton fails with "No package … is consistently vetted by all hosting
participants" or "failed package name resolution". `cdp dars list` shows what
your participant vets: it reads the participant's ID from the JSON Ledger API
(`GET /v2/parties/participant-id`) and lists its vetted packages
(`POST /v2/package-vetting/list`).

```bash
cdp dars list -ledger-api-url http://localhost:7575 -token "$TOKEN"
```

| Flag | Default | |
|---|---|---|
| `-ledger-api-url` | (required) | JSON Ledger API of your participant |
| `-token` | none | Ledger API bearer token (`ledger:read`) |
| `-prefix` | all packages | comma-separated package-name prefixes, e.g. `utility-registry-,metatarz-` |
| `-synchronizer-id` | all | only list packages vetted on this synchronizer |

Output:

```
Participant: PAR::validator-1::1220…

Synchronizer: global-domain::1220… (2 packages)
PACKAGE                  VERSION  PACKAGE ID    VALID
metatarz-wallet          0.3.4    f6e40e15…     always
utility-registry-app-v0  0.9.2    1eddd268…     always
```

Exit code `0` = ok, `2` = error.

## Development

The Canton Admin API protos are vendored in `proto/`, pinned to the Canton commit
in `proto/CANTON_COMMIT`. Regenerate `gen/` with `proto/generate.sh` (needs
`protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`).
