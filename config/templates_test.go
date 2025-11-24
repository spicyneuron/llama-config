package config

import (
	"regexp"
	"testing"
)

func TestTemplateFuncUUIDShape(t *testing.T) {
	uuidFn := TemplateFuncs["uuid"].(func() string)
	id := uuidFn()

	if matched := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id); !matched {
		t.Fatalf("uuid() returned unexpected format: %s", id)
	}
}

func TestTemplateFuncDefaultAndMath(t *testing.T) {
	defaultFn := TemplateFuncs["default"].(func(any, any) any)
	addFn := TemplateFuncs["add"].(func(any, any) any)
	mulFn := TemplateFuncs["mul"].(func(any, any) any)

	if got := defaultFn("x", nil); got != "x" {
		t.Fatalf("default(nil) = %v, want x", got)
	}
	if got := defaultFn(5.0, float64(0)); got != 5.0 {
		t.Fatalf("default(0.0) = %v, want 5", got)
	}
	if got := defaultFn("keep", "value"); got != "value" {
		t.Fatalf("default(non-zero) = %v, want original value", got)
	}

	if sum := addFn(2, "3").(float64); sum != 5 {
		t.Fatalf("add(2, \"3\") = %v, want 5", sum)
	}
	if product := mulFn("2", 4).(float64); product != 8 {
		t.Fatalf("mul(\"2\", 4) = %v, want 8", product)
	}
}

func TestTemplateFuncIndexDictAndKindIs(t *testing.T) {
	indexFn := TemplateFuncs["index"].(func(any, ...any) any)
	dictFn := TemplateFuncs["dict"].(func(...any) map[string]any)
	kindIsFn := TemplateFuncs["kindIs"].(func(string, any) bool)

	obj := map[string]any{
		"arr": []any{
			dictFn("foo", "bar"),
		},
	}

	val := indexFn(obj, "arr", 0, "foo")
	if val != "bar" {
		t.Fatalf("index into nested dict returned %v, want bar", val)
	}

	if !kindIsFn("map", obj) {
		t.Fatalf("kindIs map should be true for obj")
	}
	if !kindIsFn("slice", obj["arr"]) {
		t.Fatalf("kindIs slice should be true for arr")
	}
	if kindIsFn("string", obj["arr"]) {
		t.Fatalf("kindIs string should be false for slice")
	}
}
