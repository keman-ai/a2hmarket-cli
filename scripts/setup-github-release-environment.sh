#!/usr/bin/env bash

set -euo pipefail

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[ERROR] missing command: $1" >&2
    exit 1
  fi
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "[ERROR] missing required environment variable: ${name}" >&2
    exit 1
  fi
}

encode_base64_file() {
  local file_path="$1"
  if base64 --help 2>&1 | grep -q -- "--wrap"; then
    base64 --wrap=0 "${file_path}"
    return
  fi
  base64 < "${file_path}" | tr -d '\n'
}

derive_team_id() {
  if [[ -n "${APPLE_NOTARY_TEAM_ID:-}" ]]; then
    return
  fi
  if [[ "${APPLE_DEV_ID_APP:-}" =~ \(([A-Z0-9]{10})\)$ ]]; then
    APPLE_NOTARY_TEAM_ID="${BASH_REMATCH[1]}"
    export APPLE_NOTARY_TEAM_ID
  fi
}

repo="${GH_REPO:-keman-ai/a2hmarket-cli}"

require_cmd gh
require_cmd base64

require_env APPLE_CERT_P12_PATH
require_env APPLE_CERTIFICATE_PASSWORD
require_env APPLE_KEYCHAIN_PASSWORD
require_env APPLE_DEV_ID_APP
require_env APPLE_NOTARY_APPLE_ID
require_env APPLE_NOTARY_APP_PASSWORD

if [[ ! -f "${APPLE_CERT_P12_PATH}" ]]; then
  echo "[ERROR] certificate not found: ${APPLE_CERT_P12_PATH}" >&2
  exit 1
fi

derive_team_id
require_env APPLE_NOTARY_TEAM_ID

p12_base64="$(encode_base64_file "${APPLE_CERT_P12_PATH}")"

printf '%s' "${APPLE_DEV_ID_APP}" | gh secret set APPLE_DEV_ID_APP --repo "${repo}"
printf '%s' "${p12_base64}" | gh secret set APPLE_CERTIFICATE_P12_BASE64 --repo "${repo}"
printf '%s' "${APPLE_CERTIFICATE_PASSWORD}" | gh secret set APPLE_CERTIFICATE_PASSWORD --repo "${repo}"
printf '%s' "${APPLE_KEYCHAIN_PASSWORD}" | gh secret set APPLE_KEYCHAIN_PASSWORD --repo "${repo}"
printf '%s' "${APPLE_NOTARY_APPLE_ID}" | gh secret set APPLE_NOTARY_APPLE_ID --repo "${repo}"
printf '%s' "${APPLE_NOTARY_TEAM_ID}" | gh secret set APPLE_NOTARY_TEAM_ID --repo "${repo}"
printf '%s' "${APPLE_NOTARY_APP_PASSWORD}" | gh secret set APPLE_NOTARY_APP_PASSWORD --repo "${repo}"

cat <<EOF
[info] uploaded GitHub Actions secrets for ${repo}
[info] APPLE_DEV_ID_APP=${APPLE_DEV_ID_APP}
[info] APPLE_NOTARY_TEAM_ID=${APPLE_NOTARY_TEAM_ID}
EOF
