# Performance and implementation history

This repository contains one implementation: the fastest version that remained
correct throughout testing. Earlier experiments are summarized here rather than
kept as separate branches.

## Correctness gates

Every optimization had to pass:

- unit and integration tests covering parallel chunk boundaries, final records
  without newlines, temperature formats, canonical rounding, UTF-16 station
  ordering, and billion-row aggregate ranges;
- the race detector and `go vet`;
- byte-for-byte output comparison on a fixed 1 GiB sample; and
- a full 15.9 GB run before accepting major changes.

The fixed sample's SHA-256 result was
`513a318caf37bd0e407ae48f1523c8d5ba7d302c88a2b135186c32ce090c5e98`.

## Evolution

The implementation progressed through these designs:

1. `bufio.Scanner`, strings, floating-point parsing, and a Go map.
2. Integer temperatures and buffered byte parsing.
3. A custom open-addressed station table.
4. Line-aligned parallel file sections and worker-local tables.
5. Correct aggregate merging with 64-bit counts and sums.
6. A fused parser/table hot path.
7. Combined word-at-a-time delimiter scanning and station hashing.
8. A validated fast path for the four legal temperature layouts.

Representative warm-cache timings for the 1 GiB validation sample were:

| Stage | Time |
| --- | ---: |
| Correct parallel baseline | 0.53s |
| Worker tuning | 0.47s |
| Direct line parser | 0.33s |
| Fused parser and table update | 0.32s |
| Combined station scan and hash | 0.30s |
| Fixed-format temperature fast path | 0.25s |

On an Apple M4 with 16 GB RAM, the final implementation processed the full
15.9 GB input in 4.68 seconds with 10 workers. Reading the same file into
`/dev/null` took 4.63 seconds, so the final result was effectively limited by
storage throughput on that machine.

## Current design

Each worker receives a contiguous file section whose boundaries are moved to the
start of complete records. It reads through a reusable buffer and maintains only
the small incomplete record that can cross a read boundary.

Within each complete record, the hot path:

1. checks eight station-name bytes at a time for `;` or newline;
2. mixes those bytes into the station hash during the same scan;
3. parses the temperature directly when it matches a legal 1BRC layout;
4. probes a worker-local table; and
5. updates count, sum, minimum, and maximum in place.

Worker tables are merged only after parsing completes. Station keys are copied
once per worker, and full key equality is checked after hashing to preserve
correctness in the event of collisions.

## Rejected experiments

Several plausible optimizations were slower on the test machine and were removed:

- memory mapping increased cold-start page-fault cost;
- larger read buffers did not improve throughput;
- a 32,768-slot table caused extra probing;
- cached hashes enlarged table entries enough to hurt cache behavior;
- string fingerprints and non-owning `unsafe` string views were slower than
  `bytes.Equal`;
- scanning station names with SWAR and then hashing them in a second pass was
  slower than the combined scan; and
- a fully branched direct temperature parser was slower than a compact validated
  fast path with fallback parsing.

## Benchmarking

Build once so compilation is not included:

```sh
go build -o ./bin/process ./cmd/process
time ./bin/process
```

Worker count is hardware-dependent. Override it with `-workers` and compare full
runs. Sub-three-second processing of a 15.9 GB uncached input requires sustained
storage throughput above 5.3 GB/s regardless of parser speed.
