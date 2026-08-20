package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
)

func newProblem(r *http.Request, status int, code, title, detail string) generated.Problem {
	return newProblemFromContext(r.Context(), status, code, title, detail)
}

func newProblemFromContext(ctx context.Context, status int, code, title, detail string) generated.Problem {
	requestID := requestIDFromContext(ctx)

	problem := generated.Problem{
		Type:      "about:blank",
		Title:     title,
		Status:    status,
		Code:      code,
		RequestId: requestID,
	}
	if detail != "" {
		problem.Detail = &detail
	}
	return problem
}

func requestIDFromContext(ctx context.Context) string {
	requestID := middleware.GetReqID(ctx)
	if requestID == "" {
		requestID = "unavailable"
	}
	return requestID
}

// WriteProblem keeps validation and unexpected transport failures on the same
// application/problem+json contract as generated endpoint responses.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(newProblem(r, status, code, title, detail))
}
