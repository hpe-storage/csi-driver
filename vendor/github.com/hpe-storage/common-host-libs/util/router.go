package util

// Copyright 2019 Hewlett Packard Enterprise Development LP.
import (
	"github.com/gorilla/mux"
	log "github.com/hpe-storage/common-host-libs/logger"
	"net/http"
)

// Route struct
type Route struct {
	Name        string
	Method      string
	Pattern     string
	HandlerFunc http.HandlerFunc
}

// InitializeRouter initializes all handlers
func InitializeRouter(router *mux.Router, routes []Route) (err error) {
	for _, route := range routes {
		var handler http.Handler

		handler = route.HandlerFunc
		handler = log.HTTPLogger(handler, route.Name)
		router.
			Methods(route.Method).
			Path(route.Pattern).
			Name(route.Name).
			Handler(handler)
	}
	return nil
}
