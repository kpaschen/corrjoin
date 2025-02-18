package explorer

import (
	"encoding/json"
	"fmt"
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"time"
)

const (
	STRIDE_CACHE_SIZE = 10
	MAX_AGE           = 86400 // 1 day
)

type CorrelationExplorer struct {
	FilenameBase string
	strideCache  []*Stride
	// We assume that the rowids are stable, which is not the case after a restart.
	// TODO: move the metrics cache to be a field on Stride
	metricsCache        map[uint64](int)
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
	c.metricsCache = make(map[uint64]int)
	c.metricsCacheByRowId = make(map[int](*explorerlib.Metric))
	c.ticker = time.NewTicker(60 * time.Second)
	c.nextStrideCacheEntry = 0

	go func() {
		for {
			select {
			case _ = <-c.ticker.C:
				c.scanResultFiles()
			}
		}
	}()
	return nil
}

func (c *CorrelationExplorer) scanResultFiles() error {
	entries, err := os.ReadDir(c.FilenameBase)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		t1, _ := entries[i].Info()
		t2, _ := entries[j].Info()
		return t1.ModTime().Unix() > t2.ModTime().Unix()
	})
	for _, e := range entries {
		if !e.IsDir() {
			_, err = parseStrideFromFilename(e.Name())
			if err != nil {
				// This is not a stride file.
				continue
			}
		} else {
			_, err = parseStrideFromDirname(e.Name())
			if err != nil {
				// This is not a stride directory.
				continue
			}
		}
		// See if this stride is already in the cache
		var stride *Stride
		for _, s := range c.strideCache {
			if s == nil {
				break
			}
			if e.Name() == s.Filename || e.Name() == directoryNameForStride(*s) {
				stride = s
				break
			}
		}

		t, err := e.Info()
		if err != nil {
			continue
		}
		age := time.Now().UTC().Unix() - t.ModTime().UTC().Unix()
		if age > MAX_AGE {
			log.Printf("file %s should be deleted\n", e.Name())
		}

		// Potential situations:
		/*
		   1. e is a file and we do not have a stride yet -> try to parse e into a stride
		   2. e is a file and we have a stride:
		      if stride is in state Error or Deleted, skip e
		      if stride is in state retrying, try to read e
		      if stride is in state read, skip e (directory should exist)
		   3. e is a directory and we do not have a stride:
		       (this must be a recovery situation)
		       skip e (we're waiting for the file)
		      e is a directory and we have a stride:
		        if stride is in state Error or Deleted, skip e
		        if stride is in state retrying, skip e (handled when we see the file)
		        if stride is in state read, try to process it
		*/

		// Is e a file?
		if !e.IsDir() {
			if t.Size() == 0 {
				continue
			}
			if stride == nil {
				log.Printf("found a file and i do not have a stride for it yet\n")
				stride, err = parseStrideFromFilename(e.Name())
				if err != nil {
					log.Printf("failed to parse stride info from filename: %e\n", err)
					continue
				}
				log.Printf("created stride\n")
				c.addStrideCacheEntry(stride)
			}
			log.Printf("stride %d in status %v\n", stride.ID, stride.Status)
			switch stride.Status {
			case StrideError:
				continue
			case StrideDeleted:
				continue
			case StrideRead:
				continue
			default: // Retrying or Exists
				dirname := directoryNameForStride(*stride)
				err = os.Mkdir(fmt.Sprintf("%s/%s", c.FilenameBase, dirname), 0750)
				if err != nil && !os.IsExist(err) {
					log.Printf("failed to create stride directory: %v\n", err)
					stride.Status = StrideError
					continue
				}
				err = c.readResultFile(e.Name(), stride)
				if err != nil {
					log.Printf("failed to parse result file %s because %v\n", e.Name(), err)
					if age > 3600 {
						log.Println("giving up on this stride")
						stride.Status = StrideError
					} else {
						log.Println("will try again")
						stride.Status = StrideRetrying
					}
					break
				}
				c.convertMetricsCache()
				log.Printf("added stride %+v with %d timeseries", *stride, len(c.metricsCache))
				err = c.materializeStrideData(stride)
				if err != nil {
					log.Printf("failed to materialize stride data: %v\n", err)
					stride.Status = StrideError
					break
				}
				stride.Status = StrideRead
				break
			}
		} else {
			if stride == nil {
				continue
			}
			if stride.Status == StrideRead {
				log.Printf("ready to extract graph edges for stride %+v\n", *stride)
				err = c.extractEdges(stride)
				if err != nil {
					log.Printf("failed to extract edges: %v\n", err)
					stride.Status = StrideError
					break
				}
			} else {
				continue
			}
		}

		// Only parse and add one file at a time.
		break
	}
	return nil
}

func directoryNameForStride(stride Stride) string {
	return fmt.Sprintf("stride_%d_%d", stride.ID, stride.StartTime)
}

func (c *CorrelationExplorer) convertMetricsCache() {
	for rowid, m := range c.metricsCacheByRowId {
		if m.Fingerprint == 0 {
			log.Printf("missing metrics fingerprint for row id %d, %v\n", rowid, *m)
			continue
		}
		c.metricsCache[m.Fingerprint] = rowid
	}
}

func parseStrideFromDirname(dirname string) (*Stride, error) {
	var strideCounter int
	var startTime int
	n, err := fmt.Sscanf(dirname, "stride_%d_%d", &strideCounter, &startTime)
	if n != 2 || err != nil {
		return nil, fmt.Errorf("failed to parse stride information out of dirname %s", dirname)
	}
	return &Stride{
		ID:        strideCounter,
		StartTime: int64(startTime),
	}, nil
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
		Status:          StrideExists,
		Filename:        filename,
	}, nil
}

func (c *CorrelationExplorer) retrieveEdges(stride *Stride, graphId int) ([]explorerlib.Edge, error) {
	dirname := directoryNameForStride(*stride)
	fullDirname := fmt.Sprintf("%s/%s", c.FilenameBase, dirname)
	edgeFile, err := os.Open(fmt.Sprintf("%s/edges_%d.json", fullDirname, graphId))
	if err != nil {
		return nil, fmt.Errorf("failed to open edges file for graph id %d and stride %d: %v",
			graphId, stride.ID, err)
	}
	edgeContents, _ := ioutil.ReadAll(edgeFile)
	var results []explorerlib.Edge
	json.Unmarshal(edgeContents, &results)
	return results, nil
}

func (c *CorrelationExplorer) extractEdges(stride *Stride) error {
	if stride == nil || stride.Subgraphs == nil {
		return fmt.Errorf("need a stride with subgraphs to get the edges")
	}
	parquetExplorer := explorerlib.NewParquetExplorer(c.FilenameBase)
	err := parquetExplorer.Initialize(stride.Filename)
	if err != nil {
		return err
	}
	subgraphs := stride.Subgraphs
	if subgraphs == nil {
		return fmt.Errorf("missing subgraphs for stride %d\n", stride.ID)
	}
	dirname := directoryNameForStride(*stride)
	fullDirname := fmt.Sprintf("%s/%s", c.FilenameBase, dirname)
	edgeFiles := make(map[int]*os.File)
	previousEdgeEntries := make(map[int]bool)
	defer terminateEdgeFiles(edgeFiles)
	edgeChan := make(chan []*explorerlib.Edge, 1)
	go parquetExplorer.GetEdges(edgeChan)
	for edges := range edgeChan {
		for _, e := range edges {
			if e == nil {
				continue
			}
			graphId := subgraphs.GetGraphId(e.Source)
			if graphId == -1 {
				return fmt.Errorf("missing graph id for for %d\n", e.Source)
			}
			edgeFile, exists := edgeFiles[graphId]
			if !exists {
				edgeFile, err = os.OpenFile(fmt.Sprintf("%s/edges_%d.json", fullDirname, graphId),
					os.O_WRONLY|os.O_CREATE, 0640)
				if err != nil {
					return fmt.Errorf("failed to create edge file %d: %v", graphId, err)
				}
				edgeFiles[graphId] = edgeFile
				edgeFile.Write([]byte("["))
			}
			_, exists = previousEdgeEntries[graphId]
			if exists {
				edgeFile.Write([]byte(","))
			} else {
				previousEdgeEntries[graphId] = true
			}
			edgeSerialized, err := json.Marshal(e)
			if err != nil {
				return err
			}
			_, err = edgeFile.Write(edgeSerialized)
			if err != nil {
				return fmt.Errorf("failed to write %T to edge file: %v", edgeSerialized, err)
			}
		}
	}
	return nil
}

func terminateEdgeFiles(files map[int]*os.File) {
	for _, f := range files {
		if f == nil {
			continue
		}
		_, err := f.Write([]byte("]"))
		if err != nil {
			log.Printf("failed to write closing ] to file %s: %v\n", f.Name(), err)
			continue
		}
		f.Close()
	}
}

func (c *CorrelationExplorer) readAndCacheSubgraphs(filename string, stride *Stride) error {
	if stride.Subgraphs == nil {
		log.Printf("requesting subgraphs for stride %d\n", stride.ID)
		parquetExplorer := explorerlib.NewParquetExplorer(c.FilenameBase)
		err := parquetExplorer.Initialize(stride.Filename)
		if err != nil {
			return err
		}
		// It takes about 90s to get these on my machine.
		subgraphs, err := parquetExplorer.GetSubgraphs()
		if err != nil {
			return err
		}
		stride.Subgraphs = subgraphs
		log.Printf("obtained subgraphs for stride %d\n", stride.ID)
	}
	return nil
}

func (c *CorrelationExplorer) materializeStrideData(stride *Stride) error {
	dirname := directoryNameForStride(*stride)
	subgraphsSerialized, err := json.Marshal(*(stride.Subgraphs))
	if err != nil {
		return err
	}
	file, err := os.Create(fmt.Sprintf("%s/%s/subgraphs.json", c.FilenameBase, dirname))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(subgraphsSerialized)
	return err
}

func (c *CorrelationExplorer) addStrideCacheEntry(stride *Stride) {
	c.strideCache[c.nextStrideCacheEntry] = stride
	c.nextStrideCacheEntry = (c.nextStrideCacheEntry + 1) % STRIDE_CACHE_SIZE
}

func (c *CorrelationExplorer) readResultFile(filename string, stride *Stride) error {
	parquetExplorer := explorerlib.NewParquetExplorer(c.FilenameBase)
	err := parquetExplorer.Initialize(filename)
	if err != nil {
		return err
	}
	log.Printf("reading metrics from %s\n", filename)
	err = parquetExplorer.GetMetrics(&c.metricsCacheByRowId)
	if err != nil {
		return err
	}
	log.Printf("read metrics, now reading subgraphs\n")
	return c.readAndCacheSubgraphs(filename, stride)
}

func (c *CorrelationExplorer) getLatestStride() int {
	return (c.nextStrideCacheEntry % STRIDE_CACHE_SIZE) - 1
}
