package transfer

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/domain/automation"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/protoschema"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	automationmodels "github.com/yousysadmin/ihttp/internal/models/automation"
	projectmodels "github.com/yousysadmin/ihttp/internal/models/project"
	protoschemamodels "github.com/yousysadmin/ihttp/internal/models/protoschema"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
	sendermodels "github.com/yousysadmin/ihttp/internal/models/sender"
	"github.com/yousysadmin/ihttp/pkg"
)

// Service exports and imports projects.
type Service struct {
	db       *bolt.DB
	projects *project.Service
	log      *slog.Logger
}

// NewService builds a Service over db and the project service that owns
// the list.
func NewService(db *bolt.DB, projects *project.Service, log *slog.Logger) *Service {
	return &Service{db: db, projects: projects, log: log}
}

// buckets are the per-project buckets an export carries, in file order.
var buckets = [][]byte{
	reqlog.BucketLogs,
	reqlog.BucketMessages,
	reqlog.BucketMessageIndex,
	sender.BucketRequests,
	automation.BucketJobs,
	automation.BucketResults,
	automation.BucketResultIndex,
	protoschema.BucketSchemas,
}

// importBatch is how many records go into one write transaction.
const importBatch = 500

// Export prepares the envelope of project id. name is the project's, for
// the file name. The buckets are read in one transaction as the writer
// runs, so a large log is never held in memory.
func (s *Service) Export(ctx context.Context, id string, settingsOnly bool) (name string, write func(io.Writer) error, err error) {
	p, err := s.projects.Get(ctx, id)
	if err != nil {
		return "", nil, err
	}

	p.IsActive = false

	s.log.Debug("transfer: exporting", "id", p.ID, "name", p.Name, "settings_only", settingsOnly)

	write = func(w io.Writer) error {
		enc := response.NewStreamEncoder(w)

		err := writeTokens(enc,
			jsontext.BeginObject,
			jsontext.String("format"), jsontext.String(FormatName),
			jsontext.String("version"), jsontext.Int(Version),
			jsontext.String("exported_at"), jsontext.String(time.Now().UTC().Format(time.RFC3339)),
			jsontext.String("ihttp_version"), jsontext.String(pkg.Version),
			jsontext.String("project"),
		)
		if err != nil {
			return err
		}

		if err := response.MarshalEncode(enc, p); err != nil {
			return err
		}

		if err := writeTokens(enc, jsontext.String("buckets"), jsontext.BeginObject); err != nil {
			return err
		}

		// Records are read a page at a time so a slow download does not
		// hold the database's read lock for its whole length. A
		// settings-only export writes no records at all, leaving the
		// member an empty object so the file is still an ordinary
		// project file and imports through the same path.
		names := buckets
		if settingsOnly {
			names = nil
		}

		for _, name := range names {
			if err := writeTokens(enc, jsontext.String(string(name)), jsontext.BeginArray); err != nil {
				return err
			}

			var after []byte
			for {
				page, err := s.page(ctx, id, name, after)
				if err != nil {
					return err
				}

				for _, r := range page {
					if err := writeTokens(enc, jsontext.BeginObject, jsontext.String("k"), jsontext.String(string(r.k)), jsontext.String("v")); err != nil {
						return err
					}

					if err := enc.WriteValue(r.v); err != nil {
						return err
					}

					if err := enc.WriteToken(jsontext.EndObject); err != nil {
						return err
					}
				}

				if len(page) < exportPage {
					break
				}

				after = page[len(page)-1].k
			}

			if err := enc.WriteToken(jsontext.EndArray); err != nil {
				return err
			}
		}

		return writeTokens(enc, jsontext.EndObject, jsontext.EndObject)
	}

	return p.Name, write, nil
}

// exportPage is how many records one export transaction reads.
const exportPage = 200

type record struct {
	k, v []byte
}

// page reads up to exportPage records of a bucket after key after, or
// from the first when after is nil. Nested buckets are skipped.
func (s *Service) page(ctx context.Context, id string, name, after []byte) ([]record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var out []record

	err := s.db.View(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, project.BucketData, []byte(id), name)
		if err != nil || b == nil {
			return err
		}

		c := b.Cursor()

		var k, v []byte
		if after == nil {
			k, v = c.First()
		} else {
			k, v = c.Seek(after)
			if bytes.Equal(k, after) {
				k, v = c.Next()
			}
		}

		for ; k != nil && len(out) < exportPage; k, v = c.Next() {
			if v == nil {
				continue
			}

			out = append(out, record{k: slices.Clone(k), v: slices.Clone(v)})
		}

		return nil
	})

	return out, err
}

func writeTokens(enc *jsontext.Encoder, toks ...jsontext.Token) error {
	for _, tok := range toks {
		if err := enc.WriteToken(tok); err != nil {
			return err
		}
	}

	return nil
}

// Import reads an envelope and creates a NEW project from it, with a new
// id and the file's name, suffixed when taken. Records keep their keys
// and ids and have their project id rewritten. The open project is never
// touched. A failure after records were written leaves nothing behind.
func (s *Service) Import(ctx context.Context, r io.Reader) (projectmodels.Project, error) {
	newID := ids.New()

	p, err := s.read(ctx, r, newID)
	if err != nil {
		s.discard(newID)

		return projectmodels.Project{}, err
	}

	created, err := s.projects.Import(ctx, p, newID)
	if err != nil {
		s.discard(newID)

		return projectmodels.Project{}, err
	}

	s.log.Debug("transfer: imported", "id", created.ID, "name", created.Name, "from", p.Name)

	return created, nil
}

// read walks the envelope, writing bucket records under newID as they
// come, and returns the project member.
func (s *Service) read(ctx context.Context, r io.Reader, newID string) (projectmodels.Project, error) {
	dec := jsontext.NewDecoder(r, jsontext.AllowInvalidUTF8(true))

	var (
		p              projectmodels.Project
		haveProject    bool
		formatChecked  bool
		versionChecked bool
	)

	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return p, fmt.Errorf("%w: not a JSON object", ErrBadEnvelope)
	}

	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return p, fmt.Errorf("%w: %v", ErrBadEnvelope, err)
		}

		if tok.Kind() == '}' {
			break
		}

		switch tok.String() {
		case "format":
			v, err := dec.ReadValue()
			if err != nil {
				return p, fmt.Errorf("%w: %v", ErrBadEnvelope, err)
			}

			var name string
			if err := json.Unmarshal(v, &name); err != nil || name != FormatName {
				return p, fmt.Errorf("%w: format %s", ErrBadEnvelope, v)
			}

			formatChecked = true
		case "version":
			v, err := dec.ReadValue()
			if err != nil {
				return p, fmt.Errorf("%w: %v", ErrBadEnvelope, err)
			}

			n, err := strconv.Atoi(string(v))
			if err != nil {
				return p, fmt.Errorf("%w: version %s", ErrBadEnvelope, v)
			}

			if n > Version {
				return p, fmt.Errorf("%w: version %d, this build reads %d", ErrUnsupportedVersion, n, Version)
			}

			versionChecked = true
		case "project":
			if err := json.UnmarshalDecode(dec, &p); err != nil {
				return p, fmt.Errorf("%w: project: %v", ErrBadEnvelope, err)
			}

			haveProject = true
		case "buckets":
			if !formatChecked || !versionChecked {
				return p, fmt.Errorf("%w: format and version must come before the buckets", ErrBadEnvelope)
			}

			if err := s.readBuckets(ctx, dec, newID); err != nil {
				return p, err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return p, fmt.Errorf("%w: %v", ErrBadEnvelope, err)
			}
		}
	}

	if !formatChecked || !versionChecked || !haveProject {
		return p, fmt.Errorf("%w: the file needs a format, a version and a project", ErrBadEnvelope)
	}

	return p, nil
}

func (s *Service) readBuckets(ctx context.Context, dec *jsontext.Decoder, newID string) error {
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return fmt.Errorf("%w: buckets is not an object", ErrBadEnvelope)
	}

	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBadEnvelope, err)
		}

		if tok.Kind() == '}' {
			return nil
		}

		name := []byte(tok.String())
		if !slices.ContainsFunc(buckets, func(b []byte) bool { return string(b) == string(name) }) {
			return fmt.Errorf("%w: unknown bucket %q", ErrBadEnvelope, name)
		}

		if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '[' {
			return fmt.Errorf("%w: bucket %s is not an array", ErrBadEnvelope, name)
		}

		var batch []Record

		flush := func() error {
			if len(batch) == 0 {
				return nil
			}

			err := s.put(newID, name, batch)
			batch = batch[:0]

			return err
		}

		for dec.PeekKind() != ']' {
			if err := ctx.Err(); err != nil {
				return err
			}

			var rec Record
			if err := json.UnmarshalDecode(dec, &rec); err != nil {
				return fmt.Errorf("%w: bucket %s: %v", ErrBadEnvelope, name, err)
			}

			v, err := rewrite(name, rec.V, newID)
			if err != nil {
				return fmt.Errorf("%w: bucket %s, record %q: %v", ErrBadEnvelope, name, rec.K, err)
			}

			batch = append(batch, Record{K: rec.K, V: v})

			if len(batch) == importBatch {
				if err := flush(); err != nil {
					return err
				}
			}
		}

		if _, err := dec.ReadToken(); err != nil {
			return fmt.Errorf("%w: %v", ErrBadEnvelope, err)
		}

		if err := flush(); err != nil {
			return err
		}
	}
}

// rewrite decodes a typed record to validate its shape and point it at
// the new project. The result buckets carry no project id and travel as
// they are.
func rewrite(bucket []byte, v jsontext.Value, projectID string) (jsontext.Value, error) {
	switch string(bucket) {
	case string(reqlog.BucketLogs):
		var e reqlogmodels.Entry
		if err := database.Decode(v, &e); err != nil {
			return nil, err
		}

		e.ProjectID = projectID

		return database.Encode(e)
	case string(sender.BucketRequests):
		var r sendermodels.Request
		if err := database.Decode(v, &r); err != nil {
			return nil, err
		}

		r.ProjectID = projectID

		return database.Encode(r)
	case string(automation.BucketJobs):
		var j automationmodels.Job
		if err := database.Decode(v, &j); err != nil {
			return nil, err
		}

		j.ProjectID = projectID

		return database.Encode(j)
	case string(protoschema.BucketSchemas):
		var sc protoschemamodels.Schema
		if err := database.Decode(v, &sc); err != nil {
			return nil, err
		}

		sc.ProjectID = projectID

		return database.Encode(sc)
	default:
		if err := v.Canonicalize(); err != nil {
			return nil, err
		}

		return v, nil
	}
}

func (s *Service) put(projectID string, bucket []byte, records []Record) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := database.Bucket(tx, project.BucketData, []byte(projectID), bucket)
		if err != nil {
			return err
		}

		for _, rec := range records {
			if err := b.Put([]byte(rec.K), rec.V); err != nil {
				return err
			}
		}

		return nil
	})
}

// discard drops whatever an import wrote under id. Best effort: the
// import already failed and this is the cleanup.
func (s *Service) discard(id string) {
	_ = s.db.Update(func(tx *bolt.Tx) error {
		data, err := database.Bucket(tx, project.BucketData)
		if err != nil {
			return err
		}

		if data.Bucket([]byte(id)) == nil {
			return nil
		}

		return data.DeleteBucket([]byte(id))
	})
}
