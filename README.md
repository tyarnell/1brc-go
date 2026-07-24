# 1brc-go

A parallel Go implementation of the [One Billion Row Challenge](https://github.com/gunnarmorling/1brc).

## Run

Generate a measurement file:

```sh
go run ./cmd/create-measurements 1_000_000_000
```

Process it using all available CPUs:

```sh
go build -o ./bin/process ./cmd/process
./bin/process
```

The sorted `{station=min/mean/max}` result is written to `data/output.txt`.
Use `-workers`, `-input`, and `-output` to override the defaults, for example:

```sh
./bin/process -workers 10
```

## Test

```sh
go test ./...
```

The processor divides the input only at complete line boundaries, aggregates each
chunk independently, and merges count/min/max/sum statistics. Temperatures and
sums remain integer tenths throughout processing to avoid floating-point parsing
and rounding errors.

## Implementation

The processing path combines buffered parallel reads, word-at-a-time station
scanning and hashing, fixed-format temperature parsing, and worker-local
open-addressed hash tables. Full station names are still compared, so hash
collisions cannot corrupt results.

See [docs/performance.md](docs/performance.md) for the optimization history,
benchmark methodology, rejected experiments, and hardware limits.
