package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sync"
)

type SlackPayload struct {
	Text      string `json:"text"`
	Channel   string `json:"channel"`
	Username  string `json:"username"`
	IconEmoji string `json:"icon_emoji"`
}

func SlackNotifier(config Configuration, updateChannel chan StatusUpdate, doneGroup *sync.WaitGroup) {
	if len(config.Slack.NotifiedChannels) != 0 {
		// Now we have something to handle
	loop:
		for {
			status, more := <-updateChannel
			if !more {
				break loop
			}
			log.Printf("Notifying slack: %s\n", status)
			if err := notifySlack(status, config.Slack); err != nil {
				log.Printf("Slack notification failed: %s", err.Error())
			}
		}
	} else {
		log.Println("No slack configuration found.")
	}
	doneGroup.Done()
}

func notifySlack(status StatusUpdate, cfg SlackConfiguration) error {
	channels, ok := cfg.NotifiedChannels[status.ServerName]
	url := generateSlackUrl(cfg)
	if !ok || len(channels) == 0 {
		return nil
	}
	for _, channel := range channels {
		log.Printf("Notifying channel %s\n", channel)
		payload, err := buildSlackPayload(status, channel, cfg)
		if err != nil {
			return err
		}
		resp, err := http.PostForm(url, payload)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			body, bodyError := ioutil.ReadAll(resp.Body)
			if bodyError == nil {
				log.Printf("BODY: %s\n", string(body))
			}
			return fmt.Errorf("Slack notification failed: %d %s", resp.StatusCode, resp.Status)
		}
	}
	return nil
}

func generateSlackUrl(cfg SlackConfiguration) string {
	return fmt.Sprintf("https://%s.slack.com/services/hooks/incoming-webhook?token=%s", cfg.Team, cfg.Token)
}

func buildSlackPayload(status StatusUpdate, channel string, cfg SlackConfiguration) (url.Values, error) {
	result := make(url.Values)
	payload := SlackPayload{Text: fmt.Sprintf("%s is now *%s* (check time: %v)", status.ServerName, status.Status, status.Duration), Channel: channel}
	// TODO: Make slack name and icons configurable
	if status.Status == "offline" {
		payload.IconEmoji = ":exclamation:"
	} else {
		payload.IconEmoji = ":white_check_mark:"
	}
	payload.Username = "StatusD"
	rawData, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	result.Add("payload", string(rawData))
	return result, nil
}
