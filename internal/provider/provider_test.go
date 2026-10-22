package provider_test

import (
	"context"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/provider"
)

// A prompt sent twice, as the loss proposer's two runs send it, gets its
// recorded replies in order, and a third call has none left.
func TestReplay_ServesAPromptsRepliesInOrder(t *testing.T) {
	p := provider.NewReplay(map[string][]provider.Reply{"u": {{Text: "first"}, {Text: "second"}}})
	for _, want := range []string{"first", "second"} {
		got, err := p.Call(context.Background(), provider.Request{User: "u"})
		if err != nil || got.Text != want {
			t.Errorf("Call = %q, %v; want %q", got.Text, err, want)
		}
	}
	if _, err := p.Call(context.Background(), provider.Request{User: "u"}); err == nil {
		t.Error("third call succeeded with two recorded replies")
	}
	if _, err := p.Call(context.Background(), provider.Request{User: "other"}); err == nil {
		t.Error("call for an unrecorded prompt succeeded")
	}
	if n := len(p.Calls()); n != 4 {
		t.Errorf("%d calls recorded, want 4", n)
	}
}
