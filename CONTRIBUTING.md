# Contributing

## Commit messages

Pull requests are squash-merged, and their titles follow
[Conventional Commits](https://www.conventionalcommits.org/), for example
`feat(rust): …`, `fix(python): …` or `docs(go): …`. release-please derives versions and
release notes from them:

- `feat` bumps the minor version, `fix` the patch version, and `!` after the type or a
  `BREAKING CHANGE:` footer the major version;
- other types (`docs`, `build`, `ci`, `chore`, `refactor`, `test`, …) release nothing and are
  left out of the release notes.

## Releases

Each SDK is versioned and released on its own by
[release-please](https://github.com/googleapis/release-please) in manifest mode
(`release-please-config.json`, `.release-please-manifest.json`):

| SDK | Directory | Tag | Published to | Workflow |
| --- | --- | --- | --- | --- |
| Rust | `rust/` | `rust/vX.Y.Z` | [crates.io](https://crates.io/crates/topstepx) | `publish-rust.yml` |
| Python | `python/` | `python/vX.Y.Z` | [PyPI](https://pypi.org/project/topstepx/) | `publish-python.yml` |
| Go | `go/` | `go/vX.Y.Z` | [Go module proxy](https://pkg.go.dev/github.com/CurtisTechSolutions/TopstepX/go) | `publish-go.yml` |

1. A releasable commit on `main` that touches an SDK's directory makes release-please open or
   update the release pull request, which bumps that SDK's version and `CHANGELOG.md`.
2. Merging the release pull request creates the SDK's tag and GitHub release.
3. The tag starts the SDK's publish workflow. To retry a failed publish, run the workflow from
   the tag (Actions, the workflow, Run workflow, then the tag under "Use workflow from").

release-please writes the version into `rust/Cargo.toml` and `rust/Cargo.lock`, and into the
lines marked `x-release-please-version` in `python/src/topstepx/_version.py` and
`go/version.go`. Keep those markers.

Credentials:

- `MY_RELEASE_PLEASE_TOKEN` (repository secret): a personal access token for release-please.
  Tags it creates start the publish workflows; tags created with `GITHUB_TOKEN` would not.
- `CARGO_REGISTRY_TOKEN` (repository secret): a crates.io API token with the `publish-update`
  scope (and `publish-new` for the first release) for the `topstepx` crate.
- PyPI uses trusted publishing, so no token is stored: the `topstepx` project on PyPI trusts
  `publish-python.yml` running in the `pypi` environment of this repository.
- Go needs no credentials: the tag is the release.
