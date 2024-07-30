package paa

import (
	"fmt"
	"gonum.org/v1/gonum/mat"
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

func NormalizeSlice(slice []float64) {
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
	} else {
		for i := 0; i < length; i++ {
			diff := slice[i] - avg
			slice[i] = diff / normalizingFactor
		}
	}
}

// Reduce slice to targetColumnCount columns by dividing it into
// equi-length segments and using mean values.
func PAA(slice []float64, targetColumnCount int) []float64 {
	windowSize := len(slice) / targetColumnCount
	if windowSize < 1 {
		fmt.Printf("window size is too small\n")
	}
	ret := make([]float64, targetColumnCount, targetColumnCount)

	for i := 0; i < targetColumnCount; i++ {
		ret[i] = mean(slice[(i * windowSize):((i + 1) * windowSize)])
	}
	return ret
}
