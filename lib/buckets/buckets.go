package buckets

import (
   "fmt"
   "math"
   "golang.org/x/exp/maps"
)

// A Bucket is an n-dimensional epsilon-tree leaf.
// The coordinates are a list of the bucket indices on each level of
// the n-dimensional bucketing scheme.
// members is a list of the row indices of the data rows that ended
// up in this bucket.
type Bucket struct {
   coordinates []int
   members []int
}

// k-dimensional items are assigned into a k-dimensional bucketing scheme
// by finding a bucket at each dimension.
type BucketingScheme struct {
   // The number of levels
   dimensions int
   // The width of a bucket on each level
   width float32
   buckets map[string]*Bucket
}

// Initialize a new bucketing scheme with the given number
// of dimensions.
// The width of a bucket is determined using a constant theta (example: 0.1)
// and the window size (= the number of columns in the input before PAA).
// You need to make sure windowSize is not overly large, otherwise width will
// be very small. Then again, a window size above 1000 or so is probably really
// bad for performance anyway.
func NewBucketingScheme(dimensions int, theta float32, windowSize int) *BucketingScheme {
   return &BucketingScheme{
      dimensions: dimensions,
      width: float32(math.Sqrt(float64(2.0 * theta / float32(windowSize)))),
      buckets: map[string]*Bucket{},
   }
}

// BucketIndex returns the integer index of the bucket that value
// falls into.
func (s *BucketingScheme) BucketIndex(value float64) int {
   return int(math.Floor(value / float64(s.width)))
}

func (s *BucketingScheme) Buckets() []string {
   return maps.Keys(s.buckets)
}

// Returns the a b.dimensions-length list of integers. The ith integer
// is the index of the bucket on the ith level of the bucketing scheme
// that window falls into.
// Call this with the ith row of a matrix e.g. using m.RowView(i).Data
func (s *BucketingScheme) Assign(window []float64) ([]int, error) {
   if len(window) != s.dimensions {
      return nil, fmt.Errorf("Window passed into Assign needs to be length %d but is length %d",
         s.dimensions, len(window))
   }
   ret := make([]int, s.dimensions, s.dimensions)
   for i, v := range window {
      ret[i] = s.BucketIndex(v)
   }
   return ret, nil
}

func BucketName(coordinates []int) string {
   return fmt.Sprintf("%v", coordinates)
}

func (s *BucketingScheme) InsertRow(window []float64, index int) error {
   coordinates, err := s.Assign(window)
   if err != nil { return err }
   name := BucketName(coordinates)
   bucket, exists := s.buckets[name]
   if exists {
      bucket.members = append(bucket.members, index)
   } else {
      s.buckets[name] = &Bucket{
         coordinates: coordinates,
         members: []int{index},
      }
   }
   return nil
}

// CorrelationCandidates returns a list of lists. Each list contains
// a list of integers, which are candidates ids (aka row indices).
// The first list is for the bucket identified by bucketName. The subsequent
// lists are for the neighbours of that bucket. Only nonempty neighbour
// buckets will be returned.
func (s *BucketingScheme) CorrelationCandidates(bucketName string) [][]int {
   bucket, ok := s.buckets[bucketName]
   if !ok { return nil }
   ret := make([][]int, 0)
   ret = append(ret, bucket.members)
   n := neighbourCoordinates(bucket.coordinates)
   for _, d := range n {
      name := BucketName(d)
      neighbour, exists := s.buckets[name]
      if exists {
         // name is the name of a neighbouring bucket
         ret = append(ret, neighbour.members)
      }
   }
   return ret
}

func neighbourCoordinates(input []int) [][]int {
   // In every direction (~ dimension of input), we can either
   // leave the value as it is, or add one, or subtract 1.
   totalNeighbours := int(math.Pow(3, float64(len(input))))

   // modifyValues is a bitmap where the ith bit tells us 
   // whether we're leaving that value alone or modifying it.
   modifyValues := int(math.Pow(2, float64(len(input))))
   ret := make([][]int, 0, totalNeighbours)
   // For all numbers i from 1 to 2^(len(input) - 1)
   for i := 1; i < modifyValues; i++ {
      bitCounter := 0
      for j := 0; j < len(input); j++ {
         if i & (1 << j) > 0 { bitCounter++ }
      }
      // There are 2^bitCounter possible ways of assigning signs
      signsCount := int(math.Pow(2, float64(bitCounter)))
      newInputs := make([][]int, signsCount, signsCount)
      for k := 0; k < signsCount; k++ {
         newInputs[k] = make([]int, len(input))
         copy(newInputs[k], input)
         c := 0
         for j := 0; j < len(input); j++ {
            if i & (1 << j) > 0 { 
               if k & (1 << c) > 0 {
                  newInputs[k][j] = newInputs[k][j] - 1
               } else {
                  newInputs[k][j] = newInputs[k][j] + 1
               }
               c++
            }
         }
      }
      ret = append(ret, newInputs...)
   }
   return ret
}
