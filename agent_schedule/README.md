# 10 Go Time Patterns for Agent Scheduling — Experiment

## Run

```bash
go run agent_schedule.go
```


## Notes

This is a single-file demo. No dependencies. No external packages. Just `time` and `fmt`.

The patterns are ordered from simplest to most useful for agent scheduling:

1. `time.Now` + `time.Since` — latency measurement
2. `time.After` — timeout via channel
3. `time.AfterFunc` — scheduled callback
4. `time.NewTicker` — periodic task
5. `time.NewTimer` — one-shot scheduled event
6. `time.Tick` — simpler periodic (no Stop)
7. `time.Sleep` — simple delay
8. `time.Unix` / `time.UnixNano` — epoch timestamps
9. Retry with exponential backoff
10. Graceful shutdown with timeout

Each pattern is a complete, runnable example. The output shows the pattern number and what it does.