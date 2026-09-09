package transfer

import (
	"encoding/json/jsontext"
	"time"

	"github.com/yousysadmin/ihttp/internal/models/project"
)

// Envelope is the file's shape. Export writes the members in this
// order and Import reads them in this order: format and version come
// before the buckets, so a file that is not ours is refused before a
// record is written.
type Envelope struct {
	Format       string              `json:"format"`
	Version      int                 `json:"version"`
	ExportedAt   time.Time           `json:"exported_at"`
	IHTTPVersion string              `json:"ihttp_version"`
	Project      project.Project     `json:"project"`
	Buckets      map[string][]Record `json:"buckets"`
}

// Record is one stored key and its value as it sits in the database.
// Keys travel too, since a bucket may key its records by something other
// than the record's id.
type Record struct {
	K string         `json:"k"`
	V jsontext.Value `json:"v"`
}
