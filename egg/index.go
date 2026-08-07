package egg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Position is a 1-based source location inside an egg file.
type Position struct {
	Line int
	Col  int
}

// Index maps JSON pointer paths (e.g. "/variables/0/env_variable") to the
// position of the corresponding key in the source bytes. It exists so findings
// can be attached to a real line, which is what turns CLI output into inline
// GitHub annotations via SARIF.
type Index struct {
	pos map[string]Position
}

// Pos returns the position of the given pointer path. Lookup walks up the path
// so a finding about a missing child still lands on its parent object rather
// than defaulting to line 1.
func (i *Index) Pos(path string) Position {
	if i == nil {
		return Position{Line: 1, Col: 1}
	}
	for p := path; ; {
		if pos, ok := i.pos[p]; ok {
			return pos
		}
		idx := strings.LastIndex(p, "/")
		if idx <= 0 {
			break
		}
		p = p[:idx]
	}
	return Position{Line: 1, Col: 1}
}

// Ptr builds a JSON pointer path from segments, escaping per RFC 6901.
func Ptr(segments ...any) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteByte('/')
		switch v := s.(type) {
		case int:
			b.WriteString(strconv.Itoa(v))
		case string:
			r := strings.NewReplacer("~", "~0", "/", "~1")
			b.WriteString(r.Replace(v))
		default:
			b.WriteString(fmt.Sprint(v))
		}
	}
	return b.String()
}

// BuildIndex walks the document recording where every key and array element
// begins. It returns an error only for JSON that cannot be tokenised at all.
func BuildIndex(raw []byte) (*Index, error) {
	idx := &Index{pos: make(map[string]Position)}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	// stack frames describe the container we are currently inside.
	type frame struct {
		path    string
		isArray bool
		arrayN  int
		// expectKey alternates for objects: a token is either a key or a value.
		expectKey bool
		keyPath   string
	}
	var stack []frame

	record := func(path string, off int64) {
		if path == "" {
			return
		}
		if _, exists := idx.pos[path]; !exists {
			idx.pos[path] = positionOf(raw, off)
		}
	}

	for {
		start := skipStructural(raw, dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			// io.EOF ends the walk; anything else means the document is not
			// tokenisable and Parse will surface the richer error.
			if err.Error() == "EOF" {
				break
			}
			return idx, err
		}

		// A closing delimiter always ends the current container, whatever state
		// that container was in. Handling it first keeps the value branch below
		// from recording the delimiter as a phantom array element.
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}

		// Determine the path this token belongs to before mutating the stack.
		var cur *frame
		if len(stack) > 0 {
			cur = &stack[len(stack)-1]
		}

		if cur != nil && !cur.isArray && cur.expectKey {
			key, ok := tok.(string)
			if !ok {
				return idx, fmt.Errorf("unexpected object key token %v", tok)
			}
			cur.keyPath = cur.path + Ptr(key)
			cur.expectKey = false
			record(cur.keyPath, start)
			continue
		}

		// This token is a value (object member value, array element, or root).
		valuePath := ""
		switch {
		case cur == nil:
			valuePath = ""
		case cur.isArray:
			valuePath = cur.path + Ptr(cur.arrayN)
			cur.arrayN++
			record(valuePath, start)
		default:
			valuePath = cur.keyPath
			cur.expectKey = true
		}

		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				stack = append(stack, frame{path: valuePath, expectKey: true})
			case '[':
				stack = append(stack, frame{path: valuePath, isArray: true})
			}
		}
	}
	return idx, nil
}

// skipStructural advances past whitespace and the separators that sit between
// tokens so the recorded offset lands on the token itself.
func skipStructural(raw []byte, off int64) int64 {
	for int(off) < len(raw) {
		switch raw[off] {
		case ' ', '\t', '\n', '\r', ',', ':':
			off++
		default:
			return off
		}
	}
	return off
}

func positionOf(raw []byte, off int64) Position {
	if off < 0 {
		off = 0
	}
	if int(off) > len(raw) {
		off = int64(len(raw))
	}
	line := 1 + bytes.Count(raw[:off], []byte{'\n'})
	lineStart := bytes.LastIndexByte(raw[:off], '\n') + 1
	return Position{Line: line, Col: int(off) - lineStart + 1}
}

// LineOfOffset resolves a byte offset to a 1-based line number.
func LineOfOffset(raw []byte, off int64) int {
	return positionOf(raw, off).Line
}
