package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

// A refusal for asking too often is told as what it is, with how long to
// wait; anything else keeps what Let's Encrypt said.
func TestACMEFailure(t *testing.T) {
	limited := &acme.Error{
		StatusCode:  429,
		ProblemType: "urn:ietf:params:acme:error:rateLimited",
		Detail:      "too many certificates (5) already issued for this exact set of identifiers in the last 168h0m0s, retry after 2026-10-13 09:00:00 UTC",
		Header:      http.Header{"Retry-After": {"120"}},
	}
	msg, pause := acmeFailure(fmt.Errorf("order: %w", limited))
	if !strings.Contains(msg, "too many were requested") || !strings.Contains(msg, "retry after 2026-10-13") || pause != 2*time.Minute {
		t.Fatalf("rate limit: %q, %s", msg, pause)
	}
	limited.Header = http.Header{"Retry-After": {"999999"}}
	if _, pause := acmeFailure(limited); pause != time.Hour {
		t.Fatalf("a long wait is asked about again within the hour, got %s", pause)
	}
	msg, pause = acmeFailure(&acme.Error{ProblemType: "urn:ietf:params:acme:error:dns", Detail: "no valid A records found"})
	if msg != "Let's Encrypt: no valid A records found" || pause != time.Minute {
		t.Fatalf("other refusal: %q, %s", msg, pause)
	}
	if msg, _ := acmeFailure(errors.New("dial tcp: i/o timeout")); msg != "Let's Encrypt: dial tcp: i/o timeout" {
		t.Fatalf("plain error: %q", msg)
	}
}
