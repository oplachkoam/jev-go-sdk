// Package jev is a Go client for Jev, the System One model by TypeSafe AI.
//
// Jev does not generate text. A request carries a state (the content to
// evaluate) and a set of named, typed questions, and the response carries one
// structured, probabilistic answer per question:
//
//   - a Noul question is a yes/no question answered with the probability of yes;
//   - a Choice question picks one option from a set and reports the probability
//     of every option;
//   - a Score question places the state on an ordered scale of described levels.
//
// Create a client with [NewClient], which reads TYPESAFE_API_KEY from the
// environment, and ask questions with [SystemOneService.New]:
//
//	client := jev.NewClient()
//
//	urgent := jev.Noul("urgent", "Does this convey urgency?")
//	team := jev.Choice("team", "Which team should handle this?",
//		jev.Option("billing", "Payments, invoicing, refunds"),
//		jev.Option("technical", "Bugs, outages, integrations"),
//	)
//
//	res, err := client.SystemOne.New(ctx, jev.SystemOneNewParams{
//		State:     "Help! My payouts have been failing for 3 days.",
//		Questions: jev.Questions(urgent, team),
//	})
//	if err != nil {
//		return err
//	}
//	isUrgent, err := urgent.Answer(res) // jev.NoulAnswer
//	picked, err := team.Answer(res)     // jev.TypedChoiceAnswer[string]
//
// The typed question values built by [Noul], [Choice] and [Score] remember
// their key and know the type of their answer. They are a layer over the wire
// types, [QuestionUnionParam] and [AnswerUnion], which can be used directly
// when the questions are not known at compile time.
//
// Clients, services and individual calls are configured with the functional
// options of the option package, such as option.WithAPIKey and
// option.WithBaseURL.
//
// This is a community SDK and is not affiliated with TypeSafe AI.
package jev
