# macOS Release Signing and Notarization

`a2hmarket-cli` now signs and notarizes the macOS release assets inside GitHub Actions before they are uploaded to GitHub Releases.

## Release flow

1. Git tag `v*` triggers `.github/workflows/release.yml`.
2. The workflow runs on `macos-14`.
3. `scripts/import-apple-certificate.sh` creates a temporary keychain, imports the Developer ID `.p12`, and stores a `notarytool` profile.
4. GoReleaser builds all target binaries. The darwin binaries are signed by `scripts/codesign-darwin-binary.sh` through a build post-hook.
5. Darwin release archives are generated as `.zip` files so they can be submitted to Apple notarization.
6. `scripts/notarize-darwin-archives.sh` submits every darwin archive to Apple and waits for success.
7. Only after notarization succeeds are the archives and `checksums.txt` uploaded to GitHub Releases.

## Required GitHub Actions secrets

The workflow uses the same Apple release secret names as the reference setup in `Golden/clawhub123`:

- `APPLE_DEV_ID_APP`
- `APPLE_CERTIFICATE_P12_BASE64`
- `APPLE_CERTIFICATE_PASSWORD`
- `APPLE_KEYCHAIN_PASSWORD`
- `APPLE_NOTARY_APPLE_ID`
- `APPLE_NOTARY_TEAM_ID`
- `APPLE_NOTARY_APP_PASSWORD`

## One-time setup

1. Export your `Developer ID Application` certificate from Keychain Access as a `.p12`.
2. Copy `.env.release.example` to `.env.release.local` and fill in the Apple credentials.
3. Load the variables into your shell:

```bash
source ./.env.release.local
```

4. Push the secrets to GitHub:

```bash
./scripts/setup-github-release-environment.sh
```

By default the helper uploads secrets to `keman-ai/a2hmarket-cli`. Override `GH_REPO` if needed.

## Notes

- macOS release archives are intentionally published as `.zip`, while Linux remains `.tar.gz` and Windows remains `.zip`.
- The notarized asset is the release archive itself. That is enough for Gatekeeper online validation, even though there is no stapling step for the CLI archive format.
- If the Apple membership or Developer ID certificate expires, the macOS release job will fail during signing or notarization.
