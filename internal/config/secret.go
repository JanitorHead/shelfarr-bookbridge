package config

// SecretString hides its value from logs, fmt, and JSON. Use Reveal() only at
// the exact point a raw credential is needed (HTTP header, URL).
type SecretString string

const redacted = "***"

func (s SecretString) String() string               { return redacted }
func (s SecretString) GoString() string              { return redacted }
func (s SecretString) MarshalJSON() ([]byte, error)  { return []byte(`"` + redacted + `"`), nil }
func (s SecretString) Reveal() string                { return string(s) }
