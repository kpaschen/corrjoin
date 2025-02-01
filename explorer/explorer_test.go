package explorer

import (
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
	"testing"
)

func TestScanResultFiles(t *testing.T) {
	explorer := CorrelationExplorer{
		FilenameBase:         ".",
		strideCache:          make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE),
		metricsCache:         make(map[uint64](*explorerlib.Metric)),
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
