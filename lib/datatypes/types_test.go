package datatypes

import (
	"testing"
)

func TestMarshalRowPair(t *testing.T) {
	rp := NewRowPair(2, 3)
	b, err := rp.MarshalJSON()
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}

	var reconstructed RowPair
	err = (&reconstructed).UnmarshalJSON(b)
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
	if reconstructed.R1 != rp.R1 || reconstructed.R2 != rp.R2 {
		t.Errorf("unexpected row pair mismatch %v vs. %v", reconstructed, rp)
	}
}

func TestMarshallCorrjoinResult(t *testing.T) {
	pairs := map[RowPair]float64{
		RowPair{R1: 0, R2: 1}: 0.01,
		RowPair{R1: 1, R2: 3}: 0.02,
		RowPair{R1: 2, R2: 5}: 0.03,
		RowPair{R1: 3, R2: 7}: 0.04,
	}
	cr := &CorrjoinResult{
		CorrelatedPairs: pairs,
		StrideCounter:   1,
	}

	b, err := cr.MarshalJSON()
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}

	var reconstructed CorrjoinResult
	err = (&reconstructed).UnmarshalJSON(b)
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
	if len(reconstructed.CorrelatedPairs) != len(pairs) {
		t.Errorf("reconstructed result has wrong number of pairs")
	}
	if reconstructed.StrideCounter != cr.StrideCounter {
		t.Errorf("stride counter mismatch")
	}
	for rp, v := range pairs {
		rv, exists := reconstructed.CorrelatedPairs[rp]
		if !exists {
			t.Errorf("missing rowpair %v in reconstructed correlated pairs", rp)
		}
		if rv != v {
			t.Errorf("value mismatch in correlated pairs")
		}
	}
}
