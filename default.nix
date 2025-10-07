{ pkgs ? import
    (fetchTarball {
      name = "jpetrucciani-2025-10-07";
      url = "https://github.com/jpetrucciani/nix/archive/15d79d49616d420eb45e52479c42d57ff8f58537.tar.gz";
      sha256 = "1z373gnlz41zvqjl8hq7ks2nzsss6c1q8mv95vamxzhq6jcsqwfj";
    })
    { }
}:
let
  name = "caddy-gcs-proxy";

  tools = with pkgs; {
    go = [
      go_1_25
      go-tools
      gopls
      xcaddy
    ];
    scripts = pkgs.lib.attrsets.attrValues scripts;
  };

  scripts = with pkgs; rec {
    run-gcs-proxy = pog {
      name = "run-gcs-proxy";
      description = "run caddy with the gcs-proxy plugin in watch mode against the caddyfile in the conf dir";
      script = ''
        ${xcaddy}/bin/xcaddy run --config ./conf/Caddyfile --watch "$@"
      '';
    };
    run = pog {
      name = "run";
      description = "run run-gcs-proxy, restarting when go files are changed";
      script = ''
        ${findutils}/bin/find . -iname '*.go' | ${entr}/bin/entr -rz ${run-gcs-proxy}/bin/run-gcs-proxy
      '';
    };
  };
  paths = pkgs.lib.flatten [ (builtins.attrValues tools) ];
  env = pkgs.buildEnv {
    inherit name paths; buildInputs = paths;
  };
in
(env.overrideAttrs (_: {
  inherit name;
  NIXUP = "0.0.8";
})) // { inherit scripts; }
