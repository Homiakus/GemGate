# Releasing GemGate

GemGate releases are created from version tags by `.github/workflows/release.yml`.

## Release contract

A release tag must start with `v` and contain a semantic version, for example:

```bash
git tag v0.5.0
git push origin v0.5.0
```

The release workflow checks the tagged source, builds all supported targets from that exact commit, generates an SPDX JSON SBOM, writes SHA-256 checksums, creates GitHub artifact attestations, and publishes the assets to the matching GitHub Release.

## Build targets

The shared `scripts/build-release.sh` currently produces:

- Linux amd64 / arm64;
- macOS amd64 / arm64;
- Windows amd64 / arm64.

Unix targets are packaged as `.tar.gz`; Windows targets use `.zip`.

The same script is executed on every normal CI run with `VERSION=ci`, including cross-compilation of all targets and an executable Linux version smoke test. This keeps the tag-release path from silently diverging from the build path exercised on `main`.

## Build metadata

Tagged builds inject the version into `main.version` using Go linker flags. The build script also uses:

- `CGO_ENABLED=0` for portable static Go builds where dependencies permit;
- `-trimpath` to remove local workspace paths;
- an empty Go build id;
- normalized archive timestamps from the tagged commit timestamp;
- deterministic tar ownership and gzip timestamp suppression;
- `zip -X` for Windows archives.

These controls reduce runner-specific archive differences. They do not replace source review or provenance verification.

## Supply-chain metadata

Each release contains:

- platform archives;
- `checksums.txt` with SHA-256 digests;
- an SPDX JSON SBOM generated from the tagged repository;
- GitHub artifact attestations for the subjects listed in `checksums.txt`.

The release workflow grants only the permissions needed to publish the release and generate GitHub attestations.

## Verifying a downloaded release

Verify the checksum first:

```bash
sha256sum -c checksums.txt
```

To verify GitHub build provenance for a downloaded archive:

```bash
gh attestation verify ./gemgate_0.5.0_linux_amd64.tar.gz -R Homiakus/GemGate
```

GitHub CLI resolves the attestation from the repository and validates the signed provenance for the local artifact.

## Version behavior

A source checkout reports a development version by default. Release builds override it from the tag:

```bash
gemgate version
```

The tag is the release-version source of truth. Do not edit the version string for every release; create the correct `vX.Y.Z` tag instead.

## Before tagging

Confirm that the latest `main` CI is green and review:

- `docs/AUDIT.md` for unresolved production risks;
- `SECURITY.md` for security/deployment boundaries;
- `docs/RATE_LIMITING.md` if distributed quota behavior changed;
- provider compatibility notes if provider adapters changed.

Do not tag from a commit with failing CI merely to test the release workflow. Normal CI already exercises the shared cross-platform packaging script.
