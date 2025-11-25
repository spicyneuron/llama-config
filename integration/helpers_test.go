package integration

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/spicyneuron/llama-matchmaker/config"
)

func newPatternField(patterns ...string) config.PatternField {
	const regexFlags = "(?i)"
	pf := config.PatternField{
		Patterns: patterns,
		Compiled: make([]*regexp.Regexp, len(patterns)),
	}
	for i, pattern := range patterns {
		pf.Compiled[i] = regexp.MustCompile(regexFlags + pattern)
	}
	return pf
}

func newTestConfig(target string, rules []config.Route) *config.Config {
	return &config.Config{
		Proxies: []config.ProxyConfig{{
			Listen: "localhost:0",
			Target: target,
		}},
		Routes: rules,
	}
}

// newSafeTestServer attempts to start a test server; skips the test if binding is not permitted.
func newSafeTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, func()) {
	t.Helper()

	var srv *httptest.Server
	var closed bool

	defer func() {
		if r := recover(); r != nil {
			if !closed {
				t.Skipf("Skipping test: unable to start test server (%v)", r)
			}
		}
	}()

	srv = httptest.NewServer(http.HandlerFunc(handler))
	return srv, func() {
		closed = true
		srv.Close()
	}
}
