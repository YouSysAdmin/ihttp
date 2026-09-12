// Package instance is the stored shape of what this machine is set to,
// as against what a project is set to.
package instance

// Settings is the one document beside the projects: the choices that
// belong to the instance and hold whichever project is open.
type Settings struct {
	// AuthHeaders are header names to read as carrying a credential, ON
	// TOP of the ones the console already knows. The built-in list is
	// the cross-platform spellings - Authorization, X-Api-Key,
	// X-Csrf-Token - and a house header like X-Acme-Token is named here
	// once instead of being looked for by eye in every request.
	//
	// Instance state rather than a project's, because it is a fact
	// about how the person at this machine reads traffic: it holds for
	// every project and an export carries no part of it.
	AuthHeaders []string `json:"auth_headers,omitempty"`
}
