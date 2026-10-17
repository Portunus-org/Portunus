package api

import (
	"strings"
	"testing"
)

// Length limits count letters, not bytes: names in Cyrillic, two bytes a
// letter in UTF-8, are let in up to the same length as Latin ones.
func TestTextLimitsCountLetters(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

	// The catalog's longest names, as the panel in Russian gives them.
	for _, name := range []string{"Россия: для пользователей внутри страны", "Россия: для пользователей за рубежом"} {
		if resp, body := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": name, "content": "example.com"}, h); resp.StatusCode != 201 {
			t.Fatalf("list %q: %d %v", name, resp.StatusCode, body)
		}
	}
	long := strings.Repeat("ж", 64)
	if resp, body := e.do(t, "POST", "/api/clients", map[string]any{"name": long, "enabled": true, "note": strings.Repeat("я", 500)}, h); resp.StatusCode != 201 {
		t.Fatalf("client of 64 letters with a note of 500: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "POST", "/api/clients", map[string]any{"name": long + "ж", "enabled": true}, h); resp.StatusCode != 400 || body["code"] != "too_long" {
		t.Fatalf("client of 65 letters: %d %v", resp.StatusCode, body)
	}
}
