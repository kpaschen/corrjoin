package explorer

import (
	"fmt"
	"log"
	"html/template"
	"os"
	"net/http"
)

type Stride struct {
	ID int
	StartTime string
}

type CorrelationExplorer struct {
	clusterListTemplate *template.Template
        strideListTemplate *template.Template
        FilenameBase string
}

func (c *CorrelationExplorer) Initialize() error {
	t, err := template.ParseFiles("/templates/index.tmpl")
	if err != nil {
		log.Println(fmt.Sprintf("%v", err))
		return err
	}
	c.clusterListTemplate = t
	c.strideListTemplate = t
	return nil
}

func (c *CorrelationExplorer) findReadyStrides() []Stride {
	entries, err := os.ReadDir(c.FilenameBase)
	if err != nil {
		log.Printf("Failed to get list of stride results from %s: %v\n", c.FilenameBase, err)
		return nil
	}
	ret := make([]Stride, 0, len(entries))
	for _, e := range entries {
		filename := e.Name()
		var stride int
		var startTime string
		n, err := fmt.Sscanf(filename, "correlations_%d_%s.csv", &stride, &startTime)
		if n != 2 || err != nil {
			continue
		}
		ret = append(ret, Stride{ID: stride, StartTime: startTime})
	}
	return ret
}

func (c *CorrelationExplorer) ExploreCorrelations(w http.ResponseWriter, r *http.Request) {
	strides := c.findReadyStrides()
	if strides == nil {
		log.Printf("failed to find strides\n")
		return
	}
	if err := c.strideListTemplate.ExecuteTemplate(w, "strides",  strides); err != nil {
		log.Printf("failed to execute template: %v\n", err)
	}
}


