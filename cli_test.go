package main

import (
	"bytes"
	"errors"
	"flag"
	"reflect"
	"strings"
	"testing"
)

func TestParseOptionsRecognizesHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--staged", "--help"}} {
		if _, err := parseOptions(args); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("args %v: error = %v, want flag.ErrHelp", args, err)
		}
	}
}

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

func TestParseOptionsSupportsModelAliasAutoSelectionAndForce(t *testing.T) {
	opts, err := parseOptions([]string{"--model", "model/one", "--solver", "model/two", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.force || !reflect.DeepEqual(opts.solvers, []string{"model/one", "model/two"}) {
		t.Fatalf("unexpected options: %+v", opts)
	}
	if _, err := parseOptions([]string{"--auto-model", "--model", "model/one"}); err == nil || !strings.Contains(err.Error(), "--auto-model") {
		t.Fatalf("unexpected auto-model conflict: %v", err)
	}
}

func TestParseOptionsSupportsOfflineAndRejectsModelRoutingFlags(t *testing.T) {
	opts, err := parseOptions([]string{"--offline", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.offline || !opts.dryRun {
		t.Fatalf("offline options not parsed: %+v", opts)
	}
	for _, args := range [][]string{
		{"--offline", "--model", "model/one"},
		{"--offline", "--auto-model"},
		{"--offline", "--eu"},
		{"--offline", "--split"},
	} {
		if _, err := parseOptions(args); err == nil || !strings.Contains(err.Error(), "--offline cannot") {
			t.Fatalf("args %v: unexpected error %v", args, err)
		}
	}
}

func TestParseOptionsSupportsShortFormsAndBooleanClusters(t *testing.T) {
	opts, err := parseOptions([]string{
		"-scdfpP",
		"-x", "broken.txt,generated.json",
		"-m", "model/one",
		"-m=model/two",
		"-r", "upstream",
		"-b", "main",
		"-v",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.staged || !opts.split || !opts.dryRun || !opts.force || !opts.push || !opts.pullRequest || !opts.version {
		t.Fatalf("clustered boolean options not parsed: %+v", opts)
	}
	if want := []string{"broken.txt", "generated.json"}; !reflect.DeepEqual(opts.excludes, want) {
		t.Fatalf("excludes = %#v, want %#v", opts.excludes, want)
	}
	if want := []string{"model/one", "model/two"}; !reflect.DeepEqual(opts.solvers, want) {
		t.Fatalf("models = %#v, want %#v", opts.solvers, want)
	}
	if opts.remote != "upstream" || opts.base != "main" {
		t.Fatalf("short value options not parsed: %+v", opts)
	}

	listed, err := parseOptions([]string{"-le"})
	if err != nil || !listed.models || !listed.eu {
		t.Fatalf("clustered model listing not parsed: %+v, %v", listed, err)
	}
	automatic, err := parseOptions([]string{"-a"})
	if err != nil || !automatic.autoModel {
		t.Fatalf("short auto-model not parsed: %+v, %v", automatic, err)
	}
	offline, err := parseOptions([]string{"-od"})
	if err != nil || !offline.offline || !offline.dryRun {
		t.Fatalf("clustered offline options not parsed: %+v, %v", offline, err)
	}
	withTrailingValue, err := parseOptions([]string{"-sdm", "model/one"})
	if err != nil || !withTrailingValue.staged || !withTrailingValue.dryRun || !reflect.DeepEqual(withTrailingValue.solvers, []string{"model/one"}) {
		t.Fatalf("trailing value option not parsed: %+v, %v", withTrailingValue, err)
	}
}

func TestParseOptionsRejectsAmbiguousValueCluster(t *testing.T) {
	if _, err := parseOptions([]string{"-mcf"}); err == nil || !strings.Contains(err.Error(), "must be last") {
		t.Fatalf("ambiguous value cluster error = %v", err)
	}
}

func TestUsageExplainsOptionsPrivacyAndUseCases(t *testing.T) {
	var out bytes.Buffer
	usage(&out)
	for _, want := range []string{
		"Change selection:",
		"Models and privacy:",
		"Short option clusters:",
		"Common use cases:",
		"commitell -scd",
		"Gemini 3.1 Flash Lite -> Gemini 2.5 Flash Lite -> GPT-4o mini",
		"--force never relaxes these provider rules",
		"OPENROUTER_API_KEY",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help output missing %q:\n%s", want, out.String())
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
