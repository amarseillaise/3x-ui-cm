package httpapi

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/config"
	"github.com/amarseillaise/3x-ui-cm/internal/push"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

const (
	adminToken  = "admin-token-0123456789abcdef0123456789"
	adminSecret = "admin-link-secret-0123456789abcdef01234"
)

var defaultExpiry = time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

// panelState is the mutable part of the fake panel.
type panelState struct {
	mu          sync.Mutex
	expiry      int64
	bulkAdjusts []int // addDays values received
}

// fakePanel serves the subset of the 3x-ui API the handlers use.
func fakePanel(t *testing.T, state *panelState) *httptest.Server {
	t.Helper()
	alice := func(slim bool) string {
		state.mu.Lock()
		expiry := state.expiry
		state.mu.Unlock()
		traffic := `,"traffic":{"inboundId":1,"enable":true,"email":"alice","subId":"abc123xyz","up":100,"down":200,"expiryTime":` + itoa(expiry) + `,"total":0,"lastOnline":1789505906388}`
		if slim {
			traffic = ""
		}
		return `{"email":"alice","subId":"abc123xyz","enable":true,"expiryTime":` + itoa(expiry) + `,"totalGB":0,"inboundIds":[1,2]` + traffic + `}`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/panel/api/clients/bulkAdjust":
			var body struct {
				Emails  []string `json:"emails"`
				AddDays int      `json:"addDays"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			state.mu.Lock()
			if len(body.Emails) == 1 && body.Emails[0] == "alice" && state.expiry != 0 {
				state.expiry += int64(body.AddDays) * 86400000
			}
			state.bulkAdjusts = append(state.bulkAdjusts, body.AddDays)
			state.mu.Unlock()
			io.WriteString(w, `{"success":true,"msg":"","obj":{"adjusted":1,"skipped":{}}}`)
		case r.URL.Path == "/panel/api/server/status":
			io.WriteString(w, `{"success":true,"msg":"","obj":{"panelVersion":"3.6.0","xray":{"state":"running"}}}`)
		case r.URL.Path == "/panel/api/clients/list/paged":
			if r.URL.Query().Get("search") == "abc123xyz" {
				io.WriteString(w, `{"success":true,"msg":"","obj":{"items":[`+alice(true)+`],"total":1,"filtered":1}}`)
			} else {
				io.WriteString(w, `{"success":true,"msg":"","obj":{"items":[],"total":1,"filtered":0}}`)
			}
		case r.URL.Path == "/panel/api/clients/list":
			io.WriteString(w, `{"success":true,"msg":"","obj":[`+alice(false)+`]}`)
		case r.URL.Path == "/panel/api/clients/get/alice":
			io.WriteString(w, `{"success":true,"msg":"","obj":{"client":`+alice(true)+`,"inboundIds":[1,2],"usedTraffic":300}}`)
		case strings.HasPrefix(r.URL.Path, "/panel/api/clients/get/"):
			io.WriteString(w, `{"success":false,"msg":"client not found","obj":null}`)
		case r.URL.Path == "/panel/api/clients/onlines":
			io.WriteString(w, `{"success":true,"msg":"","obj":["alice"]}`)
		case r.URL.Path == "/panel/api/setting/all":
			io.WriteString(w, `{"success":true,"msg":"","obj":{"subEnable":true,"subPort":7115,"subPath":"/subway/","subDomain":"sub.example.com","subCertFile":"/c.crt","subURI":""}}`)
		default:
			t.Errorf("unexpected panel call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

type env struct {
	srv    *httptest.Server
	client *http.Client // keeps cookies, does not follow redirects
	store  *store.Store
	panel  *panelState
	now    time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	state := &panelState{expiry: defaultExpiry}
	panel := fakePanel(t, state)
	t.Cleanup(panel.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	x, err := xui.New(panel.URL, "tok", xui.WithCacheTTL(0))
	if err != nil {
		t.Fatal(err)
	}
	app, _ := config.ParseAppConfig([]byte("plans:\n  - {id: p30, title: T, days: 30, price: 180}\nrequisites:\n  - {label: L, value: V}\n"))
	e := config.Env{
		NodeURL: panel.URL, NodeAPIToken: "tok", AppBaseURL: "http://cab.test",
		AdminToken: adminToken, AdminLinkSecret: adminSecret, SessionSecret: strings.Repeat("s", 32),
		NotifyDays: []int{7, 3, 1}, TrustProxy: false,
	}
	pub, priv, _ := push.GenerateVAPID()
	sender := push.New(st, pub, priv, "mailto:t@example.com", nil)
	s := New(Deps{Env: e, App: app, Store: st, XUI: x, Push: sender, Static: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}})
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.renewal.SetClock(s.now)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &env{srv: srv, store: st, panel: state, now: now, client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (e *env) get(t *testing.T, path string, headers ...string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func decode(t *testing.T, res *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestClientMagicLinkAndMe(t *testing.T) {
	e := newEnv(t)
	if res := e.get(t, "/api/me"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me without session: %d", res.StatusCode)
	}
	res := e.get(t, "/s/abc123xyz")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("magic link: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	var sid *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "sid" {
			sid = c
		}
	}
	if sid == nil || !sid.HttpOnly || sid.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie missing or weak: %+v", sid)
	}

	res = e.get(t, "/api/me")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("me: %d", res.StatusCode)
	}
	var me meResponse
	decode(t, res, &me)
	if me.Subscription.Status != "active" || !me.Subscription.Online || me.Subscription.UsedBytes != 300 || me.Subscription.InboundCount != 2 {
		t.Errorf("subscription: %+v", me.Subscription)
	}
	if me.Subscription.SubscriptionURL != "https://sub.example.com:7115/subway/abc123xyz" {
		t.Errorf("sub url: %s", me.Subscription.SubscriptionURL)
	}
	if !me.Renewal.Enabled || !me.Renewal.CanRenew || me.Renewal.HasPending {
		t.Errorf("renewal: %+v", me.Renewal)
	}
	if !me.Push.Enabled || me.Push.VAPIDPublicKey == "" || me.Push.Subscribed {
		t.Errorf("push dto: %+v", me.Push)
	}

	// Client session must not reach admin routes.
	if res := e.get(t, "/api/admin/stats"); res.StatusCode != http.StatusForbidden {
		t.Errorf("client on admin route: %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/logout", nil)
	out, err := e.client.Do(req)
	if err != nil || out.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %v %d", err, out.StatusCode)
	}
	if res := e.get(t, "/api/me"); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("me after logout: %d", res.StatusCode)
	}
}

func TestClientMagicLinkRejectsUnknownAndMalformed(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/s/unknown123", "/s/ab", "/s/bad%20id!"} {
		res := e.get(t, p)
		if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/invalid-link") {
			t.Errorf("%s: %d %s", p, res.StatusCode, res.Header.Get("Location"))
		}
		if len(res.Cookies()) != 0 {
			t.Errorf("%s: no cookie expected", p)
		}
	}
}

func TestMagicLinkRateLimit(t *testing.T) {
	e := newEnv(t)
	var last int
	for i := 0; i < 12; i++ {
		last = e.get(t, "/s/unknown123").StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst, got %d", last)
	}
}

func TestAdminLinkAndBearer(t *testing.T) {
	e := newEnv(t)
	if res := e.get(t, "/a/wrong-secret"); res.Header.Get("Location") != "/invalid-link" {
		t.Fatalf("wrong secret: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	res := e.get(t, "/a/"+adminSecret)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/admin" {
		t.Fatalf("admin link: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	res = e.get(t, "/api/admin/link?email=alice")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("admin link by cookie: %d", res.StatusCode)
	}
	var link map[string]string
	decode(t, res, &link)
	if link["url"] != "https://sub.example.com:7115/subway/abc123xyz" {
		t.Errorf("url must be the subscription link: %s", link["url"])
	}
	if link["cabinetUrl"] != "http://cab.test/s/abc123xyz" {
		t.Errorf("cabinetUrl: %s", link["cabinetUrl"])
	}
	if res := e.get(t, "/api/admin/link?email=nobody"); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown email: %d", res.StatusCode)
	}
	if res := e.get(t, "/api/me"); res.StatusCode != http.StatusForbidden {
		t.Errorf("admin on client route: %d", res.StatusCode)
	}

	// Bearer token works without any cookie.
	fresh := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	out, err := fresh.Do(req)
	if err != nil || out.StatusCode != http.StatusOK {
		t.Fatalf("bearer: %v %d", err, out.StatusCode)
	}
	req.Header.Set("Authorization", "Bearer nope")
	out, _ = fresh.Do(req)
	if out.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad bearer: %d", out.StatusCode)
	}
}

func TestHealthzAndSPA(t *testing.T) {
	e := newEnv(t)
	res := e.get(t, "/healthz")
	var h map[string]string
	decode(t, res, &h)
	if h["panel"] != "ok" {
		t.Errorf("healthz: %v", h)
	}
	res = e.get(t, "/renew")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "<html>app</html>" {
		t.Errorf("spa fallback: %d %s", res.StatusCode, body)
	}
}

func (e *env) send(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestPushSubscribeLifecycle(t *testing.T) {
	e := newEnv(t)
	const body = `{"endpoint":"https://push.example.com/abc","expirationTime":null,"keys":{"p256dh":"BP","auth":"AU"}}`
	if res := e.send(t, http.MethodPost, "/api/push/subscribe", body); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("subscribe without session: %d", res.StatusCode)
	}
	e.get(t, "/s/abc123xyz")
	if res := e.send(t, http.MethodPost, "/api/push/subscribe", `{"endpoint":"","keys":{}}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid body: %d", res.StatusCode)
	}
	if res := e.send(t, http.MethodPost, "/api/push/subscribe", body); res.StatusCode != http.StatusNoContent {
		t.Fatalf("subscribe: %d", res.StatusCode)
	}
	var me meResponse
	decode(t, e.get(t, "/api/me"), &me)
	if !me.Push.Subscribed {
		t.Error("me must report subscribed")
	}
	subs, _ := e.store.ListPushSubscriptions(context.Background(), store.RoleClient, []string{"abc123xyz"})
	if len(subs) != 1 || subs[0].P256dh != "BP" {
		t.Errorf("stored: %+v", subs)
	}
	if res := e.send(t, http.MethodDelete, "/api/push/subscribe", `{"endpoint":"https://push.example.com/abc"}`); res.StatusCode != http.StatusNoContent {
		t.Fatalf("unsubscribe: %d", res.StatusCode)
	}
	decode(t, e.get(t, "/api/me"), &me)
	if me.Push.Subscribed {
		t.Error("me must report unsubscribed")
	}
}

func (e *env) adminReq(t *testing.T, method, path string, body ...string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if len(body) > 0 {
		rdr = strings.NewReader(body[0])
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rdr)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestRenewalTrustFlow(t *testing.T) {
	e := newEnv(t)
	e.get(t, "/s/abc123xyz")

	var plans struct {
		Enabled    bool          `json:"enabled"`
		Plans      []config.Plan `json:"plans"`
		HasPending bool          `json:"hasPending"`
	}
	decode(t, e.get(t, "/api/plans"), &plans)
	if !plans.Enabled || len(plans.Plans) != 1 || plans.HasPending {
		t.Fatalf("plans: %+v", plans)
	}

	if res := e.send(t, http.MethodPost, "/api/renew", `{"planId":"nope"}`); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown plan: %d", res.StatusCode)
	}

	// Active subscription: +30 days from the current expiry.
	res := e.send(t, http.MethodPost, "/api/renew", `{"planId":"p30"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("renew: %d", res.StatusCode)
	}
	var out struct {
		Request   renewalDTO2 `json:"request"`
		ExpiresAt time.Time   `json:"expiresAt"`
	}
	decode(t, res, &out)
	wantExpiry := time.UnixMilli(defaultExpiry).Add(30 * 24 * time.Hour)
	if !out.ExpiresAt.Equal(wantExpiry) || out.Request.AppliedDays != 30 || out.Request.Status != "pending" || out.Request.Email != "" {
		t.Errorf("renew result: %+v expiresAt=%v", out.Request, out.ExpiresAt)
	}

	// A second claim is blocked while the first is pending.
	if res := e.send(t, http.MethodPost, "/api/renew", `{"planId":"p30"}`); res.StatusCode != http.StatusConflict {
		t.Errorf("second renew: %d", res.StatusCode)
	}
	var me meResponse
	decode(t, e.get(t, "/api/me"), &me)
	if !me.Renewal.HasPending || me.Renewal.CanRenew {
		t.Errorf("me after renew: %+v", me.Renewal)
	}

	// Admin sees it with the email and confirms.
	var list struct {
		Requests []renewalDTO2 `json:"requests"`
	}
	decode(t, e.adminReq(t, http.MethodGet, "/api/admin/renewals?status=pending"), &list)
	if len(list.Requests) != 1 || list.Requests[0].Email != "alice" {
		t.Fatalf("admin list: %+v", list.Requests)
	}
	id := strconv.FormatInt(list.Requests[0].ID, 10)
	if res := e.adminReq(t, http.MethodPost, "/api/admin/renewals/"+id+"/confirm"); res.StatusCode != http.StatusOK {
		t.Fatalf("confirm: %d", res.StatusCode)
	}
	if res := e.adminReq(t, http.MethodPost, "/api/admin/renewals/"+id+"/confirm"); res.StatusCode != http.StatusConflict {
		t.Errorf("double confirm: %d", res.StatusCode)
	}

	// New claim, then rejection rolls the days back exactly.
	if res := e.send(t, http.MethodPost, "/api/renew", `{"planId":"p30"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("renew after confirm: %d", res.StatusCode)
	}
	decode(t, e.adminReq(t, http.MethodGet, "/api/admin/renewals?status=pending"), &list)
	id = strconv.FormatInt(list.Requests[0].ID, 10)
	if res := e.adminReq(t, http.MethodPost, "/api/admin/renewals/"+id+"/reject"); res.StatusCode != http.StatusOK {
		t.Fatalf("reject: %d", res.StatusCode)
	}
	e.panel.mu.Lock()
	gotExpiry, adjusts := e.panel.expiry, e.panel.bulkAdjusts
	e.panel.mu.Unlock()
	if gotExpiry != wantExpiry.UnixMilli() {
		t.Errorf("expiry after reject = %v, want %v", time.UnixMilli(gotExpiry), wantExpiry)
	}
	if len(adjusts) != 3 || adjusts[0] != 30 || adjusts[1] != 30 || adjusts[2] != -30 {
		t.Errorf("bulkAdjust calls = %v", adjusts)
	}

	var mine struct {
		Requests []renewalDTO2 `json:"requests"`
	}
	decode(t, e.get(t, "/api/renewals"), &mine)
	if len(mine.Requests) != 2 || mine.Requests[0].Status != "rejected" || mine.Requests[1].Status != "confirmed" {
		t.Errorf("my renewals: %+v", mine.Requests)
	}
	for _, r := range mine.Requests {
		if r.Email != "" {
			t.Error("client view must not include email")
		}
	}
	decode(t, e.get(t, "/api/me"), &me)
	if me.Renewal.HasPending || !me.Renewal.CanRenew {
		t.Errorf("me after reject: %+v", me.Renewal)
	}
}

func TestRenewalExpiredAddsOverdueDays(t *testing.T) {
	e := newEnv(t)
	e.panel.mu.Lock()
	e.panel.expiry = e.now.Add(-36 * time.Hour).UnixMilli() // expired 1.5 days ago
	e.panel.mu.Unlock()
	e.get(t, "/s/abc123xyz")
	res := e.send(t, http.MethodPost, "/api/renew", `{"planId":"p30"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("renew: %d", res.StatusCode)
	}
	var out struct {
		Request   renewalDTO2 `json:"request"`
		ExpiresAt time.Time   `json:"expiresAt"`
	}
	decode(t, res, &out)
	if out.Request.AppliedDays != 32 {
		t.Errorf("appliedDays = %d, want 32 (30 + 2 overdue)", out.Request.AppliedDays)
	}
	if out.ExpiresAt.Before(e.now.Add(30 * 24 * time.Hour)) {
		t.Errorf("client must get at least 30 days from now, got %v", out.ExpiresAt)
	}
}

func TestRenewalUnlimitedRejected(t *testing.T) {
	e := newEnv(t)
	e.panel.mu.Lock()
	e.panel.expiry = 0
	e.panel.mu.Unlock()
	e.get(t, "/s/abc123xyz")
	if res := e.send(t, http.MethodPost, "/api/renew", `{"planId":"p30"}`); res.StatusCode != http.StatusConflict {
		t.Errorf("unlimited renew: %d", res.StatusCode)
	}
	var me meResponse
	decode(t, e.get(t, "/api/me"), &me)
	if me.Renewal.CanRenew {
		t.Error("unlimited must not be renewable")
	}
}

func TestAdminBroadcast(t *testing.T) {
	e := newEnv(t)
	var received int32
	pushSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(pushSvc.Close)

	// A real-looking browser subscription for alice.
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	e.get(t, "/s/abc123xyz")
	body := `{"endpoint":"` + pushSvc.URL + `/alice","keys":{"p256dh":"` + base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()) + `","auth":"` + base64.RawURLEncoding.EncodeToString(auth) + `"}}`
	if res := e.send(t, http.MethodPost, "/api/push/subscribe", body); res.StatusCode != http.StatusNoContent {
		t.Fatalf("subscribe: %d", res.StatusCode)
	}

	if res := e.adminReq(t, http.MethodPost, "/api/admin/push", `{"title":"","body":"x","target":{"all":true}}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("empty title: %d", res.StatusCode)
	}
	if res := e.adminReq(t, http.MethodPost, "/api/admin/push", `{"title":"t","target":{"all":true,"emails":["alice"]}}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("two targets: %d", res.StatusCode)
	}
	if res := e.adminReq(t, http.MethodPost, "/api/admin/push", `{"title":"t","target":{"emails":["nobody"]}}`); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown email only: %d", res.StatusCode)
	}

	var out struct {
		Recipients, Sent, Failed, Removed int
		UnknownEmails                     []string `json:"unknownEmails"`
	}
	res := e.adminReq(t, http.MethodPost, "/api/admin/push", `{"title":"Привет","body":"Тест","url":"/","target":{"emails":["alice","ghost"]}}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("broadcast by email: %d", res.StatusCode)
	}
	decode(t, res, &out)
	if out.Recipients != 1 || out.Sent != 1 || len(out.UnknownEmails) != 1 || out.UnknownEmails[0] != "ghost" {
		t.Errorf("result: %+v", out)
	}
	res = e.adminReq(t, http.MethodPost, "/api/admin/push", `{"title":"Всем","target":{"all":true}}`)
	decode(t, res, &out)
	if out.Sent != 1 {
		t.Errorf("all: %+v", out)
	}
	if atomic.LoadInt32(&received) != 2 {
		t.Errorf("push service received %d", received)
	}
	if n, _ := e.store.CountNotifications(context.Background()); n != 2 {
		t.Errorf("notifications logged = %d", n)
	}
}
