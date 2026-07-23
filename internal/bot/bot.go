package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/dokkiitech/discordmeetmakebot/internal/meet"
	"github.com/dokkiitech/discordmeetmakebot/internal/scheduler"
)

// jst is the timezone used to interpret and display scheduled meeting times.
var jst = loadJST()

func loadJST() *time.Location {
	if loc, err := time.LoadLocation("Asia/Tokyo"); err == nil {
		return loc
	}
	return time.FixedZone("JST", 9*60*60)
}

type Bot struct {
	session *discordgo.Session
	meet    *meet.Client
	guildID string
	sched   *scheduler.Scheduler
}

func New(token string, guildID string, meetClient *meet.Client) (*Bot, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("new discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuilds
	b := &Bot{session: s, meet: meetClient, guildID: guildID}
	b.sched = scheduler.New(os.Getenv("SCHEDULE_STORE_PATH"), b.sendReminder)
	return b, nil
}

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "meet",
		Description: "Google Meet のリンクを発行し、依頼者と招待ユーザーの DM に送信します",
	},
}

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

	if err := b.sched.Start(); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}
	return nil
}

func (b *Bot) Stop() error {
	b.sched.Stop()
	return b.session.Close()
}

// sendReminder delivers a scheduled reminder to each recipient's DM.
func (b *Bot) sendReminder(recipients []string, content string) {
	for _, id := range recipients {
		if err := sendDM(b.session, id, content); err != nil {
			log.Printf("send reminder DM to %s: %v", id, err)
		}
	}
}

var userMentionRe = regexp.MustCompile(`<@!?(\d+)>`)

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleMeetCommand(s, i)
	case discordgo.InteractionModalSubmit:
		b.handleMeetModalSubmit(s, i)
	}
}

func (b *Bot) handleMeetCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := i.ApplicationCommandData()
	if data.Name != "meet" {
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "meet_modal",
			Title:    "Meet リンクを作成",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "meet_invitees",
							Label:       "招待するユーザー",
							Style:       discordgo.TextInputShort,
							Required:    true,
							Placeholder: "@user1 @user2 のようにメンションで入力（スペース区切り）",
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID: "meet_title",
							Label:    "会議タイトル",
							Style:    discordgo.TextInputShort,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "meet_duration",
							Label:       "会議時間（分）",
							Style:       discordgo.TextInputShort,
							Placeholder: "30",
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "meet_schedule",
							Label:       "開始日時（JST、任意）",
							Style:       discordgo.TextInputShort,
							Placeholder: "例: 2026-07-10 15:00 / 07-10 15:00 / 15:00",
						},
					},
				},
			},
		},
	}); err != nil {
		log.Printf("respond modal: %v", err)
	}
}

func (b *Bot) handleMeetModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	if data.CustomID != "meet_modal" {
		return
	}

	values := modalValues(data.Components)

	inviter := interactionUser(i)
	if inviter == nil {
		log.Printf("could not resolve inviter user")
		return
	}

	inviteeRaw := values["meet_invitees"]
	title := values["meet_title"]
	durationRaw := values["meet_duration"]
	scheduleRaw := values["meet_schedule"]

	duration := 30 * time.Minute
	if strings.TrimSpace(durationRaw) != "" {
		min, err := strconv.Atoi(strings.TrimSpace(durationRaw))
		if err != nil || min < 1 || min > 600 {
			respondEphemeral(s, i, "会議時間は 1〜600 分の数値で入力してください。")
			return
		}
		duration = time.Duration(min) * time.Minute
	}

	invitees := resolveInvitees(s, nil, inviteeRaw, inviter.ID)
	if len(invitees) == 0 {
		respondEphemeral(s, i, "招待ユーザーを @メンションで1名以上指定してください。")
		return
	}

	now := time.Now()
	var start time.Time
	scheduled := false
	if strings.TrimSpace(scheduleRaw) != "" {
		t, err := parseSchedule(scheduleRaw, now.In(jst))
		if err != nil {
			respondEphemeral(s, i, "日時の形式を認識できませんでした。例: 2026-07-10 15:00 / 07-10 15:00 / 15:00（JST）")
			return
		}
		if !t.After(now) {
			respondEphemeral(s, i, "未来の日時を指定してください。")
			return
		}
		start = t
		scheduled = true
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Printf("defer response: %v", err)
		return
	}

	b.createAndDeliverMeeting(s, i, inviter, invitees, title, duration, scheduled, start)
}

func modalValues(components []discordgo.MessageComponent) map[string]string {
	values := make(map[string]string)
	for _, row := range components {
		actionsRow, ok := row.(*discordgo.ActionsRow)
		if !ok {
			if ar, ok := row.(discordgo.ActionsRow); ok {
				actionsRow = &ar
			} else {
				continue
			}
		}
		for _, comp := range actionsRow.Components {
			switch input := comp.(type) {
			case *discordgo.TextInput:
				values[input.CustomID] = input.Value
			case discordgo.TextInput:
				values[input.CustomID] = input.Value
			}
		}
	}
	return values
}

func (b *Bot) createAndDeliverMeeting(s *discordgo.Session, i *discordgo.InteractionCreate, inviter *discordgo.User, invitees []*discordgo.User, title string, duration time.Duration, scheduled bool, start time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	meetingStart := time.Now()
	if scheduled {
		meetingStart = start
	}
	url, err := b.meet.CreateMeetingAt(ctx, title, meetingStart, duration)
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
	participants := strings.Join(participantMentions, " ")
	recipients := append([]*discordgo.User{inviter}, invitees...)

	now := time.Now()
	delay := time.Duration(0)
	if scheduled {
		delay = start.Sub(now)
	}

	// Immediate delivery: no schedule given, or start is within 30 minutes.
	// The Meet URL is delivered right away.
	if !scheduled || delay <= 30*time.Minute {
		var dmContent string
		if scheduled {
			dmContent = fmt.Sprintf(
				"**%s**\n開始日時: %s\n依頼者: %s\n参加者: %s\n%s",
				displayTitle, formatJST(start), inviter.Mention(), participants, url,
			)
		} else {
			dmContent = fmt.Sprintf(
				"**%s**\n依頼者: %s\n参加者: %s\n%s",
				displayTitle, inviter.Mention(), participants, url,
			)
		}
		failed := b.deliverDM(s, recipients, dmContent)

		lines := []string{fmt.Sprintf("**%s** を作成しました。", displayTitle)}
		if scheduled {
			lines = append(lines, fmt.Sprintf("開始日時: %s", formatJST(start)))
		}
		lines = append(lines,
			fmt.Sprintf("依頼者: %s", inviter.Mention()),
			fmt.Sprintf("招待: %s", mentionsOf(invitees)),
			"DM に Meet リンクを送信しました。",
		)
		if len(failed) > 0 {
			lines = append(lines, fmt.Sprintf("次のユーザーには DM を送信できませんでした（DM 受信設定をご確認ください）: %s", strings.Join(failed, " ")))
		}
		b.editResponse(s, i, strings.Join(lines, "\n"))
		return
	}

	// Scheduled in the future: notify participants now (without the URL) and
	// deliver the Meet URL via a reminder 30 minutes before the start.
	twoDaysAhead := delay >= 48*time.Hour

	reminderNote := "開始30分前に Meet リンクをお送りします。"
	if twoDaysAhead {
		reminderNote = "開始1日前と30分前にリマインドします（Meet リンクは30分前にお送りします）。"
	}
	createContent := fmt.Sprintf(
		"**%s** の予約を作成しました。\n開始日時: %s\n依頼者: %s\n参加者: %s\n%s",
		displayTitle, formatJST(start), inviter.Mention(), participants, reminderNote,
	)
	failed := b.deliverDM(s, recipients, createContent)

	recipientIDs := make([]string, 0, len(recipients))
	for _, u := range recipients {
		recipientIDs = append(recipientIDs, u.ID)
	}

	base := randomID()
	var rems []scheduler.Reminder
	if twoDaysAhead {
		rems = append(rems, scheduler.Reminder{
			ID:         base + "-1d",
			FireAt:     start.Add(-24 * time.Hour),
			Recipients: recipientIDs,
			Content: fmt.Sprintf(
				"【リマインド】明日 %s に **%s** が予定されています。\n依頼者: %s\n参加者: %s\n開始30分前に Meet リンクをお送りします。",
				formatJST(start), displayTitle, inviter.Mention(), participants,
			),
		})
	}
	rems = append(rems, scheduler.Reminder{
		ID:         base + "-30m",
		FireAt:     start.Add(-30 * time.Minute),
		Recipients: recipientIDs,
		Content: fmt.Sprintf(
			"【リマインド】まもなく %s に **%s** が始まります（開始30分前）。\n依頼者: %s\n参加者: %s\n%s",
			formatJST(start), displayTitle, inviter.Mention(), participants, url,
		),
	})
	b.sched.Schedule(rems...)

	lines := []string{
		fmt.Sprintf("**%s** を予約しました。", displayTitle),
		fmt.Sprintf("開始日時: %s", formatJST(start)),
		fmt.Sprintf("依頼者: %s", inviter.Mention()),
		fmt.Sprintf("招待: %s", mentionsOf(invitees)),
		"DM に予約の通知を送信しました。" + reminderNote,
	}
	if len(failed) > 0 {
		lines = append(lines, fmt.Sprintf("次のユーザーには DM を送信できませんでした（DM 受信設定をご確認ください）: %s", strings.Join(failed, " ")))
	}
	b.editResponse(s, i, strings.Join(lines, "\n"))
}

func resolveInvitees(s *discordgo.Session, resolved map[string]*discordgo.User, raw, inviterID string) []*discordgo.User {
	matches := userMentionRe.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{inviterID: true}
	out := make([]*discordgo.User, 0, len(matches))
	for _, m := range matches {
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true

		var u *discordgo.User
		if resolved != nil {
			u = resolved[id]
		}
		if u == nil {
			fetched, err := s.User(id)
			if err != nil {
				log.Printf("fetch user %s: %v", id, err)
				continue
			}
			u = fetched
		}
		if u.Bot {
			continue
		}
		out = append(out, u)
	}
	return out
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		log.Printf("respond ephemeral: %v", err)
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

// deliverDM sends content to each recipient's DM and returns the mentions of
// users who could not be reached.
func (b *Bot) deliverDM(s *discordgo.Session, recipients []*discordgo.User, content string) []string {
	var failed []string
	for _, u := range recipients {
		if err := sendDM(s, u.ID, content); err != nil {
			log.Printf("send DM to %s: %v", u.ID, err)
			failed = append(failed, u.Mention())
		}
	}
	return failed
}

// editResponse replaces the deferred interaction response with msg, suppressing
// mention pings.
func (b *Bot) editResponse(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &msg,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	}); err != nil {
		log.Printf("edit response: %v", err)
	}
}

func mentionsOf(users []*discordgo.User) string {
	ms := make([]string, 0, len(users))
	for _, u := range users {
		ms = append(ms, u.Mention())
	}
	return strings.Join(ms, " ")
}

func formatJST(t time.Time) string {
	return t.In(jst).Format("2006-01-02 15:04") + " (JST)"
}

func randomID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// parseSchedule interprets a user-supplied date/time string in JST relative to
// now. It accepts full dates, month-day, and time-only forms; month-day and
// time-only values roll forward to the next future occurrence.
func parseSchedule(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty datetime")
	}

	type layout struct {
		fmt      string
		withYear bool
		timeOnly bool
	}
	layouts := []layout{
		{"2006-01-02 15:04", true, false},
		{"2006/01/02 15:04", true, false},
		{"2006-1-2 15:04", true, false},
		{"2006/1/2 15:04", true, false},
		{"01-02 15:04", false, false},
		{"1-2 15:04", false, false},
		{"01/02 15:04", false, false},
		{"1/2 15:04", false, false},
		{"15:04", false, true},
	}

	for _, l := range layouts {
		t, err := time.ParseInLocation(l.fmt, raw, jst)
		if err != nil {
			continue
		}
		switch {
		case l.timeOnly:
			t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, jst)
			if !t.After(now) {
				t = t.AddDate(0, 0, 1)
			}
		case !l.withYear:
			t = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, jst)
			if t.Before(now) {
				t = t.AddDate(1, 0, 0)
			}
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized datetime format: %q", raw)
}
