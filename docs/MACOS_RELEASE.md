# LinkVideo Monitor macOS production release

This document describes the production signing, notarization and publishing path for the macOS build. Development CI remains ad-hoc signed; a public release must use Apple Developer ID identities and the notarization service.

## Required Apple credentials

Install these identities in the signing keychain on a manual release Mac:

- `Developer ID Application` for the app and every nested executable/helper;
- `Developer ID Installer` for the `.pkg` installer.

Export their exact identity names:

```bash
export MACOS_APP_IDENTITY='Developer ID Application: Example Company (TEAMID)'
export MACOS_INSTALLER_IDENTITY='Developer ID Installer: Example Company (TEAMID)'
```

For notarization, the preferred manual method is a Keychain profile created once on the release Mac:

```bash
xcrun notarytool store-credentials 'linkvideo-notary' \
  --apple-id 'release@example.com' \
  --team-id 'TEAMID' \
  --password 'APP_SPECIFIC_PASSWORD'

export MACOS_NOTARY_PROFILE='linkvideo-notary'
```

CI/release infrastructure may instead provide all three environment variables:

- `MACOS_NOTARY_APPLE_ID`
- `MACOS_NOTARY_TEAM_ID`
- `MACOS_NOTARY_PASSWORD`

The release script never prints the password.

## GitHub Actions production release

`.github/workflows/macos-production-release.yml` is the production publishing entrypoint. It is manual (`workflow_dispatch`) and intentionally runs only when dispatched from `main`.

Configure these repository variables:

- `MACOS_APP_IDENTITY` — exact `Developer ID Application: ... (TEAMID)` identity;
- `MACOS_INSTALLER_IDENTITY` — exact `Developer ID Installer: ... (TEAMID)` identity.

Configure these repository secrets:

- `MACOS_APP_CERT_P12_BASE64` — base64-encoded `.p12` containing the Developer ID Application private key and certificate;
- `MACOS_APP_CERT_PASSWORD` — password of that `.p12`;
- `MACOS_INSTALLER_CERT_P12_BASE64` — base64-encoded `.p12` containing the Developer ID Installer private key and certificate;
- `MACOS_INSTALLER_CERT_PASSWORD` — password of that `.p12`;
- `MACOS_NOTARY_APPLE_ID`;
- `MACOS_NOTARY_TEAM_ID`;
- `MACOS_NOTARY_PASSWORD` — Apple app-specific password;
- `UPDATES_REPO_TOKEN` — token allowed to create releases and push `main` in `WellJons/LinkVideo.Monitor.Updates`.

The workflow creates an ephemeral keychain on the GitHub-hosted macOS runner, imports the two `.p12` files, runs the normal production release script, and deletes the keychain at the end. Certificate files are also removed during cleanup.

Run the workflow from `main` with a version such as `0.1.0` or `0.1.0-beta.1`. The optional `mandatory` input controls only the macOS update-manifest flag; it does not weaken any signature or notarization check.

After all local production checks pass, the workflow:

1. uploads the signed/notarized ZIP, PKG, DMG and checksum list as a workflow artifact;
2. creates immutable source tag `macos-v<VERSION>` pointing at the exact `main` commit;
3. publishes the macOS assets into public `WellJons/LinkVideo.Monitor.Updates` release `v<VERSION>`;
4. refuses to replace an existing public asset with different bytes;
5. downloads the public PKG again and verifies its SHA-256;
6. updates `update-manifest-macos.json` only after the public PKG is proven identical;
7. verifies the public manifest after the push.

The public release tag may also contain Windows assets when platform versions happen to match. Platform-specific filenames and manifests keep the update channels independent.

## Preflight without credentials

Any macOS build machine can verify that the release toolchain and scripts are available without accessing certificates or Apple credentials:

```bash
bash scripts/macos/release-sign-notarize.sh --check
```

`macOS Release Preflight` also validates the production workflow structure on pull requests, so release automation changes are reviewed by CI before they reach `main`.

## Manual production release

Set a non-development version and run:

```bash
export MACOS_VERSION='0.1.0'
bash scripts/macos/release-sign-notarize.sh
```

The release pipeline is fail-closed and performs the following sequence:

1. builds the self-contained Universal app with bundled Universal FFmpeg and MediaMTX;
2. re-signs every nested executable and helper with `Developer ID Application`, Hardened Runtime and secure timestamp;
3. signs nested app bundles and finally the parent `LinkVideo.Monitor.app`;
4. verifies the complete code-signing graph;
5. submits a ZIP transport containing the app to `notarytool`, waits for acceptance and staples the ticket to the app;
6. creates the final ZIP after stapling;
7. builds the `.pkg` with `Developer ID Installer`, verifies its signature, notarizes and staples it;
8. creates the DMG from the already-notarized app and package, notarizes and staples the disk image;
9. runs `stapler`, `codesign`, `pkgutil` and Gatekeeper `spctl` assessments on the final artifacts.

The script refuses versions containing `dev` by default. `MACOS_ALLOW_DEV_RELEASE=1` exists only for controlled release-engineering tests.

## Output

For `MACOS_VERSION=0.1.0` the final files are:

- `build/macos/LinkVideo.Monitor.app`
- `build/macos/LinkVideo.Monitor_macOS_0.1.0.zip`
- `build/macos/LinkVideo.Monitor_macOS_0.1.0.pkg`
- `build/macos/LinkVideo.Monitor_macOS_0.1.0.dmg`

A release must not be published unless the complete production script succeeds. Manually skipping notarization, hardened runtime, secure timestamps or Gatekeeper validation is not an accepted release path.
