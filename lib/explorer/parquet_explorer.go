package explorer

import (
	"errors"
	"fmt"
	"github.com/kpaschen/corrjoin/lib/reporter"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/common/model"
	"io"
	"log"
	"os"
	"path/filepath"
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
	results := make([]metricRow, 2000)
	for done := false; !done; {
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
				}
				m.Fingerprint = result.MetricFingerprint
				m.LabelSet[(model.LabelName)("__name__")] = (model.LabelValue)(result.Metric)
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
						LabelSet: (model.LabelSet)(make(map[model.LabelName]model.LabelValue)),
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
