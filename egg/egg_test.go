package egg

import (
	"testing"
)

func TestFlexBoolAcceptsNumericAndStringForms(t *testing.T) {
	cases := map[string]bool{
		"true": true, "false": false,
		"1": true, "0": false,
		`"true"`: true, `"0"`: false,
		"null": false,
	}
	for in, want := range cases {
		var b FlexBool
		if err := b.UnmarshalJSON([]byte(in)); err != nil {
			t.Errorf("UnmarshalJSON(%s): %v", in, err)
			continue
		}
		if bool(b) != want {
			t.Errorf("FlexBool(%s) = %v, want %v", in, bool(b), want)
		}
	}
}

func TestFlexStringAcceptsNonStrings(t *testing.T) {
	cases := map[string]string{
		`"hello"`: "hello",
		"25565":   "25565",
		"true":    "true",
		"null":    "",
	}
	for in, want := range cases {
		var s FlexString
		if err := s.UnmarshalJSON([]byte(in)); err != nil {
			t.Errorf("UnmarshalJSON(%s): %v", in, err)
			continue
		}
		if s.String() != want {
			t.Errorf("FlexString(%s) = %q, want %q", in, s.String(), want)
		}
	}
}

func TestFlexStringSliceToleratesString(t *testing.T) {
	var f FlexStringSlice
	if err := f.UnmarshalJSON([]byte(`""`)); err != nil {
		t.Fatalf("an empty string must not fail decoding: %v", err)
	}
	if len(f) != 0 {
		t.Errorf("expected an empty slice, got %v", f)
	}
	if err := f.UnmarshalJSON([]byte(`["a","b"]`)); err != nil {
		t.Fatal(err)
	}
	if len(f) != 2 {
		t.Errorf("expected 2 entries, got %v", f)
	}
}

func TestFlexRulesSplitting(t *testing.T) {
	var r FlexRules
	if err := r.UnmarshalJSON([]byte(`"required|string|max:20"`)); err != nil {
		t.Fatal(err)
	}
	if !r.Has("required") {
		t.Error("expected Has(required)")
	}
	if !r.Has("max") {
		t.Error("parameterised rules should match on their name")
	}
	if r.Has("nullable") {
		t.Error("did not expect Has(nullable)")
	}
	if len(r.List()) != 3 {
		t.Errorf("expected 3 rules, got %v", r.List())
	}
}

func TestFlexRulesAcceptsArray(t *testing.T) {
	var r FlexRules
	if err := r.UnmarshalJSON([]byte(`["required","string"]`)); err != nil {
		t.Fatal(err)
	}
	if !r.Has("required") || !r.Has("string") {
		t.Errorf("array form did not decode: %q", r)
	}
}

func TestImagesHandlesBothSchemas(t *testing.T) {
	v2 := &Egg{DockerImages: map[string]string{"Java 21": "img:21"}}
	if len(v2.Images()) != 1 {
		t.Error("v2 docker_images should be returned")
	}
	v1 := &Egg{DockerImage: "legacy:1"}
	got := v1.Images()
	if len(got) != 1 || got["default"] != "legacy:1" {
		t.Errorf("v1 docker_image should be surfaced, got %v", got)
	}
	if len((&Egg{}).Images()) != 0 {
		t.Error("an egg with no image should return nothing")
	}
}

func TestEntrypointDefaultsToBash(t *testing.T) {
	if got := (&Egg{}).Entrypoint(); got != "bash" {
		t.Errorf("expected bash, got %q", got)
	}
}

func TestLooksLikeEgg(t *testing.T) {
	egg := []byte(`{"meta":{"version":"PTDL_v2"},"name":"x"}`)
	if !LooksLikeEgg(egg) {
		t.Error("a PTDL meta block should identify an egg")
	}
	config := []byte(`{"WorldName":"x","Difficulty":"Normal"}`)
	if LooksLikeEgg(config) {
		t.Error("a game config file should not be treated as an egg")
	}
	noMeta := []byte(`{"name":"x","author":"a@b.c","startup":"./run","scripts":{},"docker_images":{"a":"b"}}`)
	if !LooksLikeEgg(noMeta) {
		t.Error("enough egg-specific fields should identify an egg even without meta")
	}
	if LooksLikeEgg([]byte(`not json`)) {
		t.Error("invalid JSON is not an egg")
	}
}

func TestIndexResolvesPositions(t *testing.T) {
	raw := []byte("{\n  \"name\": \"x\",\n  \"variables\": [\n    {\n      \"env_variable\": \"A\"\n    }\n  ]\n}\n")
	idx, err := BuildIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.Pos(Ptr("name")).Line; got != 2 {
		t.Errorf("name should be on line 2, got %d", got)
	}
	if got := idx.Pos(Ptr("variables", 0, "env_variable")).Line; got != 5 {
		t.Errorf("env_variable should be on line 5, got %d", got)
	}
}

// A pointer with no exact entry should fall back to its nearest parent rather
// than defaulting to line 1.
func TestIndexFallsBackToParent(t *testing.T) {
	raw := []byte("{\n  \"scripts\": {\n    \"installation\": {\n      \"container\": \"x\"\n    }\n  }\n}\n")
	idx, err := BuildIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := idx.Pos(Ptr("scripts", "installation", "script")).Line
	if got != 3 {
		t.Errorf("expected the parent installation line 3, got %d", got)
	}
}

func TestParseReportsSyntaxErrorLine(t *testing.T) {
	_, err := Parse("x.json", []byte("{\n  \"a\": 1,\n  \"b\": ,\n}\n"))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	var se *SyntaxError
	if ok := asSyntaxErr(err, &se); !ok {
		t.Fatalf("expected a *SyntaxError, got %T", err)
	}
	if se.Line != 3 {
		t.Errorf("expected line 3, got %d", se.Line)
	}
}

func asSyntaxErr(err error, target **SyntaxError) bool {
	se, ok := err.(*SyntaxError)
	if ok {
		*target = se
	}
	return ok
}
