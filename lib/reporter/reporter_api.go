package reporter

import (
	"github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"time"
)

type Reporter interface {
	InitializeStride(strideCounter int,
		startTime time.Time, endTime time.Time)

	RecordTimeseriesIds(strideCounter int, tsids []lib.TsId)

	AddCorrelatedPairs(datatypes.CorrjoinResult) error

	Flush(strideCounter int) error
}
