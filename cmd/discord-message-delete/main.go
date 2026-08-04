package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"discord-message-delete/internal/bot"
)

type cleanerStore struct {
	value atomic.Value
}

const discordAttachmentFlagSpoiler discordgo.MessageAttachmentFlags = 1 << 3

const serviceName = "discord-message-delete.service"
const serviceUsage = "usage: discord-message-delete {start|stop|restart|reload|status|enable|disable|logs}\n       discord-message-delete rule add <regex>"

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
	if handled, exitCode := runServiceCommand(os.Args[1:]); handled {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}

	configPath := flag.String("config", "config.json", "path to the JSON configuration file")
	envPath := flag.String("env", ".env", "path to an optional dotenv file")
	reloadInterval := flag.Duration("reload-interval", 30*time.Second, "how often to reload config; set to 0 to disable")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reloadRequested := make(chan os.Signal, 1)
	signal.Notify(reloadRequested, syscall.SIGHUP)
	defer signal.Stop(reloadRequested)

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
	go reloadConfigLoop(ctx, *configPath, *reloadInterval, reloadRequested, activeCleaner, logger)

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		logger.Fatalf("create Discord session: %v", err)
	}
	session.Identify.Intents = gatewayIntents()

	me, err := session.User("@me")
	if err != nil {
		logger.Fatalf("lookup bot user: %v", err)
	}

	session.AddHandler(func(_ *discordgo.Session, ready *discordgo.Ready) {
		logReady(logger, ready)
	})
	session.AddHandler(func(_ *discordgo.Session, _ *discordgo.Resumed) {
		logger.Print("gateway resumed")
	})
	session.AddHandler(func(s *discordgo.Session, message *discordgo.MessageCreate) {
		handleMessage(s, message.Message, "create", me.ID, activeCleaner, logger)
	})
	session.AddHandler(func(s *discordgo.Session, message *discordgo.MessageUpdate) {
		handleMessage(s, message.Message, "update", me.ID, activeCleaner, logger)
	})

	if err := session.Open(); err != nil {
		logger.Fatalf("open Discord session: %v", err)
	}
	defer session.Close()

	logger.Print("bot is running")
	<-ctx.Done()
	logger.Print("shutting down")
}

func gatewayIntents() discordgo.Intent {
	return discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent
}

func logReady(logger *log.Logger, ready *discordgo.Ready) {
	guilds := 0
	if ready != nil {
		guilds = len(ready.Guilds)
	}
	logger.Printf("connected guilds=%d", guilds)
}

func runServiceCommand(args []string) (bool, int) {
	if len(args) == 1 && args[0] == "help" {
		fmt.Fprintln(os.Stdout, serviceUsage)
		return true, 0
	}
	if len(args) > 0 && args[0] == "rule" {
		return true, runRuleCommand(args[1:])
	}

	command, commandArgs, handled, err := serviceCommand(args)
	if !handled {
		return false, 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return true, 1
	}

	return true, runCommand(command, commandArgs...)
}

func runRuleCommand(args []string) int {
	pattern, err := ruleToAdd(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	configPath, err := activeConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find active config: %v\n", err)
		return 1
	}
	if err := bot.AppendRegexRule(configPath, pattern); err != nil {
		fmt.Fprintf(os.Stderr, "add rule: %v\n", err)
		return 1
	}
	if exitCode := runCommand("systemctl", "--user", "reload", serviceName); exitCode != 0 {
		fmt.Fprintln(os.Stderr, "the rule was saved, but the running service could not reload it")
		return exitCode
	}
	fmt.Printf("added rule %q\n", pattern)
	return 0
}

func ruleToAdd(args []string) (string, error) {
	if len(args) != 2 || args[0] != "add" || args[1] == "" {
		return "", errors.New("usage: discord-message-delete rule add <regex>")
	}
	return args[1], nil
}

func activeConfigPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(resolved), "config.json"), nil
}

func runCommand(command string, args ...string) int {
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func serviceCommand(args []string) (string, []string, bool, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, false, nil
	}

	switch args[0] {
	case "start", "stop", "restart", "reload", "status", "enable", "disable":
		if len(args) != 1 {
			return "", nil, true, fmt.Errorf("usage: discord-message-delete %s", args[0])
		}
		return "systemctl", []string{"--user", args[0], serviceName}, true, nil
	case "logs":
		if len(args) != 1 {
			return "", nil, true, errors.New("usage: discord-message-delete logs")
		}
		return "journalctl", []string{"--user", "-u", serviceName, "-f"}, true, nil
	default:
		return "", nil, true, fmt.Errorf("unknown command %q; expected start, stop, restart, reload, status, enable, disable, logs, or rule", args[0])
	}
}

func loadCleaner(configPath string) (*bot.Cleaner, error) {
	config, err := bot.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return bot.NewCleaner(config), nil
}

func reloadConfigLoop(
	ctx context.Context,
	configPath string,
	interval time.Duration,
	reloadRequested <-chan os.Signal,
	activeCleaner *cleanerStore,
	logger *log.Logger,
) {
	var ticker *time.Ticker
	var ticks <-chan time.Time
	if interval > 0 {
		ticker = time.NewTicker(interval)
		ticks = ticker.C
		defer ticker.Stop()
	}

	lastModTime := time.Time{}
	if info, err := os.Stat(configPath); err == nil {
		lastModTime = info.ModTime()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-reloadRequested:
			if reloadCleaner(configPath, activeCleaner, logger) {
				if info, err := os.Stat(configPath); err == nil {
					lastModTime = info.ModTime()
				}
			}
		case <-ticks:
			info, err := os.Stat(configPath)
			if err != nil {
				logger.Printf("config stat failed; keeping previous config: %v", err)
				continue
			}
			if info.ModTime().Equal(lastModTime) {
				continue
			}

			if reloadCleaner(configPath, activeCleaner, logger) {
				lastModTime = info.ModTime()
			}
		}
	}
}

func reloadCleaner(configPath string, activeCleaner *cleanerStore, logger *log.Logger) bool {
	cleaner, err := loadCleaner(configPath)
	if err != nil {
		logger.Printf("config reload failed; keeping previous config: %v", err)
		return false
	}
	activeCleaner.set(cleaner)
	logger.Print("config reloaded")
	return true
}

func handleMessage(
	s *discordgo.Session,
	message *discordgo.Message,
	event string,
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
	if decision.MessageCheck != nil {
		logMessageCheck(logger, event, decision)
	}
	if decision.SpoilerCheck != nil {
		logSpoilerCheck(logger, event, decision)
	}
	if !decision.Delete {
		return
	}

	if err := s.ChannelMessageDelete(message.ChannelID, message.ID); err != nil {
		logDeleteFailure(logger, event, decision.Kind, err)
		return
	}
	logger.Printf("delete succeeded event=%s rule=%s", event, decision.Kind)
}

func logMessageCheck(logger *log.Logger, event string, decision bot.Decision) {
	check := decision.MessageCheck
	logger.Printf(
		"message check event=%s ignored=%t rules_loaded=%d searchable_text=%t regex_evaluated=%t original_matched=%t normalization_changed=%t normalized_matched=%t regex_matched=%t delete=%t",
		event,
		check.Ignored,
		check.RegexRules,
		check.SearchableText,
		check.RegexEvaluated,
		check.OriginalMatched,
		check.NormalizationChanged,
		check.NormalizedMatched,
		check.RegexMatched,
		decision.Delete,
	)
}

func logSpoilerCheck(logger *log.Logger, event string, decision bot.Decision) {
	check := decision.SpoilerCheck
	logger.Printf(
		"spoiler check event=%s attachments=%d flags=%d legacy_markers=%d images=%d matching_attachments=%d visual_components=%t delete=%t",
		event,
		check.Attachments,
		check.FlaggedAttachments,
		check.LegacyMarkers,
		check.ImageAttachments,
		check.MatchingAttachments,
		check.SpoileredVisualMedia,
		decision.Delete && decision.Kind == bot.DecisionSpoilerMedia,
	)
}

func logDeleteFailure(logger *log.Logger, event string, kind bot.DecisionKind, err error) {
	var restError *discordgo.RESTError
	if errors.As(err, &restError) {
		httpStatus := 0
		if restError.Response != nil {
			httpStatus = restError.Response.StatusCode
		}
		discordCode := 0
		if restError.Message != nil {
			discordCode = restError.Message.Code
		}
		logger.Printf(
			"delete failed event=%s rule=%s http_status=%d discord_code=%d",
			event, kind, httpStatus, discordCode,
		)
		return
	}
	logger.Printf("delete failed event=%s rule=%s error_type=%T", event, kind, err)
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
