# examples/noisy

Demonstrates UNRELIABLE and WARN. A high-temperature model is asked for a coin flip and the
case expects `HEADS`, so each run scores 0 or 100 at random. Whenever either side's scores
swing by more than `max_spread_before_unreliable`, the case is reported UNRELIABLE instead
of PASS or REGRESSED, the decision is `WARN`, and the exit status is 0. Noise never fails
the gate.

```bash
cd examples/noisy
../../evalci run --baseline-dir ./baseline
echo $?   # 0
```

Requires a local Ollama server with `llama3.2:1b`. Because the outputs are random, there is
a small chance both sides happen to produce identical answers and the case PASSes; rerun to
see it.
