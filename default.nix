{ pkgs ? import
    (fetchTarball {
      name = "jpetrucciani-2024-11-25";
      url = "https://github.com/jpetrucciani/nix/archive/10b10043084bd71cc60ea06f052e970f03464580.tar.gz";
      sha256 = "0b57gj31470imrdmz75iixgl30s6vqf4dww1yadwhzlzcprcymwg";
    })
    { }
}:
let
  name = "caddy-gcs-proxy";

  tools = with pkgs; {
    go = [
      go
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
