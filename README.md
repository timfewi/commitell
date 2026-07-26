![commitell banner](public/commitell-readme-banner.png)

# commitell

`commitell` turns every dirty change in the current Git repository into one
AI-written, DCO-signed commit.

```sh
export OPENROUTER_API_KEY=sk-or-v1-...
commitell
```

It includes tracked, staged, unstaged, deleted, and untracked files. There is
no prompt and no automatic push.

## Privacy

Every OpenRouter request requires both Zero Data Retention and denied data
collection. The fixed model order is:

1. `google/gemini-3.1-flash-lite`
2. `qwen/qwen3-coder-30b-a3b-instruct`

If neither model is available under those policies, `commitell` stops before
staging anything. It also rejects common secret filenames and token patterns
locally, omits binary contents, and sends only a bounded diff plus recent
commit subjects.

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

It then runs the equivalent of:

```sh
git add -A
git commit -s -m "<subject>" -m "<optional body>"
```

Normal Git hooks remain active. If a hook or Git itself rejects the commit,
the changes remain staged and no file content is discarded.

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
