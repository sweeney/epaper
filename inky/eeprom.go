// Package inky drives Pimoroni Inky panels.
//
// It is the board-specific layer: it identifies which panel is attached by
// reading the HAT's EEPROM, maps the Pimoroni pin assignments, and hands back
// an [epaper.Device] backed by the right controller driver. The drawing and
// refresh work happens in the controller driver underneath.
package inky

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Errors this package returns. Match with [errors.Is].
var (
	// ErrBadEEPROM means the identification record could not be read or does
	// not describe a real panel — a short read, impossible geometry, or a
	// malformed write-time string. In practice it usually means no HAT is
	// attached, or the I2C read was done the wrong way (see below).
	ErrBadEEPROM = errors.New("inky: invalid EEPROM record")

	// ErrUnknownVariant means the record is well formed but names a display
	// variant this library has no entry for — a newer board than we know
	// about. The error names the variant number.
	ErrUnknownVariant = errors.New("inky: unknown display variant")
)

// eepromSize is the length of the identification record, in bytes.
//
// It lives at I2C address 0x50 and needs a two-byte register address written
// before the read (write 0x0000, repeated start, read 29). Byte-mode SMBus
// reads — what `i2cdump -b` does — return garbage from this part, which looks
// convincingly like a seating fault. Always do a combined write-then-read.
const eepromSize = 29

// Field offsets within the record. Layout, little-endian, is the vendor's
// struct format "<HHBBB22p":
//
//	0  u16  width
//	2  u16  height
//	4  u8   colour          index into colourNames
//	5  u8   pcbVariant      stored as ten times the board revision
//	6  u8   displayVariant  index into displayVariants
//	7  u8   write-time length (Pascal string)
//	8  21B  write time, ASCII
//
// Derived from Pimoroni inky 2.5.0, eeprom.py, reproduced at
// reference/vendor-inky/.
const (
	offWidth          = 0
	offHeight         = 2
	offColour         = 4
	offPCBVariant     = 5
	offDisplayVariant = 6
	offWriteTimeLen   = 7
	offWriteTime      = 8

	writeTimeCap = eepromSize - offWriteTime // 21
)

// colourNames is indexed by the record's colour byte. The holes are holes in
// the vendor's table, not omissions here; a record naming one is rejected.
//
// Note that index 7, "red/yellow", means the panel has both inks and can show
// them simultaneously. It does not mean "one of the two" — a misreading that
// cost us a day on the bench.
//
// Derived from Pimoroni inky 2.5.0, eeprom.py EPDType.valid_colors.
var colourNames = [...]string{
	1: "black",
	2: "red",
	3: "yellow",
	5: "7colour",
	6: "spectra6",
	7: "red/yellow",
}

// displayVariants is indexed by the record's displayVariant byte. The holes
// are the vendor's; a record naming one is rejected as unknown.
//
// Derived from Pimoroni inky 2.5.0, eeprom.py DISPLAY_VARIANT.
var displayVariants = [...]string{
	1:  "Red pHAT (High-Temp)",
	2:  "Yellow wHAT",
	3:  "Black wHAT",
	4:  "Black pHAT",
	5:  "Yellow pHAT",
	6:  "Red wHAT",
	7:  "Red wHAT (High-Temp)",
	8:  "Red wHAT",
	10: "Black pHAT (SSD1608)",
	11: "Red pHAT (SSD1608)",
	12: "Yellow pHAT (SSD1608)",
	14: "7-Colour (UC8159)",
	15: "7-Colour 640x400 (UC8159)",
	16: "7-Colour 640x400 (UC8159)",
	17: "Black wHAT (SSD1683)",
	18: "Red wHAT (SSD1683)",
	19: "Yellow wHAT (SSD1683)",
	20: "7-Colour 800x480 (AC073TC1A)",
	21: "Spectra 6 13.3 1600 x 1200 (EL133UF1)",
	22: "Spectra 6 7.3 800 x 480 (E673)",
	23: "Red/Yellow pHAT (JD79661)",
	24: "Red/Yellow wHAT (JD79668)",
	25: "Spectra 6 4.0 600 x 400 (E640)",
	26: "Spectra 6 7.3 800 x 480 (E673) AC",
	27: "Spectra 6 13.3 1600 x 1200 (EL133UF1) AC",
	28: "Red/Yellow wHAT (SSD2683)",
}

// maxDimension is a sanity bound on the geometry. The largest panel in the
// variant table is 1600x1200, so anything past this is a bad read rather than
// a panel we have not met — a floating I2C bus reads as 0xFFFF.
const maxDimension = 4096

// EEPROM is a panel's identification record, as written at the factory.
type EEPROM struct {
	// Width and Height are the panel's geometry in pixels.
	Width, Height int

	// Colour names the inks the panel carries, in the vendor's vocabulary:
	// "black", "red", "yellow", "red/yellow", "7colour" or "spectra6".
	// "red/yellow" means both, simultaneously.
	Colour string

	// PCBVariant is the board revision as stored, which is ten times the
	// actual revision: 100 means v10.0. Use [EEPROM.PCBRevision] for
	// something fit to show a human.
	PCBVariant uint8

	// DisplayVariant indexes the vendor's variant table; Model is the
	// corresponding name. It determines which controller driver to use.
	DisplayVariant uint8

	// Model is the panel's name, e.g. "Red/Yellow wHAT (JD79668)".
	Model string

	// WriteTime is when the record was written at the factory, as free text.
	// It may legitimately be empty.
	WriteTime string
}

// PCBRevision returns the board revision in the form a human expects, e.g.
// "10.0". The EEPROM stores the revision multiplied by ten, so this divides by
// ten; that is the whole of the trick, and it is written down because the byte
// value 100 otherwise looks like a hundred boards.
func (e EEPROM) PCBRevision() string {
	return fmt.Sprintf("%d.%d", e.PCBVariant/10, e.PCBVariant%10)
}

// ParseEEPROM decodes a panel's identification record.
//
// The buffer must be at least 29 bytes; anything beyond that is ignored, since
// an I2C read may hand back a padded buffer.
//
// Every failure returns nil and an error wrapping [ErrBadEEPROM] or
// [ErrUnknownVariant]. In particular a short read is an error rather than a
// zero-valued struct: a panel reporting 0x0 pixels would otherwise look like a
// successful parse and fail much later, somewhere less informative.
func ParseEEPROM(b []byte) (*EEPROM, error) {
	if len(b) < eepromSize {
		return nil, fmt.Errorf("inky: EEPROM read returned %d bytes, want at least %d: %w",
			len(b), eepromSize, ErrBadEEPROM)
	}

	e := &EEPROM{
		Width:          int(binary.LittleEndian.Uint16(b[offWidth:])),
		Height:         int(binary.LittleEndian.Uint16(b[offHeight:])),
		PCBVariant:     b[offPCBVariant],
		DisplayVariant: b[offDisplayVariant],
	}

	// Geometry first: it is the cheapest tell that we are looking at noise
	// rather than a record. An absent EEPROM reads as all zeroes; a floating
	// bus with nothing acknowledging reads as all ones.
	if e.Width <= 0 || e.Height <= 0 || e.Width > maxDimension || e.Height > maxDimension {
		return nil, fmt.Errorf("inky: EEPROM reports %dx%d pixels, which is not a real panel "+
			"(is a HAT attached?): %w", e.Width, e.Height, ErrBadEEPROM)
	}

	colour := b[offColour]
	if int(colour) >= len(colourNames) || colourNames[colour] == "" {
		return nil, fmt.Errorf("inky: EEPROM names colour %d, which is not in the colour table: %w",
			colour, ErrBadEEPROM)
	}
	e.Colour = colourNames[colour]

	if int(e.DisplayVariant) >= len(displayVariants) || displayVariants[e.DisplayVariant] == "" {
		return nil, fmt.Errorf("inky: display variant %d is not one this library knows "+
			"(a newer board than we have an entry for?): %w", e.DisplayVariant, ErrUnknownVariant)
	}
	e.Model = displayVariants[e.DisplayVariant]

	// Pascal string: one length byte, then that many bytes of ASCII. A length
	// past the end of the record is the classic index-out-of-range bug, so it
	// is rejected rather than clamped — a clamped value would be a plausible
	// looking but wrong timestamp.
	n := int(b[offWriteTimeLen])
	if n > writeTimeCap {
		return nil, fmt.Errorf("inky: EEPROM write-time length is %d but only %d bytes follow: %w",
			n, writeTimeCap, ErrBadEEPROM)
	}
	e.WriteTime = string(b[offWriteTime : offWriteTime+n])

	return e, nil
}
