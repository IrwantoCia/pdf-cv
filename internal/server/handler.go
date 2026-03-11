package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/IrwantoCia/pdf-cv/internal/cv"
	"github.com/IrwantoCia/pdf-cv/internal/pdfqueue"
)

const maxCVBodyBytes = 1 << 20
const maxResumeBodyBytes = 2 << 20

type handler struct {
	store    *cv.Store
	tmpl     *template.Template
	pdfQueue *pdfqueue.Service
}

func NewHandler(store *cv.Store, pdfQueue *pdfqueue.Service) (http.Handler, error) {
	if store == nil {
		return nil, errors.New("cv store is required")
	}
	if pdfQueue == nil {
		return nil, errors.New("pdf queue service is required")
	}

	tmpl, err := template.ParseFiles("web/templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	h := &handler{store: store, tmpl: tmpl, pdfQueue: pdfQueue}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.handleIndex)
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /resume/default", h.handleDefaultResume)
	mux.HandleFunc("POST /cv", h.handleSaveCV)
	mux.HandleFunc("POST /pdf/generate", h.handleGeneratePDF)
	mux.HandleFunc("GET /pdf/jobs/{id}", h.handleGetPDFJob)
	mux.HandleFunc("GET /pdf/jobs/{id}/download", h.handleDownloadPDFJob)
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFileServer()))

	return securityHeaders(mux), nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (h *handler) handleGeneratePDF(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxResumeBodyBytes)

	var resume cv.Resume
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resume); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(resume.Basics.Name) == "" {
		http.Error(w, "basics.name is required", http.StatusBadRequest)
		return
	}

	job, err := h.pdfQueue.Enqueue(r.Context(), resume, pdfqueue.EnqueueMeta{
		ClientIP:  clientIPFromRequest(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		if errors.Is(err, pdfqueue.ErrQueueFull) {
			http.Error(w, "pdf generation queue is full", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "failed to generate pdf", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jobId":       job.ID,
		"status":      job.Status,
		"position":    job.Position,
		"statusUrl":   fmt.Sprintf("/pdf/jobs/%s", job.ID),
		"downloadUrl": fmt.Sprintf("/pdf/jobs/%s/download", job.ID),
	})
}

func (h *handler) handleGetPDFJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(r.PathValue("id"))
	job, err := h.pdfQueue.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, pdfqueue.ErrNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to read job", http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"jobId":  job.ID,
		"status": job.Status,
	}
	if job.Status == pdfqueue.StatusQueued {
		resp["position"] = job.Position
	}
	if job.Status == pdfqueue.StatusReady {
		resp["downloadUrl"] = fmt.Sprintf("/pdf/jobs/%s/download", job.ID)
		if job.ExpiresAt != nil {
			resp["expiresAt"] = job.ExpiresAt.Format(time.RFC3339)
		}
	}
	if job.Status == pdfqueue.StatusFailed && job.Error != "" {
		resp["error"] = job.Error
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *handler) handleDownloadPDFJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(r.PathValue("id"))
	pdfData, err := h.pdfQueue.Download(r.Context(), jobID)
	if err != nil {
		switch {
		case errors.Is(err, pdfqueue.ErrNotFound):
			http.Error(w, "job not found", http.StatusNotFound)
		case errors.Is(err, pdfqueue.ErrNotReady):
			http.Error(w, "pdf is not ready", http.StatusConflict)
		case errors.Is(err, pdfqueue.ErrJobExpired):
			http.Error(w, "pdf has expired", http.StatusGone)
		default:
			http.Error(w, "failed to download pdf", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="cv.pdf"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfData)
}

func (h *handler) handleDefaultResume(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(cv.DefaultResume())
}

func (h *handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.Execute(w, nil); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

func (h *handler) handleSaveCV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCVBodyBytes)

	var req struct {
		Content string `json:"content"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		http.Error(w, "content cannot be empty", http.StatusBadRequest)
		return
	}

	if err := h.store.Save(r.Context(), req.Content); err != nil {
		http.Error(w, "failed to save cv", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func staticFileServer() http.Handler {
	fs := http.FileServer(http.Dir("web/static"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}
		fs.ServeHTTP(w, r)
	})
}

func clientIPFromRequest(r *http.Request) string {
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			if ip := normalizeIP(parts[0]); ip != "" {
				return ip
			}
		}
	}

	if realIP := normalizeIP(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		if ip := normalizeIP(host); ip != "" {
			return ip
		}
	}

	return normalizeIP(r.RemoteAddr)
}

func normalizeIP(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return ""
	}

	addr, err := netip.ParseAddr(candidate)
	if err != nil {
		return ""
	}

	return addr.String()
}
