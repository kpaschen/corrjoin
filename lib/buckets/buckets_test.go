package buckets

import (
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
	"testing"
)

func setupBucketingScheme() (*BucketingScheme, chan *datatypes.CorrjoinResult) {
	originalMatrix := [][]float64{
		[]float64{0.1, 0.2, 0.3},
		[]float64{0.1, 0.2, 0.3},
		[]float64{2.1, 2.2, 2.3},
	}
	svdOutputMatrix := [][]float64{
		[]float64{0.1, 0.2},
		[]float64{0.1, 0.2},
		[]float64{2.1, 2.2},
	}
	settings := settings.CorrjoinSettings{
		SvdDimensions:        3,
		EuclidDimensions:     2,
		CorrelationThreshold: 0.9,
		WindowSize:           3,
		SvdOutputDimensions:  2,
	}.ComputeSettingsFields()
	comparer := &comparisons.InProcessComparer{}
	results := make(chan *datatypes.CorrjoinResult)
	comparer.Initialize(settings, results)
	err := comparer.StartStride(originalMatrix, []bool{}, 0)
	if err != nil {
		panic(err)
	}
	return NewBucketingScheme(originalMatrix, svdOutputMatrix, []bool{}, settings, 0, comparer), results
}

func TestBucketIndex(t *testing.T) {
	scheme, _ := setupBucketingScheme()
	b := scheme.BucketIndex(0.0)
	if b != 0 {
		t.Errorf("expected bucket 0 for value 0.0 but got %d", b)
	}
	// With these settings, the bucket width is about 0.3
	b = scheme.BucketIndex(0.5)
	if b != 1 {
		t.Errorf("expected bucket 1 for value 0.5 but got %d", b)
	}
}

func TestInitialize(t *testing.T) {
	scheme, _ := setupBucketingScheme()
	err := scheme.Initialize()
	if err != nil {
		t.Errorf("unexpected error in bucket scheme initialization: %v", err)
	}
	if len(scheme.buckets) != 2 {
		t.Errorf("expected two buckets but got %d", len(scheme.buckets))
	}
}

func TestNeighbourCoordinates(t *testing.T) {
	x := neighbourCoordinates([]int{1, 2, 3})
	if len(x) != 26 {
		t.Errorf("unexpected number of neighbours %d", len(x))
	}
	x = neighbourCoordinates([]int{1, 2, 3, 4})
	if len(x) != 80 {
		t.Errorf("unexpected number of neighbours %d", len(x))
	}
}

func TestIsNeighbour(t *testing.T) {
	n := isNeighbour([]int{1, 2, 3}, []int{1, 2, 3})
	if n {
		t.Errorf("a bucket cannot be its own neighbour")
	}
	n = isNeighbour([]int{1, 2, 3}, []int{1, 2, 3, 4})
	if n {
		t.Errorf("buckets of different length cannot be neighbours")
	}
	n = isNeighbour([]int{1, 2, 3}, []int{1, 2, 5})
	if n {
		t.Errorf("buckets this far apart cannot be neighbours")
	}
	n = isNeighbour([]int{1, 2, 3}, []int{1, -2, 3})
	if n {
		t.Errorf("buckets this far apart cannot be neighbours")
	}
	for _, x := range neighbourCoordinates([]int{1, 2, 3}) {
		n = isNeighbour([]int{1, 2, 3}, x)
		if !n {
			t.Errorf("%v should have been a neighbour", x)
		}
	}
}

func TestCorrelationCandidates(t *testing.T) {
	scheme, resultChannel := setupBucketingScheme()
	scheme.Initialize()
	go func() {
		found := false
		for true {
			results, ok := <-resultChannel
			if !ok {
				break
			}
			if ok && len(results.CorrelatedPairs) > 1 {
				t.Errorf("expected one correlated pair to be found but got %d", len(results.CorrelatedPairs))
			}
			for rowPair, correlation := range results.CorrelatedPairs {
				ids := rowPair.RowIds()
				if ids[0] != 0 || ids[1] != 1 {
					t.Errorf("expected rows 0 and 1 to be correlated but got %d %d", ids[0], ids[1])
				}
				if correlation != 1 {
					t.Errorf("expected perfect correlation but got %f", correlation)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("expected one correlated pair")
		}
	}()
	err := scheme.CorrelationCandidates()
	if err != nil {
		t.Errorf("unexpected error in CorrelationCandidates: %v", err)
	}
}
