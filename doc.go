// Package epaper drives e-ink panels attached to a Raspberry Pi over SPI.
//
// The goal is to make the panel disappear: a program that wants to put
// something on an e-ink screen should think only about the picture. Nothing in
// the consumer-facing API mentions SPI, GPIO, chip-select, bit packing or
// refresh sequencing.
//
// # Proving a panel
//
// Before anything else, put the test card on it. If what appears looks like
// the card, then the wiring, the transport, the command sequence, all four
// inks, the dithering and the text rendering are all working:
//
//	dev, err := inky.Open()
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer dev.Close()
//
//	if err := testcard.Show(ctx, dev, "bench check"); err != nil {
//		log.Fatal(err)
//	}
//
// # The shape of it
//
// A [Device] is a panel. It reports its geometry and its [Palette], hands out
// correctly-sized images, and shows them:
//
//	dev, err := inky.Open()
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer dev.Close()
//
//	c := render.NewCanvasFor(dev) // bounds and palette both come from the device
//	c.Fill(epaper.White)
//	c.Rect(image.Rect(0, 0, 400, 30), epaper.Red)
//	c.Text(image.Pt(8, 6), "HELLO", face, epaper.White)
//	if err := c.Err(); err != nil {
//		log.Fatal(err) // e.g. this panel has no red
//	}
//
//	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
//	defer cancel()
//
//	if err := dev.Show(ctx, c.Image()); err != nil {
//		log.Fatal(err)
//	}
//
// The consumer never writes a palette index, an image size or a colour model.
//
// # Inks and palettes
//
// Panels differ in what they can display, so the library separates two things
// that are easy to conflate. An [Ink] is which colour you mean — universal and
// panel-independent. A [Palette] is what a given panel can actually do, and is
// declared by its driver. Asking a four-colour palette for green reports that
// it is absent rather than silently substituting something plausible.
//
// # This library will not kill your process
//
// Every failure is a returned error. Nothing here calls os.Exit, and nothing
// panics on hardware state — a library that kills its caller cannot be used in
// a service. Anything that blocks takes a [context.Context]; a full refresh
// takes around 20 seconds, which is far too long to be uncancellable.
//
// # Testing without hardware
//
// Most of this library is pure: inks, palettes, framebuffer packing, the render
// toolkit and the EEPROM parser all run on a laptop. The mock package provides
// a Device that records the frames it is shown, so a whole display program can
// be built and tested with no Pi in the room — see examples/consumertest.
//
// This is not a fallback, it is the way to work. A refresh takes 20 seconds and
// happens in another room; a PNG takes 20 milliseconds and can be diffed.
//
// # Text
//
// One thing is worth knowing before drawing any: below about 16px, a scaled
// outline font cannot render legibly on a panel with no intermediate tones, and
// no amount of tuning fixes it. Use a bitmap font. [render.FontFamily] explains
// why, and testcard.Fonts is one that needs no font file.
//
// # Orientation
//
// A device reports its panel's native geometry, which for every panel this
// library drives is landscape, and [Device.Show] rejects an image of any other
// shape with [ErrWrongSize]. There is no rotation here — see PLAN.md §9.9 for
// why that is a decision rather than an omission, and examples/portrait for the
// eight lines that mount a panel on its end.
package epaper
