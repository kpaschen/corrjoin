package lib

import (
	"fmt"
	"testing"
	"time"
)

func TestComputeSlotIndex(t *testing.T) {
	now := time.Now()
	replies := make(chan *ObservationResult, 1)
	defer close(replies)
	acc := NewTimeseriesAccumulator(6, now, replies)

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
	acc := NewTimeseriesAccumulator(6, now, replies)
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value: 0.1,
		Timestamp: now.Add(time.Second * 2),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value: 0.2,
		Timestamp: now.Add(time.Second * 6),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts2",
		Value: 0.3,
		Timestamp: now.Add(time.Second * 7),
	})

	// Expect two rows in the buffers, one with two entries, the other with
	// one entry in position 2
	if len(acc.buffers) != 2 {
		t.Errorf("expected two buffer rows but got %+v\n", acc.buffers)
	}
	b1 := acc.rowmap["ts1"]
	if len(acc.buffers[b1]) != 6 {
		t.Errorf("buffer rows should have window size 6 but got %d\n", len(acc.buffers[b1]))
	}

	if acc.buffers[b1][0] != 0.1 || acc.buffers[b1][1] != 0.2 || acc.buffers[b1][2] != 0.0 {
		t.Errorf("buffer for ts1 should be 0.1, 0.2, 0.0, ... but got %+v\n", acc.buffers[b1])
	}

	b2 := acc.rowmap["ts2"]

	if acc.buffers[b2][0] != 0.0 || acc.buffers[b2][1] != 0.3 || acc.buffers[b2][2] != 0.0 {
		t.Errorf("buffer for ts2 should be 0.0, 0.3, 0.0, ... but got %+v\n", acc.buffers[b2])
	}
}

func TestAddObservation_newStride(t *testing.T) {
	now := time.Now()
	replies := make(chan *ObservationResult, 1)
	defer close(replies)
	acc := NewTimeseriesAccumulator(2, now, replies)
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value: 0.1,
		Timestamp: now.Add(time.Second * 2),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value: 0.2,
		Timestamp: now.Add(time.Second * 5),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts2",
		Value: 0.4,
		Timestamp: now.Add(time.Second * 5),
	})
	acc.AddObservation(&Observation{
		MetricName: "ts1",
		Value: 0.3,
		Timestamp: now.Add(time.Second * 10),
	})

	select {
	case buffers := <-replies:
		fmt.Printf("got buffer reply: %v\n", buffers)
		fmt.Printf("acc buffers are now %v\n", acc.buffers)
	default:
		t.Errorf("failed to get new stride channel message")
	}
}
