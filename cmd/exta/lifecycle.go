package main

import (
	"net/http"
	"strings"
	"sync"

	"exta/internal/security"
	"exta/internal/store"
)

type admissionHandler struct {
	next     http.Handler
	mutex    sync.Mutex
	active   int
	isClosed bool
	drained  chan struct{}
}

func newAdmissionHandler(next http.Handler) *admissionHandler {
	return &admissionHandler{next: next, drained: make(chan struct{})}
}

func (handler *admissionHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	handler.mutex.Lock()
	if handler.isClosed {
		handler.mutex.Unlock()
		security.WriteError(writer, request, store.Reject("provider_unavailable", 503, "Server admission is closed"))
		return
	}
	handler.active++
	handler.mutex.Unlock()
	defer func() {
		handler.mutex.Lock()
		defer handler.mutex.Unlock()
		handler.active--
		if handler.isClosed && handler.active == 0 {
			close(handler.drained)
		}
	}()
	handler.next.ServeHTTP(writer, request)
}

func (handler *admissionHandler) close() <-chan struct{} {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	if !handler.isClosed {
		handler.isClosed = true
		if handler.active == 0 {
			close(handler.drained)
		}
	}
	return handler.drained
}

func maintenanceHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		isSession := request.URL.Path == "/v1/admin/session"
		isRead := request.Method == "GET" || request.Method == "HEAD" || request.Method == "OPTIONS"
		if strings.HasPrefix(request.URL.Path, "/mcp") || !isRead && !isSession {
			security.WriteError(writer, request, store.Reject("provider_unavailable", 503, "Maintenance mode permits local inspection only"))
			return
		}
		next.ServeHTTP(writer, request)
	})
}
