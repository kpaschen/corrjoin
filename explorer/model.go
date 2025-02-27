package explorer

import (
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
)

type StrideState int

const (
	StrideExists = iota
	StrideRead
	StrideProcessing
	StrideProcessed
	StrideRetrying
	StrideError
	StrideDeleted
)

var strideStateName = map[StrideState]string{
	StrideExists:    "exists",
	StrideRead:      "read",
	StrideProcessed: "processed",
	StrideRetrying:  "retrying",
	StrideError:     "error",
	StrideDeleted:   "deleted",
}

func (s StrideState) String() string {
	return strideStateName[s]
}

// Stride collects metadata about a stride.
type Stride struct {
	ID              int
	StartTime       int64
	EndTime         int64
	StartTimeString string
	EndTimeString   string
	Status          StrideState
	Filename        string
	subgraphs       *explorerlib.SubgraphMemberships
}

type PromQueryResponse struct {
	Status string `json:"status"`
	Data   Data   `json:"data"`
}

// Data contains the result type and the actual result.
type Data struct {
	ResultType string   `json:"resultType"`
	Result     []Result `json:"result"`
}

// Result represents each time series in the response.
type Result struct {
	Metric map[string]string `json:"metric"`
	Values [][]interface{}   `json:"values"`
}
