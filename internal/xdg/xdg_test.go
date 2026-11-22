package xdg

import (
	"errors"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Dirs
		wantErr error
	}{
		{
			name: "XDG variables set",
			env:  map[string]string{"XDG_CONFIG_HOME": "/xdg/config", "XDG_STATE_HOME": "/xdg/state", "HOME": "/home/u"},
			want: Dirs{Config: "/xdg/config/tidal-tui", State: "/xdg/state/tidal-tui"},
		},
		{
			name: "XDG variables unset fall back to HOME",
			env:  map[string]string{"HOME": "/home/u"},
			want: Dirs{Config: "/home/u/.config/tidal-tui", State: "/home/u/.local/state/tidal-tui"},
		},
		{
			name: "relative XDG values are ignored",
			env:  map[string]string{"XDG_CONFIG_HOME": "rel/config", "XDG_STATE_HOME": "./state", "HOME": "/home/u"},
			want: Dirs{Config: "/home/u/.config/tidal-tui", State: "/home/u/.local/state/tidal-tui"},
		},
		{
			name: "trailing slashes are cleaned",
			env:  map[string]string{"XDG_CONFIG_HOME": "/xdg/config/", "XDG_STATE_HOME": "/xdg/state//", "HOME": "/home/u"},
			want: Dirs{Config: "/xdg/config/tidal-tui", State: "/xdg/state/tidal-tui"},
		},
		{
			name: "XDG set without HOME",
			env:  map[string]string{"XDG_CONFIG_HOME": "/c", "XDG_STATE_HOME": "/s"},
			want: Dirs{Config: "/c/tidal-tui", State: "/s/tidal-tui"},
		},
		{
			name:    "no HOME and no config dir",
			env:     map[string]string{"XDG_STATE_HOME": "/s"},
			wantErr: ErrNoHome,
		},
		{
			name:    "no HOME and no state dir",
			env:     map[string]string{"XDG_CONFIG_HOME": "/c"},
			wantErr: ErrNoHome,
		},
		{
			name:    "relative HOME is rejected",
			env:     map[string]string{"HOME": "home/u"},
			wantErr: ErrNoHome,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(env(tt.env))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
