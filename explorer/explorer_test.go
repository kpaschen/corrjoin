package explorer

import (
	//  "encoding/json"
	"fmt"
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
	"testing"
)

func TestScanResultFiles(t *testing.T) {
	explorer := CorrelationExplorer{
		FilenameBase:         ".",
		strideCache:          make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
		metricsCache:         make(map[uint64]int),
		metricsCacheByRowId:  make(map[int](*explorerlib.Metric)),
		nextStrideCacheEntry: 0,
	}

	err := explorer.scanResultFiles()
	if err != nil {
		t.Errorf("unexpected: %e", err)
	}

	entries := len(explorer.metricsCacheByRowId)
	if entries == 0 {
		t.Errorf("no entries in cache")
	}
	fpEntries := len(explorer.metricsCache)
	if fpEntries == 0 {
		t.Errorf("no entries in cache by fp")
	}
	if fpEntries != entries {
		t.Errorf("cache count mismatch: %d in cache by rowid vs %d in cache by fp", entries, fpEntries)
	}
}

func TestGetTimeseriesById(t *testing.T) {
	explorer := CorrelationExplorer{
		// FilenameBase:         "/tmp/corrjoinResults",
		FilenameBase:         ".",
		strideCache:          make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
		metricsCache:         make(map[uint64]int),
		metricsCacheByRowId:  make(map[int](*explorerlib.Metric)),
		nextStrideCacheEntry: 0,
	}

	err := explorer.scanResultFiles()
	if err != nil {
		t.Errorf("unexpected: %e", err)
	}

	params := make(map[string]([]string))

	params["timeTo"] = []string{"1738598499834"}

	rowid, err := explorer.getMetric(params, 1)
	if err == nil {
		t.Errorf("expected error but got rowid %d", rowid)
	}

	sampleEntry, ok := explorer.metricsCacheByRowId[10]
	if !ok {
		t.Errorf("entry rowid 10 is missing")
	}

	fmt.Printf("sample entry: %+v\n", sampleEntry)
	/*
	   entryString, err := json.Marshal(sampleEntry.LabelSet)
	   if err != nil { t.Errorf("unexpected %v", err) }
	   params["ts"] = []string{string(entryString)}

	   foundRowId, err := explorer.getMetric(params, 1)
	   if err != nil {
	      t.Errorf("unexpected error %v", err)
	   }
	   if foundRowId != 10 {
	      t.Errorf("expected to get row id 10 but got %d", foundRowId)
	   }
	*/

}
