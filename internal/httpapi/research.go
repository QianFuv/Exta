package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"exta/internal/domain"
	"exta/internal/security"
	"exta/internal/service"
	"exta/internal/store"
)

func registerResearch(mux *http.ServeMux, auth *security.Auth, core *service.Service) {
	for _, admin := range []bool{false, true} {
		base := "/v1/research"
		if admin {
			base = "/v1/admin/research"
		}
		for _, entry := range []struct{ method, suffix, action, permission string }{{"POST", "", "create", "admin:research:create"}, {"GET", "", "list", "admin:read"}, {"GET", "/{id}", "get", "admin:read"}, {"POST", "/{id}/cancel", "cancel", "admin:research:cancel"}, {"POST", "/{id}/resume-observation", "resume", "admin:research:observe"}, {"POST", "/{id}/reconcile", "reconcile", "admin:research:reconcile"}} {
			if !admin && entry.action == "reconcile" {
				continue
			}
			mux.HandleFunc(entry.method+" "+base+entry.suffix, func(writer http.ResponseWriter, request *http.Request) {
				var identity store.Identity
				var err error
				if admin {
					identity, _, err = auth.Admin(request, entry.permission)
				} else {
					scope := "research:create"
					if entry.action == "get" || entry.action == "list" {
						scope = "research:read"
					} else if entry.action == "cancel" {
						scope = "research:cancel"
					} else if entry.action == "resume" {
						scope = "research:observe"
					}
					identity, err = auth.Client(request, scope)
				}
				if err != nil {
					security.WriteError(writer, request, err)
					return
				}
				owner := identity.PrincipalId
				if admin && entry.action != "create" {
					owner = ""
				}
				status := http.StatusOK
				var result any
				switch entry.action {
				case "create":
					var raw []byte
					_, raw, err = Body[json.RawMessage](core.Store, writer, request, "ResearchCreate")
					if err == nil {
						var job domain.Job
						job, err = core.Research.Accept(request.Context(), identity.PrincipalId, request.Header.Get("Idempotency-Key"), raw)
						result = job
						if err == nil {
							writer.Header().Set("Location", base+"/"+job.Id)
							status = http.StatusAccepted
						}
					}
				case "list":
					var limit int
					limit, err = Limit(request)
					if err == nil {
						result, err = core.Research.List(request.Context(), owner, request.URL.Query().Get("state"), request.URL.Query().Get("cursor"), limit)
					}
				case "get":
					limit := 25
					if value := request.URL.Query().Get("events_limit"); value != "" {
						limit, err = strconv.Atoi(value)
						if err != nil || limit < 1 || limit > 100 {
							err = store.Reject("invalid_request", 400, "Invalid event page limit")
						}
					}
					if err == nil {
						result, err = core.Research.Get(request.Context(), owner, request.PathValue("id"), request.URL.Query().Get("events_cursor"), limit)
					}
				case "cancel":
					var input domain.CancelRequest
					input, _, err = Body[domain.CancelRequest](core.Store, writer, request, "CancelRequest")
					if err == nil {
						var job domain.Job
						job, err = core.Research.Cancel(request.Context(), owner, identity.PrincipalId, request.PathValue("id"), input)
						result = job
						if job.State == "cancel_requested" || job.State == "submitting" || job.State == "submission_unknown" {
							status = http.StatusAccepted
						}
					}
				case "resume":
					var input domain.ResumeRequest
					input, _, err = Body[domain.ResumeRequest](core.Store, writer, request, "ResumeRequest")
					if err == nil {
						result, err = core.Research.Resume(request.Context(), owner, identity.PrincipalId, request.PathValue("id"), input)
						status = http.StatusAccepted
					}
				case "reconcile":
					var raw []byte
					_, raw, err = Body[json.RawMessage](core.Store, writer, request, "ReconcileRequest")
					if err == nil {
						result, err = core.Research.Reconcile(request.Context(), identity.PrincipalId, request.PathValue("id"), raw)
					}
				}
				if err != nil {
					security.WriteError(writer, request, err)
					return
				}
				security.WriteJSON(writer, status, result)
			})
		}
	}
}
