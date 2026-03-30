package updater

import (
	"io"
	"os"
)

var _stderr io.Writer = os.Stderr
