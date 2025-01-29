package reporter

import (
  "encoding/json"
	"fmt"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
  "github.com/parquet-go/parquet-go"
  "github.com/prometheus/common/model"
	"log"
	"os"
	"path/filepath"
	"time"
)

type Timeseries struct {
  ID int  `parquet:"id"`
  Metric string `parquet:"metric,optional,zstd"`
  Labels map[string]string `parquet:"labels,optional"`

  // Cannot make this optional, as then '0' will be written as null.
  // Instead, when you want to say "no correlation information", leave the Pearson
  // field blank and set Correlated to be the same as ID.
  Correlated int `parquet:"correlated"` 
  // There is no float16 datatype in go, but maybe a fixed-precision representation would be best.
  Pearson float32 `parquet:"pearson,optional"`
  Constant bool `parquet:"constant,optional"`
}

type ParquetReporter struct {
	filenameBase     string
	tsids            []string
	strideStartTimes map[int]string
	strideEndTimes   map[int]string
  writer *parquet.SortingWriter[Timeseries]
  filepath string
}

func NewParquetReporter(filenameBase string) *ParquetReporter {
	return &ParquetReporter{
		filenameBase:     filenameBase,
		strideStartTimes: make(map[int]string),
		strideEndTimes:   make(map[int]string),
    writer: nil,
    filepath: "",
	}
}

// TODO: unused parameter config
func (r *ParquetReporter) Initialize(config settings.CorrjoinSettings, strideCounter int,
	strideStart time.Time, strideEnd time.Time, tsids []string) {
	r.tsids = tsids
	r.strideStartTimes[strideCounter] = strideStart.UTC().Format("20060102150405")
	r.strideEndTimes[strideCounter] = strideEnd.UTC().Format("20060102150405")
	log.Printf("initializing with strideCounter %d, start time %s (%s), end time %s (%s)\n",
		strideCounter, r.strideStartTimes[strideCounter], strideStart.UTC().String(),
		r.strideEndTimes[strideCounter], strideEnd.UTC().String())

	startTime := r.strideStartTimes[strideCounter]
	endTime := r.strideEndTimes[strideCounter]
	filename := fmt.Sprintf("correlations_%d_%s-%s.pq", strideCounter, startTime, endTime)
	r.filepath = filepath.Join(r.filenameBase, filename)

  file, err := os.OpenFile(r.filepath, os.O_WRONLY|os.O_CREATE, 0640)
  if err != nil {
    log.Printf("failed to open ts parquet file: %e\n", err)
    return
  }

  r.writer = parquet.NewSortingWriter[Timeseries](file, 1000,
    parquet.SortingWriterConfig(
      parquet.SortingColumns(
        parquet.Ascending("id"),
      ),
    ),
  )

  metadataRows := make([]Timeseries, len(tsids), len(tsids))
  for i, tsid := range tsids {
     var metricModel model.Metric

     // This copies tsid. It might be more efficient to store byte slices
     // in the tsids array instead?
     err := json.Unmarshal(([]byte)(tsid), &metricModel)
     if err != nil {
        log.Printf("failed to unmarshal tsid %s: %e\n", tsid, err)
        return
     }
     row := Timeseries{
        ID: i,
        Correlated: i, // See above, this field is not optional.
        Metric: string(metricModel["__name__"]),
        Labels: make(map[string]string),
     }
     for key, value := range metricModel {
        if key == "__name__" { continue }
        row.Labels[string(key)] = string(value)
     }
     metadataRows[i] = row
  }
  r.writer.Write(metadataRows)
}

func extractRowsFromResult(result datatypes.CorrjoinResult) []Timeseries {
   retsize := 2 * len(result.CorrelatedPairs)
   ret := make([]Timeseries, retsize, retsize)

   ctr := 0
   for pair, pearson := range result.CorrelatedPairs {
      rowids := pair.RowIds()
      ts1 := Timeseries{
         ID: rowids[0],
         Correlated: rowids[1],
         Pearson: float32(pearson),
      }
      ret[ctr] = ts1
      ctr++
      ts2 := Timeseries{
         ID: rowids[1],
         Correlated: rowids[0],
         Pearson: float32(pearson),
      }
      ret[ctr] = ts2
      ctr++
   }

   return ret
}

func (r *ParquetReporter) AddCorrelatedPairs(result datatypes.CorrjoinResult) error {
  if r.writer == nil {
     return fmt.Errorf("missing writer for timeseries")
  }

  rows := extractRowsFromResult(result)

  n, err := r.writer.Write(rows)
  if err != nil {
     log.Printf("correlated pairs writer returned %d\n", n)
  }
  
	return err
}

func (r *ParquetReporter) Flush() error {
   if r.writer == nil {
      return nil
   }
   defer r.writer.Close()
   return r.writer.Flush()
}
