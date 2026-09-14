# tools/

Python helpers for producing fixtures. Not part of the Go build.

| Script | Runs where | Purpose |
|---|---|---|
| `conformance.py` | **On the Pi**, in the reference venv | Regenerates `testdata/conformance/*`. Uses the real vendor library as the oracle, but monkeypatches `_update` so it never touches hardware |
| `testcard_f_reference.py` | Pi or Mac | Four-ink BBC Test Card F from bring-up. `--png out.png` needs no hardware. A visual target, not a byte-comparable fixture — it contains text |

See `testdata/README.md` §4 for the regeneration commands.
