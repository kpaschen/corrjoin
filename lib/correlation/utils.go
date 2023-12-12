package correlation

import (
   "fmt"
   "math"
)

func EuclideanDistance(x []float64, y []float64) (float64, error) {
   if len(x) != len(y) {
      return 0.0, fmt.Errorf("euclidean distance needs arguments of the same length")
   }
   sum := 0.0
   for i, xi := range x {
      diff := xi - y[i]
      sum += diff * diff
   }
   return math.Sqrt(sum), nil
}

// This is the formula for incremental pearson.
func PearsonCorrelation(x []float64, y []float64) (float64, error) {
   if len(x) != len(y) {
      return 0.0, fmt.Errorf("correlation needs arguments of the same length")
   }
   var s1, s2, s3, s4, s5 float64
   for i, xi := range x {
      s1 += xi
      s2 += xi * xi
      s3 += y[i]
      s4 += y[i] * y[i]
      s5 += xi * y[i]
   }
   n := float64(len(x))

   return (n * s5 - (s1 * s3)) / math.Sqrt((n * s2 - s1 * s1) * (n * s4 - s3 * s3)), nil
}
