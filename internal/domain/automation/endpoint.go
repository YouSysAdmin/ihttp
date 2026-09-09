package automation

import (
	"errors"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
)

// Handler answers the automation routes.
type Handler struct {
	Svc *Service
}

// List handles GET /api/automation/jobs.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	list, err := h.Svc.List(r.Context(), r.URL.Query().Get("search"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ListResponse{Jobs: list})

	return nil
}

// Get handles GET /api/automation/jobs/{id}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	j, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, JobResponse{Job: j})

	return nil
}

// Save handles POST /api/automation/jobs.
func (h Handler) Save(w http.ResponseWriter, r *http.Request) error {
	var in SaveJob
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	j, err := h.Svc.Save(r.Context(), in)
	if err != nil {
		return mapErr(err)
	}

	status := http.StatusOK
	if in.ID == "" {
		status = http.StatusCreated
	}

	response.JSON(w, status, JobResponse{Job: j})

	return nil
}

// Clone handles POST /api/automation/clone/{logId}.
func (h Handler) Clone(w http.ResponseWriter, r *http.Request) error {
	j, err := h.Svc.CloneFromLog(r.Context(), r.PathValue("logId"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusCreated, JobResponse{Job: j})

	return nil
}

// Start handles POST /api/automation/jobs/{id}/start.
func (h Handler) Start(w http.ResponseWriter, r *http.Request) error {
	j, err := h.Svc.Start(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, JobResponse{Job: j})

	return nil
}

// Stop handles POST /api/automation/jobs/{id}/stop.
func (h Handler) Stop(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Stop(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Results handles GET /api/automation/jobs/{id}/results.
func (h Handler) Results(w http.ResponseWriter, r *http.Request) error {
	list, err := h.Svc.Results(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ResultsResponse{Results: list})

	return nil
}

// Result handles GET /api/automation/jobs/{id}/results/{rid}.
func (h Handler) Result(w http.ResponseWriter, r *http.Request) error {
	res, err := h.Svc.Result(r.Context(), r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ResultResponse{Result: res})

	return nil
}

// Body handles GET /api/automation/jobs/{id}/results/{rid}/body/{side}, the raw
// bytes of one result's request or response.
func (h Handler) Body(w http.ResponseWriter, r *http.Request) error {
	res, err := h.Svc.Result(r.Context(), r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		return mapErr(err)
	}

	switch r.PathValue("side") {
	case "request":
		reqlog.ServeBody(w, res.ReqHeaders, res.ReqBody)
	case "response":
		if res.StatusCode == 0 {
			return response.NotFound("no response was received")
		}

		reqlog.ServeBody(w, res.Headers, res.Body)
	default:
		return response.NotFound("side must be request or response")
	}

	return nil
}

// Snippets handles GET /api/automation/jobs/{id}/results/{rid}/snippets,
// the request as sent spelled as curl and as fetch().
func (h Handler) Snippets(w http.ResponseWriter, r *http.Request) error {
	res, err := h.Svc.Result(r.Context(), r.PathValue("id"), r.PathValue("rid"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, reqlog.Snippets(httpmsg.SnippetInput{
		Method: res.ReqMethod, URL: res.ReqURL, Headers: res.ReqHeaders, Body: res.ReqBody,
	}))

	return nil
}

// ExportHAR handles GET /api/automation/jobs/{id}/export.har.
func (h Handler) ExportHAR(w http.ResponseWriter, r *http.Request) error {
	name, write, err := h.Svc.ExportHAR(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	reqlog.ServeAttachment(w, name+".har", "application/json")

	if err := write(w); err != nil {
		h.Svc.log.Error("automation: har export", "err", err)
	}

	return nil
}

// TemplateBody handles GET /api/automation/jobs/{id}/body, the raw bytes of
// the job's template body, for the binary view in the editor.
func (h Handler) TemplateBody(w http.ResponseWriter, r *http.Request) error {
	j, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	reqlog.ServeBody(w, j.Headers, j.Body)

	return nil
}

// Delete handles DELETE /api/automation/jobs/{id}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Clear handles DELETE /api/automation/jobs.
func (h Handler) Clear(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Clear(r.Context()); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return response.NotFound("automation job not found")
	case errors.Is(err, ErrResultNotFound):
		return response.NotFound("automation result not found")
	case errors.Is(err, reqlog.ErrNotFound):
		return response.NotFound("request log entry not found")
	case errors.Is(err, project.ErrNoActiveProject):
		return response.Conflict("no project is open")
	case errors.Is(err, ErrAlreadyRunning), errors.Is(err, ErrNotRunning):
		return response.Conflict(err.Error())
	case errors.Is(err, ErrInvalidURL), errors.Is(err, ErrBadProto), errors.Is(err, ErrNoPlaceholder):
		return response.BadRequest(err.Error())
	}

	if spec, ok := errors.AsType[*SpecError](err); ok {
		return response.BadRequest(spec.Error())
	}

	return err
}
