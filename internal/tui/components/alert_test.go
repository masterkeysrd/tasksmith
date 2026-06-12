package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
)

func TestAlert(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		node := Alert(AlertProps{
			Severity: AlertInfo,
			Children: []kitex.Node{
				kitex.Text("Something happened"),
			},
		})
		if node == nil {
			t.Fatal("Alert returned nil node")
		}
	})

	t.Run("Severities", func(t *testing.T) {
		severities := []AlertSeverity{AlertSuccess, AlertInfo, AlertWarning, AlertError}
		for _, s := range severities {
			node := Alert(AlertProps{
				Severity: s,
				ShowIcon: true,
				Children: []kitex.Node{
					kitex.Text("Status message"),
				},
			})
			if node == nil {
				t.Errorf("Alert with severity %s returned nil node", s)
			}
		}
	})

	t.Run("Variants", func(t *testing.T) {
		variants := []AlertVariant{AlertStandard, AlertOutlined}
		for _, v := range variants {
			node := Alert(AlertProps{
				Severity: AlertInfo,
				Variant:  v,
				Children: []kitex.Node{
					kitex.Text("Variant test"),
				},
			})
			if node == nil {
				t.Errorf("Alert with variant %s returned nil node", v)
			}
		}
	})

	t.Run("WithAction", func(t *testing.T) {
		node := Alert(AlertProps{
			Severity: AlertError,
			Action: Button(ButtonProps{
				Children: []kitex.Node{kitex.Text("Retry")},
			}),
			Children: []kitex.Node{
				kitex.Text("Failed to sync"),
			},
		})
		if node == nil {
			t.Fatal("Alert with action returned nil node")
		}
	})

	t.Run("CustomIcon", func(t *testing.T) {
		node := Alert(AlertProps{
			Severity: AlertInfo,
			Icon:     kitex.Text("?"),
			Children: []kitex.Node{
				kitex.Text("Custom icon test"),
			},
		})
		if node == nil {
			t.Fatal("Alert with custom icon returned nil node")
		}
	})
}
