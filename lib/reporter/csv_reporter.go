package reporter

import (
	"encoding/csv"
	"fmt"
	"github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"log"
	"os"
	"path/filepath"
	"time"
)

type CsvReporter struct {
	filenameBase     string
	tsids            []lib.TsId
	strideStartTimes map[int]string
	strideEndTimes   map[int]string
}

func NewCsvReporter(filenameBase string) *CsvReporter {
	return &CsvReporter{
		filenameBase:     filenameBase,
		strideStartTimes: make(map[int]string),
		strideEndTimes:   make(map[int]string),
	}
}

func (c *CsvReporter) InitializeStride(strideCounter int, strideStart time.Time, strideEnd time.Time,
	tsids []lib.TsId) {
	c.tsids = tsids
	c.strideStartTimes[strideCounter] = strideStart.UTC().Format("20060102150405")
	c.strideEndTimes[strideCounter] = strideEnd.UTC().Format("20060102150405")
	log.Printf("initializing with strideCounter %d, start time %s (%s), end time %s (%s)\n",
		strideCounter, c.strideStartTimes[strideCounter], strideStart.UTC().String(),
		c.strideEndTimes[strideCounter], strideEnd.UTC().String())
}

func (c *CsvReporter) RecordTimeseriesIds(strideCounter int, tsids []lib.TsId) {
	idsfile := filepath.Join(c.filenameBase, fmt.Sprintf("tsids_%d_%s.csv", strideCounter,
		c.strideStartTimes[strideCounter]))
	file, err := os.OpenFile(idsfile, os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		log.Printf("failed to open ts ids file: %e\n", err)
		return
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	for i, tsid := range tsids {
		record := []string{fmt.Sprintf("%d", i), tsid.MetricName}
		err = writer.Write(record)
		if err != nil {
			log.Printf("failed to write record: %e\n", err)
		}
	}
	writer.Flush()
}

func (c *CsvReporter) csvRecordFromCorrelatedPair(pair datatypes.RowPair, pearson float64) ([]string, error) {
	rowids := pair.RowIds()
	return []string{fmt.Sprintf("%d", rowids[0]), fmt.Sprintf("%d", rowids[1]),
		fmt.Sprintf("%f", pearson)}, nil
}

func (c *CsvReporter) AddConstantRows(strideCounter int, constantRows []bool) (int, error) {
	startTime, ok := c.strideStartTimes[strideCounter]
	if !ok {
		return 0, fmt.Errorf("missing stride start time for %d", strideCounter)
	}
	endTime, ok := c.strideEndTimes[strideCounter]
	if !ok {
		return 0, fmt.Errorf("missing stride end time for %d", strideCounter)
	}
	filename := fmt.Sprintf("constant_rows_%d_%s-%s.csv", strideCounter, startTime, endTime)
	resultsPath := filepath.Join(c.filenameBase, filename)
	file, err := os.OpenFile(resultsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	ctr := 0
	for i, isConstant := range constantRows {
		if !isConstant {
			continue
		}
		err = writer.Write([]string{fmt.Sprintf("%d", i)})
		if err != nil {
			return ctr, err
		}
		ctr++
		if ctr%1000 == 0 {
			writer.Flush()
		}
	}
	writer.Flush()
	err = writer.Error()
	return ctr, err
}

func (c *CsvReporter) AddCorrelatedPairs(result datatypes.CorrjoinResult, _ []lib.TsId) error {
	startTime, ok := c.strideStartTimes[result.StrideCounter]
	if !ok {
		return fmt.Errorf("missing stride start time for %d", result.StrideCounter)
	}
	endTime, ok := c.strideEndTimes[result.StrideCounter]
	if !ok {
		return fmt.Errorf("missing stride end time for %d", result.StrideCounter)
	}
	filename := fmt.Sprintf("correlations_%d_%s-%s.csv", result.StrideCounter, startTime, endTime)
	resultsPath := filepath.Join(c.filenameBase, filename)
	file, err := os.OpenFile(resultsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	ctr := 0
	for pair, pearson := range result.CorrelatedPairs {
		record, err := c.csvRecordFromCorrelatedPair(pair, pearson)
		if err != nil {
			log.Printf("reporter: skipping bad record %v", err)
			continue
		}
		err = writer.Write(record)
		if err != nil {
			return err
		}
		if ctr >= 1000 {
			writer.Flush()
			if err = writer.Error(); err != nil {
				return err
			}
			ctr = 0
		}
		ctr++
	}
	writer.Flush()
	err = writer.Error()
	return err
}

func (c *CsvReporter) Flush(_ int) error {
	// This reporter does no internal buffering, so Flush is a noop.
	return nil
}
