package servecmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ffreis-website-compiler/internal/sitegen"
)

func Run(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	websiteRoot := fs.String("website-root", ".", "website project root; expects <website-root>/src/{assets,templates} (legacy fallback: <website-root>/{site,templates})")
	addr := fs.String("addr", ":8080", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	assetsRoot, templatesRoot, err := resolveWebsitePaths(*websiteRoot)
	if err != nil {
		return err
	}

	pages, err := sitegen.LoadPageTemplatesFromRoot(templatesRoot)
	if err != nil {
		return fmt.Errorf("loading templates: %w", err)
	}

	mux := http.NewServeMux()
	registerStatic(mux, assetsRoot)
	registerPages(mux, pages, logger)

	var handler http.Handler = mux
	handler = loggingMiddleware(logger, handler)
	handler = securityHeadersMiddleware(handler)
	handler = recoveryMiddleware(logger, handler)
	handler = requestIDMiddleware(handler)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadTimeout:       getEnvDuration("SERVE_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      getEnvDuration("SERVE_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:       getEnvDuration("SERVE_IDLE_TIMEOUT", 60*time.Second),
		ReadHeaderTimeout: getEnvDuration("SERVE_READ_HEADER_TIMEOUT", 5*time.Second),
		MaxHeaderBytes:    getEnvInt("SERVE_MAX_HEADER_BYTES", 1_048_576),
	}
	shutdownTimeout := getEnvDuration("SERVE_SHUTDOWN_TIMEOUT", 10*time.Second)

	logger.Info(
		"starting local server",
		"addr", *addr,
		"website_root", *websiteRoot,
		"assets_dir", assetsRoot,
		"templates_dir", templatesRoot,
		"pages", len(pages),
		"read_timeout", srv.ReadTimeout.String(),
		"write_timeout", srv.WriteTimeout.String(),
		"idle_timeout", srv.IdleTimeout.String(),
		"shutdown_timeout", shutdownTimeout.String(),
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}

	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		return err
	}

	logger.Info("server shutdown complete")
	return nil
}

func resolveWebsitePaths(websiteRoot string) (string, string, error) {
	newAssets := filepath.Join(websiteRoot, "src", "assets")
	newTemplates := filepath.Join(websiteRoot, "src", "templates")
	if dirExists(newAssets) && dirExists(newTemplates) {
		return newAssets, newTemplates, nil
	}

	legacyAssets := filepath.Join(websiteRoot, "site")
	legacyTemplates := filepath.Join(websiteRoot, "templates")
	if dirExists(legacyAssets) && dirExists(legacyTemplates) {
		return legacyAssets, legacyTemplates, nil
	}

	return "", "", fmt.Errorf(
		"could not resolve website directories under %s; expected src/assets + src/templates (or legacy site + templates)",
		websiteRoot,
	)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func registerPages(mux *http.ServeMux, pages []sitegen.PageTemplate, logger *slog.Logger) {
	for _, page := range pages {
		path := "/" + page.Name + ".html"
		tpl := page.Tmpl
		if page.Name == "index" {
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				renderTemplate(w, r, tpl, logger)
			})
		}

		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			renderTemplate(w, r, tpl, logger)
		})
	}
}

func renderTemplate(w http.ResponseWriter, r *http.Request, tpl *template.Template, logger *slog.Logger) {
	if err := tpl.ExecuteTemplate(w, "layout", nil); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Error("template execution failed", "path", r.URL.Path, "error", err)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

type ctxKey string

const ctxKeyRequestID ctxKey = "request_id"

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = generateRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, requestID)))
	})
}

func recoveryMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(
					"panic recovered",
					"panic", rec,
					"path", r.URL.Path,
					"method", r.Method,
					"request_id", requestIDFromContext(r.Context()),
					"stack", string(debug.Stack()),
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				recorder.status = http.StatusInternalServerError
				panic(rec)
			}
			duration := time.Since(start)
			logger.Info(
				"http request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"duration_ms", duration.Milliseconds(),
				"request_id", requestIDFromContext(r.Context()),
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		}()
		next.ServeHTTP(recorder, r)
	})
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(ctxKeyRequestID).(string)
	return requestID
}

func generateRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func registerStatic(mux *http.ServeMux, siteRoot string) {
	mux.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(filepath.Join(siteRoot, "css")))))
	mux.Handle("/fonts/", http.StripPrefix("/fonts/", http.FileServer(http.Dir(filepath.Join(siteRoot, "fonts")))))
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(filepath.Join(siteRoot, "images")))))
	mux.Handle("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir(filepath.Join(siteRoot, "js")))))
	mux.Handle("/send.js", http.FileServer(http.Dir(siteRoot)))
	mux.Handle("/contactScript.js", http.FileServer(http.Dir(siteRoot)))
	mux.Handle("/robots.txt", http.FileServer(http.Dir(siteRoot)))
	mux.Handle("/sitemap.xml", http.FileServer(http.Dir(siteRoot)))
}
