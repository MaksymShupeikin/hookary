package main

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

// newRouter wires the middleware stack and the routes.
//
// Order matters: request id first so everything downstream can log it, recovery
// before the handlers it protects, and CORS before any route that a browser
// preflights.
func (s *server) newRouter(ready shared.ReadyFunc) http.Handler {
	r := chi.NewRouter()

	r.Use(withRequestID)
	r.Use(withLogging(s.log))
	r.Use(withRecovery(s.log))
	r.Use(corsMiddleware(s.cfg))

	r.Method(http.MethodGet, "/health/live", shared.LiveHandler())
	r.Method(http.MethodGet, "/health/ready", shared.ReadyHandler(ready))

	r.Route("/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			// Public: these are how a caller gets a token in the first place.
			r.Post("/google", s.handleGoogleAuth)
			r.Post("/refresh", s.handleRefresh)
			r.With(s.optionalAuth).Post("/logout", s.handleLogout)
		})

		r.With(s.requireAuth).Get("/me", s.handleMe)
		// Browser WebSockets cannot attach an Authorization header. This route
		// authenticates the first frame before subscribing to any project.
		r.Get("/ws", s.handleJobsWebSocket)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Get("/projects", s.handleListProjects)
			r.Post("/projects", s.handleCreateProject)
			r.Get("/projects/{project_id}", s.handleGetProject)
			r.Get("/projects/{project_id}/assets", s.handleListProjectAssets)
			r.Delete("/projects/{project_id}", s.handleDeleteProject)

			r.Post("/uploads", s.handleCreateUpload)
			r.Get("/uploads/{upload_id}", s.handleGetUpload)
			r.Post("/uploads/{upload_id}/parts", s.handleUploadPartURLs)
			r.Post("/uploads/{upload_id}/complete", s.handleCompleteUpload)
			r.Delete("/uploads/{upload_id}", s.handleAbortUpload)
			r.Post("/imports", s.handleCreateURLImport)

			r.Post("/assets/{asset_id}/preview-url", s.handleAssetPreviewURL)
			r.Post("/assets/{asset_id}/analyze", s.handleAnalyzeAsset)

			r.Get("/jobs/{job_id}", s.handleGetJob)
			r.Post("/jobs/{job_id}/retry", s.handleRetryJob)
			r.Get("/projects/{project_id}/jobs", s.handleListProjectJobs)
			r.Get("/projects/{project_id}/clips", s.handleListProjectClips)
			r.Post("/projects/{project_id}/clips/render", s.handleRenderProjectClips)
			r.Post("/clips/{clip_id}/preview-url", s.handleClipPreviewURL)
			r.Post("/clips/{clip_id}/download-url", s.handleClipDownloadURL)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "No such endpoint.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed",
			"That method is not allowed here.")
	})

	return r
}

// corsMiddleware allows only the configured origins.
//
// The list is never "*": credentials ride on these requests, and a wildcard
// would let any page on the internet make them.
func corsMiddleware(cfg shared.Config) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSAllowedOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", requestIDHeader, "Idempotency-Key"},
		ExposedHeaders: []string{requestIDHeader},
		// The refresh token is an HttpOnly cookie on web, and the browser only
		// sends it when credentials are allowed.
		AllowCredentials: true,
		MaxAge:           int((12 * time.Hour).Seconds()),
	})
}
