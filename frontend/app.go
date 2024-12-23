package main

import (
	"context"
	"flag"
	"github.com/gorilla/mux"
	"github.com/kpaschen/corrjoin/explorer"
	"github.com/kpaschen/corrjoin/lib/reporter"
	"github.com/kpaschen/corrjoin/lib/settings"
	"github.com/kpaschen/corrjoin/receiver"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	var resultsDirectory string
	var webRoot string

	flag.StringVar(&metricsAddr, "metrics-address", ":9203", "The address the metrics endpoint binds to.")
	flag.StringVar(&prometheusAddr, "listen-address", ":9201", "The address that the storage endpoint binds to.")
	flag.StringVar(&explorerAddr, "explorer-address", ":9205", "The address that the explorer endpoint binds to.")

	flag.IntVar(&windowSize, "windowSize", 1020, "number of data points to use in determining correlatedness")
	flag.IntVar(&stride, "stride", 102, "the number of data points to read before computing correlation again")
	flag.IntVar(&correlationThreshold, "correlationThreshold", 90, "correlation threshold in percent")
	flag.IntVar(&ks, "ks", 15, "how many columns to reduce the input to in the first PAA step")
	flag.IntVar(&ke, "ke", 30, "how many columns to reduce the input to in the second PAA step (during bucketing)")
	flag.IntVar(&svdDimensions, "svdDimensions", 3, "How many columns to choose after SVD")
	flag.StringVar(&algorithm, "algorithm", "paa_svd", "Algorithm to use. Possible values: full_pearson, paa_only, paa_svd")
	flag.BoolVar(&skipConstantTs, "skipConstantTs", true, "Whether to ignore timeseries whose value is constant in the current window")
	flag.StringVar(&compareEngine, "comparer", "inprocess", "The comparison engine.")
	flag.StringVar(&resultsDirectory, "resultsDirectory", "/tmp/corrjoinResults", "The directory to write results to.")
	flag.StringVar(&webRoot, "webRoot", "/webroot", "Web server root")

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
		Algorithm:            algorithm,
	}
	corrjoinConfig = corrjoinConfig.ComputeSettingsFields()

	expl := &explorer.CorrelationExplorer{
		FilenameBase: resultsDirectory,
		WebRoot: webRoot,
	}
	err := expl.Initialize()
	if err != nil {
		log.Printf("failed to initialize explorer: %v\n", err)
	}

	correlationReporter := reporter.NewCsvReporter(resultsDirectory)

	processor := receiver.NewTsProcessor(corrjoinConfig, stride, correlationReporter)

	prometheusRouter := mux.NewRouter().StrictSlash(true)
	prometheusRouter.HandleFunc("/api/v1/write", processor.ReceivePrometheusData)

	explorerRouter := mux.NewRouter().StrictSlash(true)
	explorerRouter.HandleFunc("/explore", expl.ExploreCorrelations).Methods("GET")
	explorerRouter.HandleFunc("/exploreCluster", expl.ExploreCluster).Methods("GET")
	explorerRouter.HandleFunc("/exploreTimeseries", expl.ExploreTimeseries).Methods("GET")

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(cfg.metricsAddress, nil)

	prometheusServer := &http.Server{
		Addr:    cfg.prometheusAddress,
		Handler: prometheusRouter,
	}
	explorerServer := &http.Server{
		Addr:    cfg.explorerAddress,
		Handler: explorerRouter,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	go func() {
		log.Printf("correlation service listening on port %s\n", cfg.prometheusAddress)
		if err := prometheusServer.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}
	}()

	go func() {
		log.Printf("explorer service listening on port %s\n", cfg.explorerAddress)
		if err := explorerServer.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}
	}()

	<-stop
	log.Println("correlation service shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10)
	defer cancel()

	// This is where the correlation service gets a chance to dump results to disk.
	if err := prometheusServer.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
	// This is best effort, there is nothing really to do.
	if err := explorerServer.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
}
