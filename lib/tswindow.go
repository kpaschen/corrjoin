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

	constantRows []bool

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

// shift _buffer_ into _w_ from the right, displacing the first buffer.width columns
// of w.
func (w *TimeseriesWindow) ShiftBuffer(buffer [][]float64, results chan<- *comparisons.CorrjoinResult) error {

	// Weird but ok?
	if len(buffer) == 0 {
		return nil
	}

	// TODO: make an error type so I can signal whether the window
	// is busy vs. a different kind of error.
	// Then the caller can decide what to do.

	currentRowCount := len(w.buffers)
	newColumnCount := len(buffer[0])
	newRowCount := len(buffer)

	if newRowCount < currentRowCount {
		return fmt.Errorf("new buffer has wrong row count (%d, needed %d)", newRowCount, currentRowCount)
	}

	// This is a new ts window receiving its first stride.
	if currentRowCount == 0 {
		// Stride should really always be < windowsize, but check here to be sure.
		if newColumnCount < w.settings.WindowSize {
			w.buffers = buffer
			return nil
		} else {
			w.buffers = make([][]float64, newRowCount)
		}
	}

	currentColumnCount := len(w.buffers[0])

	sliceLengthToDelete := currentColumnCount + newColumnCount - w.settings.WindowSize
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
	w.StrideCounter++

	if sliceLengthToDelete > 0 {
		log.Printf("starting a run of %v on %d rows\n", w.settings.Algorithm, newRowCount)
		switch w.settings.Algorithm {
		case settings.ALGO_FULL_PEARSON:
			err = w.fullPearson(results)
		case settings.ALGO_PAA_ONLY:
			err = w.pAAOnly(results)
		case settings.ALGO_PAA_SVD:
			err = w.processBuffer(results)
		case settings.ALGO_NONE: // No-op
		default:
			err = fmt.Errorf("unsupported algorithm choice %s", w.settings.Algorithm)
		}
		if err != nil {
			return err
		}
	}

	return nil
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
	err := w.comparer.StartStride(w.normalized, w.constantRows, w.StrideCounter)
	if err != nil {
		// This could get a 'repeated start stride'
		log.Printf("failed to start stride %d: %v", w.StrideCounter, err)
	}
	return w
}

// This could be computed incrementally after shiftBuffer.
func (w *TimeseriesWindow) pAA() *TimeseriesWindow {
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
			w.postPAA = append(w.postPAA, paa.PAA(b, w.settings.SvdDimensions))
		} else {
			w.postPAA[i] = paa.PAA(b, w.settings.SvdDimensions)
		}
	}
	log.Printf("done with PAA\n")
	return w
}

func (w *TimeseriesWindow) allPairs(results chan<- *comparisons.CorrjoinResult) error {
	log.Println("start allPairs")

	ret := map[comparisons.RowPair]float64{}
	pearsonFilter := 0

	w.normalizeWindow()

	ctr := 0
	found := 0
	rc := len(w.normalized)
	for i := 0; i < rc; i++ {
		r1 := w.normalized[i]
		for j := i + 1; j < rc; j++ {
			r2 := w.normalized[j]
			pearson, err := correlation.PearsonCorrelation(r1, r2)
			if err != nil {
				return err
			}
			ctr++
			if pearson >= w.settings.CorrelationThreshold {
				pair := comparisons.NewRowPair(i, j)
				ret[*pair] = pearson
				if len(ret) >= 1000 {
					found += len(ret)
					results <- &comparisons.CorrjoinResult{CorrelatedPairs: ret,
						StrideCounter: w.StrideCounter}
					ret = make(map[comparisons.RowPair](float64))
				}
			} else {
				pearsonFilter++
			}
		}
	}
	if len(ret) > 0 {
		found += len(ret)
		results <- &comparisons.CorrjoinResult{CorrelatedPairs: ret,
			StrideCounter: w.StrideCounter}
	}

	log.Printf("full pairs for stride %d: rejected %d out of %d pairs using pearson threshold, found %d\n",
		w.StrideCounter, pearsonFilter, ctr, found)
	return nil
}

func (w *TimeseriesWindow) pAAPairs(results chan<- *comparisons.CorrjoinResult) error {
	log.Println("start paaPairs")
	ret := map[comparisons.RowPair]float64{}
	w.normalizeWindow()
	w.pAA()
	paaFilter := 0
	fp := 0
	fn := 0

	r := len(w.postPAA)

	log.Printf("epsilon is %f\n", w.settings.Epsilon1)

	// The smallest fp is the one closest to the tp line
	smallestFp := float64(10.0)
	// The largest tp is closest to the tp line on the other side
	largestTp := float64(0.0)

	counter := 0

	lineCount := r
	for i := 0; i < r; i++ {
		r1 := w.postPAA[i]
		s1 := w.normalized[i]
		for j := i + 1; j < lineCount; j++ {
			r2 := w.postPAA[j]
			distance, err := correlation.EuclideanDistance(r1, r2)
			if err != nil {
				return err
			}
			if distance > w.settings.Epsilon1 {
				paaFilter++
			}
			s2 := w.normalized[j]
			pearson, err := correlation.PearsonCorrelation(s1, s2)
			if err != nil {
				return err
			}

			// pearson > T and distance <= epsilon: true positive
			// pearson > T and distance > epsilon: false negative
			// pearson <= T and distance <= epsilon: false positive
			// pearson <= T and distance > epsilon: true negative
			if pearson >= w.settings.CorrelationThreshold {
				pair := comparisons.NewRowPair(i, j)
				ret[*pair] = pearson

				if distance > w.settings.Epsilon1 { // false negative
					log.Printf("false negative: euclidean distance between rows %d and %d is %f > %f but correlation coefficient is %f\n",
						i, j, distance, w.settings.Epsilon1, pearson)
					fn++
				} else { // true positive
					if distance > largestTp {
						largestTp = distance
					}
				}
				if len(ret) >= 1000 {
					counter += len(ret)
					results <- &comparisons.CorrjoinResult{CorrelatedPairs: ret,
						StrideCounter: w.StrideCounter}
					ret = make(map[comparisons.RowPair]float64)
				}
			} else {
				if distance <= w.settings.Epsilon1 {
					if distance < smallestFp {
						smallestFp = distance
					}
					fp++
				}
			}
		}
	}
	if len(ret) > 0 {
		counter += len(ret)
		results <- &comparisons.CorrjoinResult{CorrelatedPairs: ret,
			StrideCounter: w.StrideCounter}
	}
	log.Printf("stride %d: rejected %d pairs using paa filter, returning %d\n",
		w.StrideCounter, paaFilter, counter)
	// False negative means paa would have rejected a correlated pair, those
	// are the bad ones.
	// A high false positive count just means the filter is ineffective.
	log.Printf("false positive count: %d, false negatives: %d\n", fp, fn)
	log.Printf("largest tp: %f, smallest fp: %f\n", largestTp, smallestFp)
	return nil
}

func (w *TimeseriesWindow) correlationPairs(results chan<- *comparisons.CorrjoinResult) error {
	r := len(w.postSVD)
	if r == 0 {
		return fmt.Errorf("you must run SVD before you can get correlation pairs")
	}

	scheme := buckets.NewBucketingScheme(w.normalized, w.postSVD, w.constantRows,
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
		if svdRowCount < w.settings.MaxRowsForSvd && (len(w.constantRows) <= i || !w.constantRows[i]) {
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

func (w *TimeseriesWindow) fullPearson(results chan<- *comparisons.CorrjoinResult) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run FullPearson on")
	}
	err := w.allPairs(results)
	if err != nil {
		return err
	}

	return nil
}

func (w *TimeseriesWindow) pAAOnly(results chan<- *comparisons.CorrjoinResult) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run PAA on")
	}

	err := w.pAAPairs(results)
	if err != nil {
		return err
	}

	return nil
}

func (w *TimeseriesWindow) processBuffer(results chan<- *comparisons.CorrjoinResult) error {
	utils.ReportMemory("start processBuffers")
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to process")
	}

	utils.ReportMemory("start normalizeWindow")
	w.normalizeWindow()
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
	err = w.correlationPairs(results)
	if err != nil {
		log.Printf("failed to find correlation pairs: %v", err)
		return err
	}

	utils.ReportMemory("return from processBuffers")
	return nil
}
