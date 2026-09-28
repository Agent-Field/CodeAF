//go:build !windows

// Named schema error
package util

type NamedSchemaError struct {
	Name  string         `json:"name"`
	Data  map[string]any `json:"data"`
	Cause error          `json:"-"`
}

func (e *NamedSchemaError) Error() string { return e.Name }
func (e *NamedSchemaError) Unwrap() error { return e.Cause }

func (e *NamedSchemaError) ToObject() map[string]any {
	return map[string]any{"name": e.Name, "data": e.Data}
}

type NamedErrorFactory struct {
	Tag string
}

func NamedSchemaErrorFactory(tag string) NamedErrorFactory { return NamedErrorFactory{Tag: tag} }

func (f NamedErrorFactory) New(data map[string]any, cause ...error) *NamedSchemaError {
	var err error
	if len(cause) > 0 {
		err = cause[0]
	}
	return &NamedSchemaError{Name: f.Tag, Data: data, Cause: err}
}

func (f NamedErrorFactory) IsInstance(value any) bool {
	switch typed := value.(type) {
	case *NamedSchemaError:
		return typed != nil && typed.Name == f.Tag
	case map[string]any:
		name, _ := typed["name"].(string)
		return name == f.Tag
	default:
		return false
	}
}
