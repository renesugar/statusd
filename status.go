package main

import "time"

type StatusUpdate struct {
	ServerName string
	Status     string
	Duration   time.Duration
}

type ServerStatus struct {
	ServerName string
	Status     string
}

type StatusRegistry map[string]ServerStatus

func NewStatusRegistry() StatusRegistry {
	return make(StatusRegistry)
}

func (r StatusRegistry) SetStatus(name, status string) {
	oldStatus, found := r[name]
	if !found {
		oldStatus = ServerStatus{ServerName: name}
	}
	oldStatus.Status = status
	r[name] = oldStatus
}

func (r StatusRegistry) GetStatus(name string) string {
	status, ok := r[name]
	if !ok {
		return ""
	}
	return status.Status
}
