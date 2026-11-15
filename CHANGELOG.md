# Changelog

## Unreleased

### Added

- Interactive repository overviews start at the top in `less`, with scrolling,
  search and `q` to quit; short output and redirected output remain direct.
- Shared terminal colors for help, previews, success messages, warnings and
  errors. Both commit and overview modes accept `--color auto|always|never`.
- Grouped, colored help for both workflows, with examples and current defaults.
- A `just tree-help` shortcut for the repository overview.

### Fixed

- Dry runs display the full commit message, including its body.
- Overview help works even with invalid legacy environment defaults.
- Explicit overview flags override `RT_DEPTH` and `RT_MAX_FILES` defaults.

### Documentation

- Refreshed the quick start, option reference, color guidance and contribution
  checks.
