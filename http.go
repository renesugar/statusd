package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/zerok/statusd/Godeps/_workspace/src/gopkg.in/unrolled/render.v1"

	"github.com/zerok/statusd/Godeps/_workspace/src/github.com/gorilla/mux"
)

var Render = render.New(render.Options{Extensions: []string{".html"}})

func HttpHandler(httpAddr string, registry StatusRegistry, exitChannel <-chan struct{}, doneGroup *sync.WaitGroup) {
	log.Printf("Starting HTTP server on %s", httpAddr)
	router := mux.NewRouter()
	router.Path("/status/{server}/").HandlerFunc(httpServerStatusHandler)
	router.Path("/").HandlerFunc(httpFrontpageHandler)
	http.ListenAndServe(httpAddr, router)
}

func httpFrontpageHandler(w http.ResponseWriter, r *http.Request) {
	Render.HTML(w, 200, "index", struct{}{})
}

func httpServerStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverName, found := vars["server"]
	if !found {
		http.NotFound(w, r)
		return
	}
	statusRegistryLock.RLock()
	status := statusRegistry.GetStatus(serverName)
	statusRegistryLock.RUnlock()
	if status == "" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("mode") == "simple" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(status))
	} else {
		Render.JSON(w, http.StatusOK, struct {
			ServerName string `json:"server"`
			Status     string `json:"status"`
		}{
			serverName,
			status})
	}
}
