# Release guide

Releases use semantic version tags such as `v0.2.0`. The version in
`flake.nix`, the Git tag, the CLI output, and the Nixpkgs package must match.

## Publish an upstream release

1. Update the package version in `flake.nix` and user-facing documentation.
2. Run the complete local gate:

   ```sh
   make release-check
   ```

3. Commit the release changes with a DCO sign-off and wait for CI on `main`:

   ```sh
   git commit -s
   ```

4. Create and push a signed tag from the tested commit when a signing key is
   configured:

   ```sh
   git tag -s v0.2.0 -m "commitell 0.2.0"
   git push origin v0.2.0
   ```

   Otherwise, create an annotated tag:

   ```sh
   git tag -a v0.2.0 -m "commitell 0.2.0"
   git push origin v0.2.0
   ```

The release workflow rejects a tag that does not match the Nix package
version. It reruns the Go and Nix checks, builds Linux amd64/arm64 and Darwin
arm64 archives, writes SHA-256 checksums, and creates the GitHub release.

## Submit to Nixpkgs

Fork and clone `NixOS/nixpkgs`, create a branch from `upstream/master`, and add
`pkgs/by-name/co/commitell/package.nix`:

```nix
{
  lib,
  buildGoModule,
  fetchFromGitHub,
  gh,
  git,
  makeWrapper,
}:

buildGoModule (finalAttrs: {
  pname = "commitell";
  version = "0.2.0";

  src = fetchFromGitHub {
    owner = "timfewi";
    repo = "commitell";
    tag = "v${finalAttrs.version}";
    hash = lib.fakeHash;
  };

  vendorHash = null;
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
  ];

  nativeBuildInputs = [ makeWrapper ];
  nativeCheckInputs = [ git ];

  postInstall = ''
    wrapProgram "$out/bin/commitell" \
      --prefix PATH : ${lib.makeBinPath [ git gh ]}
  '';

  meta = {
    description = "LLM-generated conventional commit messages";
    homepage = "https://github.com/timfewi/commitell";
    changelog = "https://github.com/timfewi/commitell/releases/tag/v${finalAttrs.version}";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ timfewi ];
    mainProgram = "commitell";
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
      "aarch64-darwin"
    ];
  };
})
```

Replace `lib.fakeHash` with the hash Nix reports from the first build. If this
is the first Nixpkgs contribution for this maintainer, add the following entry
to `maintainers/maintainer-list.nix` in a separate commit:

```nix
timfewi = {
  name = "Tim Witter";
  github = "timfewi";
  githubId = 123400131;
};
```

From the Nixpkgs root, validate the package and executable:

```sh
nix build .#commitell
./result/bin/commitell --version
nix run nixpkgs#nixpkgs-review -- wip
./ci/nixpkgs-vet.sh master
```

Use `commitell: init at 0.2.0` as the Nixpkgs commit and pull-request title,
complete the Nixpkgs PR checklist, and state the platforms actually tested.
