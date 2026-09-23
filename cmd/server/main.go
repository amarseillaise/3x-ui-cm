// Command server runs the 3x-ui-cm HTTP server and background scheduler.
//
//	server serve      run the HTTP server (default)
//	server migrate    apply database migrations and exit
//	server gen-vapid  print a fresh VAPID key pair for .env
//	server sub-template  print the 3x-ui subscription-page template for APP_BASE_URL
//	server nginx-config  print the nginx server block for APP_BASE_URL
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amarseillaise/3x-ui-cm/internal/config"
	"github.com/amarseillaise/3x-ui-cm/internal/httpapi"
	"github.com/amarseillaise/3x-ui-cm/internal/nginxconf"
	"github.com/amarseillaise/3x-ui-cm/internal/notify"
	"github.com/amarseillaise/3x-ui-cm/internal/push"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
	"github.com/amarseillaise/3x-ui-cm/internal/subtemplate"
	"github.com/amarseillaise/3x-ui-cm/internal/webdist"
	"github.com/amarseillaise/3x-ui-cm/internal/xui"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = migrate()
	case "gen-vapid":
		err = genVAPID()
	case "sub-template":
		err = subTemplate()
	case "nginx-config":
		err = nginxConfig(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: server [serve|migrate|gen-vapid|sub-template|nginx-config]")
}

// nginxConfig prints the nginx server block for this deployment. Like
// sub-template it needs APP_BASE_URL only, so the full env is not validated.
func nginxConfig(args []string) error {
	fs := flag.NewFlagSet("nginx-config", flag.ContinueOnError)
	cert := fs.String("cert", os.Getenv("NGINX_CERT_FILE"), "path to fullchain.pem on the nginx host")
	key := fs.String("key", os.Getenv("NGINX_KEY_FILE"), "path to privkey.pem on the nginx host")
	upstream := fs.String("upstream", nginxconf.DefaultUpstream, "address the app listens on")
	redirect := fs.Bool("redirect-http", false, "also emit a port 80 block redirecting to https")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := nginxconf.FromAppBaseURL(os.Getenv("APP_BASE_URL"), *cert, *key)
	if err != nil {
		return err
	}
	cfg.Upstream = *upstream
	cfg.RedirectHTTP = *redirect
	out, err := nginxconf.Render(cfg)
	if err != nil {
		return err
	}
	_, err = os.Stdout.WriteString(out)
	return err
}

// subTemplate prints the panel template that redirects the subscription URL
// to the cabinet. Only APP_BASE_URL is needed, so the full env is not validated.
func subTemplate() error {
	out, err := subtemplate.Render(os.Getenv("APP_BASE_URL"))
	if err != nil {
		return err
	}
	_, err = os.Stdout.WriteString(out)
	return err
}

func genVAPID() error {
	pub, priv, err := push.GenerateVAPID()
	if err != nil {
		return err
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", pub, priv)
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func migrate() error {
	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	st, err := store.Open(env.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := st.Migrate(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("applied %d migration(s)\n", n)
	return nil
}

func serve() error {
	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	log := newLogger(env.LogLevel)
	slog.SetDefault(log)

	st, err := store.Open(env.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if n, err := st.Migrate(context.Background()); err != nil {
		return err
	} else if n > 0 {
		log.Info("migrations applied", "count", n)
	}

	app, err := config.LoadAppConfig(env.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn("config.yaml not found, renewal disabled", "path", env.ConfigPath)
		app = config.DefaultAppConfig()
	} else if err != nil {
		return err
	}

	panel, err := xui.New(env.NodeURL, env.NodeAPIToken, xui.WithInsecureTLS(env.NodeTLSInsecure), xui.WithLogger(log))
	if err != nil {
		return err
	}

	static, err := webdist.FS()
	if err != nil {
		return err
	}
	sender := push.New(st, env.VAPIDPublicKey, env.VAPIDPrivateKey, env.VAPIDSubject, log)
	if !sender.Enabled() {
		log.Warn("VAPID keys not set: Web Push disabled (run `server gen-vapid`)")
	}

	srv := httpapi.New(httpapi.Deps{
		Env:    env,
		App:    app,
		Store:  st,
		XUI:    panel,
		Push:   sender,
		Log:    log,
		Static: static,
	})
	httpServer := &http.Server{
		Addr:              env.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv.StartBackground(ctx)
	go notify.New(st, panel, sender, env.PollInterval, env.NotifyDays, env.NotifyTrafficPct, log).Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", env.ListenAddr, "db", env.DBPath, "push", env.PushEnabled(), "plans", len(app.Plans))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
