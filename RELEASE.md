# Releasing Nota

```
 your PR                 main                         GitHub Actions
 ───────                 ────                         ──────────────
 change + changeset ──►  merge  ──► changesets.yml opens/updates the "Release Nota" PR
                                    (version bump + CHANGELOG)
                                                │
 review "Release Nota"  ◄───────────────────────┘
 merge it  ────────────►  main  ──► changesets.yml tags v<version>
                                               └──► release.yml builds and publishes
                                                    the GitHub release
 by hand: Homebrew cask + landing page
```

## 1. Add a changeset to your pull request

```sh
bunx changeset
```

Pick `@nota/desktop`, choose `patch`, and write one line for the release notes. Commit the file
it writes to `.changeset/` with your change. Skip it for changes users don't see (CI, docs,
tests).

## 2. Merge to main

`changesets.yml` collects every changeset on `main` into one pull request, **Release Nota**. It
bumps `apps/desktop/package.json`, writes `apps/desktop/CHANGELOG.md` and updates itself on every
new merge.

## 3. Merge "Release Nota"

That's the release. The workflow tags `v<version>` and starts `release.yml`, which builds the
CLI, the macOS app and the Linux packages and publishes them as a GitHub release. The notes are
the CHANGELOG section followed by the merged pull requests. It takes about 10 minutes.

## 4. Finish by hand

```sh
scripts/bump-cask.sh <version>                  # Homebrew cask
rm -rf dist && bunx vp run site:build           # landing page, then deploy dist
bunx -y wrangler@4 pages deploy dist --project-name nota --branch main
```

Prereleases, failure recovery and what each job checks: [docs/release.md](docs/release.md).
