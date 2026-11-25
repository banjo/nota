# Cutting a release

The short version is in [RELEASE.md](../RELEASE.md); this is the detail.

A release is a `v*` tag. [Changesets](https://changesets.dev) decides the version and writes
the changelog, `.github/workflows/changesets.yml` creates the tag, and
`.github/workflows/release.yml` builds everything from it, publishes the GitHub release and
has Cloudflare rebuild the landing page; the Homebrew cask is updated by hand afterwards.

## Changesets

A pull request that changes what users get adds a changeset:

```sh
bunx changeset    # pick @nota/desktop, choose patch, write one line for the release notes
```

This writes a Markdown file to `.changeset/`; commit it with the change. CI, docs and test
changes need none. `@nota/desktop` carries the version of the whole product (app, CLI and web);
the other workspace packages are ignored by Changesets (`.changeset/config.json`). Nothing
checks that a pull request has one; `bunx changeset status` lists what is pending.

On every push to `main`, `changesets.yml` does one of two things:

- **There are changesets:** it opens or updates the **Release Nota** pull request. That runs
  `bun run version-packages`, which bumps `apps/desktop/package.json`, adds the entries to
  `apps/desktop/CHANGELOG.md` (with links to the PRs), deletes the changesets and updates
  `bun.lock`.
- **There are none** (for example, right after that pull request is merged): if
  `v<version from apps/desktop/package.json>` isn't a tag yet, it creates it and starts
  `release.yml` on it. Otherwise it does nothing.

The release notes are the version's section of `apps/desktop/CHANGELOG.md`, followed by
GitHub's list of merged pull requests.

Setup, once: in the repository's Settings → Actions → General, allow GitHub Actions to create
and approve pull requests. Pull requests opened by the workflow token don't start CI, so the
Release Nota pull request shows no checks; `main` is checked again after the merge.

## What the workflow does

| Job | Runner | Output |
| --- | --- | --- |
| `version` | ubuntu | the version: the tag without `v`; a prerelease when it has a `-` suffix (`v0.1.1-rc.1`) |
| `cli` (×4) | ubuntu | `nota_<v>_{darwin,linux}_{amd64,arm64}.tar.gz` (pure Go, `CGO_ENABLED=0`, cross-compiled), each with `nota`, `LICENSE`, `README.md` |
| `macos` | macos-latest (arm64) | `Nota-<v>-arm64.dmg` and `Nota-<v>-arm64.zip` from `mygo build -platform darwin/arm64`, signed ad hoc, with the CLI inside at `Contents/Resources/bin/nota` |
| `linux` | ubuntu-24.04 | `nota_<v>_amd64.deb`, `nota-<v>-1.x86_64.rpm`, `Nota-<v>-linux-amd64.tar.gz` (MyGo) and `Nota-<v>-x86_64.AppImage` (`scripts/appimage.sh`) |
| `publish` | ubuntu | the GitHub release (not a draft; `--prerelease` for `-` versions, else `--latest`) with every file and `checksums.txt`, then a rebuild of the landing page (not for prereleases) |

The version is linked into every binary: `-X github.com/banjo/nota/internal/cli.Version` for
the CLI (`nota --version`) and `NOTA_VERSION` for the app (`apps/desktop/mygo.config.ts`:
Info.plist, the packages). It comes from the tag; `apps/desktop/package.json` only decides
which tag `changesets.yml` creates.
MyGo writes a prerelease such as `0.1.1-rc.1` as `0.1.1~rc.1` in the .deb and .rpm, so it
sorts before `0.1.1`.

The macOS job checks the app before uploading it: Info.plist version, `codesign -dv` (ad hoc),
`codesign --verify --deep --strict`, `hdiutil verify` and a mount of the DMG, the bundled
CLI's `--version`, and that the app is still running 10 s after start. It also checks the
Markdown document type (below), and starts the app with a `.md` file outside the vault on the
command line: the log must say `opened file kind=external`, the file must be unchanged and
nothing may be written into the vault. The Linux job checks that the .deb's desktop entry runs
`/opt/nota/nota %U` and lists `text/markdown`.

### Markdown files (Open With)

`apps/desktop/mygo.config.ts` declares the Markdown association twice, on purpose:
`fileAssociations` (which command-line arguments reach `App.OnOpenFile`, and the Linux
`MimeType`) and `macos.infoPlist.CFBundleDocumentTypes`, which replaces the entry MyGo makes
because MyGo v0.10.0 hard-codes `LSHandlerRank: Default`. Ours is `Alternate`, role `Viewer`,
with `LSItemContentTypes: net.daringfireball.markdown` and a `UTImportedTypeDeclarations`
entry, so Nota is offered in Finder's Open With but never takes over double-click. Keep the
extensions in both places in step; the workflow fails if the built Info.plist has another rank
or role, misses `.markdown`, or still carries a second (MyGo's) document type. Nota never sets
itself as the default handler; users choose that in Finder ▸ Get Info ▸ Open with ▸ Change All.

Launch Services registers the document type when Finder first sees the app in `/Applications`
or the app first starts; the cask does not run `lsregister`. If Open With doesn't list Nota
right after `brew install`, run
`/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f /Applications/Nota.app`.
Stale entries from development builds (an extra "Nota" in Open With) go with
`lsregister -u <path to the old Nota.app>`.

A manual run (Actions → Release → Run workflow) builds everything as run artifacts and
publishes nothing unless it runs on a tag.

## Versions

The first release was **v0.1.0**. Every release after it is a **patch bump** (v0.1.1, v0.1.2,
…); minor and major bumps wait until they are decided, so changesets are `patch` for now.
`scripts/next-version.sh --rc` prints the next prerelease tag (`v0.1.2-rc.1`, `-rc.2`, …) from
the latest `vX.Y.Z` tag on origin.

## Steps

1. Make sure `main` is green (CI) and the **Release Nota** pull request lists what should ship.
2. Optionally try the pipeline with a prerelease first. The landing page never shows it, but
   delete it afterwards anyway:

   ```sh
   git switch main && git pull
   rc=$(scripts/next-version.sh --rc) # e.g. v0.1.2-rc.1
   git tag "$rc" && git push origin "$rc"
   gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
   gh release delete "$rc" --cleanup-tag --yes
   ```

3. Merge the **Release Nota** pull request. `changesets.yml` tags `v<version>` and starts
   `release.yml`.
4. Wait for the workflow (about 10 minutes) and check the release page: 10 files plus
   `checksums.txt`. Edit the notes if needed.
5. Bump the Homebrew cask (needs your own push access to `banjo/homebrew-tap`):

   ```sh
   scripts/bump-cask.sh 0.1.0 --dry-run      # print the cask, check it with ruby -c / brew style
   scripts/bump-cask.sh 0.1.0                # clone the tap, write Casks/nota.rb, commit, push
   ```

   The script downloads `Nota-0.1.0-arm64.zip`, checks it against `checksums.txt` and fills in
   `packaging/homebrew/nota.rb`. It refuses prereleases unless `--dry-run` or
   `--allow-prerelease`. Then `brew update && brew install --cask banjo/tap/nota` to try it.


## The landing page

Cloudflare Pages (project `nota-web`) builds and deploys `main` on every push. The landing page
reads the latest release at build time (`apps/landing/src/data/release.ts`, GitHub's
`releases/latest`, which never returns a prerelease or a draft), and the push that tags a
release builds before the release exists. So the publish job ends by calling a Pages deploy
hook, and the new version's download buttons appear a minute or two after the release.

The hook is `github-release` (branch `main`) under Workers & Pages → `nota-web` → Settings →
Builds → Deploy hooks, and its URL is the repository secret `CLOUDFLARE_DEPLOY_HOOK`. To
replace it, add a new hook there, set the secret to its URL
(`gh secret set CLOUDFLARE_DEPLOY_HOOK --repo banjo/nota`) and delete the old one. Without the
secret the publish job warns, and the next push to `main` updates the page.

## When something fails

- Re-running failed jobs is safe: the publish job uploads with `--clobber` into a release
  that already exists.
- A broken tag: `gh release delete vX --cleanup-tag --yes`, fix on `main` with a new
  changeset, and merge the next Release Nota pull request.
- The tag exists but no release started (the dispatch failed):
  `gh workflow run release.yml --ref vX`.
- No Developer ID yet: when there is one, set `macos.signingIdentity` and `macos.notarize` in
  `apps/desktop/mygo.config.ts` and import the certificate in the macOS job
  ([MyGo's GitHub Actions guide](https://mygo.run/docs/github-actions)); then drop the
  quarantine `postflight` from the cask and the Gatekeeper note from the README.
