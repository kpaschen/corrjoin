package reporter

import (
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/settings"
)

type Reporter interface {
	Initialize(config settings.CorrjoinSettings, tsids []string)

	AddCorrelatedPairs(map[comparisons.RowPair]float64) error

	Flush() error
}
