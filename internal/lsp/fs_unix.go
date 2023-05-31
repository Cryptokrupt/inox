//go:build unix

package internal

import (
	afs "github.com/inoxlang/inox/internal/afs"

	"github.com/inoxlang/inox/internal/globals/fs_ns"
)

const (
	DEFAULT_MAX_IN_MEM_FS_STORAGE_SIZE = 10_000_000
)

// Filesystem is a filesystem that stores the unsaved documents in a separate filesystem.
type Filesystem struct {
	afs.Filesystem
	unsavedDocuments afs.Filesystem
}

func NewDefaultFilesystem() *Filesystem {
	return &Filesystem{
		Filesystem:       fs_ns.GetOsFilesystem(),
		unsavedDocuments: fs_ns.NewMemFilesystem(DEFAULT_MAX_IN_MEM_FS_STORAGE_SIZE),
	}
}

func NewFilesystem(base afs.Filesystem, unsavedDocumentFs afs.Filesystem) *Filesystem {
	return &Filesystem{
		Filesystem:       base,
		unsavedDocuments: unsavedDocumentFs,
	}
}

func (fs *Filesystem) Open(filename string) (afs.File, error) {
	if fs.unsavedDocuments == nil {
		return fs.Filesystem.Open(filename)
	}

	f, err := fs.unsavedDocuments.Open(filename)
	if err != nil {
		return fs.Filesystem.Open(filename)
	}
	return f, nil
}

func (fs *Filesystem) docsFS() afs.Filesystem {
	if fs.unsavedDocuments == nil {
		return fs
	}
	return fs.unsavedDocuments
}

func (fs *Filesystem) Save() {

}
