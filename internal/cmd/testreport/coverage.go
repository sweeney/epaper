package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

// parseCoverage summarises a Go coverage profile by package.
//
// A profile line is "import/path/file.go:l.c,l.c numStmt count", so the
// percentage for a package is the covered statements over the total. Reading
// the profile directly avoids shelling out to `go tool cover` and gives the
// per-package split, which the tool's own summary does not.
func parseCoverage(path string) (*Coverage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type counts struct{ covered, total int }
	byPkg := map[string]*counts{}

	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				continue
			}
		}

		pkg, stmts, count, ok := parseCoverLine(line)
		if !ok {
			continue
		}
		c, exists := byPkg[pkg]
		if !exists {
			c = &counts{}
			byPkg[pkg] = c
		}
		c.total += stmts
		if count > 0 {
			c.covered += stmts
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(byPkg) == 0 {
		return nil, fmt.Errorf("no usable lines in the profile")
	}

	out := &Coverage{ByPackage: make(map[string]float64, len(byPkg))}
	for pkg, c := range byPkg {
		if c.total == 0 {
			continue
		}
		out.ByPackage[pkg] = 100 * float64(c.covered) / float64(c.total)
		out.Covered += c.covered
		out.Total += c.total
	}
	return out, nil
}

// Coverage is a profile summarised per package, plus the raw statement counts
// the overall figure needs.
type Coverage struct {
	ByPackage map[string]float64
	Covered   int
	Total     int
}

// Overall is the statement-weighted percentage — the same number
// `go tool cover -func` calls "total".
//
// Averaging the per-package percentages instead would be badly misleading
// here: this module has several packages that legitimately contain no tests
// at all, such as the hardware transports and the example commands, and each
// of those would drag a plain mean down by the same amount as a large,
// thoroughly tested one.
func (c *Coverage) Overall() float64 {
	if c == nil || c.Total == 0 {
		return 0
	}
	return 100 * float64(c.Covered) / float64(c.Total)
}

// parseCoverLine splits one profile line into its package, statement count and
// execution count.
func parseCoverLine(line string) (pkg string, stmts, count int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", 0, 0, false
	}
	colon := strings.LastIndex(fields[0], ":")
	if colon < 0 {
		return "", 0, 0, false
	}
	file := fields[0][:colon]

	var err error
	if stmts, err = strconv.Atoi(fields[1]); err != nil {
		return "", 0, 0, false
	}
	if count, err = strconv.Atoi(fields[2]); err != nil {
		return "", 0, 0, false
	}
	return pathDir(file), stmts, count, true
}

// pathDir is the import path of the package a file belongs to.
func pathDir(file string) string {
	dir := path.Dir(file)
	if dir == "." {
		return file
	}
	return dir
}

// applyCoverage fills in per-package coverage, preferring profile numbers over
// whatever go test printed, because the profile covers packages that have no
// test files of their own.
func (r *Run) applyCoverage(cov *Coverage) {
	r.Cover = cov
	have := map[string]bool{}
	for _, p := range r.Packages {
		if c, ok := cov.ByPackage[p.Name]; ok {
			p.Coverage, p.HasCover = c, true
			have[p.Name] = true
		}
	}
	// A package with code but no tests still deserves a row: 0% is a fact,
	// and silently omitting it is how untested packages stay untested.
	for name, c := range cov.ByPackage {
		if have[name] {
			continue
		}
		r.Packages = append(r.Packages, &Package{
			Name: name, Status: "notests", Coverage: c, HasCover: true,
		})
	}
}
