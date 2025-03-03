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
	Rowid       int                                  `json:"rowid"`
	Labels      map[model.LabelName]model.LabelValue `json:"labels"`
	LabelString string                               `json:"labelString"`
	Pearson     float32                              `json:"pearson"`
}

type correlatedTimeseriesResponse struct {
	Correlates []CorrelatedResponse `json:"correlates"`
	Constant   bool                 `json:"constant"`
}

type subgraphNodeResponse struct {
	Id       int    `json:"id"`
	Title    string `json:"title"`
	SubTitle string `json:"subtitle,optional"`
	MainStat string `json:"mainstat,optional"`
}

type subgraphEdgeResponse struct {
	Id        int    `json:"id"`
	Source    int    `json:"source"`
	Target    int    `json:"target"`
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
	Metric int    `json:"metric"`
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
		http.Error(w, err.Error(), http.StatusNotFound)
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
			metric, exists := stride.metricsCacheByRowId[row]
			if !exists {
				log.Printf("row %d is missing from the metrics cache\n", row)
				continue
			}
			r := subgraphNodeResponse{
				Id:       row,
				Title:    string(metric.LabelSet["__name__"]),
				MainStat: metric.MetricString(),
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

	edges, err := c.retrieveEdges(stride, subgraphId)
	if !ok {
		http.Error(w, fmt.Errorf("no subgraph for id %d", subgraphId).Error(), http.StatusNotFound)
		return
	}

	resp := make([]subgraphEdgeResponse, len(edges), len(edges))

	for i, e := range edges {
		resp[i] = subgraphEdgeResponse{
			Id:       i,
			Source:   e.Source,
			Target:   e.Target,
			Mainstat: fmt.Sprintf("%f", e.Pearson),
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
			if stride.Status == StrideDeleted {
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
		return nil, fmt.Errorf("failed to parse int from timeTo parameter %s: %v", timeToString[0], err)
	}
	for i, stride := range c.strideCache {
		if stride == nil {
			continue
		}
		if stride.Status == StrideDeleted {
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
func parseUrlDataIntoMetric(urlValue string) (*model.Metric, error) {
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
		ret[(model.LabelName)(keyvalue[0])] = (model.LabelValue)(strings.Trim(keyvalue[1], `'"`))
	}

	return &ret, nil
}

func (c *CorrelationExplorer) getMetric(params url.Values, stride *Stride) (int, error) {
	if stride == nil {
		return -1, fmt.Errorf("stride cannot be nil")
	}
	metric, err := c.getMetricRowId(params, stride)
	if err == nil && metric != nil {
		return metric.RowId, nil
	}
	labelset, ok := params["ts"]
	if !ok {
		return -1, fmt.Errorf("missing ts parameter")
	}
	modelMetric, err := parseUrlDataIntoMetric(labelset[0])

	if err != nil {
		log.Printf("err: %v\n", err)
		return -1, err
	}

	fp := modelMetric.Fingerprint()
	fmt.Printf("metric is %+v and fingerprint is %d\n", modelMetric, fp)
	rowid, exists := stride.metricsCache[uint64(fp)]
	if !exists {
		return -1, nil
	}
	return rowid, nil
}

func (c *CorrelationExplorer) getMetricRowId(params url.Values, stride *Stride) (*explorerlib.Metric, error) {
	metricRowId, ok := params["tsid"]
	if !ok {
		return nil, fmt.Errorf("missing tsid parameter")
	}
	metricId, err := strconv.ParseInt(strings.TrimSpace(metricRowId[0]), 10, 32)
	if err != nil {
		return nil, err
	}
	if stride == nil {
		return nil, fmt.Errorf("stride cannot be nil")
	}
	metric, exists := stride.metricsCacheByRowId[int(metricId)]
	if !exists {
		return nil, fmt.Errorf("no metric with row id %d", metricId)
	}
	return metric, nil
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
	metric, err := c.getMetricRowId(params, stride)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if c.prometheusBaseURL == "" {
		err = fmt.Errorf("no prometheus backend found")
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	promQLQuery := metric.ComputePrometheusQuery()
	// make a request like this: 'http://prometheus:9090/api/v1/query_range?query=up&start=2015-07-01T20:10:30.781Z&end=2015-07-01T20:11:00.781Z&step=15s'
	// TODO: maybe use the client api instead?
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
	log.Printf("response body: %s\n", resBody)

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
	if len(results) == 0 {
		err = fmt.Errorf("no results found for timeseries")
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if len(results) != 1 {
		err = fmt.Errorf("results has unexpected length. %+v", results)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := make([]TimeseriesResponse, len(results[0].Values))
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

func (c *CorrelationExplorer) GetTimeline(w http.ResponseWriter, r *http.Request) {
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

	metricRowId, err := c.getMetric(params, stride)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metricRowId == -1 {
		http.Error(w, fmt.Sprintf("no metric found for ts %s", params["ts"][0]), http.StatusNotFound)
		return
	}

	// Look up the correlates of this timeseries in all strides.
	timelinemap := make(map[int](map[int]float32))
	allRowIds := make(map[int]bool)
	for _, st := range c.strideCache {
		if st == nil {
			continue
		}
		if st.Status == StrideDeleted {
			continue
		}
		// TODO: could retrieve the data for all strides in parallel
		correlatesMap, err := c.retrieveCorrelatedTimeseries(st, metricRowId)
		if err != nil {
			log.Printf("error retrieving correlates for timeseries %d and stride %d: %v\n", metricRowId, st.ID, err)
			continue
		}
		timelinemap[st.ID] = correlatesMap
		for rowid, _ := range correlatesMap {
			allRowIds[rowid] = true
		}
	}

	log.Printf("found a total of %d timeseries for the timeline of ts %d\n", len(allRowIds), metricRowId)

	resp := make([]TimelineResponse, 0, len(c.strideCache)*len(allRowIds))
	for _, st := range c.strideCache {
		if st == nil {
			continue
		}
		if st.Status == StrideDeleted {
			continue
		}
		timelines := timelinemap[st.ID]
		for rowid, _ := range allRowIds {
			pearson, exists := timelines[rowid]
			if !exists {
				resp = append(resp, TimelineResponse{
					Time:   st.EndTimeString,
					Metric: rowid,
					State:  "0",
				})
			} else {
				resp = append(resp, TimelineResponse{
					Time:   st.EndTimeString,
					Metric: rowid,
					State:  fmt.Sprintf("%f", pearson),
				})
			}
		}
	}

	log.Printf("returning %d entries for timeline\n", len(resp))

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

	metricRowId, err := c.getMetric(params, stride)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metricRowId == -1 {
		http.Error(w, fmt.Sprintf("no metric found for ts %s", params["ts"][0]), http.StatusNotFound)
		return
	}

	correlatesMap, err := c.retrieveCorrelatedTimeseries(stride, metricRowId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if len(correlatesMap) == 0 {
		log.Printf("correlates map for %d is empty\n", metricRowId)
		resp := correlatedTimeseriesResponse{
			Correlates: make([]CorrelatedResponse, 0),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := correlatedTimeseriesResponse{
		Correlates: make([]CorrelatedResponse, 0, len(correlatesMap)),
		Constant:   false,
	}

	for otherRowId, pearson := range correlatesMap {
		otherMetric, exists := stride.metricsCacheByRowId[otherRowId]
		if !exists {
			err = fmt.Errorf("ts %d is allegedly correlated with %d but that does not exist", metricRowId, otherRowId)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		resp.Correlates = append(resp.Correlates, CorrelatedResponse{
			Rowid:       otherRowId,
			Labels:      map[model.LabelName]model.LabelValue(otherMetric.LabelSet),
			LabelString: otherMetric.MetricString(),
			Pearson:     pearson,
		})
	}

	log.Printf("returning %d correlated ts for ts %d and stride %d\n", len(resp.Correlates), metricRowId, stride.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
