package protoschema

import "github.com/yousysadmin/ihttp/internal/models/protoschema"

// ListResponse is the body of GET /api/project/schemas.
type ListResponse struct {
	Schemas []protoschema.Summary `json:"schemas"`
}

// SchemaResponse wraps one schema summary.
type SchemaResponse struct {
	Schema protoschema.Summary `json:"schema"`
}
