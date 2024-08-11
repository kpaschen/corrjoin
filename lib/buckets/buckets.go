package buckets

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/correlation"
	"github.com/kpaschen/corrjoin/lib/paa"
	"log"
	"math"
	"runtime"
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

type RowPair struct {
	r1 int
	r2 int
}

func (r RowPair) RowIds() [2]int {
	return [2]int{r.r1, r.r2}
}

func NewRowPair(r1 int, r2 int) *RowPair {
	return &RowPair{r1: r1, r2: r2}
}

func printMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	log.Printf("Alloc = %v MiB", bToMb(m.Alloc))
	log.Printf("\tTotalAlloc = %v MiB", bToMb(m.TotalAlloc))
	log.Printf("\tSys = %v MiB", bToMb(m.Sys))
	log.Printf("\tNumGC = %v\n", m.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func reportMemory(message string) {
	log.Println(message)
	printMemUsage()
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

	// the number of columns that was used for the first PAA step
	// This equals the number of columns in the svd input matrix
	ks int
	// the number of columns to use for the second PAA step
	ke int
	// The correlation Threshold (T)
	correlationThreshold float64

	// Parameters computed from the input information
	// ----------------------------------------------
	// The number of levels
	// should be the same as the number of columns in svdOutputMatrix
	dimensions int

	// thresholds for the two filtering steps
	// epsilon1 = sqrt(2 ks (1 - correlationThreshold) / n)
	// where n = number of columns in originalMatrix
	epsilon1 float64
	// epsilon2 = sqrt(2 ke (1 - correlationThreshold) / n)
	epsilon2 float64 // this is for the euclidean distance filter

	// intermediate data storage
	// ---------------------------
	// This is where we remember row pairs we've already evaluated.
	scores map[RowPair]float64
	// lazily computed paa2 values
	paa2    map[int][]float64
	buckets map[string]*Bucket

	r1ProcessedCount      int
	r1FilterCount         int
	r2ProcessedCount      int
	r2FilterCount         int
	pearsonProcessedCount int
	pearsonFilterCount    int
	dropConstantRowsCount int
}

// Create a new bucketing scheme.
func NewBucketingScheme(originalMatrix [][]float64,
	svdOutputMatrix [][]float64,
	constantRows []bool, ks int, ke int, correlationThreshold float64) *BucketingScheme {

	originalColumnCount := len(originalMatrix[0])
	postSvdColumnCount := len(svdOutputMatrix[0])

	log.Printf("using n = %d and svd output columns %d\n", originalColumnCount, postSvdColumnCount)

	// epsilon1 = sqrt(2 ks (1 - correlationThreshold) / n)
	epsilon1 := math.Sqrt(float64(2*ks) * (1.0 - correlationThreshold) / float64(originalColumnCount))

	// epsilon2 = sqrt(2 ke (1 - correlationThreshold) / n)
	epsilon2 := math.Sqrt(float64(2*ke) * (1.0 - correlationThreshold) / float64(originalColumnCount))

	log.Printf("epsilon1 %f epsilon2 %f, T %f\n", epsilon1, epsilon2, correlationThreshold)

	return &BucketingScheme{
		originalMatrix:       originalMatrix,
		svdOutputMatrix:      svdOutputMatrix,
		constantRows:         constantRows,
		ke:                   ke,
		ks:                   ks,
		dimensions:           postSvdColumnCount,
		correlationThreshold: correlationThreshold,
		epsilon1:             epsilon1,
		epsilon2:             epsilon2,
		scores:               map[RowPair]float64{},
		paa2:                 map[int][]float64{},
		buckets:              map[string]*Bucket{},
	}
}

// BucketIndex returns the integer index of the bucket that value
// falls into.
func (s *BucketingScheme) BucketIndex(value float64) int {
	return int(math.Floor(value / float64(s.epsilon1)))
}

func (s *BucketingScheme) Stats() [7]int {
	return [7]int{
		s.r1ProcessedCount, s.r1FilterCount,
		s.r2ProcessedCount, s.r2FilterCount,
		s.pearsonProcessedCount, s.pearsonFilterCount,
		s.dropConstantRowsCount,
	}
}

func (s *BucketingScheme) Initialize() error {
	rowCount := len(s.svdOutputMatrix)
	columnCount := len(s.svdOutputMatrix[0])
	if columnCount != s.dimensions {
		return fmt.Errorf("svd output matrix needs to be length %d but is length %d",
			s.dimensions, columnCount)
	}
	// When svd works perfectly, this should be something like 2.0 / epsilon1,
	// but it is usually 3 - 7.
	maxValuesPerDimension := 10.0
	log.Printf("expect %f values per dimension\n", maxValuesPerDimension)
	maxBucketCount := math.Pow(maxValuesPerDimension, float64(columnCount))
	averageEntriesPerBucket := int(float64(rowCount) / maxBucketCount)
	if averageEntriesPerBucket < 1 {
		averageEntriesPerBucket = 1
	}
	log.Printf("there are at most %d buckets, expect %d entries per bucket on average",
		int(maxBucketCount), averageEntriesPerBucket)
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
				members:     make([]int, 1, averageEntriesPerBucket),
			}
			s.buckets[name].members[0] = i
		}
	}

	reportMemory(fmt.Sprintf("initialized buckets. There are %d buckets\n", len(s.buckets)))
	for name, b := range s.buckets {
		log.Printf("bucket with name %s has %d members\n", name, len(b.members))
	}

	return nil
}

func BucketName(coordinates []int) string {
	return fmt.Sprintf("%v", coordinates)
}

func (s *BucketingScheme) CorrelationCandidates() (map[RowPair]float64, error) {
	ret := map[RowPair]float64{}
	s.r1ProcessedCount = 0
	s.r1FilterCount = 0
	s.r2ProcessedCount = 0
	s.r2FilterCount = 0
	s.pearsonProcessedCount = 0
	s.pearsonFilterCount = 0
	for i, bucket := range s.buckets {
		candidates, err := s.candidatesForBucket(bucket)
		if err != nil {
			return nil, err
		}
		reportMemory(fmt.Sprintf("there are %d candidates for bucket %s", len(candidates), i))
		for _, c := range candidates {
			s.pearsonProcessedCount++
			pearson, err := correlation.PearsonCorrelation(
				s.originalMatrix[c.r1],
				s.originalMatrix[c.r2])
			if err != nil {
				return nil, err
			}
			if pearson >= s.correlationThreshold {
				ret[c] = pearson
			} else {
				s.pearsonFilterCount++
			}
		}
		reportMemory(fmt.Sprintf("done comparing candidates for bucket %s", i))
	}
	return ret, nil
}

// candidatesForBucket returns a list of rowPairs for Bucket
// and its neighbours.
func (s *BucketingScheme) candidatesForBucket(bucket *Bucket) ([]RowPair, error) {
	ret := make([]RowPair, 0)
	for i := 0; i < len(bucket.members); i++ {
		r1 := bucket.members[i]
		for j := i + 1; j < len(bucket.members); j++ {
			r2 := bucket.members[j]
			if r1 == r2 {
				return nil, fmt.Errorf("duplicate entry %d in bucket %s", r1,
					BucketName(bucket.coordinates))
			}
			if r1 > r2 {
				r1, r2 = r2, r1
			}
			rp := RowPair{r1: r1, r2: r2}
			ok, err := s.filterPair(rp, true)
			if err != nil {
				return nil, err
			}
			if ok {
				ret = append(ret, rp)
			}
		}
	}

	n := neighbourCoordinates(bucket.coordinates)
	for _, d := range n {
		name := BucketName(d)
		neighbour, exists := s.buckets[name]
		if exists {
			for _, r1 := range bucket.members {
				for _, r2 := range neighbour.members {
					if r1 == r2 {
						return nil, fmt.Errorf("element %d is in buckets %s and %s", r1,
							BucketName(bucket.coordinates), name)
					}
					rp := RowPair{r1: r1, r2: r2}
					if r1 > r2 {
						rp = RowPair{r1: r2, r2: r1}
					}
					ok, err := s.filterPair(rp, false)
					if err != nil {
						return nil, err
					}
					if ok {
						ret = append(ret, rp)
					}
				}
			}
		}
	}
	return ret, nil
}

func (s *BucketingScheme) ignoreConstantRows(pair RowPair) (bool, error) {
	if len(s.constantRows) == 0 {
		return false, nil
	}
	if s.constantRows[pair.r1] != s.constantRows[pair.r2] {
		return false, fmt.Errorf("only one of row pair %v is constant?", pair)
	}
	return s.constantRows[pair.r1], nil
}

func (s *BucketingScheme) filterPair(pair RowPair, sameBucket bool) (bool, error) {
	_, exists := s.scores[pair]
	if exists {
		return false, nil
	}
	ignore, err := s.ignoreConstantRows(pair)
	if err != nil {
		return false, err
	}
	if ignore {
		s.dropConstantRowsCount++
		return true, nil
	}

	// This is unnecessary if the pair came from the same bucket.
	if !sameBucket {
		vec1 := s.svdOutputMatrix[pair.r1]
		vec2 := s.svdOutputMatrix[pair.r2]
		s.r1ProcessedCount++
		distance, err := correlation.EuclideanDistance(vec1, vec2)
		if err != nil {
			return false, err
		}
		s.scores[pair] = distance
		if distance > s.epsilon1 {
			s.r1FilterCount++
			return false, nil
		}
	}
	// Next filtering step needs to apply PAA (with s.ke target dimensions)
	// to s.originalMatrix[firstElement] and secondElement and compare
	// their euclidean distances to s.epsilon2
	paaVec1, exists := s.paa2[pair.r1]
	if !exists {
		input := s.originalMatrix[pair.r1]
		paaVec1 = paa.PAA(input, s.ke)
		s.paa2[pair.r1] = paaVec1
	}
	paaVec2, exists := s.paa2[pair.r2]
	if !exists {
		input := s.originalMatrix[pair.r2]
		paaVec2 = paa.PAA(input, s.ke)
		s.paa2[pair.r2] = paaVec2
	}
	s.r2ProcessedCount++
	distance, err := correlation.EuclideanDistance(paaVec1, paaVec2)
	if err != nil {
		return false, err
	}
	if distance > s.epsilon2 {
		s.r2FilterCount++
		return false, nil
	}
	return true, nil
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
	return ret
}
