// Command extraction pulls structured data out of unstructured text.
// Extraction is not a separate mode: the record is declared as the only
// tool, and the returned call's arguments are the extracted fields.
//
//	go run ./examples/extraction
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/FlameInTheDark/needle-go"
)

// Invoice is the extraction target. With one declared tool the grammar
// admits exactly one call of that name, so schema conformance is
// guaranteed rather than requested.
type Invoice struct {
	Vendor  string  `needle:"vendor" desc:"company that issued the invoice" required:"true"`
	Total   float64 `needle:"total" desc:"invoice total amount" required:"true"`
	DueDate string  `needle:"due_date" desc:"payment due date" format:"date"`
}

// Contact shows a nested record: structs inside structs become nested
// object schemas automatically.
type Contact struct {
	Name  string `needle:"name" desc:"full name" required:"true"`
	Email string `needle:"email" desc:"email address"`
	Phone string `needle:"phone" desc:"phone number"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Typed extraction: the struct's tags are the schema, and the result
	// comes back decoded into the same type. Strict mode (the default)
	// rejects temporal values that contradict a literal year in the input,
	// engine-reported fabricated values, and negated requests.
	invoice, err := needle.Extract[Invoice](ctx,
		"Invoice from Acme Corp, total 1200.00, due 2026-09-01")
	if err != nil {
		// ExtractionValidationError: values not grounded in the input.
		log.Printf("strict validation: %v", err)
	} else if invoice != nil {
		fmt.Printf("typed:      vendor=%q total=%.2f due=%q\n",
			invoice.Vendor, invoice.Total, invoice.DueDate)
	} else {
		fmt.Println("typed:      no match (empty call)")
	}

	// Grounding guard: ask about a fox and the model may still fabricate
	// a vendor and total. Strict mode rejects values that are not
	// evidenced in the input instead of returning them silently; loose
	// mode returns whatever the model produced.
	fabricated, err := needle.Extract[Invoice](ctx, "the quick brown fox jumps over the lazy dog")
	if err != nil {
		fmt.Printf("grounding:  rejected - %v\n", err)
	} else {
		fmt.Printf("grounding:  %v (model refused to fabricate)\n", fabricated)
	}
	fabricated, err = needle.Extract[Invoice](ctx,
		"the quick brown fox jumps over the lazy dog", needle.ExtractStrict(false))
	if err != nil {
		log.Fatal(err)
	}
	if fabricated != nil {
		fmt.Printf("loose:      vendor=%q total=%.2f (ungrounded, returned anyway)\n",
			fabricated.Vendor, fabricated.Total)
	}

	// Raw extraction against a hand-written JSON schema; the arguments
	// map is returned as-is.
	schema := map[string]any{
		"name":        "receipt",
		"description": "A purchase receipt shared as text",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"merchant": map[string]any{"type": "string"},
				"total":    map[string]any{"type": "number"},
				"currency": map[string]any{"type": "string"},
			},
			"required": []any{"merchant", "total"},
		},
	}
	receipt, err := needle.ExtractRaw(ctx,
		"GreenMart receipt: oat milk 3.50, total 7.75 paid by visa", schema)
	if err != nil {
		log.Fatal(err)
	}
	if blob, err := json.Marshal(receipt); err == nil {
		fmt.Printf("raw:        %s\n", blob)
	}

	// Nested records work exactly like flat ones.
	contact, err := needle.Extract[Contact](ctx,
		"John Doe can be reached at john@doe.com or +1 555 0100")
	if err != nil {
		log.Fatal(err)
	}
	if contact != nil {
		fmt.Printf("nested:     name=%q email=%q phone=%q\n",
			contact.Name, contact.Email, contact.Phone)
	}
}
