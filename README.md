# clawtop

`clawtop` shows OpenClaw model calls in the terminal. Each row is one call: when it ran, how many prompt tokens went in, and how many tokens came back.

## Run

You need Go 1.27.

```bash
go build -o clawtop .
./clawtop
```

It reads `~/.openclaw/agents/*/agent/openclaw-agent.sqlite` in read-only mode and refreshes every second. On open it selects the session with the newest event.

## Keys

- `j` / `k` or the arrow keys scroll. Scrolling up leaves follow mode. The header then says `hold`.
- `f` follows the newest row again.
- `n` and `p` move to a newer or older session.
- `q`, `esc`, or `ctrl+c` quits.

## Columns

`in` is the prompt tokens reported for that call. If the provider reports no cache, `in` is the full prompt, including the system prompt and earlier turns. `out` is only the tokens written on that call.

A row is highlighted when `out` is 3 or less. `waste` in the header is the sum of `in` on those rows.

The first row of a run shows the user text, cut at 60 characters. Later rows of the same run show the tool name.
