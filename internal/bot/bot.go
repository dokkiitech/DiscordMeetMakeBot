package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
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
		Description: "Google Meet のリンクを発行し、依頼者と招待ユーザーの DM に送信します",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "招待",
				Description: "招待する Discord ユーザー",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "招待2",
				Description: "追加で招待する Discord ユーザー（任意）",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        "招待3",
				Description: "追加で招待する Discord ユーザー（任意）",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "タイトル",
				Description: "会議のタイトル（任意）",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "分",
				Description: "会議の長さ（分単位、既定 30）",
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

	inviter := interactionUser(i)
	if inviter == nil {
		log.Printf("could not resolve inviter user")
		return
	}

	title := ""
	duration := 30 * time.Minute
	var invitees []*discordgo.User
	seen := map[string]bool{inviter.ID: true}
	for _, opt := range data.Options {
		switch opt.Name {
		case "タイトル":
			title = opt.StringValue()
		case "分":
			duration = time.Duration(opt.IntValue()) * time.Minute
		case "招待", "招待2", "招待3":
			u := opt.UserValue(s)
			if u == nil || u.Bot || seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			invitees = append(invitees, u)
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
		msg := "Meet リンクの作成に失敗しました。時間をおいて再度お試しください。"
		if _, ferr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &msg,
		}); ferr != nil {
			log.Printf("edit response: %v", ferr)
		}
		return
	}

	displayTitle := title
	if displayTitle == "" {
		displayTitle = "Google Meet"
	}

	participantMentions := make([]string, 0, 1+len(invitees))
	participantMentions = append(participantMentions, inviter.Mention())
	for _, u := range invitees {
		participantMentions = append(participantMentions, u.Mention())
	}
	dmContent := fmt.Sprintf(
		"**%s**\n依頼者: %s\n参加者: %s\n%s",
		displayTitle,
		inviter.Mention(),
		strings.Join(participantMentions, " "),
		url,
	)

	recipients := append([]*discordgo.User{inviter}, invitees...)
	var failed []string
	for _, u := range recipients {
		if err := sendDM(s, u.ID, dmContent); err != nil {
			log.Printf("send DM to %s: %v", u.ID, err)
			failed = append(failed, u.Mention())
		}
	}

	lines := []string{
		fmt.Sprintf("**%s** を作成しました。", displayTitle),
		fmt.Sprintf("依頼者: %s", inviter.Mention()),
	}
	if len(invitees) > 0 {
		ms := make([]string, 0, len(invitees))
		for _, u := range invitees {
			ms = append(ms, u.Mention())
		}
		lines = append(lines, fmt.Sprintf("招待: %s", strings.Join(ms, " ")))
	}
	lines = append(lines, "DM に Meet リンクを送信しました。")
	if len(failed) > 0 {
		lines = append(lines, fmt.Sprintf("次のユーザーには DM を送信できませんでした（DM 受信設定をご確認ください）: %s", strings.Join(failed, " ")))
	}
	channelMsg := strings.Join(lines, "\n")
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &channelMsg,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	}); err != nil {
		log.Printf("edit response: %v", err)
	}
}

func interactionUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	return i.User
}

func sendDM(s *discordgo.Session, userID, content string) error {
	ch, err := s.UserChannelCreate(userID)
	if err != nil {
		return fmt.Errorf("create dm channel: %w", err)
	}
	if _, err := s.ChannelMessageSend(ch.ID, content); err != nil {
		return fmt.Errorf("send dm message: %w", err)
	}
	return nil
}
