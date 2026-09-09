// Package protoschema is the stored shape of a gRPC schema a project
// holds: a .proto file or a compiled descriptor set, kept so gRPC bodies
// in the log can be read with field names instead of numbers.
package protoschema

import "time"

// Kind says what was uploaded.
type Kind string

// The kinds.
const (
	// KindProto is a .proto source, compiled here.
	KindProto Kind = "proto"

	// KindDescriptorSet is a serialized FileDescriptorSet, the output of
	// protoc --descriptor_set_out.
	KindDescriptorSet Kind = "descriptor_set"
)

// Schema is one upload. Set is the compiled FileDescriptorSet with every
// import the file needs, so a registry can be built from Sets alone.
// Source is the .proto text of a KindProto upload, kept so a later upload
// can import it.
type Schema struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Name       string    `json:"name"`
	Kind       Kind      `json:"kind"`
	UploadedAt time.Time `json:"uploaded_at"`
	Size       int       `json:"size"`
	Files      []string  `json:"files"`
	Services   []string  `json:"services"`
	Source     []byte    `json:"source,omitempty"`
	Set        []byte    `json:"set"`
}

// Summary is a Schema without its bytes, for the list.
type Summary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       Kind      `json:"kind"`
	UploadedAt time.Time `json:"uploaded_at"`
	Size       int       `json:"size"`
	Files      []string  `json:"files"`
	Services   []string  `json:"services"`
}

// Summarize projects a Schema onto its list row.
func (s Schema) Summarize() Summary {
	return Summary{
		ID: s.ID, Name: s.Name, Kind: s.Kind, UploadedAt: s.UploadedAt, Size: s.Size,
		Files: s.Files, Services: s.Services,
	}
}
