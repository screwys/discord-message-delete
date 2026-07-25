package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"spoiler-cleaner/internal/bot"
)

type cleanerStore struct {
	value atomic.Value
}

const discordAttachmentFlagSpoiler discordgo.MessageAttachmentFlags = 1 << 3

func newCleanerStore(cleaner *bot.Cleaner) *cleanerStore {
	store := &cleanerStore{}
	store.value.Store(cleaner)
	return store
}

func (store *cleanerStore) get() *bot.Cleaner {
	cleaner, _ := store.value.Load().(*bot.Cleaner)
	return cleaner
}

func (store *cleanerStore) set(cleaner *bot.Cleaner) {
	store.value.Store(cleaner)
}

func main() {
	configPath := flag.String("config", "config.json", "path to the JSON configuration file")
	envPath := flag.String("env", ".env", "path to an optional dotenv file")
	reloadInterval := flag.Duration("reload-interval", 30*time.Second, "how often to reload config; set to 0 to disable")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags)

	if err := loadDotenv(*envPath); err != nil && !os.IsNotExist(err) {
		logger.Fatalf("load env file: %v", err)
	}

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		logger.Fatal("DISCORD_TOKEN is required")
	}

	cleaner, err := loadCleaner(*configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}
	activeCleaner := newCleanerStore(cleaner)

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		logger.Fatalf("create Discord session: %v", err)
	}
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent

	me, err := session.User("@me")
	if err != nil {
		logger.Fatalf("lookup bot user: %v", err)
	}

	session.AddHandler(func(_ *discordgo.Session, ready *discordgo.Ready) {
		logger.Printf("connected as %s", ready.User.String())
	})
	session.AddHandler(func(s *discordgo.Session, message *discordgo.MessageCreate) {
		handleMessage(s, message.Message, me.ID, activeCleaner, logger)
	})
	session.AddHandler(func(s *discordgo.Session, message *discordgo.MessageUpdate) {
		handleMessage(s, message.Message, me.ID, activeCleaner, logger)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *reloadInterval > 0 {
		go reloadConfigLoop(ctx, *configPath, *reloadInterval, activeCleaner, logger)
	}

	if err := session.Open(); err != nil {
		logger.Fatalf("open Discord session: %v", err)
	}
	defer session.Close()

	logger.Print("bot is running")
	<-ctx.Done()
	logger.Print("shutting down")
}

func loadCleaner(configPath string) (*bot.Cleaner, error) {
	config, err := bot.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return bot.NewCleaner(config), nil
}

func reloadConfigLoop(ctx context.Context, configPath string, interval time.Duration, activeCleaner *cleanerStore, logger *log.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastModTime := time.Time{}
	if info, err := os.Stat(configPath); err == nil {
		lastModTime = info.ModTime()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(configPath)
			if err != nil {
				logger.Printf("config stat failed; keeping previous config: %v", err)
				continue
			}
			if info.ModTime().Equal(lastModTime) {
				continue
			}

			cleaner, err := loadCleaner(configPath)
			if err != nil {
				logger.Printf("config reload failed; keeping previous config: %v", err)
				continue
			}
			activeCleaner.set(cleaner)
			lastModTime = info.ModTime()
			logger.Print("config reloaded")
		}
	}
}

func handleMessage(
	s *discordgo.Session,
	message *discordgo.Message,
	botUserID string,
	activeCleaner *cleanerStore,
	logger *log.Logger,
) {
	if message == nil || message.Author == nil || message.Author.ID == botUserID {
		return
	}

	cleaner := activeCleaner.get()
	if cleaner == nil {
		logger.Print("no active cleaner configured")
		return
	}

	decision := cleaner.Decide(messageFromDiscord(message))
	if !decision.Delete {
		return
	}

	if err := s.ChannelMessageDelete(message.ChannelID, message.ID); err != nil {
		logger.Printf("delete failed channel=%s message=%s author=%s reason=%q error=%v", message.ChannelID, message.ID, message.Author.ID, decision.Reason, err)
		return
	}
	logger.Printf("deleted channel=%s message=%s author=%s reason=%q", message.ChannelID, message.ID, message.Author.ID, decision.Reason)
}

func messageFromDiscord(message *discordgo.Message) bot.Message {
	if message == nil {
		return bot.Message{}
	}

	converted := bot.Message{
		Content: message.Content,
		Embeds:  embedsFromDiscord(message.Embeds),
	}
	if message.Author != nil {
		converted.AuthorID = message.Author.ID
	}
	appendDiscordMedia(&converted, message)
	return converted
}

func appendDiscordMedia(converted *bot.Message, message *discordgo.Message) {
	converted.Attachments = append(converted.Attachments, attachmentsFromDiscord(message.Attachments)...)
	if hasSpoileredVisualComponent(message.Components, false) {
		converted.SpoileredVisualMedia = true
	}
	for _, snapshot := range message.MessageSnapshots {
		if snapshot.Message != nil {
			appendDiscordMedia(converted, snapshot.Message)
		}
	}
}

func hasSpoileredVisualComponent(components []discordgo.MessageComponent, inheritedSpoiler bool) bool {
	for _, component := range components {
		if isSpoileredVisualComponent(component, inheritedSpoiler) {
			return true
		}
	}
	return false
}

func isSpoileredVisualComponent(component discordgo.MessageComponent, inheritedSpoiler bool) bool {
	switch component := component.(type) {
	case *discordgo.Thumbnail:
		return inheritedSpoiler || component.Spoiler
	case *discordgo.MediaGallery:
		return mediaGalleryHasSpoiler(component.Items, inheritedSpoiler)
	case *discordgo.Container:
		return hasSpoileredVisualComponent(component.Components, inheritedSpoiler || component.Spoiler)
	case *discordgo.Section:
		return isSpoileredVisualComponent(component.Accessory, inheritedSpoiler)
	default:
		return false
	}
}

func mediaGalleryHasSpoiler(items []discordgo.MediaGalleryItem, inheritedSpoiler bool) bool {
	for _, item := range items {
		if inheritedSpoiler || item.Spoiler {
			return true
		}
	}
	return false
}

func embedsFromDiscord(embeds []*discordgo.MessageEmbed) []bot.Embed {
	if len(embeds) == 0 {
		return nil
	}
	result := make([]bot.Embed, 0, len(embeds))
	for _, embed := range embeds {
		if embed == nil {
			continue
		}
		converted := bot.Embed{
			Title:       embed.Title,
			Description: embed.Description,
			URL:         embed.URL,
		}
		if embed.Author != nil {
			converted.AuthorName = embed.Author.Name
			converted.AuthorURL = embed.Author.URL
		}
		if embed.Provider != nil {
			converted.ProviderName = embed.Provider.Name
			converted.ProviderURL = embed.Provider.URL
		}
		if embed.Footer != nil {
			converted.FooterText = embed.Footer.Text
		}
		for _, field := range embed.Fields {
			if field == nil {
				continue
			}
			converted.Fields = append(converted.Fields, bot.EmbedField{
				Name:  field.Name,
				Value: field.Value,
			})
		}
		result = append(result, converted)
	}
	return result
}

func loadDotenv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return fmt.Errorf("%s:%d: empty key", path, lineNumber)
		}
		value = strings.Trim(value, `"'`)

		if os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("%s:%d: set %s: %w", path, lineNumber, key, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func attachmentsFromDiscord(attachments []*discordgo.MessageAttachment) []bot.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]bot.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment == nil {
			continue
		}
		result = append(result, bot.Attachment{
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			Width:       attachment.Width,
			Height:      attachment.Height,
			Spoiler:     attachment.Flags&discordAttachmentFlagSpoiler != 0,
		})
	}
	return result
}
