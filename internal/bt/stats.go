package bt

import (
	"fmt"
	"io"
)

func dumpStats(w io.Writer, a ...any) {
	for _, v := range a {
		fmt.Fprintf(w, "%+v\n", v)
	}
}
