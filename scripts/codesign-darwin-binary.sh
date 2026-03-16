#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <binary-path> <target>" >&2
  exit 1
fi

binary_path="$1"
target="$2"

case "${target}" in
  darwin_*) ;;
  *)
    exit 0
    ;;
esac

if [[ -z "${APPLE_DEV_ID_APP:-}" ]]; then
  echo "[ERROR] APPLE_DEV_ID_APP is required for darwin signing." >&2
  exit 1
fi

timestamp_url="${APPLE_TIMESTAMP_URL:-http://timestamp.apple.com/ts01}"

codesign_args=(
  --force
  --options runtime
  --timestamp="${timestamp_url}"
  --sign "${APPLE_DEV_ID_APP}"
)

if [[ -n "${APPLE_KEYCHAIN_PATH:-}" ]]; then
  codesign_args+=(--keychain "${APPLE_KEYCHAIN_PATH}")
fi

codesign "${codesign_args[@]}" "${binary_path}"
codesign --verify --verbose=2 "${binary_path}"

echo "[info] signed ${binary_path}"
