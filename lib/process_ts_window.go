package lib

const (
	ALGO_FULL_PEARSON = "full_pearson"
	ALGO_PAA_ONLY     = "paa_only"
	ALGO_PAA_SVD      = "paa_svd"
	ALGO_NONE         = "none" // for tests
)

// The stride length is implicit in the width of the buffer.
type CorrjoinSettings struct {
	// The number of dimensions used for SVD (aka ks)
	SvdDimensions int
	// The number of dimensions to choose from the output of SVD (aka kb)
	SvdOutputDimensions int
	// The number of dimensions used for euclidean distance (aka ke)
	EuclidDimensions     int
	CorrelationThreshold float64 // aka T
	WindowSize           int     // aka n
	Algorithm            string
}
