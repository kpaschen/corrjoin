package buckets

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/settings"
	"github.com/kpaschen/corrjoin/lib/utils"
	"log"
	"math"
)

// A Bucket is an n-dimensional epsilon-tree leaf.
// The coordinates are a list of the bucket indices on each level of
// the n-dimensional bucketing scheme.
// members is a list of the row indices of the data rows that ended
// up in this bucket.
type Bucket struct {
	coordinates []int
	members     []int
}

// k-dimensional items are assigned into a k-dimensional bucketing scheme.
// Think of the scheme like an epsilon-tree, with one level per dimension
// plus the root node.
// So a 3-dimensional bucketing scheme has a tree with four levels.
// The buckets are the leaves, and the 'bucket coordinates' tell you the
// path from the root node to the leaf.
// At every level, we have floor(2/epsilon1) + 1 child nodes per node.
// The code creates only the non-empty leaf nodes.
type BucketingScheme struct {
	// Information that we need as input
	// ----------------------------------
	// originalMatrix, normalized
	originalMatrix [][]float64
	// the matrix that came out of SVD
	svdOutputMatrix [][]float64

	// Optional; when the ith entry in this slice is true,
	// that row is constant or nearly constant.
	// As an optimization, can skip that row from processing.
	constantRows []bool

	settings settings.CorrjoinSettings

	buckets map[string]*Bucket

	strideCounter int

	comparer comparisons.Engine
}

// Create a new bucketing scheme.
func NewBucketingScheme(originalMatrix [][]float64,
	svdOutputMatrix [][]float64,
	constantRows []bool, settings settings.CorrjoinSettings,
	strideCounter int,
	comparer comparisons.Engine) *BucketingScheme {

	return &BucketingScheme{
		originalMatrix:  originalMatrix,
		svdOutputMatrix: svdOutputMatrix,
		constantRows:    constantRows,
		settings:        settings,
		buckets:         map[string]*Bucket{},
		strideCounter:   strideCounter,
		comparer:        comparer,
	}
}

// BucketIndex returns the integer index of the bucket that value
// falls into.
func (s *BucketingScheme) BucketIndex(value float64) int {
	return int(math.Floor(value / float64(s.settings.Epsilon1)))
}

func (s *BucketingScheme) Initialize() error {
	rowCount := len(s.svdOutputMatrix)
	columnCount := len(s.svdOutputMatrix[0])
	if columnCount != s.settings.SvdOutputDimensions {
		return fmt.Errorf("svd output matrix needs to be length %d but is length %d",
			s.settings.SvdOutputDimensions, columnCount)
	}
	// When svd works perfectly, this should be something like 2.0 / epsilon1,
	// but it is usually 3 - 7.
	for i := 0; i < rowCount; i++ {
		if len(s.constantRows) > 0 && s.constantRows[i] {
			continue
		}
		vec := s.svdOutputMatrix[i]
		coordinates := make([]int, columnCount, columnCount)
		for j, v := range vec {
			coordinates[j] = s.BucketIndex(v)
		}
		name := BucketName(coordinates)
		bucket, exists := s.buckets[name]
		if exists {
			bucket.members = append(bucket.members, i)
		} else {
			s.buckets[name] = &Bucket{
				coordinates: coordinates,
				members:     make([]int, 1, 1000),
			}
			s.buckets[name].members[0] = i
		}
	}

	utils.ReportMemory(fmt.Sprintf("initialized buckets. There are %d buckets\n", len(s.buckets)))
	for name, b := range s.buckets {
		log.Printf("bucket with name %s has %d members\n", name, len(b.members))
	}

	return nil
}

func BucketName(coordinates []int) string {
	return fmt.Sprintf("%v", coordinates)
}

func (s *BucketingScheme) CorrelationCandidates() error {
	fmt.Printf("Correlation candidates: looking at %d buckets\n", len(s.buckets))
	for _, bucket := range s.buckets {
		err := s.candidatesForBucket(bucket)
		if err != nil {
			return err
		}
	}
	// Let the comparer know there will be no further requests for this stride.
	return s.comparer.StopStride(s.strideCounter)
}

// candidatesForBucket processes rowPairs for a Bucket and its neighbours.
func (s *BucketingScheme) candidatesForBucket(bucket *Bucket) error {
	utils.ReportMemory(fmt.Sprintf("starting on bucket %s with %d members\n",
		BucketName(bucket.coordinates), len(bucket.members)))
	for i := 0; i < len(bucket.members); i++ {
		r1 := bucket.members[i]
		for j := i + 1; j < len(bucket.members); j++ {
			r2 := bucket.members[j]
			if r1 == r2 {
				return fmt.Errorf("duplicate entry %d in bucket %s", r1,
					BucketName(bucket.coordinates))
			}
			err := s.comparer.Compare(r1, r2)
			if err != nil {
				return err
			}
		}
	}

	n := neighbourCoordinates(bucket.coordinates)
	log.Printf("bucket %s has %d neighbours\n", BucketName(bucket.coordinates), len(n))
	for _, d := range n {
		name := BucketName(d)
		neighbour, exists := s.buckets[name]
		if exists {
			for _, r1 := range bucket.members {
				for _, r2 := range neighbour.members {
					if r1 == r2 {
						return fmt.Errorf("element %d is in buckets %s and %s", r1,
							BucketName(bucket.coordinates), name)
					}
					err := s.comparer.Compare(r1, r2)
					if err != nil {
						return err
					}
				}
			}
		}
	}
	log.Printf("done with bucket %s\n", BucketName(bucket.coordinates))
	return nil
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
			if i&(1<<j) > 0 {
				bitCounter++
			}
		}
		// There are 2^bitCounter possible ways of assigning signs
		signsCount := int(math.Pow(2, float64(bitCounter)))
		newInputs := make([][]int, signsCount, signsCount)
		for k := 0; k < signsCount; k++ {
			newInputs[k] = make([]int, len(input))
			copy(newInputs[k], input)
			c := 0
			for j := 0; j < len(input); j++ {
				if i&(1<<j) > 0 {
					if k&(1<<c) > 0 {
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
	log.Printf("neighbourCoordinates for %d inputs returning %d neighbours\n",
		len(input), len(ret))
	return ret
}
