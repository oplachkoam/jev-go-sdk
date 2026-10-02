// Typed routes a support ticket with typed questions, whose answers come back
// with the types of the questions, and handles API errors.
//
//	TYPESAFE_API_KEY=... go run ./examples/typed
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/oplachkoam/jev-go-sdk"
	"github.com/oplachkoam/jev-go-sdk/option"
)

type Team string

const (
	TeamBilling   Team = "billing"
	TeamTechnical Team = "technical"
	TeamSales     Team = "sales"
)

// Ticket is sent as structured state: Jev sees its fields by name.
type Ticket struct {
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Customer string `json:"customer_plan"`
}

func main() {
	client := jev.NewClient(
		option.WithMaxRetries(3),
		option.WithRequestTimeout(5*time.Second),
	)

	var (
		urgent = jev.Noul("urgent", "Does this convey urgency?").
			WithCriteria("Explicitly time-sensitive", "No urgency expressed")
		team = jev.Choice("team", "Which team should handle this?",
			jev.Option(TeamBilling, "Payments, invoicing, refunds"),
			jev.Option(TeamTechnical, "Bugs, outages, integrations"),
			jev.Option(TeamSales, "Pricing, upgrades, new accounts"),
		)
		frustration = jev.Score("frustration", "How frustrated is the customer?",
			"Calm", "Frustrated but civil", "Very angry",
		)
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := client.SystemOne.New(ctx, jev.SystemOneNewParams{
		State: Ticket{
			Subject:  "Payouts failing",
			Body:     "Help! My payouts have been failing for 3 days.",
			Customer: "enterprise",
		},
		Questions: jev.Questions(urgent, team, frustration),
	})
	if err != nil {
		var apierr *jev.Error
		if errors.As(err, &apierr) {
			log.Fatalf("the API answered %d (request %s): %s", apierr.StatusCode, apierr.RequestID, apierr.Message)
		}
		log.Fatal(err)
	}

	isUrgent, err := urgent.Answer(res)
	if err != nil {
		log.Fatal(err)
	}
	picked, err := team.Answer(res)
	if err != nil {
		log.Fatal(err)
	}
	mood, err := frustration.Answer(res)
	if err != nil {
		log.Fatal(err)
	}

	// The answer says what; the confidence says whether to act on it.
	switch {
	case picked.Confidence < 0.6:
		fmt.Println("route to a human: the model is not sure which team fits")
	case picked.Choice == TeamBilling:
		fmt.Printf("route to billing (p=%.2f)\n", picked.Probabilities[TeamBilling])
	default:
		fmt.Printf("route to %s\n", picked.Choice)
	}
	if isUrgent.Noul > 0.8 || mood.Score > 1.5 {
		fmt.Println("page the on-call lead")
	}
}
