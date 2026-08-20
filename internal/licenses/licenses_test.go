package licenses

import (
	"strings"
	"testing"
)

func TestThirdPartyNoticesAreEmbedded(t *testing.T) {
	for _, dependency := range []string{"github.com/coder/websocket", "@codemirror/view"} {
		if !strings.Contains(ThirdParty, "Component: "+dependency) {
			t.Errorf("notices do not contain %s", dependency)
		}
	}
}
