package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"testing"
	"testing/iotest"
)

func TestSplitFileIntoChunksPreservesInput(t *testing.T) {
	data := "Alpha;1.0\nBravo;-2.5\nCharlie;30.1\nDelta;4.0\n"
	file := writeMeasurementsFile(t, data)
	chunks, err := splitFileIntoChunks(file, 3)
	if err != nil {
		t.Fatal(err)
	}

	var combined strings.Builder
	for i, chunk := range chunks {
		contents, err := io.ReadAll(chunk)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && len(contents) > 0 && contents[0] == '\n' {
			t.Errorf("chunk %d starts with the previous record's newline", i)
		}
		if i < len(chunks)-1 && (len(contents) == 0 || contents[len(contents)-1] != '\n') {
			t.Errorf("chunk %d ends in the middle of a record", i)
		}
		combined.Write(contents)
	}

	if got := combined.String(); got != data {
		t.Fatalf("chunks do not preserve input:\n got %q\nwant %q", got, data)
	}
}

func TestParallelAggregation(t *testing.T) {
	data := "Alpha;1.0\nBravo;-2.5\nAlpha;3.0\nBravo;4.5\nAlpha;-1.0\n"
	file := writeMeasurementsFile(t, data)

	result, err := aggregate(file, 3)
	if err != nil {
		t.Fatal(err)
	}

	assertStats(t, result, "Alpha", Stats{Count: 3, Min: -10, Max: 30, Sum: 30})
	assertStats(t, result, "Bravo", Stats{Count: 2, Min: -25, Max: 45, Sum: 20})
}

func TestAggregationIsIndependentOfWorkerCount(t *testing.T) {
	stations := []string{"Alpha", "Bravo", "Charlie", "Delta"}
	temperatures := []string{"-9.9", "0.0", "12.3", "99.9", "-1.2"}
	var data strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&data, "%s;%s\n", stations[i%len(stations)], temperatures[i%len(temperatures)])
	}
	file := writeMeasurementsFile(t, data.String())

	baseline, err := aggregate(file, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := maps.Collect(baseline.All())

	for _, workers := range []int{2, 3, 7, 16} {
		got, err := aggregate(file, workers)
		if err != nil {
			t.Fatalf("%d workers: %v", workers, err)
		}
		if values := maps.Collect(got.All()); !maps.Equal(values, want) {
			t.Errorf("%d workers produced different aggregates:\n got %+v\nwant %+v", workers, values, want)
		}
	}
}

func TestProcessChunkIncludesFinalRecordWithoutNewline(t *testing.T) {
	table, err := processChunk(strings.NewReader("Alpha;1.0"))
	if err != nil {
		t.Fatal(err)
	}
	assertStats(t, table, "Alpha", Stats{Count: 1, Min: 10, Max: 10, Sum: 10})
}

func TestProcessChunkPreservesStateAcrossReads(t *testing.T) {
	table, err := processChunk(iotest.OneByteReader(strings.NewReader("Alpha;-12.3\nBravo;4.5\n")))
	if err != nil {
		t.Fatal(err)
	}
	assertStats(t, table, "Alpha", Stats{Count: 1, Min: -123, Max: -123, Sum: -123})
	assertStats(t, table, "Bravo", Stats{Count: 1, Min: 45, Max: 45, Sum: 45})
}

func TestStationHashAcrossWordBoundaries(t *testing.T) {
	var input strings.Builder
	for length := 1; length <= 100; length++ {
		fmt.Fprintf(&input, "%s;1.0\n", strings.Repeat("a", length))
	}
	table, err := processChunk(strings.NewReader(input.String()))
	if err != nil {
		t.Fatal(err)
	}
	for length := 1; length <= 100; length++ {
		assertStats(t, table, strings.Repeat("a", length), Stats{Count: 1, Min: 10, Max: 10, Sum: 10})
	}
}

func TestWriteResultsUsesCanonicalRounding(t *testing.T) {
	input := "NegativeTie;-0.1\nNegativeTie;0.0\n" +
		"NegativeZero;-0.1\nNegativeZero;0.0\nNegativeZero;0.0\n" +
		"PositiveTie;0.2\nPositiveTie;0.3\n"
	table, err := processChunk(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if err := writeResults(&output, table); err != nil {
		t.Fatal(err)
	}
	want := "{NegativeTie=-0.1/0.0/0.0, NegativeZero=-0.1/0.0/0.0, PositiveTie=0.2/0.3/0.3}\n"
	if output.String() != want {
		t.Errorf("output = %q, want %q", output.String(), want)
	}
}

func TestWriteResultsUsesJavaStringOrder(t *testing.T) {
	table, err := processChunk(strings.NewReader("\ue000;1.0\n\U00010000;2.0\n"))
	if err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if err := writeResults(&output, table); err != nil {
		t.Fatal(err)
	}
	want := "{\U00010000=2.0/2.0/2.0, \ue000=1.0/1.0/1.0}\n"
	if output.String() != want {
		t.Errorf("output = %q, want %q", output.String(), want)
	}
}

func writeMeasurementsFile(t *testing.T, data string) *os.File {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "measurements-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(data); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func assertStats(t *testing.T, table *HashTable, station string, want Stats) {
	t.Helper()
	if got := table.Get([]byte(station)); got != want {
		t.Errorf("%s stats = %+v, want %+v", station, got, want)
	}
}
