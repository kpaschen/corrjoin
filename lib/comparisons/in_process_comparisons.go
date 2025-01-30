// Package comparisons contains different execution engines for
// pairwise comparison of timeseries.
package comparisons

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
	"log"
)

const (
	BUFFER_SIZE = 1000
)

// An InProcessComparer implements Engine.
type InProcessComparer struct {
	// These settings remain for the lifetime of an engine.
	config        settings.CorrjoinSettings
	constantRows  []bool
	resultChannel chan<- *datatypes.CorrjoinResult

	baseComparer  *BaseComparer
	strideCounter int
	resultsBuffer map[datatypes.RowPair]float64
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
			paa2:             make(map[int][]float64),
			constantPostPaa2: make(map[int]bool),
			stats:            StrideStats{},
		}
	}
	s.strideCounter = strideCounter
	s.constantRows = constantRows
	s.baseComparer.normalizedMatrix = normalizedMatrix
	s.baseComparer.config = s.config
	s.baseComparer.strideCounter = strideCounter
	s.baseComparer.stats = StrideStats{
		comparisons: 0,
		correlated:  0,
	}
	s.resultsBuffer = make(map[datatypes.RowPair]float64)

	return nil
}

// Compare asks for a comparison of the rows identified by index1 and index2 in the
// normalized matrix.
func (s *InProcessComparer) Compare(index1 int, index2 int) error {
	if s.strideCounter < 0 {
		return fmt.Errorf("asked for comparison but there is no current stride")
	}
	if IsConstantRow(index1, s.constantRows) || IsConstantRow(index2, s.constantRows) {
		if IsConstantRow(index1, s.constantRows) {
			log.Printf("%d is constant\n", index1)
		} else {
			log.Printf("%d is constant\n", index2)
		}
		return nil
	}
	pearson, err := s.baseComparer.Compare(index1, index2)
	if err != nil {
		return err
	}
	if pearson > 0.0 {
		// This is a match, put it into our buffer.
		var pair datatypes.RowPair
		if index1 > index2 {
			pair = *datatypes.NewRowPair(index2, index1)
		} else {
			pair = *datatypes.NewRowPair(index1, index2)
		}
		s.resultsBuffer[pair] = pearson

		if len(s.resultsBuffer) >= BUFFER_SIZE {
			s.resultChannel <- &datatypes.CorrjoinResult{
				CorrelatedPairs: s.resultsBuffer,
				StrideCounter:   s.strideCounter,
			}
			s.resultsBuffer = make(map[datatypes.RowPair]float64)
		}
	}
	return nil
}

func (s *InProcessComparer) StopStride(strideCounter int) error {
	if strideCounter != s.strideCounter {
		return fmt.Errorf("trying to stop stride %d but i am processing stride %d",
			strideCounter, s.strideCounter)
	}
	// If we have anything left in the buffer, send it now.
	if len(s.resultsBuffer) > 0 {
		s.resultChannel <- &datatypes.CorrjoinResult{
			CorrelatedPairs: s.resultsBuffer,
			StrideCounter:   s.strideCounter,
		}
	}
	// Send stride end to results channel
	s.resultChannel <- &datatypes.CorrjoinResult{
		CorrelatedPairs: map[datatypes.RowPair]float64{},
		StrideCounter:   s.strideCounter,
	}

	log.Printf("stride %d complete, stats: %+v\n", strideCounter, s.baseComparer.stats)

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
