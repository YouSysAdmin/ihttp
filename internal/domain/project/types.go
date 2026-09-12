package project

import "github.com/yousysadmin/ihttp/internal/models/project"

// CreateRequest is the body of POST /api/projects.
type CreateRequest struct {
	Name string `json:"name"`
}

// ListResponse is the body of GET /api/projects.
type ListResponse struct {
	Projects []project.Project `json:"projects"`
}

// ProjectResponse wraps one project.
type ProjectResponse struct {
	Project project.Project `json:"project"`
}

// ActiveResponse is the body of GET /api/projects/active. Project is
// null when nothing is open, which is an ordinary state and not a 404.
type ActiveResponse struct {
	Project *project.Project `json:"project"`
}

// SettingsResponse is the body every settings PUT answers with.
type SettingsResponse struct {
	Settings project.Settings `json:"settings"`
}

// ScopeRequest is the body of PUT /api/project/settings/scope.
type ScopeRequest struct {
	Rules []project.ScopeRule `json:"rules"`
}

// RulesRequest is the body of PUT /api/project/settings/rules.
type RulesRequest struct {
	Rules []project.Rule `json:"rules"`
}

// ViewsRequest is the body of PUT /api/project/settings/views.
type ViewsRequest struct {
	Views []project.View `json:"views"`
}

// NoDecryptRequest is the body of PUT /api/project/settings/no-decrypt:
// host globs relayed without being decrypted.
type NoDecryptRequest struct {
	Hosts []string `json:"hosts"`
}

// UpstreamRequest is the body of PUT /api/project/settings/upstream: a
// reference, never a URL.
type UpstreamRequest struct {
	Upstream string `json:"upstream"`
}

// HostOverridesRequest is the body of PUT
// /api/project/settings/host-overrides: where names are dialled.
type HostOverridesRequest struct {
	Overrides []project.HostOverride `json:"overrides"`
}
