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

decode_base64() {
  if base64 --help 2>&1 | grep -q -- "--decode"; then
    base64 --decode
    return
  fi
  if base64 -D </dev/null >/dev/null 2>&1; then
    base64 -D
    return
  fi
  base64 -d
}

require_cmd base64
require_cmd security
require_cmd xcrun

require_env APPLE_CERTIFICATE_P12_BASE64
require_env APPLE_CERTIFICATE_PASSWORD
require_env APPLE_DEV_ID_APP
require_env APPLE_KEYCHAIN_PASSWORD
require_env APPLE_NOTARY_APPLE_ID
require_env APPLE_NOTARY_APP_PASSWORD
require_env APPLE_NOTARY_TEAM_ID

run_dir="${RUNNER_TEMP:-$(mktemp -d)}"
cert_path="${run_dir}/apple-dev-id.p12"
keychain_path="${run_dir}/a2hmarket-release.keychain-db"
notary_profile="${APPLE_NOTARY_PROFILE:-a2hmarket-notary}"

printf '%s' "${APPLE_CERTIFICATE_P12_BASE64}" | decode_base64 > "${cert_path}"

security create-keychain -p "${APPLE_KEYCHAIN_PASSWORD}" "${keychain_path}"
security set-keychain-settings -lut 21600 "${keychain_path}"
security unlock-keychain -p "${APPLE_KEYCHAIN_PASSWORD}" "${keychain_path}"

existing_keychains=()
while IFS= read -r keychain; do
  keychain="${keychain//\"/}"
  [[ -n "${keychain}" ]] && existing_keychains+=("${keychain}")
done < <(security list-keychains -d user)

security list-keychains -d user -s "${keychain_path}" "${existing_keychains[@]}"
security default-keychain -d user -s "${keychain_path}"

security import "${cert_path}" \
  -k "${keychain_path}" \
  -P "${APPLE_CERTIFICATE_PASSWORD}" \
  -T /usr/bin/codesign \
  -T /usr/bin/security \
  -T /usr/bin/productsign

security set-key-partition-list \
  -S apple-tool:,apple: \
  -s \
  -k "${APPLE_KEYCHAIN_PASSWORD}" \
  "${keychain_path}"

if ! security find-identity -v -p codesigning "${keychain_path}" | grep -F "${APPLE_DEV_ID_APP}" >/dev/null 2>&1; then
  echo "[ERROR] Developer ID identity not found in imported keychain: ${APPLE_DEV_ID_APP}" >&2
  exit 1
fi

xcrun notarytool store-credentials "${notary_profile}" \
  --apple-id "${APPLE_NOTARY_APPLE_ID}" \
  --team-id "${APPLE_NOTARY_TEAM_ID}" \
  --password "${APPLE_NOTARY_APP_PASSWORD}" \
  --keychain "${keychain_path}"

if [[ -n "${GITHUB_ENV:-}" ]]; then
  {
    echo "APPLE_KEYCHAIN_PATH=${keychain_path}"
    echo "APPLE_NOTARY_PROFILE=${notary_profile}"
  } >> "${GITHUB_ENV}"
fi

echo "[info] imported signing identity into ${keychain_path}"
echo "[info] stored notary profile ${notary_profile}"
