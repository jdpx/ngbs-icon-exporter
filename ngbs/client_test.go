package ngbs

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func mustJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return jar
}

// newTestServer returns a server mimicking the controller: it requires a login
// POST before datapoll returns JSON, and serves the HTML login page for a
// datapoll made without a session cookie.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile("testdata/datapoll.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	const cookie = "PHPSESSID=testsession"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch {
		case r.FormValue("tab") == "login":
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "testsession"})
			w.Write([]byte(`{"result":"success","refresh":true}`))
		case r.FormValue("tab") == "datapoll":
			if !strings.Contains(r.Header.Get("Cookie"), cookie) {
				w.Write([]byte("<!DOCTYPE html><html><body>login</body></html>"))
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write(body)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestPoll(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	c, err := New(srv.URL, "500225070340", "", 5*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	data, err := c.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if data.SysID != "500225070340" {
		t.Errorf("SysID = %q, want 500225070340", data.SysID)
	}
	if data.HC != 1 {
		t.Errorf("system HC = %d, want 1 (cooling)", data.HC)
	}
	if data.WTemp != 24.3 {
		t.Errorf("WTemp = %v, want 24.3", data.WTemp)
	}
	if len(data.DP) != 5 {
		t.Errorf("got %d thermostats, want 5", len(data.DP))
	}
	r := data.DP["1.1"]
	if strings.TrimSpace(r.Name) != "Nappali" || r.Temp != 23.2 || r.RH != 56.9 {
		t.Errorf("room 1.1 = %+v, want Nappali 23.2/56.9", r)
	}
}

func TestPollReloginOnExpiry(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	c, err := New(srv.URL, "500225070340", "", 5*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Poll(context.Background()); err != nil {
		t.Fatalf("first Poll: %v", err)
	}
	// Drop the session cookie to simulate expiry; Poll must transparently
	// re-login and still succeed.
	c.http.Jar = mustJar(t)
	if _, err := c.Poll(context.Background()); err != nil {
		t.Fatalf("Poll after expiry: %v", err)
	}
}

func TestLoginRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":"error","reason":"bad sysid"}`))
	}))
	defer srv.Close()

	c, _ := New(srv.URL, "wrong", "", 5*time.Second)
	if _, err := c.Poll(context.Background()); err == nil {
		t.Fatal("expected login rejection error, got nil")
	}
}
