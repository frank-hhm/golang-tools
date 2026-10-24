package php

import (
	"fmt"
	"testing"
	"time"
)

func TestTmpFlow(t *testing.T) {
	ver := "7.4.33"
	if phpExe(ver, true) == "" {
		t.Skip("php 7.4.33 not installed")
	}
	cfg := InstanceConfig{Name: "tmpflow", Version: ver, NTS: true, Port: 18098, EnableLogs: true}
	if err := SaveInstance(cfg); err != nil {
		t.Fatal(err)
	}
	list, _ := ListInstances()
	var id string
	for _, c := range list {
		if c.Name == "tmpflow" {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatal("instance not saved")
	}
	defer RemoveInstance(id)
	t0 := time.Now()
	if err := StartInstance(id); err != nil {
		t.Fatalf("start: %v", err)
	}
	fmt.Println("start ok in", time.Since(t0))
	st, err := GetInstanceStatus(id)
	fmt.Printf("status: running=%v docRoot=%q configPath=%q\n", st.Running, st.DocRoot, st.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := RestartInstance(id); err != nil {
		t.Fatalf("restart: %v", err)
	}
	fmt.Println("restart ok")
	if err := StopInstance(id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	fmt.Println("stop ok")
}
