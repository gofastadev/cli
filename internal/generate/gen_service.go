package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenSvcInterface writes the service interface file for the scaffolded resource.
func GenSvcInterface(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/services/interfaces/%s_service.go", d.SnakeName), "svc_iface", templates.SvcInterface, d)
}

// GenSvc writes the service implementation file for the scaffolded resource.
func GenSvc(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/services/%s.service.go", d.SnakeName), "svc", templates.Svc, d)
}
