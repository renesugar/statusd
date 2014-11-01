package main

import (
	"sync"
	"time"
)

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

func (r StatusRegistry) SetStatusFromUpdate(update StatusUpdate) {
	r.SetStatus(update.ServerName, update.Status)
}

func (r StatusRegistry) GetStatus(name string) string {
	status, ok := r[name]
	if !ok {
		return ""
	}
	return status.Status
}

// The StatusRegistryManager is a singleton that is used for all
// write operations to the store and that is responsible for notifying
// any subscribers to any status changes to the StatusRegistry itself.
type StatusRegistryManager struct {
	registry             StatusRegistry
	lock                 sync.RWMutex
	notificationChannels map[chan StatusUpdate]struct{}
}

// NotifyChange registers a channel to be notified if an entry in the
// registry is updated
func (m *StatusRegistryManager) NotifyChange(channel chan StatusUpdate) {
	m.notificationChannels[channel] = struct{}{}
}

// UnnotifyChange removes the given channel from the notification list
func (m *StatusRegistryManager) UnnotifyChange(channel chan StatusUpdate) {
	delete(m.notificationChannels, channel)
}

func (m *StatusRegistryManager) notifyAll(update StatusUpdate) {
	for channel, _ := range m.notificationChannels {
		select {
		case channel <- update:
		default:
			// The send operation failed because the channel is most likely full. Skipping
		}
	}
}

func (m *StatusRegistryManager) GetStatus(serverName string) string {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.registry.GetStatus(serverName)
}

func (m *StatusRegistryManager) SetStatus(update StatusUpdate) {
	m.lock.Lock()
	m.registry.SetStatusFromUpdate(update)
	m.notifyAll(update)
	m.lock.Unlock()
}

func (m *StatusRegistryManager) ShowDown() {
	for channel, _ := range m.notificationChannels {
		close(channel)
	}
}

func NewStatusRegistryManager() StatusRegistryManager {
	m := StatusRegistryManager{
		notificationChannels: make(map[chan StatusUpdate]struct{}),
		registry:             NewStatusRegistry(),
	}
	return m
}
