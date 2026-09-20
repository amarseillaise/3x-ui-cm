package xui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	listFixture = `{"success":true,"msg":"","obj":[
	  {"email":"alice","subId":"abc123","uuid":"secret","password":"secret","enable":true,"expiryTime":1804848456300,"totalGB":0,"tgId":0,"inboundIds":[1,2],
	   "traffic":{"inboundId":1,"enable":true,"email":"alice","subId":"abc123","up":10,"down":20,"expiryTime":1804848456300,"total":0,"reset":0,"lastOnline":1789505906388}},
	  {"email":"bob","subId":"abc1234","enable":false,"expiryTime":0,"totalGB":1073741824,"inboundIds":[1],"traffic":{"up":1,"down":2}}
	]}`
	pagedFixture = `{"success":true,"msg":"","obj":{"items":[
	  {"email":"alice","subId":"abc123","enable":true,"expiryTime":1804848456300,"totalGB":0,"inboundIds":[1,2]},
	  {"email":"bob","subId":"abc1234","enable":true,"expiryTime":0,"totalGB":0,"inboundIds":[1]}
	],"total":81,"filtered":2,"page":1,"pageSize":200}}`
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/base/", "tok", WithCacheTTL(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestServerStatusSendsBearer(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/base/panel/api/server/status" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer: %q", r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{"success":true,"msg":"","obj":{"panelVersion":"3.6.0","xray":{"state":"running","version":"26.7.28"}}}`)
	})
	s, err := c.ServerStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.PanelVersion != "3.6.0" || s.Xray.State != "running" {
		t.Errorf("decoded %+v", s)
	}
}

func TestFindBySubIDExactMatch(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search") == "" {
			t.Error("search query missing")
		}
		io.WriteString(w, pagedFixture)
	})
	got, err := c.FindBySubID(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Email != "alice" {
		t.Errorf("substring match leaked: %+v", got)
	}
	if _, err := c.FindBySubID(context.Background(), "zzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestListClientsCacheAndBySubID(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		io.WriteString(w, listFixture)
	})
	ctx := context.Background()
	if _, err := c.ListClients(ctx); err != nil {
		t.Fatal(err)
	}
	recs, err := c.ClientsBySubID(ctx, "abc123")
	if err != nil || len(recs) != 1 || recs[0].Used() != 30 {
		t.Fatalf("bySubID: %+v %v", recs, err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 HTTP call, got %d", calls)
	}
	c.InvalidateCache()
	if _, err := c.ListClients(ctx); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected refetch after invalidate, got %d calls", calls)
	}
}

func TestAPIErrorOnSuccessFalse(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":false,"msg":"client not found","obj":null}`)
	})
	_, err := c.GetClient(context.Background(), "nobody")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Msg != "client not found" {
		t.Fatalf("want APIError, got %v", err)
	}
}

func TestLoginRedirectIsAnError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/base/login", http.StatusFound)
	})
	_, err := c.ServerStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected response") {
		t.Fatalf("want unexpected response error, got %v", err)
	}
}

func TestGetRetriesOnceOnTransportError(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		io.WriteString(w, `{"success":true,"msg":"","obj":{"panelVersion":"3.6.0"}}`)
	})
	if _, err := c.ServerStatus(context.Background()); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d", calls)
	}
}

func TestBulkAdjustNeverRetriesAndInvalidates(t *testing.T) {
	var calls int32
	var gotBody map[string]any
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/base/panel/api/clients/list" {
			io.WriteString(w, listFixture)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		io.WriteString(w, `{"success":true,"msg":"","obj":{"adjusted":1,"skipped":{}}}`)
	})
	ctx := context.Background()
	_, _ = c.ListClients(ctx)
	_, err := c.BulkAdjust(ctx, BulkAdjustRequest{Emails: []string{"alice"}, AddDays: 30})
	if err == nil {
		t.Fatal("first bulkAdjust must fail without retry")
	}
	if calls != 1 {
		t.Fatalf("POST must not be retried, calls = %d", calls)
	}
	res, err := c.BulkAdjust(ctx, BulkAdjustRequest{Emails: []string{"alice"}, AddDays: 30})
	if err != nil || res.Adjusted != 1 {
		t.Fatalf("second call: %+v %v", res, err)
	}
	if gotBody["addDays"] != float64(30) || gotBody["addBytes"] != nil {
		t.Errorf("body = %v", gotBody)
	}
	c.mu.Lock()
	cached := c.cache
	c.mu.Unlock()
	if cached != nil {
		t.Error("cache must be invalidated after a write")
	}
}

func TestSubscriptionURL(t *testing.T) {
	s := &Settings{SubPort: 7115, SubPath: "/subway/", SubDomain: "sub.example.com", SubCertFile: "/root/cert.crt"}
	if got := s.SubscriptionURL("abc"); got != "https://sub.example.com:7115/subway/abc" {
		t.Errorf("got %s", got)
	}
	s.SubURI = "https://cdn.example.com/s/"
	if got := s.SubscriptionURL("abc"); got != "https://cdn.example.com/s/abc" {
		t.Errorf("subURI override: %s", got)
	}
}
