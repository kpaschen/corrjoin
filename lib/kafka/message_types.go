package kafka

import (
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
)

type PairMessage struct {
	StrideCounter int
	Config        settings.CorrjoinSettings
	Pairs         []datatypes.RowPair
	Rows          map[int][]float64
}
