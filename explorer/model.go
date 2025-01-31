package explorer

// Stride collects metadata about a stride.
type Stride struct {
	ID              int
	StartTime       int64
	EndTime         int64
	StartTimeString string
	EndTimeString   string
	Status          string
	Filename        string
}

type SubgraphMemberships struct {
	// Rows maps timeseries ids to subgraph ids
	Rows map[int64]int
	// Sizes holds the size of each subgraph
	Sizes          map[int]int
	nextSubgraphId int
}
