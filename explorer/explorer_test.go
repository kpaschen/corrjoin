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
		entries := len(stride.metricsCacheByRowId)
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
		// FilenameBase:         "/tmp/corrjoinResults",
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

	rowid, err := explorer.getMetric(params, stride)
	if err == nil {
		t.Errorf("expected error but got rowid %d", rowid)
	}

	sampleEntry, ok := stride.metricsCacheByRowId[10]
	if !ok {
		t.Errorf("entry rowid 10 is missing")
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
