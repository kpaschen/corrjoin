package explorer

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

type CorrelationExplorer struct {
	clusterTemplate     *template.Template
	strideListTemplate  *template.Template
	correlationTemplate *template.Template
	FilenameBase        string
	WebRoot             string
	cache               strideCache
	prometheusBaseURL   string

	// This should be the length of a stride expressed as a duration that
	// Prometheus can understand. e.g. "10m" or "1h".
	TimeRange string // e.g. "10m"
}

type strideCache struct {
	strides map[string]Stride
}

func (m *Metric) computePrometheusGraphURL(prometheusBaseURL string, timeRange string, endTime string) {
	if len(m.Data) == 0 {
		m.PrometheusGraphURL = fmt.Sprintf("%s/graph", prometheusBaseURL)
		return
	}
	attributeString := "{"
	first := true
	for key, value := range m.Data {
		stringv, ok := value.(string)
		if !ok {
			log.Printf("value %q was not a string\n", value)
			continue
		}
		if key == "__name__" {
			continue
		}
		if first {
			attributeString = fmt.Sprintf("%s%s=\"%s\"", attributeString, key, string(stringv))
			first = false
		} else {
			attributeString = fmt.Sprintf("%s, %s=\"%s\"", attributeString, key, string(stringv))
		}
	}
	attributeString = attributeString + "}"
	attributeString = url.QueryEscape(attributeString)

	m.PrometheusGraphURL = fmt.Sprintf("%s/graph?g0.expr=%s%s&g0.tab=0&g0.display_mode=lines&g0.show_exemplars=0&g0.range_input=%s&g0.end_input=%s&g0.moment_input=%s",
		prometheusBaseURL, m.Data["__name__"], attributeString, timeRange, url.QueryEscape(endTime),
		url.QueryEscape(endTime))
}

func (c *CorrelationExplorer) Initialize() error {
	t, err := template.ParseFiles(filepath.Join(c.WebRoot, "/templates/index.tmpl"))
	if err != nil {
		log.Println(fmt.Sprintf("%v", err))
		return err
	}
	c.strideListTemplate = t
	t, err = template.ParseFiles(filepath.Join(c.WebRoot, "/templates/cluster.tmpl"))
	if err != nil {
		log.Println(fmt.Sprintf("%v", err))
		return err
	}
	c.clusterTemplate = t
	t, err = template.ParseFiles(filepath.Join(c.WebRoot, "/templates/correlations.tmpl"))
	if err != nil {
		log.Println(fmt.Sprintf("%v", err))
		return err
	}
	c.correlationTemplate = t
	c.cache = strideCache{
		strides: make(map[string]Stride),
	}
	c.prometheusBaseURL = "http://localhost:9090"
	return nil
}

func (c *ClusterAssignments) addPair(row1 int64, row2 int64) {
	c1, have1 := c.Rows[row1]
	c2, have2 := c.Rows[row2]

	// Case 1: neither row has a cluster yet -> add them both to a new cluster.
	if !have1 && !have2 {
		c.Rows[row1] = c.nextClusterId
		c.Rows[row2] = c.nextClusterId
		c.nextClusterId++
		return
	}

	// Case 2: only one row has a cluster id
	if !have2 {
		c.Rows[row2] = c1
		return
	}

	if !have1 {
		c.Rows[row1] = c2
		return
	}

	// Case 2: both rows have a cluster id. Nothing to do if they match,
	// otherwise merge.
	if c1 == c2 {
		return
	}

	// Always merge into the lower-numbered cluster.
	if c1 < c2 {
		for row, cluster := range c.Rows {
			if cluster == c2 {
				c.Rows[row] = c1
			}
		}
	} else {
		for row, cluster := range c.Rows {
			if cluster == c1 {
				c.Rows[row] = c2
			}
		}
	}
}

func (c *CorrelationExplorer) readTsNames(reader *csv.Reader, tsids []int64, stride Stride) (map[int64]Metric, error) {
	ret := make(map[int64]Metric)
	for _, id := range tsids {
		ret[id] = Metric{empty: true, Data: make(map[string]interface{})}
	}
	strideDuration := time.Unix(stride.EndTime, 0).Sub(time.Unix(stride.StartTime, 0)).String()
	strideEnd := time.Unix(stride.EndTime, 0).Format("2006-01-02 15:04:05")
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ret, err
		}
		if len(record) != 2 {
			log.Printf("unexpected ts name record: %v\n", record)
			continue
		}
		tsid, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			log.Printf("failed to convert first record %s to ts id: %v\n", record[0], err)
			continue
		}
		m, requested := ret[tsid]
		if !requested {
			continue
		}
		if !m.empty {
			log.Printf("metric id %d was requested twice\n", tsid)
			continue
		}
		err = json.Unmarshal([]byte(record[1]), &m.Data)
		if err != nil {
			log.Printf("failed to unmarshal metric %s: %v\n", record[1], err)
			continue
		}
		m.empty = false
		(&m).computePrometheusGraphURL(c.prometheusBaseURL, strideDuration, strideEnd)
		ret[tsid] = m
	}
	return ret, nil
}

func (c *CorrelationExplorer) retrieveCorrelations(reader *csv.Reader, tsid int64) (Correlations, error) {
	// Disable record length testing.
	reader.FieldsPerRecord = -1
	ret := Correlations{
		Row:        tsid,
		Correlated: make(map[int64]float64),
		Timeseries: make(map[int64]Metric),
	}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ret, err
		}
		if len(record) != 3 {
			log.Printf("unexpected record: %v\n", record)
			continue
		}
		row1, err := strconv.ParseInt(record[0], 10, 32)
		if err != nil {
			log.Printf("failed to parse row id from %v: %v\n", record[0], err)
			continue
		}
		row2, err := strconv.ParseInt(record[1], 10, 32)
		if err != nil {
			log.Printf("failed to parse row id from %v: %v\n", record[1], err)
			continue
		}
		if row1 != tsid && row2 != tsid {
			continue
		}
		pearson, err := strconv.ParseFloat(record[2], 10)
		if err != nil {
			log.Printf("failed to parse correlation from %v: %v\n", record[2], err)
			continue
		}
		if row1 == tsid {
			ret.Correlated[row2] = pearson
			continue
		}
		if row2 == tsid {
			ret.Correlated[row1] = pearson
			continue
		}
	}
	return ret, nil
}

func (c *CorrelationExplorer) evaluateStride(reader *csv.Reader) (ClusterAssignments, error) {
	clusters := ClusterAssignments{
		Rows:          make(map[int64]int),
		Sizes:         make(map[int]int),
		nextClusterId: 0,
	}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return clusters, err
		}
		if len(record) != 3 {
			log.Printf("unexpected record: %v\n", record)
			continue
		}
		row1, err := strconv.ParseInt(record[0], 10, 32)
		if err != nil {
			log.Printf("failed to parse row id from %v: %v\n", record[0], err)
			continue
		}
		row2, err := strconv.ParseInt(record[1], 10, 32)
		if err != nil {
			log.Printf("failed to parse row id from %v: %v\n", record[1], err)
			continue
		}
		clusters.addPair(row1, row2)
	}

	for _, c := range clusters.Rows {
		count, exists := clusters.Sizes[c]
		if !exists {
			clusters.Sizes[c] = 1
		} else {
			clusters.Sizes[c] = count + 1
		}
	}
	return clusters, nil
}

func (c *CorrelationExplorer) findReadyStrides() error {
	entries, err := os.ReadDir(c.FilenameBase)
	if err != nil {
		log.Printf("Failed to get list of stride results from %s: %v\n", c.FilenameBase, err)
		return err
	}
	for _, e := range entries {
		filename := e.Name()
		var strideCounter int
		var startTime int
		var endTime int
		n, err := fmt.Sscanf(filename, "correlations_%d_%d-%d.csv", &strideCounter, &startTime, &endTime)
		if n != 3 || err != nil {
			continue
		}
		startT, err := time.Parse("2006102150405", fmt.Sprintf("%d", startTime))
		if err != nil {
			log.Printf("failed to parse unix time out of timestamp %d: %v\n", startTime, err)
			continue
		}
		endT, err := time.Parse("2006102150405", fmt.Sprintf("%d", endTime))
		if err != nil {
			log.Printf("failed to parse unix time out of timestamp %d: %v\n", endTime, err)
			continue
		}
		_, cacheHit := c.cache.strides[filename]
		if cacheHit {
			continue
		}
		c.cache.strides[filename] = Stride{
			ID:              strideCounter,
			StartTime:       startT.UTC().Unix(),
			StartTimeString: startT.UTC().Format("2006-01-02 15:04:05"),
			EndTime:         endT.UTC().Unix(),
			EndTimeString:   endT.UTC().Format("2006-01-02 15:04:05"),
			Status:          "Processing",
			Filename:        filename,
		}
		idsFilename := fmt.Sprintf("tsids_%d_%d.csv", strideCounter, startTime)
		idsFile, err := os.Open(filepath.Join(c.FilenameBase, idsFilename))
		if err != nil {
			log.Printf("missing ids file for stride %d date %d\n", strideCounter, startTime)
		} else {
			log.Printf("found ts ids file for stride %d date %d\n", strideCounter, startTime)
			stride := c.cache.strides[filename]
			stride.idsFile = idsFilename
			c.cache.strides[filename] = stride
			idsFile.Close()
		}
		log.Printf("using results from stride %d started at %d\n", strideCounter, startTime)
		file, err := os.Open(filepath.Join(c.FilenameBase, filename))
		if err != nil {
			log.Printf("failed to open file %s: %v\n", filename, err)
			stride := c.cache.strides[filename]
			stride.Status = "Error"
			c.cache.strides[filename] = stride
			continue
		}
		go func() {
			defer file.Close()
			clusters, err := c.evaluateStride(csv.NewReader(file))
			if err != nil {
				log.Printf("failed to evaluate file %s: %v\n", filename, err)
				stride := c.cache.strides[filename]
				stride.Status = "Error"
				c.cache.strides[filename] = stride
			}
			log.Printf("got %d clusters from file %s\n", len(clusters.Sizes), filename)
			stride := c.cache.strides[filename]
			stride.Clusters = clusters
			stride.Status = "Ready"
			c.cache.strides[filename] = stride
		}()
	}
	return nil
}

func (c *CorrelationExplorer) ExploreByName(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	fmt.Printf("got params: %+v\n", params)
}

func (c *CorrelationExplorer) ExploreTimeseries(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	var rowID int64
	n, err := fmt.Sscanf(params["id"][0], "%d", &rowID)
	if n != 1 || err != nil {
		http.Error(w, fmt.Sprintf("failed to parse row id out of %s\n", params["id"][0]), http.StatusBadRequest)
		return
	}
	strideKey := params["stride"][0]
	stride, have := c.cache.strides[strideKey]
	if !have {
		http.Error(w, fmt.Sprintf("Stride %s not known\n", strideKey), http.StatusBadRequest)
		return
	}
	file, err := os.Open(filepath.Join(c.FilenameBase, strideKey))
	if err != nil {
		http.Error(w, fmt.Sprintf("Missing data file for cluster %s\n", strideKey), http.StatusBadRequest)
		return
	}
	defer file.Close()
	corr, err := c.retrieveCorrelations(csv.NewReader(file), rowID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get correlation information for row %d\n", rowID), http.StatusBadRequest)
		return
	}

	file, err = os.Open(filepath.Join(c.FilenameBase, stride.idsFile))
	if err != nil {
		http.Error(w, fmt.Sprintf("Missing ids file for cluster %s\n", strideKey), http.StatusBadRequest)
		return
	}
	defer file.Close()
	rowIds := make([]int64, 0, len(corr.Correlated)+1)
	rowIds = append(rowIds, rowID)
	for row, _ := range corr.Correlated {
		{
			rowIds = append(rowIds, row)
		}
	}
	corr.Timeseries, _ = c.readTsNames(csv.NewReader(file), rowIds, stride)
	log.Printf("correlations for row %d: %+v\n", rowID, corr)
	// w.WriteHeader(http.StatusOK)
	if err := c.correlationTemplate.ExecuteTemplate(w, "correlations", corr); err != nil {
		http.Error(w, fmt.Sprintf("Failed to apply template: %v\n", err), http.StatusBadRequest)
		return
	}
}

func (c *CorrelationExplorer) ExploreCluster(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	var clusterID int
	n, err := fmt.Sscanf(params["id"][0], "%d", &clusterID)
	if n != 1 || err != nil {
		http.Error(w, fmt.Sprintf("failed to parse cluster id out of %s\n", params["id"][0]), http.StatusBadRequest)
		return
	}
	strideKey := params["stride"][0]
	stride, have := c.cache.strides[strideKey]
	if !have {
		http.Error(w, fmt.Sprintf("Stride %s not known\n", strideKey), http.StatusBadRequest)
		return
	}
	rowCount, have := stride.Clusters.Sizes[clusterID]
	if !have {
		http.Error(w, fmt.Sprintf("Stride %s does not have a cluster %d\n", strideKey, clusterID),
			http.StatusBadRequest)
		return
	}
	if rowCount == 0 {
		http.Error(w, fmt.Sprintf("Cluster %d is empty\n", clusterID), http.StatusBadRequest)
		return
	}
	file, err := os.Open(filepath.Join(c.FilenameBase, stride.idsFile))
	if err != nil {
		http.Error(w, fmt.Sprintf("Missing ids file for cluster %d\n", clusterID), http.StatusBadRequest)
		return
	}
	defer file.Close()
	rowIds := make([]int64, 0, rowCount)
	for row, cluster := range stride.Clusters.Rows {
		if cluster == clusterID {
			rowIds = append(rowIds, row)
		}
	}
	tsMetricMap, err := c.readTsNames(csv.NewReader(file), rowIds, stride)
	rows := make([]Row, 0, rowCount)
	for _, row := range rowIds {
		name, exists := tsMetricMap[row]
		if !exists {
			log.Printf("missing entry for ts id %d\n", row)
			continue
		}
		rows = append(rows, Row{
			TimeseriesID:   row,
			TimeseriesName: name,
			Filename:       strideKey,
		})
	}
	if err := c.clusterTemplate.ExecuteTemplate(w, "rows", rows); err != nil {
		http.Error(w, fmt.Sprintf("Failed to apply template: %v\n", err), http.StatusBadRequest)
		return
	}
}

func (c *CorrelationExplorer) ExploreCorrelations(w http.ResponseWriter, r *http.Request) {
	err := c.findReadyStrides()
	if err != nil {
		log.Printf("failed to find strides: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to find strides: %v\n", err), http.StatusBadRequest)
		return
	}
	// TODO: sort the map values directly.
	strides := make([]Stride, 0, len(c.cache.strides))
	for _, stride := range c.cache.strides {
		strides = append(strides, stride)
	}
	sort.Slice(strides, func(i, j int) bool { return strides[i].StartTime > strides[j].StartTime })
	if err := c.strideListTemplate.ExecuteTemplate(w, "strides", strides); err != nil {
		log.Printf("failed to execute template: %v\n", err)
		http.Error(w, fmt.Sprintf("Failed to apply template: %v\n", err), http.StatusBadRequest)
		return
	}
}
