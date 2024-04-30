package main

import (
   "bufio"
   "flag"
   "fmt"
   "gonum.org/v1/gonum/mat"
   "os"
   "runtime/pprof"
   "strings"
   "strconv"
   "corrjoin/lib"
)

func main() {
   filename := flag.String("filename", "", "Name of the file to read")
   windowSize := flag.Int("windowSize", 1020, "column count of a time series window")
   stride := flag.Int("stride", 100, "how much to slide the time series window by")
   correlationThreshold := flag.Int("correlationThreshold", 90, "correlation threshold in percent")
   // paper says 15 is good
   ks := flag.Int("ks", 15, "How many columns to reduce the input to in the first paa step")
   // paper says 30 is good
   ke := flag.Int("ke", 30, "How many columns to reduce the input to in the second paa step")
   svdDimensions := flag.Int("svdOutput", 3, "How many columns to choose after svd") // aka kb
   full := flag.Bool("full", false, "Whether to run pearson on all pairs")
   cpuprofile := flag.String("cpuprofile", "", "write cpu profile here")
   flag.Parse()

   if *cpuprofile != "" {
      f, err := os.Create(*cpuprofile)
      if err != nil { panic(err) }
      pprof.StartCPUProfile(f)
      defer pprof.StopCPUProfile()
   }


   file, err := os.Open(*filename)
   if err != nil {
      panic(err)
   }
   defer file.Close()
   lineCount := 0
   columnCount := 0
   data := make([]float64, 0)
   reader := bufio.NewReader(file)

   // The data in the sample files is in column-major order, so the nth line of a file
   // is the data at time n, and each line contains an entry for each time series.
   // All the processing uses the transpose of that matrix, so each row corresponds to
   // a timeseries and each column to a point in time.
   for {
      line, err := reader.ReadString('\n')
      if len(line) == 0 { break }
      line = strings.TrimSuffix(line, "\n")
      // This is the current line number, and this is how we count how many columns the resulting matrix will have.
      columnCount++

      // parse floats out of line with strconv
      parts := strings.Split(line, " ")
      if lineCount == 0 {

         // This is the number of columns in the input file, and it'll end up being the number
         // of rows in the result matrix.
         lineCount = len(parts)
      } else {
         if lineCount != len(parts) {
            panic(fmt.Errorf("inconsistent number of values in line %d: expected %d but got %d",
               columnCount, lineCount, len(parts)))
         }
      }
      vec := make([]float64, len(parts), len(parts))
      for i, p := range parts {
         vec[i], err = strconv.ParseFloat(p, 64)
         if err != nil {
            panic(fmt.Errorf("on line %d of %s, failed to parse %s into a float: %v\n",
               columnCount, *filename, p, err))
         }
      }
      data = append(data, vec...)

      if err != nil { break }  // err is usually io.EOF
   }
   // TODO: it might be better to create just the first /windowsize/ matrix here and
   // then the sliding windows
   readMatrix := mat.NewDense(columnCount, lineCount, data)
   initialMatrix := mat.NewDense(lineCount, columnCount, nil)
   for i := 0; i < lineCount; i++ {
      for j := 0; j < columnCount; j++ {
         initialMatrix.Set(i, j, readMatrix.At(j, i))   
      }
   }
   readMatrix = nil

   processor := lib.TimeseriesProcessor{
      SvdOutputDimensions: *svdDimensions,
      SvdDimensions: *ks,
      EuclidDimensions: *ke,
      CorrelationThreshold: float64(*correlationThreshold) / 100.0,
      WindowSize: *windowSize,
   }

   initial := initialMatrix.Slice(0, lineCount, 0, *windowSize).(*mat.Dense)
   window := lib.NewTimeseriesWindow(initial)
   if *full {
      err = window.FullPearson(processor, nil)
   } else {
      err = window.ProcessBuffer(processor, nil)
      // err = window.PAAOnly(processor, nil)
   }
   if err != nil {
      fmt.Printf("caught error: %v\n", err)
   }

   shiftCount := 0
   for { 
      if *windowSize + (shiftCount + 1) * *stride > columnCount {
         fmt.Printf("reached end of data after %d shift operations\n", shiftCount)
         break
      }
      fmt.Printf("processing window number %d\n", shiftCount)
      nextWindow := initialMatrix.Slice(0, lineCount,
                                        *windowSize + shiftCount * *stride,
                                        *windowSize + (shiftCount + 1) * *stride).(*mat.Dense)
      if (*full) {
         err = window.FullPearson(processor, lib.NewTimeseriesWindow(nextWindow))
      } else {
         err = window.ProcessBuffer(processor, lib.NewTimeseriesWindow(nextWindow))
         // err = window.PAAOnly(processor, lib.NewTimeseriesWindow(nextWindow))
      }
      if err != nil {
         fmt.Printf("caught error: %v\n", err)
      }
      shiftCount++
   }
}
