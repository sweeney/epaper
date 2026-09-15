package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleJSON = `
{"Action":"run","Package":"example.com/a","Test":"TestOne"}
{"Action":"output","Package":"example.com/a","Test":"TestOne","Output":"=== RUN   TestOne\n"}
{"Action":"pass","Package":"example.com/a","Test":"TestOne","Elapsed":0.25}
{"Action":"run","Package":"example.com/a","Test":"TestTwo"}
{"Action":"output","Package":"example.com/a","Test":"TestTwo","Output":"    a_test.go:9: boom\n"}
{"Action":"fail","Package":"example.com/a","Test":"TestTwo","Elapsed":0.1}
{"Action":"run","Package":"example.com/a","Test":"TestThree"}
{"Action":"skip","Package":"example.com/a","Test":"TestThree"}
{"Action":"output","Package":"example.com/a","Output":"FAIL\texample.com/a\t0.4s\n"}
{"Action":"fail","Package":"example.com/a","Elapsed":0.4}
{"Action":"output","Package":"example.com/b","Output":"ok  \texample.com/b\t0.2s\tcoverage: 87.5% of statements\n"}
{"Action":"pass","Package":"example.com/b","Elapsed":0.2}
`

func TestParse(t *testing.T) {
	run, err := parse(strings.NewReader(sampleJSON))
	if err != nil {
		t.Fatalf("parse(): %v", err)
	}

	if run.Passed != 1 || run.Failed != 1 || run.Skipped != 1 {
		t.Errorf("counts = %d/%d/%d passed/failed/skipped, want 1/1/1",
			run.Passed, run.Failed, run.Skipped)
	}
	if run.OK() {
		t.Error("OK() = true for a run with a failure")
	}
	if len(run.Packages) != 2 {
		t.Fatalf("%d packages, want 2", len(run.Packages))
	}

	byName := map[string]*Package{}
	for _, p := range run.Packages {
		byName[p.Name] = p
	}

	a := byName["example.com/a"]
	if a == nil || !a.Failed() {
		t.Fatalf("package a = %+v, want a failure", a)
	}
	// Failures sort to the top, because that is what a reader came for.
	if a.Tests[0].Name != "TestTwo" {
		t.Errorf("first test is %q, want the failure TestTwo", a.Tests[0].Name)
	}
	if !strings.Contains(a.Tests[0].Output, "boom") {
		t.Errorf("the failing test lost its output: %q", a.Tests[0].Output)
	}

	b := byName["example.com/b"]
	if b == nil {
		t.Fatal("package b is missing")
	}
	if !b.HasCover || b.Coverage != 87.5 {
		t.Errorf("package b coverage = %v (has=%v), want 87.5", b.Coverage, b.HasCover)
	}
	// It has no tests of its own but it IS covered, by tests elsewhere, so
	// it must not be written off as untested.
	if b.Untested() {
		t.Error("package b has 87.5% coverage; Untested() must be false")
	}
	bare := &Package{Name: "example.com/c"}
	if !bare.Untested() {
		t.Error("a package with neither tests nor coverage should be Untested()")
	}
}

// A build failure or a panic writes plain text to the same stream, and losing
// it is exactly when you most need it.
func TestParseKeepsNonJSONOutput(t *testing.T) {
	in := "# example.com/broken\n./x.go:4:2: undefined: nope\n" + sampleJSON
	run, err := parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parse(): %v", err)
	}

	var found *Package
	for _, p := range run.Packages {
		if strings.Contains(p.Output, "undefined: nope") {
			found = p
		}
	}
	if found == nil {
		t.Fatal("the build error was dropped")
	}
	if !found.Failed() {
		t.Error("the package holding a build error should be marked failed")
	}
}

func TestParseEmpty(t *testing.T) {
	run, err := parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("parse(): %v", err)
	}
	if !run.OK() {
		t.Error("an empty run should not be a failure")
	}
	if len(run.Packages) != 0 {
		t.Errorf("%d packages, want 0", len(run.Packages))
	}
}

func TestCoverageFromOutput(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"ok  \tpkg\t0.3s\tcoverage: 92.4% of statements\n", 92.4, true},
		{"coverage: 0.0% of statements\n", 0, true},
		{"ok  \tpkg\t0.3s\n", 0, false},
		{"coverage: not-a-number%\n", 0, false},
		{"coverage: 50 of statements\n", 0, false},
	} {
		got, ok := coverageFromOutput(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("coverageFromOutput(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

const sampleProfile = `mode: set
example.com/a/one.go:10.20,12.3 2 1
example.com/a/one.go:14.20,16.3 2 0
example.com/b/two.go:5.10,7.2 6 1
`

func TestParseCoverage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cover.out")
	if err := os.WriteFile(path, []byte(sampleProfile), 0o644); err != nil {
		t.Fatal(err)
	}

	cov, err := parseCoverage(path)
	if err != nil {
		t.Fatalf("parseCoverage(): %v", err)
	}
	if got := cov.ByPackage["example.com/a"]; got != 50 {
		t.Errorf("package a = %v%%, want 50", got)
	}
	if got := cov.ByPackage["example.com/b"]; got != 100 {
		t.Errorf("package b = %v%%, want 100", got)
	}

	// Statement-weighted, NOT the mean of 50 and 100. Package b has three
	// times the statements, so it counts three times as much.
	if got, want := cov.Overall(), 100*8.0/10.0; got != want {
		t.Errorf("Overall() = %v, want %v — is it averaging percentages?", got, want)
	}
}

func TestParseCoverageErrors(t *testing.T) {
	if _, err := parseCoverage(filepath.Join(t.TempDir(), "nope.out")); err == nil {
		t.Error("parseCoverage() on a missing file = nil error")
	}

	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.out")
	if err := os.WriteFile(empty, []byte("mode: set\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseCoverage(empty); err == nil {
		t.Error("parseCoverage() on a profile with no data = nil error")
	}
}

func TestOverallOnNil(t *testing.T) {
	var c *Coverage
	if got := c.Overall(); got != 0 {
		t.Errorf("(*Coverage)(nil).Overall() = %v, want 0", got)
	}
}

// applyCoverage must add a row for a package that has code but no tests. A
// package silently missing from the report is how untested packages stay
// untested.
func TestApplyCoverageAddsUntestedPackages(t *testing.T) {
	run := &Run{Packages: []*Package{{Name: "example.com/a", Status: "pass"}}}
	run.applyCoverage(&Coverage{
		ByPackage: map[string]float64{"example.com/a": 90, "example.com/lonely": 12},
		Covered:   1, Total: 2,
	})

	var found bool
	for _, p := range run.Packages {
		if p.Name == "example.com/lonely" {
			found = true
			if p.Coverage != 12 {
				t.Errorf("lonely coverage = %v, want 12", p.Coverage)
			}
		}
	}
	if !found {
		t.Error("a package present only in the profile was dropped from the report")
	}
	if !run.HasCoverage() {
		t.Error("HasCoverage() = false after applying a profile")
	}
}
