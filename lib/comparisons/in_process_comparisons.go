// Package comparisons contains different execution engines for
// pairwise comparison of timeseries.
package comparisons

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/correlation"
	"github.com/kpaschen/corrjoin/lib/paa"
	"github.com/kpaschen/corrjoin/lib/settings"
	"log"
)

type strideStats struct {
	comparisons  int
	correlated   int
	constantRows int
}

// An InProcessComparer implements Engine.
type InProcessComparer struct {
	// These settings remain for the lifetime of an engine.
	config        settings.CorrjoinSettings
	resultChannel chan<- *CorrjoinResult

	// These settings change when there is a new stride.
	normalizedMatrix [][]float64
	constantRows     []bool
	strideCounter    int
	paa2             map[int][]float64

	// May retain stats for past strides.
	stats map[int]*strideStats
}

func (s *InProcessComparer) Initialize(config settings.CorrjoinSettings, results chan<- *CorrjoinResult) {
	s.config = config
	s.resultChannel = results
	s.stats = make(map[int]*strideStats)
	s.strideCounter = -1
}

func (s *InProcessComparer) StartStride(normalizedMatrix [][]float64, constantRows []bool, strideCounter int) error {
	if strideCounter < s.strideCounter {
		return fmt.Errorf("got new stride %d but current stride %d is larger", strideCounter, s.strideCounter)
	}
	if strideCounter == s.strideCounter {
		return fmt.Errorf("repeated StartStride call for stride %d", strideCounter)
	}
	s.normalizedMatrix = normalizedMatrix
	s.constantRows = constantRows
	s.strideCounter = strideCounter
	s.stats[s.strideCounter] = &strideStats{
		comparisons:  0,
		correlated:   0,
		constantRows: 0,
	}

	return nil
}

func (s *InProcessComparer) isConstantRow(index int) bool {
	if s.constantRows != nil && len(s.constantRows) > index {
		return s.constantRows[index]
	}
	return false
}

// Compare asks for a comparison of the rows identified by index1 and index2 in the
// normalized matrix.
func (s *InProcessComparer) Compare(index1 int, index2 int) error {
	if s.strideCounter < 0 {
		return fmt.Errorf("asked for comparison but there is no current stride")
	}
	if s.isConstantRow(index1) || s.isConstantRow(index2) {
		s.stats[s.strideCounter].constantRows++
		return nil
	}

	// TODO: cache paa2 outputs

	var vec1, vec2 []float64
	var pair RowPair
	if index1 > index2 {
		vec1 = s.normalizedMatrix[index2]
		vec2 = s.normalizedMatrix[index1]
		pair = RowPair{r1: index2, r2: index1}
	} else {
		vec1 = s.normalizedMatrix[index1]
		vec2 = s.normalizedMatrix[index2]
		pair = RowPair{r1: index1, r2: index2}
	}

	paaVec1 := paa.PAA(vec1, s.config.EuclidDimensions)
	paaVec2 := paa.PAA(vec2, s.config.EuclidDimensions)

	// This test is redundant for pairs that come from the same bucket.
	distance, err := correlation.EuclideanDistance(paaVec1, paaVec2)
	if err != nil {
		return err
	}
	if distance > s.config.Epsilon2 {
		// Not a match
		return nil
	}
	// Now apply pearson
	s.stats[s.strideCounter].comparisons++
	pearson, err := correlation.PearsonCorrelation(paaVec1, paaVec2)
	if err != nil {
		return err
	}
	if pearson >= s.config.CorrelationThreshold {
		// This is a match, send it to the results channel
		s.stats[s.strideCounter].correlated++
		ret := map[RowPair]float64{}
		ret[pair] = pearson
		s.resultChannel <- &CorrjoinResult{
			CorrelatedPairs: ret,
			StrideCounter:   s.strideCounter,
		}
	}
	return nil
}

func (s *InProcessComparer) StopStride(strideCounter int) error {
	if strideCounter != s.strideCounter {
		return fmt.Errorf("trying to stop stride %d but i am processing stride %d",
			strideCounter, s.strideCounter)
	}
	// Send stride end to results channel
	s.resultChannel <- &CorrjoinResult{
		CorrelatedPairs: map[RowPair]float64{},
		StrideCounter:   s.strideCounter,
	}

	log.Printf("stride %d complete, stats: %+v\n", strideCounter, s.stats[s.strideCounter])

	return nil
}

// Shutdown gives the engine a chance to cancel running computations when it is deleted.
func (s *InProcessComparer) Shutdown() error {

	log.Println("in process comparer shutting down")

	// Send stride end to results channel
	s.resultChannel <- &CorrjoinResult{
		CorrelatedPairs: map[RowPair]float64{},
		StrideCounter:   s.strideCounter,
	}

	return nil
}
