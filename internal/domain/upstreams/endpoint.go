package upstreams

import (
	"errors"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/core/upstream"
	upstreammodels "github.com/yousysadmin/ihttp/internal/models/upstream"
)

// Handler answers the upstream-proxy routes.
type Handler struct {
	Svc *Service
}

// ListResponse is the body of GET /api/upstreams. Summaries only: a
// credential never reaches a list.
type ListResponse struct {
	Servers []upstreammodels.Summary `json:"servers"`
}

// ServerResponse is one server WITH its URL, for the editor.
type ServerResponse struct {
	Server upstreammodels.Server `json:"server"`
}

// TestRequest is the body of POST /api/upstreams/test. An empty id
// tests the instance default, and an empty target uses the default target.
type TestRequest struct {
	ID     string `json:"id,omitzero"`
	Target string `json:"target,omitzero"`
}

// TestResponse is what the test found. Error is what went wrong, empty
// when nothing did - a proxy that refuses is a result, not a 500.
type TestResponse struct {
	Result upstream.Result `json:"result"`
	Error  string          `json:"error,omitempty"`
}

// List handles GET /api/upstreams.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	servers, err := h.Svc.List(r.Context())
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ListResponse{Servers: servers})

	return nil
}

// Get handles GET /api/upstreams/{id}. This is the ONE place the stored
// URL is served, credentials and all, because it is what the editor
// needs to show what it is editing.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	srv, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ServerResponse{Server: srv})

	return nil
}

// Save handles POST /api/upstreams, and an id in the body is an edit.
func (h Handler) Save(w http.ResponseWriter, r *http.Request) error {
	var in SaveRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	srv, err := h.Svc.Save(r.Context(), in)
	if err != nil {
		return mapErr(err)
	}

	status := http.StatusOK
	if in.ID == "" {
		status = http.StatusCreated
	}

	response.JSON(w, status, ServerResponse{Server: srv})

	return nil
}

// Delete handles DELETE /api/upstreams/{id}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Test handles POST /api/upstreams/test.
func (h Handler) Test(w http.ResponseWriter, r *http.Request) error {
	var in TestRequest
	if r.ContentLength != 0 {
		if err := response.Decode(r, &in); err != nil {
			return err
		}
	}

	res, _, err := h.Svc.Test(r.Context(), in.ID, in.Target)
	if err != nil {
		if errors.Is(err, upstream.ErrNotConfigured) {
			return response.BadRequest("no upstream proxy is configured - start ihttp with --upstream-proxy, or pick one from the list")
		}

		if errors.Is(err, ErrNotFound) {
			return mapErr(err)
		}

		response.JSON(w, http.StatusOK, TestResponse{Result: res, Error: err.Error()})

		return nil
	}

	response.JSON(w, http.StatusOK, TestResponse{Result: res})

	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return response.NotFound("upstream proxy not found")
	case errors.Is(err, ErrInvalidName), errors.Is(err, ErrNameTaken),
		errors.Is(err, ErrTooMany), errors.Is(err, ErrInvalidConfig):
		// The operator's to fix, and the message names what is wrong.
		return response.BadRequest(err.Error())
	}

	return err
}
