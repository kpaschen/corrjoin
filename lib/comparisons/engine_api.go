// Package comparisons contains different execution engines for
// pairwise comparison of timeseries.
package comparisons

import (
	"github.com/kpaschen/corrjoin/lib/settings"
)

type RowPair struct {
	r1 int
	r2 int
}

type CorrjoinResult struct {
	CorrelatedPairs map[RowPair]float64
	StrideCounter   int
}

func (r RowPair) RowIds() [2]int {
	return [2]int{r.r1, r.r2}
}

func NewRowPair(r1 int, r2 int) *RowPair {
	return &RowPair{r1: r1, r2: r2}
}

// An engine can take pairs of timeseries. It compares them and returns the results.
type Engine interface {

	// Initialize provides the engine with settings and a channel for results.
	Initialize(config settings.CorrjoinSettings, results chan<- *CorrjoinResult)

	// StartStride tells the engine that subsequent comparisons are for the new stride.
	StartStride(normalizedMatrix [][]float64, constantRows []bool, strideCounter int) error

	// Compare asks for a comparison of the rows identified by index1 and index2 in the
	// normalized matrix.
	Compare(index1 int, index2 int) error

	// StopStride tells the engine that no further comparisons will be requested for the old stride.
	// The engine may still send comparison results for the old stride to the results channel.
	// The engine may release memory and report usage statistics for the old stride after this point.
	StopStride(strideCounter int) error

	// Shutdown gives the engine a chance to cancel running computations when it is deleted.
	Shutdown() error
}
