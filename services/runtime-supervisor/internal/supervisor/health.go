package supervisor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type componentHealth struct {
	ComponentStatus
	Ready       bool   `json:"ready"`
	ReadyDetail string `json:"ready_detail,omitempty"`
}

type healthDocument struct {
	Mode       Mode              `json:"mode"`
	Stopping   bool              `json:"stopping"`
	Components []componentHealth `json:"components"`
}

func (supervisor *Supervisor) healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(response http.ResponseWriter, _ *http.Request) {
		if supervisor.stopping.Load() {
			http.Error(response, "stopping", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, _ *http.Request) {
		document, ready := supervisor.healthDocument()
		response.Header().Set("Content-Type", "application/json")
		if !ready {
			response.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(response).Encode(document)
	})
	mux.HandleFunc("GET /components", func(response http.ResponseWriter, _ *http.Request) {
		document, _ := supervisor.healthDocument()
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(document)
	})
	return mux
}

func (supervisor *Supervisor) healthDocument() (healthDocument, bool) {
	statuses := supervisor.Statuses()
	statusByName := make(map[string]ComponentStatus, len(statuses))
	for _, status := range statuses {
		statusByName[status.Name] = status
	}
	document := healthDocument{Mode: supervisor.mode, Stopping: supervisor.stopping.Load(), Components: make([]componentHealth, 0, len(supervisor.components))}
	ready := !document.Stopping
	for _, component := range supervisor.components {
		status := statusByName[component.Name]
		componentReady := status.Running
		detail := ""
		if componentReady && component.ReadyURL != "" {
			var err error
			componentReady, err = supervisor.probe(component.ReadyURL)
			if err != nil {
				detail = err.Error()
			}
		} else if !componentReady {
			detail = "process is not running"
		}
		document.Components = append(document.Components, componentHealth{ComponentStatus: status, Ready: componentReady, ReadyDetail: detail})
		ready = ready && componentReady
	}
	return document, ready
}

func (supervisor *Supervisor) probe(url string) (bool, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	response, err := supervisor.client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("readiness probe returned HTTP %d", response.StatusCode)
	}
	return true, nil
}
