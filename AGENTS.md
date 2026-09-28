# Working on commitell

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [README.md](README.md) before
editing. The Makefile and CI workflow own the checks; the workspace manifest
only runs those commands in the pinned Nix development shell.

- Keep test Git repositories disposable and model requests local to test HTTP
  servers. Never use a real provider key, push target, or private diff in tests.
- Keep Go and Nix package dependencies aligned. The current Go module uses only
  the standard library.
- Run `project-check fast` after changes and `project-check full` before handoff.
  The race-enabled Go tests need a C compiler, supplied by the Nix shell.
- Follow the DCO sign-off rule in CONTRIBUTING.md for commits.
