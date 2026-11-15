# Contributing

Open an issue before starting a large change. Small fixes can go straight to a
pull request.

Before submitting:

```sh
project-check fast
project-check full
```

The workspace commands run the Makefile checks in the pinned Nix development
shell. For direct use outside the workspace:

```sh
nix develop . --command make check
nix develop . --command make nix-check
```

`make check` verifies Go formatting, runs the test suite with the race detector,
and runs `go vet`. `make nix-check` checks Nix formatting and lint, evaluates
the flake on all supported systems, and builds the current system's package. If
Nix is not installed, run `go test -race ./...` and `go vet ./...`; the package
build is then checked with `make nix-check` before merging.

Keep the CLI and its help in sync. Check both `commitell --help` and
`commitell tree --help` when changing flags or behavior, and update the README's
option and environment descriptions. Record user-facing changes under
`Unreleased` in [CHANGELOG.md](CHANGELOG.md). Terminal output must work in all three
`--color` modes; keep JSON and version output plain. Preserve the standard
library-only Go module unless a dependency is needed and its Nix packaging is
updated in the same change.

Tests must use disposable Git repositories, synthetic content and local HTTP
servers. Never call a model provider or publish to a real remote in tests.

Keep changes focused and sign every commit for DCO compliance:

```sh
git commit -s
```

By contributing, you certify the
[Developer Certificate of Origin](https://developercertificate.org/).

Do not include API keys, private diffs, or other credentials in issues, test
fixtures, commits, or CI logs. Use synthetic values in regression tests.
