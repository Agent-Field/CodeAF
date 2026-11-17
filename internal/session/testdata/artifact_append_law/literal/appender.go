package literal

import "os"

func appendIndex(index string) (*os.File, error) {
	return os.OpenFile(index, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
