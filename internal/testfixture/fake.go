package testfixture

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/provider"
)

// Fake is a provider whose replies the test decides: each call is answered
// by the function it was made with. It records every request, so a test can
// assert on the parts of a request that matter (ADR 0002 §8). It is safe for
// concurrent use.
type Fake struct {
	reply func(provider.Request) (provider.Reply, error)

	mu    sync.Mutex
	calls []provider.Request
}

// NewFake returns a Fake that answers each call with reply. reply may be
// called from several goroutines at once, as a step's segments run in
// parallel.
func NewFake(reply func(provider.Request) (provider.Reply, error)) *Fake {
	return &Fake{reply: reply}
}

// Call records req, then returns ctx's error if it is done, or else what the
// Fake's function returns for req, its error unchanged.
func (f *Fake) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return provider.Reply{}, err
	}
	return f.reply(req)
}

// Calls returns the requests made so far, in the order they arrived.
func (f *Fake) Calls() []provider.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.Request(nil), f.calls...)
}

// Case is a reply case Serve gives for any user prompt that contains
// Contains.
type Case struct {
	Contains string
	Reply    string
}

// Serve returns a Fake that answers a call with the Reply of the one case
// whose Contains is in the user prompt, as often as it is asked. A prompt no
// case matches, or more than one does, fails t, naming the prompt, and the
// call returns an error. Matching on a piece of the prompt, such as a
// segment's heading, leaves a test free of how the whole prompt is laid out.
// A test whose replies depend on more than that writes its own function for
// NewFake.
func Serve(t testing.TB, cases ...Case) *Fake {
	t.Helper()
	return NewFake(func(req provider.Request) (provider.Reply, error) {
		var match []int
		for i, c := range cases {
			if strings.Contains(req.User, c.Contains) {
				match = append(match, i)
			}
		}
		if len(match) == 1 {
			return provider.Reply{Text: cases[match[0]].Reply}, nil
		}
		var err error
		if len(match) == 0 {
			err = fmt.Errorf("testfixture: no case matches user prompt %q", req.User)
		} else {
			var which []string
			for _, i := range match {
				which = append(which, fmt.Sprintf("%q", cases[i].Contains))
			}
			err = fmt.Errorf("testfixture: cases %s all match user prompt %q", strings.Join(which, ", "), req.User)
		}
		// A step calls from its own goroutines, where t.Fatal mustn't be
		// called, so the test is marked failed and the call fails.
		t.Errorf("%v", err)
		return provider.Reply{}, err
	})
}
