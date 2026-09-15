package inky

import (
	"errors"
	"strings"
	"testing"

	"github.com/sweeney/epaper/internal/gpiocdev"
)

// The chip-select error must carry the FIX, not just the symptom.
//
// Without dtoverlay=spi0-0cs the kernel owns GPIO 8, and the failure presents
// as a busy line rather than as a missing device — twenty minutes of debugging
// for anyone who has not met it before. This test is in-package so the advice
// can be checked without a Pi that is deliberately misconfigured.
func TestChipSelectErrorCarriesTheFix(t *testing.T) {
	err := gpioError(DefaultPins, gpiocdev.ErrLineBusy)

	if !errors.Is(err, ErrChipSelectBusy) {
		t.Fatalf("error = %v, want ErrChipSelectBusy", err)
	}
	for _, want := range []string{"dtoverlay=spi0-0cs", "config.txt", "reboot", "8"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Any other GPIO failure must still say the most likely cause rather than
// passing the kernel's message through bare.
func TestOtherGPIOErrorsMentionGroups(t *testing.T) {
	err := gpioError(DefaultPins, errors.New("permission denied"))

	if errors.Is(err, ErrChipSelectBusy) {
		t.Error("a non-busy failure was reported as a chip-select conflict")
	}
	if !strings.Contains(err.Error(), "gpio group") {
		t.Errorf("error %q does not suggest checking group membership", err)
	}
}
