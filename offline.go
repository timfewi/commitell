package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func generateOfflineMessage(snap snapshot) commitMessage {
	return commitMessage{Subject: offlineSubject(snap)}
}

func offlineSubject(snap snapshot) string {
	if len(snap.changes) == 0 {
		return "chore: update repository"
	}
	if len(snap.changes) == 1 {
		return offlineSingleSubject(snap.changes[0])
	}

	category := pathCategory(snap.changes[0].Path)
	sameCategory := category != ""
	action := offlineAction(snap.changes[0])
	sameAction := true
	for _, item := range snap.changes[1:] {
		if pathCategory(item.Path) != category {
			sameCategory = false
		}
		if offlineAction(item) != action {
			sameAction = false
		}
	}
	if sameCategory {
		switch category {
		case "docs":
			return "docs: update documentation"
		case "test":
			return "test: update tests"
		case "ci":
			return "ci: update workflows"
		case "build":
			return "build: update build configuration"
		}
	}
	if sameAction {
		switch action {
		case "add":
			return fmt.Sprintf("feat: add %d files", len(snap.changes))
		case "remove":
			return fmt.Sprintf("chore: remove %d files", len(snap.changes))
		case "rename":
			return fmt.Sprintf("refactor: rename %d files", len(snap.changes))
		}
	}
	return fmt.Sprintf("chore: update %d files", len(snap.changes))
}

func offlineSingleSubject(item changeSnapshot) string {
	action := offlineAction(item)
	category := pathCategory(item.Path)
	kind := category
	if kind == "" {
		switch action {
		case "add":
			kind = "feat"
		case "rename":
			kind = "refactor"
		default:
			kind = "chore"
		}
	}

	detail := filepath.Base(filepath.FromSlash(item.Path))
	if action == "rename" && item.OldPath != "" {
		detail = filepath.Base(filepath.FromSlash(item.OldPath)) + " to " + detail
	}
	return boundedSubject(kind, action+" "+detail)
}

func offlineAction(item changeSnapshot) string {
	if item.OldPath != "" || strings.ContainsAny(item.Code, "RC") {
		return "rename"
	}
	if item.Untracked || strings.Contains(item.Code, "A") || item.Code == "??" {
		return "add"
	}
	if strings.Contains(item.Code, "D") {
		return "remove"
	}
	return "update"
}

func pathCategory(path string) string {
	path = strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(path)
	if strings.HasPrefix(path, "docs/") ||
		strings.HasSuffix(base, ".md") ||
		strings.HasSuffix(base, ".mdx") ||
		strings.HasSuffix(base, ".rst") ||
		strings.HasPrefix(base, "readme") {
		return "docs"
	}
	if strings.Contains(path, "/test/") ||
		strings.Contains(path, "/tests/") ||
		strings.HasPrefix(path, "test/") ||
		strings.HasPrefix(path, "tests/") ||
		strings.Contains(base, "_test.") ||
		strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") {
		return "test"
	}
	if strings.HasPrefix(path, ".github/workflows/") ||
		strings.HasPrefix(path, ".gitlab/") {
		return "ci"
	}
	switch base {
	case "go.mod", "go.sum", "flake.nix", "flake.lock", "makefile",
		"dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"cargo.toml", "cargo.lock":
		return "build"
	}
	return ""
}

func boundedSubject(kind, detail string) string {
	const maxSubjectRunes = 72
	prefix := kind + ": "
	available := maxSubjectRunes - utf8.RuneCountInString(prefix)
	if utf8.RuneCountInString(detail) <= available {
		return prefix + detail
	}
	if available <= 1 {
		return truncateRunes(prefix, maxSubjectRunes)
	}
	return prefix + truncateRunes(detail, available-1) + "…"
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
