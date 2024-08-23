package reporter

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"testing"
)

func printCorrelations(r SetReporter) {
	fmt.Println("===")
	for _, c := range r.correlations {
		fmt.Printf("corrset: %+v\n", *c)
	}
	fmt.Println("===")
}

func TestAddCorrelatedPair(t *testing.T) {
	rep := NewSetReporter()

	// Add a new pair
	b := comparisons.NewRowPair(1, 2)
	err := rep.addCorrelatedPair(*b, 0.1)
	if err != nil {
		t.Errorf("failed to add correlated pair: %v", err)
	}
	if len(rep.correlations) != 1 {
		t.Errorf("expected one correlation set but got %d", len(rep.correlations))
	}
	printCorrelations(*rep)

	// Add a second new pair
	b = comparisons.NewRowPair(3, 4)
	err = rep.addCorrelatedPair(*b, 0.2)
	if err != nil {
		t.Errorf("failed to add correlated pair: %v", err)
	}
	if len(rep.correlations) != 2 {
		t.Errorf("expected two correlation sets but got %d", len(rep.correlations))
	}
	printCorrelations(*rep)

	// Add a pair with overlap.
	b = comparisons.NewRowPair(3, 5)
	err = rep.addCorrelatedPair(*b, 0.3)
	if err != nil {
		t.Errorf("failed to add correlated pair: %v", err)
	}
	if len(rep.correlations) != 2 {
		t.Errorf("expected two correlation sets but got %d", len(rep.correlations))
	}
	printCorrelations(*rep)

	// Add a redundant pair
	b = comparisons.NewRowPair(4, 5)
	err = rep.addCorrelatedPair(*b, 0.4)
	if err != nil {
		t.Errorf("failed to add correlated pair: %v", err)
	}
	if len(rep.correlations) != 2 {
		t.Errorf("expected two correlation sets but got %d", len(rep.correlations))
	}
	printCorrelations(*rep)

	// Add a pair that forces a merge
	b = comparisons.NewRowPair(2, 5)
	err = rep.addCorrelatedPair(*b, 0.5)
	if err != nil {
		t.Errorf("failed to add correlated pair: %v", err)
	}
	if len(rep.correlations) != 2 {
		t.Errorf("expected two correlation sets but got %d", len(rep.correlations))
	}
	if len(rep.correlations[0].members) > 0 && len(rep.correlations[1].members) > 0 {
		t.Errorf("expected one of the member lists to be empty")
	}
	printCorrelations(*rep)

	// Add another pair after merging.
	b = comparisons.NewRowPair(6, 7)
	err = rep.addCorrelatedPair(*b, 0.6)
	if err != nil {
		t.Errorf("failed to add correlated pair: %v", err)
	}
	if len(rep.correlations) != 3 {
		t.Errorf("expected three correlation sets but got %d", len(rep.correlations))
	}
	printCorrelations(*rep)
}
