package correlation

import (
	"math"
	"testing"
)

func TestEuclideanDistance(t *testing.T) {
	dist, err := EuclideanDistance([]float64{0.0, 0.1, 0.2}, []float64{0.0, 0.1})
	if err == nil {
		t.Fatalf("expected error computing euclidean distance of vectors of unequal length")
	}
	dist, err = EuclideanDistance([]float64{0.0, 0.1, 0.2}, []float64{0.0, 0.1, 0.2})
	if err != nil {
		t.Errorf("unexpected error in euclidean distance: %v", err)
	}
	if math.Abs(dist-0.0) > 0.0001 {
		t.Errorf("expected euclidean distance close to 0 but got %f", dist)
	}
	dist, err = EuclideanDistance([]float64{0.5, 0.1, 0.2}, []float64{0.0, 0.1, 0.2})
	if err != nil {
		t.Errorf("unexpected error in euclidean distance: %v", err)
	}
	if math.Abs(dist-0.5) > 0.0001 {
		t.Errorf("expected euclidean distance close to 0.5 but got %f", dist)
	}
}

type corrPair struct {
	x                   []float64
	y                   []float64
	expectedCorrelation float64
	expectError         bool
}

func TestPearsonCorrelation(t *testing.T) {
	pairs := []corrPair{
		{
			x:                   []float64{0.1, 0.2, 0.3},
			y:                   []float64{0.1, 0.2, 0.3},
			expectedCorrelation: 1.0,
			expectError:         false,
		},
		{
			x:                   []float64{0.3, 0.2, 0.1},
			y:                   []float64{0.1, 0.2, 0.3},
			expectedCorrelation: -1.0,
			expectError:         false,
		},
		{
			x:                   []float64{0.3, 0.2},
			y:                   []float64{0.1, 0.2, 0.3},
			expectedCorrelation: 0.0,
			expectError:         true,
		},
		{
			x:                   []float64{0.3, 0.3, 0.3},
			y:                   []float64{0.1, 0.2, 0.3},
			expectedCorrelation: 0.0,
			expectError:         false,
		},
	}
	for _, p := range pairs {
		actualCorrelation, err := PearsonCorrelation(p.x, p.y)
		if err != nil && !p.expectError {
			t.Errorf("unexpected error in correlation: %v", err)
			continue
		}
		if err == nil && p.expectError {
			t.Errorf("expected error but got correlation result %f", actualCorrelation)
			continue
		}
		if math.Abs(actualCorrelation-p.expectedCorrelation) > 0.0001 {
			t.Errorf("unexpected correlation result %f, expected %f", actualCorrelation, p.expectedCorrelation)
		}
	}
}
