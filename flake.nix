{
  description = "commitell — LLM-generated conventional commit messages";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        commitell =
          let
            version = "0.2.0";
          in
          pkgs.buildGoModule {
            pname = "commitell";
            inherit version;
            src = self;
            vendorHash = null; # stdlib only, no dependencies
            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];
            nativeBuildInputs = [ pkgs.makeWrapper ];
            nativeCheckInputs = [ pkgs.git ]; # tests shell out to git

            postInstall = ''
              wrapProgram "$out/bin/commitell" \
                --prefix PATH : ${
                  pkgs.lib.makeBinPath [
                    pkgs.git
                    pkgs.gh
                  ]
                }
            '';

            meta = {
              description = "LLM-generated conventional commit messages";
              homepage = "https://github.com/timfewi/commitell";
              license = pkgs.lib.licenses.mit;
              mainProgram = "commitell";
              platforms = systems;
            };
          };
        default = commitell;
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.git
            pkgs.gh
            pkgs.gnumake
            pkgs.nixfmt-tree
          ];

          shellHook = ''
            echo "commitell development shell: $(go version)"
          '';
        };
      });
    };
}
