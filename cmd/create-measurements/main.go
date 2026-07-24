package main

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

func checkArgs() (int, error) {
	if len(os.Args) != 2 {
		return 0, errors.New("Usage: create-measurements <positive integer number of records to create>")
	}
	numRowsStr := strings.ReplaceAll(os.Args[1], "_", "")
	numRows, err := strconv.Atoi(numRowsStr)
	if err != nil || numRows <= 0 {
		return 0, errors.New("Argument must be a positive integer")
	}
	return numRows, nil
}

func buildWeatherStationNameList() ([]string, error) {
	file, err := os.Open("data/weather_stations.csv")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stationSet := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "#") {
			continue
		}
		name := strings.Split(line, ";")[0]
		stationSet[name] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	stationNames := make([]string, 0, len(stationSet))
	for name := range stationSet {
		stationNames = append(stationNames, name)
	}
	return stationNames, nil
}

func convertBytes(num float64) string {
	units := []string{"bytes", "KiB", "MiB", "GiB"}
	for _, unit := range units {
		if num < 1024.0 {
			return fmt.Sprintf("%3.1f %s", num, unit)
		}
		num /= 1024.0
	}
	return fmt.Sprintf("%3.1f GiB", num)
}

func formatElapsedTime(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.3f seconds", seconds)
	} else if seconds < 3600 {
		m := int(seconds) / 60
		s := int(seconds) % 60
		return fmt.Sprintf("%d minutes %d seconds", m, s)
	} else {
		h := int(seconds) / 3600
		rem := int(seconds) % 3600
		m := rem / 60
		s := rem % 60
		if m == 0 {
			return fmt.Sprintf("%d hours %d seconds", h, s)
		}
		return fmt.Sprintf("%d hours %d minutes %d seconds", h, m, s)
	}
}

func estimateFileSize(stationNames []string, numRows int) string {
	totalNameBytes := 0
	for _, s := range stationNames {
		totalNameBytes += len([]byte(s))
	}
	avgNameBytes := float64(totalNameBytes) / float64(len(stationNames))
	avgTempBytes := 4.400200100050025
	avgLineLength := avgNameBytes + avgTempBytes + 2
	humanFileSize := convertBytes(float64(numRows) * avgLineLength)
	return fmt.Sprintf("Estimated max file size is:  %s.", humanFileSize)
}

func buildTestData(stationNames []string, numRows int) error {
	start := time.Now()
	coldest := -99.9
	hottest := 99.9
	batchSize := 10000
	chunks := numRows / batchSize
	if numRows%batchSize != 0 {
		chunks++
	}
	outputPath := "data/measurements.txt"
	os.MkdirAll("data", 0755)
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	progress := 0
	station10k := make([]string, 10000)
	for i := range station10k {
		station10k[i] = stationNames[rand.Intn(len(stationNames))]
	}
	w := bufio.NewWriter(file)
	for chunk := 0; chunk < chunks; chunk++ {
		k := batchSize
		if chunk == chunks-1 && numRows%batchSize != 0 {
			k = numRows % batchSize
		}
		batch := make([]string, k)
		for i := 0; i < k; i++ {
			station := station10k[rand.Intn(len(station10k))]
			temp := coldest + rand.Float64()*(hottest-coldest)
			batch[i] = fmt.Sprintf("%s;%.1f", station, temp)
		}
		w.WriteString(strings.Join(batch, "\n") + "\n")
		if (chunk+1)*100/chunks != progress {
			progress = (chunk + 1) * 100 / chunks
			bars := strings.Repeat("=", progress/2)
			fmt.Printf("\r[%-50s] %d%%", bars, progress)
		}
	}
	w.Flush()
	fmt.Println()
	elapsed := time.Since(start).Seconds()
	stat, _ := os.Stat(outputPath)
	humanFileSize := convertBytes(float64(stat.Size()))
	fmt.Println("Test data successfully written to data/measurements.txt")
	fmt.Printf("Actual file size:  %s\n", humanFileSize)
	fmt.Printf("Elapsed time: %s\n", formatElapsedTime(elapsed))
	return nil
}

func main() {
	numRows, err := checkArgs()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	stationNames, err := buildWeatherStationNameList()
	if err != nil {
		fmt.Println("Error reading weather station names:", err)
		os.Exit(1)
	}
	fmt.Println(estimateFileSize(stationNames, numRows))
	err = buildTestData(stationNames, numRows)
	if err != nil {
		fmt.Println("Error building test data:", err)
		os.Exit(1)
	}
	fmt.Println("Test data build complete.")
}
