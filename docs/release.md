# Cutting a release

A release is a `v*` tag. `.github/workflows/release.yml` builds everything from it and
publishes the GitHub release; the Homebrew cask and the landing page are updated by hand
afterwards.

## What the workflow does

| Job | Runner | Output |
| --- | --- | --- |
| `version` | ubuntu | the version: the tag without `v`; a prerelease when it has a `-` suffix (`v0.1.1-rc.1`) |
| `cli` (×4) | ubuntu | `nota_<v>_{darwin,linux}_{amd64,arm64}.tar.gz` (pure Go, `CGO_ENABLED=0`, cross-compiled), each with `nota`, `LICENSE`, `README.md` |
| `macos` | macos-latest (arm64) | `Nota-<v>-arm64.dmg` and `Nota-<v>-arm64.zip` from `mygo build -platform darwin/arm64`, signed ad hoc, with the CLI inside at `Contents/Resources/bin/nota` |
| `linux` | ubuntu-24.04 | `nota_<v>_amd64.deb`, `nota-<v>-1.x86_64.rpm`, `Nota-<v>-linux-amd64.tar.gz` (MyGo) and `Nota-<v>-x86_64.AppImage` (`scripts/appimage.sh`) |
| `publish` | ubuntu | the GitHub release (not a draft; `--prerelease` for `-` versions, else `--latest`) with every file and `checksums.txt` |

The version is linked into every binary: `-X github.com/banjo/nota/internal/cli.Version` for
the CLI (`nota --version`) and `NOTA_VERSION` for the app (`apps/desktop/mygo.config.ts`:
Info.plist, the packages). Nothing in the repository needs a version bump before tagging.
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

The first release is **v0.1.0**. Every release after it is a **patch bump** (v0.1.1, v0.1.2,
…); minor and major bumps wait until they are decided. `scripts/next-version.sh` prints the
next tag from the latest `vX.Y.Z` tag on origin (v0.1.0 when there is none), and
`scripts/next-version.sh --rc` the next prerelease of it (`v0.1.1-rc.1`, `-rc.2`, …).

## Steps

1. Make sure `main` is green (CI) and contains what should ship, then:

   ```sh
   git switch main && git pull
   tag=$(scripts/next-version.sh)     # v0.1.0 for the first release, then v0.1.1, …
   ```

2. Optionally try the pipeline with a prerelease first. The landing page never shows it, but
   delete it afterwards anyway:

   ```sh
   rc=$(scripts/next-version.sh --rc) # e.g. v0.1.0-rc.1
   git tag "$rc" && git push origin "$rc"
   gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
   gh release delete "$rc" --cleanup-tag --yes
   ```

3. Tag the release and push the tag:

   ```sh
   git tag -a "$tag" -m "Nota ${tag#v}" && git push origin "$tag"
   ```

4. Wait for the workflow (about 10 minutes) and check the release page: 10 files plus
   `checksums.txt`. Edit the generated notes if needed.
5. Bump the Homebrew cask (needs your own push access to `banjo/homebrew-tap`):

   ```sh
   scripts/bump-cask.sh 0.1.0 --dry-run      # print the cask, check it with ruby -c / brew style
   scripts/bump-cask.sh 0.1.0                # clone the tap, write Casks/nota.rb, commit, push
   ```

   The script downloads `Nota-0.1.0-arm64.zip`, checks it against `checksums.txt` and fills in
   `packaging/homebrew/nota.rb`. It refuses prereleases unless `--dry-run` or
   `--allow-prerelease`. Then `brew update && brew install --cask banjo/tap/nota` to try it.

6. Redeploy the landing page. It reads the latest release at build time
   (`apps/landing/src/data/release.ts`, GitHub's `releases/latest`, which never returns a
   prerelease or a draft), so the download buttons only appear after a rebuild:

   ```sh
   rm -rf dist && bunx vp run site:build
   test -f dist/app/index.html && test -f dist/index.html && test -f dist/_worker.js
   bunx -y wrangler@4 pages deploy dist --project-name nota --branch main
   ```

## When something fails

- Re-running failed jobs is safe: the publish job uploads with `--clobber` into a release
  that already exists.
- A broken tag: `gh release delete vX --cleanup-tag --yes`, fix on `main`, tag again.
- No Developer ID yet: when there is one, set `macos.signingIdentity` and `macos.notarize` in
  `apps/desktop/mygo.config.ts` and import the certificate in the macOS job
  ([MyGo's GitHub Actions guide](https://mygo.run/docs/github-actions)); then drop the
  quarantine `postflight` from the cask and the Gatekeeper note from the README.
