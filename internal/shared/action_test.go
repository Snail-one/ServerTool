package shared

import "testing"

func TestIsReturnChoice(t *testing.T) {
	for _, choice := range []string{"0", "q", "Q", "exit", " EXIT "} {
		if !IsReturnChoice(choice) {
			t.Fatalf("IsReturnChoice(%q) = false", choice)
		}
	}
	for _, choice := range []string{"", "1", "quit"} {
		if IsReturnChoice(choice) {
			t.Fatalf("IsReturnChoice(%q) = true", choice)
		}
	}
}
