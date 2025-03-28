package explorer

import (
	"encoding/json"
	"fmt"
	explorerlib "github.com/kpaschen/corrjoin/lib/explorer"
	"github.com/prometheus/common/model"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	MAX_GRAPH_SIZE = 1500
)

// Types for the REST API
type subgraphResponse struct {
	Id   int `json:"id"`
	Size int `json:"size"`
}

type CorrelatedResponse struct {
	Rowid       string                               `json:"rowid"`
	Labels      map[model.LabelName]model.LabelValue `json:"labels"`
	LabelString string                               `json:"labelString"`
	Pearson     float32                              `json:"pearson"`
}

type metricInfoResponse struct {
	Stride      int                                  `json:"stride"`
	Rowid       string                               `json:"rowid"`
	Labels      map[model.LabelName]model.LabelValue `json:"labels"`
	LabelString string                               `json:"labelString"`
	Constant    bool                                 `json:"constant"`
	SubgraphId  int                                  `json:"subgraphId"`
}

type correlatedTimeseriesResponse struct {
	Correlates []CorrelatedResponse `json:"correlates"`
	Constant   bool                 `json:"constant"`
}

type subgraphNodeResponse struct {
	Id       string `json:"id"`
	Title    string `json:"title"`
	SubTitle string `json:"subtitle,optional"`
	MainStat string `json:"mainstat,optional"`
}

type subgraphEdgeResponse struct {
	Id        int    `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Thickness int    `json:"thickness"`
	Mainstat  string `json:"mainstat,optional"`
}

type TimeseriesResponse struct {
	Time  string `json:"time"` // This must be in YYYY-MM-DDTHH:MM:SSZ format
	Value int    `json:"value"`
}

// This is what the timeline panel wants.
type TimelineResponse struct {
	Time   string `json:"time"` // This must be in YYYY-MM-DDTHH:MM:SSZ format
	Metric string `json:"metric"`
	State  string `json:"state"`
}

func (c *CorrelationExplorer) GetStrides(w http.ResponseWriter, r *http.Request) {
	ret := make([]Stride, 0, STRIDE_CACHE_SIZE)
	for _, stride := range c.strideCache {
		if stride == nil {
			continue
		}
		ret = append(ret, *stride)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (c *CorrelationExplorer) GetSubgraphs(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	stride, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		http.Error(w, fmt.Errorf("invalid stride id").Error(), http.StatusNotFound)
		return
	}
	subgraphs := stride.subgraphs
	if subgraphs == nil {
		err := fmt.Errorf("no subgraphs found for stride %d\n", stride.ID)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := make([]subgraphResponse, 0, len(subgraphs.Sizes))
	for graphId, graphSize := range subgraphs.Sizes {
		resp = append(resp, subgraphResponse{Id: graphId, Size: graphSize})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// Get the nodes list for one subgraph.
func (c *CorrelationExplorer) GetSubgraphNodes(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	stride, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		err := fmt.Errorf("stride not found")
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	subgraphs := stride.subgraphs
	if subgraphs == nil {
		log.Printf("stride %d has no subgraphs\n", stride.ID)
		http.Error(w, fmt.Sprintf("stride %d has no subgraphs", stride.ID), http.StatusNotFound)
		return
	}
	subgraphId, err := c.getSubgraphId(params)
	if err != nil {
		log.Printf("error %v getting subgraph id from params\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	size, ok := subgraphs.Sizes[subgraphId]
	if !ok {
		err := fmt.Errorf("no subgraph with id %d", subgraphId)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Grafana will run out of memory trying to render large graphs. Worst case, this makes
	// the browser tab crash as well.
	// Truncating the nodes list like this will make the graph not render in grafana because
	// there will be orphaned edges.
	if size > MAX_GRAPH_SIZE {
		log.Printf("truncating graph %d from %d to %d nodes\n", subgraphId, size, MAX_GRAPH_SIZE)
		size = MAX_GRAPH_SIZE
	}

	resp := make([]subgraphNodeResponse, 0, size)

	for row, graphId := range subgraphs.Rows {
		if graphId == subgraphId {
			metric, exists := stride.metricsCache[row]
			if !exists {
				log.Printf("row %d is missing from the metrics cache\n", row)
				continue
			}
			r := subgraphNodeResponse{
				Id:       fmt.Sprintf("fp-%d", row),
				Title:    string(metric.LabelSet["__name__"]),
				SubTitle: metric.MetricString(),
				MainStat: fmt.Sprintf("%d", row),
			}
			resp = append(resp, r)
			if len(resp) >= size {
				break
			}
		}
	}
	log.Printf("returning %d nodes for graph %d\n", len(resp), subgraphId)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// Get the edges list for one subgraph.
func (c *CorrelationExplorer) GetSubgraphEdges(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	stride, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		err := fmt.Errorf("no stride found for params %v", params)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	subgraphs := stride.subgraphs
	if subgraphs == nil {
		err := fmt.Errorf("stride %d has no subgraphs", stride.ID)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	subgraphId, err := c.getSubgraphId(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, ok := subgraphs.Sizes[subgraphId]
	if !ok {
		http.Error(w, fmt.Errorf("no subgraph for id %d", subgraphId).Error(), http.StatusNotFound)
		return
	}

	edges, err := c.retrieveEdges(stride, subgraphId, MAX_GRAPH_SIZE)
	if err != nil {
		log.Printf("failed to get edges for subgraph %d: %v\n", subgraphId, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	log.Printf("retrieveEdges returned %d edges for subgraph %d\n", len(edges), subgraphId)

	resp := make([]subgraphEdgeResponse, len(edges), len(edges))

	for i, e := range edges {
		resp[i] = subgraphEdgeResponse{
			Id:       i,
			Source:   fmt.Sprintf("fp-%d", e.Source),
			Target:   fmt.Sprintf("fp-%d", e.Target),
			Mainstat: fmt.Sprintf("%f", e.Pearson),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (c *CorrelationExplorer) DumpMetricCache(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	stride, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		err := fmt.Errorf("no stride found for params %v", params)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var metricName string
	metricNames, ok := params["metric"]
	if !ok {
		metricName = ""
	} else {
		metricName = metricNames[0]
	}

	if metricName == "" {
		log.Printf("retrieving random metrics\n")
	} else {
		log.Printf("retrieving metrics with name %s\n", metricName)
	}

	maxMetrics := 200
	resp := make([]explorerlib.Metric, 0, maxMetrics)

	ctr := 0
	for _, cacheEntry := range stride.metricsCache {
		if metricName != "" {
			labels := cacheEntry.LabelSet
			if labels[model.LabelName("__name__")] != model.LabelValue(metricName) {
				continue
			}
		}
		resp = append(resp, *cacheEntry)
		ctr++
		if ctr > maxMetrics {
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (c *CorrelationExplorer) getSubgraphId(params url.Values) (int, error) {
	subgraph, ok := params["subgraph"]
	if !ok {
		return -1, fmt.Errorf("missing subgraph parameter in params %v", params)
	}
	subgraphID, err := strconv.ParseInt(strings.TrimSpace(subgraph[0]), 10, 32)
	if err != nil {
		return -1, fmt.Errorf("failed to parse an integer out of %+v", subgraph[0])
	}
	return int(subgraphID), nil
}

func (c *CorrelationExplorer) getStride(params url.Values) (*Stride, error) {
	strideId, ok := params["strideId"]
	if ok {
		s, err := strconv.ParseInt(strings.TrimSpace(strideId[0]), 10, 32)
		if err != nil {
			return nil, nil
		}
		for _, stride := range c.strideCache {
			if stride == nil {
				continue
			}
			if stride.Status != StrideProcessed {
				continue
			}
			if stride.ID == int(s) {
				return stride, nil
			}
		}
		return nil, nil
	}
	timeToString, ok := params["timeTo"]
	if !ok {
		// If there is no timeTo parameter, just return the latest stride.
		return c.getLatestStride(), nil
	}
	timeTo, err := strconv.ParseInt(strings.TrimSpace(timeToString[0]), 10, 64)
	if err != nil {
		log.Printf("failed to parse %s into an int64: %v\n", timeToString[0], err)
		inputFormat := "2006-01-02T15:04:05"
		t, err := time.Parse(inputFormat, timeToString[0])
		if err != nil {
			return nil, fmt.Errorf("failed to parse timeTo parameter %s: %v", timeToString[0], err)
		}
		timeTo = t.Unix()
	}
	for i, stride := range c.strideCache {
		if stride == nil {
			continue
		}
		if stride.Status != StrideProcessed {
			continue
		}
		if timeTo >= stride.StartTime && timeTo <= stride.EndTime {
			log.Printf("returning stride %d (%s -> %s) for time %s\n", i,
				stride.StartTimeString, stride.EndTimeString,
				time.Unix(timeTo, 0).UTC())
			return stride, nil
		}
	}
	return nil, nil
}

// Assume urlValue is of the form
// {__name__="kube_pod_container_info", container="grafana"}
// Note the keys are not quoted, that's why you cannot just unmarshal this with json.
// we assume there are no nested objects and parse this as just a key-value map.
func parseUrlDataIntoMetric(urlValue string, dropLabels map[string]bool) (*model.Metric, error) {
	if len(urlValue) == 0 {
		return nil, fmt.Errorf("empty url value")
	}
	log.Printf("parseUrlDataIntoMetric: got value %+v\n", urlValue)
	if urlValue[0] != '{' {
		return nil, fmt.Errorf("expected value to start with '{'")
	}
	if urlValue[len(urlValue)-1] != '}' {
		return nil, fmt.Errorf("expected value to end with '}'")
	}

	encodedLabelSet := urlValue[1 : len(urlValue)-1]

	ret := make(model.Metric)

	parts := strings.Split(encodedLabelSet, ", ")
	for _, part := range parts {
		keyvalue := strings.Split(part, "=")
		if len(keyvalue) != 2 {
			return nil, fmt.Errorf("bad key-value pair: %s\n", part)
		}
		dropme := dropLabels[keyvalue[0]]
		if !dropme {
			ret[(model.LabelName)(keyvalue[0])] = (model.LabelValue)(strings.Trim(keyvalue[1], `'"`))
		}
	}

	return &ret, nil
}

func (c *CorrelationExplorer) getIndexFromParams(params url.Values) (int, error) {
	index, ok := params["index"]
	if !ok {
		return -1, fmt.Errorf("missing index parameter")
	}
	value, err := strconv.ParseInt(index[0], 10, 32)
	if err != nil {
		return -1, err
	}
	return int(value), nil
}

func (c *CorrelationExplorer) getMetrics(params url.Values, stride *Stride) ([]uint64, error) {
	if stride == nil {
		return nil, fmt.Errorf("stride cannot be nil")
	}
	log.Printf("getMetrics: params is %v\n", params)
	metrics, err := c.getMetricsById(params, stride)
	if err == nil && metrics != nil && len(metrics) > 0 {
		ret := make([]uint64, len(metrics), len(metrics))
		for i, m := range metrics {
			ret[i] = m.Fingerprint
		}
		return ret, nil
	}

	labelset, ok := params["ts"]
	if !ok {
		return nil, fmt.Errorf("missing ts parameter")
	}
	modelMetric, err := parseUrlDataIntoMetric(labelset[0], c.dropLabels)

	if err != nil {
		log.Printf("getMetrics parsing error: %v\n", err)
		return nil, err
	}

	fp := modelMetric.Fingerprint()
	m, exists := stride.metricsCache[uint64(fp)]
	if !exists {
		log.Printf("metric not found in cache\n")
		return nil, nil
	}
	return []uint64{m.Fingerprint}, nil
}

func (c *CorrelationExplorer) getMetricsById(params url.Values, stride *Stride) ([]*explorerlib.Metric, error) {
	if stride == nil {
		return nil, fmt.Errorf("stride cannot be nil")
	}
	metricId, ok := params["tsid"]
	if !ok {
		return nil, fmt.Errorf("missing tsid parameter")
	}
	tsids, err := c.parseTsIdsFromGraphiteResult(strings.TrimSpace(metricId[0]))
	if err != nil {
		log.Printf("getMetricsById: failed to parse ts id: %v\n", err)
		return nil, err
	}
	log.Printf("tsids: %v\n", tsids)
	ret := make([]*explorerlib.Metric, len(tsids), len(tsids))
	for i, tsid := range tsids {
		metric, exists := stride.metricsCache[tsid]
		if !exists {
			log.Printf("getMetricsById: did not find a metric with id %d\n", tsid)
			return nil, fmt.Errorf("no metric with id %d", tsid)
		}
		ret[i] = metric
	}
	return ret, nil
}

func (c *CorrelationExplorer) getTimeOverride(params url.Values) (string, string, error) {
	isOverride, ok := params["timeOverride"]
	if ok && isOverride[0] == "false" {
		return "", "", nil
	}
	timeToString := ""
	timeFromString := ""
	formatString := explorerlib.FORMAT
	inputFormat := "2006-01-02T15:04:05"
	timeTo, ok := params["timeTo"]
	if ok {
		if timeTo[0] == "now" {
			timeToString = time.Now().UTC().Format(formatString)
		} else {
			t, err := time.Parse(inputFormat, timeTo[0])
			if err != nil {
				return "", "", err
			}
			timeToString = t.UTC().Format(formatString)
		}
	}
	timeFrom, ok := params["timeFrom"]
	if ok {
		t, err := time.Parse(inputFormat, timeFrom[0])
		if err != nil {
			return "", timeToString, err
		}
		timeFromString = t.UTC().Format(formatString)
	} else {
		if timeToString != "" {
			t, _ := time.Parse(formatString, timeToString)
			timeFrom := t.Add(-time.Minute * 10)
			timeFromString = timeFrom.UTC().Format(formatString)
		}
	}
	return timeFromString, timeToString, nil
}

func (c *CorrelationExplorer) GetMetricHistory(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	log.Printf("processing history request with parameters %+v\n", params)
	stride, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		http.Error(w, fmt.Sprintf("no stride found for time"), http.StatusNotFound)
		return
	}
	metrics, err := c.getMetricsById(params, stride)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if metrics == nil || len(metrics) == 0 {
		http.Error(w, "missing metrics", http.StatusNotFound)
		return
	}
	resp := make([]TimelineResponse, 0, len(metrics)*len(c.strideCache))
	for _, st := range c.strideCache {
		if st == nil {
			continue
		}
		if st.Status != StrideProcessed {
			continue
		}
		for _, m := range metrics {
			if m.Constant {
				resp = append(resp, TimelineResponse{
					Time:   st.StartTimeString,
					Metric: fmt.Sprintf("fp-%d", m.Fingerprint),
					State:  "-1",
				})
			} else {
				if st.subgraphs == nil {
					continue
				}
				graphId, exists := st.subgraphs.Rows[m.Fingerprint]
				if !exists {
					// Metric is not correlated with anything.
					resp = append(resp, TimelineResponse{
						Time:   st.StartTimeString,
						Metric: fmt.Sprintf("fp-%d", m.Fingerprint),
						State:  "0",
					})
				} else {
					size := st.subgraphs.Sizes[graphId]
					resp = append(resp, TimelineResponse{
						Time:   st.StartTimeString,
						Metric: fmt.Sprintf("fp-%d", m.Fingerprint),
						State:  fmt.Sprintf("%d", size),
					})
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (c *CorrelationExplorer) GetTimeseries(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	log.Printf("processing timeseries request with parameters %+v\n", params)
	stride, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		http.Error(w, fmt.Sprintf("no stride found for time"), http.StatusNotFound)
		return
	}
	metrics, err := c.getMetricsById(params, stride)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if metrics == nil || len(metrics) == 0 {
		http.Error(w, "missing metrics", http.StatusNotFound)
		return
	}
	if c.prometheusBaseURL == "" {
		err = fmt.Errorf("no prometheus backend configured")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	index, err := c.getIndexFromParams(params)
	if err != nil || index == -1 {
		index = 0
	}
	if index >= len(metrics) {
		resp := make([]TimeseriesResponse, 0)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
		return
	}

	startTimeString := stride.StartTimeString
	endTimeString := stride.EndTimeString

	timeFromOverride, timeToOverride, _ := c.getTimeOverride(params)
	if timeFromOverride != "" {
		startTimeString = timeFromOverride
	}
	if timeToOverride != "" {
		endTimeString = timeToOverride
	}

	promQLQuery := metrics[index].ComputePrometheusQuery()
	// TODO: use the client api instead?
	requestURL := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%s&end=%s&step=15s",
		c.prometheusBaseURL,
		promQLQuery,
		url.QueryEscape(startTimeString),
		url.QueryEscape(endTimeString))
	log.Printf("requesting ts data using %s\n", requestURL)
	res, err := http.Get(requestURL)
	if err != nil {
		log.Printf("failed to request timeseries data: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("got response with status code %d\n", res.StatusCode)
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("failed to read response: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var parsedResponse PromQueryResponse
	err = json.Unmarshal([]byte(resBody), &parsedResponse)
	if err != nil {
		log.Printf("failed to parse response: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	results := parsedResponse.Data.Result
	// results should be a list. We assume that the list has exactly one entry because
	// our timeseries id is supposed to be unique.
	// Sometimes the timeseries we get in the link contain labels that grafana doesn't recognise,
	// usually because they've been dropped. In that case, results will be empty here.
	if len(results) == 0 {
		log.Printf("response was empty\n")
		resp := make([]TimeseriesResponse, 0)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
		return
	}
	if len(results) != 1 {
		err = fmt.Errorf("results has unexpected length. %+v", results)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := make([]TimeseriesResponse, len(results[0].Values))
	log.Printf("making timeseries response with %d values\n", len(results[0].Values))
	for i, tsr := range results[0].Values {
		tm, err := explorerlib.ConvertToUnixTime(tsr[0])
		if err != nil {
			log.Printf("failed to convert %v (%T) to time\n", tsr[0], tsr[0])
			continue
		}
		value, _ := strconv.ParseInt(tsr[1].(string), 10, 32)
		resp[i] = TimeseriesResponse{
			Time:  tm.UTC().Format(explorerlib.FORMAT),
			Value: int(value),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (c *CorrelationExplorer) extendTimeline(resp []TimelineResponse,
	correlatesMap map[uint64]float32, knownTimeseries map[uint64]bool,
	st *Stride) []TimelineResponse {

	rowidsFound := make(map[uint64]bool)
	newRowIds := make(map[uint64]bool)
	for rowid, pearson := range correlatesMap {
		alreadyKnown := knownTimeseries[rowid]
		if !alreadyKnown {
			knownTimeseries[rowid] = true
			newRowIds[rowid] = true
		}
		rowidsFound[rowid] = true
		resp = append(resp, TimelineResponse{
			Time:   st.StartTimeString,
			Metric: fmt.Sprintf("fp-%d", rowid),
			State:  fmt.Sprintf("%f", pearson),
		})
	}
	// Insert '0' values for time series that were present in an earlier stride but are now missing.
	for rowid, _ := range knownTimeseries {
		_, ok := rowidsFound[rowid]
		if !ok {
			resp = append(resp, TimelineResponse{
				Time:   st.StartTimeString,
				Metric: fmt.Sprintf("fp-%d", rowid),
				State:  "0.0",
			})
		}
	}
	// insert '0' values for time series that are new in this stride.
	for _, earlierStride := range c.strideCache {
		if earlierStride == nil {
			continue
		}
		if earlierStride.Status != StrideProcessed {
			continue
		}
		if earlierStride.ID == st.ID {
			break
		}
		for rowid, _ := range newRowIds {
			resp = append(resp, TimelineResponse{
				Time:   earlierStride.StartTimeString,
				Metric: fmt.Sprintf("fp-%d", rowid),
				State:  "0.0",
			})
		}
	}
	return resp
}

func (c *CorrelationExplorer) GetTimeline(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	stride, err := c.getStride(params)
	if err != nil {
		log.Printf("GetTimeline: error getting stride %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		http.Error(w, fmt.Sprintf("no stride found for time"), http.StatusNotFound)
		return
	}

	metricRowIds, err := c.getMetrics(params, stride)
	if err != nil {
		log.Printf("GetTimeline: error %v getting metrics\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metricRowIds == nil {
		log.Printf("no metrics found for GetTimeline\n")
		http.Error(w, fmt.Sprintf("no metric found for ts %s", params["ts"][0]), http.StatusNotFound)
		return
	}

	resp := make([]TimelineResponse, 0, len(c.strideCache)*(len(metricRowIds)))

	// If more than one metric row id is in the request, limit the response
	// to just these. Otherwise we get all correlated pairs for the given row id.
	var otherRowIds []uint64
	if len(metricRowIds) == 1 {
		otherRowIds = []uint64{}
	} else {
		otherRowIds = metricRowIds[1 : len(metricRowIds)-1]
	}

	rowidsFromAllStrides := make(map[uint64]bool)
	// Look up the Pearson correlation of the first selected timeseries with any of the others across all strides.
	for _, st := range c.strideCache {
		if st == nil {
			continue
		}
		if st.Status != StrideProcessed {
			continue
		}
		correlatesMap, err := c.retrieveCorrelatedTimeseries(st, metricRowIds[0], otherRowIds, 20)
		if err != nil {
			log.Printf("error retrieving correlates for timeseries %d and stride %d: %v\n", metricRowIds[0], st.ID, err)
			continue
		}
		resp = c.extendTimeline(resp, correlatesMap, rowidsFromAllStrides, st)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (c *CorrelationExplorer) GetMetricInfo(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	stride, err := c.getStride(params)
	if err != nil {
		log.Printf("error getting stride: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		log.Printf("no stride found\n")
		http.Error(w, fmt.Sprintf("no stride found for time"), http.StatusNotFound)
		return
	}
	log.Printf("GetMetricInfo: using stride %d\n", stride.ID)

	metricRowIds, err := c.getMetrics(params, stride)
	if err != nil {
		log.Printf("failed to identify metrics from request: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metricRowIds == nil || len(metricRowIds) == 0 {
		log.Printf("no metrics found for params %v\n", params)
		http.Error(w, fmt.Sprintf("no metrics found for params %v", params), http.StatusNotFound)
		return
	}

	resp := make([]metricInfoResponse, 0, len(metricRowIds))

	for _, rowid := range metricRowIds {
		m, exists := stride.metricsCache[rowid]
		if !exists || m == nil {
			log.Printf("missing metric with row id %d\n", rowid)
			continue
		}
		subgraphId := -1
		if stride.subgraphs != nil {
			var exists bool
			subgraphId, exists = stride.subgraphs.Rows[rowid]
			if !exists {
				subgraphId = -1
			}
		}
		resp = append(resp, metricInfoResponse{
			Stride:      stride.ID,
			Rowid:       fmt.Sprintf("fp-%d", rowid),
			Labels:      m.LabelSet,
			LabelString: m.MetricString(),
			Constant:    m.Constant,
			SubgraphId:  subgraphId,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (c *CorrelationExplorer) GetCorrelatedSeries(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	stride, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if stride == nil {
		http.Error(w, fmt.Sprintf("no stride found for time"), http.StatusNotFound)
		return
	}

	metricRowIds, err := c.getMetrics(params, stride)
	if err != nil {
		log.Printf("failed to identify metrics from request: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metricRowIds == nil || len(metricRowIds) == 0 {
		http.Error(w, fmt.Sprintf("no metric found for ts %s", params["ts"][0]), http.StatusNotFound)
		return
	}

	var otherRowIds []uint64
	if len(metricRowIds) == 1 {
		otherRowIds = []uint64{}
	} else {
		otherRowIds = metricRowIds[1 : len(metricRowIds)-1]
	}

	correlatesMap, err := c.retrieveCorrelatedTimeseries(stride, metricRowIds[0], otherRowIds, 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if len(correlatesMap) == 0 {
		resp := correlatedTimeseriesResponse{
			Correlates: make([]CorrelatedResponse, 0),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := correlatedTimeseriesResponse{
		Correlates: make([]CorrelatedResponse, 0, 200),
		Constant:   false,
	}

	for otherRowId, pearson := range correlatesMap {
		otherMetric, exists := stride.metricsCache[otherRowId]
		if !exists {
			err = fmt.Errorf("ts %d is allegedly correlated with %d but that does not exist", metricRowIds[0], otherRowId)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		resp.Correlates = append(resp.Correlates, CorrelatedResponse{
			Rowid:       fmt.Sprintf("fp-%d", otherRowId),
			Labels:      map[model.LabelName]model.LabelValue(otherMetric.LabelSet),
			LabelString: otherMetric.MetricString(),
			Pearson:     pearson,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
