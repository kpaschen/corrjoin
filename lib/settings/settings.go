// Package settings contains all the parameters for the corrjoin algorithm.
package settings

import (
	"math"
)

const (
	ALGO_FULL_PEARSON = "full_pearson"
	ALGO_PAA_ONLY     = "paa_only"
	ALGO_PAA_SVD      = "paa_svd"
	ALGO_NONE         = "none" // for tests
)

type CorrjoinSettings struct {
	// The number of columns used for the first PAA step.
	// Equals the number of columns in the svd input matrix
	SvdDimensions int // also Ks
	// The number of columns to use for the second PAA step.
	EuclidDimensions int // also Ke
	// The correlation Threshold (T)
	CorrelationThreshold float64

	// The number of columns in the input matrix.
	// Equals the window size.
	WindowSize int // also OriginalColumnCount

	// The number of dimensions to choose from the output of SVD (aka kb)
	SvdOutputDimensions int // == Dimensions

	// Thresholds for the two filtering steps
	// epsilon1 = sqrt(2 ks (1 - correlationThreshold) / n)
	// where n = originalColumnCount
	Epsilon1 float64
	// epsilon2 = sqrt(2 ke (1 - correlationThreshold) / n)
	Epsilon2 float64 // this is for the euclidean distance filter

	// Cap on number of rows for svd. 10000 is a good number.
	// This is an implementation detail that works around a limitation
	// on the lapack implementation where svd fails on very large
	// matrices.
	MaxRowsForSvd int

	// How often we expect new samples, in seconds.
	SampleInterval int

	// Number of rows per row group in Parquet.
	// Bigger number mean more memory usage but better compression.
	// 10000 works but outputs are about twice the size of an equivalent csv file.
  // This is an int64 because the parquet library takes that type, not because I think
  // anything > maxint32 is a good choice.
	MaxRowsPerRowGroup int64

	Algorithm string
}

func (s CorrjoinSettings) ComputeSettingsFields() CorrjoinSettings {
	s.Epsilon1 = math.Sqrt(float64(2*s.SvdDimensions) * (1.0 - s.CorrelationThreshold) / float64(s.WindowSize))
	s.Epsilon2 = math.Sqrt(float64(2*s.EuclidDimensions) * (1.0 - s.CorrelationThreshold) / float64(s.WindowSize))
	if s.Algorithm == "" {
		s.Algorithm = ALGO_NONE
	}
	if s.MaxRowsForSvd == 0 {
		s.MaxRowsForSvd = 10000
	}
	if s.SampleInterval == 0 {
		s.SampleInterval = 20
	}
	if s.MaxRowsPerRowGroup == 0 {
		s.MaxRowsPerRowGroup = 100000
	}
	return s
}
