package explorer

import (
	"github.com/prometheus/common/model"
)

type Metric struct {
	Fingerprint        uint64 // redundant if it's the cache key
	RowId              int    // not needed if we write the fingerprint to parquet
	LabelSet           model.LabelSet
	PrometheusGraphURL string // computed on demand
	Constant           bool
}

type SubgraphMemberships struct {
	// Rows maps timeseries ids to subgraph ids
	Rows map[int]int
	// Sizes holds the size of each subgraph
	Sizes          map[int]int
	nextSubgraphId int
}
