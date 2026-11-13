package varflags

import "os"

func appendIndex(index string) (*os.File, error) {
	return os.OpenFile(index, appendFlags, 0o600)
}
