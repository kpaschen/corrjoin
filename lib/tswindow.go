package lib

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/buckets"
	"github.com/kpaschen/corrjoin/lib/correlation"
	"github.com/kpaschen/corrjoin/lib/paa"
	"github.com/kpaschen/corrjoin/lib/svd"
	"gonum.org/v1/gonum/mat"
	"log"
	"math"
	"runtime"
	"slices"
)

type CorrjoinResult struct {
	CorrelatedPairs map[buckets.RowPair]float64
	ConstantRows    []bool
}

// A TimeseriesWindow is a sliding window over a list of timeseries.
type TimeseriesWindow struct {

	// This is the raw data.
	// Every row corresponds to a timeseries.
	// Rows can be empty if they correspond to a timeseries that we haven't
	// received data for in a while.
	// TODO: figure out whether to remove those rows before computation or fill
	// them with zeroes.
	buffers [][]float64

	// TODO: maintain the buffers normalized by keeping track of the
	// normalization factor and the avg of the previous stride
	normalized [][]float64

	constantRows []bool

	postPAA [][]float64

	postSVD [][]float64

	maxRowsForSvd int

	windowSize int
}

func NewTimeseriesWindow(windowSize int) *TimeseriesWindow {
	return &TimeseriesWindow{windowSize: windowSize, maxRowsForSvd: 10000}
}

// shift _buffer_ into _w_ from the right, displacing the first buffer.width columns
// of w.
func (w *TimeseriesWindow) ShiftBuffer(buffer [][]float64, settings CorrjoinSettings) (*CorrjoinResult, error) {

	// Weird but ok?
	if len(buffer) == 0 {
		return nil, nil
	}

	// TODO: make an error type so I can signal whether the window
	// is busy vs. a different kind of error.
	// Then the caller can decide what to do.

	currentRowCount := len(w.buffers)
	newColumnCount := len(buffer[0])
	newRowCount := len(buffer)

	if newRowCount < currentRowCount {
		return nil, fmt.Errorf("new buffer has wrong row count (%d, needed %d)", newRowCount, currentRowCount)
	}

	// This is a new ts window receiving its first stride.
	if currentRowCount == 0 {
		// Stride should really always be < windowsize, but check here to be sure.
		if newColumnCount < w.windowSize {
			w.buffers = buffer
			return nil, nil
		} else {
			w.buffers = make([][]float64, newRowCount)
		}
	}

	currentColumnCount := len(w.buffers[0])

	sliceLengthToDelete := currentColumnCount + newColumnCount - w.windowSize
	if sliceLengthToDelete < 0 {
		sliceLengthToDelete = 0
	}
	log.Printf("shifting %d columns into window; shifting %d columns out of window\n",
		newColumnCount, sliceLengthToDelete)

	// Append new data.
	for i, buf := range w.buffers {
		// This would leak memory if w.buffers[i] held anything other than a basic data type.
		w.buffers[i] = append(buf[sliceLengthToDelete:], buffer[i]...)
	}
	currentRowCount = len(w.buffers)
	if newRowCount > currentRowCount {
		for i := currentRowCount; i < newRowCount; i++ {
			row := make([]float64, currentColumnCount-sliceLengthToDelete, currentColumnCount-sliceLengthToDelete)
			row = append(row, buffer[i]...)
			w.buffers = append(w.buffers, row)
		}
	}

	var err error
	var res *CorrjoinResult
	res = nil
	if sliceLengthToDelete > 0 {
		log.Printf("starting a run of %v on %d rows\n", settings.Algorithm, newRowCount)
		switch settings.Algorithm {
		case ALGO_FULL_PEARSON:
			res, err = w.fullPearson(settings)
		case ALGO_PAA_ONLY:
			res, err = w.pAAOnly(settings)
		case ALGO_PAA_SVD:
			res, err = w.processBuffer(settings)
		case ALGO_NONE: // No-op
		default:
			err = fmt.Errorf("unsupported algorithm choice %s", settings.Algorithm)
		}
		if err != nil {
			return nil, err
		}
	}
	if res != nil {
		res.ConstantRows = w.constantRows
	}

	return res, nil
}

// This could be computed incrementally after shiftBuffer.
func (w *TimeseriesWindow) normalizeWindow() *TimeseriesWindow {
	log.Printf("start normalizing window\n")
	if len(w.normalized) < len(w.buffers) {
		w.normalized = slices.Grow(w.normalized, len(w.buffers)-len(w.normalized))
	}
	if len(w.constantRows) < len(w.buffers) {
		w.constantRows = slices.Grow(w.constantRows, len(w.buffers)-len(w.constantRows))
	}
	constantRowCounter := 0
	for i, b := range w.buffers {
		if i >= len(w.normalized) {
			w.normalized = append(w.normalized, slices.Clone(b))
		} else {
			w.normalized[i] = slices.Clone(b)
		}
		if i >= len(w.constantRows) {
			w.constantRows = append(w.constantRows, paa.NormalizeSlice(w.normalized[i]))
		} else {
			w.constantRows[i] = paa.NormalizeSlice(w.normalized[i])
		}
		if w.constantRows[i] {
			constantRowCounter++
		}
	}
	log.Printf("done normalizing window. found %d constant rows\n", constantRowCounter)
	return w
}

// This could be computed incrementally after shiftBuffer.
func (w *TimeseriesWindow) pAA(targetColumnCount int) *TimeseriesWindow {
	log.Printf("start paa\n")
	if len(w.postPAA) < len(w.buffers) {
		w.postPAA = slices.Grow(w.postPAA, len(w.buffers)-len(w.postPAA))
	}
	if len(w.normalized) < len(w.buffers) {
		w.normalizeWindow()
	}
	// TODO: skip constantRows during PAA
	for i, b := range w.normalized {
		if i >= len(w.postPAA) {
			w.postPAA = append(w.postPAA, paa.PAA(b, targetColumnCount))
		} else {
			w.postPAA[i] = paa.PAA(b, targetColumnCount)
		}
	}
	log.Printf("done with PAA\n")
	return w
}

func (w *TimeseriesWindow) allPairs(correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {

	log.Println("start allPairs")

	ret := map[buckets.RowPair]float64{}
	pearsonFilter := 0

	w.normalizeWindow()

	ctr := 0
	rc := len(w.normalized)
	for i := 0; i < rc; i++ {
		r1 := w.normalized[i]
		for j := i + 1; j < rc; j++ {
			r2 := w.normalized[j]
			pearson, err := correlation.PearsonCorrelation(r1, r2)
			if err != nil {
				return nil, err
			}
			ctr++
			if pearson >= correlationThreshold {
				pair := buckets.NewRowPair(i, j)
				ret[*pair] = pearson
			} else {
				pearsonFilter++
			}
		}
	}
	log.Printf("full pairs: rejected %d out of %d pairs using pearson threshold, found %d\n", pearsonFilter, ctr, len(ret))
	return ret, nil
}

func (w *TimeseriesWindow) pAAPairs(paa int, correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {

	log.Println("start paaPairs")
	ret := map[buckets.RowPair]float64{}
	w.normalizeWindow()
	w.pAA(paa)
	paaFilter := 0
	fp := 0
	fn := 0

	r := len(w.postPAA)
	c := len(w.postPAA[0])
	originalColumns := len(w.buffers[0])

	epsilon := math.Sqrt(float64(2*c) * (1.0 - correlationThreshold) / float64(originalColumns))
	log.Printf("epsilon is %f\n", epsilon)

	// The smallest fp is the one closest to the tp line
	smallestFp := float64(10.0)
	// The largest tp is closest to the tp line on the other side
	largestTp := float64(0.0)

	lineCount := r
	for i := 0; i < r; i++ {
		r1 := w.postPAA[i]
		s1 := w.normalized[i]
		for j := i + 1; j < lineCount; j++ {
			r2 := w.postPAA[j]
			distance, err := correlation.EuclideanDistance(r1, r2)
			if err != nil {
				return nil, err
			}
			if distance > epsilon {
				paaFilter++
			}
			s2 := w.normalized[j]
			pearson, err := correlation.PearsonCorrelation(s1, s2)
			if err != nil {
				return nil, err
			}

			// pearson > T and distance <= epsilon: true positive
			// pearson > T and distance > epsilon: false negative
			// pearson <= T and distance <= epsilon: false positive
			// pearson <= T and distance > epsilon: true negative
			if pearson >= correlationThreshold {
				pair := buckets.NewRowPair(i, j)
				ret[*pair] = pearson

				if distance > epsilon { // false negative
					log.Printf("false negative: euclidean distance between rows %d and %d is %f > %f but correlation coefficient is %f\n", i, j, distance, epsilon, pearson)
					fn++
				} else { // true positive
					if distance > largestTp {
						largestTp = distance
					}
				}
			} else {
				if distance <= epsilon {
					if distance < smallestFp {
						smallestFp = distance
					}
					fp++
				}
			}
		}
	}
	log.Printf("rejected %d pairs using paa filter, returning %d\n", paaFilter, len(ret))
	// False negative means paa would have rejected a correlated pair, those
	// are the bad ones.
	// A high false positive count just means the filter is ineffective.
	log.Printf("false positive count: %d, false negatives: %d\n", fp, fn)
	log.Printf("largest tp: %f, smallest fp: %f\n", largestTp, smallestFp)
	return ret, nil
}

// correlationPairs returns a list of pairs of row indices
// and their pearson correlations.
func (w *TimeseriesWindow) correlationPairs(ks int, ke int, correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {
	log.Println("start correlationPairs")
	r := len(w.postSVD)
	if r == 0 {
		return nil, fmt.Errorf("you must run SVD before you can get correlation pairs")
	}

	scheme := buckets.NewBucketingScheme(w.normalized, w.postSVD, w.constantRows,
		ks, ke, correlationThreshold)
	reportMemory("created scheme")
	err := scheme.Initialize()
	reportMemory("initialized scheme")
	if err != nil {
		return nil, err
	}
	pairs, err := scheme.CorrelationCandidates()
	reportMemory("got candidates")
	if err != nil {
		return nil, err
	}
	stats := scheme.Stats()
	log.Printf("correlation pair statistics:\npairs compared in r1: %d\npairs rejected by r1: %d\npairs compared in r2: %d\npairs rejected by r2: %d\npairs compared using pearson: %d\npairs rejected by pearson: %d, constant ts dropped: %d\n",
		stats[0], stats[1], stats[2], stats[3], stats[4], stats[5], stats[6])

	totalRows := float32(r * r / 2)
	log.Printf("r1 pruning rate: %f\nr2 pruning rate: %f\n", float32(stats[1])/totalRows,
		float32(stats[3])/totalRows)
	log.Printf("there were %f total comparisons and after bucketing we did %d or %f\n",
		totalRows, stats[0], float32(stats[0])/totalRows)
	return pairs, nil
}

func (w *TimeseriesWindow) sVD(k int) (*TimeseriesWindow, error) {
	log.Println("start svd")
	rowCount := len(w.postPAA)
	if rowCount == 0 {
		return nil, fmt.Errorf("the postPAA field needs to be filled before you call SVD")
	}
	columnCount := len(w.postPAA[0])

	svd := &svd.TruncatedSVD{K: k}

	fullData := make([]float64, 0, rowCount*columnCount)
	svdData := make([]float64, 0, w.maxRowsForSvd*columnCount)
	// I ran into failures (with no error messages) for svd on large
	// inputs (over 40k rows with 600 columns). It looks like some implementations
	// struggle at that size.
	// The following samples the data so we stay below maxRowsForSvd rows.
	svdRowCount := 0
	var modulus int
	if w.maxRowsForSvd == 0 || w.maxRowsForSvd >= rowCount {
		log.Printf("will attempt to compute svd on full %d rows", rowCount)
		modulus = 1
	} else {
		log.Printf("reducing matrix from %d to %d rows for svd", rowCount, w.maxRowsForSvd)
		modulus = rowCount / w.maxRowsForSvd
	}
	for i, r := range w.postPAA {
		fullData = append(fullData, r...)
		if svdRowCount < w.maxRowsForSvd && (len(w.constantRows) <= i || !w.constantRows[i]) {
			if i%modulus == 0 {
				svdData = append(svdData, r...)
				svdRowCount++
			}
		}
	}

	fullMatrix := mat.NewDense(rowCount, columnCount, fullData)
	svdMatrix := mat.NewDense(svdRowCount, columnCount, svdData)

	ret, err := svd.FitTransform(svdMatrix, fullMatrix)
	if err != nil {
		return nil, err
	}
	if len(w.postSVD) < rowCount {
		w.postSVD = slices.Grow(w.postSVD, rowCount-len(w.postSVD))
	}
	for i := 0; i < rowCount; i++ {
		row := ret.RawRowView(i)
		if i < len(w.postSVD) {
			w.postSVD[i] = row
		} else {
			w.postSVD = append(w.postSVD, row)
		}
	}
	log.Println("done with svd")
	return w, nil
}

func (w *TimeseriesWindow) fullPearson(settings CorrjoinSettings) (*CorrjoinResult, error) {
	r := len(w.buffers)
	if r == 0 {
		return nil, fmt.Errorf("no data to run FullPearson on")
	}
	pairs, err := w.allPairs(settings.CorrelationThreshold)
	if err != nil {
		return nil, err
	}

	totalPairs := r * (r - 1) / 2

	log.Printf("FullPearson: found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	return &CorrjoinResult{CorrelatedPairs: pairs}, nil
}

func (w *TimeseriesWindow) pAAOnly(settings CorrjoinSettings) (*CorrjoinResult, error) {
	r := len(w.buffers)
	if r == 0 {
		return nil, fmt.Errorf("no data to run PAA on")
	}

	paaPairs, err := w.pAAPairs(settings.SvdDimensions, settings.CorrelationThreshold)
	if err != nil {
		return nil, err
	}

	totalPairs := r * (r - 1) / 2

	log.Printf("PAAOnly: found %d correlated pairs out of %d possible (%d)\n", len(paaPairs), totalPairs,
		100*len(paaPairs)/totalPairs)
	return &CorrjoinResult{CorrelatedPairs: paaPairs}, nil
}

// printMemUsage outputs the current, total and OS memory being used. As well as the number
// of garage collection cycles completed.
func printMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	log.Printf("Alloc = %v MiB", bToMb(m.Alloc))
	log.Printf("\tTotalAlloc = %v MiB", bToMb(m.TotalAlloc))
	log.Printf("\tSys = %v MiB", bToMb(m.Sys))
	log.Printf("\tNumGC = %v\n", m.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func reportMemory(message string) {
	log.Println(message)
	printMemUsage()
}

func (w *TimeseriesWindow) processBuffer(settings CorrjoinSettings) (*CorrjoinResult, error) {
	reportMemory("start processBuffers")
	r := len(w.buffers)
	if r == 0 {
		return nil, fmt.Errorf("no data to process")
	}

	reportMemory("start normalizeWindow")
	w.normalizeWindow()
	reportMemory("start paa")
	w.pAA(settings.SvdDimensions)

	// Apply SVD
	reportMemory("start svd")
	_, err := w.sVD(settings.SvdOutputDimensions)
	if err != nil {
		log.Printf("svd failed for input %+v\n", w.postPAA)
		return nil, err
	}
	// CorrelationBuckets outputs a set of row index pairs.
	reportMemory("start correlationPairs")
	pairs, err := w.correlationPairs(settings.SvdDimensions, settings.EuclidDimensions,
		settings.CorrelationThreshold)
	if err != nil {
		log.Printf("failed to find correlation pairs: %v", err)
		return nil, err
	}

	totalPairs := r * (r - 1) / 2

	log.Printf("ProcessBuffer: found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	reportMemory("return from processBuffers")
	return &CorrjoinResult{CorrelatedPairs: pairs}, nil
}
