package main

import (
	"testing"
)
func TestRegistryFinishWakesWaiter(t *testing.T) {
	r := NewRegistry()

	e, existed := r.Start("a")
	if existed {
		t.Fatal("first Start should create the entry")
	}

	_, existed = r.Start("a")
	if !existed {
		t.Fatal("second Start should find the existing entry")
	}

	r.Finish("a", nil)

	<-e.done // would hang forever if Finish didn't close it
}
