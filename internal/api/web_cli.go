//go:build cli

package api

import "io/fs"

func defaultWeb() fs.FS { return nil }
