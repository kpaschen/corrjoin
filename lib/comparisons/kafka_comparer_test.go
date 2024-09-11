package comparisons

import (
	"encoding/json"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/kafka"
	"github.com/kpaschen/corrjoin/lib/settings"
	"testing"
)

func rowEquals(expected []float64, actual []float64) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, f := range expected {
		if f != actual[i] {
			return false
		}
	}
	return true
}

func TestEncodePairMessage(t *testing.T) {
	comparer := &KafkaComparer{
		config:        settings.CorrjoinSettings{},
		strideCounter: 123,
		normalizedMatrix: [][]float64{
			{0.0, 0.1, 0.2},
			{0.1, 0.2, 0.3},
			{0.2, 0.3, 0.4},
			{1.2, 0.3, 0.4},
			{0.2, 1.3, 0.4},
			{0.2, 0.3, 1.4},
		},
		candidateBuffer: make([]datatypes.RowPair, 0, 10),
	}
	comparer.candidateBuffer = append(comparer.candidateBuffer,
		*datatypes.NewRowPair(0, 1))
	comparer.candidateBuffer = append(comparer.candidateBuffer,
		*datatypes.NewRowPair(0, 5))

	msg, err := comparer.encodePairMessage()
	if err != nil {
		t.Errorf("unexpected error in encodePairMessage: %v", err)
	}

	var res kafka.PairMessage
	err = json.Unmarshal(msg, &res)
	if err != nil {
		t.Errorf("unexpected error decoding pair message: %v", err)
	}
	if len(res.Pairs) != 2 {
		t.Errorf("expected 2 pairs but got %d", len(res.Pairs))
	}
	ids := res.Pairs[0].RowIds()
	if ids != [2]int{0, 1} {
		t.Errorf("expected first pair to be 0,1 but got %v", ids)
	}
	ids = res.Pairs[1].RowIds()
	if ids != [2]int{0, 5} {
		t.Errorf("expected second pair to be 0,5 but got %v", ids)
	}
	if len(res.Rows) != 3 {
		t.Errorf("expected 3 rows but got %d", len(res.Rows))
	}
	row, ok := res.Rows[0]
	if !ok {
		t.Errorf("failed to find row 0")
	}
	if !rowEquals(row, comparer.normalizedMatrix[0]) {
		t.Errorf("row mismatch on row 0: %v vs %v", row, comparer.normalizedMatrix[0])
	}
	row, ok = res.Rows[1]
	if !ok {
		t.Errorf("failed to find row 1")
	}
	if !rowEquals(row, comparer.normalizedMatrix[1]) {
		t.Errorf("row mismatch on row 1: %v vs %v", row, comparer.normalizedMatrix[1])
	}
	row, ok = res.Rows[5]
	if !ok {
		t.Errorf("failed to find row 5")
	}
	if !rowEquals(row, comparer.normalizedMatrix[5]) {
		t.Errorf("row mismatch on row 5: %v vs %v", row, comparer.normalizedMatrix[5])
	}
}
