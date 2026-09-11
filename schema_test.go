package needle

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type lightsArgs struct {
	Room       string  `needle:"room" desc:"which room to control" required:"true"`
	Action     string  `needle:"action" desc:"on, off, or dim" enum:"on|off|dim" required:"true"`
	Brightness *int    `needle:"brightness" desc:"0 to 100" min:"0" max:"100"`
	Color      *string `needle:"color" enum:"warm white|cool white|red"`
}

func TestSchemaFromStructTags(t *testing.T) {
	schema, err := SchemaFromStruct[lightsArgs]()
	if err != nil {
		t.Fatal(err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("missing properties: %v", schema)
	}
	if _, ok := props["room"].(map[string]any); !ok {
		t.Fatalf("missing room property: %v", props)
	}
	if want, got := "which room to control", props["room"].(map[string]any)["description"]; want != got {
		t.Errorf("room description = %q, want %q", got, want)
	}
	action := props["action"].(map[string]any)
	enum, ok := action["enum"].([]any)
	if !ok || !reflect.DeepEqual(enum, []any{"on", "off", "dim"}) {
		t.Errorf("action enum = %v, want [on off dim]", action["enum"])
	}
	bright := props["brightness"].(map[string]any)
	if bright["minimum"] != float64(0) || bright["maximum"] != float64(100) {
		t.Errorf("brightness bounds = %v/%v", bright["minimum"], bright["maximum"])
	}
	color := props["color"].(map[string]any)
	enum, _ = color["enum"].([]any)
	if len(enum) != 3 || enum[0] != "warm white" {
		t.Errorf("color enum = %v, want [warm white cool white red]", color["enum"])
	}
	required, ok := schema["required"].([]string)
	if !ok || !reflect.DeepEqual(required, []string{"room", "action"}) {
		t.Errorf("required = %v, want [room action] (declaration order)", schema["required"])
	}
}

type namesArgs struct {
	BrightnessPercent int    `json:"brightness_percent"`
	URLPath           string `json:"-"`
	APIKey            *string
	Any               any
}

func TestSchemaNameFallbacksAndTypes(t *testing.T) {
	schema, err := SchemaFromStruct[namesArgs]()
	if err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	for _, name := range []string{"brightness_percent", "api_key", "any"} {
		if _, ok := props[name]; !ok {
			t.Errorf("missing property %q in %v", name, props)
		}
	}
	if _, ok := props["url_path"]; ok {
		t.Errorf("needle:\"-\" field should be skipped")
	}
	if _, ok := props["urlpath"]; ok {
		t.Errorf("json:\"-\" field should be skipped")
	}
	if props["brightness_percent"].(map[string]any)["type"] != "integer" {
		t.Errorf("int field should be integer")
	}
	if props["api_key"].(map[string]any)["type"] != "string" {
		t.Errorf("pointer-to-string should be string")
	}
	if _, ok := props["any"].(map[string]any)["type"]; ok {
		t.Errorf("any field should be unconstrained")
	}
	required := schema["required"].([]string)
	if !reflect.DeepEqual(required, []string{"brightness_percent"}) {
		t.Errorf("required = %v, want [brightness_percent] (pointer and any fields optional)", required)
	}
}

type nestedArgs struct {
	Vendor string `needle:"vendor" required:"true"`
	Items  []item `needle:"items"`
}

type item struct {
	Name     string  `needle:"name" required:"true"`
	Quantity int     `needle:"quantity" min:"1"`
	Price    float64 `needle:"price" gt:"0"`
}

func TestNestedStructsAndArrays(t *testing.T) {
	schema, err := SchemaFromStruct[nestedArgs]()
	if err != nil {
		t.Fatal(err)
	}
	items := schema["properties"].(map[string]any)["items"].(map[string]any)
	if items["type"] != "array" {
		t.Fatalf("items type = %v", items["type"])
	}
	itemSchema := items["items"].(map[string]any)
	if itemSchema["type"] != "object" {
		t.Fatalf("item type = %v", itemSchema["type"])
	}
	quantity := itemSchema["properties"].(map[string]any)["quantity"].(map[string]any)
	if quantity["minimum"] != float64(1) {
		t.Errorf("quantity.minimum = %v", quantity["minimum"])
	}
	price := itemSchema["properties"].(map[string]any)["price"].(map[string]any)
	if _, ok := price["exclusiveMinimum"]; !ok {
		t.Errorf("price should carry exclusiveMinimum for gt")
	}
	itemRequired := itemSchema["required"].([]string)
	if !reflect.DeepEqual(itemRequired, []string{"name", "quantity", "price"}) {
		t.Errorf("item required = %v (non-pointer fields default to required)", itemRequired)
	}
}

type embeddedArgs struct {
	Base      `needle:"base,skip-unimplemented"` // named: stays a nested property
	ExtraFlag bool                               `needle:"extra_flag"`
}

type Base struct {
	ID    string `needle:"id" required:"true"`
	Owner string `needle:"owner"`
}

func TestNamedEmbeddedStruct(t *testing.T) {
	schema, err := SchemaFromStruct[embeddedArgs]()
	if err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["base"]; !ok {
		t.Fatalf("named embedded struct should become a property: %v", props)
	}
	if _, ok := props["extra_flag"]; !ok {
		t.Fatalf("extra_flag missing: %v", props)
	}
	base := props["base"].(map[string]any)
	if base["type"] != "object" {
		t.Errorf("base.type = %v", base["type"])
	}
}

func TestParamsBuilder(t *testing.T) {
	params := Params(map[string]Property{
		"room":       Str(Desc("which room"), IsRequired),
		"on":         Bool(IsRequired),
		"brightness": Int(Desc("percent"), Min(0), Max(100)),
		"tags":       ArrayOf(Str(), MinItems(1), Unique()),
	})
	if params["type"] != "object" {
		t.Fatal("params must be an object")
	}
	props := params["properties"].(map[string]any)
	bright := props["brightness"].(map[string]any)
	if bright["minimum"] != float64(0) || bright["maximum"] != float64(100) {
		t.Errorf("brightness = %v", bright)
	}
	tags := props["tags"].(map[string]any)
	if tags["type"] != "array" || tags["minItems"] != 1 || tags["uniqueItems"] != true {
		t.Errorf("tags = %v", tags)
	}
	if _, ok := tags["items"].(map[string]any); !ok {
		t.Errorf("tags.items missing")
	}
	required := params["required"].([]string)
	if !reflect.DeepEqual(required, []string{"on", "room"}) {
		t.Errorf("required = %v", required)
	}
	// The internal required marker must never leak into the schema JSON.
	blob, _ := json.Marshal(params)
	if strings.Contains(string(blob), "__required") {
		t.Errorf("internal marker leaked into schema JSON: %s", blob)
	}
}

func TestEnumProperty(t *testing.T) {
	p := Enum("heat", "cool", "auto")
	if p["type"] != "string" {
		t.Errorf("enum type = %v", p["type"])
	}
	enum := p["enum"].([]any)
	if len(enum) != 3 || enum[2] != "auto" {
		t.Errorf("enum = %v", enum)
	}
}

func TestToolFuncSchemaAndInvoke(t *testing.T) {
	tool := ToolFunc("control_lights", "Turn lights on or off", func(ctx context.Context, a lightsArgs) (any, error) {
		if a.Room != "kitchen" {
			t.Errorf("Room = %q", a.Room)
		}
		if a.Brightness == nil || *a.Brightness != 42 {
			t.Errorf("Brightness = %v", a.Brightness)
		}
		return map[string]any{"ok": true}, nil
	})
	schema := tool.Schema()
	if schema["name"] != "control_lights" {
		t.Errorf("name = %v", schema["name"])
	}
	params := schema["parameters"].(map[string]any)
	if _, ok := params["properties"].(map[string]any)["room"]; !ok {
		t.Errorf("parameters missing room: %v", params)
	}
	result, err := tool.Invoke(nil, map[string]any{
		"room": "kitchen", "action": "dim", "brightness": 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["ok"] != true {
		t.Errorf("result = %v", result)
	}
}

func TestToolFuncInvokeRejectsBadArgs(t *testing.T) {
	tool := ToolFunc("x", "x", func(ctx context.Context, a lightsArgs) (any, error) {
		return nil, nil
	})
	// brightness is an integer in the schema; a string value fails decode
	if _, err := tool.Invoke(nil, map[string]any{"room": "r", "action": "on", "brightness": "not-a-number"}); err == nil {
		t.Error("expected decode error for bad argument type")
	}
}

func TestHandlerlessToolInvoke(t *testing.T) {
	tool := NewTool("schema_only", "no handler").Params(map[string]Property{
		"x": Str(IsRequired),
	}).Build()
	if _, err := tool.Invoke(nil, map[string]any{"x": "1"}); err == nil {
		t.Error("expected unknown-tool error")
	}
}
