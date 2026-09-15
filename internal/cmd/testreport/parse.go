package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// event is one line of `go test -json`.
type event struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// Test is one test's result.
type Test struct {
	Name    string
	Package string
	Status  string // pass, fail, skip
	Elapsed time.Duration
	Output  string
}

// Package groups the tests in one Go package.
type Package struct {
	Name     string
	Status   string
	Elapsed  time.Duration
	Coverage float64
	HasCover bool
	Tests    []*Test
	Output   string // package-level output, e.g. a build failure
}

// Failed reports whether anything in the package failed.
func (p *Package) Failed() bool { return p.Status == "fail" }

// Untested reports whether the package has neither tests nor covered
// statements. Saying so beats showing it a red 0% bar, which reads as a
// problem rather than as a main package or a hardware-only one.
func (p *Package) Untested() bool { return len(p.Tests) == 0 && p.Coverage == 0 }

// Counts returns passed, failed and skipped for the package.
func (p *Package) Counts() (pass, fail, skip int) {
	for _, t := range p.Tests {
		switch t.Status {
		case "pass":
			pass++
		case "fail":
			fail++
		case "skip":
			skip++
		}
	}
	return
}

// Run is everything the page shows.
type Run struct {
	Title     string
	Commit    string
	Branch    string
	Generated time.Time

	Packages []*Package
	Images   []Image
	Cover    *Coverage

	Passed  int
	Failed  int
	Skipped int
	Elapsed time.Duration
}

// OK reports whether the whole run passed.
func (r *Run) OK() bool { return r.Failed == 0 }

// HasCoverage reports whether anything measured coverage, whether from a
// profile or from go test's own summary line.
func (r *Run) HasCoverage() bool {
	if r.HasTotal() {
		return true
	}
	for _, p := range r.Packages {
		if p.HasCover {
			return true
		}
	}
	return false
}

// HasTotal reports whether a coverage PROFILE was supplied, which is the only
// thing that makes an overall figure computable.
//
// go test prints a percentage per package but not the statement counts behind
// it, so without the profile there is no honest way to combine them — see
// [Coverage.Overall] for why averaging the percentages is not it.
func (r *Run) HasTotal() bool { return r.Cover != nil && r.Cover.Total > 0 }

// Coverage is the statement-weighted percentage across the whole run, or 0 if
// no profile was supplied. Check [Run.HasTotal] first.
func (r *Run) Coverage() float64 { return r.Cover.Overall() }

// parse reads `go test -json` events.
//
// It tolerates non-JSON lines, because a build failure or a panic writes plain
// text to the same stream and losing that is exactly when you need it most.
func parse(r io.Reader) (*Run, error) {
	run := &Run{}
	pkgs := map[string]*Package{}
	tests := map[string]*Test{}

	key := func(pkg, test string) string { return pkg + "\x00" + test }

	pkg := func(name string) *Package {
		p, ok := pkgs[name]
		if !ok {
			p = &Package{Name: name, Status: "pass"}
			pkgs[name] = p
			run.Packages = append(run.Packages, p)
		}
		return p
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)

	var stray strings.Builder
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if line[0] != '{' {
			stray.Write(line)
			stray.WriteByte('\n')
			continue
		}

		var e event
		if err := json.Unmarshal(line, &e); err != nil {
			stray.Write(line)
			stray.WriteByte('\n')
			continue
		}
		if e.Package == "" {
			continue
		}
		p := pkg(e.Package)

		if e.Test == "" {
			switch e.Action {
			case "output":
				p.Output += e.Output
				if c, ok := coverageFromOutput(e.Output); ok {
					p.Coverage, p.HasCover = c, true
				}
			case "fail":
				p.Status = "fail"
				p.Elapsed = time.Duration(e.Elapsed * float64(time.Second))
			case "pass":
				p.Elapsed = time.Duration(e.Elapsed * float64(time.Second))
			case "skip":
				if p.Status != "fail" {
					p.Status = "skip"
				}
			}
			continue
		}

		k := key(e.Package, e.Test)
		t, ok := tests[k]
		if !ok {
			t = &Test{Name: e.Test, Package: e.Package}
			tests[k] = t
			p.Tests = append(p.Tests, t)
		}

		switch e.Action {
		case "output":
			t.Output += e.Output
		case "pass", "fail", "skip":
			t.Status = e.Action
			t.Elapsed = time.Duration(e.Elapsed * float64(time.Second))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading test output: %w", err)
	}

	if s := strings.TrimSpace(stray.String()); s != "" {
		// Keep it: a build error lands here and it is the whole story.
		run.Packages = append(run.Packages, &Package{
			Name: "(output not attributed to a package)", Status: "fail", Output: s,
		})
	}

	for _, p := range run.Packages {
		sort.Slice(p.Tests, func(i, j int) bool {
			if p.Tests[i].Status != p.Tests[j].Status {
				return statusRank(p.Tests[i].Status) < statusRank(p.Tests[j].Status)
			}
			return p.Tests[i].Name < p.Tests[j].Name
		})
		pass, fail, skip := p.Counts()
		run.Passed += pass
		run.Failed += fail
		run.Skipped += skip
		run.Elapsed += p.Elapsed
		if fail > 0 {
			p.Status = "fail"
		}
	}
	return run, nil
}

// statusRank sorts failures to the top.
func statusRank(s string) int {
	switch s {
	case "fail":
		return 0
	case "skip":
		return 1
	default:
		return 2
	}
}

// coverageFromOutput pulls the percentage out of go test's summary line,
// e.g. "ok  \tpkg\t0.3s\tcoverage: 92.4% of statements".
func coverageFromOutput(s string) (float64, bool) {
	const marker = "coverage: "
	i := strings.Index(s, marker)
	if i < 0 {
		return 0, false
	}
	rest := s[i+len(marker):]
	j := strings.Index(rest, "%")
	if j < 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(rest[:j]), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
