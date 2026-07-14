package chat

import (
	"testing"
)

func TestGetPreviewAndDetails(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		maxLines    int
		needDetails bool
		wantPreview string
		wantDetails string
		wantHasMore bool
	}{
		{
			name:        "Empty content",
			content:     "",
			maxLines:    3,
			needDetails: false,
			wantPreview: "",
			wantDetails: "",
			wantHasMore: false,
		},
		{
			name:        "Fewer lines than maxLines",
			content:     "line1\nline2",
			maxLines:    5,
			needDetails: true,
			wantPreview: "line1\nline2",
			wantDetails: "",
			wantHasMore: false,
		},
		{
			name:        "Exactly maxLines",
			content:     "1\n2\n3",
			maxLines:    3,
			needDetails: true,
			wantPreview: "1\n2\n3",
			wantDetails: "",
			wantHasMore: false,
		},
		{
			name:        "More lines without details requested",
			content:     "1\n2\n3\n4\n5",
			maxLines:    3,
			needDetails: false,
			wantPreview: "1\n2\n3",
			wantDetails: "",
			wantHasMore: true,
		},
		{
			name:        "More lines with details requested",
			content:     "1\n2\n3\n4\n5",
			maxLines:    3,
			needDetails: true,
			wantPreview: "1\n2\n3",
			wantDetails: "4\n5",
			wantHasMore: true,
		},
		{
			name:        "More lines trailing newlines/spaces",
			content:     "1\n2\n3\n  \n  ",
			maxLines:    3,
			needDetails: true,
			wantPreview: "1\n2\n3",
			wantDetails: "",
			wantHasMore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPreview, gotDetails, gotHasMore := getPreviewAndDetails(tt.content, tt.maxLines, tt.needDetails)
			if gotPreview != tt.wantPreview {
				t.Errorf("getPreviewAndDetails() gotPreview = %q, want %q", gotPreview, tt.wantPreview)
			}
			if gotDetails != tt.wantDetails {
				t.Errorf("getPreviewAndDetails() gotDetails = %q, want %q", gotDetails, tt.wantDetails)
			}
			if gotHasMore != tt.wantHasMore {
				t.Errorf("getPreviewAndDetails() gotHasMore = %v, want %v", gotHasMore, tt.wantHasMore)
			}
		})
	}
}

func TestCountRemainingLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
		want     int
	}{
		{
			name:     "Fewer lines",
			content:  "1\n2",
			maxLines: 5,
			want:     0,
		},
		{
			name:     "Exactly maxLines",
			content:  "1\n2\n3",
			maxLines: 3,
			want:     0,
		},
		{
			name:     "One more line",
			content:  "1\n2\n3\n4",
			maxLines: 3,
			want:     1,
		},
		{
			name:     "Multiple more lines",
			content:  "1\n2\n3\n4\n5\n6",
			maxLines: 3,
			want:     3,
		},
		{
			name:     "More lines trailing whitespace",
			content:  "1\n2\n3\n  \n  ",
			maxLines: 3,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countRemainingLines(tt.content, tt.maxLines)
			if got != tt.want {
				t.Errorf("countRemainingLines() = %v, want %v", got, tt.want)
			}
		})
	}
}
