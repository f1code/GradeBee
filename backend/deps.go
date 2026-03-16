// deps.go defines the dependency-injection interface used by HTTP handlers to
// obtain Google API service clients. The production implementation delegates to
// Clerk and Google; tests swap in a stub via the serviceDeps variable.
package handler

import "net/http"

// deps abstracts external service calls for testability.
type deps interface {
	// GoogleServices returns authenticated Google API clients for the user.
	GoogleServices(r *http.Request) (*googleServices, error)
}

// prodDeps is the real implementation that calls Clerk + Google APIs.
type prodDeps struct{}

func (prodDeps) GoogleServices(r *http.Request) (*googleServices, error) {
	return newGoogleServices(r)
}

// serviceDeps is the active dependency implementation. Tests override this.
var serviceDeps deps = prodDeps{}
