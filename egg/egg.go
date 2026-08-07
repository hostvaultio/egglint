// Package egg models the Pterodactyl / Pelican egg export format (PTDL_v1 and
// PTDL_v2) and parses it in a way that tolerates the variation found in real
// exports while preserving enough source position information to report
// findings against specific lines.
//
// Real-world eggs are messier than the panel's own exporter suggests: numeric
// booleans, non-string default values, and both the v1 singular `docker_image`
// and the v2 `docker_images` map all appear in widely used repositories. Parsing
// is therefore deliberately permissive — the linter's job is to *report* those
// problems, not to fail to load the file in the first place.
package egg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SchemaVersion identifies the egg export format.
type SchemaVersion string

const (
	// SchemaV1 is the legacy export format using a singular `docker_image`.
	SchemaV1 SchemaVersion = "PTDL_v1"
	// SchemaV2 is the current export format using a `docker_images` map.
	SchemaV2 SchemaVersion = "PTDL_v2"
)

// Egg is a parsed egg export.
type Egg struct {
	Comment      string            `json:"_comment,omitempty"`
	Meta         Meta              `json:"meta"`
	ExportedAt   string            `json:"exported_at,omitempty"`
	Name         string            `json:"name"`
	Author       string            `json:"author"`
	UUID         string            `json:"uuid,omitempty"`
	Description  string            `json:"description,omitempty"`
	Features     FlexStringSlice   `json:"features,omitempty"`
	DockerImages map[string]string `json:"docker_images,omitempty"`
	// DockerImage is the PTDL_v1 singular form. Kept separate from DockerImages
	// so a rule can tell the two schemas apart; use Images() to read either.
	DockerImage  string          `json:"docker_image,omitempty"`
	FileDenylist FlexStringSlice `json:"file_denylist,omitempty"`
	Startup      string          `json:"startup"`
	Config       Config          `json:"config"`
	Scripts      Scripts         `json:"scripts"`
	Variables    []Varible       `json:"variables,omitempty"`
}

// Meta carries the schema version and optional update URL.
type Meta struct {
	Version   SchemaVersion `json:"version"`
	UpdateURL *string       `json:"update_url"`
}

// Config holds the four panel configuration blocks. Files, Startup and Logs are
// JSON documents that the panel stores *as strings* — a nesting quirk that is a
// frequent source of broken eggs, so they stay strings here and are validated by
// a rule rather than being decoded eagerly.
type Config struct {
	Files   string `json:"files"`
	Startup string `json:"startup"`
	Logs    string `json:"logs"`
	// Stop is a plain command string (e.g. "stop"), NOT a JSON document.
	Stop string `json:"stop"`
}

// Scripts holds the installation script block.
type Scripts struct {
	Installation Installation `json:"installation"`
}

// Installation describes how the egg installs itself.
type Installation struct {
	Script     string `json:"script"`
	Container  string `json:"container"`
	Entrypoint string `json:"entrypoint"`
}

// Varible is an egg variable. (The name preserves the panel's own historical
// spelling in some exports; JSON tags are what actually matter.)
type Varible struct {
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	EnvVariable  FlexString `json:"env_variable"`
	DefaultValue FlexString `json:"default_value"`
	UserViewable FlexBool   `json:"user_viewable"`
	UserEditable FlexBool   `json:"user_editable"`
	Rules        FlexRules  `json:"rules"`
	FieldType    string     `json:"field_type,omitempty"`
}

// Images returns the egg's docker images regardless of schema version, so rules
// do not have to branch on v1 vs v2.
func (e *Egg) Images() map[string]string {
	if len(e.DockerImages) > 0 {
		return e.DockerImages
	}
	if e.DockerImage != "" {
		return map[string]string{"default": e.DockerImage}
	}
	return nil
}

// Entrypoint returns the declared install entrypoint, defaulting to "bash" to
// match the panel's own behaviour when the field is absent.
func (e *Egg) Entrypoint() string {
	if e.Scripts.Installation.Entrypoint == "" {
		return "bash"
	}
	return e.Scripts.Installation.Entrypoint
}

// FlexString is a string that also accepts numbers, booleans and null, because
// `default_value` is typed loosely across published eggs.
type FlexString string

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(strings.Trim(string(b), `"`))
	return nil
}

// String returns the value as a plain string.
func (f FlexString) String() string { return string(f) }

// FlexBool is a bool that also accepts 0/1 and "true"/"false", both of which
// appear in older exports.
type FlexBool bool

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	switch s {
	case "", "null":
		*f = false
		return nil
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		// Numeric forms other than 0/1 are treated as truthy rather than an error;
		// a malformed flag should not stop the rest of the egg from being linted.
		n, nerr := strconv.ParseFloat(s, 64)
		if nerr != nil {
			return fmt.Errorf("cannot parse %q as boolean", s)
		}
		*f = n != 0
		return nil
	}
	*f = FlexBool(v)
	return nil
}

// FlexStringSlice is a list of strings that also accepts null or a bare string,
// both of which occur in published eggs where the schema expects an array.
//
// Tolerating them is deliberate. Decoding must not abort on one malformed field,
// because an aborted decode means every other rule is silently skipped for that
// egg — the file with a problem is exactly the one that most needs checking. The
// type deviation itself is reported by its own rule.
type FlexStringSlice []string

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexStringSlice) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = nil
		return nil
	}
	if b[0] == '[' {
		var out []string
		if err := json.Unmarshal(b, &out); err != nil {
			// A heterogeneous array still yields the entries we can read.
			var loose []any
			if err2 := json.Unmarshal(b, &loose); err2 != nil {
				return err
			}
			out = out[:0]
			for _, v := range loose {
				if s, ok := v.(string); ok {
					out = append(out, s)
				}
			}
		}
		*f = out
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		*f = nil
		return nil
	}
	if s == "" {
		*f = nil
	} else {
		*f = []string{s}
	}
	return nil
}

// FlexRules is a Laravel validation rule string. Some eggs express it as a JSON
// array instead of a pipe-delimited string.
type FlexRules string

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexRules) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '[' {
		var parts []string
		if err := json.Unmarshal(b, &parts); err != nil {
			return err
		}
		*f = FlexRules(strings.Join(parts, "|"))
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*f = FlexRules(s)
	return nil
}

// List splits the rule string into individual Laravel rules.
func (f FlexRules) List() []string {
	if f == "" {
		return nil
	}
	raw := strings.Split(string(f), "|")
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// Has reports whether the given bare rule (e.g. "required", "nullable") is
// present. Parameterised rules such as `max:20` match on the name before the
// colon.
func (f FlexRules) Has(name string) bool {
	for _, r := range f.List() {
		if r == name || strings.HasPrefix(r, name+":") {
			return true
		}
	}
	return false
}

// String returns the raw rule string.
func (f FlexRules) String() string { return string(f) }

// File is a parsed egg plus the source needed to report positions against it.
type File struct {
	Path  string
	Raw   []byte
	Egg   *Egg
	Index *Index
}

// Parse decodes egg JSON and builds a position index for it. A JSON syntax
// error is returned as a *SyntaxError carrying the offending line.
func Parse(path string, raw []byte) (*File, error) {
	idx, err := BuildIndex(raw)
	if err != nil {
		// BuildIndex fails first on malformed JSON. Wrap its error so callers
		// always receive a *SyntaxError carrying a resolved line, rather than
		// sometimes getting the encoding/json error unchanged.
		return &File{Path: path, Raw: raw}, &SyntaxError{
			Msg:  err.Error(),
			Line: LineOfOffset(raw, offsetFromError(err)),
		}
	}
	var e Egg
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&e); err != nil {
		return &File{Path: path, Raw: raw, Index: idx}, &SyntaxError{
			Msg:  err.Error(),
			Line: LineOfOffset(raw, offsetFromError(err)),
		}
	}
	return &File{Path: path, Raw: raw, Egg: &e, Index: idx}, nil
}

// SyntaxError is a JSON parse failure with a resolved source line.
type SyntaxError struct {
	Msg  string
	Line int
}

func (e *SyntaxError) Error() string { return e.Msg }

func offsetFromError(err error) int64 {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return se.Offset
	}
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return ute.Offset
	}
	return 0
}
