package sender

import (
	"errors"
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
)

// Handler answers the sender routes.
type Handler struct {
	Svc *Service
}

// List handles GET /api/sender/requests.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	list, err := h.Svc.List(r.Context(), q.Get("search"), q.Get("only_in_scope") == "true")
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ListResponse{Requests: list})

	return nil
}

// Get handles GET /api/sender/requests/{id}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	req, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, RequestResponse{Request: req})

	return nil
}

// Snippets handles GET /api/sender/requests/{id}/snippets.
func (h Handler) Snippets(w http.ResponseWriter, r *http.Request) error {
	req, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, reqlog.Snippets(httpmsg.SnippetInput{
		Method: req.Method, URL: req.URL, Proto: req.Proto, Headers: req.Headers, Body: req.Body,
	}))

	return nil
}

// Save handles POST /api/sender/requests.
func (h Handler) Save(w http.ResponseWriter, r *http.Request) error {
	var in SaveRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	req, err := h.Svc.Save(r.Context(), in)
	if err != nil {
		return mapErr(err)
	}

	status := http.StatusOK
	if in.ID == "" {
		status = http.StatusCreated
	}

	response.JSON(w, status, RequestResponse{Request: req})

	return nil
}

// Clone handles POST /api/sender/clone/{logId}.
func (h Handler) Clone(w http.ResponseWriter, r *http.Request) error {
	req, err := h.Svc.CloneFromLog(r.Context(), r.PathValue("logId"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusCreated, RequestResponse{Request: req})

	return nil
}

// Send handles POST /api/sender/requests/{id}/send.
func (h Handler) Send(w http.ResponseWriter, r *http.Request) error {
	req, err := h.Svc.Send(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, RequestResponse{Request: req})

	return nil
}

// Body handles GET /api/sender/requests/{id}/body/{side}, the raw bytes.
func (h Handler) Body(w http.ResponseWriter, r *http.Request) error {
	req, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	switch r.PathValue("side") {
	case "request":
		reqlog.ServeBody(w, req.Headers, req.Body)
	case "response":
		if req.Response == nil {
			return response.NotFound("not sent yet")
		}

		reqlog.ServeBody(w, req.Response.Headers, req.Response.Body)
	default:
		return response.NotFound("side must be request or response")
	}

	return nil
}

// Delete handles DELETE /api/sender/requests/{id}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Clear handles DELETE /api/sender/requests.
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
		return response.NotFound("sender request not found")
	case errors.Is(err, reqlog.ErrNotFound):
		return response.NotFound("request log entry not found")
	case errors.Is(err, project.ErrNoActiveProject):
		return response.Conflict("no project is open")
	case errors.Is(err, ErrInvalidURL), errors.Is(err, ErrBadProto):
		return response.BadRequest(err.Error())
	case errors.Is(err, filter.ErrInvalid):
		return response.BadRequest(err.Error())
	}

	if se, ok := errors.AsType[*SendError](err); ok {
		return response.BadGateway(se.Err.Error())
	}

	return err
}
