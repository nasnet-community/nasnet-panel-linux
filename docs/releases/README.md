# Releasing

1. Choose the next version. Use a major version for incompatible API or database changes.
2. Update `web-panel/package.json` and write concise notes in `docs/releases/vX.Y.Z.md`, including upgrade requirements. Preview the notes before publishing.
3. Run the checks in `.github/workflows/ci.yml`, commit the release changes, push `main`, and wait for CI to pass on that commit.
4. Tag that commit with `git tag -a vX.Y.Z -m 'Release vX.Y.Z'`, then push the tag with `git push origin vX.Y.Z`.
5. Wait for both jobs in `.github/workflows/release.yml`. They build Linux amd64/arm64 binaries and offline bundles, create a draft GitHub release using the saved notes, and publish Docker images to GHCR.
6. Verify the assets, checksums, image architectures, and embedded version. Publish the draft with `gh release edit vX.Y.Z --draft=false --latest`.

The Git tag supplies the panel binary and Docker version through build flags. Keep `cmd.Version` as `dev` for unversioned development builds. Do not move an already published tag; use a new version for a correction.
