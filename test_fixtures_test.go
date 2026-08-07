package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/strings.jsonl
var testStringsJSONL string

type stringFixture struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func testString(t testing.TB, name string) string {
	t.Helper()
	fixtures := testStrings(t)
	value, ok := fixtures[name]
	if !ok {
		t.Fatalf("test string fixture %q not found", name)
	}
	return value
}

func testStrings(t testing.TB) map[string]string {
	t.Helper()
	fixtures := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(testStringsJSONL))
	for scanner.Scan() {
		var fixture stringFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("decode testdata/strings.jsonl: %v", err)
		}
		if fixture.Name == "" {
			t.Fatal("testdata/strings.jsonl contains an empty fixture name")
		}
		if _, exists := fixtures[fixture.Name]; exists {
			t.Fatalf("testdata/strings.jsonl contains duplicate fixture %q", fixture.Name)
		}
		fixtures[fixture.Name] = fixture.Value
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read testdata/strings.jsonl: %v", err)
	}
	return fixtures
}

func TestStringFixturesAreValid(t *testing.T) {
	if fixtures := testStrings(t); len(fixtures) == 0 {
		t.Fatal("testdata/strings.jsonl is empty")
	}
}
