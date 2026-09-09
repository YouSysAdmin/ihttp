package transfer

import (
	"errors"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
)

// Handler answers the transfer routes.
type Handler struct {
	Svc *Service
}

// Export handles GET /api/projects/{id}/export, the project as a file.
// With settings_only the file carries the settings and no traffic, for
// sharing a set of rules, a scope and the filters without sharing
// anybody's captured exchanges. The two get different extensions so
// which is which is plain on disk.
func (h Handler) Export(w http.ResponseWriter, r *http.Request) error {
	settingsOnly := r.URL.Query().Has("settings_only")

	name, write, err := h.Svc.Export(r.Context(), r.PathValue("id"), settingsOnly)
	if err != nil {
		return mapErr(err)
	}

	ext := ".ihttp.json"
	if settingsOnly {
		ext = ".ihttp-config.json"
	}

	reqlog.ServeAttachment(w, name+ext, "application/json")

	// The status is gone once writing starts. A failure here reaches the
	// client as a document cut short, and the log as the reason.
	if err := write(w); err != nil {
		h.Svc.log.Warn("transfer: export cut short", "err", err)
	}

	return nil
}

// Import handles POST /api/projects/import: the body is the file, the
// answer the new project.
func (h Handler) Import(w http.ResponseWriter, r *http.Request) error {
	body := http.MaxBytesReader(w, r.Body, MaxImportBytes)

	p, err := h.Svc.Import(r.Context(), body)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusCreated, project.ProjectResponse{Project: p})

	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, project.ErrNotFound):
		return response.NotFound("project not found")
	case errors.Is(err, ErrBadEnvelope), errors.Is(err, ErrUnsupportedVersion),
		errors.Is(err, project.ErrInvalidSettings), errors.Is(err, project.ErrNameTaken):
		return response.BadRequest(err.Error())
	}

	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return response.PayloadTooLarge("the file is larger than the import allows")
	}

	return err
}
