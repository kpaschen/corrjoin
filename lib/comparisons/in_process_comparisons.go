// Package comparisons contains different execution engines for
// pairwise comparison of timeseries.
package comparisons

import (
	"github.com/kpaschen/corrjoin/lib/correlation"
	"github.com/kpaschen/corrjoin/lib/paa"
	"github.com/kpaschen/corrjoin/lib/settings"
)

// An InProcessComparer implements Engine.
type InProcessComparer struct {
	config           settings.CorrjoinSettings
	normalizedMatrix *[][]float64
	resultChannel    chan<- *CorrjoinResult
	constantRows     []bool // TODO: fill these in
	strideCounter    int    // TODO: this too
}

// Initialize provides the engine with the data and settings it needs.
func (s *InProcessComparer) Initialize(config settings.CorrjoinSettings, normalizedMatrix *[][]float64, results chan<- *CorrjoinResult) {
	s.config = config
	s.normalizedMatrix = normalizedMatrix
	s.resultChannel = results
}

// Compare asks for a comparison of the rows identified by index1 and index2 in the
// normalized matrix.
func (s *InProcessComparer) Compare(index1 int, index2 int) error {
	// TODO: ignore constant rows

	// TODO: cache paa2 outputs

	var vec1, vec2 []float64
	var pair RowPair
	if index1 > index2 {
		vec1 = (*s.normalizedMatrix)[index2]
		vec2 = (*s.normalizedMatrix)[index1]
		pair = RowPair{r1: index2, r2: index1}
	} else {
		vec1 = (*s.normalizedMatrix)[index1]
		vec2 = (*s.normalizedMatrix)[index2]
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
	pearson, err := correlation.PearsonCorrelation(paaVec1, paaVec2)
	if err != nil {
		return err
	}
	if pearson >= s.config.CorrelationThreshold {
		// This is a match, send it to the results channel
		ret := map[RowPair]float64{}
		ret[pair] = pearson
		s.resultChannel <- &CorrjoinResult{
			CorrelatedPairs: ret,
			StrideCounter:   s.strideCounter,
		}
	}
	return nil
}

// Shutdown gives the engine a chance to cancel running computations when it is deleted.
func (s *InProcessComparer) Shutdown() error {

	// Send stride end to results channel
	s.resultChannel <- &CorrjoinResult{
		CorrelatedPairs: map[RowPair]float64{},
		StrideCounter:   s.strideCounter,
	}
	return nil
}
