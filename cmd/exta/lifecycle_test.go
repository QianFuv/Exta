package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdmissionDrainsFinalizersBeforeClosingStorage(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	handler := newAdmissionHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-finish }))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	<-entered
	drained := handler.close()
	select {
	case <-drained:
		t.Fatal("live finalizer was not joined")
	default:
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 503 {
		t.Fatal("new admission after shutdown")
	}
	close(finish)
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("finalizer did not drain")
	}
	<-handler.close()
}

func TestMaintenancePermitsInspectionWithoutExecution(t *testing.T) {
	called := 0
	handler := maintenanceHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { called++; writer.WriteHeader(204) }))
	for _, entry := range []struct {
		method, path string
		status       int
	}{{"GET", "/v1/research", 204}, {"GET", "/healthz", 204}, {"POST", "/v1/admin/session", 204}, {"POST", "/v1/research", 503}, {"POST", "/v1/admin/usage/refresh", 503}, {"POST", "/mcp", 503}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(entry.method, entry.path, nil))
		if response.Code != entry.status {
			t.Fatalf("%s %s: %d", entry.method, entry.path, response.Code)
		}
	}
	if called != 3 {
		t.Fatal("maintenance dispatched work")
	}
}
