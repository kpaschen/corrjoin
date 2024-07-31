package lib

import (
	"fmt"
)

const (
	ALGO_FULLPEARSON = "full_pearson"
	ALGO_PAA_ONLY    = "paa_only"
	ALGO_PAA_SVD     = "paa_svd"
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

func (w *TimeseriesWindow) FullPearson(settings CorrjoinSettings) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run FullPearson on")
	}
	pairs, err := w.AllPairs(settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	totalPairs := r * r / 2

	fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	return nil
}

func (w *TimeseriesWindow) PAAOnly(settings CorrjoinSettings) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to run PAA on")
	}

	paaPairs, err := w.PAAPairs(settings.SvdDimensions, settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	totalPairs := r * r / 2

	fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(paaPairs), totalPairs,
		100*len(paaPairs)/totalPairs)
	return nil
}

func (w *TimeseriesWindow) ProcessBuffer(settings CorrjoinSettings) error {
	r := len(w.buffers)
	if r == 0 {
		return fmt.Errorf("no data to process")
	}

	w.normalizeWindow()
	w.PAA(settings.SvdDimensions)

	// Apply SVD
	_, err := w.SVD(settings.SvdOutputDimensions)
	if err != nil {
		return err
	}
	// CorrelationBuckets outputs a set of row index pairs.
	pairs, err := w.CorrelationPairs(settings.SvdDimensions, settings.EuclidDimensions,
		settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	totalPairs := r * r / 2

	fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	return nil
}
