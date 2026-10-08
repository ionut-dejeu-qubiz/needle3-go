// Command needle3-extraction extracts a structured record with Needle 3.
//
//	go run ./examples/needle3-extraction
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/FlameInTheDark/needle-go"
)

// Shipment is the record extracted from the support note below. The schema
// is derived from the struct tags, just like a tool's argument schema.
type Shipment struct {
	OrderID      string `needle:"order_id" desc:"order identifier" required:"true"`
	Customer     string `needle:"customer" desc:"customer name" required:"true"`
	Status       string `needle:"status" desc:"shipment status" required:"true"`
	DeliveryDate string `needle:"delivery_date" desc:"promised delivery date" format:"date"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	shipment, err := needle.Extract[Shipment](ctx,
		"Support note: order N3-1042 for Maya Chen is delayed and is now expected on 2026-10-14.",
		needle.ExtractGeneration(3),
	)
	if err != nil {
		log.Fatal(err)
	}
	if shipment == nil {
		fmt.Println("no shipment found")
		return
	}

	fmt.Printf("order=%q customer=%q status=%q delivery=%q\n",
		shipment.OrderID,
		shipment.Customer,
		shipment.Status,
		shipment.DeliveryDate,
	)
}
