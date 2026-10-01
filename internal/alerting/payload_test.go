package alerting

import "testing"

func TestDashboardURL(t *testing.T) {
	cases := []struct {
		baseURL string
		want    string
	}{
		{"", ""},
		{"https://groundtruth.example.com", "https://groundtruth.example.com/workspaces/ws-1"},
		// An operator-typed trailing slash must not produce "//workspaces".
		{"https://groundtruth.example.com/", "https://groundtruth.example.com/workspaces/ws-1"},
		{"https://groundtruth.example.com//", "https://groundtruth.example.com/workspaces/ws-1"},
	}
	for _, tc := range cases {
		if got := dashboardURL(tc.baseURL, "ws-1"); got != tc.want {
			t.Errorf("dashboardURL(%q) = %q, want %q", tc.baseURL, got, tc.want)
		}
	}
}
