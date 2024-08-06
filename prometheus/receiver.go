package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/gorilla/mux"
	corrjoin "github.com/kpaschen/corrjoin/lib"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage/remote"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

type config struct {
	listenAddress  string
	metricsAddress string
}

var (
	receivedSamples = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "received_samples_total",
			Help: "Total number of received samples.",
		},
	)
	requestedCorrelationBatches = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "requested_correlation_batches_total",
			Help: "Total number of times a correlation batch computation has been requested.",
		},
	)
	correlationDurationHist = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:                            "correlation_duration_milliseconds_histogram",
			Help:                            "Duration of correlation computation calls.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
		},
	)

	correlationDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "correlation_duration_milliseconds",
			Help:                            "Duration of correlation computation calls.",
		},
	)

	correlatedPairs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "correlated_timeseries",
			Help: "pairs of correlated timeseries",
		},
		[]string{
			"ts1",
			"ts2",
		},
	)
)

func init() {
	prometheus.MustRegister(receivedSamples)
	prometheus.MustRegister(requestedCorrelationBatches)
	prometheus.MustRegister(correlationDurationHist)
	prometheus.MustRegister(correlationDuration)
	prometheus.MustRegister(correlatedPairs)
}

type tsProcessor struct {
	accumulator *corrjoin.TimeseriesAccumulator
	settings    *corrjoin.CorrjoinSettings
	window      *corrjoin.TimeseriesWindow
	observationQueue chan(*corrjoin.Observation)
}

func (t *tsProcessor) observeTs(req *prompb.WriteRequest) error {
	for _, ts := range req.Timeseries {
		metric := make(model.Metric, len(ts.Labels))
		for _, l := range ts.Labels {
			metric[model.LabelName(l.Name)] = model.LabelValue(l.Value)
		}
		mjson, err := json.Marshal(metric)
		if err != nil {
			return err
		}
		metricName := string(mjson)
		sampleCounter := 0
		for _, s := range ts.Samples {
			// TODO: compare value against 0x7ff0000000000002
			t.observationQueue <- &corrjoin.Observation{
				MetricName: metricName,
				Value: s.Value,
				Timestamp: time.Unix(s.Timestamp/1000, 0).UTC(),
			}
			sampleCounter++
		}
		receivedSamples.Add(float64(sampleCounter))
	}
	return nil
}

func (t *tsProcessor) protoToSamples(req *prompb.WriteRequest) model.Samples {
	var samples model.Samples
	for _, ts := range req.Timeseries {
		metric := make(model.Metric, len(ts.Labels))
		for _, l := range ts.Labels {
			metric[model.LabelName(l.Name)] = model.LabelValue(l.Value)
		}

		for _, s := range ts.Samples {
			samples = append(samples, &model.Sample{
				Metric:    metric,
				Value:     model.SampleValue(s.Value),
				Timestamp: model.Time(s.Timestamp),
			})
		}
	}
	return samples
}

func (t *tsProcessor) receivePrometheusData(w http.ResponseWriter, r *http.Request) {
	req, err := remote.DecodeWriteRequest(r.Body)
	if err != nil {
		log.Printf("failed to decode write request: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TODO: either this or the accumulator need to evaluate the stale marker.
	// That is a special NaN value 0x7ff0000000000002

	// Later: pass samples to the processor here.
	// samples := t.protoToSamples(req)
	// For now, convert samples directly and add them as observations
	err = t.observeTs(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func reportCorrelation(tsids []string, result *corrjoin.CorrjoinResult) {
	for pair, pearson := range result.CorrelatedPairs {
		rowIds := pair.RowIds()
		if rowIds[0] > len(tsids) || rowIds[1] > len(tsids) {
			log.Printf("nameless rows %d, %d reported as correlated", rowIds[0], rowIds[1])
			continue
		}
		if result.ConstantRows[rowIds[0]] != result.ConstantRows[rowIds[1]] {
			log.Printf("a constant row correlated with a non-constant one?")
		        log.Printf("correlated: %s and %s with strength %f", tsids[rowIds[0]], tsids[rowIds[1]], pearson)
		} else {
			if result.ConstantRows[rowIds[0]] {
				log.Print("skipping constant timeseries")
			}
		}
		// TODO: this only updates values when two ts are correlated, not when they aren't.
		// Should report a 0 for previously-correlated-now-uncorrelated pairs. In order to do that,
		// have to remember which ones were previously correlated.
		correlatedPairs.With(prometheus.Labels{
			"ts1": tsids[rowIds[0]],
			"ts2": tsids[rowIds[1]],
		}).Set(pearson)
	}
}

func main() {
	var metricsAddr string
	var listenAddr string
	var windowSize int
	var stride int
	var correlationThreshold int
	var ks int
	var ke int
	var svdDimensions int
	var algorithm string
	var skipConstantTs bool

	flag.StringVar(&metricsAddr, "metrics-address", ":9203", "The address the metrics endpoint binds to.")
	flag.StringVar(&listenAddr, "listen-address", ":9201", "The address that the storage endpoint binds to.")
	flag.IntVar(&windowSize, "windowSize", 1020, "number of data points to use in determining correlatedness")
	flag.IntVar(&stride, "stride", 102, "the number of data points to read before computing correlation again")
	flag.IntVar(&correlationThreshold, "correlationThreshold", 90, "correlation threshold in percent")
	flag.IntVar(&ks, "ks", 15, "how many columns to reduce the input to in the first PAA step")
	flag.IntVar(&ke, "ke", 30, "how many columns to reduce the input to in the second PAA step (during bucketing)")
	flag.IntVar(&svdDimensions, "svdDimensions", 3, "How many columns to choose after SVD")
	flag.StringVar(&algorithm, "algorithm", "paa_svd", "Algorithm to use. Possible values: full_pearson, paa_only, paa_svd")
	flag.BoolVar(&skipConstantTs, "skipConstantTs", true, "Whether to ignore timeseries whose value is constant in the current window")

	flag.Parse()

	cfg := &config{
		listenAddress:  listenAddr,
		metricsAddress: metricsAddr,
	}

	bufferChannel := make(chan *corrjoin.ObservationResult, 1)
	defer close(bufferChannel)

	observationQueue := make(chan *corrjoin.Observation, 1)
	defer close(observationQueue)

	processor := &tsProcessor{
		accumulator: corrjoin.NewTimeseriesAccumulator(stride, time.Now().UTC(), bufferChannel),
		settings: &corrjoin.CorrjoinSettings{
			SvdDimensions:        ks,
			SvdOutputDimensions:  svdDimensions,
			EuclidDimensions:     ke,
			CorrelationThreshold: float64(float64(correlationThreshold) / 100.0),
			WindowSize:           windowSize,
			Algorithm:            algorithm,
		},
		window: corrjoin.NewTimeseriesWindow(windowSize),
		observationQueue: observationQueue,
	}

	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/api/v1/write", processor.receivePrometheusData)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(cfg.metricsAddress, nil)

	server := &http.Server{
		Addr:    cfg.listenAddress,
		Handler: router,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	go func() {
		log.Printf("correlation service listening on port %s\n", cfg.listenAddress)
		if err := server.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}
	}()

	go func() {
		log.Println("correlation service watching observation queue")
		for {
		select {
			case observation := <-observationQueue:
				processor.accumulator.AddObservation(observation)
		}
		}
	}()

	go func() {
		log.Println("correlation service waiting for buffers")
		for {
			select {
			case observationResult := <-bufferChannel:
				if observationResult.Err != nil {
					log.Printf("failed to process window: %v", observationResult.Err)
					break
				}
				requestedCorrelationBatches.Inc()
				requestStart := time.Now()
				res, err := processor.window.ShiftBuffer(observationResult.Buffers, *processor.settings)
				// TODO: check for error type.
				if err != nil {
					log.Printf("failed to process window: %v", err)
				}
				requestEnd := time.Now()
				elapsed := requestEnd.Sub(requestStart)
				correlationDurationHist.Observe(float64(elapsed.Milliseconds()))
				correlationDuration.Set(float64(elapsed.Milliseconds()))
				log.Printf("correlation batch successfully processed in %d milliseconds\n", elapsed.Milliseconds())
				if res != nil {
					reportCorrelation(processor.accumulator.Tsids, res)
				}
			case <-time.After(10 * time.Minute):
				log.Fatalf("got no timeseries data for 10 minutes")
				break
			}
		}
	}()

	<-stop
	log.Println("correlation service shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10)
	defer cancel()

	// This is where the correlation service gets a chance to dump results to disk.

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
}
