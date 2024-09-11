package explorer

import (
	"encoding/csv"
	"fmt"
	"html/templates"
	"net/http"
)

type CorrelationExplorer struct {
	template.Template clusterListTemplate
}

func (c *CorrelationExplorer) Initialize() error {
	c.clusterListTemplate, err := template.ParseFiles("./templates/index.tmpl")
	if err != nil {
		log.Println(fmt.Sprintf("%v", err))
		return err
	}
}

func (c *CorrelationExplorer) exploreCorrelations(w http.ResponseWriter, r *http.Request) {
	if err := clusterListTemplate.ExecuteTemplate(w, "clusters",  []); err != nil {
		log.Println("failed to execute template: %v\n", err)
	}
}


