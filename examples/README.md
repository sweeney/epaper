# examples

Four ways to use the library, smallest first. Each is a runnable program you
can read top to bottom.

| Example | Shows | Needs a panel? |
|---|---|---|
| [`hello`](hello) | The smallest useful program: open, draw, show | Yes |
| [`offline`](offline) | Building a layout with no hardware, saving a PNG | **No** |
| [`dashboard`](dashboard) | A realistic status panel: rows, alignment, bars, wrapping | Optional (`-png`) |
| [`consumertest`](consumertest) | Testing *your own* display code against the mock | **No** |

## Start with `offline`

The fastest way to build anything for e-ink is to not use the panel. A refresh
takes about 20 seconds; a PNG takes about 20 milliseconds, and you can diff it.

Get the layout right offline, then swap `mock.New` for `inky.Open` — that one
line is the only difference, because both satisfy [`epaper.Device`].

## Then read `consumertest`

It is the least obvious of the four and probably the most useful. Your display
code is just a function from data to an image, so it can be tested like any
other function: no Pi, no panel, no 25-second wait. `consumertest` shows the
whole pattern, including how to assert that a value actually made it onto the
screen and that nothing ran off the edge.

[`epaper.Device`]: https://pkg.go.dev/github.com/sweeney/epaper#Device
