package intercept

import (
	"errors"
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
)

// Handler answers the intercept routes.
type Handler struct {
	Svc *Service
}

// List handles GET /api/intercept/items.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	response.JSON(w, http.StatusOK, ListResponse{Items: h.Svc.Items()})

	return nil
}

// Get handles GET /api/intercept/items/{id}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	item, err := h.Svc.Item(r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ItemResponse{Item: item})

	return nil
}

// Body handles GET /api/intercept/items/{id}/body/{side}, the raw bytes
// of a waiting item.
func (h Handler) Body(w http.ResponseWriter, r *http.Request) error {
	item, err := h.Svc.Item(r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	switch r.PathValue("side") {
	case "request":
		reqlog.ServeBody(w, item.Request.Headers, item.Request.Body)
	case "response":
		if item.Response == nil {
			return response.NotFound("this item is a request")
		}

		reqlog.ServeBody(w, item.Response.Headers, item.Response.Body)
	default:
		return response.NotFound("side must be request or response")
	}

	return nil
}

// ForwardRequest handles POST /api/intercept/requests/{id}/forward. An
// empty body forwards the request as it arrived.
func (h Handler) ForwardRequest(w http.ResponseWriter, r *http.Request) error {
	var in *ForwardRequest

	if r.ContentLength != 0 {
		in = new(ForwardRequest)
		if err := response.Decode(r, in); err != nil {
			return err
		}
	}

	if err := h.Svc.ForwardRequest(r.PathValue("id"), in); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// DropRequest handles POST /api/intercept/requests/{id}/drop.
func (h Handler) DropRequest(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.DropRequest(r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// ForwardResponse handles POST /api/intercept/responses/{id}/forward.
func (h Handler) ForwardResponse(w http.ResponseWriter, r *http.Request) error {
	var in *ForwardResponse

	if r.ContentLength != 0 {
		in = new(ForwardResponse)
		if err := response.Decode(r, in); err != nil {
			return err
		}
	}

	if err := h.Svc.ForwardResponse(r.PathValue("id"), in); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// DropResponse handles POST /api/intercept/responses/{id}/drop.
func (h Handler) DropResponse(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.DropResponse(r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return response.NotFound("nothing is waiting under that id")
	case errors.Is(err, ErrGone):
		return response.Conflict("the client gave up before you answered")
	case errors.Is(err, ErrBadForward):
		return response.BadRequest(err.Error())
	case errors.Is(err, filter.ErrInvalid):
		return response.BadRequest(err.Error())
	}

	return err
}
