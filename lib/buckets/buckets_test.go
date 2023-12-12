package buckets

import (
   "fmt"
   "gonum.org/v1/gonum/mat"
   //"math"
   "testing"
)

func setupBucketingScheme() *BucketingScheme {
   initialTsData := []float64{
      0.1, 0.2, 0.3,
      1.1, 1.2, 1.3,
      2.1, 2.2, 2.3,
   }
   originalMatrix := mat.NewDense(3, 3, initialTsData)
   postSvdData := []float64{
   0.1, 0.2,
   0.1, 0.2,
   2.1, 2.2,
   }
   svdOutputMatrix := mat.NewDense(3, 2, postSvdData)
   return NewBucketingScheme(originalMatrix, svdOutputMatrix, 3, 2, 0.9)
}

func TestBucketIndex(t *testing.T) {
   scheme := setupBucketingScheme()
   b := scheme.BucketIndex(0.0)
   if b != 0 { 
      t.Errorf("expected bucket 0 for value 0.0 but got %d", b)
   }
   // With these settings, the bucket width is about 0.3
   b = scheme.BucketIndex(0.5)
   if b != 1 {
      t.Errorf("expected bucket 1 for value 0.5 but got %d", b)
   }
}

func TestInitialize(t *testing.T) {
   scheme := setupBucketingScheme()
   err := scheme.Initialize()
   if err != nil {
      t.Errorf("unexpected error in bucket scheme initialization: %v", err)
   }
   if len(scheme.buckets) != 2 {
      t.Errorf("expected two buckets but got %d", len(scheme.buckets))
   }
}

func TestNeighbourCoordinates(t *testing.T) {
   x := neighbourCoordinates([]int{1,2,3})
   if len(x) != 26 {
      t.Errorf("unexpected number of neighbours %d", len(x))
   }
   x = neighbourCoordinates([]int{1,2,3,4})
   if len(x) != 80 {
      t.Errorf("unexpected number of neighbours %d", len(x))
   }
}

func TestCorrelationCandidates(t *testing.T) {
   scheme := setupBucketingScheme()
   scheme.Initialize()
   cand, err := scheme.CorrelationCandidates()
   if err != nil {
      fmt.Errorf("unexpected error in CorrelationCandidates: %v", err)
   }
   if len(cand) != 1 {
      fmt.Errorf("expected one correlated pair to be found but got %d", len(cand))
   }
   for rowPair, correlation := range cand {
      if rowPair.r1 != 0 || rowPair.r2 != 1 {
         fmt.Errorf("expected rows 0 and 1 to be correlated but got %d %d", rowPair.r1, rowPair.r2)
      }
      if correlation != 1 {
         fmt.Errorf("expected perfect correlation but got %f", correlation)
      }
   }
}
