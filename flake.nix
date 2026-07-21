{
  description = "typist: adaptive typing trainer backend";

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
            # Go toolchain
            go_1_26
            gopls
            gotools
            golangci-lint
            govulncheck
            wgo # file-watch live reload for `make watch`

            # Database tools
            goose
            sqlc
            postgresql_18

            # Auth tools
            oapi-codegen

            # TUI dev tools
            asciinema
          ];

          shellHook = ''
            export DATABASE_URL="postgres://typing:typing@localhost:5432/typing?sslmode=disable"
            # Dev-only placeholder; config.go's prod guard rejects it when APP_ENV=production.
            export JWT_SECRET="dev-only-change-me"
            export GOFLAGS="-mod=readonly"
            echo "typist dev shell"
            echo "  go:        $(go version | cut -d' ' -f3)"
            echo "  goose:     $(goose --version 2>&1 | head -1)"
            echo "  sqlc:      $(sqlc version)"
            echo ""
            echo "Run 'make help' to list all make commands."
          '';
        };

        # `nix build` - reproducible binary build.
        # TODO(phase-7): packages.default = pkgs.buildGoModule { ... };

        # `nix build .#dockerImage` - reproducible Docker image, same artifact
        # as the Dockerfile but with Nix-pinned base layers.
        # TODO(phase-7): packages.dockerImage = pkgs.dockerTools.buildImage { ... };
      }
    )
    // {
      # NixOS module for deploying to the homelab.
      # TODO(phase-7): nixosModules.default = ./deploy/nix/module.nix;
    };
}
