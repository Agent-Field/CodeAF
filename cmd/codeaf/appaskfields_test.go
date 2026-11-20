package main

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A QUESTION THAT ASKS FOR SEVERAL FIGURES NAMES EACH ONE ON THE WIRE (qloop
// run41/run42 fire-plan-ask t1): the card said only "Give me your numbers", and
// the person had to guess which numbers. The model's named blanks are what the
// app draws as labelled boxes, so they must reach the ask in the model's order,
// unnamed blanks left out.
func TestAppAskOfCarriesTheQuestionsNamedFields(t *testing.T) {
	q := session.Question{ID: 3, Kind: session.QuestionAsk, Head: "Your numbers for the FIRE plan", Input: session.InputShape{
		Kind: session.InputBlanks,
		Blanks: []session.Blank{
			{Label: "Monthly spend in Chennai", Kind: session.BlankNumber},
			{Label: "  "},
			{Label: "Savings so far", Kind: session.BlankNumber, Default: "0"},
			{Label: "Account type", Kind: session.BlankChoice, Choices: []string{"RRSP", "TFSA"}},
		},
	}}
	got := appAskOf("a", q).Fields
	want := []appwire.AskField{
		{Label: "Monthly spend in Chennai", Kind: "number"},
		{Label: "Savings so far", Kind: "number", Default: "0"},
		{Label: "Account type", Kind: "choice", Choices: []string{"RRSP", "TFSA"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}
	if f := appAskOf("a", session.Question{ID: 4, Kind: session.QuestionAsk, Head: "Which?"}).Fields; f != nil {
		t.Fatalf("a plain question grew fields: %#v", f)
	}
	secret := q
	secret.Input.Secret = true
	if f := appAskOf("a", secret).Fields; f != nil {
		t.Fatalf("a secret question grew fields: %#v", f)
	}
}
