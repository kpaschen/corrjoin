package lib

import (
   "fmt"
   "gonum.org/v1/gonum/mat"
   "math"
   "testing"
)

func TestNormalizeWindow(t *testing.T) {
   initialTsData := []float64{
      0.1, 0.2, 0.3,
      1.1, 1.2, 1.3,
      2.1, 2.2, 2.3,
   }
   initial := mat.NewDense(3, 3, initialTsData)
   tswindow := &TimeseriesWindow{
       data: initial,
   }
   tswindow.normalizeWindow()

   fmt.Printf("normalized: %+v\n", tswindow.data)
}

func TestShiftBuffer(t *testing.T) {
   initialTsData := []float64{
      0.1, 0.2, 0.3,
      1.1, 1.2, 1.3,
      2.1, 2.2, 2.3,
   }
   bufferData := []float64{
      0.4, 1.4, 2.4,
   }
   // rows, columns, backing slice
   initial := mat.NewDense(3, 3, initialTsData)
   buffer := mat.NewDense(3, 1, bufferData)

   tswindow := &TimeseriesWindow{
       data: initial,
   }
   bufferWindow := TimeseriesWindow{
       data: buffer,
   }

   err := tswindow.shiftBuffer(bufferWindow)

   if err != nil {
      t.Errorf("unexpected error %v shifting buffer into time series window", err)
   }

   fmt.Printf("shifted matrix: %+v\n", *tswindow.data)

   expectedData := []float64{
      0.2, 0.3, 0.4,
      1.2, 1.3, 1.4,
      2.2, 2.3, 2.4,
   }
   expected := mat.NewDense(3, 3, expectedData)

   if !mat.EqualApprox(tswindow.data, expected, 0.0001) {
      t.Errorf("expected shifted matrix to be %+v but got %v", expected, tswindow.data)
   }
}

func TestPAA(t *testing.T) {
   initialTsData := []float64{
      0.1, 0.2, 0.3, 0.4,
      1.1, 1.2, 1.3, 1.4,
      2.1, 2.2, 2.3, 2.4,
   }
   initial := mat.NewDense(3, 4, initialTsData)
   tswindow := &TimeseriesWindow{
       data: initial,
   }
   reduced := tswindow.PAA(2)
   rows, columns := reduced.data.Dims()  
   if rows != 3 || columns != 2 {
      t.Errorf("Expected post-PAA matrix to have 3 rows and 2 columns but got (%d, %d)", rows, columns)
   }
}

func TestSVD(t *testing.T) {
   // Input matrix A has 2 rows, 3 columns
   // U should be 2x2, V should be 3x3
   // But we compute ThinV, so V is only 3x2 because there's just 2 nonzero eigenvalues.
   initialTsData := []float64{
      3.0, 2.0, 2.0,
      2.0, 3.0, -2.0,
   }
   initial := mat.NewDense(2, 3, initialTsData)
   tswindow := &TimeseriesWindow{ data: initial }
   svd, err := tswindow.SVD(2)
   if svd == nil {
      t.Errorf("svd is not supposed to fail")
   }
   if err != nil {
      t.Errorf("svd returned error %v", err)
   }
   r, c := svd.data.Dims()
   if r != 2 || c != 2 {
      t.Errorf("svd returned unexpected dimensions (%d, %d) rather than (2,2)", r, c)
   }
   if math.Signbit(svd.data.At(0,0)) != math.Signbit(svd.data.At(1,0)) {
      t.Errorf("expected values in first column to have the same sign")
   }
   if math.Abs(svd.data.At(0,0) - svd.data.At(1,0)) > 0.001 {
      t.Errorf("expected values in first vector to be the same")
   }
}

func TestCorrelationPairs(t *testing.T) {
   initialTsData := []float64{
      0.1, 0.2, 0.3, 0.4,
      1.1, 1.2, 1.3, 1.4,
      1.1, 1.2, 1.3, 1.4,
      2.1, 2.2, 2.3, 2.4,
   }
   initial := mat.NewDense(4, 4, initialTsData)
   tswindow := &TimeseriesWindow{
       data: initial,
   }
   pairs, err := tswindow.CorrelationPairs(initial, 4, 3, 0.9)
   if err != nil {
      t.Errorf("unexpected error in correlationpairs: %v", err)
   }
   fmt.Printf("pairs: %+v\n", pairs)
}
