package receiver

import (
	"encoding/json"
	corrjoin "github.com/kpaschen/corrjoin/lib"
	"github.com/kpaschen/corrjoin/lib/comparisons"
	"github.com/kpaschen/corrjoin/lib/datatypes"
	"github.com/kpaschen/corrjoin/lib/reporter"
	"github.com/kpaschen/corrjoin/lib/settings"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage/remote"
	"log"
	"net/http"
	"time"
)

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
	resultsChannel   chan (*datatypes.CorrjoinResult)
	bufferChannel    chan (*corrjoin.ObservationResult)
	comparer         comparisons.Engine
	strideStartTimes map[int]time.Time
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
			observation := &corrjoin.Observation{
				MetricFingerprint: (uint64)(metric.Fingerprint()),
				MetricName:        metricName,
				Value:             s.Value,
				Timestamp:         time.Unix(s.Timestamp/1000, 0).UTC(),
			}
			t.accumulator.AddObservation(observation)
			sampleCounter++
		}
		receivedSamples.Add(float64(sampleCounter))
	}
	return nil
}

func (t *tsProcessor) ReceivePrometheusData(w http.ResponseWriter, r *http.Request) {
	req, err := remote.DecodeWriteRequest(r.Body)
	if err != nil {
		log.Printf("failed to decode write request: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TODO: either this or the accumulator need to evaluate the stale marker.
	// That is a special NaN value 0x7ff0000000000002

	// For now, convert samples directly and add them as observations
	err = t.observeTs(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func NewTsProcessor(corrjoinConfig settings.CorrjoinSettings, strideLength int,
	reporter *reporter.ParquetReporter) *tsProcessor {

	// The buffer channel is how the accumulator lets us know there are enough
	// timeseries data in the buffer for a stride.
	bufferChannel := make(chan *corrjoin.ObservationResult, 1)

	// The results channel is where we hear about correlated timeseries.
	resultsChannel := make(chan *datatypes.CorrjoinResult, 1)
	//defer close(resultsChannel)

	comparer := &comparisons.InProcessComparer{}
	comparer.Initialize(corrjoinConfig, resultsChannel)

	processor := &tsProcessor{
		accumulator: corrjoin.NewTimeseriesAccumulator(strideLength, time.Now().UTC(),
			corrjoinConfig.SampleInterval, bufferChannel),
		settings:         &corrjoinConfig,
		window:           corrjoin.NewTimeseriesWindow(corrjoinConfig, comparer),
		resultsChannel:   resultsChannel,
		bufferChannel:    bufferChannel,
		comparer:         comparer,
		strideStartTimes: make(map[int]time.Time),
	}

	go func() {
		log.Println("waiting for buffers")
		for {
			select {
			case observationResult := <-bufferChannel:
				if observationResult.Err != nil {
					log.Printf("failed to process window: %v", observationResult.Err)
				} else {
					log.Printf("got an observation request\n")
					reporter.Flush()
					requestedCorrelationBatches.Inc()
					requestStart := time.Now()
					// TODO: this is a hack because ShiftBuffer only returns after the window
					// is done processing. Fix this when I add the lock to the window.
					// TODO: should the window call the reporter?
					processor.strideStartTimes[processor.window.StrideCounter+1] = requestStart
					reporter.Initialize(processor.window.StrideCounter+1,
						processor.accumulator.CurrentStrideStartTs,
						processor.accumulator.CurrentStrideMaxTs,
						processor.accumulator.Tsids)
					// TODO: this has to return quickly.

					err := processor.window.ShiftBuffer(observationResult.Buffers)
					// TODO: check for error type.
					// If window is busy, hold the observationResult
					if err != nil {
						log.Printf("failed to process window: %v", err)
					}
					reporter.AddConstantRows(processor.window.ConstantRows)
				}
			case <-time.After(10 * time.Minute):
				log.Printf("got no timeseries data for 10 minutes")
			}
		}
	}()

	go func() {
		log.Println("waiting for correlation results")
		for {
			select {
			case correlationResult := <-resultsChannel:
				if len(correlationResult.CorrelatedPairs) == 0 {
					log.Printf("empty correlation result, done with stride %d\n",
						correlationResult.StrideCounter)
					requestEnd := time.Now()
					requestStart, ok := processor.strideStartTimes[correlationResult.StrideCounter]
					if !ok {
						log.Printf("missing start time for stride %d?\n", correlationResult.StrideCounter)
					} else {
						elapsed := requestEnd.Sub(requestStart)
						correlationDurationHist.Observe(float64(elapsed.Milliseconds()))
						correlationDuration.Set(float64(elapsed.Milliseconds()))
						log.Printf("correlation batch processed in %d milliseconds\n", elapsed.Milliseconds())
					}
					reporter.Flush()
				} else {
					err := reporter.AddCorrelatedPairs(*correlationResult)
					if err != nil {
						log.Printf("failed to log results: %v\n", err)
					}
				}
			}
		}
	}()

	return processor
}
