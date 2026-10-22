{
  description = "slipbox: turns a source's text into traced, refined notes";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        # `nix develop` - reproducible dev environment.
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_26
            gopls
            gotools
          ];

          shellHook = ''
            export GOFLAGS="-mod=readonly"
            # The flake's go_1_26 is the only toolchain; fail loudly rather than
            # silently downloading one if go.mod ever outruns nixpkgs.
            export GOTOOLCHAIN="local"
          '';
        };
      }
    );
}
