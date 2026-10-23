package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

func TestPackagedRuntime(t *testing.T) {
	source := os.Getenv("EXTA_SMOKE_BINARY")
	if source == "" {
		t.Skip("requires an explicitly built native artifact via EXTA_SMOKE_BINARY")
	}
	root := t.TempDir()
	name := "exta"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(root, name)
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(binary, content, 0700); err != nil {
		t.Fatal(err)
	}
	environment := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			environment = append(environment, entry)
		}
	}
	if runtime.GOOS == "windows" {
		environment = append(environment, "PATH="+filepath.Join(os.Getenv("SystemRoot"), "System32"))
	} else {
		environment = append(environment, "PATH=/usr/bin:/bin")
	}
	invoke := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		process := exec.CommandContext(ctx, binary, args...)
		process.Dir = root
		process.Env = environment
		hideProcess(process)
		output, err := process.CombinedOutput()
		if ctx.Err() != nil {
			return output, fmt.Errorf("native command deadline: %w", ctx.Err())
		}
		return output, err
	}
	expectRefusal := func(diagnostic string, args ...string) {
		t.Helper()
		output, err := invoke(args...)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), diagnostic) {
			t.Fatalf("expected refusal %q: %v %s", diagnostic, err, output)
		}
	}
	version, err := invoke("version")
	if err != nil {
		t.Fatal("native version failed", err)
	}
	var build domain.BuildInfo
	if err = json.Unmarshal(version, &build); err != nil || build.ContractHash != domain.ContractSHA256 || build.Target != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("native build identity: %+v %v", build, err)
	}
	directory := filepath.Join(root, "state")
	if _, err = invoke("init", "--data-dir", directory); err != nil {
		t.Fatal("native init failed", err)
	}
	for _, path := range []string{directory, filepath.Join(directory, "master.key"), filepath.Join(directory, "exta.sqlite")} {
		if err = security.CheckPrivate(path); err != nil {
			t.Fatal(err)
		}
	}
	expectRefusal("init requires a new or empty data directory", "init", "--data-dir", directory)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	origin := "http://" + address
	configuration := []byte(store.JSON(map[string]string{"listen": address, "origin": origin}))
	if err = os.WriteFile(filepath.Join(directory, "config.json"), configuration, 0600); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(key)
	state, err := store.OpenExisting(t.Context(), filepath.Join(directory, "exta.sqlite"), vault)
	if err != nil {
		t.Fatal(err)
	}
	var owner string
	if err = state.DB.QueryRow("SELECT id FROM principals LIMIT 1").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	token, err := state.MintToken(t.Context(), owner, domain.TokenCreate{Label: "Packaged synthetic smoke", Scopes: []domain.ClientScope{"read", "research:read"}, ExpiresAt: store.Stamp(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.AddKey(t.Context(), owner, domain.UpstreamKeyCreate{Provider: "exa", QuotaPolicy: (domain.QuotaPolicy{Unit: "micro_usd", Allocation: 10000000, PerOperationReservation: 1000000, ActiveJobs: 2}).Input(), Label: "Synthetic offline key", Enabled: true, Secret: "synthetic-invalid-offline-smoke-key"})
	if err != nil {
		t.Fatal(err)
	}
	core, err := service.New(state, nil, security.UrlPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.DB.Exec(`UPDATE settings SET data=json_set(data,'$.limits.principal_research_active',2)`); err != nil {
		t.Fatal(err)
	}
	jobs := []domain.Job{}
	for _, kind := range []string{"unknown", "known"} {
		job, err := core.Research.Accept(t.Context(), owner, "packaged-smoke-"+kind, []byte(`{"input":"Offline packaged recovery fixture","budget":{"unit":"micro_usd","amount":1000000,"mode":"hard"}}`))
		if err != nil {
			t.Fatal(err)
		}
		past := store.Stamp(time.Now().Add(-time.Hour))
		if kind == "unknown" {
			_, err = state.DB.Exec(`UPDATE research_jobs SET state='submitting',lease_token='crashed-create',lease_until_ms=?,data=json_set(data,'$.job.state','submitting','$.action','create','$.job.observation_deadline',?) WHERE id=?`, time.Now().Add(time.Hour).UnixMilli(), past, job.Id)
		} else {
			_, err = state.DB.Exec(`UPDATE research_jobs SET state='running',upstream_id='synthetic_known',lease_token='crashed-poll',lease_until_ms=?,data=json_set(data,'$.job.state','running','$.job.upstream_job_id','synthetic_known','$.action','poll','$.job.observation_deadline',?) WHERE id=?`, time.Now().Add(time.Hour).UnixMilli(), past, job.Id)
		}
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	if _, err = state.DB.Exec(`UPDATE credential_versions SET validity='invalid'`); err != nil {
		t.Fatal(err)
	}
	if _, err = state.DB.Exec(`UPDATE runtime_gates SET blocked=1`); err != nil {
		t.Fatal(err)
	}
	if err = state.Close(); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup")
	if _, err = invoke("backup", "--data-dir", directory, "--output", backup); err != nil {
		t.Fatal("native backup failed", err)
	}
	start := func(extra ...string) func() {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		args := append([]string{"serve", "--data-dir", directory}, extra...)
		process := exec.CommandContext(ctx, binary, args...)
		process.Dir = root
		process.Env = environment
		hideProcess(process)
		var diagnostics bytes.Buffer
		process.Stdout = &diagnostics
		process.Stderr = &diagnostics
		if err := process.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		ended := make(chan error, 1)
		go func() { ended <- process.Wait() }()
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			cancel()
			<-ended
		}
		t.Cleanup(stop)
		client := http.Client{Timeout: time.Second}
		for attempt := 0; attempt < 50; attempt++ {
			response, err := client.Get(origin + "/healthz")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					return stop
				}
			}
			select {
			case err := <-ended:
				stopped = true
				cancel()
				t.Fatalf("native server exited: %v %s", err, diagnostics.String())
			case <-time.After(100 * time.Millisecond):
			}
		}
		stop()
		t.Fatal("native startup timed out")
		return nil
	}
	client := http.Client{Timeout: 3 * time.Second}
	request := func(method, path, body string) (int, []byte) {
		req, err := http.NewRequestWithContext(t.Context(), method, origin+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token.TokenValue)
		req.Header.Set("Accept", "application/json, text/event-stream")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if path == "/mcp" {
			req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	stop := start()
	expectRefusal("exclusive data-directory lock", "serve", "--data-dir", directory)
	if status, _ := request("GET", "/v1/capabilities", ""); status != 200 {
		t.Fatalf("REST capabilities: %d", status)
	}
	if status, data := request("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`); status != 200 || !bytes.Contains(data, []byte(`"tools"`)) {
		t.Fatalf("native MCP list status=%d", status)
	}
	for index, job := range jobs {
		var detail domain.JobDetail
		status := 0
		for attempt := 0; attempt < 50; attempt++ {
			var data []byte
			status, data = request("GET", "/v1/research/"+job.Id, "")
			err = json.Unmarshal(data, &detail)
			if err != nil || status != 200 || detail.Job.State == "observation_expired" {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil || status != 200 || !detail.Job.ActiveSlotHeld || detail.Job.BudgetHoldState != "held" || detail.Job.State != "observation_expired" {
			t.Fatalf("native conservative recovery failed for fixture %d: status=%d state=%s error=%v", index, status, detail.Job.State, err)
		}
		if index == 1 && (detail.Job.UpstreamJobId == nil || *detail.Job.UpstreamJobId != "synthetic_known") {
			t.Fatal("known upstream ID lost across restart")
		}
		if detail.Job.CompletionReason == nil || *detail.Job.CompletionReason != "observation_limit" || detail.Job.PollingCount != 0 {
			t.Fatal("recovery dispatched instead of expiring observation")
		}
		for _, event := range detail.Events {
			if strings.HasPrefix(event.Reason, "worker_") && event.Reason != "worker_recovered" {
				t.Fatal("recovery recorded a provider dispatch")
			}
		}
	}
	stop()
	state, err = store.OpenExisting(t.Context(), filepath.Join(directory, "exta.sqlite"), vault)
	if err != nil {
		t.Fatal(err)
	}
	var dispatched int
	err = state.DB.QueryRow(`SELECT count(*) FROM research_jobs WHERE json_extract(data,'$.create_attempts')<>0 OR json_extract(data,'$.total_polls')<>0`).Scan(&dispatched)
	closeError := state.Close()
	if err != nil || closeError != nil || dispatched != 0 {
		t.Fatalf("durable dispatch counters: %d %v %v", dispatched, err, closeError)
	}
	expectRefusal("restore refused: destination identity, schema or activity differs", "restore", "--data-dir", directory, "--from", backup)
	if _, err = invoke("doctor", "--data-dir", directory); err != nil {
		t.Fatal("native doctor failed", err)
	}
	if _, err = invoke("migrate", "--data-dir", directory, "--backup", filepath.Join(root, "migration")); err != nil {
		t.Fatal("native current-schema migration failed", err)
	}
	listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	expectRefusal("configured address is unavailable", "serve", "--data-dir", directory)
	listener.Close()
	if err = os.Rename(filepath.Join(directory, "master.key"), filepath.Join(directory, "saved.key")); err != nil {
		t.Fatal(err)
	}
	stop = start()
	if status, data := request("GET", "/v1/ready", ""); status != 200 || !bytes.Contains(data, []byte(`"secret_store":"locked"`)) {
		t.Fatal("missing master key did not lock native runtime")
	}
	stop()
	if err = os.Rename(filepath.Join(directory, "saved.key"), filepath.Join(directory, "master.key")); err != nil {
		t.Fatal(err)
	}
	stop = start("--maintenance")
	if status, _ := request("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); status != 503 {
		t.Fatal("maintenance admitted MCP dispatch")
	}
	stop()
	t.Log("Native isolated executable: version, private init, repeat/lock/port refusal, REST/MCP, conservative restart, stale rollback refusal, doctor, schema-current migration, locked key and maintenance passed; no provider dispatch; process termination was forced for crash/restart coverage")
}
