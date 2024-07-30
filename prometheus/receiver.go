package main

import (
	"context"
	"encoding/json"
	"flag"
	//   "fmt"
	//   "github.com/gogo/protobuf/proto"
	//   "github.com/golang/snappy"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	// "github.com/prometheus/common/promlog"
	corrjoin "github.com/kpaschen/corrjoin/lib"
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
	correlationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:                            "correlation_duration_seconds",
			Help:                            "Duration of correlation computation calls.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
		},
	)
)

func init() {
	prometheus.MustRegister(receivedSamples)
	prometheus.MustRegister(requestedCorrelationBatches)
	prometheus.MustRegister(correlationDuration)
}

type tsProcessor struct {
	accumulator *corrjoin.TimeseriesAccumulator
	processor   *corrjoin.TimeseriesProcessor

	buffers [][]float64
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
		receivedSamples.Add(float64(len(ts.Samples)))
		for _, s := range ts.Samples {
			t.accumulator.AddObservation(string(mjson), s.Value, time.Unix(s.Timestamp/1000,
				0).UTC())
		}
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

	// Later: pass samples to the processor here.
	// samples := t.protoToSamples(req)
	// For now, convert samples directly and add them as observations
	t.observeTs(req)
}

// TODO: move this to the tswindow code so it can handle the rotation
func (t *tsProcessor) handleTimeseriesBuffer(buf [][]float64) {
	// If t.window is nil, check if we can create the first window
	// Otherwise, rotate the new data in.

	originalLength := len(buffers)
	originalWidth := 0
	if originalLength > 0 {
		originalWidth = len(buffers[0])
	}

	// Append timeseries data
	for i, b := range buf {
		if i < originalLength {
			buffers[i] = append(buffers[i], b...)
		} else {
			prefix := make([]float64, originalWidth, originalWidth)
			buffers = append(buffers, append(prefix, b...))
		}
	}
}

func main() {
	var metricsAddr string
	var listenAddr string

	flag.StringVar(&metricsAddr, "metrics-address", ":9203", "The address the metrics endpoint binds to.")
	flag.StringVar(&listenAddr, "listen-address", ":9201", "The address that the storage endpoint binds to.")

	flag.Parse()

	cfg := &config{
		listenAddress:  listenAddr,
		metricsAddress: metricsAddr,
	}

	bufferChannel := make(chan [][]float64, 2)
	defer close(bufferChannel)

	processor := &tsProcessor{
		// windowsize 10, stride 5
		accumulator: corrjoin.NewTimeseriesAccumulator(5, time.Now().UTC(), bufferChannel),
		processor: &corrjoin.TimeseriesProcessor{
			SvdDimensions:        3,
			SvdOutputDimensions:  3, // Use 15
			EuclidDimensions:     4, // Use 30
			CorrelationThreshold: 90,
			WindowSize:           10, // 1020
			// stride should be 102
		},
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
		log.Printf("correlation service waiting for buffers\n")
		select {
		case buffers := <-bufferChannel:
			log.Printf("I should make a timeseries window from %+v\n", buffers)
		case <-time.After(10 * time.Minute):
			log.Fatalf("got no timeseries data for 10 minutes")
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
