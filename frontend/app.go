package main

import (
	"context"
	"flag"
	"github.com/gorilla/mux"
	"github.com/kpaschen/corrjoin/explorer"
	"github.com/kpaschen/corrjoin/lib/settings"
	"github.com/kpaschen/corrjoin/receiver"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
)

type config struct {
	explorerAddress   string
	prometheusAddress string
	metricsAddress    string
}

func main() {
	var metricsAddr string
	var prometheusAddr string
	var explorerAddr string
	var windowSize int
	var stride int
	var correlationThreshold int
	var ks int
	var ke int
	var svdDimensions int
	var algorithm string
	var skipConstantTs bool
	var compareEngine string
	var parquetMaxRowsPerRowGroup int
	var sampleInterval int
	var resultsDirectory string
	var justExplore bool
	var noExplore bool
	var prometheusURL string
	var strideMaxAgeSeconds int
	var maxRows int
	var labeldrop string

	flag.StringVar(&metricsAddr, "metrics-address", ":9203", "The address the metrics endpoint binds to.")
	flag.StringVar(&prometheusAddr, "listen-address", ":9201", "The address that the storage endpoint binds to.")
	flag.StringVar(&explorerAddr, "explorer-address", ":9205", "The address that the explorer endpoint binds to.")

	flag.IntVar(&windowSize, "windowSize", 1020, "number of data points to use in determining correlatedness")
	flag.IntVar(&stride, "stride", 102, "the number of data points to read before computing correlation again")
	flag.IntVar(&sampleInterval, "sampleInterval", 20, "the time between samples, in seconds. This should be at least the global Prometheus scrape interval")
	flag.IntVar(&correlationThreshold, "correlationThreshold", 90, "correlation threshold in percent")
	flag.IntVar(&ks, "ks", 15, "how many columns to reduce the input to in the first PAA step")
	flag.IntVar(&ke, "ke", 30, "how many columns to reduce the input to in the second PAA step (during bucketing)")
	flag.IntVar(&svdDimensions, "svdDimensions", 3, "How many columns to choose after SVD")
	flag.StringVar(&algorithm, "algorithm", "paa_svd", "Algorithm to use. Possible values: full_pearson, paa_only, paa_svd")
	flag.BoolVar(&skipConstantTs, "skipConstantTs", true, "Whether to ignore timeseries whose value is constant in the current window")
	flag.StringVar(&compareEngine, "comparer", "inprocess", "The comparison engine.")
	flag.IntVar(&parquetMaxRowsPerRowGroup, "parquetMaxRowsPerRowGroup", 100000, "Number of rows per row group in Parquet. Small numbers reduce memory usage but cost more disk space; large numbers cost more memory but improve compression.")
	flag.StringVar(&resultsDirectory, "resultsDirectory", "/tmp/corrjoinResults", "The directory with the result files.")
	flag.BoolVar(&justExplore, "justExplore", false, "If true, launch only the explorer endpoint")
	flag.BoolVar(&noExplore, "noExplore", false, "If true, do not launch the explorer endpoint")
	flag.StringVar(&prometheusURL, "prometheusURL", "", "A URL for the prometheus service")
	flag.IntVar(&strideMaxAgeSeconds, "strideMaxAgeSeconds", 7200, "The maximum time to keep stride data around for.")
	flag.IntVar(&maxRows, "maxRows", 0, "The maximum number of timeseries to process. 0 means no limit.")
	flag.StringVar(&labeldrop, "labeldrop", "", "The labels to drop from timeseries, separated by |")

	flag.Parse()

	cfg := &config{
		prometheusAddress: prometheusAddr,
		metricsAddress:    metricsAddr,
		explorerAddress:   explorerAddr,
	}

	corrjoinConfig := settings.CorrjoinSettings{
		SvdDimensions:        ks,
		SvdOutputDimensions:  svdDimensions,
		EuclidDimensions:     ke,
		CorrelationThreshold: float64(float64(correlationThreshold) / 100.0),
		WindowSize:           windowSize,
		StrideLength:         stride,
		SampleInterval:       sampleInterval,
		Algorithm:            algorithm,
		MaxRowsPerRowGroup:   int64(parquetMaxRowsPerRowGroup),
		ResultsDirectory:     resultsDirectory,
		MaxRows:              maxRows,
	}
	corrjoinConfig = corrjoinConfig.ComputeSettingsFields()

	var expl *explorer.CorrelationExplorer
	var explorerRouter *mux.Router

	if !noExplore {
		expl = &explorer.CorrelationExplorer{
			FilenameBase: resultsDirectory,
		}
		err := expl.Initialize(prometheusURL, strideMaxAgeSeconds, strings.Split(labeldrop, "|"))
		if err != nil {
			log.Printf("failed to initialize explorer: %v\n", err)
		}

		explorerRouter = mux.NewRouter().StrictSlash(true)
		explorerRouter.HandleFunc("/getStrides", expl.GetStrides).Methods("GET")
		explorerRouter.HandleFunc("/getSubgraphs", expl.GetSubgraphs).Methods("GET")
		explorerRouter.HandleFunc("/getSubgraphNodes", expl.GetSubgraphNodes).Methods("GET")
		explorerRouter.HandleFunc("/getSubgraphEdges", expl.GetSubgraphEdges).Methods("GET")
		explorerRouter.HandleFunc("/getCorrelatedSeries", expl.GetCorrelatedSeries).Methods("GET")
		explorerRouter.HandleFunc("/getTimeseries", expl.GetTimeseries).Methods("GET")
		explorerRouter.HandleFunc("/getTimeline", expl.GetTimeline).Methods("GET")
		explorerRouter.HandleFunc("/getMetricInfo", expl.GetMetricInfo).Methods("GET")
		explorerRouter.HandleFunc("/dumpMetricCache", expl.DumpMetricCache).Methods("GET")
		explorerRouter.HandleFunc("/getMetricHistory", expl.GetMetricHistory).Methods("GET")
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(cfg.metricsAddress, nil)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	var prometheusServer *http.Server

	if !justExplore {
		processor := receiver.NewTsProcessor(corrjoinConfig)
		prometheusRouter := mux.NewRouter().StrictSlash(true)
		prometheusRouter.HandleFunc("/api/v1/write", processor.ReceivePrometheusData)
		prometheusServer = &http.Server{
			Addr:    cfg.prometheusAddress,
			Handler: prometheusRouter,
		}
		go func() {
			log.Printf("correlation service listening on port %s\n", cfg.prometheusAddress)
			if err := prometheusServer.ListenAndServe(); err != nil {
				if err != http.ErrServerClosed {
					processor.Shutdown()
					log.Fatal(err)
				}
			}
		}()
	}

	var explorerServer *http.Server

	if !noExplore {
		explorerServer := &http.Server{
			Addr:    cfg.explorerAddress,
			Handler: explorerRouter,
		}

		go func() {
			log.Printf("explorer service listening on port %s\n", cfg.explorerAddress)
			if err := explorerServer.ListenAndServe(); err != nil {
				if err != http.ErrServerClosed {
					log.Fatal(err)
				}
			}
		}()
	}

	<-stop
	log.Println("correlation service shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10)
	defer cancel()

	// This is where the correlation service gets a chance to dump results to disk.
	if !justExplore && prometheusServer != nil {
		if err := prometheusServer.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}
	if !noExplore {
		// This is best effort, there is nothing really to do.
		if err := explorerServer.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}
}
