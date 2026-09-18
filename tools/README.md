# tools/

Python helpers for producing fixtures. Not part of the Go build.

| Script | Runs where | Purpose |
|---|---|---|
| `conformance.py` | **On the Pi**, in the reference venv (recreate it — `PLAN.md` §11.3) | Regenerates `testdata/conformance/*`. Uses the real vendor library as the oracle, but monkeypatches `_update` so it never touches hardware |
| `frame_jd79661.py` | **On the Pi**, reference venv, **pHAT attached** | Regenerates `testdata/frame/jd79661-frame.*`: the JD79661's rotated, padded frame layout, from the real vendor driver |
| `frame_e640.py` | **On the Pi**, reference venv, **Impression attached** | Regenerates `testdata/frame/e640-frame.*`: the E640's remap, rotation and nibble order, through the vendor's own `set_image()` |
| `testcard_f_reference.py` | Pi or Mac | Four-ink BBC Test Card F from bring-up. `--png out.png` needs no hardware. A visual target, not a byte-comparable fixture — it contains text |

See `testdata/README.md` §4 for the regeneration commands.
