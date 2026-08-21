# Building and testing

Install the development dependencies and build all binaries with:

```sh
./tools/install-go.sh
npm --prefix web ci
bazel build //:build
bazel test //cmd/... //internal/...
bazel build //:frontend_checks
```

The common workflows also have short commands:

```sh
./b build
./b test
./b release
```

`./b test` runs both Go and frontend checks. `./b release` builds the complete
Linux amd64/arm64 suite and the Intel Mac hub in one parallel Bazel invocation,
then publishes the binaries with stable names in `bin/`. Unsuffixed names in
`bin/` point to the current build machine's platform when it is released.

`bazel build //:build` builds all three host binaries in parallel and does not
run tests. `bazel test //cmd/... //internal/...` runs Go tests, while
`bazel build //:frontend_checks` runs the independently scheduled frontend
lint, type-check, and unit-test targets. Run `bazel run //cmd/line-count` to
inspect source line counts.

Host binaries are available below `bazel-bin/cmd/`. Build the complete release
matrix directly with:

```sh
bazel build //:release
```

The Intel Mac hub is a CGO-free `darwin/amd64` binary. Repository servers and
agent CLIs are released for Linux amd64 and arm64.

## Publish a GitHub release

Install the [GitHub CLI](https://cli.github.com/), then create a release by
passing a version tag to the release script:

```sh
./release.sh v0.1.0
```

The script interactively asks for a GitHub personal access token with permission
to create releases in `OrKoN/git-review`. Input is hidden, the token is provided
to `gh` only for the lifetime of the script, and it is not stored by the script.
If the tag does not exist in the destination repository, the script creates it
at the current tip of the repository's default branch. It then clones that exact
tag into a temporary directory, verifies the checkout, runs the complete test
and release builds, verifies every expected binary, creates the GitHub release,
generates release notes, and uploads all platform-specific binaries. It does not
upload the unsuffixed local convenience copies from `bin/`.

Set `GIT_REVIEW_REPOSITORY` to publish to another `OWNER/REPOSITORY`.
