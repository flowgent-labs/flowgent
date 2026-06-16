package utils

import (
	"encoding/json"
	"time"

	"gopkg.in/yaml.v3"
)

// UnitDuration is a time.Duration wrapper with JSON/YAML string unmarshaling,
// allowing duration strings like "5m", "30s", "1h" in configuration files.
type UnitDuration struct{ time.Duration }

func (d UnitDuration) MarshalJSON() ([]byte, error)      { return json.Marshal(d.String()) }
func (d *UnitDuration) UnmarshalJSON(b []byte) error     { return d.unmarshal(b) }
func (d UnitDuration) MarshalYAML() (interface{}, error) { return d.String(), nil }
func (d *UnitDuration) UnmarshalYAML(n *yaml.Node) error { return d.unmarshal([]byte(n.Value)) }
func (d *UnitDuration) unmarshal(b []byte) error {
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
func (d UnitDuration) ToDuration() time.Duration { return d.Duration }
func (d UnitDuration) IsPositive() bool          { return d.Duration > 0 }
