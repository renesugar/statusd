package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/zerok/statusd/Godeps/_workspace/src/gopkg.in/v2/yaml"
)

const (
	DEFAULT_TIMEOUT = 30
	DEFAULT_DELAY   = 30
	STATUS_OFFLINE  = "offline"
	STATUS_ONLINE   = "online"
)

type ServerConfiguration struct {
	IsAliveUrl string `yaml:"isAliveUrl"`
	Timeout    int    `yaml:"timeout"`
	Delay      int    `yaml:"delay"`
}

type HttpConfiguration struct {
	HostAddr string `yaml:"addr"`
}

type SlackConfiguration struct {
	Token            string              `yaml:"token"`
	Team             string              `yaml:"team"`
	NotifiedChannels map[string][]string `yaml:"channels"`
}

type Configuration struct {
	Servers map[string]ServerConfiguration `yaml:"servers"`
	Slack   SlackConfiguration             `yaml:"slack"`
	Http    HttpConfiguration              `yaml:"http"`
}

var statusRegistryLock *sync.RWMutex = &sync.RWMutex{}
var statusRegistry StatusRegistry = NewStatusRegistry()

// NewConfiguration parses YAML data provided through a Reader
// into our configuration object. If any error occurs, no
// Configuration will be returned and an error is generated.
func NewConfiguration(r io.Reader) (*Configuration, error) {
	result := &Configuration{}
	rawData, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(rawData, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// NewConfigurationFromFile creates a new Configuration struct from
// the file behind the given path.
func NewConfigurationFromFile(filepath string) (*Configuration, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return NewConfiguration(file)
}

// The StatusHandler updates the global server status mapping and triggers notifications
// if a server's status has changed.
func StatusHandler(config Configuration, registry StatusRegistry, statusUpdateChannel <-chan StatusUpdate, exitChannel chan struct{}, doneGroup *sync.WaitGroup) {
	notificationChannel := make(chan StatusUpdate, 10)
	notificationDoneGroup := sync.WaitGroup{}
	notificationDoneGroup.Add(1)
	go SlackNotifier(config, notificationChannel, &notificationDoneGroup)
loop:
	for {
		select {
		case status := <-statusUpdateChannel:
			statusRegistryLock.Lock()
			previousStatus := registry.GetStatus(status.ServerName)
			registry.SetStatus(status.ServerName, status.Status)
			statusRegistryLock.Unlock()
			// If this was the first time the server got a status, don't send out a notification to avoid
			// noise during restarts.
			log.Println(status)
			if previousStatus != "" {
				notificationChannel <- status
			} else {
				log.Println("Skipping first status from entering the notification chain")
			}
			break
		case <-exitChannel:
			break loop
		}
	}
	close(notificationChannel)
	log.Println("Waiting for notification handlers to shut down.")
	notificationDoneGroup.Wait()
	doneGroup.Done()
}

// ServerHandler is responsible for checking a single server periodically and
// reporting any status changes through the statusUpdateChannel.
func ServerHandler(serverName string, serverConfig ServerConfiguration, statusUpdateChannel chan<- StatusUpdate, exitChannel chan struct{}, doneGroup *sync.WaitGroup) {
	client := http.Client{}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return fmt.Errorf("Received a redirection as response")
	}
	previousStatus := ""
	newStatus := ""
	timeout := serverConfig.Timeout
	delay := serverConfig.Delay
	if timeout == 0 {
		timeout = DEFAULT_TIMEOUT
	}
	if delay == 0 {
		delay = DEFAULT_DELAY
	}
	finalTimeout, err := time.ParseDuration(fmt.Sprintf("%ds", timeout))
	if err != nil {
		finalTimeout = DEFAULT_TIMEOUT * time.Second
	}
	finalDelay, err := time.ParseDuration(fmt.Sprintf("%ds", delay))
	if err != nil {
		finalDelay = DEFAULT_DELAY * time.Second
	}
	client.Timeout = finalTimeout
	log.Printf("Processing server %v with a timeout of %vs\n", serverName, finalTimeout.Seconds())
	var nextPlannedCheck time.Time
loop:
	for {
		select {
		case <-exitChannel:
			log.Printf("Handler for %s received exit signal\n", serverName)
			break loop
		default:
		}

		if time.Now().Before(nextPlannedCheck) {
			time.Sleep(time.Second)
			continue
		}

		log.Printf("Checking %s\n", serverName)
		startTime := time.Now()
		resp, err := client.Get(serverConfig.IsAliveUrl)
		duration := time.Now().Sub(startTime)
		if err != nil {
			newStatus = STATUS_OFFLINE
		} else {
			if resp.StatusCode != 200 {
				newStatus = STATUS_OFFLINE

			} else {
				newStatus = STATUS_ONLINE
			}
		}
		if newStatus != previousStatus {
			statusUpdateChannel <- StatusUpdate{ServerName: serverName, Status: newStatus, Duration: duration}
		}
		previousStatus = newStatus

		// Check the server periodically
		if newStatus == STATUS_OFFLINE {
			nextPlannedCheck = time.Now().Add(finalDelay * 2)
		} else {
			nextPlannedCheck = time.Now().Add(finalDelay)
		}
	}
	log.Printf("Shutting down %s worker\n", serverName)
	doneGroup.Done()
}

func main() {
	var configPath string
	// First we have to determine what servers should be checked and how. For that we
	// parse our configuration file.
	flag.StringVar(&configPath, "config", "", "Path to a configuration file")
	flag.Parse()
	if configPath == "" {
		log.Fatalln("Please specify a configuration file using the -config flag")
	}
	config, err := NewConfigurationFromFile(configPath)
	if err != nil {
		log.Fatalln(err)
	}

	if config.Servers == nil || len(config.Servers) == 0 {
		log.Fatalln("No servers configured")
	}

	doneGroup := sync.WaitGroup{}
	exitChannel := make(chan struct{}, len(config.Servers))
	statusUpdateChannel := make(chan StatusUpdate, len(config.Servers))
	signalChannel := make(chan os.Signal)
	// For every server we create a seperate go-routine that checks the server periodically
	for serverName, serverConfig := range config.Servers {
		doneGroup.Add(1)
		go ServerHandler(serverName, serverConfig, statusUpdateChannel, exitChannel, &doneGroup)
	}

	doneGroup.Add(1)
	go StatusHandler(*config, statusRegistry, statusUpdateChannel, exitChannel, &doneGroup)

	if config.Http.HostAddr != "" {
		// Can't add a waitgroup handler for the HTTP server just yet. Perhaps in Go 1.4 ;)
		go HttpHandler(config.Http.HostAddr, statusRegistry, exitChannel, &doneGroup)
	} else {
		log.Println("No HTTP configuration present. Not starting HTTP server.")
	}

	go func() {
		for {
			sign := <-signalChannel
			log.Printf("Received %v. Shutting down workers", sign)
			for _ = range config.Servers {
				exitChannel <- struct{}{}
			}
			// An additional notification for the status handler and the web interface
			exitChannel <- struct{}{}
			exitChannel <- struct{}{}
		}
	}()

	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Waiting for threads to exit.")
	doneGroup.Wait()
	close(exitChannel)
	close(statusUpdateChannel)
}
