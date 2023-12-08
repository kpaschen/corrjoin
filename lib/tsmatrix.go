package lib

import (
   "fmt"
   "gonum.org/v1/gonum/mat"
   "corrjoin/lib/buckets"
   "corrjoin/lib/paa"
   "corrjoin/lib/svd"
)

type TimeseriesWindow struct {
   data *mat.Dense
}

type CorrelationResult struct {
   timeSeriesOne int
   timeSeriesTwo int
   pearson float32
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

func (w *TimeseriesWindow) normalizeWindow() {
   rowCount, _ := w.data.Dims()
   for i := 0; i < rowCount; i++ {
      paa.NormalizeSlice(w.data.RawRowView(i))
   }
}

func (w *TimeseriesWindow) PAA(targetColumnCount int) *TimeseriesWindow {
   rowCount, _ := w.data.Dims()
   result := mat.NewDense(rowCount, targetColumnCount, nil)
   for i := 0; i < rowCount; i++ {
      fmt.Printf("raw data in row %d: %+v\n", i, w.data.RawRowView(i))
      row := paa.PAA(w.data.RawRowView(i), targetColumnCount)
      fmt.Printf("reduced: %+v\n", row)
      result.SetRow(i, row)
   }
   return &TimeseriesWindow{
      data: result,
   }
}

func (w *TimeseriesWindow) BucketingFilter(dimensions int, theta float32, windowSize int) (
   []CorrelationResult, error) {
   scheme := buckets.NewBucketingScheme(dimensions, theta, windowSize) 
   rowCount, _ := w.data.Dims()
   // First fill the bucketing scheme.
   for i := 0; i < rowCount; i++ {
      coordinates, err := scheme.Assign(w.data.RawRowView(i))
      _ = coordinates
      if err != nil {
         return nil, err
      }
   }
   // Now retrieve correlation candidates from all the buckets.
   buckets := scheme.Buckets()
   for _, b := range buckets {
      candidates := scheme.CorrelationCandidates(b)
      fmt.Printf("correlation candidates in bucket %s and its neighbours: %v\n",
      b, candidates)
   }
   return nil, nil
}

func (w *TimeseriesWindow) FullSVD() *TimeseriesWindow {
   var svd mat.SVD
   ok := svd.Factorize(w.data, mat.SVDThinV)
   if !ok { return nil }
   var dst mat.Dense // This will hold V in matrix form
   svd.VTo(&dst)
   // values := svd.Values(nil)

   // The R code for the paper actually returns V * S where S is the 
   // diagonal matrix with the singular values, but it also doesn't appear
   // to sort the singular values by size first.

  // see also: https://stats.stackexchange.com/questions/79043/why-pca-of-data-by-means-of-svd-of-the-data
   // S := mat.NewDiagDense(len(values), values)
   // var ret mat.Dense
   // ret.Product(S, dst.T())

   // Just use v here without additional scaling by the singular values.

   // TODO: call Slice to truncate u and v
   _, c := dst.Dims()
   r, _ := w.data.Dims()
   result := mat.NewDense(c, r, nil)
   result.Product(w.data, &dst)
   return &TimeseriesWindow{ data: result }
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
