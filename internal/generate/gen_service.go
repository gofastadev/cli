package generate

import (
	"github.com/gofastadev/cli/internal/generate/templates"
)

// GenSvcInterface writes the service interface file for the scaffolded resource.
func GenSvcInterface(d ScaffoldData) error {
	return WriteTemplate(d.L().SvcIfaceFile(d.SnakeName), "svc_iface", templates.SvcInterface, d)
}

// GenSvc writes the service implementation file for the scaffolded resource.
func GenSvc(d ScaffoldData) error {
	return WriteTemplate(d.L().SvcImplFile(d.SnakeName), "svc", templates.Svc, d)
}
