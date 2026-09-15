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
		f, err := os.OpenFile(*summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			log.Printf("summary: %v (continuing)", err)
		} else {
			opts := SummaryOptions{Repo: *repo, SHA: *commit, Artifact: *artifact}
			if err := writeSummary(f, run, opts); err != nil {
				log.Printf("summary: %v (continuing)", err)
			}
			if err := f.Close(); err != nil {
				log.Printf("summary: %v (continuing)", err)
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
