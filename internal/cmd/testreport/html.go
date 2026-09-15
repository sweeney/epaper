package main

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
)

// render writes the report.
func render(w io.Writer, run *Run) error {
	return page.Execute(w, run)
}

var funcs = template.FuncMap{
	"dur": func(d time.Duration) string {
		switch {
		case d == 0:
			return "—"
		case d < time.Millisecond:
			return "<1ms"
		case d < time.Second:
			return fmt.Sprintf("%dms", d.Milliseconds())
		}
		return fmt.Sprintf("%.2fs", d.Seconds())
	},
	"pct":   func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
	"round": func(v float64) int { return int(v + 0.5) },
	"kb":    func(n int) string { return fmt.Sprintf("%.0f KB", float64(n)/1024) },
	"counts": func(p *Package) string {
		pass, fail, skip := p.Counts()
		parts := []string{fmt.Sprintf("%d passed", pass)}
		if fail > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", fail))
		}
		if skip > 0 {
			parts = append(parts, fmt.Sprintf("%d skipped", skip))
		}
		return strings.Join(parts, ", ")
	},
	// short drops the module prefix, which is the same on every row and
	// pushes the part you actually read off the edge.
	"short": shortPath,
	"coverClass": func(v float64) string {
		switch {
		case v >= 90:
			return "good"
		case v >= 70:
			return "ok"
		}
		return "poor"
	},
	"trim": strings.TrimSpace,
}

var page = template.Must(template.New("report").Funcs(funcs).Parse(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} — test report</title>
<style>
  :root {
    --bg: #fbfaf8; --fg: #1a1a1a; --muted: #6b6a67; --line: #e2dfd9;
    --card: #ffffff; --pass: #2f7d4f; --fail: #b3352b; --skip: #8a7a3e;
    --accent: #be2d28; --ink-yellow: #ebcd28;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #17181a; --fg: #e9e7e3; --muted: #9a9894; --line: #2f3135;
      --card: #1f2124; --pass: #63b483; --fail: #e0685c; --skip: #c4ad63;
    }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 0 16px 64px; background: var(--bg); color: var(--fg);
    font: 15px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  }
  .wrap { max-width: 1000px; margin: 0 auto; }
  header { padding: 32px 0 20px; border-bottom: 1px solid var(--line); }
  h1 { margin: 0 0 6px; font-size: 26px; letter-spacing: -0.01em; }
  h2 { font-size: 18px; margin: 36px 0 12px; }
  .sub { color: var(--muted); font-size: 13px; }
  .sub code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }

  .badge {
    display: inline-block; padding: 3px 12px; border-radius: 999px;
    font-size: 13px; font-weight: 650; letter-spacing: .04em; color: #fff;
  }
  .badge.pass { background: var(--pass); }
  .badge.fail { background: var(--fail); }

  .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 12px; margin: 20px 0 8px; }
  .card { background: var(--card); border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; }
  .card .n { font-size: 24px; font-weight: 650; font-variant-numeric: tabular-nums; }
  .card .l { font-size: 12px; color: var(--muted); text-transform: uppercase; letter-spacing: .06em; }
  .n.pass { color: var(--pass); } .n.fail { color: var(--fail); } .n.skip { color: var(--skip); }

  table { width: 100%; border-collapse: collapse; font-size: 14px; }
  th { text-align: left; font-size: 12px; text-transform: uppercase; letter-spacing: .06em;
       color: var(--muted); font-weight: 600; padding: 8px 10px; border-bottom: 1px solid var(--line); }
  td { padding: 9px 10px; border-bottom: 1px solid var(--line); vertical-align: middle; }
  td.num { text-align: right; font-variant-numeric: tabular-nums; color: var(--muted); }
  .pkg { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }

  .bar { display: inline-block; width: 90px; height: 7px; border-radius: 4px;
         background: var(--line); overflow: hidden; vertical-align: middle; margin-right: 8px; }
  .bar > i { display: block; height: 100%; }
  .bar > i.good { background: var(--pass); }
  .bar > i.ok   { background: var(--skip); }
  .bar > i.poor { background: var(--fail); }

  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 8px; }
  .dot.pass { background: var(--pass); } .dot.fail { background: var(--fail); }
  .dot.skip { background: var(--skip); } .dot.notests { background: var(--line); }

  details { border: 1px solid var(--line); border-radius: 10px; background: var(--card); margin-bottom: 10px; }
  details[open] { padding-bottom: 6px; }
  summary { cursor: pointer; padding: 11px 14px; list-style: none; display: flex;
            align-items: center; gap: 10px; flex-wrap: wrap; }
  summary::-webkit-details-marker { display: none; }
  summary::before { content: "▸"; color: var(--muted); font-size: 12px; }
  details[open] > summary::before { content: "▾"; }
  summary .spacer { flex: 1; }
  .tests { padding: 0 14px; }
  .t { display: flex; align-items: baseline; gap: 10px; padding: 5px 0; border-top: 1px solid var(--line); font-size: 13.5px; }
  .t .name { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .t .spacer { flex: 1; }
  pre { background: var(--bg); border: 1px solid var(--line); border-radius: 8px;
        padding: 10px 12px; overflow-x: auto; font-size: 12.5px; line-height: 1.45;
        font-family: ui-monospace, SFMono-Regular, Menlo, monospace; white-space: pre-wrap; }

  .renders { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 16px; align-items: start; }
  .render { background: var(--card); border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
  .render .frame {
    /* A checkerboard, so white areas of a 1-bit render are still visible. */
    background-color: #fff;
    background-image:
      linear-gradient(45deg, #eceae6 25%, transparent 25%, transparent 75%, #eceae6 75%),
      linear-gradient(45deg, #eceae6 25%, transparent 25%, transparent 75%, #eceae6 75%);
    background-size: 16px 16px; background-position: 0 0, 8px 8px;
    padding: 12px; display: flex; justify-content: center;
  }
  .render img { max-width: 100%; height: auto; image-rendering: pixelated;
                border: 1px solid rgba(0,0,0,.15); }
  .render .meta { padding: 9px 12px; font-size: 12.5px; color: var(--muted);
                  display: flex; gap: 10px; align-items: baseline; border-top: 1px solid var(--line); }
  .render .meta b { color: var(--fg); font-weight: 600;
                    font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .render.changed { border-color: var(--fail); box-shadow: 0 0 0 1px var(--fail); }
  .pair { display: grid; grid-template-columns: 1fr 1fr; gap: 1px; background: var(--line); }
  .pair figure { margin: 0; background: var(--card); }
  .pair figcaption { font-size: 11px; text-transform: uppercase; letter-spacing: .06em;
                     color: var(--muted); padding: 7px 10px 0; }
  .tag { margin-left: auto; font-size: 11px; font-weight: 700; letter-spacing: .06em;
         color: #fff; background: var(--fail); border-radius: 4px; padding: 2px 7px; }
  .empty { color: var(--muted); font-style: italic; }
  footer { margin-top: 48px; padding-top: 16px; border-top: 1px solid var(--line);
           color: var(--muted); font-size: 12.5px; }
</style>
<div class="wrap">

<header>
  <h1>{{.Title}} <span class="badge {{if .OK}}pass{{else}}fail{{end}}">{{if .OK}}PASS{{else}}FAIL{{end}}</span></h1>
  <div class="sub">
    {{with .Branch}}<code>{{.}}</code> · {{end}}
    {{with .Commit}}<code>{{.}}</code> · {{end}}
    {{.Generated.Format "2 Jan 2006, 15:04:05 MST"}}
  </div>
</header>

<div class="cards">
  <div class="card"><div class="n pass">{{.Passed}}</div><div class="l">Passed</div></div>
  <div class="card"><div class="n {{if .Failed}}fail{{end}}">{{.Failed}}</div><div class="l">Failed</div></div>
  <div class="card"><div class="n skip">{{.Skipped}}</div><div class="l">Skipped</div></div>
  {{if .HasCoverage}}
  <div class="card"><div class="n">{{pct .Coverage}}</div><div class="l">Coverage</div></div>
  {{end}}
  <div class="card"><div class="n">{{dur .Elapsed}}</div><div class="l">Duration</div></div>
</div>

{{if .Images}}
<h2>Renders</h2>
<p class="sub">Golden images from this run. Most of what these tests assert is what got
drawn, and a passing count says nothing about whether it still looks right.</p>
<div class="renders">
  {{range .Images}}
  <div class="render{{if .Changed}} changed{{end}}">
    {{if .Changed}}
    <div class="pair">
      <figure><figcaption>expected</figcaption><div class="frame"><img src="{{.Data}}" alt="{{.Name}} expected"></div></figure>
      <figure><figcaption>actual</figcaption><div class="frame"><img src="{{.Actual}}" alt="{{.Name}} actual"></div></figure>
    </div>
    {{else}}
    <div class="frame"><img src="{{.Data}}" alt="{{.Name}}" width="{{.Width}}" height="{{.Height}}"></div>
    {{end}}
    <div class="meta">
      <b>{{.Name}}</b><span>{{.Width}}×{{.Height}}</span><span>{{kb .Bytes}}</span>
      {{if .Changed}}<span class="tag">CHANGED</span>{{end}}
    </div>
  </div>
  {{end}}
</div>
{{end}}

<h2>Packages</h2>
{{range .Packages}}
<details {{if .Failed}}open{{end}}>
  <summary>
    <span class="dot {{.Status}}"></span>
    <span class="pkg">{{short .Name}}</span>
    <span class="spacer"></span>
    {{if .Untested}}<span class="sub">no tests</span>
    {{else if .HasCover}}<span class="bar"><i class="{{coverClass .Coverage}}" style="width:{{round .Coverage}}%"></i></span><span class="sub">{{pct .Coverage}}</span>{{end}}
    {{if not .Untested}}<span class="sub">{{counts .}}</span>{{end}}
    <span class="sub">{{dur .Elapsed}}</span>
  </summary>
  <div class="tests">
    {{if and (not .Tests) (not (trim .Output))}}<p class="empty">No tests.</p>{{end}}
    {{range .Tests}}
    <div class="t">
      <span class="dot {{.Status}}"></span>
      <span class="name">{{.Name}}</span>
      <span class="spacer"></span>
      <span class="sub">{{dur .Elapsed}}</span>
    </div>
    {{if eq .Status "fail"}}<pre>{{trim .Output}}</pre>{{end}}
    {{end}}
    {{if .Failed}}{{with trim .Output}}<pre>{{.}}</pre>{{end}}{{end}}
  </div>
</details>
{{end}}

<footer>
  Generated by <code>internal/cmd/testreport</code>. Self-contained: no network, no scripts.
</footer>

</div>
</html>
`))
