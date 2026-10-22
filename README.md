# slipbox

`s2n` turns the text of a source, one chapter at a time, into refined notes for
an Obsidian vault. It runs three model stages unattended, from start to finish.
Every output statement traces to a verbatim source quote, and losses are
proposed and added as traced statements rather than taken on trust.

How it works is in [`docs/design.md`](docs/design.md), the decisions behind it
in [`docs/adr/`](docs/adr/), and its vocabulary in [`GLOSSARY.md`](GLOSSARY.md).

## Building

```bash
nix develop          # optional: Go 1.26 and the tools
go build ./cmd/s2n
go test ./...
```

## Running

```bash
s2n run -vault ~/vault "<source>.md"     # or set S2N_VAULT
s2n resume <run folder>                  # pick up a stopped run
```

A run prints its run folder and runs every step, then puts the notes, the
name-clash list and the run report in `<vault>/Staging/<source name>/`. If a
run stops, it says what failed, where, and what to do, and prints the command
that resumes it. `s2n run -h` lists the flags. They set the vault and the
staging folder, a step's model, effort or reply shape, or a step to stop
after.

Model calls go through `claude -p` on your subscription. Start `s2n` from a
plain shell, not from inside a Claude session: inside one, the harness
overhead was 55–65% of the measured bill.
