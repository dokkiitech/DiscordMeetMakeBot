package meet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Client wraps Google Calendar API and issues Meet conference URLs
// by creating short calendar events with a Hangouts/Meet conference attached.
type Client struct {
	svc        *calendar.Service
	calendarID string
}

// Config holds OAuth2 credentials for a Google account that owns a calendar
// where Meet links can be created.
type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	CalendarID   string
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RefreshToken == "" {
		return nil, fmt.Errorf("google oauth credentials are not fully configured")
	}
	calID := cfg.CalendarID
	if calID == "" {
		calID = "primary"
	}

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
		Scopes: []string{calendar.CalendarEventsScope},
	}
	tokenSrc := oauthCfg.TokenSource(ctx, &oauth2.Token{
		RefreshToken: cfg.RefreshToken,
	})

	svc, err := calendar.NewService(ctx, option.WithTokenSource(tokenSrc))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}
	return &Client{svc: svc, calendarID: calID}, nil
}

// CreateMeeting creates a calendar event with a Meet conference and returns the
// Meet join URL. The event is short (30 minutes by default starting now).
func (c *Client) CreateMeeting(ctx context.Context, title string, duration time.Duration) (string, error) {
	if title == "" {
		title = "Discord Meet"
	}
	if duration <= 0 {
		duration = 30 * time.Minute
	}
	now := time.Now()
	end := now.Add(duration)

	reqID, err := randomRequestID()
	if err != nil {
		return "", err
	}

	event := &calendar.Event{
		Summary:     title,
		Description: "Created by DiscordMeetMakeBot",
		Start: &calendar.EventDateTime{
			DateTime: now.Format(time.RFC3339),
		},
		End: &calendar.EventDateTime{
			DateTime: end.Format(time.RFC3339),
		},
		ConferenceData: &calendar.ConferenceData{
			CreateRequest: &calendar.CreateConferenceRequest{
				RequestId: reqID,
				ConferenceSolutionKey: &calendar.ConferenceSolutionKey{
					Type: "hangoutsMeet",
				},
			},
		},
	}

	created, err := c.svc.Events.
		Insert(c.calendarID, event).
		ConferenceDataVersion(1).
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("insert event: %w", err)
	}

	if created.HangoutLink != "" {
		return created.HangoutLink, nil
	}
	if created.ConferenceData != nil {
		for _, ep := range created.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && ep.Uri != "" {
				return ep.Uri, nil
			}
		}
	}
	return "", fmt.Errorf("meet link was not returned by Google Calendar")
}

func randomRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
