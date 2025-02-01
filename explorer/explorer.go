package explorer

import (
	"encoding/json"
	"fmt"
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
	"github.com/prometheus/common/model"
	"log"
	"net/http"
	"os"
	"sort"
	"time"
)

const (
	STRIDE_CACHE_SIZE = 10
	MAX_AGE           = 86400 // 1 day
)

type CorrelationExplorer struct {
	filenameBase        string
	strideCache         []*Stride
	metricsCache        map[uint64](*explorerlib.Metric)
	metricsCacheByRowId map[int](*explorerlib.Metric)
	prometheusBaseURL   string

	// This should be the length of a stride expressed as a duration that
	// Prometheus can understand. e.g. "10m" or "1h".
	TimeRange string // e.g. "10m"

	ticker               *time.Ticker
	nextStrideCacheEntry int
}

func (c *CorrelationExplorer) Initialize() error {
	c.prometheusBaseURL = "http://localhost:9090"
	c.strideCache = make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE)
	c.metricsCache = make(map[uint64](*explorerlib.Metric))
	c.metricsCacheByRowId = make(map[int](*explorerlib.Metric))
	c.ticker = time.NewTicker(60 * time.Second)
	c.nextStrideCacheEntry = 0

	go func() {
		for {
			select {
			case _ = <-c.ticker.C:
				err := c.scanResultFiles()
				if err != nil {
					log.Printf("Error scanning result files: %e\n", err)
				}
			}
		}
	}()
	return nil
}

func (c *CorrelationExplorer) scanResultFiles() error {
	entries, err := os.ReadDir(c.filenameBase)
	if err != nil {
		log.Printf("Failed to get list of stride results from %s: %v\n", c.filenameBase, err)
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		t1, _ := entries[i].Info()
		t2, _ := entries[j].Info()
		return t1.ModTime().Unix() > t2.ModTime().Unix()
	})
	for _, e := range entries {
		// See if this stride is already in the cache
		foundInCache := false
		for _, s := range c.strideCache {
			if s == nil {
				break
			}
			if e.Name() == s.Filename {
				foundInCache = true
				break
			}
		}
		if foundInCache {
			continue
		}
		t, err := e.Info()
		if err != nil {
			continue
		}
		age := time.Now().UTC().Unix() - t.ModTime().UTC().Unix()
		if age > MAX_AGE {
			log.Printf("file %s should be deleted\n", e.Name())
		}
		if t.Size() == 0 {
			continue
		}
		log.Printf("new file found: %s of size %d for time %v\n", e.Name(), t.Size(), t.ModTime())

		newStride, err := parseStrideFromFilename(e.Name())
		if err != nil {
			log.Printf("failed to parse stride info from filename: %e\n", err)
			continue
		}

		err = c.readResultFile(e.Name())
		if err != nil {
			log.Printf("failed to parse result file %s, will try again\n", e.Name())
			continue
		}
		c.convertMetricsCache()
		log.Printf("added stride %+v", *newStride)
		c.addStrideCacheEntry(newStride)

		// Only parse and add one file at a time.
		break
	}
	return nil
}

func (c *CorrelationExplorer) convertMetricsCache() {
	for rowid, m := range c.metricsCacheByRowId {
		if m.Fingerprint == 0 {
			log.Printf("missing metrics fingerprint for row id %d, %v\n", rowid, *m)
			continue
		}
		other, exists := c.metricsCache[m.Fingerprint]
		if exists {
			log.Printf("duplicate entry for fingerprint %d: already have %v, trying to insert %v\n",
				m.Fingerprint, other, *m)
			continue
		}
		c.metricsCache[m.Fingerprint] = m
	}
}

func parseStrideFromFilename(filename string) (*Stride, error) {
	var strideCounter int
	var startTime int
	var endTime int
	n, err := fmt.Sscanf(filename, "correlations_%d_%d-%d.pq", &strideCounter, &startTime, &endTime)
	if n != 3 || err != nil {
		return nil, fmt.Errorf("failed to parse stride information out of filename %s", filename)
	}
	startT, err := time.Parse("2006102150405", fmt.Sprintf("%d", startTime))
	if err != nil {
		return nil, err
	}
	endT, err := time.Parse("2006102150405", fmt.Sprintf("%d", endTime))
	if err != nil {
		return nil, err
	}
	return &Stride{
		ID:              strideCounter,
		StartTime:       startT.UTC().Unix(),
		StartTimeString: startT.UTC().Format("2006-01-02 15:04:05"),
		EndTime:         endT.UTC().Unix(),
		EndTimeString:   endT.UTC().Format("2006-01-02 15:04:05"),
		Status:          "Read",
		Filename:        filename,
	}, nil
}

func (c *CorrelationExplorer) addStrideCacheEntry(stride *Stride) {
	c.strideCache[c.nextStrideCacheEntry] = stride
	c.nextStrideCacheEntry = (c.nextStrideCacheEntry + 1) % STRIDE_CACHE_SIZE
}

func (c *CorrelationExplorer) readResultFile(filename string) error {
	parquetExplorer := explorerlib.NewParquetExplorer(c.filenameBase)
	err := parquetExplorer.Initialize(filename)
	if err != nil {
		return err
	}
	return parquetExplorer.GetMetrics(&c.metricsCacheByRowId)
}

// TODO: this also needs the timeframe
// TODO: this should redirect to a dashboard url and pass information as
// parameters (probably just url params?):
// send: stride id, row id, whether this metric was constant during that stride
// if possible: list of correlated ts row ids and their pearson values
// TODO:  implement the nodes/edges calls as separate endpoints
// maybe get the list of correlated ts as another json via another endpoint
// TODO: another endpoint for nodes/edges data for the development over time
func (c *CorrelationExplorer) ExploreByName(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	fmt.Printf("got params: %+v\n", params)
	// example: got params: map[name:[{__name__="process_cpu_seconds_total", container="receiver", endpoint="metrics", instance="10.100.1.8:9203", job="correlation-service", namespace="default", pod="correlation-processor-75b6d895df-xps42", service="correlation-service"}]]
	var metric model.Metric
	err := json.Unmarshal(([]byte)(params["ts"][0]), &metric)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse timeseries out of %s\n", params["ts"]), http.StatusBadRequest)
		return
	}
	fp := uint64(metric.Fingerprint())
	cachedMetric, found := c.metricsCache[fp]
	if !found {
		http.Error(w, fmt.Sprintf("metric %s not found\n", params["ts"]), http.StatusNotFound)
		return
	}
	log.Printf("retrieved %v for metric query %s\n", *cachedMetric, params["ts"])
}

/*
func (c *CorrelationExplorer) ExploreTimeseries(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	var rowID int64
	n, err := fmt.Sscanf(params["id"][0], "%d", &rowID)
	if n != 1 || err != nil {
		http.Error(w, fmt.Sprintf("failed to parse row id out of %s\n", params["id"][0]), http.StatusBadRequest)
		return
	}
	strideKey := params["stride"][0]
	stride, have := c.cache.strides[strideKey]
	if !have {
		http.Error(w, fmt.Sprintf("Stride %s not known\n", strideKey), http.StatusBadRequest)
		return
	}
	file, err := os.Open(filepath.Join(c.filenameBase, strideKey))
	if err != nil {
		http.Error(w, fmt.Sprintf("Missing data file for cluster %s\n", strideKey), http.StatusBadRequest)
		return
	}
	defer file.Close()
	corr, err := c.retrieveCorrelations(csv.NewReader(file), rowID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get correlation information for row %d\n", rowID), http.StatusBadRequest)
		return
	}

	file, err = os.Open(filepath.Join(c.filenameBase, stride.idsFile))
	if err != nil {
		http.Error(w, fmt.Sprintf("Missing ids file for cluster %s\n", strideKey), http.StatusBadRequest)
		return
	}
	defer file.Close()
	rowIds := make([]int64, 0, len(corr.Correlated)+1)
	rowIds = append(rowIds, rowID)
	for row, _ := range corr.Correlated {
		{
			rowIds = append(rowIds, row)
		}
	}
	corr.Timeseries, _ = c.readTsNames(csv.NewReader(file), rowIds, stride)
	log.Printf("correlations for row %d: %+v\n", rowID, corr)
	// w.WriteHeader(http.StatusOK)
	if err := c.correlationTemplate.ExecuteTemplate(w, "correlations", corr); err != nil {
		http.Error(w, fmt.Sprintf("Failed to apply template: %v\n", err), http.StatusBadRequest)
		return
	}
}
*/
