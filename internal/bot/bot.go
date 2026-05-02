package bot

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/dokkiitech/discordmeetmakebot/internal/meet"
)

type Bot struct {
	session *discordgo.Session
	meet    *meet.Client
	guildID string
}

func New(token string, guildID string, meetClient *meet.Client) (*Bot, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("new discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuilds
	return &Bot{session: s, meet: meetClient, guildID: guildID}, nil
}

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "meet",
		Description: "Issue a Google Meet URL",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "title",
				Description: "Meeting title (optional)",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "minutes",
				Description: "Meeting duration in minutes (default 30)",
				Required:    false,
				MinValue:    ptrFloat(1),
				MaxValue:    600,
			},
		},
	},
}

func ptrFloat(v float64) *float64 { return &v }

func (b *Bot) Start(ctx context.Context) error {
	b.session.AddHandler(b.handleInteraction)

	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open session: %w", err)
	}

	registered, err := b.session.ApplicationCommandBulkOverwrite(b.session.State.User.ID, b.guildID, commands)
	if err != nil {
		return fmt.Errorf("register slash commands: %w", err)
	}
	log.Printf("registered %d slash command(s)", len(registered))
	return nil
}

func (b *Bot) Stop() error {
	return b.session.Close()
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := i.ApplicationCommandData()
	if data.Name != "meet" {
		return
	}

	title := ""
	duration := 30 * time.Minute
	for _, opt := range data.Options {
		switch opt.Name {
		case "title":
			title = opt.StringValue()
		case "minutes":
			duration = time.Duration(opt.IntValue()) * time.Minute
		}
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Printf("defer response: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	url, err := b.meet.CreateMeeting(ctx, title, duration)
	if err != nil {
		log.Printf("create meeting: %v", err)
		_, ferr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: ptrString("Failed to create a Meet link. Please try again later."),
		})
		if ferr != nil {
			log.Printf("edit response: %v", ferr)
		}
		return
	}

	displayTitle := title
	if displayTitle == "" {
		displayTitle = "Google Meet"
	}
	content := fmt.Sprintf("**%s**\n%s", displayTitle, url)
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	}); err != nil {
		log.Printf("edit response: %v", err)
	}
}

func ptrString(s string) *string { return &s }
