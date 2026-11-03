# Benchmarks

VarMQ against other Go worker pools and Redis and SQLite job queues, from raw benchmark output, with the groups it does not win.

This page compares VarMQ with other Go worker pools, Redis job queues and SQLite job queues. Every row on it is one line of the raw `go test -bench` output in [goptics/benchmarks](https://github.com/goptics/benchmarks). Every figure is read from those rows or averaged over them. The run is from 2026-10-25, with VarMQ v1.10.0, at commit [`a0d0e0b`](https://github.com/goptics/benchmarks/tree/a0d0e0bf78aac11ebcfc0688600c79d6a8fccb93). The page holds all 1,667 rows of that run. Each result links to its line in the raw file.

## Explore the rows

Pick a dataset and narrow it with the select menus. Each lane is one subject's mean over the groups in scope. Its last column divides that mean by VarMQ. Press a square in the map to see one group, with a link to each of its rows in the raw file.

## How to read the numbers

- **A row is one line of output.** A group is the set of rows that run one case with different subjects. One example case is `Sleep10ms` with 50K workers and one producer. A subject leads a group when its row is the best one. When a VarMQ subject and another subject share the best value, the group is tied. A tied group counts for neither side. The map marks it with “=”.
- **Two iterations per row.** The files use `-benchtime=2x`, so each row covers two iterations, with no warm-up iteration. Absolute numbers change from run to run, so compare ratios inside one run.
- **A lane is a mean.** When several groups are in scope, a lane is the arithmetic mean of that subject over them. A subject that misses some of the groups is left out, because its mean would cover other rows. Narrow the filters to see it.
- **MB means 10^6 bytes.** Memory is the `B/op` column divided by one million. The charts at [benchmarks.goptics.org](https://benchmarks.goptics.org) divide by 2^20 but still print MB. So their memory figures are 4.6 % smaller.
- **Pool memory covers the whole process.** It counts every allocation during a run of 1M tasks. VarMQ's `Add` never blocks, so waiting tasks sit in an unbounded queue and use memory.
- **† marks a baseline.** `GoroutinePool` and `BufferedPool` are N goroutines that read one channel. They are not libraries. Switch them off with the Baselines control.
- **Redis has two VarMQ subjects.** `VarMQ` runs with one instance. `VarMQDistributed` runs with one and with four. Where VarMQ does not run, the × column divides by `VarMQDistributed`. The charts leave `VarMQDistributed` out of the one-instance, enqueue and latency groups. This page keeps it, because it is in the raw file.
- **Latency is enqueue to handler.** One job at a time, 1,000 jobs, 10 workers, one instance. Lower is better.

For the full method of each suite, read the READMEs in the benchmarks repository: [pool](https://github.com/goptics/benchmarks/blob/a0d0e0bf78aac11ebcfc0688600c79d6a8fccb93/pool/README.md), [Redis](https://github.com/goptics/benchmarks/blob/a0d0e0bf78aac11ebcfc0688600c79d6a8fccb93/redis/README.md) and [SQLite](https://github.com/goptics/benchmarks/blob/a0d0e0bf78aac11ebcfc0688600c79d6a8fccb93/sqlite/README.md).

## Where the data comes from

The page reads a committed snapshot, `src/components/bench/data.json`. A script builds it from the raw `.txt` files at one commit of the benchmarks repository. The script keeps the line number of every row, which is how the page links each figure to its line. The docs build does not use the network.

To update the snapshot, run `node scripts/bench-data.mjs --ref main --check-live` and then `pnpm test` in the `docs/` folder of the VarMQ repository.

## Run it yourself

Install Go 1.26 or later and [Task](https://taskfile.dev), then clone [goptics/benchmarks](https://github.com/goptics/benchmarks). The Redis suite needs Docker. The SQLite suite needs a C compiler.

```bash
# Quick check: one iteration of every benchmark
task pool:bench
task redis:up redis:bench redis:down
task sqlite:bench

# The published files: two iterations per row (-benchtime=2x)
task pool:txt                        # at -cpu 24, 8 and 4
task redis:up redis:txt redis:down
task sqlite:txt
```

Run details, at the foot of the explorer, shows the command for the groups in scope. The full run takes hours. The committed pool files took 1282 s, 917 s and 904 s.

See also the [comparison](https://varmq.goptics.org/reference/comparison/) of features and the [micro-benchmark log](https://varmq.goptics.org/bench-log/) of each release.
