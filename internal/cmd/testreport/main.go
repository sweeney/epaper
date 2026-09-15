// Command testreport turns `go test -json` output into a single HTML page.
//
//	go test -json ./... | go run ./internal/cmd/testreport -o report.html
//
// The page is self-contained: no CDN, no JavaScript dependencies, every image
// inlined. Drop it in a CI artifact and it opens anywhere.
//
// It embeds the golden PNGs alongside the results, which for this library is
// the point. Most of what these tests assert is what got drawn, and "42 tests
// passed" tells you nothing about whether the test card still looks right.
// Being able to see it in the same page as the results closes that gap.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"time"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("testreport: ")

	var (
		jsonPath = flag.String("json", "", "go test -json output (default: stdin)")
		coverPat = flag.String("cover", "", "coverage profile to summarise")
		goldens  = flag.String("goldens", "", "directory of PNGs to embed")
		outPath  = flag.String("o", "report.html", "file to write")
		title    = flag.String("title", "epaper", "page title")
		commit   = flag.String("commit", "", "full commit SHA: shown abbreviated, and used to link the golden images")
		branch   = flag.String("branch", "", "branch name to show")
		summary  = flag.String("summary", "", "also write a Markdown summary here (e.g. $GITHUB_STEP_SUMMARY)")
		artifact = flag.String("artifact", "", "artifact name to mention in the summary")
		repo     = flag.String("repo", "", "owner/name, so the summary can link the golden images")
	)
	flag.Parse()

	in := os.Stdin
	if *jsonPath != "" {
		f, err := os.Open(*jsonPath)
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		in = f
	}

	run, err := parse(in)
	if err != nil {
		log.Fatal(err)
	}
	run.Title = *title
	run.Commit = *commit
	run.Branch = *branch
	run.Generated = time.Now()

	if *coverPat != "" {
		cov, err := parseCoverage(*coverPat)
		if err != nil {
			log.Printf("coverage: %v (continuing without it)", err)
		} else {
			run.applyCoverage(cov)
		}
	}

	if *goldens != "" {
		imgs, err := loadImages(*goldens)
		if err != nil {
			log.Printf("goldens: %v (continuing without them)", err)
		} else {
			run.Images = imgs
		}
	}

	sort.Slice(run.Packages, func(i, j int) bool {
		// Failures first — that is what a reader came for.
		if run.Packages[i].Failed() != run.Packages[j].Failed() {
			return run.Packages[i].Failed()
		}
		return run.Packages[i].Name < run.Packages[j].Name
	})

	out, err := os.Create(*outPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := render(out, run); err != nil {
		_ = out.Close()
		log.Fatal(err)
	}
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}

	if *summary != "" {
		// Append: a workflow may write several sections to the same file.
		//
		// Note that GitHub gives each STEP its own summary file and
		// concatenates them for the job, so a later step cannot inspect what
		// this one wrote. That is why the size is reported here rather than
		// checked afterwards — a check in a separate step reads a different,
		// empty file and reports a failure that did not happen.
		f, err := os.OpenFile(*summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			log.Printf("summary: %v (continuing)", err)
		} else {
			var counted countingWriter
			opts := SummaryOptions{Repo: *repo, SHA: *commit, Artifact: *artifact}
			if err := writeSummary(io.MultiWriter(f, &counted), run, opts); err != nil {
				log.Printf("summary: %v (continuing)", err)
			}
			if err := f.Close(); err != nil {
				log.Printf("summary: %v (continuing)", err)
			}
			fmt.Printf("wrote %d bytes of Markdown summary to %s\n", counted.n, *summary)
			if counted.n == 0 {
				log.Print("the summary is empty; the run page will show nothing")
			}
		}
	}

	fmt.Printf("%s: %d passed, %d failed, %d skipped across %d packages -> %s\n",
		*title, run.Passed, run.Failed, run.Skipped, len(run.Packages), *outPath)

	// Exit non-zero if the run failed, so this can stand in for the test
	// command's own status in a pipeline.
	if run.Failed > 0 {
		os.Exit(1)
	}
}

// countingWriter records how much was written, so the job log can show that
// the summary was produced. There is no API for reading a step summary back.
type countingWriter struct{ n int }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}
