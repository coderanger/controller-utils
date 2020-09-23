/*
Copyright 2020 Noah Kantrowitz

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package templates

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

type filter struct {
	glob        string
	shouldMatch bool
}

type FilteredFileSystem struct {
	fs      http.FileSystem
	filters []*filter
}

type FilteredFile struct {
	http.File
	filters []*filter
}

func allowedByFilters(name string, filters []*filter) error {
	for _, f := range filters {
		matches, err := filepath.Match(f.glob, name[1:])
		if err != nil {
			return err
		}
		if matches != f.shouldMatch {
			return fmt.Errorf("Does not match filter %s", f.glob)
		}
	}
	return nil
}

func NewFilteredFileSystem(fs http.FileSystem) *FilteredFileSystem {
	return &FilteredFileSystem{fs: fs, filters: []*filter{}}
}

func (ffs *FilteredFileSystem) Include(glob string) *FilteredFileSystem {
	return ffs.addFilter(glob, true)
}

func (ffs *FilteredFileSystem) Exclude(glob string) *FilteredFileSystem {
	return ffs.addFilter(glob, false)
}

func (ffs *FilteredFileSystem) addFilter(glob string, shouldMatch bool) *FilteredFileSystem {
	filters := make([]*filter, len(ffs.filters), len(ffs.filters)+1)
	copy(filters, ffs.filters)
	filters = append(filters, &filter{glob: glob, shouldMatch: shouldMatch})
	return &FilteredFileSystem{
		fs:      ffs.fs,
		filters: filters,
	}
}

func (ffs *FilteredFileSystem) Open(name string) (http.File, error) {
	file, err := ffs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	err = allowedByFilters(name, ffs.filters)
	if err != nil {
		return nil, err
	}

	return &FilteredFile{File: file, filters: ffs.filters}, nil
}

func (ff *FilteredFile) Readdir(count int) ([]os.FileInfo, error) {
	fis, err := ff.File.Readdir(count)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(fis))
	for _, fi := range fis {
		if allowedByFilters(fi.Name(), ff.filters) == nil {
			out = append(out, fi)
		}
	}
	return out, nil
}

// Check interface compliance.
var _ http.FileSystem = &FilteredFileSystem{}
var _ http.File = &FilteredFile{}
