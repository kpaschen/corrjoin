package explorer

import (
	"testing"
)

func TestParseIds(t *testing.T) {
	explorer := CorrelationExplorer{}
	ids, err := explorer.parseTsIdsFromGraphiteResult("{77,12}")
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("expected two ids but got %v", ids)
	}

	if ids[0] != 77 || ids[1] != 12 {
		t.Errorf("expected 77,11 but got %v", ids)
	}

	ids, err = explorer.parseTsIdsFromGraphiteResult("9821293140777017000")

	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
}
