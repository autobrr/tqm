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
	maxFieldsPerEmbed   = 25
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
	Timestamp   time.Time            `json:"timestamp"`
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

func (d *discordSender) Send(title, description string, fields []DiscordEmbedsField) error {
	var (
		allEmbeds   []DiscordEmbed
		totalFields = len(fields)
		timestamp   = time.Now()

		batches      [][]DiscordEmbed
		currentBatch []DiscordEmbed
		currentChars int
	)

	// embedChars returns the number of characters in an embed.
	embedChars := func(e DiscordEmbed) int {
		count := len(e.Title) + len(e.Description)
		for _, f := range e.Fields {
			count += len(f.Name) + len(f.Value)
		}
		return count
	}

	// only send a summary embed if no fields are present, there are more fields than allowed,
	// or the config setting "detailed" is set to false
	if totalFields == 0 || totalFields > maxTotalFields || !d.config.Detailed {
		allEmbeds = append(allEmbeds, DiscordEmbed{
			Title:       title,
			Description: description,
			Color:       int(LIGHT_BLUE),
			Timestamp:   timestamp,
		})
	} else {
		for i := 0; i < totalFields; i += maxFieldsPerEmbed {
			end := i + maxFieldsPerEmbed
			if end > totalFields {
				end = totalFields
			}

			embed := DiscordEmbed{
				Color:     int(LIGHT_BLUE),
				Timestamp: timestamp,
				Fields:    fields[i:end],
			}

			if i == 0 {
				embed.Title = title
				embed.Description = description
			}

			allEmbeds = append(allEmbeds, embed)
		}
	}

	flush := func() {
		if len(currentBatch) == 0 {
			return
		}
		batches = append(batches, currentBatch)
		currentBatch = nil
		currentChars = 0
	}

	for _, e := range allEmbeds {
		eChars := embedChars(e)

		// If adding this embed breaks either the embed-count or char limit, flush first
		if len(currentBatch) >= maxEmbedsPerMessage || currentChars+eChars > maxCharactersPerMsg {
			flush()
		}

		currentBatch = append(currentBatch, e)
		currentChars += eChars
	}
	flush()

	totalMsgs := len(batches)

	for i, batch := range batches {
		// If more than one message, append the counter to the first embed’s title
		if totalMsgs > 1 && len(batch) > 0 {
			batch[0].Title = fmt.Sprintf("%s (%d/%d)", title, i+1, totalMsgs)
			batch[0].Description = description
		}

		msg := DiscordMessage{
			Content: nil,
			Embeds:  batch,
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
	if d.config.Service.Discord != "" {
		return true
	}

	return false
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

// BuildField constructs a DiscordEmbedsField based on the provided action and build options.
func BuildField(action Action, opt BuildOptions) DiscordEmbedsField {
	switch action {
	case ActionRetag:
		return buildRetagField(opt.Torrent, opt.NewTags, opt.NewUpLimit)
	case ActionRelabel:
		return buildRelabelField(opt.Torrent, opt.NewLabel)
	case ActionClean, ActionPause:
		return buildCleanField(opt.Torrent)
	case ActionOrphan:
		return buildOrphanField(opt.Orphan, opt.OrphanSize, opt.IsFile)
	}
	return DiscordEmbedsField{}
}

func buildRetagField(torrent config.Torrent, newTags []string, newUpLimit int64) DiscordEmbedsField {
	var data []DiscordEmbedsField

	equal := func(fd []DiscordEmbedsField) bool {
		return strings.EqualFold(fd[0].Value, fd[1].Value)
	}

	tagData := []DiscordEmbedsField{
		{"Old Tags:", strings.Join(torrent.Tags, ", "), false},
		{"New Tags:", strings.Join(newTags, ", "), false},
	}

	if !equal(tagData) {
		data = append(data, tagData...)
	}

	limitStr := func(limit int64) string {
		if limit == -1 {
			return "Unlimited"
		}
		return fmt.Sprintf("%d KiB/s", limit)
	}

	uploadData := []DiscordEmbedsField{
		{"Old Upload Limit:", limitStr(torrent.UpLimit), false},
		{"New Upload Limit:", limitStr(newUpLimit), false},
	}

	if !equal(uploadData) {
		data = append(data, uploadData...)
	}

	return DiscordEmbedsField{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: buildCodeBlock(data),
	}
}

func buildRelabelField(torrent config.Torrent, newLabel string) DiscordEmbedsField {
	data := []DiscordEmbedsField{
		{"Old Label:", torrent.Label, false},
		{"New Label:", newLabel, false},
	}

	return DiscordEmbedsField{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: buildCodeBlock(data),
	}
}

func buildCleanField(torrent config.Torrent) DiscordEmbedsField {
	data := []DiscordEmbedsField{
		{"Ratio:", fmt.Sprintf("%.2f", torrent.Ratio), false},
		{"Label:", torrent.Label, false},
		{"Tags:", strings.Join(torrent.Tags, ", "), false},
		{"Tracker:", torrent.TrackerName, false},
		{"Status:", torrent.TrackerStatus, false},
	}

	return DiscordEmbedsField{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: buildCodeBlock(data),
	}
}

func buildOrphanField(orphan string, orphanSize int64, isFile bool) DiscordEmbedsField {
	var (
		sizeStr string
		prefix  = "Folder"
	)

	if isFile {
		prefix = "File"
		sizeStr = fmt.Sprintf(" (%s)", humanize.IBytes(uint64(orphanSize)))
	}

	return DiscordEmbedsField{
		Name: fmt.Sprintf("%-*s%s%s", 10, prefix, orphan, sizeStr),
	}
}

func buildCodeBlock(data []DiscordEmbedsField) string {
	var maxLabelLen int
	for _, d := range data {
		if len(d.Name) > maxLabelLen {
			maxLabelLen = len(d.Name)
		}
	}

	var sb strings.Builder
	sb.WriteString("```\n")

	for _, d := range data {
		// %-*s left-justifies the label and pads it with spaces to maxLabelLen+5
		line := fmt.Sprintf("%-*s%s\n", maxLabelLen+5, d.Name, d.Value)
		sb.WriteString(line)
	}

	sb.WriteString("```")

	return sb.String()
}
