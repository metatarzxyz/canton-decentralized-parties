#!/usr/bin/env bash
# Regenerates gen/ from the vendored Canton protos (pinned in CANTON_COMMIT).
# Needs protoc, protoc-gen-go and protoc-gen-go-grpc.
set -euo pipefail
cd "$(dirname "$0")"
MOD=github.com/metatarzxyz/canton-decentralized-parties
MAPS=""
FILES=()
for f in $(find com scalapb -name "*.proto" | sort); do
  MAPS="$MAPS,M$f=$MOD/gen/$(dirname "$f")"
  FILES+=("$f")
done
MAPS="${MAPS#,},Mgoogle/rpc/status.proto=google.golang.org/genproto/googleapis/rpc/status"
rm -rf ../gen && mkdir -p ../gen
protoc -I . \
  --go_out=../gen --go_opt=paths=source_relative --go_opt="$MAPS" \
  --go-grpc_out=../gen --go-grpc_opt=paths=source_relative --go-grpc_opt="$MAPS" \
  "${FILES[@]}"
