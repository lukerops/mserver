//go:build !unix

package local

import "io/fs"

func inodeOf(fs.FileInfo) uint64 { return 0 }
