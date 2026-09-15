// Package hwtest holds the tests that need real hardware.
//
// Everything here is behind the "hardware" build tag, so it never runs in CI
// and never runs on a laptop. The tests are gathered into one package because
// `go test -c` compiles a single package into a single binary, and one binary
// is what gets shipped to the Pi:
//
//	make test-hw HOST=sweeney@192.168.1.6
//
// That cross-compiles, scps, runs and removes the binary. No Go toolchain is
// needed on the Pi.
//
// These tests drive real hardware. They read the EEPROM, claim GPIO lines and
// open the SPI device; the ones that refresh the panel take about 20 seconds
// each and are marked as such.
package hwtest
