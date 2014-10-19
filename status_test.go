package main

import (
	"testing"
)

func TestStatusRegistrySetStatus(t *testing.T) {
	r := NewStatusRegistry()
	r.SetStatus("test", "value")
	if r["test"].Status != "value" {
		t.Error("SetStatus didn't update the internal state of the registry")
	}
}
