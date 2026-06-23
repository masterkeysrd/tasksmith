package analytics

import (
	"github.com/masterkeysrd/kite/extras/kites"
)

type viewState struct {
	timeframe          string // "today", "7days", "30days"
	metricUnit         string // "calls", "tokens"
	providerFilter     string // "ALL" or custom provider name
	activeTab          string // "SUMMARY", "PROJECTS", "TOOLS"
	availableProviders []string
}

var store = kites.Create(viewState{
	timeframe:      "7days",
	metricUnit:     "calls",
	providerFilter: "ALL",
	activeTab:      "SUMMARY",
})

// SetTimeframe updates the analytics timeframe.
func SetTimeframe(t string) {
	store.Set(func(s viewState) viewState {
		s.timeframe = t
		return s
	})
}

// UseTimeframe returns the timeframe reactively.
func UseTimeframe() string {
	return kites.Use(store, func(s viewState) string {
		return s.timeframe
	})
}

// SetMetricUnit updates the analytics metric unit.
func SetMetricUnit(m string) {
	store.Set(func(s viewState) viewState {
		s.metricUnit = m
		return s
	})
}

// UseMetricUnit returns the metric unit reactively.
func UseMetricUnit() string {
	return kites.Use(store, func(s viewState) string {
		return s.metricUnit
	})
}

// SetProviderFilter updates the provider filter.
func SetProviderFilter(p string) {
	store.Set(func(s viewState) viewState {
		s.providerFilter = p
		return s
	})
}

// UseProviderFilter returns the provider filter reactively.
func UseProviderFilter() string {
	return kites.Use(store, func(s viewState) string {
		return s.providerFilter
	})
}

// SetActiveTab updates the active analytics tab.
func SetActiveTab(tab string) {
	store.Set(func(s viewState) viewState {
		s.activeTab = tab
		return s
	})
}

// UseActiveTab returns the active tab reactively.
func UseActiveTab() string {
	return kites.Use(store, func(s viewState) string {
		return s.activeTab
	})
}

// SetAvailableProviders updates the list of available providers.
func SetAvailableProviders(list []string) {
	store.Set(func(s viewState) viewState {
		s.availableProviders = list
		return s
	})
}

// CycleProviderFilter cycles through the available provider filters.
func CycleProviderFilter() {
	store.Set(func(s viewState) viewState {
		list := []string{"ALL"}
		for _, p := range s.availableProviders {
			if p != "" && p != "ALL" {
				list = append(list, p)
			}
		}
		currIdx := 0
		for i, p := range list {
			if p == s.providerFilter {
				currIdx = i
				break
			}
		}
		nextIdx := (currIdx + 1) % len(list)
		s.providerFilter = list[nextIdx]
		return s
	})
}

// CycleActiveTab cycles through the active tabs.
func CycleActiveTab() {
	store.Set(func(s viewState) viewState {
		tabs := []string{"SUMMARY", "PROJECTS", "TOOLS"}
		currIdx := 0
		for i, t := range tabs {
			if t == s.activeTab {
				currIdx = i
				break
			}
		}
		nextIdx := (currIdx + 1) % len(tabs)
		s.activeTab = tabs[nextIdx]
		return s
	})
}

// ToggleMetricUnit toggles between calls and tokens.
func ToggleMetricUnit() {
	store.Set(func(s viewState) viewState {
		if s.metricUnit == "calls" {
			s.metricUnit = "tokens"
		} else {
			s.metricUnit = "calls"
		}
		return s
	})
}
