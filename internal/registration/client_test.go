package registration

import "testing"

func TestNormalizeHubURL(t *testing.T) {
	for _, valid := range []string{"http://review.example", "https://review.example", "http://127.0.0.1:8080/"} {
		if _, err := NormalizeHubURL(valid); err != nil {
			t.Errorf("valid URL %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "ftp://review.example", "http://user@review.example", "http://review.example/path", "review.example"} {
		if _, err := NormalizeHubURL(invalid); err == nil {
			t.Errorf("invalid URL %q accepted", invalid)
		}
	}
}
