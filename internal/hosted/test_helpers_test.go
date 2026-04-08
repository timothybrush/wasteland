package hosted

import "io/fs"

type emptyFS struct{}

func (emptyFS) Open(_ string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: "", Err: fs.ErrNotExist}
}
