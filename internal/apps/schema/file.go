package schema

import "fmt"

// ValidateFileValue applies the kit's file value schema in the live writer.
func ValidateFileValue(kind string, value any) error {
	entry := schemas().entries["field/"+kind]
	if entry == nil || entry.Validate(value) != nil {
		return fmt.Errorf("must contain valid file metadata")
	}
	return nil
}
