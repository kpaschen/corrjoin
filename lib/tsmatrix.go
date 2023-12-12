package lib

import (
   "fmt"
   "gonum.org/v1/gonum/mat"
   "corrjoin/lib/buckets"
   "corrjoin/lib/paa"
   "corrjoin/lib/svd"
)

// A TimeseriesWindow is a sliding window over a list of timeseries.
// TODO: make this keep track of the number of shifts that have happend, or 
// some kind of absolute timestamp.
type TimeseriesWindow struct {
   data *mat.Dense
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
   return &TimeseriesWindow{ data: newMatrix }
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
   return &TimeseriesWindow{ data: ret }, nil
}
