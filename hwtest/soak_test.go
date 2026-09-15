//go:build hardware

package hwtest

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/sweeney/epaper/driver/jd79668"
	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/render"
	"github.com/sweeney/epaper/testcard"
)

var (
	soakRuns   = flag.Int("soak.runs", 8, "refreshes per soak test")
	soakVendor = flag.Bool("soak.vendor-timing", false,
		"restore the reference implementation's 300ms per-command delay")
)

// TestSoak refreshes the panel repeatedly and checks every refresh behaves the
// same way.
//
// It exists for PLAN §9.3. Removing the vendor's 300 ms per-command delay
// makes a refresh 4.9 s faster and the output looked perfect, but "it worked
// three times" is not evidence about a race. If the delay were masking a
// settling problem between driving CS/DC and clocking the byte out, the
// symptom would be INTERMITTENT — an occasional corrupt or short refresh, not
// a consistent one.
//
// What this can check without eyes on the panel: that no refresh errors, and
// that every one takes about as long as the last. A refresh that returns early
// means the panel never really started, which is the failure the timing would
// hide.
//
//	make test-hw HOST=... ARGS="-test.run TestSoak -soak.runs 20"
func TestSoak(t *testing.T) {
	dev, err := inky.OpenWith(inky.Options{CommandDelay: commandDelay(*soakVendor)})
	if err != nil {
		t.Fatalf("inky.Open(): %v", err)
	}
	defer func() { _ = dev.Close() }()

	label := "default timing (no per-command delay)"
	if *soakVendor {
		label = "vendor timing (300ms per command)"
	}
	t.Logf("soaking %d refreshes, %s", *soakRuns, label)

	var durations []time.Duration
	for i := range *soakRuns {
		c := render.NewCanvasFor(dev)
		// Alternate the pattern so consecutive refreshes are not identical;
		// a driver that quietly did nothing would otherwise be invisible.
		if i%2 == 0 {
			testcard.DrawConformance(c)
		} else {
			testcard.Draw(c, testcard.Options{
				Lines: []string{dev.Model(), "soak run", timestamp(i)},
			})
		}
		if err := c.Err(); err != nil {
			t.Fatalf("run %d: drawing: %v", i+1, err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), drawTimeout)
		started := time.Now()
		err := dev.Show(ctx, c.Image())
		elapsed := time.Since(started)
		cancel()

		if err != nil {
			t.Fatalf("run %d of %d failed after %s: %v", i+1, *soakRuns, elapsed.Round(time.Millisecond), err)
		}
		durations = append(durations, elapsed)
		t.Logf("  run %2d: %s", i+1, elapsed.Round(time.Millisecond))
	}

	// Consistency is the actual assertion. A refresh that suddenly returns
	// early, or takes much longer, is the panel behaving differently — which
	// is what an intermittent timing fault would look like.
	lo, hi := durations[0], durations[0]
	var total time.Duration
	for _, d := range durations {
		lo = min(lo, d)
		hi = max(hi, d)
		total += d
	}
	mean := total / time.Duration(len(durations))
	spread := hi - lo

	t.Logf("min %s  mean %s  max %s  spread %s",
		lo.Round(time.Millisecond), mean.Round(time.Millisecond),
		hi.Round(time.Millisecond), spread.Round(time.Millisecond))

	if spread > 2*time.Second {
		t.Errorf("refresh times vary by %s across %d runs; a timing fault would look like this",
			spread.Round(time.Millisecond), len(durations))
	}
	if lo < 5*time.Second {
		t.Errorf("fastest refresh was %s, far too short for a full redraw", lo.Round(time.Millisecond))
	}
}

func commandDelay(vendor bool) time.Duration {
	if vendor {
		return jd79668.VendorCommandDelay
	}
	return 0
}

func timestamp(i int) string {
	return time.Now().Format("15:04:05") + " #" + itoa(i+1)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	n := len(buf)
	for v > 0 {
		n--
		buf[n] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[n:])
}
