package reporter

import (
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
	"time"
)

type Reporter interface {
	Initialize(config settings.CorrjoinSettings, strideCounter int, startTime time.Time, tsids []string)

	AddCorrelatedPairs(map[datatypes.RowPair]float64) error

	Flush() error
}
