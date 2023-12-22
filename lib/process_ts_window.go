package lib

import (
   "fmt"
)

// The stride length is implicit in the width of the buffer.
type TimeseriesProcessor struct {
   // The number of dimensions used for SVD (aka ks)
   SvdDimensions int
   // The number of dimensions to choose from the output of SVD (aka kb)
   SvdOutputDimensions int
   // The number of dimensions used for euclidean distance (aka ke)
   EuclidDimensions int
   CorrelationThreshold float64  // aka T
   WindowSize int // aka n
}

func (w *TimeseriesWindow) FullPearson(processor TimeseriesProcessor,
   buffer *TimeseriesWindow) error {
   if buffer != nil {
      // Shift _buffer_ into _window_. The stride length is equal to the
      // width of _buffer_. 
      err := w.shiftBuffer(*buffer)
      if err != nil {
         return err
      }
   }

   originalMatrix := w

   // Normalize _window_
   normalizedMatrix := originalMatrix.normalizeWindow()

   pairs, err := normalizedMatrix.AllPairs(normalizedMatrix.data,
      processor.CorrelationThreshold)
   if err != nil { return err }

   for pair, corr := range pairs {
      fmt.Printf("row pair %+v is correlated: %f\n", pair, corr)
   }
   return nil
}

func (w *TimeseriesWindow) ProcessBuffer(processor TimeseriesProcessor,
    buffer *TimeseriesWindow) error {

   if buffer != nil {
      // Shift _buffer_ into _window_. The stride length is equal to the
      // width of _buffer_. 
      err := w.shiftBuffer(*buffer)
      if err != nil {
         return err
      }
   }

   // Need to keep the original matrix intact because
   // we have to normalize again after the next buffer arrives.
   originalMatrix := w

   // Normalize _window_
   normalizedMatrix := originalMatrix.normalizeWindow()

   // Reduce dimensions of w to processor.svdDimensions
   windowForSvd := normalizedMatrix.PAA(processor.SvdDimensions)

   // Apply SVD
   windowForBucketing, err := windowForSvd.SVD(processor.SvdOutputDimensions)
   if err != nil {
      return err
   }
   // CorrelationBuckets outputs a set of row index pairs.
   pairs, err := windowForBucketing.CorrelationPairs(normalizedMatrix.data,
      processor.SvdDimensions, processor.EuclidDimensions,
      processor.CorrelationThreshold)
   if err != nil { return err }

   for pair, corr := range pairs {
      fmt.Printf("row pair %+v is correlated: %f\n", pair, corr)
   }

   return nil
}
