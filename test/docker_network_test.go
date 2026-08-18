package main

import "testing"

func TestShouldUseHostNetwork(t *testing.T) {
	tests := []struct {
		goos string
		want bool
	}{
		{goos: "linux", want: true},
		{goos: "darwin", want: false},
		{goos: "windows", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := shouldUseHostNetwork(tt.goos); got != tt.want {
				t.Fatalf("shouldUseHostNetwork(%q) = %t, want %t", tt.goos, got, tt.want)
			}
		})
	}
}
