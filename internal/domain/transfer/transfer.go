// Package transfer moves a project between instances as one file: every
// record under the project's data bucket, plus the project itself, in a
// JSON envelope. Records travel in their STORED form, so an export
// decodes nothing and an import writes what it reads.
package transfer

import "errors"

// FormatName names the envelope, the first member of the file.
const FormatName = "ihttp-project"

// Version is the envelope version this build writes and reads.
const Version = 1

// MaxImportBytes bounds an import body.
const MaxImportBytes = 2 << 30

// The refusals a caller can act on.
var (
	ErrBadEnvelope        = errors.New("transfer: not an ihttp project file")
	ErrUnsupportedVersion = errors.New("transfer: the file is from a newer ihttp")
)
