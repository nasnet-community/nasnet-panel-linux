# Releasing

1. Choose the next version. Use a major version for incompatible API or database changes. A tag with a hyphen, such as `v1.2.0-rc.1`, is a prerelease: only panels with the beta channel on offer it.
2. Update `web-panel/package.json` and write concise notes in `docs/releases/vX.Y.Z.md`, including upgrade requirements. Preview the notes before publishing. Panels show these notes before they update.
3. Run the checks in `.github/workflows/ci.yml`, commit the release changes, push `main`, and wait for CI to pass on that commit.
4. Tag that commit with `git tag -a vX.Y.Z -m 'Release vX.Y.Z'`, then push the tag with `git push origin vX.Y.Z`.
5. Wait for both jobs in `.github/workflows/release.yml`. They build Linux amd64/arm64 binaries and offline bundles, sign them, create a draft GitHub release using the saved notes, and publish Docker images to GHCR.
6. Verify the assets, checksums, image architectures, and embedded version, and check the signature with `go run ./cmd/release-sign verify release.json` (with `release.json.sig` next to it). Publish the draft with `gh release edit vX.Y.Z --draft=false --latest`, or without `--latest` for a prerelease. Panels see a release only once it is published.

The Git tag supplies the panel binary and Docker version through build flags. Keep `cmd.Version` as `dev` for unversioned development builds. Do not move an already published tag; use a new version for a correction. Panels compare tags, so a rebuilt tag is never offered.

## Signing

Every release is signed with an Ed25519 key, and panels refuse files no trusted key signed. The workflow stops before building if the `RELEASE_SIGNING_KEY` secret is missing or isn't trusted by the code being released.

- **Set up once:** run `go run ./cmd/release-sign keygen`. Put the private key in the repository secret `RELEASE_SIGNING_KEY` and keep an offline copy: losing it means every panel needs a manual `sudo nasnet` update to trust a new key. Add the printed `mustKey(…)` line to `PublicKeys` in `pkg/signing/keys.go`, and the quoted line under it to `RELEASE_KEYS` in `nasnet-tool.sh`, which checks releases with OpenSSL before the panel can. A test fails while the two lists differ.
- **Rotate:** ship a release, signed by the old key, that lists both keys; switch the secret in the next one; drop the old key in the one after.
- **What is signed:** `release.json` lists every asset with its SHA-256 and carries the notes; `bundle.json` inside each offline bundle lists every file in it, so an uploaded bundle checks itself; each agent binary has a `.sig` its nodes verify.

## Upgrade steps

A release that older panels can't jump to directly names the oldest version that may update straight to it, in a comment at the top of its notes:

```markdown
<!-- min_from: v1.2.0 -->
```

Panels older than that are offered the newest release they can take first.
