package server

import (
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/automation"
	"github.com/yousysadmin/ihttp/internal/domain/instance"
	"github.com/yousysadmin/ihttp/internal/domain/intercept"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/protoschema"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/domain/rules"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"github.com/yousysadmin/ihttp/internal/domain/transfer"
	"github.com/yousysadmin/ihttp/internal/domain/upstreams"
)

// registerRoutes wires every route. Handlers live beside their domain in
// an endpoint.go, and this file only says which path reaches which.
func registerRoutes(mux *http.ServeMux, d Deps) {
	h := func(fn response.Handler) http.Handler { return response.Handle(fn) }

	mux.Handle("GET /api/info", h(infoHandler(d)))
	mux.Handle("GET /api/events", eventStream(d))
	mux.HandleFunc("GET /api/ca.pem", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", `attachment; filename="ihttp-ca.pem"`)
		_, _ = w.Write(d.CA.CAPEM())
	})

	p := project.Handler{Svc: d.Projects}
	mux.Handle("GET /api/projects", h(p.List))
	mux.Handle("POST /api/projects", h(p.Create))
	mux.Handle("GET /api/projects/active", h(p.Active))
	mux.Handle("POST /api/projects/close", h(p.Close))
	mux.Handle("POST /api/projects/{id}/open", h(p.Open))
	mux.Handle("DELETE /api/projects/{id}", h(p.Delete))
	mux.Handle("GET /api/project/settings", h(p.Settings))
	mux.Handle("PUT /api/project/settings/scope", h(p.PutScope))
	mux.Handle("PUT /api/project/settings/intercept", h(p.PutIntercept))
	mux.Handle("PUT /api/project/settings/request-log", h(p.PutRequestLog))
	mux.Handle("PUT /api/project/settings/views", h(p.PutViews))
	mux.Handle("PUT /api/project/settings/upstream", h(p.PutUpstream))
	mux.Handle("PUT /api/project/settings/rules", h(p.PutRules))
	mux.Handle("PUT /api/project/settings/no-decrypt", h(p.PutNoDecrypt))

	// Captured values: runtime state of the rules hook, not a setting.
	if d.Rules != nil {
		rl := rules.Handler{Svc: d.Rules}
		mux.Handle("GET /api/rules/variables", h(rl.Variables))
		mux.Handle("DELETE /api/rules/variables", h(rl.Clear))
	}

	// The instance's own settings, as against a project's.
	in := instance.Handler{Svc: d.Instance}
	mux.Handle("GET /api/settings", h(in.Settings))
	mux.Handle("PUT /api/settings/auth-headers", h(in.PutAuthHeaders))

	u := upstreams.Handler{Svc: d.Upstreams}
	mux.Handle("GET /api/upstreams", h(u.List))
	mux.Handle("POST /api/upstreams", h(u.Save))
	mux.Handle("POST /api/upstreams/test", h(u.Test))
	mux.Handle("GET /api/upstreams/{id}", h(u.Get))
	mux.Handle("DELETE /api/upstreams/{id}", h(u.Delete))

	x := transfer.Handler{Svc: d.Transfer}
	mux.Handle("GET /api/projects/{id}/export", h(x.Export))
	mux.Handle("POST /api/projects/import", h(x.Import))

	l := reqlog.Handler{Svc: d.ReqLogs}
	if d.Schemas != nil {
		l.Schemas = d.Schemas
	}

	sc := protoschema.Handler{Svc: d.Schemas}
	mux.Handle("GET /api/project/schemas", h(sc.List))
	mux.Handle("POST /api/project/schemas", h(sc.Upload))
	mux.Handle("DELETE /api/project/schemas/{id}", h(sc.Delete))

	mux.Handle("GET /api/request-logs", h(l.List))
	mux.Handle("DELETE /api/request-logs", h(l.Clear))
	mux.Handle("GET /api/request-logs/{id}", h(l.Get))
	mux.Handle("PATCH /api/request-logs/{id}", h(l.Patch))
	mux.Handle("DELETE /api/request-logs/{id}", h(l.Delete))
	mux.Handle("POST /api/request-logs/{id}/save", h(l.Save))
	mux.Handle("DELETE /api/request-logs/{id}/save", h(l.Unsave))
	mux.Handle("GET /api/request-logs/tags", h(l.Tags))
	mux.Handle("GET /api/request-logs/hosts", h(l.Hosts))
	mux.Handle("GET /api/request-logs/top", h(l.Top))
	mux.Handle("GET /api/request-logs/{id}/body/{side}", h(l.Body))
	mux.Handle("GET /api/request-logs/{id}/snippets", h(l.Snippets))
	mux.Handle("GET /api/request-logs/{id}/grpc/{side}", h(l.GRPC))
	mux.Handle("GET /api/request-logs/{id}/messages", h(l.Messages))
	mux.Handle("GET /api/request-logs/{id}/messages/{seq}", h(l.Message))
	mux.Handle("GET /api/request-logs/{id}/messages/{seq}/body", h(l.MessageBody))
	mux.Handle("GET /api/request-logs/{id}/messages/{seq}/protobuf", h(l.MessageProtobuf))
	mux.Handle("GET /api/request-logs/export.har", h(l.ExportHAR))
	mux.Handle("POST /api/request-logs/import.har", h(l.ImportHAR))

	i := intercept.Handler{Svc: d.Intercept}
	mux.Handle("GET /api/intercept/items", h(i.List))
	mux.Handle("GET /api/intercept/items/{id}", h(i.Get))
	mux.Handle("GET /api/intercept/items/{id}/body/{side}", h(i.Body))
	mux.Handle("POST /api/intercept/requests/{id}/forward", h(i.ForwardRequest))
	mux.Handle("POST /api/intercept/requests/{id}/drop", h(i.DropRequest))
	mux.Handle("POST /api/intercept/responses/{id}/forward", h(i.ForwardResponse))
	mux.Handle("POST /api/intercept/responses/{id}/drop", h(i.DropResponse))

	s := sender.Handler{Svc: d.Sender}
	mux.Handle("GET /api/sender/requests", h(s.List))
	mux.Handle("POST /api/sender/requests", h(s.Save))
	mux.Handle("DELETE /api/sender/requests", h(s.Clear))
	mux.Handle("GET /api/sender/requests/{id}", h(s.Get))
	mux.Handle("GET /api/sender/requests/{id}/body/{side}", h(s.Body))
	mux.Handle("GET /api/sender/requests/{id}/snippets", h(s.Snippets))
	mux.Handle("DELETE /api/sender/requests/{id}", h(s.Delete))
	mux.Handle("POST /api/sender/requests/{id}/send", h(s.Send))
	mux.Handle("POST /api/sender/clone/{logId}", h(s.Clone))

	f := automation.Handler{Svc: d.Automation}
	mux.Handle("GET /api/automation/jobs", h(f.List))
	mux.Handle("POST /api/automation/jobs", h(f.Save))
	mux.Handle("DELETE /api/automation/jobs", h(f.Clear))
	mux.Handle("GET /api/automation/jobs/{id}", h(f.Get))
	mux.Handle("GET /api/automation/jobs/{id}/body", h(f.TemplateBody))
	mux.Handle("DELETE /api/automation/jobs/{id}", h(f.Delete))
	mux.Handle("POST /api/automation/jobs/{id}/start", h(f.Start))
	mux.Handle("POST /api/automation/jobs/{id}/stop", h(f.Stop))
	mux.Handle("GET /api/automation/jobs/{id}/results", h(f.Results))
	mux.Handle("GET /api/automation/jobs/{id}/results/{rid}", h(f.Result))
	mux.Handle("GET /api/automation/jobs/{id}/results/{rid}/body/{side}", h(f.Body))
	mux.Handle("GET /api/automation/jobs/{id}/results/{rid}/snippets", h(f.Snippets))
	mux.Handle("GET /api/automation/jobs/{id}/export.har", h(f.ExportHAR))
	mux.Handle("POST /api/automation/clone/{logId}", h(f.Clone))

	// Anything else under /api is a JSON 404, and the rest of the space
	// is the console.
	mux.HandleFunc("/api/", notFound)
	mux.Handle("/", consoleHandler(d.Console))
}

// InfoResponse is the body of GET /api/info.
type InfoResponse struct {
	Info
	ActiveProjectID string `json:"active_project_id"`
}

func infoHandler(d Deps) response.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		response.JSON(w, http.StatusOK, InfoResponse{
			Info:            d.Info,
			ActiveProjectID: d.Projects.ActiveID(),
		})

		return nil
	}
}
