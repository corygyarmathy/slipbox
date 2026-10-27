package testfixture

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/provider"
)

// recorder is a testing.TB that keeps what a helper reports, so a test can
// check that the helper fails without failing itself. Its Fatalf ends the
// goroutine it is called on, as testing's does, so it is called on one of
// its own (run).
type recorder struct {
	testing.TB
	mu   sync.Mutex
	msgs []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

// run calls f with r on a goroutine of its own, and returns what r was told.
func (r *recorder) run(f func(testing.TB)) []string {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.msgs)
}

func TestFake_ServesWhatTheFunctionReturns(t *testing.T) {
	want := provider.Reply{Text: "a reply", Usage: provider.Usage{Turns: 1}}
	f := NewFake(func(req provider.Request) (provider.Reply, error) {
		if req.User != "a prompt" {
			t.Errorf("function got user prompt %q", req.User)
		}
		return want, nil
	})
	got, err := f.Call(context.Background(), provider.Request{User: "a prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != want.Text || got.Usage.Turns != want.Usage.Turns {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFake_RecordsEveryRequestInOrder(t *testing.T) {
	f := NewFake(func(provider.Request) (provider.Reply, error) { return provider.Reply{}, nil })
	reqs := []provider.Request{
		{Model: "m1", Effort: "low", System: "s1", User: "u1"},
		{Model: "m2", Effort: "high", System: "s2", User: "u2"},
	}
	for _, req := range reqs {
		if _, err := f.Call(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Calls(); !slices.Equal(got, reqs) {
		t.Errorf("Calls() = %+v, want %+v", got, reqs)
	}
}

// TestFake_RecordsConcurrentCalls makes calls from many goroutines at once,
// as a step's segments do; run it under -race.
func TestFake_RecordsConcurrentCalls(t *testing.T) {
	f := NewFake(func(req provider.Request) (provider.Reply, error) {
		return provider.Reply{Text: req.User}, nil
	})
	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			user := fmt.Sprint(i)
			reply, err := f.Call(context.Background(), provider.Request{User: user})
			if err != nil || reply.Text != user {
				t.Errorf("call %d: got %q, %v", i, reply.Text, err)
			}
		})
	}
	wg.Wait()
	var got []string
	for _, req := range f.Calls() {
		got = append(got, req.User)
	}
	slices.Sort(got)
	var want []string
	for i := range n {
		want = append(want, fmt.Sprint(i))
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Calls() users = %v, want each of %v once", got, want)
	}
}

func TestFake_PassesTheFunctionsErrorThrough(t *testing.T) {
	want := &provider.UsageLimited{Limit: provider.FiveHour}
	f := NewFake(func(provider.Request) (provider.Reply, error) { return provider.Reply{}, want })
	_, err := f.Call(context.Background(), provider.Request{})
	if err != want {
		t.Errorf("got error %v, want %v unchanged", err, want)
	}
}

func TestFake_ReturnsTheContextsErrorOnceCancelled(t *testing.T) {
	called := false
	f := NewFake(func(provider.Request) (provider.Reply, error) {
		called = true
		return provider.Reply{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Call(ctx, provider.Request{User: "u"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got error %v, want context.Canceled", err)
	}
	if called {
		t.Error("the function was called after the context was cancelled")
	}
	if got := len(f.Calls()); got != 1 {
		t.Errorf("recorded %d calls, want the cancelled one", got)
	}
}

func TestServe_ServesTheCaseThePromptContains(t *testing.T) {
	f := Serve(t,
		Case{Contains: "## The Lens", Reply: "lens reply"},
		Case{Contains: "## Storms", Reply: "storms reply"},
	)
	for _, tc := range []struct{ user, want string }{
		{"segment 2\n## The Lens\nbody", "lens reply"},
		{"segment 3\n## Storms\nbody", "storms reply"},
		// A case serves as often as it is asked.
		{"again\n## The Lens\n", "lens reply"},
	} {
		got, err := f.Call(context.Background(), provider.Request{User: tc.user})
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != tc.want {
			t.Errorf("user prompt %q: got %q, want %q", tc.user, got.Text, tc.want)
		}
	}
}

func TestServe_FailsWhenNoCaseMatches(t *testing.T) {
	r := &recorder{TB: t}
	var err error
	msgs := r.run(func(tb testing.TB) {
		_, err = Serve(tb, Case{Contains: "## The Lens", Reply: "x"}).
			Call(context.Background(), provider.Request{User: "## Storms"})
	})
	if err == nil {
		t.Error("the call didn't fail")
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "no case matches") || !strings.Contains(msgs[0], `"## Storms"`) {
		t.Errorf("the test was told %q, want one failure naming the prompt", msgs)
	}
}

func TestServe_FailsWhenTwoCasesMatch(t *testing.T) {
	r := &recorder{TB: t}
	var err error
	msgs := r.run(func(tb testing.TB) {
		_, err = Serve(tb, Case{Contains: "Lens", Reply: "x"}, Case{Contains: "The", Reply: "y"}).
			Call(context.Background(), provider.Request{User: "## The Lens"})
	})
	if err == nil {
		t.Error("the call didn't fail")
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], `"Lens", "The"`) || !strings.Contains(msgs[0], `"## The Lens"`) {
		t.Errorf("the test was told %q, want one failure naming both cases and the prompt", msgs)
	}
}
