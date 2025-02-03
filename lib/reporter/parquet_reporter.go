package reporter

import (
	"encoding/json"
	"fmt"
	"github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/common/model"
	"log"
	"os"
	"path/filepath"
	"time"
)

type Timeseries struct {
	ID                int               `parquet:"id"`
	Metric            string            `parquet:"metric,optional,zstd"`
	MetricFingerprint uint64            `parquet:"metricFingerprint,optional"`
	Labels            map[string]string `parquet:"labels,optional"`

	// Cannot make this optional, as then '0' will be written as null.
	// Instead, when you want to say "no correlation information", leave the Pearson
	// field blank and set Correlated to be the same as ID.
	Correlated int `parquet:"correlated"`
	// There is no float16 datatype in go, but maybe a fixed-precision representation would be best.
	Pearson  float32 `parquet:"pearson,optional"`
	Constant bool    `parquet:"constant,optional"`
}

type ParquetReporter struct {
	filenameBase string
	//	tsids            []lib.TsId
	strideStartTimes map[int]string
	strideEndTimes   map[int]string
	// I tried a SortingWriter but it used too much memory.
	strideWriters      map[int](*parquet.GenericWriter[Timeseries])
	maxRowsPerRowGroup int64
}

func NewParquetReporter(filenameBase string, maxRows int64) *ParquetReporter {
	return &ParquetReporter{
		filenameBase:       filenameBase,
		strideStartTimes:   make(map[int]string),
		strideEndTimes:     make(map[int]string),
		strideWriters:      make(map[int]*parquet.GenericWriter[Timeseries]),
		maxRowsPerRowGroup: maxRows,
	}
}

func (r *ParquetReporter) InitializeStride(strideCounter int,
	strideStart time.Time, strideEnd time.Time) {

	writer, exists := r.strideWriters[strideCounter]
	if exists && writer != nil {
		return
	}

	r.strideStartTimes[strideCounter] = strideStart.UTC().Format("20060102150405")
	r.strideEndTimes[strideCounter] = strideEnd.UTC().Format("20060102150405")
	log.Printf("initializing with strideCounter %d, start time %s (%s), end time %s (%s)\n",
		strideCounter, r.strideStartTimes[strideCounter], strideStart.UTC().String(),
		r.strideEndTimes[strideCounter], strideEnd.UTC().String())

	startTime := r.strideStartTimes[strideCounter]
	endTime := r.strideEndTimes[strideCounter]
	filename := fmt.Sprintf("correlations_%d_%s-%s.pq", strideCounter, startTime, endTime)
	path := filepath.Join(r.filenameBase, filename)

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		log.Printf("failed to open ts parquet file: %e\n", err)
		return
	}

	// max rows per row group 10k is good for memory use but the files are about 3.5G per stride.
	r.strideWriters[strideCounter] = parquet.NewGenericWriter[Timeseries](file, parquet.MaxRowsPerRowGroup(r.maxRowsPerRowGroup))

}

func extractRowsFromResult(result datatypes.CorrjoinResult) []Timeseries {
	retsize := 2 * len(result.CorrelatedPairs)
	ret := make([]Timeseries, retsize, retsize)

	ctr := 0
	for pair, pearson := range result.CorrelatedPairs {
		rowids := pair.RowIds()
		ts1 := Timeseries{
			ID:         rowids[0],
			Correlated: rowids[1],
			Pearson:    float32(pearson),
		}
		ret[ctr] = ts1
		ctr++
		ts2 := Timeseries{
			ID:         rowids[1],
			Correlated: rowids[0],
			Pearson:    float32(pearson),
		}
		ret[ctr] = ts2
		ctr++
	}
	return ret
}

func (r *ParquetReporter) RecordTimeseriesIds(strideCounter int, tsids []lib.TsId) error {
	writer, exists := r.strideWriters[strideCounter]
	if !exists || writer == nil {
		return fmt.Errorf("missing writer for timeseries")
	}
	// r.tsids = tsids
	metadataRows := make([]Timeseries, len(tsids), len(tsids))
	for i, tsid := range tsids {
		var metricModel model.Metric

		// This copies tsid. It might be more efficient to store byte slices
		// in the tsids array instead?
		err := json.Unmarshal(([]byte)(tsid.MetricName), &metricModel)
		if err != nil {
			log.Printf("failed to unmarshal tsid %s: %e\n", tsid.MetricName, err)
			return err
		}
		row := Timeseries{
			ID:                i,
			Correlated:        i, // See above, this field is not optional.
			Metric:            string(metricModel["__name__"]),
			Labels:            make(map[string]string),
			MetricFingerprint: (uint64)(metricModel.Fingerprint()),
		}
		for key, value := range metricModel {
			if key == "__name__" {
				continue
			}
			row.Labels[string(key)] = string(value)
		}
		metadataRows[i] = row
	}
	n, err := writer.Write(metadataRows)
	log.Printf("wrote %d timeseries ids for stride %d\n", n, strideCounter)
	return err
}

func (r *ParquetReporter) AddConstantRows(strideCounter int, constantRows []bool) error {
	writer, exists := r.strideWriters[strideCounter]
	if !exists || writer == nil {
		return fmt.Errorf("missing writer for timeseries")
	}
	newRows := make([]Timeseries, 0, int(len(constantRows)/10))
	for rowid, isConstant := range constantRows {
		if isConstant {
			newRows = append(newRows, Timeseries{
				ID:         rowid,
				Correlated: rowid,
				Constant:   isConstant,
			})
		}
	}
	n, err := writer.Write(newRows)
	log.Printf("recorded %d constant rows for stride %d\n", n, strideCounter)
	return err
}

func (r *ParquetReporter) AddCorrelatedPairs(result datatypes.CorrjoinResult) error {
	writer, exists := r.strideWriters[result.StrideCounter]
	if !exists || writer == nil {
		return fmt.Errorf("missing writer for timeseries")
	}

	rows := extractRowsFromResult(result)
	_, err := writer.Write(rows)

	return err
}

func (r *ParquetReporter) Flush(strideCounter int) error {
	writer, exists := r.strideWriters[strideCounter]
	if !exists || writer == nil {
		return nil
	}
	defer writer.Close()
	return writer.Flush()
}
