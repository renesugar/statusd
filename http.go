package main

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/zerok/statusd/Godeps/_workspace/src/gopkg.in/unrolled/render.v1"

	"github.com/zerok/statusd/Godeps/_workspace/src/github.com/gorilla/mux"
)

var Render = render.New(render.Options{Extensions: []string{".html"}})
var httpStatusRegistry StatusRegistry = NewStatusRegistry()
var httpStatusRegistryManager = NewStatusRegistryManager()
var httpStatusRegistryLock sync.RWMutex

type ServerStatusModel struct {
	Name   string
	Status string
}

type StatusOverviewModel struct {
	Servers []ServerStatusModel
}

var upgrader = websocket.Upgrader{}

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
			httpStatusRegistryManager.SetStatus(update)
		}
	}()
	statusRegistryManager.NotifyChange(statusUpdateChannel)
	defer statusRegistryManager.UnnotifyChange(statusUpdateChannel)
	log.Printf("Starting HTTP server on %s", httpAddr)
	router := mux.NewRouter()
	router.Path("/status/{server}/").HandlerFunc(httpServerStatusHandler)
	router.Path("/overviewUpdates/").HandlerFunc(httpOverviewUpdatesHandler)
	router.Path("/").HandlerFunc(httpFrontpageHandler)
	http.ListenAndServe(httpAddr, router)
}

// httpOverviewUpdatesHandler offers a websocket channel that notifies the
// receiver of updates to any registered server.
func httpOverviewUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to build Websocket connect", http.StatusInternalServerError)
		return
	}
	updates := make(chan StatusUpdate, 5)
	readyToExit := false
	// Start a go-routine that drains the read messages and closes the connection
	// if requested by the user.
	go func() {
		defer httpStatusRegistryManager.UnnotifyChange(updates)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				conn.Close()
				readyToExit = true
				break
			}
		}
	}()
	httpStatusRegistryManager.NotifyChange(updates)
	defer httpStatusRegistryManager.UnnotifyChange(updates)
overviewUpdatesLoop:
	for {
		if readyToExit {
			break overviewUpdatesLoop
		}
		var update StatusUpdate
		var ok bool = true
		var updateAvailable = false

		select {
		case update, ok = <-updates:
			updateAvailable = true
		default:
			// We sleep and continue in order to keep the connection open
			// indefinitely in case no update is ever received.
			time.Sleep(500 * time.Millisecond)
			continue overviewUpdatesLoop
		}

		if !ok {
			break overviewUpdatesLoop
		}
		if updateAvailable {
			if err = conn.WriteJSON(update); err != nil {
				break overviewUpdatesLoop
			}
			updateAvailable = false
		}
	}
	conn.Close()
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
