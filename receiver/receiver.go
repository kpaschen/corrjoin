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
			Name: "corrjoin_received_samples_total",
			Help: "Total number of received samples.",
		},
	)
	requestedCorrelationBatches = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "corrjoin_requested_correlation_batches_total",
			Help: "Total number of times a correlation batch computation has been requested.",
		},
	)
	numberOfTimeseries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "corrjoin_number_of_timeseries",
			Help: "number of timeseries",
		},
	)
	constantTimeseries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "corrjoin_constant_timeseries",
			Help: "number of constant timeseries",
		},
	)
	correlationDurationHist = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:                            "correlation_duration_milliseconds_histogram",
			Help:                            "Duration of correlation computation calls.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  10,
			NativeHistogramMinResetDuration: 1 * time.Hour,
		},
	)

	correlationDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "correlation_duration_milliseconds",
			Help: "Duration of correlation computation calls.",
		},
	)

	strideOverruns = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "corrjoin_stride_computation_overruns",
			Help: "Number of times a correlation stride computation has overrun",
		},
	)
)

func init() {
	prometheus.MustRegister(receivedSamples)
	prometheus.MustRegister(requestedCorrelationBatches)
	prometheus.MustRegister(correlationDurationHist)
	prometheus.MustRegister(correlationDuration)
	prometheus.MustRegister(strideOverruns)
	prometheus.MustRegister(numberOfTimeseries)
	prometheus.MustRegister(constantTimeseries)
}

type tsProcessor struct {
	accumulator                 *corrjoin.TimeseriesAccumulator
	settings                    *settings.CorrjoinSettings
	window                      *corrjoin.TimeseriesWindow
	observationQueue            chan (*corrjoin.Observation)
	resultsChannel              chan (*datatypes.CorrjoinResult)
	bufferChannel               chan (*corrjoin.ObservationResult)
	comparer                    comparisons.Engine
	requestProcessingStartTimes map[int]time.Time
	strideStartTimes            map[int]time.Time
	reporter                    *reporter.ParquetReporter
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
				MetricFingerprint: (uint64)(metric.Fingerprint()),
				MetricName:        metricName,
				Value:             s.Value,
				Timestamp:         time.Unix(s.Timestamp/1000, 0).UTC(),
			}
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

func (t *tsProcessor) Shutdown() error {
	if t.reporter != nil {
		return t.reporter.Flush(-1) // Flush all writers
	}
	return nil
}

func NewTsProcessor(corrjoinConfig settings.CorrjoinSettings) *tsProcessor {

	// The observation queue is how we hand timeseries data to the accumulator.
	observationQueue := make(chan *corrjoin.Observation, 1)

	// The buffer channel is how the accumulator lets us know there are enough
	// timeseries data in the buffer for a stride.
	bufferChannel := make(chan *corrjoin.ObservationResult, 1)

	// The results channel is where we hear about correlated timeseries.
	resultsChannel := make(chan *datatypes.CorrjoinResult, 1)

	comparer := &comparisons.InProcessComparer{}
	comparer.Initialize(corrjoinConfig, resultsChannel)

	processor := &tsProcessor{
		accumulator: corrjoin.NewTimeseriesAccumulator(corrjoinConfig.StrideLength,
			time.Now().UTC(), corrjoinConfig.SampleInterval, corrjoinConfig.MaxRows, bufferChannel),
		settings:                    &corrjoinConfig,
		observationQueue:            observationQueue,
		window:                      corrjoin.NewTimeseriesWindow(corrjoinConfig, comparer),
		resultsChannel:              resultsChannel,
		bufferChannel:               bufferChannel,
		comparer:                    comparer,
		strideStartTimes:            make(map[int]time.Time),
		requestProcessingStartTimes: make(map[int]time.Time),
		reporter: reporter.NewParquetReporter(
			corrjoinConfig.ResultsDirectory, corrjoinConfig.MaxRowsPerRowGroup),
	}

	go func() {
		log.Println("watching observation queue")
		for {
			select {
			case observation := <-observationQueue:
				processor.accumulator.AddObservation(observation)
			}
		}
	}()

	go func() {
		log.Println("waiting for buffers")
		for {
			select {
			case observationResult := <-bufferChannel:
				if observationResult.Err != nil {
					log.Printf("failed to process window: %v", observationResult.Err)
				} else {
					log.Printf("got an observation request\n")
					requestedCorrelationBatches.Inc()
					requestStart := time.Now()
					stride := processor.window.StrideCounter + 1

					processor.strideStartTimes[stride] = observationResult.CurrentStrideStartTs

					stridesPerWindow := processor.settings.WindowSize / processor.settings.StrideLength
					if stride < stridesPerWindow {
						log.Printf("got data for stride %d but that is not enough for filling the window\n", stride)
					} else {
						firstStride := stride - stridesPerWindow + 1
						log.Printf("got a result for stride %d. There are %d strides per window, so I think the first stride for this window should be %d\n", stride, stridesPerWindow, firstStride)

						windowStart := processor.strideStartTimes[firstStride]
						windowEnd := observationResult.CurrentStrideMaxTs

						log.Printf("that gives us a window span from %v to %v aka %s to %s\n",
							windowStart, windowEnd,
							windowStart.UTC().Format("20060102150405"),
							windowEnd.UTC().Format("20060102150405"))

						// This creates the output file for an entire window, not just for the stride.
						processor.reporter.InitializeStride(stride, windowStart, windowEnd)
					}

					err, willRunComputation := processor.window.ShiftBuffer(observationResult.Buffers)

					if err != nil {
						// TODO: if window is busy, hold the observationResult
						switch err.(type) {
						case corrjoin.WindowIsBusyError:
							strideOverruns.Inc()
							log.Printf("computation time overrun on stride %d\n", processor.window.StrideCounter)
						default:
							log.Printf("failed to process window: %v", err)
						}
					}
					if err == nil && willRunComputation {
						processor.requestProcessingStartTimes[stride] = requestStart
						log.Printf("started processing stride %d\n", processor.window.StrideCounter)
					}
				}
			case <-time.After(10 * time.Minute):
				log.Printf("got no stride data for 10 minutes")
			}
		}
	}()

	// All writing to the reporter happens from this goroutine.
	go func() {
		log.Println("waiting for correlation results")
		for {
			select {
			case correlationResult := <-resultsChannel:
				if len(correlationResult.CorrelatedPairs) == 0 {
					log.Printf("empty correlation result, done with stride %d\n",
						correlationResult.StrideCounter)
					requestEnd := time.Now()
					requestStart, ok := processor.requestProcessingStartTimes[correlationResult.StrideCounter]
					if !ok {
						log.Printf("missing start time for stride %d?\n", correlationResult.StrideCounter)
					} else {
						elapsed := requestEnd.Sub(requestStart)
						correlationDurationHist.Observe(float64(elapsed.Milliseconds()))
						correlationDuration.Set(float64(elapsed.Milliseconds()))
						log.Printf("correlation batch processed in %d milliseconds\n", elapsed.Milliseconds())
					}
					stride := correlationResult.StrideCounter
					processor.reporter.RecordTimeseriesIds(stride, processor.accumulator.Tsids)
					numberOfTimeseries.Set(float64(len(processor.accumulator.Tsids)))
					processor.reporter.AddConstantRows(stride, processor.window.ConstantRows)
					constantTimeseries.Set(float64(len(processor.window.ConstantRows)))
					err := processor.reporter.Flush(stride)
					if err != nil {
						log.Printf("failed to flush results writer: %e\n", err)
					}
					log.Printf("finished recording data for stride %d\n", stride)
				} else {
					err := processor.reporter.AddCorrelatedPairs(*correlationResult)
					if err != nil {
						log.Printf("failed to log results: %v\n", err)
					}
				}
			}
		}
	}()

	return processor
}
