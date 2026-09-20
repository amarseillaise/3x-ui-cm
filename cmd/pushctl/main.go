// Command pushctl drives the admin API of 3x-ui-cm from the shell.
//
// Configuration: APP_BASE_URL and ADMIN_TOKEN from the environment or from a
// .env file in the current directory.
//
//	pushctl send -title "Текст" [-body "..."] [-url /renew] (-all | -sub id1,id2 | -email a,b)
//	pushctl pending                 list unconfirmed renewal requests
//	pushctl history [-limit 50]     list recent requests of any status
//	pushctl confirm <id>            mark a request as paid
//	pushctl reject <id>             roll the days back and reject
//	pushctl link -email <email>     print the client's subscription link (stdout) and direct cabinet link (stderr)
//	pushctl stats                   counters
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type client struct {
	base  string
	token string
	http  *http.Client
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	loadDotEnv(".env")
	c := &client{base: strings.TrimRight(os.Getenv("APP_BASE_URL"), "/"), token: os.Getenv("ADMIN_TOKEN"), http: &http.Client{Timeout: 60 * time.Second}}
	if c.base == "" || c.token == "" {
		fmt.Fprintln(os.Stderr, "error: APP_BASE_URL and ADMIN_TOKEN must be set (env or .env)")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "send":
		err = c.send(os.Args[2:])
	case "pending":
		err = c.list("pending", 100)
	case "history":
		fs := flag.NewFlagSet("history", flag.ExitOnError)
		limit := fs.Int("limit", 50, "max rows")
		_ = fs.Parse(os.Args[2:])
		err = c.list("", *limit)
	case "confirm", "reject":
		if len(os.Args) < 3 {
			err = errors.New("request id required")
			break
		}
		err = c.resolve(os.Args[2], os.Args[1])
	case "link":
		fs := flag.NewFlagSet("link", flag.ExitOnError)
		email := fs.String("email", "", "client email in the panel")
		_ = fs.Parse(os.Args[2:])
		err = c.link(*email)
	case "stats":
		err = c.printJSON(http.MethodGet, "/api/admin/stats", nil)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: pushctl <command>
  send -title T [-body B] [-url U] (-all | -sub id,id | -email a,b)
  pending | history [-limit N] | confirm <id> | reject <id> | link -email E | stats`)
}

func (c *client) send(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	title := fs.String("title", "", "notification title (required)")
	body := fs.String("body", "", "notification text")
	url := fs.String("url", "", "path to open on tap, e.g. /renew")
	all := fs.Bool("all", false, "send to every client")
	subs := fs.String("sub", "", "comma-separated subscription ids")
	emails := fs.String("email", "", "comma-separated client emails")
	_ = fs.Parse(args)
	if *title == "" {
		return errors.New("-title is required")
	}
	target := map[string]any{}
	switch {
	case *all:
		target["all"] = true
	case *subs != "":
		target["subIds"] = splitList(*subs)
	case *emails != "":
		target["emails"] = splitList(*emails)
	default:
		return errors.New("choose a target: -all, -sub or -email")
	}
	return c.printJSON(http.MethodPost, "/api/admin/push", map[string]any{"title": *title, "body": *body, "url": *url, "target": target})
}

func (c *client) list(status string, limit int) error {
	path := fmt.Sprintf("/api/admin/renewals?status=%s&limit=%d", status, limit)
	var out struct {
		Requests []struct {
			ID          int64  `json:"id"`
			Email       string `json:"email"`
			PlanTitle   string `json:"planTitle"`
			Amount      int    `json:"amount"`
			Currency    string `json:"currency"`
			Status      string `json:"status"`
			AppliedDays int    `json:"appliedDays"`
			CreatedAt   string `json:"createdAt"`
			Error       string `json:"error"`
		} `json:"requests"`
	}
	if err := c.call(http.MethodGet, path, nil, &out); err != nil {
		return err
	}
	if len(out.Requests) == 0 {
		fmt.Println("no requests")
		return nil
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintf(w, "%-6s %-10s %-20s %-12s %8s %5s  %s\n", "ID", "STATUS", "CREATED", "PLAN", "AMOUNT", "DAYS", "EMAIL")
	for _, r := range out.Requests {
		created := r.CreatedAt
		if t, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil {
			created = t.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%-6d %-10s %-20s %-12s %5d %s %5d  %s", r.ID, r.Status, created, r.PlanTitle, r.Amount, r.Currency, r.AppliedDays, r.Email)
		if r.Error != "" {
			fmt.Fprintf(w, "  [%s]", r.Error)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func (c *client) resolve(id, action string) error {
	return c.printJSON(http.MethodPost, "/api/admin/renewals/"+id+"/"+action, nil)
}

func (c *client) link(email string) error {
	if email == "" {
		return errors.New("-email is required")
	}
	var out struct {
		URL        string `json:"url"`
		CabinetURL string `json:"cabinetUrl"`
	}
	if err := c.call(http.MethodGet, "/api/admin/link?email="+urlQueryEscape(email), nil, &out); err != nil {
		return err
	}
	fmt.Println(out.URL)
	if out.CabinetURL != "" && out.CabinetURL != out.URL {
		fmt.Fprintln(os.Stderr, "direct:", out.CabinetURL)
	}
	return nil
}

func (c *client) printJSON(method, path string, body any) error {
	var out json.RawMessage
	if err := c.call(method, path, body, &out); err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out, "", "  "); err != nil {
		fmt.Println(string(out))
		return nil
	}
	fmt.Println(pretty.String())
	return nil
}

func (c *client) call(method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s (%d): %s", e.Error, resp.StatusCode, e.Message)
		}
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '~':
			b.WriteByte(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}

// loadDotEnv sets variables from a simple KEY=VALUE file without overriding the environment.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists && key != "" {
			_ = os.Setenv(key, val)
		}
	}
}
