package utils

import (
	"encoding/json"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration wrapper with JSON/YAML string unmarshaling.
type Duration struct{ time.Duration }

func (d Duration) MarshalJSON() ([]byte, error)      { return json.Marshal(d.String()) }
func (d *Duration) UnmarshalJSON(b []byte) error     { return d.unmarshal(b) }
func (d Duration) MarshalYAML() (interface{}, error) { return d.String(), nil }
func (d *Duration) UnmarshalYAML(n *yaml.Node) error { return d.unmarshal([]byte(n.Value)) }
func (d *Duration) unmarshal(b []byte) error {
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "" || s == "null" {
		d.Duration = 0
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}
func (d Duration) ToDuration() time.Duration { return d.Duration }
func (d Duration) IsPositive() bool          { return d.Duration > 0 }
