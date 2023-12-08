package lib

import (
   "fmt"
)

// From these we can compute:
// epsilon1 := sqrt(2 * ks * (t - T)/n)
// epsilon2 := sqrt(2 * ke * (1 - T)/n)
// The stride length is implicit in the width of the buffer.
type TimeseriesProcessor struct {
   // The number of dimensions used for SVD (aka ks)
   svdDimensions int
   // The number of dimensions to choose from the output of SVD (aka kb)
   svdOutputDimensions int
   // The number of dimensions used for euclidean distance (aka ke)
   euclidDimensions int
   correlationThreshold float32  // aka T
   windowSize int // aka n
}

func (w *TimeseriesWindow) processBuffer(processor TimeseriesProcessor,
    buffer TimeseriesWindow) ([]CorrelationResult, error) {

   ret := make([]CorrelationResult, 0, 0)
   // Shift _buffer_ into _window_. The stride length is equal to the
   // width of _buffer_. 
   err := w.shiftBuffer(buffer)
   if err != nil {
      return ret, err
   }

   // Normalize _window_
   w.normalizeWindow()

   // Reduce dimensions of w to processor.svdDimensions
   windowForSvd := w.PAA(processor.svdDimensions)

   // Apply SVD
   windowForBucketing, err := windowForSvd.SVD(processor.svdOutputDimensions)
   if err != nil {
      return ret, err
   }
   fmt.Printf("window for bucketing: %+v\n", windowForBucketing.data)

   // BucketingFilter outputs a set of CorrelationResult
   // C1 := BucketingFilter(WindowForBucketing, kb, epsilon1)
   // C2 := []

   // go over all pairs in C1 and add those that pass the euclidean distance filter
   // to C2
 
   // go over all pairs in C2 and add those that are above the correlation threshold 
   // to the result
   return ret, nil
}
