package providers

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2"

	oidc "github.com/coreos/go-oidc"
)

// OIDCProvider represents an OIDC based Identity Provider
type OIDCProvider struct {
	*ProviderData

	Verifier *oidc.IDTokenVerifier
}

// NewOIDCProvider initiates a new OIDCProvider
func NewOIDCProvider(p *ProviderData) *OIDCProvider {
	p.ProviderName = "OpenID Connect"
	return &OIDCProvider{ProviderData: p}
}

// Redeem exchanges the OAuth2 authentication token for an ID token
func (p *OIDCProvider) Redeem(redirectURL, code string) (s *SessionState, err error) {
	ctx := context.Background()
	c := oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: p.RedeemURL.String(),
		},
		RedirectURL: redirectURL,
	}
	token, err := c.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %v", err)
	}
	s, err = p.createSessionState(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("unable to update session: %v", err)
	}
	return
}

// RefreshSessionIfNeeded checks if the session has expired and uses the
// RefreshToken to fetch a new ID token if required
func (p *OIDCProvider) RefreshSessionIfNeeded(s *SessionState) (bool, error) {
	if s == nil || timeVal(s.ExpiresOn).After(time.Now()) || s.RefreshToken == "" {
		return false, nil
	}

	origExpiration := s.ExpiresOn

	err := p.redeemRefreshToken(s)
	if err != nil {
		return false, fmt.Errorf("unable to redeem refresh token: %v", err)
	}

	fmt.Printf("refreshed id token %s (expired on %s)\n", s, origExpiration)
	return true, nil
}

func (p *OIDCProvider) redeemRefreshToken(s *SessionState) (err error) {
	c := oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: p.RedeemURL.String(),
		},
	}
	ctx := context.Background()
	t := &oauth2.Token{
		RefreshToken: s.RefreshToken,
		Expiry:       time.Now().Add(-time.Hour),
	}
	token, err := c.TokenSource(ctx, t).Token()
	if err != nil {
		return fmt.Errorf("failed to get token: %v", err)
	}
	newSession, err := p.createSessionState(ctx, token)
	if err != nil {
		return fmt.Errorf("unable to update session: %v", err)
	}
	s.AccessToken = newSession.AccessToken
	s.IDToken = newSession.IDToken
	s.RefreshToken = newSession.RefreshToken
	s.ExpiresOn = newSession.ExpiresOn
	s.Email = newSession.Email
	return
}

func (p *OIDCProvider) createSessionState(ctx context.Context, token *oauth2.Token) (*SessionState, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("token response did not contain an id_token")
	}

	// Parse and verify ID Token payload.
	idToken, err := p.Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("could not verify id_token: %v", err)
	}

	// Extract custom claims.
	var claims struct {
		Email    string `json:"email"`
		Verified *bool  `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse id_token claims: %v", err)
	}

	if claims.Email == "" {
		return nil, fmt.Errorf("id_token did not contain an email")
	}
	if claims.Verified != nil && !*claims.Verified {
		return nil, fmt.Errorf("email in id_token (%s) isn't verified", claims.Email)
	}

	return &SessionState{
		AccessToken:  token.AccessToken,
		IDToken:      rawIDToken,
		RefreshToken: token.RefreshToken,
		ExpiresOn:    &token.Expiry,
		Email:        claims.Email,
	}, nil
}

// ValidateSessionState checks that the session's IDToken is still valid
func (p *OIDCProvider) ValidateSessionState(s *SessionState) bool {
	ctx := context.Background()
	_, err := p.Verifier.Verify(ctx, s.IDToken)
	if err != nil {
		return false
	}

	return true
}
