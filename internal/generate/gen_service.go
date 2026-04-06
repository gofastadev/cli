package generate

import (
	"fmt"

	"github.com/gofastadev/cli/internal/generate/templates"
)

func GenSvcInterface(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/services/interfaces/%s_service.go", d.SnakeName), "svc_iface", templates.SvcInterface, d)
}

func GenSvc(d ScaffoldData) error {
	return WriteTemplate(fmt.Sprintf("app/services/%s.service.go", d.SnakeName), "svc", templates.Svc, d)
}
