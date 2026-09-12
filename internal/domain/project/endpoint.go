package project

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/models/project"
)

// Handler answers the project routes.
type Handler struct {
	Svc *Service
}

// List handles GET /api/projects.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	list, err := h.Svc.List(r.Context())
	if err != nil {
		return err
	}

	response.JSON(w, http.StatusOK, ListResponse{Projects: list})

	return nil
}

// Create handles POST /api/projects.
func (h Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var in CreateRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	p, err := h.Svc.Create(r.Context(), in.Name)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusCreated, ProjectResponse{Project: p})

	return nil
}

// Active handles GET /api/projects/active.
func (h Handler) Active(w http.ResponseWriter, r *http.Request) error {
	var out ActiveResponse
	if a := h.Svc.Active(); a != nil {
		p := a.Project
		out.Project = &p
	}

	response.JSON(w, http.StatusOK, out)

	return nil
}

// Open handles POST /api/projects/{id}/open.
func (h Handler) Open(w http.ResponseWriter, r *http.Request) error {
	p, err := h.Svc.Open(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ProjectResponse{Project: p})

	return nil
}

// Close handles POST /api/projects/close.
func (h Handler) Close(w http.ResponseWriter, r *http.Request) error {
	h.Svc.Close()
	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Delete handles DELETE /api/projects/{id}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Settings handles GET /api/project/settings for the open project.
func (h Handler) Settings(w http.ResponseWriter, r *http.Request) error {
	a := h.Svc.Active()
	if a == nil {
		return mapErr(ErrNoActiveProject)
	}

	response.JSON(w, http.StatusOK, SettingsResponse{Settings: a.Project.Settings})

	return nil
}

// PutScope handles PUT /api/project/settings/scope.
func (h Handler) PutScope(w http.ResponseWriter, r *http.Request) error {
	var in ScopeRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.Scope = in.Rules

		return nil
	})
}

// PutIntercept handles PUT /api/project/settings/intercept.
func (h Handler) PutIntercept(w http.ResponseWriter, r *http.Request) error {
	var in project.InterceptSettings
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.Intercept = in

		return nil
	})
}

// PutRules handles PUT /api/project/settings/rules.
func (h Handler) PutRules(w http.ResponseWriter, r *http.Request) error {
	var in RulesRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.Rules = in.Rules

		return nil
	})
}

// PutRequestLog handles PUT /api/project/settings/request-log.
func (h Handler) PutRequestLog(w http.ResponseWriter, r *http.Request) error {
	var in project.RequestLogSettings
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.RequestLog = in

		return nil
	})
}

// PutViews handles PUT /api/project/settings/views: the named ways of
// looking at the log. Ids are minted for views that arrive without one,
// so the console can send a new view without inventing an id.
func (h Handler) PutViews(w http.ResponseWriter, r *http.Request) error {
	var in ViewsRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	for i := range in.Views {
		in.Views[i].Name = strings.TrimSpace(in.Views[i].Name)
		in.Views[i].Query = strings.TrimSpace(in.Views[i].Query)

		if in.Views[i].ID == "" {
			in.Views[i].ID = ids.New()
		}
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.Views = in.Views

		return nil
	})
}

// PutUpstream handles PUT /api/project/settings/upstream: which of the
// instance's proxies this project goes out through. A reference, so an
// export of this project carries no credentials of ours.
func (h Handler) PutUpstream(w http.ResponseWriter, r *http.Request) error {
	var in UpstreamRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.Upstream = strings.TrimSpace(in.Upstream)

		return nil
	})
}

// PutNoDecrypt handles PUT /api/project/settings/no-decrypt: the hosts
// this project relays without looking inside. Blank lines are dropped
// here rather than stored, since a list of hosts with holes in it is
// only ever a typo.
func (h Handler) PutNoDecrypt(w http.ResponseWriter, r *http.Request) error {
	var in NoDecryptRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	hosts := make([]string, 0, len(in.Hosts))

	for _, host := range in.Hosts {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.NoDecrypt = hosts

		return nil
	})
}

// PutHostOverrides handles PUT /api/project/settings/host-overrides:
// where this project dials a name. A row with no host or no address is
// dropped here rather than stored, since half an override is only ever
// a half-finished edit.
func (h Handler) PutHostOverrides(w http.ResponseWriter, r *http.Request) error {
	var in HostOverridesRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	overrides := make([]project.HostOverride, 0, len(in.Overrides))

	for _, o := range in.Overrides {
		o.Host = strings.TrimSpace(o.Host)
		o.Address = strings.TrimSpace(o.Address)

		if o.Host == "" || o.Address == "" {
			continue
		}

		overrides = append(overrides, o)
	}

	return h.update(w, r, func(s *project.Settings) error {
		s.HostOverrides = overrides

		return nil
	})
}

func (h Handler) update(w http.ResponseWriter, r *http.Request, change func(*project.Settings) error) error {
	p, err := h.Svc.UpdateSettings(r.Context(), change)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, SettingsResponse{Settings: p.Settings})

	return nil
}

// mapErr turns the package's refusals into statuses. A settings document
// that does not compile is reported with the compiler's message, since
// that message names the rule or filter at fault.
func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return response.NotFound("project not found")
	case errors.Is(err, ErrNoActiveProject):
		return response.Conflict("no project is open")
	case errors.Is(err, ErrActive):
		return response.Conflict("close the project before deleting it")
	case errors.Is(err, ErrNameTaken), errors.Is(err, ErrInvalidName):
		return response.BadRequest(err.Error())
	}

	if _, ok := errors.AsType[*response.StatusError](err); ok {
		return err
	}

	// Settings that do not compile carry a message a person can act on.
	if errors.Is(err, ErrInvalidSettings) {
		return response.BadRequest(strings.TrimPrefix(err.Error(), ErrInvalidSettings.Error()+": "))
	}

	return err
}
