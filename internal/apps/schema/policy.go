package schema

// DataClass is the handling declared for a record field.
type DataClass string

const (
	ClassPublic       DataClass = "public"
	ClassInternal     DataClass = "internal"
	ClassConfidential DataClass = "confidential"
	ClassRestricted   DataClass = "restricted"
	ClassPII          DataClass = "pii"
)

// Rank orders declarations so a second app cannot weaken an inherited class.
func (c DataClass) Rank() int {
	switch c {
	case "", ClassPublic:
		return 0
	case ClassInternal:
		return 1
	case ClassConfidential:
		return 2
	case ClassRestricted, ClassPII:
		return 3
	default:
		return 4
	}
}

// RecordPolicy is declared by the type's owner and inherited by every use.
type RecordPolicy struct {
	Row    *RowPolicy        `json:"row,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

// RowPolicy grants visibility when the field equals a trusted person attribute.
type RowPolicy struct {
	Field     string `json:"field"`
	Attribute string `json:"attribute"`
}

// Restricted classes require an explicit model destination approval.
func (c DataClass) Restricted() bool { return c == ClassRestricted || c == ClassPII }
