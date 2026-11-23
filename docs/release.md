# Cutting a release

A release is a `v*` tag. `.github/workflows/release.yml` builds everything from it and
publishes the GitHub release; the Homebrew cask and the landing page are updated by hand
afterwards.

## What the workflow does

| Job | Runner | Output |
| --- | --- | --- |
| `version` | ubuntu | the version: the tag without `v`; a prerelease when it has a `-` suffix (`v1.2.0-rc.1`) |
| `cli` (×4) | ubuntu | `nota_<v>_{darwin,linux}_{amd64,arm64}.tar.gz` (pure Go, `CGO_ENABLED=0`, cross-compiled), each with `nota`, `LICENSE`, `README.md` |
| `macos` | macos-14 (arm64) | `Nota-<v>-arm64.dmg` and `Nota-<v>-arm64.zip` from `mygo build -platform darwin/arm64`, signed ad hoc, with the CLI inside at `Contents/Resources/bin/nota` |
| `linux` | ubuntu-24.04 | `nota_<v>_amd64.deb`, `nota-<v>-1.x86_64.rpm`, `Nota-<v>-linux-amd64.tar.gz` (MyGo) and `Nota-<v>-x86_64.AppImage` (`scripts/appimage.sh`) |
| `publish` | ubuntu | the GitHub release (not a draft; `--prerelease` for `-` versions, else `--latest`) with every file and `checksums.txt` |

The version is linked into every binary: `-X github.com/banjo/nota/internal/cli.Version` for
the CLI (`nota --version`) and `NOTA_VERSION` for the app (`apps/desktop/mygo.config.ts`:
Info.plist, the packages). Nothing in the repository needs a version bump before tagging.
MyGo writes a prerelease such as `1.2.0-rc.1` as `1.2.0~rc.1` in the .deb and .rpm, so it
sorts before `1.2.0`.

The macOS job checks the app before uploading it: Info.plist version, `codesign -dv` (ad hoc),
`codesign --verify --deep --strict`, `hdiutil verify` and a mount of the DMG, the bundled
CLI's `--version`, and that the app is still running 10 s after start.

A manual run (Actions → Release → Run workflow) builds everything as run artifacts and
publishes nothing unless it runs on a tag.

## Steps

1. Make sure `main` is green (CI) and contains what should ship.
2. Optionally try the pipeline with a prerelease first:

   ```sh
   git tag v1.2.0-rc.1 && git push origin v1.2.0-rc.1
   gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
   ```

   Delete it afterwards with `gh release delete v1.2.0-rc.1 --cleanup-tag --yes`.

3. Tag the release on `main` and push the tag:

   ```sh
   git switch main && git pull
   git tag -a v1.2.0 -m "Nota 1.2.0" && git push origin v1.2.0
   ```

4. Wait for the workflow (about 10 minutes) and check the release page: 10 files plus
   `checksums.txt`. Edit the generated notes if needed.
5. Bump the Homebrew cask (needs your own push access to `banjo/homebrew-tap`):

   ```sh
   scripts/bump-cask.sh 1.2.0 --dry-run      # print the cask, check it with ruby -c / brew style
   scripts/bump-cask.sh 1.2.0                # clone the tap, write Casks/nota.rb, commit, push
   ```

   The script downloads `Nota-1.2.0-arm64.zip`, checks it against `checksums.txt` and fills in
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
