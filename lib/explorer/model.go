package explorer

import (
	"github.com/prometheus/common/model"
)

type Metric struct {
	Fingerprint        uint64 // this is a stable id for the metric.
	RowId              int    // not needed, use the Fingerprint
	LabelSet           model.LabelSet
	PrometheusGraphURL string // computed on demand
	Constant           bool
}

type SubgraphMemberships struct {
	// Rows maps timeseries ids to subgraph ids
	Rows map[uint64]int
	// Sizes holds the size of each subgraph
	Sizes          map[int]int
	nextSubgraphId int
}

type Edge struct {
	Source  uint64
	Target  uint64
	Pearson float32
}
