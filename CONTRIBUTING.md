# Contributing

Open an issue before starting a large change. Small fixes can go straight to a
pull request.

Before submitting:

```sh
nix develop path:. --command make check
nix develop path:. --command make nix-check
```

`make check` verifies Go formatting, runs the test suite with the race detector,
and runs `go vet`. `make nix-check` checks Nix formatting and lint, evaluates
the flake on all supported systems, and builds the current system's package. If
Nix is not installed, run `go test -race ./...` and `go vet ./...` and let the
Nix CI job cover the package build.

Keep changes focused and sign every commit for DCO compliance:

```sh
git commit -s
```

By contributing, you certify the
[Developer Certificate of Origin](https://developercertificate.org/).

Do not include API keys, private diffs, or other credentials in issues, test
fixtures, commits, or CI logs. Use synthetic values in regression tests.
