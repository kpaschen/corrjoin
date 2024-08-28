package lib

import (
	"fmt"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
	"math"
	"testing"
)

var (
	config = settings.CorrjoinSettings{
		Algorithm: settings.ALGO_NONE,
	}
	comparer = &comparisons.InProcessComparer{}
)

func TestNormalizeWindow(t *testing.T) {
	config.WindowSize = 3
	config.MaxRowsForSvd = 2
	results := make(chan *datatypes.CorrjoinResult, 1)
	defer close(results)
	comparer.Initialize(config, results)
	tswindow := &TimeseriesWindow{
		settings: config,
		comparer: comparer,
	}
	bufferWindow := [][]float64{
		[]float64{0.1, 0.2, 0.3},
		[]float64{1.1, 1.2, 1.3},
		[]float64{2.1, 2.2, 2.3},
	}

	err := tswindow.ShiftBuffer(bufferWindow, results)
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
	tswindow.normalizeWindow()

	for _, b := range tswindow.normalized {
		if len(b) != 3 {
			t.Errorf("row should have length three but is %v", b)
		}
		if math.Abs(b[1]) > 0.0001 {
			t.Errorf("middle value should be close to zero but is %f", b[1])
		}
		if math.Abs(b[0]+b[2]) > 0.0001 {
			t.Errorf("outer values should sum to zero but are %f %f", b[0], b[2])
		}
	}

	if !matrixEqual(tswindow.buffers, bufferWindow, 0.001) {
		t.Errorf("normalization should have left the raw data alone but bufferWindow is %v and buffers are %v",
			bufferWindow, tswindow.buffers)
	}
}

func matrixEqual(a [][]float64, b [][]float64, epsilon float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i, r := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j, c := range r {
			if math.Abs(c-b[i][j]) > epsilon {
				return false
			}
		}
	}
	return true
}

func TestShiftBuffer(t *testing.T) {
	config.WindowSize = 3
	results := make(chan *datatypes.CorrjoinResult, 1)
	defer close(results)
	comparer.Initialize(config, results)
	tswindow := &TimeseriesWindow{
		settings: config,
		comparer: comparer,
	}
	bufferWindow := [][]float64{
		[]float64{0.1, 0.2, 0.3},
		[]float64{1.1, 1.2, 1.3},
		[]float64{2.1, 2.2, 2.3},
	}

	fmt.Printf("got here\n")

	err := tswindow.ShiftBuffer(bufferWindow, results)

	if err != nil {
		t.Errorf("unexpected error %v shifting buffer into time series window", err)
	}

	if !matrixEqual(bufferWindow, tswindow.buffers, 0.001) {
		t.Errorf("initial shiftbuffer should have copied %v but got %v", bufferWindow, tswindow.buffers)
	}

	wrongSizeBuffer := [][]float64{
		[]float64{0.4, 0.5},
	}
	err = tswindow.ShiftBuffer(wrongSizeBuffer, results)
	if err == nil {
		t.Errorf("expected error for mismatched buffer shift")
	}

	strideBuffer := [][]float64{
		[]float64{0.4},
		[]float64{1.4},
		[]float64{2.4},
	}
	err = tswindow.ShiftBuffer(strideBuffer, results)
	if err != nil {
		t.Errorf("unexpected error %v shifting buffer into ts window", err)
	}

	expected := [][]float64{
		[]float64{0.2, 0.3, 0.4},
		[]float64{1.2, 1.3, 1.4},
		[]float64{2.2, 2.3, 2.4},
	}
	if !matrixEqual(expected, tswindow.buffers, 0.001) {
		t.Errorf("after ShiftBuffer: expected %v but got %v", expected, tswindow.buffers)
	}

}

func TestPAA(t *testing.T) {
	config.WindowSize = 4
	config.SvdDimensions = 2
	results := make(chan *datatypes.CorrjoinResult, 1)
	defer close(results)
	comparer.Initialize(config, results)
	tswindow := &TimeseriesWindow{
		settings: config,
		comparer: comparer,
	}
	bufferWindow := [][]float64{
		[]float64{0.1, 0.2, 0.3, 0.4},
		[]float64{1.1, 1.2, 1.3, 1.4},
		[]float64{2.1, 2.2, 2.3, 2.4},
	}

	// tswindow.ShiftBuffer(bufferWindow, results)

	tswindow.buffers = bufferWindow
	// Cheat a little just to make the values easier to check.
	tswindow.normalized = tswindow.buffers

	tswindow.pAA()
	if len(tswindow.postPAA) != 3 {
		t.Fatalf("expected post-PAA matrix to have three rows but it has %d", len(tswindow.postPAA))
	}
	if len(tswindow.postPAA[0]) != 2 {
		t.Errorf("expected post-PAA matrix to have two columns but it has %d", len(tswindow.postPAA[0]))
	}

	expectedData := [][]float64{
		[]float64{0.15, 0.35},
		[]float64{1.15, 1.35},
		[]float64{2.15, 2.35},
	}
	if !matrixEqual(expectedData, tswindow.postPAA, 0.0001) {
		t.Errorf("expected post-PAA matrix to be %+v but got %v", expectedData, tswindow.postPAA)
	}
}

func TestSVD(t *testing.T) {
	// Input matrix A has 2 rows, 3 columns
	// U should be 2x2, V should be 3x3
	// But we compute ThinV, so V is only 3x2 because there's just 2 nonzero eigenvalues.
	config.WindowSize = 3
	config.SvdDimensions = 3
	config.SvdOutputDimensions = 2
	config.MaxRowsForSvd = 10000
	results := make(chan *datatypes.CorrjoinResult, 1)
	defer close(results)
	comparer.Initialize(config, results)
	tswindow := &TimeseriesWindow{
		settings: config,
		comparer: comparer,
	}
	bufferWindow := [][]float64{
		[]float64{3.0, 2.0, 2.0},
		[]float64{2.0, 3.0, -2.0},
	}

	tswindow.ShiftBuffer(bufferWindow, results)
	tswindow.postPAA = bufferWindow

	svd, err := tswindow.sVD()
	if svd == nil {
		t.Errorf("svd is not supposed to fail")
	}
	if err != nil {
		t.Errorf("svd returned error %v", err)
	}
	fmt.Printf("post svd %+v\n", svd.postSVD)

	// Expect two rows, two columns
	if len(svd.postSVD) != 2 || len(svd.postSVD[0]) != 2 {
		t.Errorf("expected postsvd matrix to be 2x2 but it is %d x %d", len(svd.postSVD), len(svd.postSVD[0]))
	}

	// In this result, the values in the first column are the same.
	if math.Abs(svd.postSVD[0][0]-svd.postSVD[1][0]) > 0.0001 {
		t.Errorf("expected first column of post svd matrix to be all the same")
	}

	// The values in the second column are the same except for the sign.
	if math.Abs(svd.postSVD[0][1]+svd.postSVD[1][1]) > 0.0001 {
		t.Errorf("expected second column of post svd matrix to be all the same except for the sign")
	}
}

func TestCorrelationPairs(t *testing.T) {
	initialTsData := [][]float64{
		[]float64{-0.1, 0.8, -0.9, 1.4},
		[]float64{1.1, 1.2, 1.3, 1.4},
		[]float64{1.1, 1.2, 1.3, 1.4},
		[]float64{2.3, -8.2, 1.3, 0.4},
	}
	config.WindowSize = 4
	config.SvdDimensions = 4
	config.SvdOutputDimensions = 4
	config.EuclidDimensions = 3
	config.CorrelationThreshold = 0.9
	config.ComputeSettingsFields()
	results := make(chan *datatypes.CorrjoinResult, 1)
	defer close(results)
	comparer.Initialize(config, results)
	tswindow := &TimeseriesWindow{
		settings: config,
		buffers:  initialTsData,
		comparer: comparer,
	}
	tswindow.normalizeWindow()
	tswindow.postSVD = tswindow.normalized
	comparer.StartStride(tswindow.normalized, tswindow.constantRows, tswindow.StrideCounter)
	found := false
	go func() {
		for true {
			result, ok := <-results
			if ok && len(result.CorrelatedPairs) == 1 {
				found = true
			}
			if !ok {
				break
			}
		}
	}()

	err := tswindow.correlationPairs()
	if err != nil {
		t.Errorf("unexpected error in correlationpairs: %v", err)
	}

	if !found {
		t.Errorf("expected to find a correlated pair")
	}
}
