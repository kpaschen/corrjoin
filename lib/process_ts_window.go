package lib

import (
   "fmt"
)

// The stride length is implicit in the width of the buffer.
type TimeseriesProcessor struct {
   // The number of dimensions used for SVD (aka ks)
   svdDimensions int
   // The number of dimensions to choose from the output of SVD (aka kb)
   svdOutputDimensions int
   // The number of dimensions used for euclidean distance (aka ke)
   euclidDimensions int
   correlationThreshold float64  // aka T
   windowSize int // aka n
}

func (w *TimeseriesWindow) processBuffer(processor TimeseriesProcessor,
    buffer TimeseriesWindow) error {

   // Shift _buffer_ into _window_. The stride length is equal to the
   // width of _buffer_. 
   err := w.shiftBuffer(buffer)
   if err != nil {
      return err
   }

   // Need to keep the original matrix intact because
   // we have to normalize again after the next buffer arrives.
   originalMatrix := w

   // Normalize _window_
   normalizedMatrix := originalMatrix.normalizeWindow()

   // Reduce dimensions of w to processor.svdDimensions
   windowForSvd := normalizedMatrix.PAA(processor.svdDimensions)

   // Apply SVD
   windowForBucketing, err := windowForSvd.SVD(processor.svdOutputDimensions)
   if err != nil {
      return err
   }
   fmt.Printf("window for bucketing: %+v\n", windowForBucketing.data)

   // CorrelationBuckets outputs a set of row index pairs.
   pairs, err := windowForBucketing.CorrelationPairs(normalizedMatrix.data,
      processor.svdDimensions, processor.euclidDimensions,
      processor.correlationThreshold)
   if err != nil { return err }

   for pair, corr := range pairs {
      fmt.Printf("row pair %+v is correlated: %f\n", pair, corr)
   }

   return nil
}
