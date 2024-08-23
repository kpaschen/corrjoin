package reporter

import (
	"encoding/csv"
	"fmt"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/settings"
	"log"
	"os"
)

type CsvReporter struct {
	filenameBase string
	tsids []string
}

func NewCsvReporter(filenameBase string) *CsvReporter {
	return &CsvReporter{
		filenameBase: filenameBase,
	}
}

func (c *CsvReporter) Initialize(config settings.CorrjoinSettings, tsids []string) {
	c.tsids = tsids
}

func (c *CsvReporter) csvRecordFromCorrelatedPair(pair comparisons.RowPair, pearson float64) ([]string, error) {
	rowids := pair.RowIds()
	if rowids[0] >= len(c.tsids) || rowids[1] >= len(c.tsids) {
		return []string{fmt.Sprintf("%d", rowids[0]), fmt.Sprintf("%d", rowids[1]),
			fmt.Sprintf("%f", pearson)}, nil
	}
	name1 := c.tsids[rowids[0]]
	name2 := c.tsids[rowids[1]]
	return []string{name1, name2, fmt.Sprintf("%f", pearson)}, nil
}

func (c *CsvReporter) AddCorrelatedPairs(result comparisons.CorrjoinResult) error {
	filename := fmt.Sprintf("%s_%d.csv", c.filenameBase, result.StrideCounter)
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil { return err }
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
		if err != nil { return err }
		if ctr >= 1000 {
			writer.Flush()
			if err = writer.Error(); err != nil { return err }
			ctr = 0
		}
		ctr++
	}
	writer.Flush()
	err = writer.Error()
	return err
}

func (c *CsvReporter) Flush() error {
	// This reporter does no internal buffering, so Flush is a noop.
	return nil
}

