package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/gorilla/mux"
	corrjoin "github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/reporter"
	"github.com/kpaschen/corrjoin/lib/settings"
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
			Help: "Duration of correlation computation calls.",
		},
	)
)

func init() {
	prometheus.MustRegister(receivedSamples)
	prometheus.MustRegister(requestedCorrelationBatches)
	prometheus.MustRegister(correlationDurationHist)
	prometheus.MustRegister(correlationDuration)
}

type tsProcessor struct {
	accumulator      *corrjoin.TimeseriesAccumulator
	settings         *settings.CorrjoinSettings
	window           *corrjoin.TimeseriesWindow
	observationQueue chan (*corrjoin.Observation)
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
			t.observationQueue <- &corrjoin.Observation{
				MetricName: metricName,
				Value:      s.Value,
				Timestamp:  time.Unix(s.Timestamp/1000, 0).UTC(),
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
	var compareEngine string

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
	flag.StringVar(&compareEngine, "comparer", "inprocess", "The comparison engine. Possible values are 'inprocess' or 'kafka'")

	flag.Parse()

	cfg := &config{
		listenAddress:  listenAddr,
		metricsAddress: metricsAddr,
	}

	// The observation queue is how we hand timeseries data to the accumulator.
	observationQueue := make(chan *corrjoin.Observation, 1)
	defer close(observationQueue)

	// The buffer channel is how the accumulator lets us know there are enough
	// timeseries data in the buffer for a stride.
	bufferChannel := make(chan *corrjoin.ObservationResult, 1)
	defer close(bufferChannel)

	// The results channel is where we hear about correlated timeseries.
	resultsChannel := make(chan *comparisons.CorrjoinResult, 1)
	defer close(resultsChannel)

	corrjoinConfig := settings.CorrjoinSettings{
		SvdDimensions:        ks,
		SvdOutputDimensions:  svdDimensions,
		EuclidDimensions:     ke,
		CorrelationThreshold: float64(float64(correlationThreshold) / 100.0),
		WindowSize:           windowSize,
		Algorithm:            algorithm,
	}
	corrjoinConfig = corrjoinConfig.ComputeSettingsFields()

	var comparer comparisons.Engine
	if compareEngine == "inprocess" {
		comparer = &comparisons.InProcessComparer{}
	} else {
		panic("currently only the in process comparer is supported")
	}

	comparer.Initialize(corrjoinConfig, resultsChannel)

	processor := &tsProcessor{
		accumulator:      corrjoin.NewTimeseriesAccumulator(stride, time.Now().UTC(), corrjoinConfig.SampleInterval, bufferChannel),
		settings:         &corrjoinConfig,
		window:           corrjoin.NewTimeseriesWindow(corrjoinConfig, comparer),
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

	strideStartTimes := make(map[int]time.Time)
	correlationReporter := reporter.NewCsvReporter("/tmp/correlations")

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
				} else {
					requestedCorrelationBatches.Inc()
					requestStart := time.Now()
					strideStartTimes[processor.window.StrideCounter] = requestStart
					// TODO: this has to return quickly.
					err := processor.window.ShiftBuffer(observationResult.Buffers, resultsChannel)
					// TODO: check for error type.
					// If window is busy, hold the observationResult
					if err != nil {
						log.Printf("failed to process window: %v", err)
					}
				}
			case <-time.After(10 * time.Minute):
				log.Printf("got no timeseries data for 10 minutes")
			}
		}
	}()

	go func() {
		log.Println("Correlation service waiting for correlation results")
		for {
			select {
			case correlationResult := <-resultsChannel:
				if len(correlationResult.CorrelatedPairs) == 0 {
					log.Printf("empty correlation result, done with stride %d\n",
						correlationResult.StrideCounter)
					requestEnd := time.Now()
					requestStart, ok := strideStartTimes[correlationResult.StrideCounter]
					if !ok {
						log.Printf("missing start time for stride %d?\n", correlationResult.StrideCounter)
					} else {
						elapsed := requestEnd.Sub(requestStart)
						correlationDurationHist.Observe(float64(elapsed.Milliseconds()))
						correlationDuration.Set(float64(elapsed.Milliseconds()))
						log.Printf("correlation batch processed in %d milliseconds\n", elapsed.Milliseconds())
					}
					correlationReporter.Flush()
				} else {
					correlationReporter.AddCorrelatedPairs(*correlationResult)
				}
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
