package tidal

import (
	"context"
	"fmt"
)

// User is the account information from GET /users/me. TIDAL does not expose
// the subscription tier or maximum quality here.
type User struct {
	ID            string
	Username      string
	Country       string
	Email         string
	EmailVerified bool
}

// Me returns the logged-in user's account information.
func (c *Client) Me(ctx context.Context) (User, error) {
	doc, err := c.Get(ctx, "/users/me", nil)
	if err != nil {
		return User{}, err
	}
	r, err := doc.One()
	if err != nil {
		return User{}, fmt.Errorf("tidal: users/me: %w", err)
	}
	attrs, err := DecodeAttributes[struct {
		Username      string `json:"username"`
		Country       string `json:"country"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"emailVerified"`
	}](r)
	if err != nil {
		return User{}, err
	}
	return User{
		ID:            r.ID,
		Username:      attrs.Username,
		Country:       attrs.Country,
		Email:         attrs.Email,
		EmailVerified: attrs.EmailVerified,
	}, nil
}
