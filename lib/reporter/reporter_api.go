package reporter

import (
	"github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"time"
)

type Reporter interface {
	Initialize(strideCounter int,
		startTime time.Time, endTime time.Time, tsids []lib.TsId)

	AddCorrelatedPairs(datatypes.CorrjoinResult) error

	Flush() error
}
