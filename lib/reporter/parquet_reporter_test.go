package reporter

import (
	"encoding/json"
	"errors"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/settings"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/common/model"
	"io"
	"os"
	"strings"
	"testing"
	"time"
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
	if err != nil {
		t.Errorf("failed to marshal sample metric")
	}

	sampleMetric = model.Metric{}
	sampleMetric[model.LabelName("__name__")] = model.LabelValue("myOtherMetric")
	sampleMetric[model.LabelName("pod")] = model.LabelValue("podab")
	sampleMetricString2, err := json.Marshal(sampleMetric)
	if err != nil {
		t.Errorf("failed to marshal sample metric")
	}

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
	defer os.RemoveAll(tempdir)
	rep := NewParquetReporter(tempdir)

	sampleMetric := model.Metric{}
	sampleMetric[model.LabelName("__name__")] = model.LabelValue("myMetric")
	sampleMetric[model.LabelName("pod")] = model.LabelValue("podxy")
	sampleMetricString, err := json.Marshal(sampleMetric)
	if err != nil {
		t.Errorf("failed to marshal sample metric")
	}

	sampleMetric = model.Metric{}
	sampleMetric[model.LabelName("__name__")] = model.LabelValue("myOtherMetric")
	sampleMetric[model.LabelName("pod")] = model.LabelValue("podab")
	sampleMetricString2, err := json.Marshal(sampleMetric)
	if err != nil {
		t.Errorf("failed to marshal sample metric")
	}

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
		StrideCounter:   1,
	}
	rp1 := datatypes.NewRowPair(1, 2)
	results1.CorrelatedPairs[*rp1] = 0.95

	rp2 := datatypes.NewRowPair(0, 2)
	results1.CorrelatedPairs[*rp2] = 0.99

	err = rep.AddCorrelatedPairs(results1)
	if err != nil {
		t.Errorf("failed to add correlated pair")
	}

	rep.Flush()

	idIndex := -1
	schema := parquet.SchemaOf(Timeseries{})
	for _, path := range schema.Columns() {
		leaf, _ := schema.Lookup(path...)
		v := strings.Join(path, ".")
		if v == "id" {
			idIndex = leaf.ColumnIndex
		}
		break
	}

	if idIndex == -1 {
		t.Errorf("did not find id column in parquet file")
	}

	pqfile, err := os.Open(rep.filepath)
	if err != nil {
		t.Errorf("failed to open results file")
	}
	stat, _ := pqfile.Stat()
	p, err := parquet.OpenFile(pqfile, stat.Size())
	if err != nil {
		t.Errorf("parquet failed to open results file")
	}

	if len(p.RowGroups()) != 1 {
		t.Errorf("Expected to find one row group but got %d", len(p.RowGroups()))
	}

	rg := p.RowGroups()[0]
	idchunk := rg.ColumnChunks()[idIndex]
	ididx, _ := idchunk.ColumnIndex()
	found := parquet.Find(ididx, parquet.ValueOf(0), parquet.CompareNullsLast(idchunk.Type().Compare))
	if found == ididx.NumPages() {
		t.Errorf("did not find ts id 0 in parquet file")
	}

	reader := parquet.NewGenericReader[Timeseries](pqfile)
	defer reader.Close()
	offsetidx, _ := idchunk.OffsetIndex()
	reader.SeekToRow(offsetidx.FirstRowIndex(found))
	result := make([]Timeseries, 10)
	for done := false; !done; {
		// Read the next rows into the given rows slice
		// returns the number of rows read and an io.EOF when no more rows
		numRead, err := reader.Read(result)
		if err != nil {
			if errors.Is(err, io.EOF) {
				done = true
			} else {
				t.Errorf("error while reading file: %v\n", err)
			}
		}
		if numRead != 6 {
			t.Errorf("expected to find six rows in total but got %d", numRead)
		}
		foundMetricRow := false
		foundCorrelationRow := false
		for i, results := range result {
			if i >= numRead {
				break
			}
			if results.ID != 0 {
				continue
			}
			if results.Metric == "myMetric" {
				if results.Labels == nil {
					t.Errorf("expected labels on row %+v", results)
				}
				foundMetricRow = true
			}
			if results.Pearson != 0 {
				if results.Correlated != 2 || results.Pearson != 0.99 {
					t.Errorf("expected correlation 0.99 with ts 2 but got %+v", results)
				}
				foundCorrelationRow = true
			}
		}
		if !foundMetricRow {
			t.Errorf("metrics row missing")
		}
		if !foundCorrelationRow {
			t.Errorf("correlation row missing")
		}
	}
}
