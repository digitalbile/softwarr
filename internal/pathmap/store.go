package pathmap

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/digitalbile/softwarr/internal/db/dbq"
)

var ErrNotFound = errors.New("That path mapping doesn't exist.")

// inputError is a validation failure that is safe to show to the admin.
type inputError struct{ msg string }

func (e *inputError) Error() string { return e.msg }

func IsInputError(err error) bool {
	var ie *inputError
	return errors.As(err, &ie)
}

// Input is the body for creating or updating a mapping.
type Input struct {
	Host       string `json:"host"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
}

// Store reads and writes the saved mappings.
type Store struct{ q *dbq.Queries }

func NewStore(q *dbq.Queries) *Store { return &Store{q: q} }

func (s *Store) List(ctx context.Context) ([]Mapping, error) { return Load(ctx, s.q) }

func (s *Store) Create(ctx context.Context, in Input) (Mapping, error) {
	in, err := validate(in)
	if err != nil {
		return Mapping{}, err
	}
	row, err := s.q.CreatePathMapping(ctx, dbq.CreatePathMappingParams{Host: in.Host, RemotePath: in.RemotePath, LocalPath: in.LocalPath})
	if err != nil {
		return Mapping{}, err
	}
	return toMapping(row), nil
}

func (s *Store) Update(ctx context.Context, id int64, in Input) (Mapping, error) {
	in, err := validate(in)
	if err != nil {
		return Mapping{}, err
	}
	row, err := s.q.UpdatePathMapping(ctx, dbq.UpdatePathMappingParams{Host: in.Host, RemotePath: in.RemotePath, LocalPath: in.LocalPath, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return Mapping{}, ErrNotFound
	}
	if err != nil {
		return Mapping{}, err
	}
	return toMapping(row), nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	n, err := s.q.DeletePathMapping(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func toMapping(r dbq.PathMapping) Mapping {
	return Mapping{ID: r.ID, Host: r.Host, RemotePath: r.RemotePath, LocalPath: r.LocalPath}
}

// validate trims the input and gives both paths a trailing separator.
func validate(in Input) (Input, error) {
	in.Host = strings.TrimSpace(in.Host)
	in.RemotePath = strings.TrimSpace(in.RemotePath)
	in.LocalPath = strings.TrimSpace(in.LocalPath)
	switch {
	case in.Host == "":
		return in, &inputError{"Enter the download client host."}
	case in.RemotePath == "":
		return in, &inputError{"Enter the remote path."}
	case in.LocalPath == "":
		return in, &inputError{"Enter the local path."}
	case !strings.HasPrefix(in.LocalPath, "/"):
		return in, &inputError{"The local path must start with /."}
	}
	if IsWindows(in.RemotePath) {
		if !strings.HasSuffix(in.RemotePath, `\`) && !strings.HasSuffix(in.RemotePath, "/") {
			in.RemotePath += `\`
		}
	} else if !strings.HasSuffix(in.RemotePath, "/") {
		in.RemotePath += "/"
	}
	if !strings.HasSuffix(in.LocalPath, "/") {
		in.LocalPath += "/"
	}
	return in, nil
}
