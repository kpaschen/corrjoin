package reporter

import (
	"encoding/json"
	"github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/common/model"
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
	rep := NewParquetReporter(tempdir, 100000)

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

	tsid1 := lib.TsId{
		MetricName:        string(sampleMetricString),
		MetricFingerprint: uint64(1),
	}
	tsid2 := lib.TsId{
		MetricName:        string(sampleMetricString2),
		MetricFingerprint: uint64(2),
	}
	// rep.Initialize(1, time.Now(), time.Now(), []lib.TsId{tsid1, tsid2})
	rep.InitializeStride(1, time.Now(), time.Now())
	rep.RecordTimeseriesIds(1, []lib.TsId{tsid1, tsid2})

	rep.Flush(1)
}

func TestAddCorrelatedPairs(t *testing.T) {
	tempdir, err := os.MkdirTemp("", "corrjoinTest")
	if err != nil {
		t.Errorf("failed to create temp dir")
	}
	defer os.RemoveAll(tempdir)
	rep := NewParquetReporter(tempdir, 1000)

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

	tsid1 := lib.TsId{
		MetricName:        string(sampleMetricString),
		MetricFingerprint: uint64(1),
	}
	tsid2 := lib.TsId{
		MetricName:        string(sampleMetricString2),
		MetricFingerprint: uint64(2),
	}
	rep.InitializeStride(1, time.Now(), time.Now())
	rep.RecordTimeseriesIds(1, []lib.TsId{tsid1, tsid2})

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

	rep.Flush(1)

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
		t.Errorf("did not find id column in parquet schema")
	}
}
