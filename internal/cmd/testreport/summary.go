package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// SummaryOptions describes where the summary is being written and what it can
// link to.
type SummaryOptions struct {
	// Repo and SHA build raw.githubusercontent URLs for the golden images.
	//
	// GitHub's Markdown strips data: URIs, so the images cannot be inlined
	// the way the HTML report does it. They can be LINKED, though, because
	// the goldens are committed — at this exact SHA, which is what makes the
	// summary show the renders this run actually used rather than whatever
	// is on main now.
	Repo string
	SHA  string

	// Artifact is the name of the uploaded HTML report, mentioned so a
	// reader knows where the full thing is.
	Artifact string
}

// writeSummary emits GitHub-flavoured Markdown for $GITHUB_STEP_SUMMARY.
//
// This is the page someone actually looks at: it is on the run itself, with
// no download. The HTML artifact is the deep dive — it has the expected and
// actual renders side by side, which Markdown cannot do.
func writeSummary(w io.Writer, run *Run, opts SummaryOptions) error {
	var b strings.Builder

	status := "✅&nbsp;**PASS**"
	if !run.OK() {
		status = "❌&nbsp;**FAIL**"
	}
	fmt.Fprintf(&b, "## %s %s\n\n", run.Title, status)

	cov := "—"
	if run.HasTotal() {
		cov = fmt.Sprintf("%.1f%%", run.Coverage())
	}
	b.WriteString("| ✅ Passed | ❌ Failed | ⏭️ Skipped | 📊 Coverage | ⏱️ Duration |\n")
	b.WriteString("|---:|---:|---:|---:|---:|\n")
	fmt.Fprintf(&b, "| **%d** | %s | %d | %s | %s |\n\n",
		run.Passed, failCell(run.Failed), run.Skipped, cov, run.Elapsed.Round(time.Millisecond))

	writeFailures(&b, run)
	writeRenders(&b, run, opts)
	writeCoverage(&b, run)
	writeSlowest(&b, run)

	if opts.Artifact != "" {
		fmt.Fprintf(&b, "\n---\n\n📄 **%s** — the same results as one self-contained page, "+
			"with every render inlined and any that changed shown expected-beside-actual. "+
			"It is uploaded unzipped, so it opens straight from the artifact list.\n", opts.Artifact)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// failCell makes a non-zero failure count impossible to skim past.
func failCell(n int) string {
	if n == 0 {
		return "0"
	}
	return fmt.Sprintf("**%d**", n)
}

// writeFailures comes first, because it is why anyone opened the page.
func writeFailures(b *strings.Builder, run *Run) {
	if run.OK() {
		return
	}
	b.WriteString("### Failures\n\n")
	for _, p := range run.Packages {
		for _, t := range p.Tests {
			if t.Status != "fail" {
				continue
			}
			fmt.Fprintf(b, "<details open><summary><code>%s</code> · <b>%s</b></summary>\n\n```\n%s\n```\n\n</details>\n\n",
				shortPath(p.Name), t.Name, strings.TrimSpace(t.Output))
		}
		if len(p.Tests) == 0 && p.Failed() && strings.TrimSpace(p.Output) != "" {
			fmt.Fprintf(b, "<details open><summary><code>%s</code></summary>\n\n```\n%s\n```\n\n</details>\n\n",
				shortPath(p.Name), strings.TrimSpace(p.Output))
		}
	}
}

// writeRenders shows what the tests actually drew.
//
// For this library that is most of what they assert, and a passing count says
// nothing about whether the test card still looks right. Two per row, at half
// size, so the section stays scannable.
func writeRenders(b *strings.Builder, run *Run, opts SummaryOptions) {
	if len(run.Images) == 0 || opts.Repo == "" || opts.SHA == "" {
		return
	}

	b.WriteString("### Renders\n\n")

	var changed []string
	for _, img := range run.Images {
		if img.Changed {
			changed = append(changed, img.Name)
		}
	}
	if len(changed) > 0 {
		fmt.Fprintf(b, "> ⚠️ **%d render(s) changed: %s.** The images below are the "+
			"committed goldens — what was EXPECTED. See the artifact for what was actually drawn.\n\n",
			len(changed), "`"+strings.Join(changed, "`, `")+"`")
	}

	b.WriteString("<table>\n")
	for i, img := range run.Images {
		if i%2 == 0 {
			b.WriteString("<tr>\n")
		}
		label := img.Name
		if img.Changed {
			label = "⚠️ " + img.Name + " (changed)"
		}
		fmt.Fprintf(b, "<td align=\"center\" width=\"50%%\">\n\n"+
			"<img src=\"https://raw.githubusercontent.com/%s/%s/testdata/golden/%s.png\" width=\"%d\" alt=\"%s\"><br>\n"+
			"<sub><code>%s</code> · %d×%d</sub>\n\n</td>\n",
			opts.Repo, opts.SHA, img.Name, displayWidth(img.Width), img.Name, label, img.Width, img.Height)
		if i%2 == 1 || i == len(run.Images)-1 {
			b.WriteString("</tr>\n")
		}
	}
	b.WriteString("</table>\n\n")
}

// displayWidth keeps a render readable without letting it dominate the page.
// e-ink art is small and pixel-exact, so it is shown at its natural size up to
// a cap rather than scaled up and blurred by the browser.
func displayWidth(w int) int {
	const max = 400
	if w > max {
		return max
	}
	return w
}

func writeCoverage(b *strings.Builder, run *Run) {
	if !run.HasCoverage() {
		return
	}
	b.WriteString("### Coverage\n\n```\n")

	pkgs := append([]*Package(nil), run.Packages...)
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })

	width := 0
	for _, p := range pkgs {
		width = max(width, len(shortPath(p.Name)))
	}
	for _, p := range pkgs {
		name := shortPath(p.Name)
		if p.Untested() {
			fmt.Fprintf(b, "%-*s  %s  no tests\n", width, name, strings.Repeat("·", 12))
			continue
		}
		fmt.Fprintf(b, "%-*s  %s  %5.1f%%\n", width, name, bar(p.Coverage, 12), p.Coverage)
	}
	if run.HasTotal() {
		fmt.Fprintf(b, "%-*s  %s  %5.1f%%\n", width, "TOTAL", bar(run.Coverage(), 12), run.Coverage())
	}
	b.WriteString("```\n\n")
}

// bar draws a coverage bar out of block characters, which survive being pasted
// anywhere and need no images.
func bar(pct float64, width int) string {
	filled := int(pct/100*float64(width) + 0.5)
	filled = min(max(filled, 0), width)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// writeSlowest is the one piece of information that tells you where the test
// suite's time is actually going.
func writeSlowest(b *strings.Builder, run *Run) {
	type entry struct {
		pkg, name string
		d         time.Duration
	}
	var all []entry
	for _, p := range run.Packages {
		for _, t := range p.Tests {
			all = append(all, entry{shortPath(p.Name), t.Name, t.Elapsed})
		}
	}
	if len(all) == 0 {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].d > all[j].d })
	if all[0].d == 0 {
		return // nothing measurable
	}
	if len(all) > 10 {
		all = all[:10]
	}

	b.WriteString("<details><summary>🐢 Slowest tests</summary>\n\n")
	b.WriteString("| Test | Package | Time |\n|---|---|---:|\n")
	for _, e := range all {
		fmt.Fprintf(b, "| `%s` | `%s` | %s |\n", e.name, e.pkg, e.d.Round(time.Millisecond))
	}
	b.WriteString("\n</details>\n\n")
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
