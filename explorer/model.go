package explorer

// Stride collects metadata about a stride.
type Stride struct {
        ID        int
        StartTime int64
        EndTime int64
        StartTimeString string
        EndTimeString string
        Clusters ClusterAssignments
        Status string
        Filename string
        idsFile string
}

// ClusterAssignments summarizes a cluster.
type ClusterAssignments struct {
        // Rows maps timeseries ids to cluster ids
        Rows          map[int64]int
        // Sizes holds the size of each cluster
        Sizes map[int]int
        nextClusterId int

        // TODO: a map of clusterid -> cluster name
        // TODO: a graph representation of each cluster?
}

// Row is actually a Timeseries
type Row struct {
        TimeseriesID int64
        TimeseriesName Metric
        Filename string
}

// Correlations are the correlation results for one Row (timeseries) and one
// stride. Correlated contains the other timeseries that this one is correlated
// with, and Timeseries contains the names and urls of those timeseries.
type Correlations struct {
        Row int64
        Correlated map[int64]float64
        Timeseries map[int64]Metric
        strideKey string
}

// A Metric actually a prometheus timeseries spec
type Metric struct {
        empty bool
        Data map[string]interface{}
        PrometheusGraphURL string
}

