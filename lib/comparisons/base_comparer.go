package comparisons

import (
	"github.com/kpaschen/corrjoin/lib/correlation"
	"github.com/kpaschen/corrjoin/lib/paa"
	"github.com/kpaschen/corrjoin/lib/settings"
	"log"
)

type StrideStats struct {
	comparisons int
	correlated  int
}

type BaseComparer struct {
	config settings.CorrjoinSettings

	// For legacy use cases, you can use a full matrix here.
	normalizedMatrix [][]float64
	normalizedRows   map[int][]float64
	strideCounter    int
	paa2             map[int][]float64

	stats StrideStats
}

func NewBaseComparer(
	config settings.CorrjoinSettings,
	strideCounter int,
	normalizedRows map[int][]float64,
) *BaseComparer {
	return &BaseComparer{
		config:         config,
		normalizedRows: normalizedRows,
		strideCounter:  strideCounter,
		paa2:           make(map[int][]float64),
		stats:          StrideStats{},
	}
}

func IsConstantRow(index int, constantRows []bool) bool {
	if constantRows != nil && len(constantRows) > index {
		return constantRows[index]
	}
	return false
}

func (b *BaseComparer) getVector(index int) []float64 {
	if b.normalizedRows == nil {
		return b.normalizedMatrix[index]
	} else {
		return b.normalizedRows[index]
	}
}

func (b *BaseComparer) Compare(index1 int, index2 int) (float64, error) {
	// TODO: cache paa2 outputs

	var vec1, vec2 []float64
	vec1 = b.getVector(index1)
	vec2 = b.getVector(index2)

	if vec1 == nil {
		log.Printf("did not find row %d in rows provided\n", index1)
		return 0.0, nil
	}
	if vec2 == nil {
		log.Printf("did not find row %d in rows provided\n", index2)
		return 0.0, nil
	}

	if len(vec1) != len(vec2) {
		log.Printf("mismatched lengths %d vs. %d for vectors %d, %d", len(vec1), len(vec2), index1, index2)
		return 0.0, nil
	}

	paaVec1, constantVec1 := paa.PAA(vec1, b.config.EuclidDimensions)
	paaVec2, constantVec2 := paa.PAA(vec2, b.config.EuclidDimensions)

	// A constant vector cannot match a non-constant one.
	if constantVec1 != constantVec2 {
		return 0.0, nil
	}

	// This test is redundant for pairs that come from the same bucket.
	distance, err := correlation.EuclideanDistance(paaVec1, paaVec2)
	if err != nil {
		return 0.0, err
	}
	if distance > b.config.Epsilon2 {
		// Not a match
		return 0.0, nil
	}
	// Now apply pearson
	b.stats.comparisons++

	// TODO: the code used to do a pearson correlation of paaVec1 and paaVec2 here,
	// but surely that will give us false positives?
	pearson, err := correlation.PearsonCorrelation(vec1, vec2)

	if err != nil {
		return 0.0, err
	}
	// log.Printf("pearson correlation of %+v and %+v is %f\n", paaVec1, paaVec2, pearson)

	if pearson >= b.config.CorrelationThreshold {
		b.stats.correlated++
		return pearson, nil
	}
	return 0.0, nil
}
