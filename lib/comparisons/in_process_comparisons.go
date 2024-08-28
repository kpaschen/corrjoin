// Package comparisons contains different execution engines for
// pairwise comparison of timeseries.
package comparisons

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
	"log"
)

// An InProcessComparer implements Engine.
type InProcessComparer struct {
	// These settings remain for the lifetime of an engine.
	config        settings.CorrjoinSettings
	constantRows  []bool
	resultChannel chan<- *datatypes.CorrjoinResult

	baseComparer  *BaseComparer
	strideCounter int
}

func (s *InProcessComparer) Initialize(config settings.CorrjoinSettings, results chan<- *datatypes.CorrjoinResult) {
	s.config = config
	s.resultChannel = results
	s.strideCounter = -1
}

func (s *InProcessComparer) StartStride(normalizedMatrix [][]float64, constantRows []bool, strideCounter int) error {
	if strideCounter < s.strideCounter {
		return fmt.Errorf("got new stride %d but current stride %d is larger", strideCounter, s.strideCounter)
	}
	if strideCounter == s.strideCounter {
		return fmt.Errorf("repeated StartStride call for stride %d", strideCounter)
	}
	if s.baseComparer == nil {
		s.baseComparer = &BaseComparer{
			paa2:  make(map[int][]float64),
			stats: make(map[int]*StrideStats),
		}
	}
	s.strideCounter = strideCounter
	s.constantRows = constantRows
	s.baseComparer.normalizedMatrix = normalizedMatrix
	s.baseComparer.config = s.config
	s.baseComparer.strideCounter = strideCounter
	s.baseComparer.stats[s.strideCounter] = &StrideStats{
		comparisons: 0,
		correlated:  0,
	}

	return nil
}

// Compare asks for a comparison of the rows identified by index1 and index2 in the
// normalized matrix.
func (s *InProcessComparer) Compare(index1 int, index2 int) error {
	if s.strideCounter < 0 {
		return fmt.Errorf("asked for comparison but there is no current stride")
	}
	if IsConstantRow(index1, s.constantRows) || IsConstantRow(index2, s.constantRows) {
		return nil
	}
	pearson, err := s.baseComparer.Compare(index1, index2)
	if err != nil {
		return err
	}
	if pearson > 0.0 {
		// This is a match, send it to the results channel
		ret := map[datatypes.RowPair]float64{}
		var pair datatypes.RowPair
		if index1 > index2 {
			pair = *datatypes.NewRowPair(index2, index1)
		} else {
			pair = *datatypes.NewRowPair(index1, index2)
		}
		ret[pair] = pearson
		s.resultChannel <- &datatypes.CorrjoinResult{
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
	s.resultChannel <- &datatypes.CorrjoinResult{
		CorrelatedPairs: map[datatypes.RowPair]float64{},
		StrideCounter:   s.strideCounter,
	}

	log.Printf("stride %d complete, stats: %+v\n", strideCounter, s.baseComparer.stats[s.strideCounter])

	return nil
}

// Shutdown gives the engine a chance to cancel running computations when it is deleted.
func (s *InProcessComparer) Shutdown() error {

	log.Println("in process comparer shutting down")

	// Send stride end to results channel
	s.resultChannel <- &datatypes.CorrjoinResult{
		CorrelatedPairs: map[datatypes.RowPair]float64{},
		StrideCounter:   s.strideCounter,
	}

	return nil
}
