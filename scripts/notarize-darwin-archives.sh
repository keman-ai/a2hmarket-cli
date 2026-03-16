#!/usr/bin/env bash

set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "usage: $0 [dist-dir]" >&2
  exit 1
fi

if [[ -z "${APPLE_NOTARY_PROFILE:-}" ]]; then
  echo "[ERROR] APPLE_NOTARY_PROFILE is required." >&2
  exit 1
fi

dist_dir="${1:-dist}"

if [[ ! -d "${dist_dir}" ]]; then
  echo "[ERROR] dist directory not found: ${dist_dir}" >&2
  exit 1
fi

shopt -s nullglob
archives=("${dist_dir}"/*_darwin_*.zip)
shopt -u nullglob

if [[ ${#archives[@]} -eq 0 ]]; then
  echo "[warn] no darwin zip archives found under ${dist_dir}, skipping notarization"
  exit 0
fi

for archive_path in "${archives[@]}"; do
  echo "[info] notarizing ${archive_path}"
  notary_args=(
    notarytool submit "${archive_path}"
    --keychain-profile "${APPLE_NOTARY_PROFILE}"
    --wait
    --output-format json
  )
  if [[ -n "${APPLE_KEYCHAIN_PATH:-}" ]]; then
    notary_args+=(--keychain "${APPLE_KEYCHAIN_PATH}")
  fi
  xcrun "${notary_args[@]}" >/tmp/"$(basename "${archive_path}")".notary.json
done

echo "[info] notarized ${#archives[@]} macOS archive(s)"
