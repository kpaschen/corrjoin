package datatypes

import (
	"encoding/json"
)

// RowPair is a pair of row indices.
// The fields are public because this struct gets
// json-encoded.
type RowPair struct {
	R1 int
	R2 int
}

type CorrjoinResult struct {
	CorrelatedPairs map[RowPair]float64
	StrideCounter   int
}

func (r RowPair) RowIds() [2]int {
	return [2]int{r.R1, r.R2}
}

func NewRowPair(r1 int, r2 int) *RowPair {
	return &RowPair{R1: r1, R2: r2}
}

func (r *RowPair) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		R1 int `json:"r1"`
		R2 int `json:"r2"`
	}{
		R1: r.R1,
		R2: r.R2,
	})
}

func (r *RowPair) UnmarshalJSON(data []byte) error {
	rp := &struct {
		R1 int `json:"r1"`
		R2 int `json:"r2"`
	}{}
	if err := json.Unmarshal(data, &rp); err != nil {
		return err
	}
	r.R1 = rp.R1
	r.R2 = rp.R2
	return nil
}

func translateMap(pairs map[RowPair]float64) map[string]float64 {
	ret := make(map[string]float64)
	for key, val := range pairs {
		k, _ := (&key).MarshalJSON()
		ret[string(k[:])] = val
	}
	return ret
}

func retranslateMap(pairs map[string]float64) map[RowPair]float64 {
	ret := make(map[RowPair]float64)
	var rp RowPair
	for key, val := range pairs {
		(&rp).UnmarshalJSON([]byte(key))
		ret[rp] = val
	}
	return ret
}

func (c *CorrjoinResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		CorrelatedPairs map[string]float64 `json:"correlatedPairs"`
		StrideCounter   int                `json:"strideCounter"`
	}{
		CorrelatedPairs: translateMap(c.CorrelatedPairs),
		StrideCounter:   c.StrideCounter,
	})
}

func (c *CorrjoinResult) UnmarshalJSON(data []byte) error {
	cr := &struct {
		CorrelatedPairs map[string]float64 `json:"correlatedPairs"`
		StrideCounter   int                `json:"strideCounter"`
	}{}
	if err := json.Unmarshal(data, &cr); err != nil {
		return err
	}
	c.StrideCounter = cr.StrideCounter
	c.CorrelatedPairs = retranslateMap(cr.CorrelatedPairs)
	return nil
}
