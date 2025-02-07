package explorer

import (
	"encoding/json"
	"fmt"
	"github.com/prometheus/common/model"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Types for the REST API
type timeseriesIdResponse struct {
	Rowid int `json:"rowid"`
}

type strideListResponse struct {
	Strides []Stride `json:"strides"`
}

type subgraphResponse struct {
	Id   int `json:"id"`
	Size int `json:"size"`
}

type subgraphListResponse struct {
	Subgraphs []subgraphResponse `json:"subgraphs"`
}

type TimeseriesResponse struct {
	Rowid   int               `json:"rowid"`
	Labels  map[string]string `json:"labels"`
	Pearson float32           `json:"pearson"`
}

type correlatedTimeseriesResponse struct {
	Correlates []TimeseriesResponse `json:"correlates"`
}

type subgraphNodeResponse struct {
	Id       int    `json:"id"`
	Title    string `json:"title"`
	SubTitle string `json:"subtitle,optional"`
}

type subgraphEdgeResponse struct {
	Id        int    `json:"id"`
	Source    int    `json:"source"`
	Target    int    `json:"target"`
	Thickness int    `json:"thickness"`
	Mainstat  string `json:"mainstat,optional"`
}

type subgraphNodesResponse struct {
	Nodes []subgraphNodeResponse `json:"nodes"`
}

type subgraphEdgesResponse struct {
	Edges []subgraphEdgeResponse `json:"edges"`
}

func (c *CorrelationExplorer) GetStrides(w http.ResponseWriter, r *http.Request) {
	ret := strideListResponse{
		Strides: make([]Stride, 0, STRIDE_CACHE_SIZE),
	}
	for _, stride := range c.strideCache {
		if stride == nil {
			continue
		}
		ret.Strides = append(ret.Strides, *stride)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ret)
}

func (c *CorrelationExplorer) GetSubgraphs(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	strideId, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strideId == -1 {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	stride := c.strideCache[strideId]
	subgraphs := stride.Subgraphs
	if subgraphs == nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := subgraphListResponse{
		Subgraphs: make([]subgraphResponse, 0, len(subgraphs.Sizes)),
	}
	for graphId, graphSize := range subgraphs.Sizes {
		resp.Subgraphs = append(resp.Subgraphs,
			subgraphResponse{Id: graphId, Size: graphSize})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// Get the nodes list for one subgraph.
func (c *CorrelationExplorer) GetSubgraphNodes(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	strideId, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strideId == -1 {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	stride := c.strideCache[strideId]
	subgraphs := stride.Subgraphs
	if subgraphs == nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	subgraphId, err := c.getSubgraphId(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	size, ok := subgraphs.Sizes[subgraphId]
	if !ok {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := subgraphNodesResponse{
		Nodes: make([]subgraphNodeResponse, 0, size),
	}

	for row, graphId := range subgraphs.Rows {
		if graphId == subgraphId {
			r := subgraphNodeResponse{
				Id:    row,
				Title: fmt.Sprintf("%d", row),
				// TODO: retrieve the metric from the cache so we can print the metric name
				// and the labels here
			}
			resp.Nodes = append(resp.Nodes, r)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// Get the edges list for one subgraph.
func (c *CorrelationExplorer) GetSubgraphEdges(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	strideId, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strideId == -1 {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	stride := c.strideCache[strideId]
	subgraphs := stride.Subgraphs
	if subgraphs == nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	subgraphId, err := c.getSubgraphId(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	size, ok := subgraphs.Sizes[subgraphId]
	if !ok {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := subgraphEdgesResponse{
		Edges: make([]subgraphEdgeResponse, 0, size),
	}

	/*
	   // TODO: read the correlation information
	     counter := 0
	     for row, graphId := range subgraphs.Rows {
	        if graphId == subgraphId {
	           e := subgraphEdgeResponse{
	              Id: counter,
	              Source:
	              Target:
	           }
	           resp.Edges = append(resp.Edges, e)
	           counter++
	        }
	     }
	*/
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (c *CorrelationExplorer) getSubgraphId(params url.Values) (int, error) {
	subgraph, ok := params["subgraph"]
	if !ok {
		return -1, fmt.Errorf("missing subgraph parameter")
	}
	subgraphID, err := strconv.ParseInt(strings.TrimSpace(subgraph[0]), 10, 32)
	if err != nil {
		return -1, fmt.Errorf("failed to parse an integer out of %+v", subgraph[0])
	}
	return int(subgraphID), nil
}

func (c *CorrelationExplorer) getStride(params url.Values) (int, error) {
	timeToString, ok := params["timeTo"]
	if !ok {
		// If there is no timeTo parameter, just return the latest stride.
		return c.getLatestStride(), nil
	}
	timeTo, err := strconv.ParseInt(strings.TrimSpace(timeToString[0]), 10, 64)
	if err != nil {
		log.Printf("failed to parse %s into an int64: %v\n", timeToString[0], err)
		return -1, fmt.Errorf("failed to parse int from timeTo parameter %s: %v", timeToString[0], err)
	}
	for i, stride := range c.strideCache {
		if stride == nil {
			continue
		}
		if timeTo >= stride.StartTime && timeTo <= stride.EndTime {
			log.Printf("returning stride %d (%s -> %s) for time %s\n", i,
				stride.StartTimeString, stride.EndTimeString,
				time.Unix(timeTo, 0).UTC())
			return i, nil
		}
	}
	return -1, nil
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

func (c *CorrelationExplorer) getMetric(params url.Values, strideId int) (int, error) {
	labelset, ok := params["ts"]
	if !ok {
		return -1, fmt.Errorf("missing ts parameter")
	}
	metric, err := parseUrlDataIntoMetric(labelset[0])

	if err != nil {
		log.Printf("err: %v\n", err)
		return -1, err
	}
	fp := metric.Fingerprint()
	fmt.Printf("metric is %+v and fingerprint is %d\n", metric, fp)
	rowid, exists := c.metricsCache[uint64(fp)]
	if !exists {
		return -1, nil
	}
	return rowid, nil
}

func (c *CorrelationExplorer) GetTimeseriesId(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	strideId, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strideId == -1 {
		http.Error(w, fmt.Sprintf("no stride found for time"), http.StatusNotFound)
		return
	}

	metricRowId, err := c.getMetric(params, strideId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metricRowId == -1 {
		http.Error(w, fmt.Sprintf("no metric found for ts %s", params["ts"][0]), http.StatusNotFound)
		return
	}

	// return json for metric row id
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(timeseriesIdResponse{Rowid: metricRowId})
}

func (c *CorrelationExplorer) GetCorrelatedSeries(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	strideId, err := c.getStride(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strideId == -1 {
		http.Error(w, fmt.Sprintf("no stride found for time"), http.StatusNotFound)
		return
	}

	metricRowId, err := c.getMetric(params, strideId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if metricRowId == -1 {
		http.Error(w, fmt.Sprintf("no metric found for ts %s", params["ts"][0]), http.StatusNotFound)
		return
	}

	// TODO: look up this metric and retrieve everything correlated with it
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(timeseriesIdResponse{Rowid: metricRowId})
}
