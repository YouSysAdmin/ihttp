package instance

import instancemodels "github.com/yousysadmin/ihttp/internal/models/instance"

// SettingsResponse is the body of GET /api/settings and of every write
// that changes it, so a form always has what was actually stored.
type SettingsResponse struct {
	Settings instancemodels.Settings `json:"settings"`
}

// AuthHeadersRequest is the body of PUT /api/settings/auth-headers. The
// list is sent whole: the form edits it whole.
type AuthHeadersRequest struct {
	AuthHeaders []string `json:"auth_headers"`
}
