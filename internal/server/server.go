package server

import "net/http"

type Routes struct {
	Health   http.Handler
	Ingest   http.Handler
	GetEvent http.Handler
}

func New(r Routes) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /v1/events", r.Ingest)
	mux.Handle("GET /v1/events/{id}", r.GetEvent)
	mux.Handle("GET /healthz", r.Health)
	return mux
}
