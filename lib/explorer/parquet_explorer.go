package explorer

import (
  "errors"
	"fmt"
  "github.com/parquet-go/parquet-go"
  "github.com/kpaschen/corrjoin/lib/reporter"
  "io"
	"log"
	"os"
	"path/filepath"
)

type ParquetExplorer struct {
	filenameBase     string
  file *parquet.File
  idIndex int
  correlatedIndex int
  pearsonIndex int
}

func NewParquetExplorer(filenameBase string) *ParquetExplorer {
	return &ParquetExplorer{
		filenameBase:     filenameBase,
    file: nil,
    idIndex: -1,
    correlatedIndex: -1,
    pearsonIndex: -1,
	}
}

func (p *ParquetExplorer) Initialize(filename string) error {
  schema := parquet.SchemaOf(reporter.Timeseries{})
  for _, path := range schema.Columns() {
     if len(path) != 1 { continue }
     leaf, _ := schema.Lookup(path...)
     switch path[0] {
        case "id": 
           p.idIndex = leaf.ColumnIndex
        case "correlated":
           p.correlatedIndex = leaf.ColumnIndex
        case "pearson":
           p.pearsonIndex = leaf.ColumnIndex
     }
  }

  if p.idIndex < 0 || p.correlatedIndex < 0 || p.pearsonIndex < 0 {
     return fmt.Errorf("bad schema: missing columns for id, correlated, or pearson")
  }

	filepath := filepath.Join(p.filenameBase, filename)

  pqfile, err := os.Open(filepath)
  if err != nil {
    log.Printf("failed to open ts parquet file: %e\n", err)
    return err
  }
  stat, _ := pqfile.Stat()
  p.file, err = parquet.OpenFile(pqfile, stat.Size()) 
  if err != nil {
    log.Printf("Parquet: failed to open ts parquet file: %e\n", err)
    return err
  }
  return nil
}

// TODO: use a datatype and include constness
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
            if i >= numRead { break }
            if result.ID != timeSeriesId { continue } 
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
