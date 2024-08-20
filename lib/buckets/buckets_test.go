package buckets

import (
	"github.com/kpaschen/corrjoin/lib/settings"
	"testing"
)

func setupBucketingScheme() (*BucketingScheme, chan *CorrjoinResult) {
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
	replies := make(chan *CorrjoinResult, 1)
	settings := settings.CorrjoinSettings{
		SvdDimensions:        3,
		EuclidDimensions:     2,
		CorrelationThreshold: 0.9,
		WindowSize:           3,
		SvdOutputDimensions:  2,
	}
	return NewBucketingScheme(originalMatrix, svdOutputMatrix, []bool{}, settings.ComputeSettingsFields(), 0, replies), replies
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
				if rowPair.r1 != 0 || rowPair.r2 != 1 {
					t.Errorf("expected rows 0 and 1 to be correlated but got %d %d", rowPair.r1, rowPair.r2)
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
