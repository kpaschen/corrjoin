package lib

import (
	"fmt"
	"log"
	"math"
	"time"
)

const (
	// Default is one sample every 5s
	SAMPLE_TIME = 5.0
)

// A TimeseriesAccumulator keeps track of timeseries data as it arrives.
// It maps the timeseries id (serialised name + labelset) to the tsmatrix
// rowid and it accumulates data until it has reached the stride length.
// When it reaches the stride length, it sends the collected buffers to
// a channel.
// TODO: decide what to do if there is a computation ongoing at the time the
// stride is reached.

type TimeseriesAccumulator struct {
	stride int

	// rowmap maps the timeseries names to row ids
	rowmap map[string]int

	// The ids of the timeseries, in order.
	// A tsid is a json serialization of the metric name and the labels.
	// invariant: rowmap[tsids[i]] == i for 0 <= i <= maxRow
	tsids []string

	// buffers maps rowids to observations
	// This is a map because it is possible for a timeseries that we have
	// a rowid for to disappear, but maybe it would be easier to just make
	// this an array of arrays.
	buffers              map[int]([]float64)
	maxRow               int
	currentStrideStartTs time.Time
	currentStrideMaxTs   time.Time
	sampleTime           int
	strideDuration       time.Duration

	bufferChannel chan<- [][]float64
}

func maxTime(startTime time.Time, strideDuration time.Duration) time.Time {
	t1 := startTime.Add(strideDuration)

	// This uses Add not Sub because Sub returns a Duration.
	return t1.Add(-1 * time.Second)
}

func NewTimeseriesAccumulator(stride int, startTime time.Time, bc chan<- [][]float64) *TimeseriesAccumulator {
	strideDuration, _ := time.ParseDuration(fmt.Sprintf("%ds", stride*SAMPLE_TIME))
	return &TimeseriesAccumulator{
		stride:               stride,
		rowmap:               make(map[string]int),
		tsids:                make([]string, 0, 5000), // TODO: initialize capacity based on a config value.
		buffers:              make(map[int][]float64),
		maxRow:               0,
		sampleTime:           int(SAMPLE_TIME),
		currentStrideStartTs: startTime,
		currentStrideMaxTs:   maxTime(startTime, strideDuration),
		strideDuration:       strideDuration,
		bufferChannel:        bc,
	}
}

func (a *TimeseriesAccumulator) computeSlotIndex(timestamp time.Time) (int32, error) {
	if timestamp.After(a.currentStrideMaxTs) {
		return int32(-1), nil
	}
	if timestamp.Before(a.currentStrideStartTs) {
		return int32(-2), fmt.Errorf("backfill timestamp, ignore")
	}
	diff := timestamp.Sub(a.currentStrideStartTs).Seconds()
	return int32(diff / float64(a.sampleTime)), nil
}

func (a *TimeseriesAccumulator) extractMatrixData() [][]float64 {
	ret := make([][]float64, a.maxRow)
	for i, b := range a.buffers {
		ret[i] = b // This is a move
		a.buffers[i] = make([]float64, a.stride, a.stride)
	}
	return ret
}

func (a *TimeseriesAccumulator) AddObservation(tsName string, value float64, timestamp time.Time) error {
	colcount := a.stride
	slot, err := a.computeSlotIndex(timestamp)
	if err != nil {
		return err
	}
	if slot < 0 {
		log.Printf("publish %d rows to channel\n", len(a.buffers))
		// publish buffer data to channel

		// TODO: since this can block, have to guard against concurrent calls.
		a.bufferChannel <- a.extractMatrixData()
		a.currentStrideStartTs = timestamp
		a.currentStrideMaxTs = maxTime(timestamp, a.strideDuration)
		slot, err = a.computeSlotIndex(timestamp)
		if slot < 0 {
			panic("got negative timestamp after resetting buffers")
		}
	}

	rowid, ok := a.rowmap[tsName]
	if !ok {
		rowid = a.maxRow
		if rowid >= 10 {
			return nil
		}
		a.rowmap[tsName] = rowid
		a.buffers[rowid] = make([]float64, colcount, colcount)
		a.tsids = append(a.tsids, tsName)
		a.maxRow += 1
	}

	if math.IsNaN(value) {
		value = float64(0)
	}
	a.buffers[rowid][slot] = value

	return nil
}
