{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {

  buildInputs = [
    pkgs.grpc-tools
    pkgs.protoc-gen-go-grpc
  ];
}
