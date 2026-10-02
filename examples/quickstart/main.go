// Quickstart asks one question of each type about a support ticket, using the
// wire-level union types.
//
//	TYPESAFE_API_KEY=... go run ./examples/quickstart
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/oplachkoam/jev-go-sdk"
)

func main() {
	client := jev.NewClient() // reads TYPESAFE_API_KEY

	res, err := client.SystemOne.New(context.Background(), jev.SystemOneNewParams{
		State: "Help! My payouts have been failing for 3 days.",
		Questions: map[string]jev.QuestionUnionParam{
			"urgent": jev.QuestionParamOfNoul("Does this convey urgency?"),
			"team": jev.QuestionParamOfChoice("Which team should handle this?",
				jev.ChoiceOptionParam{Name: "billing", Description: "Payments, invoicing, refunds"},
				jev.ChoiceOptionParam{Name: "technical", Description: "Bugs, outages, integrations"},
				jev.ChoiceOptionParam{Name: "sales", Description: "Pricing, upgrades, new accounts"},
			),
			"frustration": jev.QuestionParamOfScore("How frustrated is the customer?",
				"Calm", "Frustrated", "Very angry",
			),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("answered by %s using %d input tokens\n", res.Model, res.Usage.InputTokens)

	for key, answer := range res.Answers {
		switch answer := answer.AsAny().(type) {
		case jev.NoulAnswer:
			fmt.Printf("%s: yes with probability %.2f\n", key, answer.Noul)
		case jev.ChoiceAnswer:
			fmt.Printf("%s: %s (confidence %.2f)\n", key, answer.Choice, answer.Confidence)
		case jev.ScoreAnswer:
			fmt.Printf("%s: %.2f (confidence %.2f)\n", key, answer.Score, answer.Confidence)
			for _, level := range answer.Levels() {
				fmt.Printf("  %d %-12v %.2f\n", level.Level, level.Description, level.Probability)
			}
		}
	}
}
