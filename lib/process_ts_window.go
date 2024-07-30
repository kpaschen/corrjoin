package lib

import (
	"fmt"
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
}

func (w *TimeseriesWindow) FullPearson(settings CorrjoinSettings,
	buffer [][]float64) error {
	// Shift _buffer_ into _window_. The stride length is equal to the
	// width of _buffer_.
	err := w.ShiftBuffer(buffer)
	if err != nil {
		return err
	}

	pairs, err := w.AllPairs(settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	r := len(buffer)
	totalPairs := r * r / 2

	fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	return nil
}

func (w *TimeseriesWindow) PAAOnly(settings CorrjoinSettings,
	buffer [][]float64) error {
	// Shift _buffer_ into _window_. The stride length is equal to the
	// width of _buffer_.
	err := w.ShiftBuffer(buffer)
	if err != nil {
		return err
	}

	paaPairs, err := w.PAAPairs(settings.SvdDimensions, settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	r := len(buffer)
	totalPairs := r * r / 2

	fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(paaPairs), totalPairs,
		100*len(paaPairs)/totalPairs)
	return nil
}

func (w *TimeseriesWindow) ProcessBuffer(settings CorrjoinSettings,
	buffer [][]float64) error {

	// Shift _buffer_ into _window_. The stride length is equal to the
	// width of _buffer_.
	err := w.ShiftBuffer(buffer)
	if err != nil {
		return err
	}

	w.normalizeWindow()
	w.PAA(settings.SvdDimensions)

	// Apply SVD
	_, err = w.SVD(settings.SvdOutputDimensions)
	if err != nil {
		return err
	}
	// CorrelationBuckets outputs a set of row index pairs.
	pairs, err := w.CorrelationPairs(settings.SvdDimensions, settings.EuclidDimensions,
		settings.CorrelationThreshold)
	if err != nil {
		return err
	}

	r := len(buffer)
	totalPairs := r * r / 2

	fmt.Printf("found %d correlated pairs out of %d possible (%d)\n", len(pairs), totalPairs,
		100*len(pairs)/totalPairs)

	return nil
}
