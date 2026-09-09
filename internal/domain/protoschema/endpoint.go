package protoschema

import (
	"errors"
	"io"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/project"
)

// Handler answers the schema routes.
type Handler struct {
	Svc *Service
}

// List handles GET /api/project/schemas.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	list, err := h.Svc.List(r.Context())
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ListResponse{Schemas: list})

	return nil
}

// Upload handles POST /api/project/schemas?name=<file name>. The body is
// the file as it is.
func (h Handler) Upload(w http.ResponseWriter, r *http.Request) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxSchemaBytes))
	if err != nil {
		return mapErr(err)
	}

	sc, err := h.Svc.Upload(r.Context(), r.URL.Query().Get("name"), data)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusCreated, SchemaResponse{Schema: sc})

	return nil
}

// Delete handles DELETE /api/project/schemas/{id}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return response.NotFound("schema not found")
	case errors.Is(err, project.ErrNoActiveProject):
		return response.Conflict("no project is open")
	case errors.Is(err, ErrInvalid):
		return response.BadRequest(err.Error())
	}

	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return response.PayloadTooLarge("the schema is larger than the upload allows")
	}

	return err
}
