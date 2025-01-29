package reporter

import (
  "encoding/json"
  "fmt"
  "os"
  "strings"
	"testing"
  "time"
  "github.com/kpaschen/corrjoin/lib/datatypes"
  "github.com/kpaschen/corrjoin/lib/settings"
  "github.com/parquet-go/parquet-go"
  "github.com/prometheus/common/model"
)

func TestInitialize(t *testing.T) {
  tempdir, err := os.MkdirTemp("", "corrjoinTest")
  if err != nil {
     t.Errorf("failed to create temp dir")
  }
  defer os.RemoveAll(tempdir)
	rep := NewParquetReporter(tempdir)
 
  sampleMetric := model.Metric{}
  sampleMetric[model.LabelName("__name__")] = model.LabelValue("myMetric") 
  sampleMetric[model.LabelName("pod")] = model.LabelValue("podxy") 
  sampleMetricString, err := json.Marshal(sampleMetric)
  if err != nil { t.Errorf("failed to marshal sample metric") }

  sampleMetric = model.Metric{}
  sampleMetric[model.LabelName("__name__")] = model.LabelValue("myOtherMetric") 
  sampleMetric[model.LabelName("pod")] = model.LabelValue("podab") 
  sampleMetricString2, err := json.Marshal(sampleMetric)
  if err != nil { t.Errorf("failed to marshal sample metric") }

  settings := settings.CorrjoinSettings{
    SvdDimensions:        3,
    EuclidDimensions:     2,
    CorrelationThreshold: 0.9,
    WindowSize:           3,
    SvdOutputDimensions:  2,
  }.ComputeSettingsFields()
  rep.Initialize(settings, 1, time.Now(), time.Now(), []string{string(sampleMetricString), string(sampleMetricString2)})

  rep.Flush()
}

func TestAddCorrelatedPairs(t *testing.T) {
  tempdir, err := os.MkdirTemp("", "corrjoinTest")
  if err != nil {
     t.Errorf("failed to create temp dir")
  }
  fmt.Printf("temp dir: %s\n", tempdir)
  // defer os.RemoveAll(tempdir)
	rep := NewParquetReporter(tempdir)
 
  sampleMetric := model.Metric{}
  sampleMetric[model.LabelName("__name__")] = model.LabelValue("myMetric") 
  sampleMetric[model.LabelName("pod")] = model.LabelValue("podxy") 
  sampleMetricString, err := json.Marshal(sampleMetric)
  if err != nil { t.Errorf("failed to marshal sample metric") }

  sampleMetric = model.Metric{}
  sampleMetric[model.LabelName("__name__")] = model.LabelValue("myOtherMetric") 
  sampleMetric[model.LabelName("pod")] = model.LabelValue("podab") 
  sampleMetricString2, err := json.Marshal(sampleMetric)
  if err != nil { t.Errorf("failed to marshal sample metric") }

  settings := settings.CorrjoinSettings{
    SvdDimensions:        3,
    EuclidDimensions:     2,
    CorrelationThreshold: 0.9,
    WindowSize:           3,
    SvdOutputDimensions:  2,
  }.ComputeSettingsFields()
  rep.Initialize(settings, 1, time.Now(), time.Now(), []string{string(sampleMetricString), string(sampleMetricString2)})

  results1 := datatypes.CorrjoinResult{
     CorrelatedPairs: make(map[datatypes.RowPair]float64),
     StrideCounter: 1,
  }
  rp1 := datatypes.NewRowPair(1, 2)
  results1.CorrelatedPairs[*rp1] = 0.95

  rp2 := datatypes.NewRowPair(0, 2)
  results1.CorrelatedPairs[*rp2] = 0.99

  err = rep.AddCorrelatedPairs(results1)
  if err != nil { t.Errorf("failed to add correlated pair") }

  rep.Flush()

  var idIndex, correlatedIndex, pearsonIndex int
  schema := parquet.SchemaOf(Timeseries{})
  for _, path := range schema.Columns() {
    leaf, _ :=  schema.Lookup(path...)
    v := strings.Join(path, ".")
    if v == "id" { idIndex = leaf.ColumnIndex }
    if v == "correlated" { correlatedIndex = leaf.ColumnIndex }
    if v == "pearson" { pearsonIndex = leaf.ColumnIndex }
    fmt.Printf("%d -> %q\n", leaf.ColumnIndex, strings.Join(path, "."))
  }

  pqfile, err := os.Open(rep.filepath)
  if err != nil { t.Errorf("failed to open results file") }
  stat, _ := pqfile.Stat()
  p, err := parquet.OpenFile(pqfile, stat.Size())
  if err != nil { t.Errorf("parquet failed to open results file") }
  
  for _, rg := range p.RowGroups() {
     fmt.Printf("handling row group %+v\n", rg)
     idchunk := rg.ColumnChunks()[idIndex]
     ididx, _ := idchunk.ColumnIndex()
     found := parquet.Find(ididx, parquet.ValueOf(0), parquet.CompareNullsLast(idchunk.Type().Compare)) 
     if found == ididx.NumPages() {
        fmt.Println("page for id 0 not found in current row group")
        continue
     }
     fmt.Printf("find returned page %d for id 0\n", found)
     
     correlatedChunk := rg.ColumnChunks()[correlatedIndex]
     pearsonChunk := rg.ColumnChunks()[pearsonIndex]
     _ = correlatedChunk
     _ = pearsonChunk
     reader := parquet.NewGenericReader[Timeseries](pqfile) 
     defer reader.Close()
     offsetidx, _ := idchunk.OffsetIndex()
     reader.SeekToRow(offsetidx.FirstRowIndex(found))
     result := make([]Timeseries, 10)
     // Read the next rows into the given rows slice
     // returns the number of rows read and an io.EOF when no more rows
     _, _ = reader.Read(result)
     for _, results := range result {
        // Need to check these results for a match on the id column
        fmt.Printf("row in range: %v\n", results)
     }
  }
}
