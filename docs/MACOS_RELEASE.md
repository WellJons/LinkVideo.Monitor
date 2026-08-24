# LinkVideo Monitor macOS production release

This document describes the production signing and notarization path for the macOS build. Development CI remains ad-hoc signed; a public release must use Apple Developer ID identities and the notarization service.

## Required Apple credentials

Install these identities in the signing keychain on the release Mac:

- `Developer ID Application` for the app and every nested executable/helper;
- `Developer ID Installer` for the `.pkg` installer.

Export their exact identity names:

```bash
export MACOS_APP_IDENTITY='Developer ID Application: Example Company (TEAMID)'
export MACOS_INSTALLER_IDENTITY='Developer ID Installer: Example Company (TEAMID)'
```

For notarization, the preferred method is a Keychain profile created once on the release Mac:

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

## Preflight without credentials

Any macOS build machine can verify that the release toolchain and scripts are available without accessing certificates or Apple credentials:

```bash
bash scripts/macos/release-sign-notarize.sh --check
```

This is the mode used by normal macOS CI.

## Production release

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
