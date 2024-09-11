package comparisons

import (
	"github.com/kpaschen/corrjoin/lib/settings"
	"testing"
)

func TestCompare(t *testing.T) {
	rows := make(map[int][]float64)
	rows[1] = []float64{0.1, 1.2, 2.3}
	rows[2] = []float64{1.1, 2.2, 3.3}
	rows[3] = []float64{1.1, 2.2, 3.3}
	bc := NewBaseComparer(
		settings.CorrjoinSettings{EuclidDimensions: 3},
		0,
		rows,
	)
	corr, err := bc.Compare(0, 1)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if corr != 0.0 {
		t.Errorf("expected correlation 0.0 but got %f", corr)
	}

	corr, err = bc.Compare(1, 2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if corr != 0.0 {
		t.Errorf("expected correlation 0.0 but got %f", corr)
	}

	corr, err = bc.Compare(3, 2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if corr != 1.0 {
		t.Errorf("expected correlation 1.0 but got %f", corr)
	}
}
