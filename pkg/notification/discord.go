package notification

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/autobrr/pkg/errors"
	"github.com/autobrr/autobrr/pkg/sharedhttp"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"
)

const (
	maxEmbedsPerMessage = 10
	maxCharactersPerMsg = 6000

	// hardcoded limit of fields to avoid hammering the api
	maxTotalFields = 250
)

type DiscordMessage struct {
	Content   any            `json:"content"`
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []DiscordEmbed `json:"embeds,omitempty"`
}

type DiscordEmbed struct {
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Color       int                  `json:"color"`
	Fields      []DiscordEmbedsField `json:"fields,omitempty"`
	Footer      DiscordEmbedsFooter  `json:"footer,omitzero"`
	Timestamp   time.Time            `json:"timestamp"`
}

type DiscordEmbedsFooter struct {
	Text string `json:"text"`
}

type DiscordEmbedsField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type EmbedColors int

const (
	LIGHT_BLUE EmbedColors = 0x58b9ff
	RED        EmbedColors = 0xed4245
	GREEN      EmbedColors = 0x57f287
	GRAY       EmbedColors = 0x99aab5
)

// Discord markdown characters that need escaping
var discordMarkdownChars = regexp.MustCompile(`([\\*_~` + "`" + `|>])`)

// escapeDiscordMarkdown escapes Discord markdown formatting characters
func escapeDiscordMarkdown(text string) string {
	if text == "" {
		return text
	}

	// Escape Discord markdown characters: \ * _ ~ ` | >
	// We use a regex to find and escape these characters
	return discordMarkdownChars.ReplaceAllString(text, `\$1`)
}

// DiscordRateLimit holds rate limit information from Discord headers
type DiscordRateLimit struct {
	Limit      int           // X-RateLimit-Limit
	Remaining  int           // X-RateLimit-Remaining
	ResetTime  time.Time     // X-RateLimit-Reset (unix timestamp)
	Bucket     string        // X-RateLimit-Bucket
	Scope      string        // X-RateLimit-Scope
	Global     bool          // X-RateLimit-Global
	RetryAfter time.Duration // Retry-After (if rate limited)
}

// RateLimiter manages Discord API rate limits
type RateLimiter struct {
	mu         sync.RWMutex
	buckets    map[string]*DiscordRateLimit
	globalLock *time.Time // When global rate limit expires
	log        *logrus.Entry
}

func NewRateLimiter(log *logrus.Entry) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*DiscordRateLimit),
		log:     log.WithField("component", "rate_limiter"),
	}
}

// Wait blocks until it's safe to make a request
func (rl *RateLimiter) Wait(bucket string) {
	rl.mu.RLock()

	// Check global rate limit first
	if rl.globalLock != nil && time.Now().Before(*rl.globalLock) {
		waitTime := time.Until(*rl.globalLock)
		rl.mu.RUnlock()
		rl.log.Warnf("Global rate limit active, waiting %v", waitTime)
		time.Sleep(waitTime)
		return
	}

	// Check bucket-specific rate limit
	if limit, exists := rl.buckets[bucket]; exists {
		if limit.Remaining <= 0 && time.Now().Before(limit.ResetTime) {
			waitTime := time.Until(limit.ResetTime)
			rl.mu.RUnlock()
			rl.log.Warnf("Bucket %s rate limited, waiting %v", bucket, waitTime.Truncate(time.Millisecond))
			time.Sleep(waitTime)
			return
		}
	}

	rl.mu.RUnlock()
}

// Update processes rate limit headers from Discord response
func (rl *RateLimiter) Update(bucket string, headers http.Header) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit := &DiscordRateLimit{Bucket: bucket}

	// Parse rate limit headers
	if val := headers.Get("X-RateLimit-Limit"); val != "" {
		rl.log.Tracef("X-RateLimit-Limit header: %s", val)
		if parsed, err := strconv.Atoi(val); err == nil {
			limit.Limit = parsed
		}
	}

	if val := headers.Get("X-RateLimit-Remaining"); val != "" {
		rl.log.Tracef("X-RateLimit-Remaining header: %s", val)
		if parsed, err := strconv.Atoi(val); err == nil {
			limit.Remaining = parsed
		}
	}

	if val := headers.Get("X-RateLimit-Reset"); val != "" {
		rl.log.Tracef("X-RateLimit-Reset header: %s", val)
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			limit.ResetTime = time.Unix(int64(parsed), 0)
		}
	}

	limit.Scope = headers.Get("X-RateLimit-Scope")
	limit.Global = headers.Get("X-RateLimit-Global") == "true"

	rl.log.Tracef("X-RateLimit-Scope header: %v", limit.Scope)
	rl.log.Tracef("X-RateLimit-Global header: %v", limit.Global)

	// Handle retry-after for rate limited requests
	if val := headers.Get("Retry-After"); val != "" {
		rl.log.Tracef("Retry-After header: %s", val)
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			limit.RetryAfter = time.Duration(parsed * float64(time.Second))

			// Set global lock if this is a global rate limit
			if limit.Global {
				globalUnlock := time.Now().Add(limit.RetryAfter)
				rl.globalLock = &globalUnlock
				rl.log.Warnf("Global rate limit detected, locked until %v", globalUnlock)
			}
		}
	}

	// Store bucket rate limit info
	rl.buckets[bucket] = limit

	rl.log.Tracef("Rate limit updated for bucket %s: %d/%d remaining, resets at %v",
		bucket, limit.Remaining, limit.Limit, limit.ResetTime)
}

// Clean removes expired rate limit entries
func (rl *RateLimiter) Clean() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Clean expired global lock
	if rl.globalLock != nil && now.After(*rl.globalLock) {
		rl.globalLock = nil
		rl.log.Debug("Global rate limit expired")
	}

	// Clean expired bucket limits
	for bucket, limit := range rl.buckets {
		if now.After(limit.ResetTime) {
			delete(rl.buckets, bucket)
			rl.log.Debugf("Cleaned expired rate limit for bucket %s", bucket)
		}
	}
}

type discordSender struct {
	log    *logrus.Entry
	config config.NotificationsConfig

	httpClient  *http.Client
	rateLimiter *RateLimiter
}

func (d *discordSender) Name() string {
	return "discord"
}

func NewDiscordSender(log *logrus.Entry, config config.NotificationsConfig) Sender {
	sender := &discordSender{
		log:    log.WithField("sender", "discord"),
		config: config,
		httpClient: &http.Client{
			Timeout:   time.Second * 30,
			Transport: sharedhttp.Transport,
		},
	}

	sender.rateLimiter = NewRateLimiter(sender.log)

	// Start cleanup routine
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			sender.rateLimiter.Clean()
		}
	}()

	return sender
}

// Calculate the actual JSON size of an embed
func (d *discordSender) calculateEmbedSize(embed DiscordEmbed) (int, error) {
	jsonData, err := json.Marshal(embed)
	if err != nil {
		return 0, err
	}
	return len(jsonData), nil
}

func (d *discordSender) Send(title string, description string, client string, runTime time.Duration, fields []Field, dryRun bool) error {
	var (
		allEmbeds   []DiscordEmbed
		totalFields = len(fields)
		timestamp   = time.Now()

		batches      [][]DiscordEmbed
		currentBatch []DiscordEmbed
		currentChars int
	)

	// Add (Dry Run) to title if enabled
	if dryRun {
		title = title + " [Dry Run]"
	}

	// if the config setting "skip_empty_run" is set to true, and there are no fields,
	// skip sending the message entirely.
	if totalFields == 0 && d.config.SkipEmptyRun {
		return nil
	}

	rt := runTime.Truncate(time.Millisecond).String()

	// only send a summary embed if no fields are present, there are more fields than allowed,
	// or the config setting "detailed" is set to false
	if totalFields == 0 || totalFields > maxTotalFields || !d.config.Detailed {
		allEmbeds = append(allEmbeds, DiscordEmbed{
			Title:       title,
			Description: description,
			Color:       int(LIGHT_BLUE),
			Footer: DiscordEmbedsFooter{
				Text: d.buildFooter(0, 0, client, rt),
			},
			Timestamp: timestamp,
		})
	} else {
		// Create one embed per torrent using the existing field data
		for i, field := range fields {
			embed := DiscordEmbed{
				Color:  int(LIGHT_BLUE),
				Fields: d.parseFieldValueToInlineFields(field.Value),
				Footer: DiscordEmbedsFooter{
					Text: d.buildFooter(i+1, totalFields, client, rt),
				},
				Timestamp: timestamp,
			}

			// Only add description if field name is not empty
			if field.Name != "" {
				embed.Description = fmt.Sprintf("**%s**", escapeDiscordMarkdown(field.Name))
			}

			allEmbeds = append(allEmbeds, embed)
		}

		// Add a summary embed if there is more than one field
		if totalFields > 1 {
			allEmbeds = append(allEmbeds, DiscordEmbed{
				Title:       fmt.Sprintf("%s - Summary", title),
				Description: description,
				Color:       int(LIGHT_BLUE),
				Footer: DiscordEmbedsFooter{
					Text: d.buildFooter(0, 0, client, rt),
				},
				Timestamp: timestamp,
			})
		}
	}

	// Batch embeds for messages (max 10 embeds per message)
	flush := func() {
		if len(currentBatch) == 0 {
			return
		}
		batches = append(batches, currentBatch)
		currentBatch = nil
		currentChars = 0
	}

	for _, e := range allEmbeds {
		eSize, err := d.calculateEmbedSize(e)
		if err != nil {
			return errors.Wrap(err, "failed to calculate embed size for batching")
		}

		// If adding this embed breaks either the embed-count or char limit, flush first
		if len(currentBatch) >= maxEmbedsPerMessage || currentChars+eSize > maxCharactersPerMsg {
			flush()
		}

		currentBatch = append(currentBatch, e)
		currentChars += eSize
	}
	flush()

	totalMsgs := len(batches)

	for i, batch := range batches {
		// Only set the title if it's the first embed in the batch and doesn't already have a title
		if batch[0].Title == "" {
			batch[0].Title = escapeDiscordMarkdown(title)

			// If more than one message, append the counter to the first embed’s title
			if totalMsgs > 1 {
				batch[0].Title = fmt.Sprintf("%s (%d/%d)", batch[0].Title, i+1, totalMsgs)
			}
		}

		msg := DiscordMessage{
			Content:   nil,
			Username:  d.config.Service.Discord.Username,
			AvatarURL: d.config.Service.Discord.AvatarURL,
			Embeds:    batch,
		}

		jsonData, err := json.Marshal(msg)
		if err != nil {
			return errors.Wrap(err, "could not marshal json request for a message chunk")
		}

		if sendErr := d.sendRequest(jsonData); sendErr != nil {
			return errors.Wrap(sendErr, "failed to send a message chunk to Discord")
		}

		d.log.Debugf("Sent Discord message %d/%d (%d embeds, %d chars).",
			i+1, totalMsgs, len(batch), len(jsonData))
	}

	d.log.Debugf("All %d Discord messages sent successfully.", totalMsgs)
	return nil
}

func (d *discordSender) CanSend() bool {
	return d.config.Service.Discord.WebhookURL != ""
}

func (d *discordSender) sendRequest(jsonData []byte) error {
	// Extract bucket identifier from webhook URL for rate limiting
	// Discord webhooks use a per-webhook bucket system
	bucket := d.getBucketFromURL(d.config.Service.Discord.WebhookURL)

	// Wait for rate limit clearance
	d.rateLimiter.Wait(bucket)

	req, err := http.NewRequest(http.MethodPost, d.config.Service.Discord.WebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return errors.Wrap(err, "could not create request")
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := d.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "client request error")
	}
	defer res.Body.Close()

	// Update rate limiter with response headers
	d.rateLimiter.Update(bucket, res.Header)

	d.log.Tracef("Discord response status: %d", res.StatusCode)

	// Handle rate limit responses
	if res.StatusCode == http.StatusTooManyRequests {
		body, readErr := io.ReadAll(bufio.NewReader(res.Body))
		if readErr != nil {
			return errors.Wrap(readErr, "could not read rate limit response body")
		}

		d.log.Warnf("Discord rate limit hit (429): %s", string(body))

		// The rate limiter has already been updated with retry-after info
		// Return error to indicate the request failed due to rate limiting
		return errors.New("discord rate limit exceeded, request will be retried")
	}

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		body, readErr := io.ReadAll(bufio.NewReader(res.Body))
		if readErr != nil {
			return errors.Wrap(readErr, "could not read body")
		}

		return errors.New("unexpected status: %v body: %v", res.StatusCode, string(body))
	}

	d.log.Debug("Notification successfully sent to discord")
	return nil
}

// getBucketFromURL extracts a bucket identifier from the webhook URL
// For Discord webhooks, we can use the webhook ID as the bucket identifier
func (d *discordSender) getBucketFromURL(webhookURL string) string {
	// Discord webhook URLs follow the pattern:
	// https://discord.com/api/webhooks/{webhook.id}/{webhook.token}
	parts := strings.Split(webhookURL, "/")
	if len(parts) >= 6 && parts[4] == "webhooks" {
		return fmt.Sprintf("webhook_%s", parts[5]) // Use webhook ID as bucket
	}

	// Fallback to generic bucket if URL parsing fails
	return "webhook_default"
}

// BuildField constructs a Field based on the provided action and build options.
func (d *discordSender) BuildField(action Action, opt BuildOptions) Field {
	switch action {
	case ActionRetag:
		return d.buildRetagField(opt.Torrent, opt.NewTags, opt.NewUpLimit)
	case ActionRelabel:
		return d.buildRelabelField(opt.Torrent, opt.NewLabel)
	case ActionClean:
		return d.buildGenericField(opt.Torrent, opt.RemovalReason)
	case ActionPause:
		return d.buildGenericField(opt.Torrent, "")
	case ActionOrphan:
		return d.buildOrphanField(opt.Orphan, opt.OrphanSize, opt.IsFile)
	}

	return Field{}
}

func (d *discordSender) buildRetagField(torrent config.Torrent, newTags []string, newUpLimit int64) Field {
	var inlineFields []DiscordEmbedsField

	equal := func(a, b string) bool {
		return strings.EqualFold(a, b)
	}

	limitStr := func(limit int64) string {
		if limit == -1 {
			return "Unlimited"
		}
		return fmt.Sprintf("%d KiB/s", limit)
	}

	oldTags := strings.Join(torrent.TagsSlice(), ", ")
	newTagsStr := strings.Join(newTags, ", ")
	oldUpLimit := limitStr(torrent.UpLimit)
	newUpLimitStr := limitStr(newUpLimit)

	// Add fields only if they're different
	if !equal(oldTags, newTagsStr) {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Old Tags",
			Value:  escapeDiscordMarkdown(oldTags),
			Inline: true,
		})
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "New Tags",
			Value:  escapeDiscordMarkdown(newTagsStr),
			Inline: true,
		})
	}

	if !equal(oldUpLimit, newUpLimitStr) {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Old Upload Limit",
			Value:  escapeDiscordMarkdown(oldUpLimit),
			Inline: true,
		})
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "New Upload Limit",
			Value:  escapeDiscordMarkdown(newUpLimitStr),
			Inline: true,
		})
	}

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: string(jsonData),
	}
}

func (d *discordSender) buildRelabelField(torrent config.Torrent, newLabel string) Field {
	var inlineFields []DiscordEmbedsField

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Old Label",
		Value:  escapeDiscordMarkdown(torrent.Label),
		Inline: true,
	})
	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "New Label",
		Value:  escapeDiscordMarkdown(newLabel),
		Inline: true,
	})

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: string(jsonData),
	}
}

func (d *discordSender) buildGenericField(torrent config.Torrent, reason string) Field {
	// Build inline fields directly and store as JSON in the value
	var inlineFields []DiscordEmbedsField

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Ratio",
		Value:  fmt.Sprintf("%.2f", torrent.Ratio),
		Inline: true,
	})

	if torrent.Label != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Label",
			Value:  escapeDiscordMarkdown(torrent.Label),
			Inline: true,
		})
	}

	if len(torrent.Tags) > 0 && strings.Join(torrent.TagsSlice(), ", ") != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Tags",
			Value:  escapeDiscordMarkdown(strings.Join(torrent.TagsSlice(), ", ")),
			Inline: true,
		})
	}

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Tracker",
		Value:  escapeDiscordMarkdown(torrent.TrackerName),
		Inline: true,
	})

	if torrent.TrackerStatus != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Tracker Status",
			Value:  escapeDiscordMarkdown(torrent.TrackerStatus),
			Inline: false,
		})
	}

	if reason != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Reason",
			Value:  escapeDiscordMarkdown(reason),
			Inline: false,
		})
	}

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: string(jsonData),
	}
}

func (d *discordSender) buildOrphanField(orphan string, orphanSize int64, isFile bool) Field {
	var inlineFields []DiscordEmbedsField

	prefix := "Folder"
	if isFile {
		prefix = "File"
	}

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Type",
		Value:  prefix,
		Inline: true,
	})

	if isFile {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Size",
			Value:  humanize.IBytes(uint64(orphanSize)),
			Inline: true,
		})
	}

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Path",
		Value:  escapeDiscordMarkdown(orphan),
		Inline: false,
	})

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  "", // Empty name since path is already in the Path field
		Value: string(jsonData),
	}
}

func (d *discordSender) buildFooter(progress int, totalFields int, client string, runTime string) string {
	if totalFields == 0 {
		return fmt.Sprintf("Client: %s | Started: %s ago", client, runTime)
	}

	return fmt.Sprintf("Progress: %d/%d | Client: %s | Started: %s ago", progress, totalFields, client, runTime)
}

// Updated parseFieldValueToInlineFields to handle JSON data
func (d *discordSender) parseFieldValueToInlineFields(value string) []DiscordEmbedsField {
	var fields []DiscordEmbedsField

	// Parse as JSON (all field types now use this format)
	if err := json.Unmarshal([]byte(value), &fields); err != nil {
		// Log error but return empty fields rather than fallback
		d.log.WithError(err).Error("Failed to parse field value as JSON")
		return []DiscordEmbedsField{}
	}

	return fields
}
