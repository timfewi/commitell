![commitell banner](public/commitell-readme-banner.png)

# commitell

`commitell` turns Git changes into one or more AI-written, DCO-signed commits.
With no options it preserves the original workflow and commits the complete
dirty working tree.

```sh
export OPENROUTER_API_KEY=sk-or-v1-...
commitell
```

Select, split, and publish changes when needed:

```sh
commitell --staged --exclude "broken.txt,generated.json"
commitell --split --solver google/gemini-3.1-flash-lite --dry-run
commitell --split --push --pr --base main
```

List models that are compatible with your OpenRouter account, its privacy
settings and guardrails, commitell's required parameters, and an available
Zero Data Retention endpoint:

```sh
commitell --models
commitell --models --eu
```

`--models --eu` and commit runs with `--eu` use OpenRouter's EU in-region
routing. This is a routing property, not a legal GDPR certification.

## Options

- `--staged` commits only the content already in the Git index.
- `--exclude FILES` excludes exact repository-relative paths. Repeat the flag
  or pass comma-separated paths. Globs and filenames containing commas are not
  supported.
- `--split` asks the model to group whole files into at most eight logical
  commits. A file and both sides of a rename remain in one commit.
- `--solver MODEL` is repeatable and defines the complete model fallback order.
- `--dry-run` generates and prints the plan without changing Git or publishing.
- `--eu` routes model discovery and completions through `eu.openrouter.ai`.
- `--push` pushes the current non-default branch after all commits succeed.
- `--pr` implies `--push` and creates a draft pull request with GitHub CLI.
- `--remote NAME` selects the publishing remote; the default is `origin`.
- `--base BRANCH` declares the protected default and pull-request base branch.

If a changed file cannot safely be read or contains a likely secret, commitell
stops before an API request and names the file:

```text
Try again with --exclude "path/to/file".
```

Files are never excluded automatically.

## Privacy

Every OpenRouter completion request requires both Zero Data Retention and
denied data collection. The default model order is:

1. `google/gemini-3.1-flash-lite`
2. `qwen/qwen3-coder-30b-a3b-instruct`

Repeated `--solver` flags replace this order. If no selected model is available
under the required policies, `commitell` stops before staging anything. It also
rejects common secret filenames and token patterns locally, omits binary
contents, and sends only a bounded diff plus recent commit subjects.

`--models` obtains the account-filtered list from OpenRouter and intersects it
with current ZDR endpoints. OpenRouter applies the API key's account privacy
settings and guardrails to that list; commitell does not request privileged
guardrail details or require a management key.

The local scanner is deliberately conservative, but it is not a complete
secret-management system. OpenRouter still processes the request and retains
request metadata under its published policy even when prompt retention is
disabled.

## Install

Requires Go 1.26+ and Git:

```sh
go install github.com/timfewi/commitell@latest
```

Or build the current checkout:

```sh
go build .
```

### Nix

Run it without installing anything:

```sh
nix run github:timfewi/commitell
```

The flake declares `packages.default` and `devShells.default` for `x86_64` and
`aarch64` on Linux and Darwin. To install it from a NixOS or home-manager
config, add the input and reference the package in a module that receives
`inputs`:

```nix
# flake.nix
inputs.commitell = {
  url = "github:timfewi/commitell";
  inputs.nixpkgs.follows = "nixpkgs";
};

# any module with `inputs` in scope
environment.systemPackages = [
  inputs.commitell.packages.${pkgs.stdenv.hostPlatform.system}.default
];
```

`commitell` reads `OPENROUTER_API_KEY` from the environment. Do not put the key
in a Nix expression — string literals end up world-readable in `/nix/store`.
Wrap the package instead and read the secret at runtime, for example from
[agenix](https://github.com/ryantm/agenix):

```nix
pkgs.writeShellApplication {
  name = "commitell";
  runtimeInputs = [ pkgs.git pkgs.coreutils ];
  text = ''
    OPENROUTER_API_KEY="$(cat ${config.age.secrets.openrouter-api-key.path})" \
      exec ${inputs.commitell.packages.${pkgs.stdenv.hostPlatform.system}.default}/bin/commitell "$@"
  '';
}
```

## Behavior

Before changing the index, `commitell`:

- rejects merge conflicts and in-progress Git operations;
- checks `user.name` and `user.email`;
- scans the outbound text for likely secrets;
- generates and validates a subject with an optional body;
- verifies that the working tree did not change during analysis.

With no selection options it then runs the equivalent of:

```sh
git add -A
git commit -s -m "<subject>" -m "<optional body>"
```

Normal Git hooks remain active. Selective and split commits use a temporary Git
index so excluded or unrelated staged changes remain untouched. If a later
split commit fails, earlier split commits remain in history and commitell
reports how many groups completed; it never discards the remaining working-tree
or index content.

Publishing is deliberately explicit. It refuses detached HEAD and the declared
or detected default branch, never force-pushes, and never creates a branch. PR
preflight checks `gh auth status`; commitell then pushes itself and invokes
`gh pr create --draft --fill` with explicit base and head branches.

## Development

```sh
go test ./...
go vet ./...
```

With Nix, `nix develop` provides Go, gopls, and Git; `nix build` runs the tests
as part of the build.

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the
small set of project rules.

## License

MIT
