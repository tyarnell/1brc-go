package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/bits"
	"os"
	"runtime"
	"slices"
	"unicode/utf16"
)

const (
	hashTableSize  = 1 << 16
	readBufferSize = 1 << 20
)

func splitFileIntoChunks(file *os.File, count int) ([]io.Reader, error) {
	if count <= 0 {
		return nil, fmt.Errorf("chunk count must be positive")
	}

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	totalSize := info.Size()
	if totalSize == 0 {
		return nil, nil
	}

	boundaries := make([]int64, count+1)
	boundaries[count] = totalSize
	for i := 1; i < count; i++ {
		offset := totalSize * int64(i) / int64(count)
		boundaries[i], err = nextLineStart(file, offset, totalSize)
		if err != nil {
			return nil, err
		}
	}

	chunks := make([]io.Reader, 0, count)
	for i := range count {
		start, end := boundaries[i], boundaries[i+1]
		if end > start {
			chunks = append(chunks, io.NewSectionReader(file, start, end-start))
		}
	}
	return chunks, nil
}

func nextLineStart(file *os.File, offset, fileSize int64) (int64, error) {
	if offset <= 0 || offset >= fileSize {
		return offset, nil
	}

	var previous [1]byte
	if _, err := file.ReadAt(previous[:], offset-1); err != nil {
		return 0, err
	}
	if previous[0] == '\n' {
		return offset, nil
	}

	reader := bufio.NewReader(io.NewSectionReader(file, offset, fileSize-offset))
	prefix, err := reader.ReadString('\n')
	if err == io.EOF {
		return fileSize, nil
	}
	if err != nil {
		return 0, err
	}
	return offset + int64(len(prefix)), nil
}

func processChunk(reader io.Reader) (*HashTable, error) {
	table := NewHashTable(hashTableSize)
	buffer := make([]byte, readBufferSize+128)
	buffered := 0

	for {
		n, err := reader.Read(buffer[buffered:])
		if err != nil && err != io.EOF {
			return nil, err
		}

		end := buffered + n
		consumed, parseErr := processMeasurements(buffer[:end], table, err == io.EOF)
		if parseErr != nil {
			return nil, parseErr
		}
		buffered = copy(buffer, buffer[consumed:end])

		if err == io.EOF {
			return table, nil
		}
		if buffered == len(buffer) {
			return nil, fmt.Errorf("measurement exceeds %d bytes", len(buffer))
		}
	}
}

func processMeasurements(data []byte, table *HashTable, final bool) (int, error) {
	position := 0
	tableMask := uint64(len(table.table) - 1)
	for position < len(data) {
		lineStart := position
		stationEnd, hash, complete, err := scanStation(data, position)
		if err != nil {
			return 0, err
		}
		if !complete {
			if final {
				return 0, fmt.Errorf("incomplete measurement %q", data[lineStart:])
			}
			return lineStart, nil
		}

		station := data[lineStart:stationEnd]
		position = stationEnd + 1

		var temperature int32
		parsedTemperature := false
		if position+5 < len(data) {
			temperaturePosition := position
			negative := data[temperaturePosition] == '-'
			if negative {
				temperaturePosition++
			}
			first := int32(data[temperaturePosition] - '0')
			if data[temperaturePosition+1] == '.' {
				end := temperaturePosition + 3
				if data[end] == '\n' {
					temperature = first*10 + int32(data[temperaturePosition+2]-'0')
					position = end + 1
					parsedTemperature = true
				}
			} else {
				end := temperaturePosition + 4
				if data[end] == '\n' {
					temperature = first*100 + int32(data[temperaturePosition+1]-'0')*10 + int32(data[temperaturePosition+3]-'0')
					position = end + 1
					parsedTemperature = true
				}
			}
			if negative {
				temperature = -temperature
			}
		}
		if !parsedTemperature {
			temperatureStart := position
			for position < len(data) && data[position] != '\n' {
				position++
			}
			if position == len(data) && !final {
				return lineStart, nil
			}
			var err error
			temperature, err = parseTemperature(data[temperatureStart:position])
			if err != nil {
				return 0, fmt.Errorf("station %q: %w", station, err)
			}
			if position < len(data) {
				position++
			}
		}

		index := hash & tableMask
		for {
			entry := &table.table[index]
			if entry.key == nil {
				entry.key = bytes.Clone(station)
				entry.stats = Stats{Count: 1, Min: temperature, Max: temperature, Sum: int64(temperature)}
				table.size++
				break
			}
			if bytes.Equal(entry.key, station) {
				entry.stats.Count++
				entry.stats.Sum += int64(temperature)
				if temperature < entry.stats.Min {
					entry.stats.Min = temperature
				}
				if temperature > entry.stats.Max {
					entry.stats.Max = temperature
				}
				break
			}
			index = (index + 1) & tableMask
		}
	}
	return position, nil
}

func scanStation(data []byte, start int) (int, uint64, bool, error) {
	const (
		semicolon = 0x3b3b3b3b3b3b3b3b
		newline   = 0x0a0a0a0a0a0a0a0a
		ones      = 0x0101010101010101
		highBits  = 0x8080808080808080
	)

	position := start
	hash := uint64(stationHashSeed)
	for position+8 <= len(data) {
		word := binary.LittleEndian.Uint64(data[position:])
		semicolonXor := word ^ semicolon
		newlineXor := word ^ newline
		matches := ((semicolonXor - ones) & ^semicolonXor & highBits) |
			((newlineXor - ones) & ^newlineXor & highBits)
		if matches != 0 {
			offset := bits.TrailingZeros64(matches) / 8
			end := position + offset
			if data[end] == '\n' {
				return 0, 0, false, fmt.Errorf("missing temperature for station %q", data[start:end])
			}
			if offset > 0 {
				mask := uint64(1)<<(offset*8) - 1
				hash = mixStationHash(hash, word&mask)
			}
			return end, finishStationHash(hash, end-start), true, nil
		}
		hash = mixStationHash(hash, word)
		position += 8
	}

	var tail uint64
	tailLength := 0
	for position < len(data) {
		switch data[position] {
		case ';':
			if tailLength > 0 {
				hash = mixStationHash(hash, tail)
			}
			return position, finishStationHash(hash, position-start), true, nil
		case '\n':
			return 0, 0, false, fmt.Errorf("missing temperature for station %q", data[start:position])
		default:
			tail |= uint64(data[position]) << (8 * tailLength)
			tailLength++
			position++
		}
	}
	return position, 0, false, nil
}

func parseTemperature(value []byte) (int32, error) {
	digit := func(char byte) (int32, bool) {
		return int32(char - '0'), char >= '0' && char <= '9'
	}

	switch len(value) {
	case 3: // 1.2
		whole, wholeOK := digit(value[0])
		fraction, fractionOK := digit(value[2])
		if wholeOK && value[1] == '.' && fractionOK {
			return whole*10 + fraction, nil
		}
	case 4: // 12.3 or -1.2
		if value[0] == '-' {
			whole, wholeOK := digit(value[1])
			fraction, fractionOK := digit(value[3])
			if wholeOK && value[2] == '.' && fractionOK {
				return -(whole*10 + fraction), nil
			}
		} else {
			tens, tensOK := digit(value[0])
			ones, onesOK := digit(value[1])
			fraction, fractionOK := digit(value[3])
			if tensOK && onesOK && value[2] == '.' && fractionOK {
				return tens*100 + ones*10 + fraction, nil
			}
		}
	case 5: // -12.3
		tens, tensOK := digit(value[1])
		ones, onesOK := digit(value[2])
		fraction, fractionOK := digit(value[4])
		if value[0] == '-' && tensOK && onesOK && value[3] == '.' && fractionOK {
			return -(tens*100 + ones*10 + fraction), nil
		}
	}
	return 0, fmt.Errorf("invalid temperature %q", value)
}

func mergeHashTables(tables []*HashTable) *HashTable {
	merged := NewHashTable(hashTableSize)
	for _, table := range tables {
		merged.Merge(table)
	}
	return merged
}

func aggregate(file *os.File, workers int) (*HashTable, error) {
	chunks, err := splitFileIntoChunks(file, workers)
	if err != nil {
		return nil, err
	}

	type result struct {
		table *HashTable
		err   error
	}
	results := make(chan result, len(chunks))
	for _, chunk := range chunks {
		go func() {
			table, err := processChunk(chunk)
			results <- result{table: table, err: err}
		}()
	}

	tables := make([]*HashTable, 0, len(chunks))
	var firstError error
	for range chunks {
		result := <-results
		if result.err != nil && firstError == nil {
			firstError = result.err
		}
		if result.table != nil {
			tables = append(tables, result.table)
		}
	}
	if firstError != nil {
		return nil, firstError
	}
	return mergeHashTables(tables), nil
}

func roundedMean(sum, count int64) int64 {
	quotient, remainder := sum/count, sum%count
	if remainder >= 0 {
		if 2*remainder >= count {
			quotient++
		}
	} else if -2*remainder > count {
		quotient--
	}
	return quotient
}

func writeResults(writer io.Writer, table *HashTable) error {
	type stationName struct {
		name  string
		utf16 []uint16
	}
	stations := make([]stationName, 0, table.Len())
	for name := range table.Keys() {
		stations = append(stations, stationName{name: name, utf16: utf16.Encode([]rune(name))})
	}
	slices.SortFunc(stations, func(a, b stationName) int {
		return slices.Compare(a.utf16, b.utf16)
	})

	buffer := bufio.NewWriter(writer)
	if _, err := fmt.Fprint(buffer, "{"); err != nil {
		return err
	}
	for i, station := range stations {
		if i > 0 {
			if _, err := fmt.Fprint(buffer, ", "); err != nil {
				return err
			}
		}
		stats := table.Get([]byte(station.name))
		if _, err := fmt.Fprintf(buffer, "%s=%.1f/%.1f/%.1f",
			station.name,
			float64(stats.Min)/10,
			float64(roundedMean(stats.Sum, stats.Count))/10,
			float64(stats.Max)/10,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(buffer, "}"); err != nil {
		return err
	}
	return buffer.Flush()
}

func run(inputPath, outputPath string, workers int) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer input.Close()

	table, err := aggregate(input, workers)
	if err != nil {
		return err
	}

	output, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer output.Close()
	return writeResults(output, table)
}

func main() {
	input := flag.String("input", "data/measurements.txt", "measurement input file")
	output := flag.String("output", "data/output.txt", "result output file")
	workers := flag.Int("workers", runtime.NumCPU(), "number of parallel workers")
	flag.Parse()

	if err := run(*input, *output, *workers); err != nil {
		panic(err)
	}
	fmt.Printf("Results written to %s.\n", *output)
}
