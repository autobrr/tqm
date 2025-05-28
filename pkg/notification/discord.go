package notification

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	Content interface{}    `json:"content"`
	Embeds  []DiscordEmbed `json:"embeds,omitempty"`
}

type DiscordEmbed struct {
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Color       int                  `json:"color"`
	Fields      []DiscordEmbedsField `json:"fields,omitempty"`
	Footer      DiscordEmbedsFooter  `json:"footer,omitempty"`
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

type discordSender struct {
	log    *logrus.Entry
	config config.NotificationsConfig

	httpClient *http.Client
}

func (d *discordSender) Name() string {
	return "discord"
}

func NewDiscordSender(log *logrus.Entry, config config.NotificationsConfig) Sender {
	return &discordSender{
		log:    log.WithField("sender", "discord"),
		config: config,
		httpClient: &http.Client{
			Timeout:   time.Second * 30,
			Transport: sharedhttp.Transport,
		},
	}
}

// Calculate the actual JSON size of an embed
func (d *discordSender) calculateEmbedSize(embed DiscordEmbed) (int, error) {
	jsonData, err := json.Marshal(embed)
	if err != nil {
		return 0, err
	}
	return len(jsonData), nil
}

func (d *discordSender) Send(title string, description string, runTime time.Duration, fields []Field, dryRun bool) error {
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
		title = title + " (Dry Run)"
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
				Text: d.buildFooter(0, totalFields, rt),
			},
			Timestamp: timestamp,
		})
	} else {
		// Create one embed per torrent using the existing field data
		for i, field := range fields {
			embed := DiscordEmbed{
				Title:  title,
				Color:  int(LIGHT_BLUE),
				Fields: d.parseFieldValueToInlineFields(field.Value),
				Footer: DiscordEmbedsFooter{
					Text: d.buildFooter(i+1, totalFields, rt),
				},
				Timestamp: timestamp,
			}

			// Only add description if field name is not empty
			if field.Name != "" {
				embed.Description = fmt.Sprintf("**%s**", field.Name)
			}

			allEmbeds = append(allEmbeds, embed)
		}
		allEmbeds = append(allEmbeds, DiscordEmbed{
			Title:       fmt.Sprintf("%s - Summary", title),
			Description: description,
			Color:       int(LIGHT_BLUE),
			Footer: DiscordEmbedsFooter{
				Text: d.buildFooter(0, 0, rt),
			},
			Timestamp: timestamp,
		})
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
		msg := DiscordMessage{
			Content: nil,
			Embeds:  batch,
		}
		jsonData, err := json.Marshal(msg)
		if err != nil {
			return errors.Wrap(err, "could not marshal json request for a message chunk")
		}
		if sendErr := d.sendRequest(jsonData); sendErr != nil {
			return errors.Wrap(err, "failed to send a message chunk to Discord")
		}

		d.log.Debugf("Sent Discord message %d/%d (%d embeds, %d chars).",
			i+1, totalMsgs, len(batch), len(jsonData))
	}

	d.log.Debugf("All %d Discord messages sent successfully.", totalMsgs)
	return nil
}

func (d *discordSender) CanSend() bool {
	return d.config.Service.Discord != ""
}

func (d *discordSender) sendRequest(jsonData []byte) error {
	req, err := http.NewRequest(http.MethodPost, d.config.Service.Discord, bytes.NewBuffer(jsonData))
	if err != nil {
		return errors.Wrap(err, "could not create request")
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := d.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "client request error")
	}
	defer res.Body.Close()

	d.log.Tracef("Discord response status: %d", res.StatusCode)

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

// BuildField constructs a Field based on the provided action and build options.
func (d *discordSender) BuildField(action Action, opt BuildOptions) Field {
	switch action {
	case ActionRetag:
		return d.buildRetagField(opt.Torrent, opt.NewTags, opt.NewUpLimit)
	case ActionRelabel:
		return d.buildRelabelField(opt.Torrent, opt.NewLabel)
	case ActionClean:
		return d.buildCleanField(opt.Torrent, opt.RemovalReason)
	case ActionPause:
		return d.buildPauseField(opt.Torrent)
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

	oldTags := strings.Join(torrent.Tags, ", ")
	newTagsStr := strings.Join(newTags, ", ")
	oldUpLimit := limitStr(torrent.UpLimit)
	newUpLimitStr := limitStr(newUpLimit)

	// Add fields only if they're different
	if !equal(oldTags, newTagsStr) {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Old Tags",
			Value:  oldTags,
			Inline: true,
		})
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "New Tags",
			Value:  newTagsStr,
			Inline: true,
		})
	}

	if !equal(oldUpLimit, newUpLimitStr) {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Old Upload Limit",
			Value:  oldUpLimit,
			Inline: true,
		})
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "New Upload Limit",
			Value:  newUpLimitStr,
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
		Value:  torrent.Label,
		Inline: true,
	})
	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "New Label",
		Value:  newLabel,
		Inline: true,
	})

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: string(jsonData),
	}
}

func (d *discordSender) buildPauseField(torrent config.Torrent) Field {
	var inlineFields []DiscordEmbedsField

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Ratio",
		Value:  fmt.Sprintf("%.2f", torrent.Ratio),
		Inline: true,
	})

	if torrent.Label != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Label",
			Value:  torrent.Label,
			Inline: true,
		})
	}

	if len(torrent.Tags) > 0 && strings.Join(torrent.Tags, ", ") != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Tags",
			Value:  strings.Join(torrent.Tags, ", "),
			Inline: true,
		})
	}

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Tracker",
		Value:  torrent.TrackerName,
		Inline: true,
	})

	if torrent.TrackerStatus != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Tracker Status",
			Value:  torrent.TrackerStatus,
			Inline: false,
		})
	}

	// No reason field for pause actions

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: string(jsonData),
	}
}

func (d *discordSender) buildCleanField(torrent config.Torrent, removalReason string) Field {
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
			Value:  torrent.Label,
			Inline: true,
		})
	}

	if len(torrent.Tags) > 0 && strings.Join(torrent.Tags, ", ") != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Tags",
			Value:  strings.Join(torrent.Tags, ", "),
			Inline: true,
		})
	}

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Tracker",
		Value:  torrent.TrackerName,
		Inline: true,
	})

	if torrent.TrackerStatus != "" {
		inlineFields = append(inlineFields, DiscordEmbedsField{
			Name:   "Tracker Status",
			Value:  torrent.TrackerStatus,
			Inline: false,
		})
	}

	inlineFields = append(inlineFields, DiscordEmbedsField{
		Name:   "Reason",
		Value:  removalReason,
		Inline: false,
	})

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: string(jsonData),
	}
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
		Value:  orphan,
		Inline: false,
	})

	// Serialize to JSON to store in the field value
	jsonData, _ := json.Marshal(inlineFields)

	return Field{
		Name:  "", // Empty name since path is already in the Path field
		Value: string(jsonData),
	}
}

func (d *discordSender) buildFooter(progress int, totalFields int, runTime string) string {
	if totalFields == 0 {
		return fmt.Sprintf("Started: %s ago", runTime)
	}

	return fmt.Sprintf("Progress: %d/%d | Started: %s ago", progress, totalFields, runTime)
}
