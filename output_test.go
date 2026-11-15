package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var ansiStyle = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestOverviewPager(t *testing.T) {
	content := "first repository\n" + strings.Repeat("more repositories\n", 100) + "last repository\n"
	for _, behavior := range []string{"copy", "missing", "failure"} {
		t.Run(behavior, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			if behavior != "missing" {
				script := "#!/bin/sh\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done\n"
				if behavior == "failure" {
					script = "#!/bin/sh\nexit 1\n"
				}
				if err := os.WriteFile(filepath.Join(bin, "less"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var out, errOut bytes.Buffer
			err := pageOverview(content, &out, &errOut)
			if behavior == "failure" {
				if err == nil || !strings.Contains(err.Error(), "overview pager") {
					t.Fatalf("pager failure was hidden: %v", err)
				}
				return
			}
			if err != nil || out.String() != content {
				t.Fatalf("overview lost content: %v, %q", err, &out)
			}
		})
	}
}

func TestColorModes(t *testing.T) {
	for _, env := range []struct{ noColor, term string }{{"", "xterm-256color"}, {"1", "xterm"}, {"", "dumb"}} {
		t.Setenv("NO_COLOR", env.noColor)
		t.Setenv("TERM", env.term)
		for mode, want := range map[string]bool{"always": true, "never": false, "auto": false} {
			var out bytes.Buffer
			if got := styleFor(mode, &out).color; got != want {
				t.Errorf("styleFor(%q) with NO_COLOR=%q TERM=%q = %v, want %v", mode, env.noColor, env.term, got, want)
			}
		}
	}
	for _, args := range [][]string{{"--color", "sometimes"}, {"--color="}, {"--color"}, {"--color", "sometimes", "--help"}} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("commit workflow accepted invalid color: %v", args)
		}
		if _, err := parseTreeOptions(args); err == nil {
			t.Errorf("overview accepted invalid color: %v", args)
		}
	}
}

func TestCommandHelpColorsAndStreams(t *testing.T) {
	t.Setenv("COMMITELL_API_KEY", "")
	t.Setenv("COMMITELL_MODELS", ",")
	t.Setenv("RT_DEPTH", "invalid")
	t.Setenv("RT_MAX_FILES", "invalid")
	t.Setenv("CMT_ROOT", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("NO_COLOR", "1")
	for _, prefix := range [][]string{nil, {"-t"}, {"tree"}} {
		var plain string
		for _, mode := range []string{"auto", "never", "always"} {
			var out, errOut bytes.Buffer
			args := append(append([]string{}, prefix...), "--color", mode, "--help")
			if code := command(args, strings.NewReader(""), &out, &errOut); code != 0 || errOut.Len() != 0 {
				t.Fatalf("help %v: exit %d, stderr %q", args, code, &errOut)
			}
			if colored := strings.Contains(out.String(), "\x1b["); colored != (mode == "always") {
				t.Fatalf("help %v has color=%v", args, colored)
			}
			if mode == "auto" {
				plain = out.String()
			} else if got := ansiStyle.ReplaceAllString(out.String(), ""); got != plain {
				t.Fatalf("color mode changed help content for %v", args)
			}
			if !strings.Contains(out.String(), "--color WHEN") || !strings.Contains(out.String(), "Examples") {
				t.Fatalf("incomplete help for %v: %s", args, &out)
			}
			for _, line := range strings.Split(ansiStyle.ReplaceAllString(out.String(), ""), "\n") {
				if utf8.RuneCountInString(line) > 80 {
					t.Fatalf("help exceeds 80 columns: %q", line)
				}
			}
		}
	}
}

func TestCommandErrorsUseStderrAndRequestedColor(t *testing.T) {
	for _, mode := range []string{"auto", "never", "always"} {
		for _, prefix := range [][]string{nil, {"tree"}} {
			var out, errOut bytes.Buffer
			args := append(append([]string{}, prefix...), "--color", mode, "--unknown")
			code := command(args, strings.NewReader(""), &out, &errOut)
			if code == 0 || out.Len() != 0 || !strings.Contains(errOut.String(), "unknown") {
				t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, &out, &errOut)
			}
			if colored := strings.Contains(errOut.String(), "\x1b[31m"); colored != (mode == "always") {
				t.Fatalf("%v: unexpected diagnostic color: %q", args, &errOut)
			}
		}
	}
}

func TestVersionStaysPlainWithForcedColor(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := command([]string{"--color", "always", "--version"}, nil, &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("version: exit %d, stderr %q", code, &errOut)
	}
	if out.String() != "commitell "+currentVersion()+"\n" {
		t.Fatalf("unexpected version output: %q", &out)
	}
}

func TestRunColorsSuccessWarningsAndPreview(t *testing.T) {
	for _, mode := range []string{"auto", "never", "always"} {
		for _, preview := range []bool{false, true} {
			repo := newRepository(t)
			writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
			var out, errOut bytes.Buffer
			cfg := config{dir: repo, out: &out, errOut: &errOut, options: options{offline: true, force: true, dryRun: preview, colorMode: mode}}
			if err := run(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			color := "\x1b[32m"
			if preview {
				color = "\x1b[36m"
			}
			if strings.Contains(out.String(), color) != (mode == "always") || strings.Contains(errOut.String(), "\x1b[33m") != (mode == "always") {
				t.Fatalf("mode=%s preview=%v: stdout %q, stderr %q", mode, preview, &out, &errOut)
			}
			if !preview && strings.Contains(git(t, repo, "log", "-1", "--format=%B"), "\x1b") {
				t.Fatal("terminal styling leaked into a commit message")
			}
		}
	}
}

func TestTreeFlagsOverrideLegacyDefaults(t *testing.T) {
	t.Setenv("RT_DEPTH", "invalid")
	t.Setenv("RT_MAX_FILES", "invalid")
	opts, err := parseTreeOptions([]string{"--depth", "3", "--max-files", "0"})
	if err != nil || opts.depth != 3 || opts.limit != 0 {
		t.Fatalf("explicit tree options = %+v, %v", opts, err)
	}
	if _, err := parseTreeOptions(nil); err == nil {
		t.Fatal("ignored invalid environment defaults without explicit overrides")
	}
}

func TestDiagnosticsEscapeUntrustedControlCharacters(t *testing.T) {
	var out bytes.Buffer
	cfg := config{options: options{colorMode: "never"}}
	cfg.notice(&out, sgrYellow, "bad\x1b[31mmodel")
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), `\x1b`) {
		t.Fatalf("control character leaked: %q", &out)
	}
}
