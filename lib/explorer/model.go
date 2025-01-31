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
