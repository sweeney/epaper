package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// writeSummary emits GitHub-flavoured Markdown for $GITHUB_STEP_SUMMARY.
//
// The HTML report is the thing worth reading, but it takes a download and two
// clicks. This puts the numbers, and any failures, on the run page itself —
// which is where someone looks first when a build goes red.
func writeSummary(w io.Writer, run *Run, artifact string) error {
	var b strings.Builder

	status := "✅ **PASS**"
	if !run.OK() {
		status = "❌ **FAIL**"
	}
	fmt.Fprintf(&b, "## %s — %s\n\n", run.Title, status)

	fmt.Fprintf(&b, "| Passed | Failed | Skipped | Coverage | Duration |\n")
	fmt.Fprintf(&b, "|---:|---:|---:|---:|---:|\n")
	cov := "—"
	if run.HasCoverage() {
		cov = fmt.Sprintf("%.1f%%", run.Coverage())
	}
	fmt.Fprintf(&b, "| %d | %d | %d | %s | %s |\n\n",
		run.Passed, run.Failed, run.Skipped, cov, run.Elapsed.Round(1e6))

	if !run.OK() {
		b.WriteString("### Failures\n\n")
		for _, p := range run.Packages {
			for _, t := range p.Tests {
				if t.Status != "fail" {
					continue
				}
				fmt.Fprintf(&b, "<details><summary><code>%s</code> · %s</summary>\n\n```\n%s\n```\n\n</details>\n\n",
					shortPath(p.Name), t.Name, strings.TrimSpace(t.Output))
			}
			if len(p.Tests) == 0 && p.Failed() && strings.TrimSpace(p.Output) != "" {
				fmt.Fprintf(&b, "<details><summary><code>%s</code></summary>\n\n```\n%s\n```\n\n</details>\n\n",
					shortPath(p.Name), strings.TrimSpace(p.Output))
			}
		}
	}

	b.WriteString("### Coverage by package\n\n")
	b.WriteString("| Package | Coverage | Tests |\n|---|---:|---:|\n")
	pkgs := append([]*Package(nil), run.Packages...)
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	for _, p := range pkgs {
		if p.Untested() {
			fmt.Fprintf(&b, "| `%s` | — | no tests |\n", shortPath(p.Name))
			continue
		}
		pass, fail, skip := p.Counts()
		c := "—"
		if p.HasCover {
			c = fmt.Sprintf("%.1f%%", p.Coverage)
		}
		counts := fmt.Sprintf("%d", pass)
		if fail > 0 {
			counts += fmt.Sprintf(" (%d failed)", fail)
		}
		if skip > 0 {
			counts += fmt.Sprintf(" (%d skipped)", skip)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", shortPath(p.Name), c, counts)
	}

	if artifact != "" {
		fmt.Fprintf(&b, "\nFull report, including the rendered goldens: **%s** artifact.\n", artifact)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// shortPath drops the module prefix, which is identical on every row.
func shortPath(s string) string {
	const prefix = "github.com/sweeney/epaper"
	switch {
	case s == prefix:
		return "epaper"
	case strings.HasPrefix(s, prefix+"/"):
		return strings.TrimPrefix(s, prefix+"/")
	}
	return s
}
