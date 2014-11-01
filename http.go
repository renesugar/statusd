package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/zerok/statusd/Godeps/_workspace/src/gopkg.in/unrolled/render.v1"

	"github.com/zerok/statusd/Godeps/_workspace/src/github.com/gorilla/mux"
)

var Render = render.New(render.Options{Extensions: []string{".html"}})
var httpStatusRegistry StatusRegistry = NewStatusRegistry()
var httpStatusRegistryLock sync.RWMutex

type ServerStatusModel struct {
	Name   string
	Status string
}

type StatusOverviewModel struct {
	Servers []ServerStatusModel
}

// The HttpHandler sets up a HTTP endpoint to be used by 3rd parties to check if servers
// are available or not. It is also notified of any change to the status registry in order
// to notify live handlers.
func HttpHandler(httpAddr string, doneGroup *sync.WaitGroup) {
	statusUpdateChannel := make(chan StatusUpdate, 5)
	go func() {
		for {
			update, ok := <-statusUpdateChannel
			if !ok {
				break
			}
			httpStatusRegistryLock.Lock()
			httpStatusRegistry.SetStatusFromUpdate(update)
			httpStatusRegistryLock.Unlock()
		}
	}()
	statusRegistryManager.NotifyChange(statusUpdateChannel)
	defer statusRegistryManager.UnnotifyChange(statusUpdateChannel)
	log.Printf("Starting HTTP server on %s", httpAddr)
	router := mux.NewRouter()
	router.Path("/status/{server}/").HandlerFunc(httpServerStatusHandler)
	router.Path("/").HandlerFunc(httpFrontpageHandler)
	http.ListenAndServe(httpAddr, router)
}

func httpFrontpageHandler(w http.ResponseWriter, r *http.Request) {
	model := StatusOverviewModel{}
	httpStatusRegistryLock.RLock()
	for name, _ := range httpStatusRegistry {
		model.Servers = append(model.Servers, ServerStatusModel{Name: name, Status: httpStatusRegistry.GetStatus(name)})
	}
	httpStatusRegistryLock.RUnlock()
	Render.HTML(w, 200, "index", model)
}

func httpServerStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverName, found := vars["server"]
	if !found {
		http.NotFound(w, r)
		return
	}
	httpStatusRegistryLock.RLock()
	status := httpStatusRegistry.GetStatus(serverName)
	httpStatusRegistryLock.RUnlock()
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
