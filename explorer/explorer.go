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

const (
	MAX_GRAPH_SIZE = 1500
)

// Types for the REST API
type timeseriesIdResponse struct {
	Rowid int `json:"rowid"`
}

type subgraphResponse struct {
	Id   int `json:"id"`
	Size int `json:"size"`
}

type TimeseriesResponse struct {
	Rowid   int                                  `json:"rowid"`
	Labels  map[model.LabelName]model.LabelValue `json:"labels"`
	Pearson float32                              `json:"pearson"`
}

type correlatedTimeseriesResponse struct {
	Correlates []TimeseriesResponse `json:"correlates"`
	Constant   bool                 `json:"bool"`
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
	subgraphs := stride.subgraphs
	if subgraphs == nil {
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
	strideId, err := c.getStride(params)
	if err != nil {
    log.Printf("error %v retrieving stride %d\n", err, strideId)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strideId == -1 {
    log.Printf("stride %d not found\n", strideId)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	stride := c.strideCache[strideId]
	subgraphs := stride.subgraphs
	if subgraphs == nil {
    log.Printf("stride %d has no subgraphs\n", strideId)
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
    log.Printf("no subgraph with id %d\n", subgraphId)
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
			metric, exists := c.metricsCacheByRowId[row]
			if !exists {
				log.Printf("row %d is missing from the metrics cache\n", row)
				continue
			}
			r := subgraphNodeResponse{
				Id:       row,
				Title: string(metric.LabelSet["__name__"]),
				MainStat: metric.MetricString(),
			}
			resp = append(resp, r)
			if len(resp) >= size {
				break
			}
		}
	}
  log.Printf("returning nodes for graph %d\n", subgraphId)
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
	subgraphs := stride.subgraphs
	if subgraphs == nil {
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	edges, err := c.retrieveEdges(stride, subgraphId)
	if !ok {
		http.Error(w, err.Error(), http.StatusNotFound)
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
		return -1, fmt.Errorf("missing subgraph parameter")
	}
	subgraphID, err := strconv.ParseInt(strings.TrimSpace(subgraph[0]), 10, 32)
	if err != nil {
		return -1, fmt.Errorf("failed to parse an integer out of %+v", subgraph[0])
	}
	return int(subgraphID), nil
}

func (c *CorrelationExplorer) getStride(params url.Values) (int, error) {
	strideId, ok := params["strideId"]
	if ok {
		s, err := strconv.ParseInt(strings.TrimSpace(strideId[0]), 10, 32)
		if err != nil {
			return -1, nil
		}
		for i, stride := range c.strideCache {
			if stride == nil {
				continue
			}
			if stride.ID == int(s) {
				return i, nil
			}
		}
		return -1, nil
	}
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

	stride := c.strideCache[strideId]
	subgraphs := stride.subgraphs
	if subgraphs == nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	metric, exists := c.metricsCacheByRowId[metricRowId]
	if !exists {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if metric.Constant {
		resp := correlatedTimeseriesResponse{
			Correlates: make([]TimeseriesResponse, 0),
			Constant:   true,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
		return
	}

	graphId, exists := subgraphs.Rows[metricRowId]
	if !exists {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	edges, err := c.retrieveEdges(stride, graphId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := correlatedTimeseriesResponse{
		Correlates: make([]TimeseriesResponse, 0, len(edges)),
		Constant:   false,
	}

	for _, e := range edges {
		if e.Pearson > 0 && (e.Source == metricRowId || e.Target == metricRowId) {
			var otherRowId int
			if e.Source == metricRowId {
				otherRowId = e.Target
			} else {
				otherRowId = e.Source
			}
			otherMetric, exists := c.metricsCacheByRowId[metricRowId]
			if !exists {
				log.Printf("ts %d is allegedly correlated with %d but that does not exist\n", metricRowId, otherRowId)
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			resp.Correlates = append(resp.Correlates, TimeseriesResponse{
				Rowid:   otherRowId,
				Labels:  map[model.LabelName]model.LabelValue(otherMetric.LabelSet),
				Pearson: e.Pearson,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
