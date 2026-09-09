package protoschema

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/protoschema"
)

// Service stores schemas and resolves methods against them.
type Service struct {
	store    *Store
	projects *project.Service

	// registry caches the merged descriptor registry of the project it
	// was built for. Any write drops it.
	mu        sync.Mutex
	regProj   string
	regBuilt  bool
	registry  *protoregistry.Files
	regFailed error
}

// NewService builds a Service.
func NewService(store *Store, projects *project.Service) *Service {
	return &Service{store: store, projects: projects}
}

// List returns the open project's schemas.
func (s *Service) List(ctx context.Context) ([]protoschema.Summary, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, project.ErrNoActiveProject
	}

	all, err := s.store.List(ctx, active.Project.ID)
	if err != nil {
		return nil, err
	}

	out := make([]protoschema.Summary, 0, len(all))
	for _, sc := range all {
		out = append(out, sc.Summarize())
	}

	return out, nil
}

// Upload takes a .proto source or a descriptor set named name. A .proto
// is compiled with the project's other .proto uploads and the well-known
// types as imports, and stored with everything it needs.
func (s *Service) Upload(ctx context.Context, name string, data []byte) (protoschema.Summary, error) {
	active := s.projects.Active()
	if active == nil {
		return protoschema.Summary{}, project.ErrNoActiveProject
	}

	name = strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." {
		name = "schema"
	}

	existing, err := s.store.List(ctx, active.Project.ID)
	if err != nil {
		return protoschema.Summary{}, err
	}

	sc := protoschema.Schema{
		ID:         ids.New(),
		ProjectID:  active.Project.ID,
		Name:       name,
		UploadedAt: time.Now(),
		Size:       len(data),
	}

	var set *descriptorpb.FileDescriptorSet

	if strings.HasSuffix(strings.ToLower(name), ".proto") {
		sc.Kind = protoschema.KindProto
		sc.Source = data

		set, err = compile(ctx, name, data, existing)
		if err != nil {
			return protoschema.Summary{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	} else {
		sc.Kind = protoschema.KindDescriptorSet

		set = &descriptorpb.FileDescriptorSet{}
		if err := proto.Unmarshal(data, set); err != nil || len(set.File) == 0 {
			return protoschema.Summary{}, fmt.Errorf("%w: not a FileDescriptorSet", ErrInvalid)
		}

		if _, err := protodesc.NewFiles(set); err != nil {
			return protoschema.Summary{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}

	sc.Set, err = proto.Marshal(set)
	if err != nil {
		return protoschema.Summary{}, err
	}

	sc.Files, sc.Services = describe(set, sc.Kind == protoschema.KindProto, name)

	// A file uploaded again replaces its earlier self, in one write.
	var replaces []string
	for _, old := range existing {
		if old.Name == sc.Name {
			replaces = append(replaces, old.ID)
		}
	}

	if err := s.store.Replace(ctx, replaces, sc); err != nil {
		return protoschema.Summary{}, err
	}

	s.invalidate()

	return sc.Summarize(), nil
}

// Delete removes one schema of the open project.
func (s *Service) Delete(ctx context.Context, id string) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	if err := s.store.Delete(ctx, active.Project.ID, id); err != nil {
		return err
	}

	s.invalidate()

	return nil
}

// Method answers the input and output message of a gRPC path such as
// /pkg.Service/Method against the open project's schemas. ok is false
// when no schema knows the service.
func (s *Service) Method(ctx context.Context, fullMethod string) (in, out protoreflect.MessageDescriptor, ok bool) {
	active := s.projects.Active()
	if active == nil {
		return nil, nil, false
	}

	files, err := s.files(ctx, active.Project.ID)
	if err != nil || files == nil {
		return nil, nil, false
	}

	service, method, _ := strings.Cut(strings.TrimPrefix(fullMethod, "/"), "/")

	d, err := files.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, nil, false
	}

	sd, isService := d.(protoreflect.ServiceDescriptor)
	if !isService {
		return nil, nil, false
	}

	md := sd.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		return nil, nil, false
	}

	return md.Input(), md.Output(), true
}

// files builds, or returns the cached, registry over every schema of the
// project. Files present in several sets are taken once.
func (s *Service) files(ctx context.Context, projectID string) (*protoregistry.Files, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.regProj == projectID && s.regBuilt {
		return s.registry, s.regFailed
	}

	all, err := s.store.List(ctx, projectID)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	merged := &descriptorpb.FileDescriptorSet{}

	for _, sc := range all {
		set := &descriptorpb.FileDescriptorSet{}
		if err := proto.Unmarshal(sc.Set, set); err != nil {
			continue
		}

		for _, f := range set.File {
			if seen[f.GetName()] {
				continue
			}

			seen[f.GetName()] = true
			merged.File = append(merged.File, f)
		}
	}

	s.regProj = projectID
	s.regBuilt = true
	s.registry = nil
	s.regFailed = nil

	if len(merged.File) == 0 {
		return nil, nil
	}

	files, err := protodesc.NewFiles(merged)
	if err != nil {
		s.regFailed = err

		return nil, err
	}

	s.registry = files

	return files, nil
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.regProj = ""
	s.regBuilt = false
	s.registry = nil
	s.regFailed = nil
	s.mu.Unlock()
}

// compile turns one .proto into a descriptor set with its imports. The
// other .proto uploads of the project resolve imports by their name, or
// by their base name when the import carries a path they were not
// uploaded with. The well-known types come from protocompile.
func compile(ctx context.Context, name string, source []byte, others []protoschema.Schema) (*descriptorpb.FileDescriptorSet, error) {
	sources := map[string][]byte{name: source}
	byBase := map[string][]byte{}

	for _, sc := range others {
		if sc.Kind == protoschema.KindProto && sc.Name != name {
			sources[sc.Name] = sc.Source
			byBase[path.Base(sc.Name)] = sc.Source
		}
	}

	resolver := &protocompile.SourceResolver{
		Accessor: func(p string) (io.ReadCloser, error) {
			if src, ok := sources[p]; ok {
				return io.NopCloser(bytes.NewReader(src)), nil
			}

			if src, ok := byBase[path.Base(p)]; ok {
				return io.NopCloser(bytes.NewReader(src)), nil
			}

			return nil, fmt.Errorf("import %q is not among the uploaded files", p)
		},
	}

	compiler := protocompile.Compiler{
		Resolver:       protocompile.WithStandardImports(resolver),
		SourceInfoMode: protocompile.SourceInfoNone,
	}

	files, err := compiler.Compile(ctx, name)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, errors.New("nothing compiled")
	}

	set := &descriptorpb.FileDescriptorSet{}
	seen := map[string]bool{}

	var add func(fd protoreflect.FileDescriptor)
	add = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}

		seen[fd.Path()] = true

		imports := fd.Imports()
		for i := range imports.Len() {
			add(imports.Get(i).FileDescriptor)
		}

		set.File = append(set.File, protodesc.ToFileDescriptorProto(fd))
	}

	add(files[0])

	return set, nil
}

// describe lists the files a set carries and the services the upload
// itself declares. For a .proto that is the one file, for a set every
// file in it.
func describe(set *descriptorpb.FileDescriptorSet, protoOnly bool, name string) (files, services []string) {
	files = []string{}
	services = []string{}

	for _, f := range set.File {
		files = append(files, f.GetName())

		if protoOnly && f.GetName() != name {
			continue
		}

		for _, svc := range f.Service {
			full := svc.GetName()
			if pkg := f.GetPackage(); pkg != "" {
				full = pkg + "." + full
			}

			services = append(services, full)
		}
	}

	slices.Sort(services)

	return files, services
}
