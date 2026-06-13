package handler

import "net/http"

type Handler interface {
	RegisterRoutes(mux *http.ServeMux)
}
