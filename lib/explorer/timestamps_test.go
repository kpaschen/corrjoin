package explorer

import (
	"testing"
)

func TestConvertToUnixTime(t *testing.T) {
	tm, err := ConvertToUnixTime(1740986452)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "2025-03-03T07:20:52.00Z"
	str := tm.UTC().Format(FORMAT)
	if str != expected {
		t.Errorf("unexpected result for timestamp: %s vs %s", str, expected)
	}

	tm, err = ConvertToUnixTime(1435781430.781)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = "2015-07-01T20:10:30.00Z"
	str = tm.UTC().Format(FORMAT)
	if str != expected {
		t.Errorf("unexpected result for timestamp: %s vs %s", str, expected)
	}
}
