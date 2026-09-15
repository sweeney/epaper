package inky_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/sweeney/epaper/inky"
	"github.com/sweeney/epaper/testcard"
)

// Open finds the attached panel, works out what it is from the HAT's EEPROM,
// and returns a driver for it.
//
// This example needs real hardware, so it is compiled but not run.
func ExampleOpen() {
	dev, err := inky.Open()
	if err != nil {
		// The one failure worth handling specially: without
		// dtoverlay=spi0-0cs the kernel SPI driver owns GPIO 8 and the panel
		// cannot be addressed. The error says so, but a program that runs
		// unattended may want to act on it.
		if errors.Is(err, inky.ErrChipSelectBusy) {
			log.Fatal("add dtoverlay=spi0-0cs to /boot/firmware/config.txt and reboot")
		}
		log.Fatal(err)
	}
	defer dev.Close()

	fmt.Println(dev.Model(), dev.Bounds())

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := testcard.Show(ctx, dev, "first light"); err != nil {
		log.Fatal(err)
	}
}

// Identify reads the HAT's EEPROM without claiming any other hardware, which
// is useful for working out what is plugged in.
//
// This example needs real hardware, so it is compiled but not run.
func ExampleIdentify() {
	info, err := inky.Identify(inky.DefaultI2CPath)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s: %dx%d, %s inks, board v%s, written %s\n",
		info.Model, info.Width, info.Height, info.Colour,
		info.PCBRevision(), info.WriteTime)
}

// ParseEEPROM decodes a record you have already read, which is how the
// identification can be tested without a Pi.
func ExampleParseEEPROM() {
	// The 29 bytes from our board, captured 2026-09-14.
	raw := []byte{
		0x90, 0x01, // width  = 400
		0x2C, 0x01, // height = 300
		0x07, // colour = red/yellow
		0x64, // pcb variant = 100, stored as ten times the revision
		0x18, // display variant = 24
		0x15, // write-time length
		'2', '0', '2', '5', '-', '0', '8', '-', '2', '0', ' ',
		'1', '5', ':', '5', '1', ':', '5', '5', '.', '5',
	}

	info, err := inky.ParseEEPROM(raw)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(info.Model)
	fmt.Println(info.Width, "x", info.Height, info.Colour)
	fmt.Println("board v" + info.PCBRevision())
	// Output:
	// Red/Yellow wHAT (JD79668)
	// 400 x 300 red/yellow
	// board v10.0
}
