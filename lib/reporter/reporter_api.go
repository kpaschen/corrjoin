package reporter

import (
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
	"time"
)

type Reporter interface {
	Initialize(config settings.CorrjoinSettings, strideCounter int,
		 startTime time.Time, endTime time.Time, tsids []string)

	AddCorrelatedPairs(datatypes.CorrjoinResult) error

	Flush() error
}
