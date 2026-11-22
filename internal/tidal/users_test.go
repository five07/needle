package tidal

import (
	"context"
	"errors"
	"testing"
)

func TestMe(t *testing.T) {
	tests := []struct {
		name    string
		reply   reply
		want    User
		wantErr bool
		errIs   error
	}{
		{
			name: "live response shape",
			reply: reply{status: 200, body: `{"data":{"id":"208583796","type":"users","attributes":{
				"country":"US","developerAccessTier":"THIRD_PARTY","emailVerified":true,
				"username":"listener@example.com","email":"listener@example.com"}},"links":{"self":"/users/me"}}`},
			want: User{ID: "208583796", Username: "listener@example.com", Country: "US", Email: "listener@example.com", EmailVerified: true},
		},
		{name: "array data", reply: reply{status: 200, body: `{"data":[]}`}, wantErr: true, errIs: ErrNoData},
		{name: "bad attributes", reply: reply{status: 200, body: `{"data":{"id":"1","type":"users","attributes":{"country":5}}}`}, wantErr: true},
		{name: "unauthorized", reply: reply{status: 401}, wantErr: true, errIs: ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replies := []reply{tt.reply}
			if tt.reply.status == 401 {
				replies = append(replies, tt.reply) // refresh then 401 again
			}
			f := newFakeAPI(t, replies...)
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA, refreshed: tokenB})

			got, err := c.Me(context.Background())

			if tt.wantErr {
				if err == nil || (tt.errIs != nil && !errors.Is(err, tt.errIs)) {
					t.Fatalf("err = %v, want %v", err, tt.errIs)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Me() = %+v, want %+v", got, tt.want)
			}
			if p := f.requests()[0].path; p != "/v2/users/me" {
				t.Errorf("path = %s", p)
			}
		})
	}
}
