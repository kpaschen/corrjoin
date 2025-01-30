package lib

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/buckets"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/correlation"
	"github.com/kpaschen/corrjoin/lib/paa"
	"github.com/kpaschen/corrjoin/lib/settings"
	"github.com/kpaschen/corrjoin/lib/svd"
	"github.com/kpaschen/corrjoin/lib/utils"
	"gonum.org/v1/gonum/mat"
	"log"
	"slices"
)

// A TimeseriesWindow is a sliding window over a list of timeseries.
type TimeseriesWindow struct {

	// This is the raw data.
	// Every row corresponds to a timeseries.
	// Rows can be empty if they correspond to a timeseries that we haven't
	// received data for in a while.
	// TODO: compact these rows from time to time.
	buffers [][]float64

	// TODO: maintain the buffers normalized by keeping track of the
	// normalization factor and the avg of the previous stride
	normalized [][]float64

	ConstantRows []bool

	postPAA [][]float64

	postSVD [][]float64

	settings settings.CorrjoinSettings
	comparer comparisons.Engine

	windowSize    int
	StrideCounter int
}

func NewTimeseriesWindow(settings settings.CorrjoinSettings, comparer comparisons.Engine) *TimeseriesWindow {
	return &TimeseriesWindow{settings: settings, StrideCounter: 0, comparer: comparer}
}

func (w *TimeseriesWindow) shiftBufferIntoWindow(buffer [][]float64) (bool, error) {
	// Weird but ok?
	if len(buffer) == 0 {
		return false, nil
	}
	currentRowCount := len(w.buffers)
	newColumnCount := len(buffer[0])
	newRowCount := len(buffer)

	if newRowCount < currentRowCount {
		return false, fmt.Errorf(
			"new buffer has wrong row count (%d, needed %d)", newRowCount, currentRowCount)
	}

	for i, buf := range buffer {
		if len(buf) != newColumnCount {
			return false, fmt.Errorf("before copy: bad column count %d in row %d (expected %d)", len(buf), i, newColumnCount)
		}
	}

	// This is a new ts window receiving its first stride.
	if currentRowCount == 0 {
		// Stride should really always be < windowsize, but check here to be sure.
		if newColumnCount <= w.settings.WindowSize {
			w.buffers = buffer
			return newColumnCount == w.settings.WindowSize, nil
		} else {
			return false, fmt.Errorf("received buffer with width %d greater than window size %d",
				newColumnCount, w.settings.WindowSize)
		}
	}

	oldColumnCount := len(w.buffers[0])
	sliceLengthToDelete := oldColumnCount + newColumnCount - w.settings.WindowSize
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
	expected := len(w.buffers[0])
	for i, buf := range w.buffers {
		if len(buf) != expected {
			return false, fmt.Errorf("before extend: bad column count %d in row %d (expected %d)", len(buf), i, expected)
		}
	}
	currentRowCount = len(w.buffers)
	if newRowCount > currentRowCount {
		for i := currentRowCount; i < newRowCount; i++ {
			row := make([]float64, w.settings.WindowSize-newColumnCount, w.settings.WindowSize)
			row = append(row, buffer[i]...)
			if len(row) != expected {
				return false, fmt.Errorf("during extend: bad column count %d in row %d (expected %d)", len(row), i, expected)
			}
			w.buffers = append(w.buffers, row)
		}
	}
	return sliceLengthToDelete > 0, nil
}

// shift _buffer_ into _w_ from the right, displacing the first buffer.width columns
// of w.
// Returns true if computation was performed, false if there was nothing to do.
func (w *TimeseriesWindow) ShiftBuffer(buffer [][]float64) (error, bool) {

	// TODO: make an error type so I can signal whether the window
	// is busy vs. a different kind of error.
	// Then the caller can decide what to do.

	newStride, err := w.shiftBufferIntoWindow(buffer)
	if err != nil {
		return err, false
	}
	if !newStride {
		return nil, false
	}

	w.StrideCounter++

	w.normalizeWindow()
	log.Printf("starting a run of %v on %d rows\n", w.settings.Algorithm, len(w.normalized))
	err = w.comparer.StartStride(w.normalized, w.ConstantRows, w.StrideCounter)
	if err != nil {
		// This could get a 'repeated start stride'
		log.Printf("failed to start stride %d: %v", w.StrideCounter, err)
	}

	switch w.settings.Algorithm {
	case settings.ALGO_FULL_PEARSON:
		err = w.fullPearson()
	case settings.ALGO_PAA_ONLY:
		err = w.pAAOnly()
	case settings.ALGO_PAA_SVD:
		err = w.processBuffer()
	case settings.ALGO_NONE: // No-op
	default:
		err = fmt.Errorf("unsupported algorithm choice %s", w.settings.Algorithm)
	}
	if err != nil {
		return err, true
	}

	return nil, true
}

// This could be computed incrementally.
func (w *TimeseriesWindow) normalizeWindow() *TimeseriesWindow {
	log.Printf("start normalizing window\n")
	if len(w.normalized) < len(w.buffers) {
		w.normalized = slices.Grow(w.normalized, len(w.buffers)-len(w.normalized))
	}
	if len(w.ConstantRows) < len(w.buffers) {
		w.ConstantRows = slices.Grow(w.ConstantRows, len(w.buffers)-len(w.ConstantRows))
	}
	constantRowCounter := 0
	for i, b := range w.buffers {
		if i >= len(w.normalized) {
			w.normalized = append(w.normalized, slices.Clone(b))
		} else {
			w.normalized[i] = slices.Clone(b)
		}
		if i >= len(w.ConstantRows) {
			w.ConstantRows = append(w.ConstantRows, paa.NormalizeSlice(w.normalized[i]))
		} else {
			w.ConstantRows[i] = paa.NormalizeSlice(w.normalized[i])
		}
		if w.ConstantRows[i] {
			constantRowCounter++
		}
	}
	log.Printf("done normalizing window. found %d constant rows\n", constantRowCounter)
	return w
}

// This could be computed incrementally after shiftBuffer.
func (w *TimeseriesWindow) pAA() *TimeseriesWindow {
	log.Printf("start paa\n")
	if len(w.postPAA) < len(w.normalized) {
		w.postPAA = slices.Grow(w.postPAA, len(w.normalized)-len(w.postPAA))
	}
	constantCounter := 0
	for i, b := range w.normalized {
		// TODO: skip constantRows during PAA?
		paaResults, constant := paa.PAA(b, w.settings.SvdDimensions)
		if constant && !w.ConstantRows[i] {
			constantCounter++
		}
		if i >= len(w.postPAA) {
			w.postPAA = append(w.postPAA, paaResults)
		} else {
			w.postPAA[i] = paaResults
		}
	}
	log.Printf("done with PAA, had %d additional constant rows\n", constantCounter)
	return w
}

func (w *TimeseriesWindow) correlationPairs() error {
	r := len(w.postSVD)
	if r == 0 {
		return fmt.Errorf("you must run SVD before you can get correlation pairs")
	}

	scheme := buckets.NewBucketingScheme(w.normalized, w.postSVD, w.ConstantRows,
		w.settings, w.StrideCounter, w.comparer)
	utils.ReportMemory("created scheme")
	err := scheme.Initialize()
	utils.ReportMemory("initialized scheme")
	if err != nil {
		return err
	}
	err = scheme.CorrelationCandidates()
	utils.ReportMemory("asked for candidates")
	if err != nil {
		return err
	}

	return nil
}

func (w *TimeseriesWindow) sVD() (*TimeseriesWindow, error) {
	log.Println("start svd")
	rowCount := len(w.postPAA)
	if rowCount == 0 {
		return nil, fmt.Errorf("the postPAA field needs to be filled before you call SVD")
	}
	columnCount := len(w.postPAA[0])

	svd := &svd.TruncatedSVD{K: w.settings.SvdOutputDimensions}

	fullData := make([]float64, 0, rowCount*columnCount)
	svdData := make([]float64, 0, w.settings.MaxRowsForSvd*columnCount)
	// I ran into failures (with no error messages) for svd on large
	// inputs (over 40k rows with 600 columns). It looks like some implementations
	// struggle at that size.
	// The following samples the data so we stay below maxRowsForSvd rows.
	svdRowCount := 0
	var modulus int
	if w.settings.MaxRowsForSvd == 0 || w.settings.MaxRowsForSvd >= rowCount {
		log.Printf("will attempt to compute svd on full %d rows", rowCount)
		modulus = 1
	} else {
		log.Printf("reducing matrix from %d to %d rows for svd", rowCount, w.settings.MaxRowsForSvd)
		modulus = rowCount / w.settings.MaxRowsForSvd
	}
	for i, r := range w.postPAA {
		fullData = append(fullData, r...)
		if svdRowCount < w.settings.MaxRowsForSvd && (len(w.ConstantRows) <= i || !w.ConstantRows[i]) {
			// TODO: check if r is constant
			if i%modulus == 0 {
				svdData = append(svdData, r...)
				svdRowCount++
			}
		}
	}

	fullMatrix := mat.NewDense(rowCount, columnCount, fullData)
	svdMatrix := mat.NewDense(svdRowCount, columnCount, svdData)

	log.Printf("computing svd using a matrix with %d columns and %d rows\n", columnCount, svdRowCount)

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

func (w *TimeseriesWindow) fullPearson() error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run FullPearson on")
	}
	rc := len(w.normalized)
	for i := 0; i < rc; i++ {
		for j := i + 1; j < rc; j++ {
			err := w.comparer.Compare(i, j)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *TimeseriesWindow) pAAOnly() error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run PAA on")
	}

	w.pAA()
	r = len(w.postPAA)
	for i := 0; i < r; i++ {
		r1 := w.postPAA[i]
		for j := i + 1; j < r; j++ {
			r2 := w.postPAA[j]
			distance, err := correlation.EuclideanDistance(r1, r2)
			if err != nil {
				return err
			}
			if distance > w.settings.Epsilon1 {
				continue
			}
			err = w.comparer.Compare(i, j)
			if err != nil {
				return err
			}

		}
	}
	return nil
}

func (w *TimeseriesWindow) processBuffer() error {
	utils.ReportMemory("start processBuffers")
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to process")
	}

	utils.ReportMemory("start paa")
	w.pAA()

	// Apply SVD
	utils.ReportMemory("start svd")
	_, err := w.sVD()
	if err != nil {
		log.Printf("svd failed for input %+v\n", w.postPAA)
		return err
	}
	// CorrelationBuckets outputs a set of row index pairs.
	utils.ReportMemory("start correlationPairs")
	err = w.correlationPairs()
	if err != nil {
		log.Printf("failed to find correlation pairs: %v", err)
		return err
	}

	utils.ReportMemory("return from processBuffers")
	return nil
}
