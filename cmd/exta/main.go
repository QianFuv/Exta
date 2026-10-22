// Exta is a local, single-process gateway with external SQLite state.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

var version = "dev"

type config struct {
	Listen             string                   `json:"listen"`
	Origin             string                   `json:"origin"`
	DevelopmentOrigins []string                 `json:"development_origins,omitempty"`
	TlsCertFile        string                   `json:"tls_cert_file,omitempty"`
	TlsKeyFile         string                   `json:"tls_key_file,omitempty"`
	ExaUsage           []service.ExaUsageConfig `json:"exa_usage,omitempty"`
}

func main() {
	if err := security.PrepareProcessOwnership(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot establish process file ownership:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildInfo() domain.BuildInfo {
	return domain.BuildInfo{Version: version, SchemaVersion: 1, ContractHash: domain.ContractSHA256, Target: runtime.GOOS + "/" + runtime.GOARCH}
}

func defaultDirectory() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Exta")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if directory := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(directory) {
		return filepath.Join(directory, "exta")
	}
	return filepath.Join(home, ".local", "share", "exta")
}

func run(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: exta <version|init|serve|doctor|backup|restore|migrate> [--data-dir path]")
	}
	if args[0] == "version" {
		return json.NewEncoder(output).Encode(buildInfo())
	}
	flags := flag.NewFlagSet("exta "+args[0], flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	directory := flags.String("data-dir", defaultDirectory(), "Absolute external state directory")
	maintenance := flags.Bool("maintenance", false, "Serve local inspection without provider dispatch")
	backupOutput := flags.String("output", "", "New backup directory")
	keyOutput := flags.String("key-output", "", "Separate new key-recovery directory (default: backup directory plus .key-recovery)")
	backupSource := flags.String("from", "", "Verified backup directory to restore")
	migrationBackup := flags.String("backup", "", "New pre-migration backup directory")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || !filepath.IsAbs(*directory) {
		return errors.New("an absolute --data-dir is required")
	}
	switch args[0] {
	case "init":
		return initialize(ctx, *directory, output)
	case "serve":
		return serveMode(ctx, *directory, diagnostics, *maintenance)
	case "doctor":
		return doctor(ctx, *directory, output)
	case "backup":
		return backupCommand(ctx, *directory, *backupOutput, *keyOutput, output, false)
	case "migrate":
		return backupCommand(ctx, *directory, *migrationBackup, *keyOutput, output, true)
	case "restore":
		return restoreCommand(ctx, *directory, *backupSource, output, nil)
	default:
		return errors.New("unknown command")
	}
}

func initialize(ctx context.Context, directory string, output io.Writer) error {
	if err := security.CheckNoLinks(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	directory = filepath.Clean(directory)
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot inspect data directory")
	}
	if len(entries) > 0 {
		return errors.New("init requires a new or empty data directory; existing files were preserved")
	}
	if err = security.PreparePrivateDirectory(directory); err != nil {
		return err
	}
	lock, err := security.LockDirectory(filepath.Join(directory, "exta.lock"))
	if err != nil {
		return errors.New("data directory is already in use")
	}
	defer lock.Close()
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return err
	}
	if err = writeNew(filepath.Join(directory, "master.key"), key); err != nil {
		return err
	}
	configuration := config{Listen: "127.0.0.1:8789", Origin: "http://127.0.0.1:8789"}
	encoded, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return err
	}
	if err = writeNew(filepath.Join(directory, "config.json"), append(encoded, '\n')); err != nil {
		return err
	}
	vault, err := security.NewVault(key)
	if err != nil {
		return err
	}
	state, err := store.Open(ctx, filepath.Join(directory, "exta.sqlite"), vault)
	if err != nil {
		return errors.New("cannot initialize database; partial files were preserved")
	}
	defer state.Close()
	credential := store.RandomToken()
	if _, err = state.BootstrapAdmin(ctx, credential); err != nil {
		return errors.New("cannot initialize operator")
	}
	_, err = fmt.Fprintln(output, "Exta initialized. Save this one-time operator credential securely:\n"+credential)
	return err
}

func writeNew(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("refusing to overwrite runtime file")
	}
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return errors.New("cannot write runtime file")
	}
	return file.Sync()
}

func loadConfig(directory string) (config, error) {
	var result config
	file, err := os.Open(filepath.Join(directory, "config.json"))
	if err != nil {
		return result, errors.New("existing config.json is required; run init explicitly")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 65537))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return result, errors.New("invalid runtime configuration")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return result, errors.New("configuration must contain one JSON object")
	}
	host, _, err := net.SplitHostPort(result.Listen)
	if err != nil {
		return result, errors.New("listen must be an explicit IP and port")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return result, errors.New("listen must use an explicit IP address")
	}
	origin, err := url.Parse(result.Origin)
	if err != nil || origin.Host == "" {
		return result, errors.New("configured origin is invalid")
	}
	if (result.TlsCertFile == "") != (result.TlsKeyFile == "") {
		return result, errors.New("both TLS certificate and key are required")
	}
	if !ip.IsLoopback() && (origin.Scheme != "https" || result.TlsCertFile == "") {
		return result, errors.New("nonloopback binding requires explicit HTTPS, TLS files and trusted Origin")
	}
	if (origin.Scheme == "https") != (result.TlsCertFile != "") {
		return result, errors.New("origin scheme must match the listener TLS configuration")
	}
	if err := service.ValidateExaUsage(result.ExaUsage); err != nil {
		return result, err
	}
	return result, nil
}

func openRuntime(ctx context.Context, directory string) (*store.Store, *security.Auth, config, *os.File, error) {
	if err := security.CheckNoLinks(directory); err != nil {
		return nil, nil, config{}, nil, err
	}
	for _, name := range []string{"", "config.json", "exta.sqlite", "master.key", "exta.lock", "exta.sqlite-wal", "exta.sqlite-shm"} {
		path := filepath.Join(directory, name)
		if err := security.CheckPrivate(path); err != nil {
			if name != "" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, config{}, nil, errors.New("runtime state permissions are unsafe or unreadable; ownership and access must be restricted to the service user")
		}
	}
	lock, err := lockOperations(directory, false)
	if err != nil {
		return nil, nil, config{}, nil, err
	}
	local, err := inspectLocal(ctx, directory)
	if err != nil {
		lock.Close()
		return nil, nil, config{}, nil, err
	}
	configuration := local.Config
	state, err := store.OpenExisting(ctx, filepath.Join(directory, "exta.sqlite"), local.Vault)
	if err != nil {
		lock.Close()
		return nil, nil, configuration, nil, errors.New("database is unavailable or incompatible")
	}
	auth, err := security.NewAuth(state, configuration.Origin, configuration.DevelopmentOrigins)
	if err != nil {
		state.Close()
		lock.Close()
		return nil, nil, configuration, nil, err
	}
	return state, auth, configuration, lock, nil
}

func serve(ctx context.Context, directory string, diagnostics io.Writer) error {
	return serveMode(ctx, directory, diagnostics, false)
}

func serveMode(ctx context.Context, directory string, diagnostics io.Writer, maintenance bool) error {
	state, auth, configuration, lock, err := openRuntime(ctx, directory)
	if err != nil {
		return err
	}
	defer func() {
		state.Close()
		lock.Close()
	}()
	if !maintenance {
		if err = state.RecoverRequests(ctx); err != nil {
			return err
		}
	}
	core, err := service.New(state, nil, security.UrlPolicy{})
	if err != nil {
		return err
	}
	if !maintenance {
		if err = core.Research.Recover(ctx); err != nil {
			return err
		}
	}
	handler, err := managementHandlerWithCore(state, auth, configuration, core)
	if err != nil {
		return err
	}
	if maintenance {
		handler = maintenanceHandler(handler)
	}
	admission := newAdmissionHandler(handler)
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server := &http.Server{Addr: configuration.Listen, Handler: admission, BaseContext: func(net.Listener) context.Context { return requests }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	listener, err := net.Listen("tcp", configuration.Listen)
	if err != nil {
		return errors.New("configured address is unavailable")
	}
	if !maintenance {
		if err = state.Write(ctx, func(tx *sql.Tx) error { return store.AdvanceActivityTx(ctx, tx) }); err != nil {
			listener.Close()
			return err
		}
	}
	finished := make(chan error, 1)
	workerContext, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerFinished := make(chan error, 1)
	if maintenance {
		workerFinished <- nil
	} else {
		go func() { workerFinished <- runWorkers(workerContext, core) }()
	}
	go func() {
		if configuration.TlsCertFile != "" {
			finished <- server.ServeTLS(listener, configuration.TlsCertFile, configuration.TlsKeyFile)
		} else {
			finished <- server.Serve(listener)
		}
	}()
	fmt.Fprintln(diagnostics, "Exta listening at "+configuration.Origin)
	if maintenance {
		fmt.Fprintln(diagnostics, "Maintenance mode: provider dispatch is disabled")
	}
	var workerWatch <-chan error
	if !maintenance {
		workerWatch = workerFinished
	}
	serverStopped := false
	workerStopped := false
	select {
	case err = <-finished:
		serverStopped = true
	case err = <-workerWatch:
		workerStopped = true
		if err == nil {
			err = errors.New("research worker stopped unexpectedly")
		}
	case <-ctx.Done():
	}
	drained := admission.close()
	cancelWorker()
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	shutdownErr := server.Shutdown(shutdown)
	cancel()
	cancelRequests()
	if shutdownErr != nil {
		_ = server.Close()
	}
	if !serverStopped {
		<-finished
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	joined := make(chan error, 1)
	go func() {
		<-drained
		var workerErr error
		if !workerStopped {
			workerErr = <-workerFinished
		}
		joined <- workerErr
	}()
	err = errors.Join(err, <-joined)
	if shutdownErr != nil && err == nil {
		err = shutdownErr
	}
	return err
}
