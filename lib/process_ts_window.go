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

   r, _ := w.data.Dims()

   originalMatrix := w

   // Normalize _window_
   normalizedMatrix := originalMatrix.normalizeWindow()

   pairs, err := normalizedMatrix.AllPairs(normalizedMatrix.data,
      processor.CorrelationThreshold)
   if err != nil { return err }

   totalPairs := r * r / 2

   fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
      100 * len(pairs) / totalPairs)

   return nil
}

func (w *TimeseriesWindow) PAAOnly(processor TimeseriesProcessor,
   buffer *TimeseriesWindow) error {
   if buffer != nil {
      // Shift _buffer_ into _window_. The stride length is equal to the
      // width of _buffer_. 
      err := w.shiftBuffer(*buffer)
      if err != nil {
         return err
      }
   }

   normalizedMatrix := w.normalizeWindow() 
   // normalizedMatrix := w

   // Reduce dimensions of w to processor.svdDimensions
   postPAA := normalizedMatrix.PAA(processor.SvdDimensions)

   paaPairs, err := postPAA.PAAPairs(normalizedMatrix.data, processor.CorrelationThreshold)
   if err != nil { return err }

   r, _ := w.data.Dims()
   totalPairs := r * r / 2

   fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(paaPairs), totalPairs,
      100 * len(paaPairs) / totalPairs)
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

   originalMatrix := w
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

   r, _ := w.data.Dims()
   totalPairs := r * r / 2

   fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
      100 * len(pairs) / totalPairs)

   return nil
}
