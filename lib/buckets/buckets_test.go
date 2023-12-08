package buckets

import (
   "fmt"
   //"math"
   "testing"
)

func TestBucketIndex(t *testing.T) {
   scheme := NewBucketingScheme(2, 0.1, 2)
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

func TestAssign(t *testing.T) {
   scheme := NewBucketingScheme(3, 0.1, 5)
   buckets, err := scheme.Assign([]float64{0.0})
   if err == nil {
      t.Errorf("expected error for short window in bucketing")
   }
   buckets, err = scheme.Assign([]float64{0.0, 1.0, 1.0, 1.0})
   if err == nil {
      t.Errorf("expected error for overlong window in bucketing")
   }
   buckets, err = scheme.Assign([]float64{0.0, 0.335, -1.23 })
   if err != nil {
      t.Errorf("unexpected error in bucketing: %v", err)
   }
   fmt.Printf("buckets for valid vector: %+v\n", buckets)
}

func TestInsertRow(t *testing.T) {
   scheme := NewBucketingScheme(3, 0.1, 5)
   err := scheme.InsertRow([]float64{0.0, 0.335, -1.23}, 1)
   if err != nil {
      t.Errorf("unexpected error in bucketing: %v", err)
   }
   err = scheme.InsertRow([]float64{0.0, 0.335, -1.23}, 3)
   if err != nil {
      t.Errorf("unexpected error in bucketing: %v", err)
   }
   bucket, ok := scheme.buckets["[0 1 -7]"]
   if !ok {
      t.Errorf("missing entry in bucketing scheme")
   }
   membersFound := bucket.members
   membersExpected := []int{1,3}
   if len(membersFound) != len(membersExpected) || membersFound[0] != membersExpected[0] ||
      membersFound[1] != membersExpected[1] {
      t.Errorf("expected members 1 and 3 in bucket but got %v\n", membersFound)
   }
   err = scheme.InsertRow([]float64{1.0, 0.335, -1.23}, 2)
   if err != nil {
      t.Errorf("unexpected error in bucketing: %v", err)
   }
}

func TestNeighbourCoordinates(t *testing.T) {
   x := neighbourCoordinates([]int{1,2,3})
   if len(x) != 26 {
      t.Errorf("unexpected number of neighbours %d", len(x))
   }
   fmt.Printf("neighbours: %v\n", x)
}

func TestCorrelationCandidates(t *testing.T) {
   scheme := NewBucketingScheme(3, 0.1, 5)
   scheme.InsertRow([]float64{0.0, 0.335, -1.23}, 1)
   scheme.InsertRow([]float64{0.001, 0.335, -1.23}, 3)
   scheme.InsertRow([]float64{0.32, 0.335, -1.23}, 5)
   scheme.InsertRow([]float64{1.0, 0.235, 0.23}, 2)
   buckets := scheme.Buckets()
   if len(buckets) != 3 {
      t.Errorf("unexpected number %d of buckets", len(buckets))
   }
   for _, b := range buckets {
      cand := scheme.CorrelationCandidates(b)
      fmt.Printf("got candidates %v in bucket %s\n", cand, b)
   }
}
