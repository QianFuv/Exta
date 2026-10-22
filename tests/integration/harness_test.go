package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type capturedCall struct {
	Method, Path, Credential string
	Body                     []byte
}
type mockReply struct {
	Status  int
	Body    string
	Headers map[string]string
}
type mockProvider struct {
	Server  *httptest.Server
	mutex   sync.Mutex
	calls   []capturedCall
	replies []mockReply
}

func newMockProvider(t *testing.T, replies ...mockReply) *mockProvider {
	t.Helper()
	mock := &mockProvider{replies: replies}
	mock.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
		if err != nil {
			t.Error(err)
		}
		mock.mutex.Lock()
		defer mock.mutex.Unlock()
		credential := request.Header.Get("Authorization")
		if credential == "" {
			credential = request.Header.Get("x-api-key")
		}
		mock.calls = append(mock.calls, capturedCall{Method: request.Method, Path: request.URL.RequestURI(), Credential: credential, Body: body})
		if len(mock.replies) == 0 {
			t.Error("unexpected extra upstream call")
			writer.WriteHeader(500)
			return
		}
		reply := mock.replies[0]
		mock.replies = mock.replies[1:]
		writer.Header().Set("Content-Type", "application/json")
		for key, value := range reply.Headers {
			writer.Header().Set(key, value)
		}
		writer.WriteHeader(reply.Status)
		io.WriteString(writer, reply.Body)
	}))
	t.Cleanup(mock.Server.Close)
	return mock
}

func (mock *mockProvider) Calls() []capturedCall {
	mock.mutex.Lock()
	defer mock.mutex.Unlock()
	return append([]capturedCall(nil), mock.calls...)
}

func TestMockHarnessCapturesOnlyLocalSyntheticCalls(t *testing.T) {
	mock := newMockProvider(t, mockReply{Status: 200, Body: `{"ok":true}`})
	response, err := mock.Server.Client().Get(mock.Server.URL + "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(mock.Calls()) != 1 || mock.Calls()[0].Path != "/fixture" {
		t.Fatal("fixture ledger did not capture request")
	}
}
