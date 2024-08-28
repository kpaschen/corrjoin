package datatypes

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
