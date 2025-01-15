package lib

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestComputeSlotIndex(t *testing.T) {
	now := time.Now()
	replies := make(chan *ObservationResult, 1)
	defer close(replies)
	acc := NewTimeseriesAccumulator(6, now, 5, replies)

	s0, err := acc.computeSlotIndex(now)
	if err != nil {
		t.Errorf("unexpected error %v for computeSlotIndex", err)
	}
	if s0 != 0 {
		t.Errorf("expected slot 0 for initial time but got %d", s0)
	}

	// Default sample time is 5, so 6 seconds ~ slot 1
	s1, _ := acc.computeSlotIndex(now.Add(time.Second * 6))
	if s1 != 1 {
		t.Errorf("expected slot 1 but got %d", s1)
	}

	s2, _ := acc.computeSlotIndex(now.Add(time.Second * 20))
	if s2 != 4 {
		t.Errorf("expected slot 4 for second 20 but got %d", s2)
	}
}

func TestAddObservation(t *testing.T) {
	now := time.Now()
	replies := make(chan *ObservationResult, 1)
	defer close(replies)
	acc := NewTimeseriesAccumulator(6, now, 5, replies)
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.1,
		Timestamp:  now.Add(time.Second * 2),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.2,
		Timestamp:  now.Add(time.Second * 6),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts2",
		Value:      0.3,
		Timestamp:  now.Add(time.Second * 7),
	})

	// Expect two rows in the buffers, one with two entries, the other with
	// one entry in position 2
	if len(acc.buffers) != 2 {
		t.Errorf("expected two buffer rows but got %+v\n", acc.buffers)
	}
	b1 := acc.rowmap["ts1"]

	if acc.buffers[b1][0] != 0.1 || acc.buffers[b1][1] != 0.2 {
		t.Errorf("buffer for ts1 should be 0.1, 0.2, ... but got %+v\n", acc.buffers[b1])
	}

	b2 := acc.rowmap["ts2"]

	if acc.buffers[b2][0] != 0.0 || acc.buffers[b2][1] != 0.3 {
		t.Errorf("buffer for ts2 should be 0.0, 0.3, ... but got %+v\n", acc.buffers[b2])
	}
}

func TestAddObservation_newStride(t *testing.T) {
	now := time.Now()
	replies := make(chan *ObservationResult, 1)
	defer close(replies)
	acc := NewTimeseriesAccumulator(2, now, 5, replies)
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.1,
		Timestamp:  now.Add(time.Second * 2),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.2,
		Timestamp:  now.Add(time.Second * 5),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts2",
		Value:      0.4,
		Timestamp:  now.Add(time.Second * 5),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.3,
		Timestamp:  now.Add(time.Second * 10),
	})

	select {
	case buffers := <-replies:
		if len(buffers.Buffers) != 2 {
			t.Errorf("expected two items in reply but got %d", len(buffers.Buffers))
		}
		if len(buffers.Buffers[0]) != 2 || len(buffers.Buffers[1]) != 2 {
			t.Errorf("expected both buffers to have length 2 but got %d and %d", len(buffers.Buffers[0]), len(buffers.Buffers[1]))
		}
		fmt.Printf("got buffer reply: %v\n", buffers)
		fmt.Printf("acc buffers are now %v\n", acc.buffers)
	default:
		t.Errorf("failed to get new stride channel message")
	}
}

func TestAddObservation_interpolate(t *testing.T) {
	now := time.Now()
	replies := make(chan *ObservationResult, 1)
	defer close(replies)
	acc := NewTimeseriesAccumulator(6, now, 2, replies)
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.1,
		Timestamp:  now.Add(time.Second * 2),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts2",
		Value:      0.4,
		Timestamp:  now.Add(time.Second * 2),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.2,
		Timestamp:  now.Add(time.Second * 7),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value:      0.3,
		Timestamp:  now.Add(time.Second * 20),
	})
	select {
	case buffers := <-replies:
		fmt.Printf("got buffer reply: %v\n", buffers)
		if buffers.Buffers[0][1] != 0.1 {
			t.Errorf("expected first non-zero value to be 0.1 but got %f", buffers.Buffers[0][1])
		}
		if buffers.Buffers[0][3] != 0.2 {
			t.Errorf("expected last non-zero value to be 0.2 but got %f", buffers.Buffers[0][3])
		}
		if math.Abs(buffers.Buffers[0][2]-0.15) > 0.0001 {
			t.Errorf("expected middle value to be 0.15 but got %f", buffers.Buffers[0][2])
		}
		if len(buffers.Buffers[0]) != len(buffers.Buffers[1]) {
			t.Errorf("expected both rows to be the same length")
		}
	default:
		t.Errorf("failed to get new stride channel message")
	}
}
