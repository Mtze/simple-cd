package main

import "testing"

func TestProcessRunnerRefusesHTTPS(t *testing.T) {
	t.Setenv("SIMPLECD_RUNNER", "process")
	if _, err := newRunner("https://cd.example.org"); err == nil {
		t.Fatal("process runner accepted an https base URL")
	}
	if _, err := newRunner("http://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
}
