package instance

import (
	"errors"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/response"
)

// Handler answers the instance settings routes.
type Handler struct {
	Svc *Service
}

// Settings handles GET /api/settings.
func (h Handler) Settings(w http.ResponseWriter, r *http.Request) error {
	set, err := h.Svc.Get(r.Context())
	if err != nil {
		return err
	}

	response.JSON(w, http.StatusOK, SettingsResponse{Settings: set})

	return nil
}

// PutAuthHeaders handles PUT /api/settings/auth-headers.
func (h Handler) PutAuthHeaders(w http.ResponseWriter, r *http.Request) error {
	var in AuthHeadersRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	set, err := h.Svc.SetAuthHeaders(r.Context(), in.AuthHeaders)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, SettingsResponse{Settings: set})

	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidHeader), errors.Is(err, ErrTooMany):
		// The operator's to fix, and the message names what is wrong.
		return response.BadRequest(err.Error())
	}

	return err
}
