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
   ks := flag.Int("ks", 150, "How many columns to reduce the input to in the first paa step")
   // paper says 30 is good
   ke := flag.Int("ke", 300, "How many columns to reduce the input to in the second paa step")
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
   for {
      line, err := reader.ReadString('\n')
      if len(line) == 0 { break }
      line = strings.TrimSuffix(line, "\n")
      lineCount++

      // parse floats out of line with strconv
      parts := strings.Split(line, " ")
      if columnCount == 0 {
         columnCount = len(parts)
      } else {
         if columnCount != len(parts) {
            panic(fmt.Errorf("inconsistent number of values in line %d: expected %d but got %d",
               lineCount, columnCount, len(parts)))
         }
      }
      vec := make([]float64, len(parts), len(parts))
      for i, p := range parts {
         vec[i], err = strconv.ParseFloat(p, 64)
         if err != nil {
            panic(fmt.Errorf("on line %d of %s, failed to parse %s into a float: %v\n",
               lineCount, *filename, p, err))
         }
      }
      data = append(data, vec...)

      if err != nil { break }  // err is usually io.EOF
   }
   // TODO: it might be better to create just the first /windowsize/ matrix here and
   // then the sliding windows
   initialMatrix := mat.NewDense(lineCount, columnCount, data)

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
      nextWindow := initialMatrix.Slice(0, lineCount,
                                        *windowSize + shiftCount * *stride,
                                        *windowSize + (shiftCount + 1) * *stride).(*mat.Dense)
      if (*full) {
         err = window.FullPearson(processor, lib.NewTimeseriesWindow(nextWindow))
      } else {
         err = window.ProcessBuffer(processor, lib.NewTimeseriesWindow(nextWindow))
      }
      if err != nil {
         fmt.Printf("caught error: %v\n", err)
      }
      shiftCount++
   }
}
