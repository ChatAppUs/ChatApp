package main

import "net/http"

func (a *App) RegisterAnonymousRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/anonymous/preferences", a.requireAuth(a.handleGetPreferences))
	mux.HandleFunc("PUT /api/anonymous/preferences", a.requireAuth(a.handleUpdatePreferences))
	mux.HandleFunc("GET /api/me/emergency/panic-code", a.requireAuth(a.handleGetPanicCodeStatus))
	mux.HandleFunc("PUT /api/me/emergency/panic-code", a.requireAuth(a.handleEmergencyPanicCode))
	mux.HandleFunc("POST /api/me/emergency/soft-wipe", a.requireAuth(a.handleEmergencySoftWipe))
	mux.HandleFunc("POST /api/me/emergency/hard-wipe", a.requireAuth(a.handleEmergencyHardWipe))
}