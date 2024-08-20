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

	// Initialize provides the engine with the data and settings it needs.
	Initialize(config settings.CorrjoinSettings, normalizedMatrix *[][]float64, results chan<- *CorrjoinResult)

	// Compare asks for a comparison of the rows identified by index1 and index2 in the
	// normalized matrix.
	Compare(index1 int, index2 int) error

	// Shutdown gives the engine a chance to cancel running computations when it is deleted.
	Shutdown() error
}
