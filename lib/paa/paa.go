package paa

import (
	"gonum.org/v1/gonum/mat"
	"log"
	"math"
)

func sumVec(vector mat.Vector) float64 {
	ret := 0.0
	for i := 0; i < vector.Len(); i++ {
		ret += vector.AtVec(i)
	}
	return ret
}

func sum(slice []float64) float64 {
	ret := 0.0
	for i := 0; i < len(slice); i++ {
		ret += slice[i]
	}
	return ret
}

func mean(slice []float64) float64 {
	return sum(slice) / float64(len(slice))
}

func isSliceConstant(row []float64, epsilon float64) bool {
	if epsilon < 0.0 {
		return false
	}
	if len(row) == 0 {
		return true
	}
	firstValue := row[0]
	for _, v := range row[1:] {
		if math.Abs(v-firstValue) > epsilon {
			return false
		}
	}
	return true
}

// NormalizeSlice modifies slice by normalizing it using an l2-norm.
// It returns true if the resulting slice is constant to within
// 0.0001.
func NormalizeSlice(slice []float64) bool {
	length := len(slice)
	avg := mean(slice)
	var sumOfSquares float64

	for i := 0; i < length; i++ {
		diff := slice[i] - avg
		sumOfSquares += diff * diff
	}
	normalizingFactor := math.Sqrt(sumOfSquares)
	if normalizingFactor == 0.0 {
		for i := 0; i < length; i++ {
			slice[i] = 0.0
		}
		return true
	} else {
		for i := 0; i < length; i++ {
			diff := slice[i] - avg
			slice[i] = diff / normalizingFactor
		}
		return isSliceConstant(slice, 0.0001)
	}
}

// Reduce slice to targetColumnCount columns by dividing it into
// equi-length segments and using mean values.
func PAA(slice []float64, targetColumnCount int) ([]float64, bool) {
	windowSize := len(slice) / targetColumnCount
	if windowSize < 1 {
		log.Printf("window size is too small. Slice length %d divided by targetColumnCount %d is %d\n", len(slice), targetColumnCount, windowSize)
		return nil, true
	}
	ret := make([]float64, targetColumnCount, targetColumnCount)

	for i := 0; i < targetColumnCount; i++ {
		ret[i] = mean(slice[(i * windowSize):((i + 1) * windowSize)])
	}
	constant := isSliceConstant(ret, 0.0001)
	return ret, constant
}
