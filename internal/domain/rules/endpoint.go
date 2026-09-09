package rules

import (
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/response"
)

// Handler serves the captured values. There is no endpoint for the
// rules themselves: a rule is a project setting, edited through
// PUT /api/project/settings/rules like the rest of them.
type Handler struct {
	Svc *Service
}

// VariablesResponse is the body of GET /api/rules/variables.
type VariablesResponse struct {
	Variables []Var `json:"variables"`
}

// Variables handles GET /api/rules/variables: what the capture rules
// have read so far.
//
// The values are shown as they are, tokens and all. This is a local
// tool reading its own operator's traffic, and a captured value that
// cannot be seen cannot be debugged - the alternative is a rule that
// silently sends the wrong thing.
func (h Handler) Variables(w http.ResponseWriter, r *http.Request) error {
	vars := h.Svc.Variables()
	if vars == nil {
		vars = []Var{}
	}

	response.JSON(w, http.StatusOK, VariablesResponse{Variables: vars})

	return nil
}

// Clear handles DELETE /api/rules/variables: forget them, for a value
// that has gone stale - an expired token being the reason this exists.
func (h Handler) Clear(w http.ResponseWriter, r *http.Request) error {
	h.Svc.ClearVariables()
	w.WriteHeader(http.StatusNoContent)

	return nil
}
