# Releasing

A release of shed is a set of prebuilt `shed` binaries attached to a GitHub
release, built by [.github/workflows/release.yml](../.github/workflows/release.yml)
when a person pushes a `v*` tag. The tag is the only thing that runs a
workflow; the project's gate is `dagger check`, run by hand.

## Cutting a release

1. Make sure `main` is the commit you want to ship and that it is green:

   ```sh
   DAGGER_X_RELEASE=v1.0.0-beta.14 dagger check
   ```

2. Tag it and push the tag. The tag name is the version: it is what the
   binaries report with `shed version` and what the asset names carry.

   ```sh
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

3. Watch the run with `gh run watch` or on the Actions tab. It builds the four
   platforms in parallel, then writes `checksums.txt`, creates the release
   with notes generated from the commits since the previous tag, and uploads
   everything to it.

To ship a fix, tag again. The workflow creates one release per tag and never
changes an existing one; re-running it for a tag whose release exists fails.

## What a release contains

One gzipped tarball per platform, each holding a single executable named
`shed`, and a `checksums.txt`:

| Asset | Platform |
| --- | --- |
| `shed_<version>_darwin_amd64.tar.gz` | macOS, Intel |
| `shed_<version>_darwin_arm64.tar.gz` | macOS, Apple silicon |
| `shed_<version>_linux_amd64.tar.gz` | Linux, x86-64 |
| `shed_<version>_linux_arm64.tar.gz` | Linux, arm64 |
| `checksums.txt` | SHA-256 of every tarball, in `sha256sum` format |

`<version>` is the tag exactly as pushed, including the leading `v`. The
binaries are built with `CGO_ENABLED=0`; shed's SQLite driver is pure Go.
A binary built any other way reports `dev` from `shed version`, with the
commit it was built from.
