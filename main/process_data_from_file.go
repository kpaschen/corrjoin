package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/buckets"
	"log"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
)

func main() {
	filename := flag.String("filename", "", "Name of the file to read")
	windowSize := flag.Int("windowSize", 1020, "column count of a time series window")
	stride := flag.Int("stride", 102, "how much to slide the time series window by")
	correlationThreshold := flag.Int("correlationThreshold", 90, "correlation threshold in percent")
	// paper says 15 is good
	ks := flag.Int("ks", 15, "How many columns to reduce the input to in the first paa step")
	// paper says 30 is good
	ke := flag.Int("ke", 30, "How many columns to reduce the input to in the second paa step")
	// Use more than 3 when you have more than about 20k timeseries.
	svdDimensions := flag.Int("svdOutput", 3, "How many columns to choose after svd") // aka kb
	full := flag.Bool("full", false, "Whether to run pearson on all pairs")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile here")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	file, err := os.Open(*filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	lineCount := 0
	columnCount := 0
	reader := bufio.NewReader(file)
	var data [][]float64

	settings := lib.CorrjoinSettings{
		SvdOutputDimensions:  *svdDimensions,
		SvdDimensions:        *ks,
		EuclidDimensions:     *ke,
		CorrelationThreshold: float64(*correlationThreshold) / 100.0,
		WindowSize:           *windowSize,
		Algorithm:            lib.ALGO_PAA_SVD,
	}
	if *full {
		settings.Algorithm = lib.ALGO_FULL_PEARSON
	}

	shiftCount := 0

	window := lib.NewTimeseriesWindow(*windowSize)

	results := make(chan *buckets.CorrjoinResult, 1)
	defer close(results)

	result_ctr := 0
	go func() {
		log.Println("waiting for results")
		for {
			select {
			case correlationResult := <-results:
				if len(correlationResult.CorrelatedPairs) == 0 {
					log.Printf("total %d results for stride %d\n",
						result_ctr,
						correlationResult.StrideCounter)
					result_ctr = 0
					continue
				}
				result_ctr += len(correlationResult.CorrelatedPairs)
			}
		}
	}()

	// The data in the sample files is in column-major order, so the nth line of a file
	// is the data at time n, and each line contains an entry for each time series.
	// All the processing uses the transpose of that matrix, so each row corresponds to
	// a timeseries and each column to a point in time.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		} // err is usually io.EOF
		if len(line) == 0 {
			break
		}

		line = strings.TrimSuffix(line, "\n")
		// This is the current line number, and this is how we count how many columns the resulting matrix will have.

		// parse floats out of line with strconv
		parts := strings.Split(line, " ")
		if lineCount == 0 {
			// This is the number of columns in the input file, and it'll end up being the number
			// of rows in the result matrix.
			lineCount = len(parts)
			data = make([][]float64, lineCount, lineCount)
		} else {
			if lineCount != len(parts) {
				panic(fmt.Errorf("inconsistent number of values in line %d: expected %d but got %d",
					columnCount, lineCount, len(parts)))
			}
		}
		for i, p := range parts {
			if len(data[i]) == 0 {
				data[i] = make([]float64, *stride, *stride)
			}
			data[i][columnCount], err = strconv.ParseFloat(p, 64)
			if err != nil {
				panic(fmt.Errorf("on line %d of %s, failed to parse %s into a float: %v\n",
					columnCount, *filename, p, err))
			}
		}
		columnCount++
		if columnCount >= *stride {
			// This triggers computation once the window is full.
			// You can capture and print the results here but it'll make the system appear slow, so I don't
			// do it by default.
			err = window.ShiftBuffer(data, settings, results)
			if err != nil {
				log.Printf("caught error: %v\n", err)
				break
			}
			columnCount = 0
			data = make([][]float64, lineCount, lineCount)
			shiftCount++
		}

	}
}
