package paa

import (
	"gonum.org/v1/gonum/mat"
	"math"
	"testing"
)

func TestSumVec(t *testing.T) {
	vec := mat.NewVecDense(3, []float64{0.1, 0.2, 0.3})
	ret := sumVec(vec)
	diff := math.Abs(ret - 0.6)
	if diff > 0.000001 {
		t.Errorf("expected vector sum to be 0.6 but got %f, difference is %f", ret, diff)
	}
}

func TestNormalizeSlice(t *testing.T) {
	vec := []float64{0.1, 0.2, 0.3}
	isConstant := NormalizeSlice(vec)

	if isConstant {
		t.Errorf("did not expect %v to be considered constant", vec)
	}

	// The average should be 0.2, so the first value
	// in the normalized vector must be negative, the
	// second close to zero, and the last one positive.
	if vec[0] >= 0.0 {
		t.Errorf("expected 0.1 to be below average but got %f", vec[0])
	}
	if math.Abs(vec[1]) > 0.0001 {
		t.Errorf("expected 0.2 to be average but got %f", vec[1])
	}
	if vec[2] <= 0.0 {
		t.Errorf("expected 0.3 to be above average but got %f", vec[2])
	}
}

func TestPAA(t *testing.T) {
	vec := []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}
	smaller, constant := PAA(vec, 3)
	if constant {
		t.Errorf("did not expect vector to be constant post PAA")
	}
	if len(smaller) != 3 {
		t.Errorf("expected reduced vector to have length 3 but it has %d", len(smaller))
	}
	if math.Abs(smaller[0]-0.15) > 0.0001 {
		t.Errorf("expected smaller[0] to be %f but it is %f", 0.15, smaller[0])
	}
	if math.Abs(smaller[1]-0.35) > 0.0001 {
		t.Errorf("expected smaller[1] to be %f but it is %f", 0.35, smaller[1])
	}
	if math.Abs(smaller[2]-0.55) > 0.0001 {
		t.Errorf("expected smaller[2] to be %f but it is %f", 0.55, smaller[2])
	}
}
