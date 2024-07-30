package lib

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/buckets"
	"github.com/kpaschen/corrjoin/lib/correlation"
	"github.com/kpaschen/corrjoin/lib/paa"
	"github.com/kpaschen/corrjoin/lib/svd"
	"gonum.org/v1/gonum/mat"
	"math"
)

// A TimeseriesWindow is a sliding window over a list of timeseries.
// TODO: make this keep track of the number of shifts that have happend, or
// some kind of absolute timestamp.
type TimeseriesWindow struct {
	data *mat.Dense
}

func NewTimeseriesWindow(data *mat.Dense) *TimeseriesWindow {
	return &TimeseriesWindow{data: data}
}

// shift _buffer_ into _w_ from the right, displacing the first buffer.width columns
// of w.
func (w *TimeseriesWindow) shiftBuffer(buffer TimeseriesWindow) error {
	rowCount, columnCount := w.data.Dims()
	bufferRows, bufferColumns := buffer.data.Dims()
	if rowCount != bufferRows {
		return fmt.Errorf("mismatched row count %d vs. %d in shiftBuffer", rowCount, bufferRows)
	}
	if bufferColumns > columnCount {
		return fmt.Errorf("stride length %d is greater than window size %d", bufferColumns, columnCount)
	}
	if columnCount%bufferColumns != 0 {
		return fmt.Errorf("stride length %d should be a divisor of window size %d", bufferColumns, columnCount)
	}

	// Should probably experiment with different implementations here.
	// The code below is implementable using the existing mat.Dense type, which is nice,
	// but it might not be memory-access-friendly especially for large matrices.

	// Remove the first /bufferColumns/ columns from w.
	wWithoutBuffer := w.data.Slice(0, rowCount, bufferColumns, columnCount)

	// Append buffer to the reduced slice.
	// This performs an unnecessary copy of wWithoutBuffer
	w.data.Augment(wWithoutBuffer, buffer.data)

	return nil
}

// This copies the original matrix instead of modifying it in place because
// we will need a part of the original matrix when computing the normalizations
// for the subsequent shifts.
func (w *TimeseriesWindow) normalizeWindow() *TimeseriesWindow {
	newMatrix := mat.DenseCopyOf(w.data)
	rowCount, _ := newMatrix.Dims()
	for i := 0; i < rowCount; i++ {
		paa.NormalizeSlice(newMatrix.RawRowView(i))
	}
	return &TimeseriesWindow{data: newMatrix}
}

func (w *TimeseriesWindow) PAA(targetColumnCount int) *TimeseriesWindow {
	rowCount, _ := w.data.Dims()
	result := mat.NewDense(rowCount, targetColumnCount, nil)
	for i := 0; i < rowCount; i++ {
		row := paa.PAA(w.data.RawRowView(i), targetColumnCount)
		result.SetRow(i, row)
	}
	return &TimeseriesWindow{
		data: result,
	}
}

func (w *TimeseriesWindow) AllPairs(matrix *mat.Dense, correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {
	ret := map[buckets.RowPair]float64{}
	pearsonFilter := 0

	rc, _ := matrix.Dims()
	for i := 0; i < rc; i++ {
		r1 := matrix.RawRowView(i)
		for j := i + 1; j < rc; j++ {
			r2 := matrix.RawRowView(j)
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
	fmt.Printf("full pairs: rejected %d pairs using pearson threshold\n", pearsonFilter)
	return ret, nil
}

func (w *TimeseriesWindow) PAAPairs(originalMatrix *mat.Dense, correlationThreshold float64) (
	map[buckets.RowPair]float64, error) {

	ret := map[buckets.RowPair]float64{}
	paaFilter := 0
	fp := 0
	fn := 0

	r, c := w.data.Dims()
	_, originalN := originalMatrix.Dims()

	fmt.Printf("post-paa matrix has %d columns, original has %d\n", c, originalN)

	epsilon := math.Sqrt(float64(2*c) * (1.0 - correlationThreshold) / float64(originalN))
	fmt.Printf("epsilon is %f\n", epsilon)

	// The smallest fp is the one closest to the tp line
	smallestFp := float64(10.0)
	// The largest tp is closest to the tp line on the other side
	largestTp := float64(0.0)

	lineCount := r
	for i := 0; i < lineCount; i++ {
		r1 := w.data.RawRowView(i)
		s1 := originalMatrix.RawRowView(i)
		for j := i + 1; j < lineCount; j++ {
			r2 := w.data.RawRowView(j)
			distance, err := correlation.EuclideanDistance(r1, r2)
			if err != nil {
				return nil, err
			}
			if distance > epsilon {
				paaFilter++
			}
			s2 := originalMatrix.RawRowView(j)
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
					fmt.Printf("false negative: euclidean distance between rows %d and %d is %f > %f but correlation coefficient is %f\n", i, j, distance, epsilon, pearson)
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
	fmt.Printf("rejected %d pairs using paa filter, returning %d\n", paaFilter, len(ret))
	// False negative means paa would have rejected a correlated pair, those
	// are the bad ones.
	// A high false positive count just means the filter is ineffective.
	fmt.Printf("false positive count: %d, false negatives: %d\n", fp, fn)
	fmt.Printf("largest tp: %f, smallest fp: %f\n", largestTp, smallestFp)
	return ret, nil
}

// CorrelationPairs returns a list of pairs of row indices
// and their pearson correlations.
func (w *TimeseriesWindow) CorrelationPairs(originalMatrix *mat.Dense,
	ks int, ke int, correlationThreshold float64) (map[buckets.RowPair]float64, error) {
	scheme := buckets.NewBucketingScheme(originalMatrix, w.data, ks, ke, correlationThreshold)
	err := scheme.Initialize()
	if err != nil {
		return nil, err
	}
	pairs, err := scheme.CorrelationCandidates()
	if err != nil {
		return nil, err
	}
	stats := scheme.Stats()
	fmt.Printf("correlation pair statistics:\npairs compared in r1: %d\npairs rejected by r1: %d\npairs compared in r2: %d\npairs rejected by r2: %d\npairs compared using pearson: %d\npairs rejected by pearson: %d\n",
		stats[0], stats[1], stats[2], stats[3], stats[4], stats[5])

	r, _ := originalMatrix.Dims()
	totalRows := float32(r * r / 2)
	fmt.Printf("r1 pruning rate: %f\nr2 pruning rate: %f\n", float32(stats[1])/totalRows,
		float32(stats[3])/totalRows)
	fmt.Printf("there were %f total comparisons and after bucketing we did %d or %f\n",
		totalRows, stats[0], float32(stats[0])/totalRows)
	return pairs, nil
}

func (w *TimeseriesWindow) SVD(k int) (*TimeseriesWindow, error) {
	svd := &svd.TruncatedSVD{K: k}
	// TODO: instead of FitTransform, call Fit() here and save that matrix
	// so it can be reused by later Transform() calls.
	// nlp has Save() and Load() for that purpose.
	ret, err := svd.FitTransform(w.data)
	if err != nil {
		return nil, err
	}
	return &TimeseriesWindow{data: ret}, nil
}
