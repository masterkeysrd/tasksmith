package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
)

func TestAccordion(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Accordion(AccordionProps{},
			AccordionSummary(AccordionSummaryProps{}, kitex.Text("Summary")),
			AccordionDetails(AccordionDetailsProps{}, kitex.Text("Details")),
		)
		if node == nil {
			t.Fatal("Accordion returned nil node")
		}
	})

	t.Run("DefaultExpanded", func(t *testing.T) {
		node := Accordion(AccordionProps{
			DefaultExpanded: true,
		},
			AccordionSummary(AccordionSummaryProps{}, kitex.Text("Summary")),
			AccordionDetails(AccordionDetailsProps{}, kitex.Text("Details")),
		)
		if node == nil {
			t.Fatal("Expanded Accordion returned nil node")
		}
	})

	t.Run("Controlled", func(t *testing.T) {
		expanded := true
		node := Accordion(AccordionProps{
			Expanded: &expanded,
		},
			AccordionSummary(AccordionSummaryProps{}, kitex.Text("Summary")),
			AccordionDetails(AccordionDetailsProps{}, kitex.Text("Details")),
		)
		if node == nil {
			t.Fatal("Controlled Accordion returned nil node")
		}
	})

	t.Run("OutOfOrderChildren", func(t *testing.T) {
		node := Accordion(AccordionProps{},
			AccordionDetails(AccordionDetailsProps{}, kitex.Text("Details")),
			AccordionSummary(AccordionSummaryProps{}, kitex.Text("Summary")),
		)
		if node == nil {
			t.Fatal("Out of order Accordion returned nil node")
		}
	})
}
