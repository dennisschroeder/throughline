# v0.1.0 release checklist

Manual checklist for the maintainer to run by hand before and during a Throughline release. Steps
are ordered; run them in sequence. Commands assume a POSIX shell in the repository root unless noted
otherwise.

## 1. Create the repository and push

This is the irreversible-visibility step: it makes the code world-readable for the first time.
Confirm you intend to publish before running it.

```bash
gh repo create dennisschroeder/throughline --public --source . --remote origin --push
```

If `gh` is unavailable, use the manual equivalent:

```bash
git remote add origin https://github.com/dennisschroeder/throughline.git
git push -u origin main
```

- [ ] Repository created and pushed under `github.com/dennisschroeder/throughline`.

## 2. Confirm public visibility

```bash
gh repo view dennisschroeder/throughline --json visibility
```

- [ ] Output shows `"visibility": "PUBLIC"`.

## 3. Confirm clean CI on `main`

Push any remaining changes, then confirm the CI workflow (`.github/workflows/ci.yml`) is green on
`main`:

```bash
gh run list --branch main --limit 1
gh run watch
```

- [ ] Latest run on `main` completed successfully (`gofmt -l`, `go vet`, `go test`, `go build`,
      `CGO_ENABLED=0 go build` all pass).

## 3a. Homebrew tap (one-time setup)

`.goreleaser.yaml`'s `brews:` block pushes a formula to `dennisschroeder/homebrew-throughline` on
every tagged release, authenticated with the `HOMEBREW_TAP_GITHUB_TOKEN` repository secret. Before
the first release that should auto-publish a formula:

```bash
gh repo create dennisschroeder/homebrew-throughline --public
gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo dennisschroeder/throughline
```

The secret value must be a token (fine-grained PAT, `contents: write` on the tap repo) — the
default `GITHUB_TOKEN` cannot push across repositories. Skipping this step does not fail the
release; GoReleaser's `brews` publish step fails independently and the archives/checksums still
publish.

- [ ] Tap repository exists.
- [ ] `HOMEBREW_TAP_GITHUB_TOKEN` secret set on the `throughline` repository.

## 4. Tag the release

This is the step that actually cuts the release: pushing the tag triggers
`.github/workflows/release.yml`, which runs GoReleaser.

```bash
git tag -a v0.1.0 -m "Throughline v0.1.0"
git push origin v0.1.0
```

- [ ] Tag `v0.1.0` pushed; release workflow triggered.

## 5. Artifact dry run

Run this before tagging for real, and consider running it earlier in development too, not only as a
final checkbox — it catches archive naming and content problems without touching any remote state.

```bash
goreleaser release --snapshot --clean
```

Inspect `dist/` for the four platform archives and the checksums file:

- `throughline_{{.Version}}_darwin_amd64.tar.gz`
- `throughline_{{.Version}}_darwin_arm64.tar.gz`
- `throughline_{{.Version}}_linux_amd64.tar.gz`
- `throughline_{{.Version}}_linux_arm64.tar.gz`
- `throughline_{{.Version}}_checksums.txt`

Confirm each archive contains the `throughline` binary, `README.md`, and `LICENSE`. Then clean up:

```bash
rm -rf dist/
```

- [ ] All four archives and the checksums file present with correct names and contents.
- [ ] `dist/` removed afterward.

## 6. Verify checksums

Against the snapshot output (step 5) or the real tagged artifacts:

```bash
shasum -a 256 -c throughline_*_checksums.txt
```

- [ ] Every archive reports `OK`.

## 7. Fresh-machine smoke test

Extract one archive on a machine (or directory) with no Go toolchain and no existing Throughline
checkout:

```bash
tar -xzf throughline_*_darwin_arm64.tar.gz -C /path/with/no/go/toolchain
cd /path/with/no/go/toolchain
./throughline version
mkdir empty-workspace && cd empty-workspace
./throughline init
ls .throughline/throughline.db
```

- [ ] `throughline version` reports the expected version/commit/date.
- [ ] `throughline init` succeeds in an empty directory.
- [ ] `.throughline/throughline.db` exists after `init`.

## 8. Release notes

GoReleaser generates the changelog automatically from conventional commit messages since the
previous tag; no manual changelog file is required. To add manual highlights on top of the
generated notes:

```bash
gh release edit v0.1.0 --notes-file <path-to-notes.md>
```

The GitHub UI's release editor works equally well for small edits.

- [ ] Generated changelog reviewed for accuracy.
- [ ] Manual highlights added, if desired.

## 9. Publish

GoReleaser publishes the release directly by default; it only stays a draft if `.goreleaser.yaml`
sets `draft: true`. Confirm either way:

```bash
gh release view v0.1.0
```

If the release is already published, confirm it is not marked as a draft. If it was created as a
draft, promote it explicitly (via `gh release edit v0.1.0 --draft=false` or the GitHub UI).

- [ ] `gh release view v0.1.0` shows a published (non-draft) release.

## 10. Post-publish install test

Repeat the download, checksum verification, and install steps from `docs/install.md` against the
real published `v0.1.0` release (not the snapshot build), then confirm the installed binary:

```bash
throughline version
```

- [ ] Fresh download/verify/install from the public release succeeds.
- [ ] `throughline version` reports `v0.1.0` from the installed binary.
