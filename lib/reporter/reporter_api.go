package reporter

import (
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
)

type Reporter interface {
	Initialize(config settings.CorrjoinSettings, tsids []string)

	AddCorrelatedPairs(map[datatypes.RowPair]float64) error

	Flush() error
}
