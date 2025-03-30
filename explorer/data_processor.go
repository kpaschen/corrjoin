package explorer

import (
	"encoding/csv"
	"fmt"
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	STRIDE_CACHE_SIZE = 10
)

type CorrelationExplorer struct {
	FilenameBase      string
	strideCache       []*Stride
	prometheusBaseURL string

	maxAgeSeconds int
	ticker        *time.Ticker
	dropLabels    map[string]bool
}

func (c *CorrelationExplorer) Initialize(baseUrl string, maxAgeSeconds int, dropLabels []string) error {
	c.prometheusBaseURL = baseUrl
	c.maxAgeSeconds = maxAgeSeconds
	c.strideCache = make([]*Stride, STRIDE_CACHE_SIZE, STRIDE_CACHE_SIZE)
	c.ticker = time.NewTicker(180 * time.Second)
	c.dropLabels = make(map[string]bool)

	for _, lb := range dropLabels {
		c.dropLabels[lb] = true
	}

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
	var strideParsedFromFilename *Stride
	for _, e := range entries {
		if !e.IsDir() {
			strideParsedFromFilename, err = parseStrideFromFilename(e.Name())
			if err != nil {
				// This is not a stride file.
				continue
			}
		} else {
			strideParsedFromFilename, err = parseStrideFromDirname(e.Name())
			if err != nil {
				// This is not a stride directory.
				continue
			}
		}
		if strideParsedFromFilename == nil {
			log.Printf("stride parsed from filename %s is nil\n", e.Name())
			continue
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

		// See if we already have a stride with this id and a different time.
		// In that case, delete the older file/directory.
		for _, s := range c.strideCache {
			if s == nil {
				break
			}
			if s.ID == strideParsedFromFilename.ID {
				if s.StartTime < strideParsedFromFilename.StartTime {
					log.Printf("existing stride with id %d and filename %s is older than new stride with filename %s\n",
						s.ID, s.Filename, e.Name())
					fullPath := filepath.Join(c.FilenameBase, s.Filename)
					log.Printf("try to remove %s\n", fullPath)
					err = os.RemoveAll(fullPath)
					if err != nil {
						log.Printf("failed to remove %s: %v\n", fullPath, err)
						continue
					}
					fullPath = filepath.Join(c.FilenameBase, directoryNameForStride(*s))
					log.Printf("try to remove %s\n", fullPath)
					err = os.RemoveAll(fullPath)
					if err != nil {
						log.Printf("failed to remove %s: %v\n", fullPath, err)
						continue
					}
					s.Status = StrideDeleted
				} else if strideParsedFromFilename.StartTime < s.StartTime {
					log.Printf("new stride with id %d and filename %s is older than existing stride with filename %s\n",
						strideParsedFromFilename.ID, e.Name(), s.Filename)
					fullPath := filepath.Join(c.FilenameBase, e.Name())
					err = os.RemoveAll(fullPath)
					if err != nil {
						log.Printf("failed to remove %s: %v\n", fullPath, err)
						continue
					}
				}
				// If the times are equal, then probably one is the parquet file and the other is the directory,
				// so nothing to do.
				break
			}
		}

		age := int(time.Now().UTC().Unix() - t.ModTime().UTC().Unix())
		if c.maxAgeSeconds > 0 && age > c.maxAgeSeconds {
			fullPath := filepath.Join(c.FilenameBase, e.Name())
			log.Printf("file %s should be deleted\n", fullPath)
			err = os.RemoveAll(fullPath)
			if err != nil {
				log.Printf("failed to remove %s: %v\n", fullPath, err)
				continue
			}
			if stride != nil {
				stride.Status = StrideDeleted
			}
			continue
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
				log.Printf("found file %s and i do not have a stride for it yet\n", e.Name())
				stride, err = parseStrideFromFilename(e.Name())
				if err != nil {
					log.Printf("failed to parse stride info from filename: %e\n", err)
					continue
				}
				log.Printf("created stride\n")
				c.addStrideCacheEntry(stride)
			}
			switch stride.Status {
			case StrideError:
				continue
			case StrideDeleted:
				continue
			case StrideRead:
				continue
			case StrideProcessing:
				continue
			case StrideProcessed:
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
					if age > 600 {
						log.Println("giving up on this stride")
						stride.Status = StrideError
					} else {
						stride.Status = StrideRetrying
					}
					break
				}
				log.Printf("added stride %d with %d timeseries", stride.ID, len(stride.metricsCache))
				stride.Status = StrideRead
				break
			}
		} else {
			if stride == nil {
				continue
			}
			if stride.Status == StrideRead {
				log.Printf("ready to extract graph edges for stride %d\n", stride.ID)
				stride.Status = StrideProcessing
				err = c.extractEdges(stride)
				if err != nil {
					log.Printf("failed to extract edges: %v\n", err)
					stride.Status = StrideError
					break
				}
				log.Printf("stride %d is now ready for exploration\n", stride.ID)
				stride.Status = StrideProcessed
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
		StartTimeString: startT.UTC().Format(explorerlib.FORMAT),
		EndTime:         endT.UTC().Unix(),
		EndTimeString:   endT.UTC().Format(explorerlib.FORMAT),
		Status:          StrideExists,
		Filename:        filename,
		metricsCache:    make(map[uint64](*explorerlib.Metric)),
	}, nil
}

func (c *CorrelationExplorer) retrieveCorrelatedTimeseries(stride *Stride, tsRowId uint64,
	onlyConsider []uint64, maxResults int) (map[uint64]float32, error) {

	if stride == nil || stride.subgraphs == nil {
		return nil, fmt.Errorf("stride has no subgraphs")
	}

	ret := make(map[uint64]float32)

	metric, exists := stride.metricsCache[tsRowId]
	if !exists {
		return ret, fmt.Errorf("no metric with id %d", tsRowId)
	}
	if metric.Constant {
		log.Printf("metric %d is constant in stride %d\n", tsRowId, stride.ID)
		return ret, nil
	}

	graphId, exists := stride.subgraphs.Rows[tsRowId]
	if !exists {
		log.Printf("no graph found for row id %d in stride %d\n", tsRowId, stride.ID)
		return ret, nil // This timeseries is not correlated with anything.
	}
	edges, err := c.retrieveEdges(stride, graphId, MAX_GRAPH_SIZE)
	if err != nil {
		log.Printf("failed to retrieve edges: %v\n", err)
		return ret, err
	}

	ctr := 0
	for _, e := range edges {
		if e.Pearson > 0 && (e.Source == tsRowId || e.Target == tsRowId) {
			var otherRowId uint64
			if e.Source == tsRowId {
				otherRowId = e.Target
			} else {
				otherRowId = e.Source
			}
			if len(onlyConsider) > 0 {
				found := false
				for _, c := range onlyConsider {
					if c == otherRowId {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			ret[otherRowId] = e.Pearson
			ctr++
			if maxResults > 0 && ctr >= maxResults {
				break
			}
		}
	}
	log.Printf("returning %d correlates for ts %d in stride %d\n", len(ret), tsRowId, stride.ID)
	return ret, nil
}

func (c *CorrelationExplorer) retrieveEdges(stride *Stride, graphId int, maxNodes int) ([]explorerlib.Edge, error) {
	dirname := directoryNameForStride(*stride)
	fullDirname := fmt.Sprintf("%s/%s", c.FilenameBase, dirname)
	edgeFile, err := os.Open(fmt.Sprintf("%s/edges_%d.csv", fullDirname, graphId))
	if err != nil {
		return nil, fmt.Errorf("failed to open edges file for graph id %d and stride %d: %v",
			graphId, stride.ID, err)
	}
	defer edgeFile.Close()

	reader := csv.NewReader(edgeFile)
	reader.ReuseRecord = true
	var results []explorerlib.Edge
	// Read the header line
	_, err = reader.Read()
	if err != nil {
		if err == io.EOF {
			log.Printf("got eof immediately on file edges_%d.csv\n", graphId)
			return results, nil
		}
		log.Printf("error reading edges file: %v\n", err)
		return nil, err
	}
	ctr := 0
	for {
		row, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				log.Printf("reached end of file\n")
				return results, nil
			}
			log.Printf("error reading edges file: %v\n", err)
			return nil, err
		}
		source, err := strconv.ParseUint(row[0], 10, 64)
		if err != nil {
			return nil, err
		}
		target, err := strconv.ParseUint(row[1], 10, 64)
		if err != nil {
			return nil, err
		}
		pearson, err := strconv.ParseFloat(row[2], 32)
		if err != nil {
			return nil, err
		}

		results = append(results, explorerlib.Edge{
			Source:  uint64(source),
			Target:  uint64(target),
			Pearson: float32(pearson),
		})
		ctr++
		if maxNodes > 0 && ctr > maxNodes {
			// Returning an empty edges list will make the graph render without edges.
			log.Printf("not returning all edges since maxNodes %d was exceeded\n", maxNodes)
			return []explorerlib.Edge{}, nil
		}
	}
}

func (c *CorrelationExplorer) extractEdges(stride *Stride) error {
	if stride == nil || stride.subgraphs == nil {
		return fmt.Errorf("need a stride with subgraphs to get the edges")
	}
	parquetExplorer := explorerlib.NewParquetExplorer(c.FilenameBase)
	err := parquetExplorer.Initialize(stride.Filename)
	if err != nil {
		return err
	}
	defer parquetExplorer.Delete()
	subgraphs := stride.subgraphs
	if subgraphs == nil {
		return fmt.Errorf("missing subgraphs for stride %d\n", stride.ID)
	}
	dirname := directoryNameForStride(*stride)
	fullDirname := fmt.Sprintf("%s/%s", c.FilenameBase, dirname)
	edgeFiles := make(map[int]*os.File)
	edgeWriters := make(map[int]*csv.Writer)
	defer terminateEdgeFiles(edgeWriters, edgeFiles)
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
			size := subgraphs.Sizes[graphId]
			if size < 2 {
				continue
			}
			edgeFile, exists := edgeFiles[graphId]
			if !exists {
				edgeFile, err = os.OpenFile(fmt.Sprintf("%s/edges_%d.csv", fullDirname, graphId),
					os.O_WRONLY|os.O_CREATE, 0640)
				if err != nil {
					return fmt.Errorf("failed to create edge file %d: %v", graphId, err)
				}
				edgeWriter := csv.NewWriter(edgeFile)
				edgeFiles[graphId] = edgeFile
				edgeWriters[graphId] = edgeWriter
				err = edgeWriter.Write([]string{"ID", "Correlated", "Pearson"})
				if err != nil {
					return fmt.Errorf("failed to write header to edge csv file: %v\n", err)
				}
			}
			err = edgeWriters[graphId].Write([]string{fmt.Sprintf("%d", e.Source), fmt.Sprintf("%d", e.Target), fmt.Sprintf("%f", e.Pearson)})
			if err != nil {
				return fmt.Errorf("failed to write %v to edge file: %v", e, err)
			}
		}
	}
	return nil
}

func terminateEdgeFiles(writers map[int]*csv.Writer, files map[int]*os.File) {
	for _, w := range writers {
		if w == nil {
			continue
		}
		w.Flush()
	}
	for _, f := range files {
		if f == nil {
			continue
		}
		f.Close()
	}
}

func (c *CorrelationExplorer) readAndCacheSubgraphs(parquetExplorer *explorerlib.ParquetExplorer, stride *Stride) error {
	if stride.subgraphs == nil {
		log.Printf("requesting subgraphs for stride %d\n", stride.ID)
		// It takes about 90s to get these on my machine.
		subgraphs, err := parquetExplorer.GetSubgraphs()
		if err != nil {
			return err
		}
		stride.subgraphs = subgraphs
	}
	return nil
}

func (c *CorrelationExplorer) addStrideCacheEntry(stride *Stride) {
	oldestTime := stride.StartTime
	oldestEntry := -1
	for i, s := range c.strideCache {
		if s == nil {
			c.strideCache[i] = stride
			return
		}
		if s.Status == StrideDeleted {
			log.Printf("removing deleted stride for time %v from cache\n", s.StartTime)
			s = nil
			c.strideCache[i] = stride
			return
		}
		if oldestEntry == -1 || s.StartTime < oldestTime {
			oldestTime = s.StartTime
			oldestEntry = i
		}
	}
	if oldestEntry == -1 {
		log.Printf("no entry found for stride %d with start time %v\n", stride.ID, stride.StartTime)
	}
	if c.strideCache[oldestEntry] != nil {
		log.Printf("evicting stride from time %v from cache\n", c.strideCache[oldestEntry].StartTime)
		s := c.strideCache[oldestEntry]
		fullPath := filepath.Join(c.FilenameBase, s.Filename)
		log.Printf("try to remove %s\n", fullPath)
		err := os.RemoveAll(fullPath)
		if err != nil {
			log.Printf("failed to remove %s: %v\n", fullPath, err)
		}
		fullPath = filepath.Join(c.FilenameBase, directoryNameForStride(*s))
		log.Printf("try to remove %s\n", fullPath)
		err = os.RemoveAll(fullPath)
		if err != nil {
			log.Printf("failed to remove %s: %v\n", fullPath, err)
		}
		s.Status = StrideDeleted
		c.strideCache[oldestEntry] = nil
	}
	c.strideCache[oldestEntry] = stride
}

func (c *CorrelationExplorer) readResultFile(filename string, stride *Stride) error {
	parquetExplorer := explorerlib.NewParquetExplorer(c.FilenameBase)
	err := parquetExplorer.Initialize(filename)
	if err != nil {
		return err
	}
	defer parquetExplorer.Delete()
	log.Printf("reading metrics from %s\n", filename)
	err = parquetExplorer.GetMetrics(&stride.metricsCache)
	if err != nil {
		return err
	}
	log.Printf("read metrics, now reading subgraphs\n")
	return c.readAndCacheSubgraphs(parquetExplorer, stride)
}

func (c *CorrelationExplorer) getLatestStride() *Stride {
	newestEntry := -1
	for i, s := range c.strideCache {
		if s == nil {
			continue
		}
		if s.Status != StrideProcessed {
			continue
		}
		if newestEntry == -1 || s.StartTime > c.strideCache[newestEntry].StartTime {
			newestEntry = i
		}
	}
	if newestEntry == -1 {
		log.Printf("no valid strides found\n")
		return nil
	}
	return c.strideCache[newestEntry]
}

func (c *CorrelationExplorer) parseTsIdsFromGraphiteResult(graphite string) ([]uint64, error) {
	metricId, err := strconv.ParseUint(graphite, 10, 64)
	if err == nil {
		return []uint64{metricId}, nil
	}
	strippedGraphite := strings.Trim(graphite, "{}")
	tsids := strings.Split(strippedGraphite, ",")
	ret := make([]uint64, len(tsids), len(tsids))
	for i, tsid := range tsids {
		var metricId uint64
		res, err := fmt.Sscanf(tsid, "fp-%d", &metricId)
		if err != nil || res == 0 {
			metricId, err = strconv.ParseUint(tsid, 10, 64)
			if err != nil {
				return nil, err
			}
		}
		log.Printf("parsed %d out of %s\n", metricId, tsid)
		ret[i] = metricId
	}
	return ret, nil
}
