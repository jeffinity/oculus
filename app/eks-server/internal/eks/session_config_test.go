package eks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPathHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}

	got, err := expandPath("~/.ssh/jump_rsa")
	if err != nil {
		t.Fatalf("expandPath() error = %v", err)
	}
	want := filepath.Join(home, ".ssh/jump_rsa")
	if got != want {
		t.Fatalf("expandPath() = %q, want %q", got, want)
	}
}

func TestBuildSSHClientConfigRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		opts SessionOptions
		want string
	}{
		{
			name: "missing host",
			opts: SessionOptions{User: "jeff", IdentityFile: "~/.ssh/jump_rsa"},
			want: "jumpserver host is required",
		},
		{
			name: "missing user",
			opts: SessionOptions{Host: "jumpserver-sg.gainetics.io", IdentityFile: "~/.ssh/jump_rsa"},
			want: "jumpserver user is required",
		},
		{
			name: "missing identity",
			opts: SessionOptions{Host: "jumpserver-sg.gainetics.io", User: "jeff"},
			want: "jumpserver identity_file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildSSHClientConfig(tt.opts)
			if err == nil {
				t.Fatalf("buildSSHClientConfig() error = nil")
			}
			if err.Error() != tt.want {
				t.Fatalf("buildSSHClientConfig() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestPortOrDefault(t *testing.T) {
	if got := portOrDefault(0); got != 22 {
		t.Fatalf("portOrDefault(0) = %d, want 22", got)
	}
	if got := portOrDefault(2222); got != 2222 {
		t.Fatalf("portOrDefault(2222) = %d, want 2222", got)
	}
}
