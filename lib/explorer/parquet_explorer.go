package explorer

import (
	"errors"
	"fmt"
	"github.com/kpaschen/corrjoin/lib/reporter"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/common/model"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
)

type ParquetExplorer struct {
	filenameBase           string
	file                   *parquet.File
	idIndex                int
	correlatedIndex        int
	pearsonIndex           int
	constantIndex          int
	metricFingerprintIndex int
	metricIndex            int
	labelsKeyIndex         int
	labelsValueIndex       int
}

func NewParquetExplorer(filenameBase string) *ParquetExplorer {
	return &ParquetExplorer{
		filenameBase:           filenameBase,
		file:                   nil,
		idIndex:                -1,
		correlatedIndex:        -1,
		pearsonIndex:           -1,
		constantIndex:          -1,
		metricFingerprintIndex: -1,
		metricIndex:            -1,
		labelsKeyIndex:         -1,
		labelsValueIndex:       -1,
	}
}

func (p *ParquetExplorer) Initialize(filename string) error {
	schema := parquet.SchemaOf(reporter.Timeseries{})
	for _, path := range schema.Columns() {
		leaf, _ := schema.Lookup(path...)
		switch path[0] {
		case "id":
			p.idIndex = leaf.ColumnIndex
		case "correlated":
			p.correlatedIndex = leaf.ColumnIndex
		case "pearson":
			p.pearsonIndex = leaf.ColumnIndex
		case "metric":
			p.metricIndex = leaf.ColumnIndex
		case "metricFingerprint":
			p.metricFingerprintIndex = leaf.ColumnIndex
		case "labels":
			if len(path) == 3 {
				if path[2] == "key" {
					p.labelsKeyIndex = leaf.ColumnIndex
				} else if path[2] == "value" {
					p.labelsValueIndex = leaf.ColumnIndex
				}
			}
		}
	}

	if p.idIndex < 0 || p.correlatedIndex < 0 || p.pearsonIndex < 0 {
		return fmt.Errorf("bad schema: missing columns for id, correlated, or pearson")
	}

	filepath := filepath.Join(p.filenameBase, filename)

	pqfile, err := os.Open(filepath)
	if err != nil {
		log.Printf("failed to open ts parquet file %s: %v\n", filename, err)
		return err
	}
	stat, _ := pqfile.Stat()
	p.file, err = parquet.OpenFile(pqfile, stat.Size())
	if err != nil {
		log.Printf("Parquet: failed to open ts parquet file %s: %v\n", filename, err)
		return err
	}
	return nil
}

func (p *ParquetExplorer) Delete() error {
  p.file = nil
	return nil
}

// Reduced schema type for reading just metrics information from parquet.
type metricRow struct {
	ID                int               `parquet:"id"`
	Metric            string            `parquet:"metric,optional,zstd"`
	MetricFingerprint uint64            `parquet:"metricFingerprint,optional"`
	Labels            map[string]string `parquet:"labels,optional"`
	Constant          bool              `parquet:"constant,optional"`
}

func (p *ParquetExplorer) GetMetrics(cache *map[int]*Metric) error {
	reader := parquet.NewGenericReader[metricRow](p.file)
	defer reader.Close()
	for done := false; !done; {
		results := make([]metricRow, 2000)
		numRead, err := reader.Read(results)
		if err != nil {
			if errors.Is(err, io.EOF) {
				done = true
			} else {
				return err
			}
		}
		for i, result := range results {
			if i >= numRead {
				break
			}
			// The MetricFingerprint gets written on the rows that have the metrics
			// metadata, but not on the rows with the constant bit.
			if result.Metric != "" {
				m, exists := (*cache)[result.ID]
				if !exists {
					m = &Metric{
						RowId:       result.ID,
						Fingerprint: result.MetricFingerprint,
						LabelSet:    (model.LabelSet)(make(map[model.LabelName]model.LabelValue)),
					}
					(*cache)[result.ID] = m
				} else {
					log.Printf("metric found for row id %d and metric fp %d: %v\n", m.RowId, m.Fingerprint, m)
					log.Printf("expanding with fingerprint %d, name %s, and labels %v\n",
						result.MetricFingerprint, result.Metric, result.Labels)
				}
				m.Fingerprint = result.MetricFingerprint
				m.LabelSet["__name__"] = (model.LabelValue)(result.Metric)
				// TODO: these are currently always on the same row as the metric name, not sure
				// if that will stay that way.
				if result.Labels != nil {
					for k, v := range result.Labels {
						m.LabelSet[(model.LabelName)(k)] = (model.LabelValue)(v)
					}
				}
			}
			if result.Constant {
				m, exists := (*cache)[result.ID]
				if !exists {
					m = &Metric{
						RowId:    result.ID,
						Constant: true,
						LabelSet: make(map[model.LabelName]model.LabelValue),
					}
					(*cache)[result.ID] = m
				} else {
					m.Constant = true
				}
			}
		}
	}
	return nil
}

func (s *SubgraphMemberships) addPair(row1 int, row2 int) {
	c1, have1 := s.Rows[row1]
	c2, have2 := s.Rows[row2]

	// Case 1: neither row has a graph yet -> add them both to a new graph.
	if !have1 && !have2 {
		s.Rows[row1] = s.nextSubgraphId
		s.Rows[row2] = s.nextSubgraphId
		s.Sizes[s.nextSubgraphId] += 2
		s.nextSubgraphId++
		return
	}

	// Case 2: only one row has a graph id
	if !have2 {
		s.Rows[row2] = c1
		s.Sizes[c1]++
		return
	}

	if !have1 {
		s.Rows[row1] = c2
		s.Sizes[c2]++
		return
	}

	// Case 2: both rows have a graph id. Nothing to do if they match,
	// otherwise merge.
	if c1 == c2 {
		return
	}

	// Always merge into the lower-numbered graph.
	if c1 < c2 {
		for row, graph := range s.Rows {
			if graph == c2 {
				s.Rows[row] = c1
				s.Sizes[c1]++
			}
		}
		delete(s.Sizes, c2)
	} else {
		for row, graph := range s.Rows {
			if graph == c1 {
				s.Rows[row] = c2
				s.Sizes[c2]++
			}
		}
		delete(s.Sizes, c1)
	}
}

func (s *SubgraphMemberships) GetGraphId(rowId int) int {
	if s.Rows == nil {
		return -1
	}
	graphId, ok := s.Rows[rowId]
	if !ok {
		return -1
	}
	return graphId
}

// Read subgraph information from a parquet file.
func (p *ParquetExplorer) GetSubgraphs() (*SubgraphMemberships, error) {
	reader := parquet.NewGenericReader[reporter.Timeseries](p.file)
	defer reader.Close()
	subgraphs := &SubgraphMemberships{
		Rows:           make(map[int]int),
		Sizes:          make(map[int]int),
		nextSubgraphId: 0,
	}
	results := make([]reporter.Timeseries, 2000)
	for done := false; !done; {
		numRead, err := reader.Read(results)
		if err != nil {
			if errors.Is(err, io.EOF) {
				done = true
			} else {
				return nil, err
			}
		}
		for i, result := range results {
			if i >= numRead {
				break
			}
			if result.Constant {
				continue
			}
			if !(result.Pearson > 0.0) {
				continue
			}
			subgraphs.addPair(result.ID, result.Correlated)
		}
	}
	return subgraphs, nil
}

func (p *ParquetExplorer) GetEdges(edgeChan chan<- []*Edge) error {
	defer close(edgeChan)
	if p.file == nil {
		return fmt.Errorf("parquet explorer has no parquet file")
	}
	reader := parquet.NewGenericReader[reporter.Timeseries](p.file)
	defer reader.Close()
	results := make([]reporter.Timeseries, 2000)
	for done := false; !done; {
		numRead, err := reader.Read(results)
		if err != nil {
			if errors.Is(err, io.EOF) {
				done = true
			} else {
				return err
			}
		}
		edgeBuf := make([]*Edge, 0, numRead)
		for i, result := range results {
			if i >= numRead {
				break
			}
			if result.Constant {
				continue
			}
			if !(result.Pearson > 0.0) {
				continue
			}
			if result.ID >= result.Correlated {
				continue
			}
			edgeBuf = append(edgeBuf, &Edge{
				Source:  result.ID,
				Target:  result.Correlated,
				Pearson: result.Pearson,
			})
		}
		edgeChan <- edgeBuf
	}
	return nil
}

// TODO: parse into Metric
func (p *ParquetExplorer) LookupMetric(timeSeriesId int) (map[string]string, error) {
	if p.file == nil {
		return nil, fmt.Errorf("parquet explorer has no parquet file")
	}
	ret := make(map[string]string)
	for _, rg := range p.file.RowGroups() {
		idchunk := rg.ColumnChunks()[p.idIndex]
		ididx, _ := idchunk.ColumnIndex()
		found := parquet.Find(ididx, parquet.ValueOf(timeSeriesId),
			parquet.CompareNullsLast(idchunk.Type().Compare))
		if found == ididx.NumPages() {
			// Id is not in this row group
			continue
		}
		reader := parquet.NewGenericReader[reporter.Timeseries](p.file)
		defer reader.Close()
		offsetidx, _ := idchunk.OffsetIndex()
		reader.SeekToRow(offsetidx.FirstRowIndex(found))
		results := make([]reporter.Timeseries, 10)
		// Read the next rows into the given rows slice
		// returns the number of rows read and an io.EOF when no more rows
		for done := false; !done; {
			numRead, err := reader.Read(results)
			if err != nil {
				if errors.Is(err, io.EOF) {
					done = true
				} else {
					return ret, err
				}
			}
			for i, result := range results {
				if i >= numRead {
					break
				}
				if result.ID != timeSeriesId {
					continue
				}
				if result.Metric != "" {
					ret["__name__"] = result.Metric
				}
				if result.Labels != nil {
					for k, v := range result.Labels {
						ret[k] = v
					}
				}
			}
		}
		reader.Close()
	}
	return ret, nil
}

func (m *Metric) ComputePrometheusGraphURL(prometheusBaseURL string, timeRange string, endTime string) {
	if len(m.LabelSet) == 0 {
		m.PrometheusGraphURL = fmt.Sprintf("%s/graph", prometheusBaseURL)
		return
	}
	queryString := m.ComputePrometheusQuery()

	m.PrometheusGraphURL = fmt.Sprintf("%s/graph?g0.expr=%s&g0.tab=0&g0.display_mode=lines&g0.show_exemplars=0&g0.range_input=%s&g0.end_input=%s&g0.moment_input=%s",
		prometheusBaseURL, queryString, timeRange, url.QueryEscape(endTime),
		url.QueryEscape(endTime))
}

func (m *Metric) ComputePrometheusQuery() string {
	name := ""
	attributeString := "{"
	first := true
	for key, value := range m.LabelSet {
		stringv := value
		if key == "__name__" {
			name = string(stringv)
			continue
		}
		if first {
			attributeString = fmt.Sprintf("%s%s=\"%s\"", attributeString, key, string(stringv))
			first = false
		} else {
			attributeString = fmt.Sprintf("%s, %s=\"%s\"", attributeString, key, string(stringv))
		}
	}
	attributeString = attributeString + "}"
	attributeString = url.QueryEscape(attributeString)
	return fmt.Sprintf("%s%s", name, attributeString)
}

func (m *Metric) MetricString() string {
	labelNames := make([]string, 0, len(m.LabelSet))
	for key := range m.LabelSet {
		if key == "__name__" {
			continue
		}
		labelNames = append(labelNames, string(key))
	}
	sort.Strings(labelNames)
	first := true
	attributeString := "{"
	for _, labelName := range labelNames {
		value, ok := m.LabelSet[model.LabelName(labelName)]
		if !ok {
			continue
		}
		if first {
			attributeString = fmt.Sprintf("%s%s=\"%s\"", attributeString, labelName, string(value))
			first = false
		} else {
			attributeString = fmt.Sprintf("%s, %s=\"%s\"", attributeString, labelName, string(value))
		}
	}
	attributeString = attributeString + "}"
	return attributeString
}
