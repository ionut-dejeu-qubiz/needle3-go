package needle

import (
	"reflect"
	"testing"
)

// Regression: schema names like "due_date" must reach fields named DueDate
// without json tags (the bug decoded them as zero values).
type rekeyInvoice struct {
	Vendor   string  `needle:"vendor" required:"true"`
	TotalDue float64 `needle:"total_due" required:"true"`
	DueDate  string  `needle:"due_date" format:"date"`
	Ignored  string  `needle:"-"`
	Legacy   string  `json:"legacy_name"`
	Contact  *rekeyContact
	Items    []rekeyItem
	When     string `needle:"when"` // time-like string, decode as-is
}
type rekeyContact struct {
	EmailAddress string `needle:"email_address"`
	DisplayName  string
}
type rekeyItem struct {
	SKU      string `needle:"sku"`
	Quantity int    `needle:"quantity"`
}

func TestDecodeArgumentsRekeysSnakeCase(t *testing.T) {
	args := map[string]any{
		"vendor":        "Acme Corp",
		"total_due":     1200.0,
		"due_date":      "2026-09-01",
		"legacy_name":   "kept",
		"contact":       map[string]any{"email_address": "hi@acme.dev", "display_name": "Acme"},
		"items":         []any{map[string]any{"sku": "A1", "quantity": 2}},
		"when":          "2026-09-01T10:00:00Z",
		"unknown_extra": "passes through",
	}
	decoded, err := DecodeArguments[rekeyInvoice](args)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Vendor != "Acme Corp" {
		t.Errorf("Vendor = %q", decoded.Vendor)
	}
	if decoded.TotalDue != 1200 {
		t.Errorf("TotalDue = %v", decoded.TotalDue)
	}
	if decoded.DueDate != "2026-09-01" {
		t.Errorf("DueDate = %q (snake_case rekey regression)", decoded.DueDate)
	}
	if decoded.Legacy != "kept" {
		t.Errorf("Legacy = %q (json tag names must keep working)", decoded.Legacy)
	}
	if decoded.Contact == nil || decoded.Contact.EmailAddress != "hi@acme.dev" {
		t.Errorf("Contact = %+v (nested rekey via pointer)", decoded.Contact)
	}
	if decoded.Contact.DisplayName != "Acme" {
		t.Errorf("Contact.DisplayName = %q (snake_case fallback)", decoded.Contact.DisplayName)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].SKU != "A1" || decoded.Items[0].Quantity != 2 {
		t.Errorf("Items = %+v (nested rekey via slice)", decoded.Items)
	}
	if decoded.When != "2026-09-01T10:00:00Z" {
		t.Errorf("When = %q", decoded.When)
	}
}

func TestDecodeArgumentsEmpty(t *testing.T) {
	decoded, err := DecodeArguments[rekeyInvoice](nil)
	if err != nil || decoded.Vendor != "" {
		t.Fatalf("nil arguments should decode to zero values, got %v, %v", decoded, err)
	}
}

func TestDecodeArgumentsTypeMismatch(t *testing.T) {
	args := map[string]any{"total_due": "not a number"}
	if _, err := DecodeArguments[rekeyInvoice](args); err == nil {
		t.Fatal("expected a decode error for a type mismatch")
	}
}

func TestJSONTagNameFallbacks(t *testing.T) {
	schema, err := SchemaFromStruct[rekeyInvoice]()
	if err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	for _, name := range []string{"vendor", "total_due", "due_date", "legacy_name", "contact", "items", "when"} {
		if _, ok := props[name]; !ok {
			t.Errorf("missing property %q", name)
		}
	}
	if _, ok := props["ignored"]; ok {
		t.Errorf("needle:\"-\" field leaked into schema")
	}
	contact := props["contact"].(map[string]any)
	inner := contact["properties"].(map[string]any)
	if _, ok := inner["email_address"]; !ok {
		t.Errorf("missing contact.email_address")
	}
	if _, ok := inner["display_name"]; !ok {
		t.Errorf("missing contact.display_name")
	}
}

func TestSmartHomeStyleSchema(t *testing.T) {
	type controlLightsArgs struct {
		Room       string  `needle:"room" desc:"The room to control." required:"true"`
		Action     string  `needle:"action" desc:"on, off, or dim." enum:"on|off|dim" required:"true"`
		Brightness *int    `needle:"brightness_percent" desc:"Brightness from 0 to 100." min:"0" max:"100"`
		Color      *string `needle:"color" enum:"warm white|cool white|red|green|blue" desc:"The light color."`
	}
	schema, err := SchemaFromStruct[controlLightsArgs]()
	if err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	bright := props["brightness_percent"].(map[string]any)
	if bright["minimum"] != float64(0) || bright["maximum"] != float64(100) {
		t.Errorf("brightness bounds = %v / %v", bright["minimum"], bright["maximum"])
	}
	color := props["color"].(map[string]any)
	if got := color["enum"].([]any); len(got) != 5 || got[0] != "warm white" {
		t.Errorf("color enum = %v", got)
	}
	if !reflect.DeepEqual(schema["required"].([]string), []string{"room", "action"}) {
		t.Errorf("required = %v", schema["required"])
	}
}
