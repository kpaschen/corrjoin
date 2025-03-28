package explorer

import (
	"fmt"
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
	"testing"
	"time"
)

func TestScanResultFiles(t *testing.T) {
	explorer := CorrelationExplorer{
		FilenameBase: "./testdata",
		strideCache:  make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
	}

	err := explorer.scanResultFiles()
	if err != nil {
		t.Errorf("unexpected: %e", err)
	}

	strideCount := len(explorer.strideCache)
	if strideCount == 0 {
		t.Errorf("no strides")
	}

	allStridesAreNil := true
	for strideId, stride := range explorer.strideCache {
		if stride == nil {
			continue
		}
		allStridesAreNil = false
		entries := len(stride.metricsCache)
		if entries == 0 {
			t.Errorf("no metrics for stride %d", strideId)
		}
		fpEntries := len(stride.metricsCache)
		if fpEntries == 0 {
			t.Errorf("no entries in cache by fp")
		}
		if fpEntries != entries {
			t.Errorf("cache count mismatch: %d in cache by rowid vs %d in cache by fp", entries, fpEntries)
		}
	}

	if allStridesAreNil {
		t.Errorf("all strides are nil")
	}

}

func TestScanStrideAgain(t *testing.T) {
	explorer := CorrelationExplorer{
		FilenameBase: "./testdata",
		strideCache:  make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
	}

	err := explorer.scanResultFiles()
	if err != nil {
		t.Errorf("unexpected: %e", err)
	}

	// Scan again so we do the exists -> read transition now
	err = explorer.scanResultFiles()
	if err != nil {
		t.Errorf("unexpected: %e", err)
	}
}

func TestGetTimeseriesById(t *testing.T) {
	explorer := CorrelationExplorer{
		FilenameBase: "./testdata",
		strideCache:  make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
	}

	err := explorer.scanResultFiles()
	if err != nil {
		t.Errorf("unexpected: %e", err)
	}

	stride := explorer.strideCache[0]
	if stride == nil {
		t.Fatalf("no stride")
	}

	params := make(map[string]([]string))

	params["timeTo"] = []string{"1738598499834"}

	rowids, err := explorer.getMetrics(params, stride)
	if err == nil {
		t.Errorf("expected error but got rowids %v", rowids)
	}

	sampleEntry, ok := stride.metricsCache[18395833292221308750]
	if !ok {
		t.Errorf("entry rowid 18395833292221308750 is missing")
	}
	fmt.Printf("sample entry: %+v\n", sampleEntry)
}

func TestGetTimeOverride(t *testing.T) {
	explorer := CorrelationExplorer{
		FilenameBase: "./tmp",
		strideCache:  make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
	}
	params := make(map[string]([]string))
	params["timeTo"] = []string{"now"}
	from, to, err := explorer.getTimeOverride(params)
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
	fmt.Printf("from based on now: %s, to: %s, err: %v\n", from, to, err)

	now := time.Now().UTC()
	toParsed, err := time.Parse(explorerlib.FORMAT, to)
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
	difference := now.Sub(toParsed)
	if difference > time.Duration(1*time.Minute) {
		t.Errorf("timeTo should be close to now but there is a %+v difference", difference)
	}

	fromParsed, err := time.Parse(explorerlib.FORMAT, from)
	if err != nil {
		t.Errorf("unexpected: %v", err)
	}
	difference = toParsed.Sub(fromParsed)
	if difference != time.Duration(10*time.Minute) {
		t.Errorf("difference between 'from' and 'to' should be 10m but is %+v", difference)
	}
}

func TestExtendTimeline(t *testing.T) {
	explorer := CorrelationExplorer{
		FilenameBase: "./tmp",
		strideCache:  make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
	}
	resp := make([]TimelineResponse, 0, 0)
	explorer.strideCache[0] = &Stride{
		ID:              2,
		StartTimeString: time.Now().UTC().Format(explorerlib.FORMAT),
		Status:          StrideProcessed,
	}
	explorer.strideCache[1] = &Stride{
		ID:              3,
		StartTimeString: time.Now().UTC().Format(explorerlib.FORMAT),
		Status:          StrideProcessed,
	}
	explorer.strideCache[2] = &Stride{
		ID:              4,
		StartTimeString: time.Now().UTC().Format(explorerlib.FORMAT),
		Status:          StrideProcessed,
	}

	knownTimeseries := make(map[uint64]bool)

	// Base case: appending data for the first stride.
	resp = explorer.extendTimeline(resp, map[uint64]float32{1: 0.9, 2: 0.8}, knownTimeseries, explorer.strideCache[0])

	if len(resp) != 2 {
		t.Errorf("both data points should have been added to result but got %v", resp)
	}
	if !knownTimeseries[1] || !knownTimeseries[2] {
		t.Errorf("1 and 2 should have been added to knownTimeseries")
	}
	if len(knownTimeseries) != 2 {
		t.Errorf("only 1 and 2 should have been added to knownTimeseries but i have %v", knownTimeseries)
	}

	// Append data for another stride, adding one time series that we didn't have data for before.
	// Expect data for the added timeseries to be backfilled for the earlier stride.
	resp = explorer.extendTimeline(resp, map[uint64]float32{1: 0.95, 2: 0.88, 3: 0.9}, knownTimeseries, explorer.strideCache[1])
	if len(resp) != 6 {
		t.Errorf("expected 6 entries in response but got %d", len(resp))
	}
	if !knownTimeseries[1] || !knownTimeseries[2] || !knownTimeseries[3] {
		t.Errorf("3 should have been added to knownTimeseries")
	}
	if len(knownTimeseries) != 3 {
		t.Errorf("only 1, 2 and 3 should have been added to knownTimeseries but i have %v", knownTimeseries)
	}

	// Append data for another stride, but drop one of the time series. Its value must be filled in as 0.
	resp = explorer.extendTimeline(resp, map[uint64]float32{2: 0.89, 3: 0.91}, knownTimeseries, explorer.strideCache[2])
	if len(resp) != 9 {
		t.Errorf("expected 9 entries in response but got %d", len(resp))
	}
	if !knownTimeseries[1] || !knownTimeseries[2] || !knownTimeseries[3] {
		t.Errorf("3 should have been added to knownTimeseries")
	}
	if len(knownTimeseries) != 3 {
		t.Errorf("only 1, 2 and 3 should have been added to knownTimeseries but i have %v", knownTimeseries)
	}
}
