// Package protoschema keeps the gRPC schemas of a project and answers
// which message a method takes and returns, so a gRPC body in the log
// reads as JSON with field names. A .proto is compiled in-process with
// protocompile, a descriptor set is taken as protoc produced it.
package protoschema

import "errors"

// The refusals a caller can act on.
var (
	ErrNotFound = errors.New("protoschema: schema not found")
	ErrInvalid  = errors.New("protoschema: not a .proto or a descriptor set")
)

// BucketSchemas is the per-project bucket name under project.BucketData.
var BucketSchemas = []byte("grpc_schemas")

// MaxSchemaBytes bounds one upload.
const MaxSchemaBytes = 16 << 20
