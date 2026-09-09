// Package project owns the list of projects and the ONE that is open.
//
// The open project is process state, not a row: the proxy consults it
// on every exchange, so it lives in an atomic pointer to an immutable
// snapshot that is rebuilt whenever the project or its settings change.
// Everything else - the request log, the intercept queue, the sender -
// asks this package which project is active and what its compiled
// settings say, and never keeps a copy of its own.
package project

import "errors"

// The refusals a caller can act on. The endpoint maps each to a status.
var (
	ErrNoActiveProject = errors.New("project: no project is open")
	ErrNotFound        = errors.New("project: not found")
	ErrNameTaken       = errors.New("project: a project with that name exists")
	ErrInvalidName     = errors.New("project: name must be 1 to 64 printable characters")
	ErrActive          = errors.New("project: the open project cannot be deleted")
	ErrInvalidSettings = errors.New("project: settings do not compile")
)
