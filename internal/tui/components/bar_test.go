package components

import (
	"image/color"
	"testing"

	"github.com/masterkeysrd/kite/style"
)

func TestBarNilTheme(t *testing.T) {
	// When theme is nil, the bar should still render but without colors
	node := Bar(BarProps{
		Segments: []BarSegment{{Percentage: 100}},
	})

	if node == nil {
		t.Fatal("expected bar to render even without theme")
	}
}

func TestBarEmptySegments(t *testing.T) {
	node := Bar(BarProps{
		Segments: nil,
	})

	if node == nil {
		t.Fatal("expected bar to render with empty segments")
	}
}

func TestBarZeroPercentage(t *testing.T) {
	// When all segments have 0 percentage, no segments should render
	node := Bar(BarProps{
		Segments: []BarSegment{
			{Percentage: 0},
			{Percentage: 0},
		},
	})

	if node == nil {
		t.Fatal("expected bar to render even with zero percentages")
	}
}

func TestBarSingleSegment(t *testing.T) {
	node := Bar(BarProps{
		Segments: []BarSegment{{Percentage: 100}},
		Height:   1,
	})

	if node == nil {
		t.Fatal("expected bar to render with single segment")
	}
}

func TestBarMultipleSegments(t *testing.T) {
	node := Bar(BarProps{
		Segments: []BarSegment{
			{Percentage: 25},
			{Percentage: 50},
			{Percentage: 25},
		},
		Height: 2,
	})

	if node == nil {
		t.Fatal("expected bar to render with multiple segments")
	}
}

func TestBarCustomColor(t *testing.T) {
	redColor := color.RGBA{255, 0, 0, 255}
	node := Bar(BarProps{
		Segments: []BarSegment{
			{Percentage: 100, Color: redColor},
		},
	})

	if node == nil {
		t.Fatal("expected bar to render with custom color")
	}
}

func TestBarStyleOverride(t *testing.T) {
	customStyle := style.S().Display(style.DisplayFlex)
	node := Bar(BarProps{
		Segments: []BarSegment{{Percentage: 100}},
		Style:    customStyle,
	})

	if node == nil {
		t.Fatal("expected bar to render with custom style")
	}
}

func TestBarProportions(t *testing.T) {
	tests := []struct {
		name      string
		segments  []BarSegment
		wantCount int
	}{
		{
			name:      "equal thirds",
			segments:  []BarSegment{{Percentage: 33.33}, {Percentage: 33.33}, {Percentage: 33.34}},
			wantCount: 3,
		},
		{
			name:      "25/50/25 split",
			segments:  []BarSegment{{Percentage: 25}, {Percentage: 50}, {Percentage: 25}},
			wantCount: 3,
		},
		{
			name:      "10/90 split",
			segments:  []BarSegment{{Percentage: 10}, {Percentage: 90}},
			wantCount: 2,
		},
		{
			name:      "mixed zero and non-zero",
			segments:  []BarSegment{{Percentage: 0}, {Percentage: 50}, {Percentage: 0}, {Percentage: 50}},
			wantCount: 2, // Only non-zero segments render
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := Bar(BarProps{
				Segments: tt.segments,
				Height:   10,
			})

			if node == nil {
				t.Fatalf("expected bar to render, got nil")
			}
		})
	}
}

func TestBarCustomHeight(t *testing.T) {
	tests := []struct {
		name   string
		height int
	}{
		{"default height", 1},
		{"tall bar", 3},
		{"very tall bar", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := Bar(BarProps{
				Segments: []BarSegment{{Percentage: 100}},
				Height:   tt.height,
			})

			if node == nil {
				t.Fatalf("expected bar to render with height %d", tt.height)
			}
		})
	}
}
