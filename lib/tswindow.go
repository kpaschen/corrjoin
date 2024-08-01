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
	"slices"
)

// A TimeseriesWindow is a sliding window over a list of timeseries.
// TODO: make this keep track of the number of shifts that have happend, or
// some kind of absolute timestamp.
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

	postPAA [][]float64

	postSVD [][]float64

	windowSize int
}

func NewTimeseriesWindow(windowSize int) *TimeseriesWindow {
	return &TimeseriesWindow{windowSize: windowSize}
}

// shift _buffer_ into _w_ from the right, displacing the first buffer.width columns
// of w.
func (w *TimeseriesWindow) ShiftBuffer(buffer [][]float64, settings CorrjoinSettings) error {

	// Weird but ok?
	if len(buffer) == 0 {
		return nil
	}

	currentRowCount := len(w.buffers)
	newColumnCount := len(buffer[0])
	newRowCount := len(buffer)

	if currentRowCount > 0 && newRowCount != currentRowCount {
		return fmt.Errorf("new buffer has wrong row count (%d, needed %d)", newRowCount, currentRowCount)
	}

	// This is a new ts window receiving its first stride.
	if currentRowCount == 0 {
		// Stride should really always be < windowsize, but check here to be sure.
		if newColumnCount < w.windowSize {
			w.buffers = buffer
			return nil
		} else {
			w.buffers = make([][]float64, newRowCount)
		}
	}

	currentColumnCount := len(w.buffers[0])

	sliceLengthToDelete := currentColumnCount + newColumnCount - w.windowSize
	if sliceLengthToDelete < 0 {
		sliceLengthToDelete = 0
	}

	// Append new data.
	for i, buf := range w.buffers {
		// This would leak memory if w.buffers[i] held anything other than a basic data type.
		w.buffers[i] = append(buf[sliceLengthToDelete:], buffer[i]...)
	}

	var err error
	if sliceLengthToDelete > 0 {
		switch settings.Algorithm {
		case ALGO_FULL_PEARSON:
			err = w.fullPearson(settings)
		case ALGO_PAA_ONLY:
			err = w.pAAOnly(settings)
		case ALGO_PAA_SVD:
			err = w.processBuffer(settings)
		case ALGO_NONE: // No-op
		default:
			err = fmt.Errorf("unsupported algorithm choice %s", settings.Algorithm)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// This could be computed incrementally after shiftBuffer.
func (w *TimeseriesWindow) normalizeWindow() *TimeseriesWindow {
	if len(w.normalized) < len(w.buffers) {
		w.normalized = slices.Grow(w.normalized, len(w.buffers)-len(w.normalized))
	}
	for i, b := range w.buffers {
		if i >= len(w.normalized) {
			w.normalized = append(w.normalized, slices.Clone(b))
		} else {
			w.normalized[i] = slices.Clone(b)
		}
		paa.NormalizeSlice(w.normalized[i])
	}
	return w
}

// This could be computed incrementally after shiftBuffer.
func (w *TimeseriesWindow) pAA(targetColumnCount int) *TimeseriesWindow {
	if len(w.postPAA) < len(w.buffers) {
		w.postPAA = slices.Grow(w.postPAA, len(w.buffers)-len(w.postPAA))
	}
	if len(w.normalized) < len(w.buffers) {
		w.normalizeWindow()
	}
	for i, b := range w.normalized {
		if i >= len(w.postPAA) {
			w.postPAA = append(w.postPAA, paa.PAA(b, targetColumnCount))
		} else {
			w.postPAA[i] = paa.PAA(b, targetColumnCount)
		}
	}
	return w
}

func (w *TimeseriesWindow) allPairs(correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {

	ret := map[buckets.RowPair]float64{}
	pearsonFilter := 0

	w.normalizeWindow()

	rc := len(w.normalized)
	for i := 0; i < rc; i++ {
		r1 := w.normalized[i]
		for j := i + 1; j < rc; j++ {
			r2 := w.normalized[j]
			pearson, err := correlation.PearsonCorrelation(r1, r2)
			if err != nil {
				return nil, err
			}
			if pearson >= correlationThreshold {
				pair := buckets.NewRowPair(i, j)
				ret[*pair] = pearson
			} else {
				pearsonFilter++
			}
		}
	}
	log.Printf("full pairs: rejected %d pairs using pearson threshold\n", pearsonFilter)
	return ret, nil
}

func (w *TimeseriesWindow) pAAPairs(paa int, correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {

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

// CorrelationPairs returns a list of pairs of row indices
// and their pearson correlations.
func (w *TimeseriesWindow) correlationPairs(ks int, ke int, correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {
	r := len(w.postSVD)
	if r == 0 {
		return nil, fmt.Errorf("you must run SVD before you can get correlation pairs")
	}

	scheme := buckets.NewBucketingScheme(w.normalized, w.postSVD, ks, ke, correlationThreshold)
	err := scheme.Initialize()
	if err != nil {
		return nil, err
	}
	pairs, err := scheme.CorrelationCandidates()
	if err != nil {
		return nil, err
	}
	stats := scheme.Stats()
	log.Printf("correlation pair statistics:\npairs compared in r1: %d\npairs rejected by r1: %d\npairs compared in r2: %d\npairs rejected by r2: %d\npairs compared using pearson: %d\npairs rejected by pearson: %d\n",
		stats[0], stats[1], stats[2], stats[3], stats[4], stats[5])

	totalRows := float32(r * r / 2)
	log.Printf("r1 pruning rate: %f\nr2 pruning rate: %f\n", float32(stats[1])/totalRows,
		float32(stats[3])/totalRows)
	log.Printf("there were %f total comparisons and after bucketing we did %d or %f\n",
		totalRows, stats[0], float32(stats[0])/totalRows)
	return pairs, nil
}

func (w *TimeseriesWindow) sVD(k int) (*TimeseriesWindow, error) {
	rowCount := len(w.postPAA)
	if rowCount == 0 {
		return nil, fmt.Errorf("the postPAA field needs to be filled before you call SVD")
	}
	columnCount := len(w.postPAA[0])

	svd := &svd.TruncatedSVD{K: k}
	// TODO: instead of FitTransform, call Fit() here and save that matrix
	// so it can be reused by later Transform() calls.
	// nlp has Save() and Load() for that purpose.

	data := make([]float64, 0, rowCount*columnCount)
	for _, r := range w.postPAA {
		data = append(data, r...)
	}

	matrix := mat.NewDense(rowCount, columnCount, data)
	ret, err := svd.FitTransform(matrix)
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
	return w, nil
}

func (w *TimeseriesWindow) fullPearson(settings CorrjoinSettings) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run FullPearson on")
	}
	pairs, err := w.allPairs(settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	totalPairs := r * r / 2

	log.Printf("FullPearson: found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	return nil
}

func (w *TimeseriesWindow) pAAOnly(settings CorrjoinSettings) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run PAA on")
	}

	paaPairs, err := w.pAAPairs(settings.SvdDimensions, settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	totalPairs := r * r / 2

	log.Printf("PAAOnly: found %d correlated pairs out of %d possible (%d)\n", len(paaPairs), totalPairs,
		100*len(paaPairs)/totalPairs)
	return nil
}

func (w *TimeseriesWindow) processBuffer(settings CorrjoinSettings) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to process")
	}

	w.normalizeWindow()
	w.pAA(settings.SvdDimensions)

	// Apply SVD
	_, err := w.sVD(settings.SvdOutputDimensions)
	if err != nil {
		log.Printf("svd failed for input %+v\n", w.postPAA)
		return err
	}
	// CorrelationBuckets outputs a set of row index pairs.
	pairs, err := w.correlationPairs(settings.SvdDimensions, settings.EuclidDimensions,
		settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	totalPairs := r * r / 2

	log.Printf("ProcessBuffer: found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	return nil
}
