{
  description = "commitell — LLM-generated conventional commit messages";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        commitell = pkgs.buildGoModule {
          pname = "commitell";
          version = "0.1.0";
          src = self;
          vendorHash = null; # stdlib only, no dependencies
          nativeCheckInputs = [ pkgs.git ]; # tests shell out to git
          meta = {
            description = "LLM-generated conventional commit messages";
            homepage = "https://github.com/timfewi/commitell";
            license = pkgs.lib.licenses.mit;
            mainProgram = "commitell";
          };
        };
        default = commitell;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.gopls pkgs.git ];
        };
      });
    };
}
