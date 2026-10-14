package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Cyberhunter88/cctv-viewer/internal/viewer"
	"github.com/Cyberhunter88/cctv-viewer/web"
	"golang.org/x/term"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	if e := run(); e != nil {
		slog.Error(e.Error())
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(version)
			return nil
		case "healthcheck":
			client := http.Client{Timeout: 3 * time.Second}
			r, e := client.Get("http://127.0.0.1:8080/healthz")
			if e != nil {
				return e
			}
			defer r.Body.Close()
			if r.StatusCode != 200 {
				return fmt.Errorf("nicht bereit: %d", r.StatusCode)
			}
			return nil
		case "admin":
			flags := flag.NewFlagSet("admin", flag.ContinueOnError)
			name := flags.String("username", "admin", "Benutzername")
			recoverLogin := flags.Bool("recover", false, "Lokale Anmeldung aktivieren und OIDC deaktivieren; anschließend neu starten")
			dir := flags.String("data", os.Getenv("CTV_DATA_DIR"), "Datenverzeichnis")
			if e := flags.Parse(os.Args[2:]); e != nil {
				return e
			}
			if *dir == "" {
				*dir = "/data"
			}
			password, e := viewer.Secret("CTV_ADMIN_PASSWORD")
			if e != nil {
				return e
			}
			if password == "" {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("interaktives Terminal oder CTV_ADMIN_PASSWORD_FILE erforderlich")
				}
				fmt.Fprint(os.Stderr, "Neues Administrator-Passwort (mindestens 14 Zeichen): ")
				b, e := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if e != nil {
					return e
				}
				password = string(b)
				fmt.Fprint(os.Stderr, "Passwort wiederholen: ")
				b, e = term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if e != nil {
					return e
				}
				if string(b) != password {
					return fmt.Errorf("Passwörter stimmen nicht überein")
				}
			}
			db, e := viewer.OpenStore(*dir)
			if e != nil {
				return e
			}
			defer db.Close()
			if e = db.CreateAdminRecovery(*name, password, *recoverLogin); e != nil {
				return e
			}
			fmt.Println("Administrator angelegt.")
			if *recoverLogin {
				fmt.Println("Lokale Anmeldung aktiviert, OIDC deaktiviert. Viewer neu starten.")
			}
			return nil
		default:
			return fmt.Errorf("unbekannter Befehl")
		}
	}
	cfg, e := viewer.LoadConfig()
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app, e := viewer.New(ctx, cfg, web.Assets())
	if e != nil {
		return e
	}
	defer app.Close()
	server := &http.Server{Addr: cfg.Address, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	done := make(chan error, 1)
	go func() {
		slog.Info("CCTV Viewer gestartet", "version", version, "address", cfg.Address)
		done <- server.ListenAndServe()
	}()
	select {
	case e = <-done:
		if e != http.ErrServerClosed {
			return e
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
	return nil
}
