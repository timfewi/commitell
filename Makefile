.PHONY: check fmt fmt-check nix-check release-check test vet

check: fmt-check test vet

fmt:
	gofmt -w *.go
	nix fmt

fmt-check:
	@test -z "$$(gofmt -l *.go)" || { \
		echo "Go files need formatting:"; \
		gofmt -l *.go; \
		exit 1; \
	}

test:
	go test -race ./...

vet:
	go vet ./...

nix-check:
	nix flake check

release-check: check nix-check
	@package_version="$$(nix eval --raw .#packages.$$(nix eval --raw --impure --expr builtins.currentSystem).commitell.version)"; \
	test "$$(go run -ldflags "-X main.version=$$package_version" . --version)" = "commitell $$package_version"; \
	echo "release checks passed for commitell $$package_version"
