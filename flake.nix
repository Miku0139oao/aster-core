{
  description = "Aster Core";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  inputs.utils.url = "github:numtide/flake-utils";

  outputs = { self, nixpkgs, utils }:
    utils.lib.eachDefaultSystem
      (system:
        let
          pkgs = import nixpkgs {
            inherit system;
            overlays = [ self.overlay ];
          };
        in
        rec {
          packages.default = pkgs.aster-core;
        }
      ) //
    (
      let version = nixpkgs.lib.substring 0 8 self.lastModifiedDate or self.lastModified or "19700101"; in
      {
        overlay = final: prev: {

          aster-core = final.buildGoModule {
            pname = "aster-core";
            inherit version;
            src = ./.;

            vendorHash = "sha256-4AVCfBDRdBr/shmz0ZGvDeam+IIo50Mjcukd6tNPJ/g=";

            subPackages = [ "." ];

            env.CGO_ENABLED = 0;

            ldflags = [
              "-s"
              "-w"
              "-X github.com/Miku0139oao/aster-core/constant.Version=dev-${version}"
              "-X github.com/Miku0139oao/aster-core/constant.BuildTime=${version}"
            ];
            
            tags = [
              "with_gvisor"
            ];

            # Network required 
            doCheck = false;

          };
        };
      }
    );
}

