# Show all available repository commands.
default:
    @just --list

# Run formatting checks, race-enabled tests, and go vet.
check:
    make check

# Format Go and Nix sources.
fmt:
    make fmt

# Check Go formatting without modifying files.
fmt-check:
    make fmt-check

# Run the Go test suite with the race detector.
test:
    make test

# Run Go's static analyzer.
vet:
    make vet

# Validate the Nix flake.
nix-check:
    make nix-check

# Run all release checks.
release-check:
    make release-check

# Build the commitell executable.
build:
    go build .

# Run commitell from source and forward optional arguments.
run *args:
    go run . {{args}}

# Show commitell's user help.
help:
    go run . --help
