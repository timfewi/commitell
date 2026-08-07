package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseOptionsComposesWorkflowFlags(t *testing.T) {
	opts, err := parseOptions([]string{
		"--staged",
		"--exclude", "broken.txt,generated.json",
		"--exclude", "docs/draft.md",
		"--solver", "model/one",
		"--solver", "model/two",
		"--split",
		"--dry-run",
		"--eu",
		"--pr",
		"--remote", "upstream",
		"--base", "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.staged || !opts.split || !opts.dryRun || !opts.eu || !opts.push || !opts.pullRequest {
		t.Fatalf("boolean options not parsed: %+v", opts)
	}
	if want := []string{"broken.txt", "generated.json", "docs/draft.md"}; !reflect.DeepEqual(opts.excludes, want) {
		t.Fatalf("excludes = %#v, want %#v", opts.excludes, want)
	}
	if want := []string{"model/one", "model/two"}; !reflect.DeepEqual(opts.solvers, want) {
		t.Fatalf("solvers = %#v, want %#v", opts.solvers, want)
	}
	if opts.remote != "upstream" || opts.base != "main" {
		t.Fatalf("publish options not parsed: %+v", opts)
	}
}

func TestParseOptionsModelsIsReadOnly(t *testing.T) {
	if _, err := parseOptions([]string{"--models", "--eu"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--models", "--solver", "model/one"},
		{"--models", "--push"},
		{"--models", "--exclude", "file.txt"},
	} {
		if _, err := parseOptions(args); err == nil || !strings.Contains(err.Error(), "--models can only") {
			t.Fatalf("args %v: unexpected error %v", args, err)
		}
	}
}

func TestParseOptionsRejectsUnsafeExclude(t *testing.T) {
	for _, path := range []string{"/tmp/file", "../file", ""} {
		if _, err := parseOptions([]string{"--exclude", path}); err == nil {
			t.Fatalf("accepted excluded path %q", path)
		}
	}
}
