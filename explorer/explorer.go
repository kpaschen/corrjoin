package explorer

import (
	"fmt"
	"log"
	"html/template"
	"net/http"
)

type CorrelationExplorer struct {
	clusterListTemplate *template.Template
}

func (c *CorrelationExplorer) Initialize() error {
	t, err := template.ParseFiles("./templates/index.tmpl")
	if err != nil {
		log.Println(fmt.Sprintf("%v", err))
		return err
	}
	c.clusterListTemplate = t
	return nil
}

func (c *CorrelationExplorer) ExploreCorrelations(w http.ResponseWriter, r *http.Request) {
	if err := c.clusterListTemplate.ExecuteTemplate(w, "clusters",  nil); err != nil {
		log.Printf("failed to execute template: %v\n", err)
	}
}


