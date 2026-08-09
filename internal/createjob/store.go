package createjob

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

type storeFile struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Jobs      []*Job    `json:"jobs"`
}

func (s *Store) Load() ([]*Job, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Job{}, nil
		}
		return nil, err
	}

	var file storeFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Jobs == nil {
		return []*Job{}, nil
	}
	return file.Jobs, nil
}

func (s *Store) Save(jobs []*Job) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(storeFile{
		Version:   1,
		UpdatedAt: time.Now(),
		Jobs:      jobs,
	}, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
