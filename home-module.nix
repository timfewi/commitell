{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.commitell;
  wrapper = pkgs.writeShellApplication {
    name = "commitell";
    text = ''
      export COMMITELL_PROVIDER=openai-compatible
      export COMMITELL_BASE_URL=https://api.openai.com/v1
      default_model=${lib.escapeShellArg cfg.model}
      default_key_file=${lib.escapeShellArg cfg.apiKeyFile}
      export COMMITELL_MODELS="''${COMMITELL_MODELS:-$default_model}"
      if [[ -z "''${COMMITELL_API_KEY:-}" ]]; then
        key_file="''${COMMITELL_API_KEY_FILE:-$default_key_file}"
        if [[ -n "$key_file" && -r "$key_file" ]]; then
          COMMITELL_API_KEY=$(< "$key_file")
          export COMMITELL_API_KEY
        fi
      fi
      exec ${lib.getExe cfg.package} "$@"
    '';
  };
in
{
  options.programs.commitell = {
    enable = lib.mkEnableOption "commitell with OpenAI runtime credentials";

    model = lib.mkOption {
      type = lib.types.str;
      default = "gpt-6-luna";
      description = "Default OpenAI model. COMMITELL_MODELS or --model can override it.";
    };

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.commitell;
      defaultText = lib.literalExpression "pkgs.commitell";
      description = "The commitell package.";
    };

    apiKeyFile = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = ''
        Runtime file containing the OpenAI key. COMMITELL_API_KEY takes
        precedence, followed by COMMITELL_API_KEY_FILE, then this path.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [
      wrapper
      (pkgs.writeShellScriptBin "cmt" ''exec ${lib.getExe wrapper} "$@"'')
    ];
  };
}
