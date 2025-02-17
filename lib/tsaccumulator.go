package lib

import (
	"fmt"
	"log"
	"math"
	"time"
)

type Observation struct {
	MetricName string
	Value      float64
	Timestamp  time.Time
}

type ObservationResult struct {
	Buffers [][]float64
	Err     error
}

// A TimeseriesAccumulator keeps track of timeseries data as it arrives.
// It maps the timeseries id (serialised name + labelset) to the tsmatrix
// rowid and it accumulates data until it has reached the stride length.
// When it reaches the stride length, it sends the collected buffers to
// a channel.
// TODO: return quickly if there is a computation ongoing at the time the
// stride is reached.

type TimeseriesAccumulator struct {
	stride int

	// rowmap maps the timeseries names to row ids
	rowmap map[string]int

	// The ids of the timeseries, in order.
	// A tsid is a json serialization of the metric name and the labels.
	// invariant: rowmap[Tsids[i]] == i for 0 <= i <= maxRow
	Tsids []string

	// buffers maps rowids to observations
	// This is a map because it is possible for a timeseries that we have
	// a rowid for to disappear, but maybe it would be easier to just make
	// this an array of arrays.
	buffers              map[int]([]float64)
	maxRow               int
	CurrentStrideStartTs time.Time
	CurrentStrideMaxTs   time.Time
	sampleTime           int
	strideDuration       time.Duration

	bufferChannel chan<- *ObservationResult
}

func maxTime(startTime time.Time, strideDuration time.Duration) time.Time {
	t1 := startTime.Add(strideDuration)

	// This uses Add not Sub because Sub returns a Duration.
	return t1.Add(-1 * time.Second)
}

func NewTimeseriesAccumulator(stride int, startTime time.Time, sampleInterval int,
	bc chan<- *ObservationResult) *TimeseriesAccumulator {
	strideDuration, _ := time.ParseDuration(fmt.Sprintf("%ds", stride*sampleInterval))
	return &TimeseriesAccumulator{
		stride:               stride,
		rowmap:               make(map[string]int),
		Tsids:                make([]string, 0, 5000), // TODO: initialize capacity based on a config value.
		buffers:              make(map[int][]float64),
		maxRow:               0,
		sampleTime:           sampleInterval,
		CurrentStrideStartTs: startTime,
		CurrentStrideMaxTs:   maxTime(startTime, strideDuration),
		strideDuration:       strideDuration,
		bufferChannel:        bc,
	}
}

func (a *TimeseriesAccumulator) computeSlotIndex(timestamp time.Time) (int, error) {
	if timestamp.After(a.CurrentStrideMaxTs) {
		return -1, nil
	}
	if timestamp.Before(a.CurrentStrideStartTs) {
		return -2, fmt.Errorf("backfill timestamp, ignore")
	}
	diff := timestamp.Sub(a.CurrentStrideStartTs).Seconds()
	return int(diff / float64(a.sampleTime)), nil
}

func (a *TimeseriesAccumulator) extractMatrixData() *ObservationResult {
	ret := make([][]float64, a.maxRow)
	for i, b := range a.buffers {
		ret[i] = b // This is a move
		a.buffers[i] = make([]float64, 0, a.stride)
	}
	return &ObservationResult{
		Buffers: ret,
		Err:     nil,
	}
}

func (a *TimeseriesAccumulator) completeRows() {
	for j, b := range a.buffers {
		if len(b) > cap(b) {
			log.Printf("row %d has length %d greater than capacity %d\n", j, len(b), cap(b))
			panic("bug")
		}
		if len(b) < cap(b) {
			interpolatedValue := float64(0)
			if len(b) > 0 {
				interpolatedValue = b[len(b)-1]
			}
			for i := len(b); i < cap(b); i++ {
				a.buffers[j] = append(a.buffers[j], interpolatedValue)
			}
		}
	}
}

func (a *TimeseriesAccumulator) AddObservation(observation *Observation) {
	colcount := a.stride
	slot, err := a.computeSlotIndex(observation.Timestamp)
	if err != nil {
		// This is a backfill, it is safe to ignore.
		return
	}
	if slot < 0 {
		a.CurrentStrideStartTs = observation.Timestamp
		a.CurrentStrideMaxTs = maxTime(observation.Timestamp, a.strideDuration)
		slot, err = a.computeSlotIndex(observation.Timestamp)
		if err != nil {
			a.bufferChannel <- &ObservationResult{Buffers: nil, Err: err}
			return
		}
		if slot < 0 {
			panic("got negative timestamp after resetting buffers")
		}

		a.completeRows()

		log.Printf("publish %d rows to channel\n", len(a.buffers))
		a.bufferChannel <- a.extractMatrixData()
	}

	rowid, ok := a.rowmap[observation.MetricName]
	if !ok {
		rowid = a.maxRow
		a.rowmap[observation.MetricName] = rowid
		a.buffers[rowid] = make([]float64, 0, colcount)
		a.Tsids = append(a.Tsids, observation.MetricName)
		if a.Tsids[rowid] != observation.MetricName {
			log.Printf("tsid for %d is %s but should be %s\n", rowid, a.Tsids[rowid], observation.MetricName)
			panic("code bug")
		}
		a.maxRow += 1
	}

	if math.IsNaN(observation.Value) {
		observation.Value = float64(0)
	}

	if slot < len(a.buffers[rowid]) {
		// Sometimes there is a double message for the same slot, just ignore it.
		// Could verify that the values are the same here.
		return
	}

	lastSlot := len(a.buffers[rowid]) - 1
	// If we have skipped a timeslot, fill the gap with an interpolated value.
	if lastSlot < slot-1 {
		interpolatedValue := float64(0)
		if len(a.buffers[rowid]) > 0 {
			interpolatedValue = (a.buffers[rowid][lastSlot] + observation.Value) / float64(2)
		}
		for i := lastSlot + 1; i < slot; i++ {
			a.buffers[rowid] = append(a.buffers[rowid], interpolatedValue)
		}
		if len(a.buffers[rowid]) != slot {
			log.Printf("wrong length for buffer %d: %d when it should be %d", rowid, len(a.buffers[rowid]), slot)
			panic("bug!")
		}
	}
	a.buffers[rowid] = append(a.buffers[rowid], observation.Value)
	if len(a.buffers[rowid]) != slot+1 {
		log.Printf("wrong length for buffer %d: %d when it should be %d", rowid, len(a.buffers[rowid]), slot+1)
		panic("bug")
	}
}
