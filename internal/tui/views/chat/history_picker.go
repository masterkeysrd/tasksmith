package chat

import (
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
)

// HistoryPickerProps defines the properties for the HistoryPicker component.
type HistoryPickerProps struct {
	SetInputValue func(string)
}

// HistoryPicker is a searchable modal that lets the user filter and select past user inputs from the workspace database.
var HistoryPicker = kitex.FC("HistoryPicker", func(props HistoryPickerProps) kitex.Node {
	isOpen := active.UseModal() == "historypicker"
	if !isOpen {
		return nil
	}

	debouncedQuery, setDebouncedQuery := kitex.UseState("")
	searchTimer := kitex.UseRef[*time.Timer](nil)

	historyQuery := queries.UseGetInputHistory(api.GetInputHistoryRequest{
		Query: debouncedQuery(),
		Limit: 50,
	})

	var items []components.PickerItem
	if historyQuery.Data != nil {
		for _, input := range historyQuery.Data.Inputs {
			items = append(items, components.PickerItem{
				ID:    input,
				Label: input,
			})
		}
	}

	return components.Picker(components.PickerProps{
		IsOpen:      isOpen,
		Title:       "History Picker",
		Placeholder: "Search past inputs...",
		Items:       items,
		OnClose: func() {
			active.SetModal("")
		},
		OnSelect: func(item components.PickerItem) {
			props.SetInputValue(item.Label)
			active.SetModal("")
			mode.Set(mode.Insert)
		},
		OnSearchChange: func(q string) {
			if searchTimer.Current != nil {
				searchTimer.Current.Stop()
			}
			searchTimer.Current = time.AfterFunc(150*time.Millisecond, func() {
				setDebouncedQuery(q)
			})
		},
	})
})
