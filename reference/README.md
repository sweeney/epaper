# reference/

Third-party source, kept for reference. **Not part of the build** — nothing in
`epaper` imports or links any of this, and Go ignores `.py` files.

## `vendor-inky/`

Three files from Pimoroni's `inky` Python library, version **2.5.0**, as
installed on our Pi at `~/inky-trial/venv/lib/python3.13/site-packages/inky/`.

| File | Why it is here |
|---|---|
| `inky_jd79668.py` | **The authority** for the command sequence, payload constants, pin assignments, reset timing and the packing expression. Every magic number in `driver/jd79668` traces back to this file |
| `eeprom.py` | The EEPROM struct layout, the colour table and the display-variant table |
| `auto.py` | How detection maps an EEPROM record to a driver |

### Licence

Pimoroni's `inky` is **MIT licensed**. Copyright Pimoroni Ltd.

<https://github.com/pimoroni/inky>

These files are reproduced unmodified under that licence. Our library is an
independent implementation, but the constants and command sequence are derived
from this work, and that derivation is acknowledged here and in `NOTICE`.

### Reading them

Start at `Inky.setup()` for the init sequence, `Inky._update()` for the refresh
sequence, and `Inky.show()` for the packing. `PLAN.md` §2.5 is a distilled
version of those three; this is the primary source if anything is ambiguous.

Two things in here are deliberately **not** copied into our implementation, and
the reasons are in `PLAN.md`:

- `_send_command` sleeps 300 ms before *every* command (~4.8 s per refresh).
  Whether that is load-bearing is PLAN §9.3, to be measured before M6.
- `_busy_wait` sleeps the full 40 s timeout when BUSY reads high on entry. We
  return an error instead — see PLAN §2.3.
