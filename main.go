package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	pkgerr "github.com/pkg/errors"
	"github.com/yaqyn/waymark-link-shortener/internal/apperr"
	"github.com/yaqyn/waymark-link-shortener/internal/build"
	"github.com/yaqyn/waymark-link-shortener/internal/store"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	hashPassword := flag.Bool("hash-password", false, "read a password from stdin and print its bcrypt hash")
	flag.Parse()
	if *hashPassword {
		password, err := io.ReadAll(io.LimitReader(os.Stdin, 1024))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		password = []byte(strings.TrimRight(string(password), "\r\n"))
		if len(password) < 12 || len(password) > 72 {
			fmt.Fprintln(os.Stderr, "password must contain 12–72 bytes")
			os.Exit(1)
		}
		hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(string(hash))
		return
	}

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	shutdownTracing, err := initTracing(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize tracing: %v\n", err)
		return 1
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "failed to shut down tracing: %v\n", err)
		}
	}()

	logger, cleanup, err := initializeLogger(os.Getenv("WAYMARK_LOG_FILE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	}
	defer func() {
		if err := cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to clean up logger: %v\n", err)
		}
	}()
	hostname, err := os.Hostname()
	if err != nil {
		logger.Error("failed to get hostname", "error", err)
		return 1
	}
	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", os.Getenv("ENV")),
		slog.String("hostname", hostname),
	)

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error("failed to create store", "error", err)
		return 1
	}
	username := strings.TrimSpace(os.Getenv("WAYMARK_USERNAME"))
	if username == "" {
		username = "frodo"
	}
	if len([]rune(username)) > 64 || strings.ContainsAny(username, ":\r\n\t") {
		logger.Error("username must be at most 64 characters without colons or control characters")
		return 1
	}
	hash := os.Getenv("WAYMARK_PASSWORD_HASH")
	if path := os.Getenv("WAYMARK_PASSWORD_HASH_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Error("failed to read password hash", "error", err)
			return 1
		}
		hash = strings.TrimSpace(string(data))
	}
	if hash == "" {
		if os.Getenv("ENV") == "production" {
			logger.Error("WAYMARK_PASSWORD_HASH or WAYMARK_PASSWORD_HASH_FILE is required in production")
			return 1
		}
		hash = demoHash
		logger.Warn("Using the documented development account; configure credentials before deployment")
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost < 10 || cost > 14 {
		logger.Error("bcrypt password hash must have a cost between 10 and 14")
		return 1
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("configuration-validation")); err != nil && !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		logger.Error("malformed bcrypt password hash")
		return 1
	}
	s := newServer(st, httpPort, cancel, logger)
	s.auth = newAuth(username, hash, os.Getenv("ENV") == "production")
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- s.start() }()
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if err != nil {
			logger.Error("server failed", "error", err)
			return 1
		}
		return 0
	}
	logger.Info("Waymark is shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", "error", err)
		return 1
	}
	if serverErr := <-serverErrors; serverErr != nil {
		logger.Error("server error", "error", serverErr)
		return 1
	}
	return 0
}

type closeFunc func() error

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {
	stderrHandler := tint.NewTextHandler(os.Stderr, &tint.Options{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
		NoColor:     !(isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())),
	})
	var console slog.Handler = stderrHandler
	if os.Getenv("ENV") == "production" {
		console = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: replaceAttr})
	}
	if logFile == "" {
		return slog.New(console), func() error { return nil }, nil
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	if err := file.Close(); err != nil {
		return nil, nil, err
	}
	fileLogger := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    1,
		MaxAge:     28,
		MaxBackups: 10,
		LocalTime:  false,
		Compress:   true,
	}
	fileHandler := slog.NewJSONHandler(fileLogger, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: replaceAttr,
	})
	logger := slog.New(slog.NewMultiHandler(console, fileHandler))
	return logger, fileLogger.Close, nil
}

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

type multiError interface {
	error
	Unwrap() []error
}

var sensitiveKeys = []string{"password", "key", "apikey", "secret", "pin", "creditcardno", "user", "authorization", "cookie", "token", "username"}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if slices.Contains(sensitiveKeys, strings.ToLower(a.Key)) {
		return slog.String(a.Key, "[REDACTED]")
	}
	if a.Value.Kind() == slog.KindString {
		if u, err := url.Parse(a.Value.String()); err == nil && u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				u.User = url.UserPassword(u.User.Username(), "[REDACTED]")
				return slog.String(a.Key, u.String())
			}
		}
	}
	if a.Key != "error" {
		return a
	}
	err, ok := a.Value.Any().(error)
	if !ok {
		return a
	}
	if multiErr, ok := errors.AsType[multiError](err); ok {
		var attrs []slog.Attr
		for i, child := range multiErr.Unwrap() {
			attrs = append(attrs, slog.GroupAttrs(fmt.Sprintf("error_%d", i+1), errorAttrs(child)...))
		}
		return slog.GroupAttrs("errors", attrs...)
	}
	return slog.GroupAttrs("error", errorAttrs(err)...)
}

func errorAttrs(err error) []slog.Attr {
	attrs := []slog.Attr{slog.String("message", err.Error())}
	attrs = append(attrs, apperr.Attrs(err)...)
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.String("stack_trace", fmt.Sprintf("%+v", stackErr.StackTrace())))
	}
	return attrs
}
