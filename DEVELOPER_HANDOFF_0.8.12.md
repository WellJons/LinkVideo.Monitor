# LinkVideo Monitor 0.8.12 — developer handoff

This document accompanies the final 0.8.12 audit branch.

## Release target

- Product version: `0.8.12`
- Windows compatibility target: Windows 7 through current Windows releases
- Shipping Go compatibility toolchain: Go 1.20.x
- Local RTSP server: MediaMTX 1.0.3 on Windows 7; MediaMTX 1.19.3 on Windows 8+ with SHA-256 verification

## Final hardening included

- Transactional installer staging and rollback for normal/manual installation.
- ZIP payload path traversal/rooted-path rejection.
- Protected-desktop SYSTEM service no longer bypasses the user's `LaunchWithWindows` choice.
- Local HTTP control surface remains loopback-only and protected against cross-origin state-changing requests.
- Remote endpoint validation and secure capture request validation retained.
- Obsolete window-capture and superseded encoder-selection code removed.
- Application version marker finalized from `0.8.12-beta` to `0.8.12`.

## Validation gates

The `CI` and `Full Audit` GitHub Actions workflows are the release gates. Final results and artifact digests will be added here after the green run.
