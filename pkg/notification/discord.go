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

func (d *discordSender) Send(title string, description string, runTime time.Duration, fields []Field) error {
	var (
		allEmbeds   []DiscordEmbed
		totalFields = len(fields)
		timestamp   = time.Now()

		batches      [][]DiscordEmbed
		currentBatch []DiscordEmbed
		currentChars int
	)

	// if the config setting "skip_empty_run" is set to true, and there are no fields,
	// skip sending the message entirely.
	if totalFields == 0 && d.config.SkipEmptyRun {
		return nil
	}

	// embedChars returns the number of characters in an embed.
	embedChars := func(e DiscordEmbed) int {
		count := len(e.Title) + len(e.Description)
		for _, f := range e.Fields {
			count += len(f.Name) + len(f.Value)
		}
		return count
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
		df := d.convertFields(fields)

		for i := 0; i < totalFields; i += maxFieldsPerEmbed {
			end := i + maxFieldsPerEmbed
			if end > totalFields {
				end = totalFields
			}

			embed := DiscordEmbed{
				Color:  int(LIGHT_BLUE),
				Fields: df[i:end],
				Footer: DiscordEmbedsFooter{
					Text: d.buildFooter(end, totalFields, rt),
				},
				Timestamp: timestamp,
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
	case ActionClean, ActionPause:
		return d.buildCleanField(opt.Torrent)
	case ActionOrphan:
		return d.buildOrphanField(opt.Orphan, opt.OrphanSize, opt.IsFile)
	}

	return Field{}
}

func (d *discordSender) convertFields(fields []Field) []DiscordEmbedsField {
	var df []DiscordEmbedsField

	for _, f := range fields {
		df = append(df, DiscordEmbedsField{
			Name:  f.Name,
			Value: f.Value,
		})
	}

	return df
}

func (d *discordSender) buildRetagField(torrent config.Torrent, newTags []string, newUpLimit int64) Field {
	var data []Field

	equal := func(fd []Field) bool {
		return strings.EqualFold(fd[0].Value, fd[1].Value)
	}

	tagData := []Field{
		{"Old Tags:", strings.Join(torrent.Tags, ", ")},
		{"New Tags:", strings.Join(newTags, ", ")},
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

	uploadData := []Field{
		{"Old Upload Limit:", limitStr(torrent.UpLimit)},
		{"New Upload Limit:", limitStr(newUpLimit)},
	}

	if !equal(uploadData) {
		data = append(data, uploadData...)
	}

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: d.buildCodeBlock(data),
	}
}

func (d *discordSender) buildRelabelField(torrent config.Torrent, newLabel string) Field {
	data := []Field{
		{"Old Label:", torrent.Label},
		{"New Label:", newLabel},
	}

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: d.buildCodeBlock(data),
	}
}

func (d *discordSender) buildCleanField(torrent config.Torrent) Field {
	data := []Field{
		{"Ratio:", fmt.Sprintf("%.2f", torrent.Ratio)},
		{"Label:", torrent.Label},
		{"Tags:", strings.Join(torrent.Tags, ", ")},
		{"Tracker:", torrent.TrackerName},
		{"Status:", torrent.TrackerStatus},
	}

	return Field{
		Name:  fmt.Sprintf("%s (%s)", torrent.Name, humanize.IBytes(uint64(torrent.TotalBytes))),
		Value: d.buildCodeBlock(data),
	}
}

func (d *discordSender) buildOrphanField(orphan string, orphanSize int64, isFile bool) Field {
	var (
		sizeStr string
		prefix  = "Folder"
	)

	if isFile {
		prefix = "File"
		sizeStr = fmt.Sprintf(" (%s)", humanize.IBytes(uint64(orphanSize)))
	}

	return Field{
		Name: fmt.Sprintf("%-*s%s%s", 10, prefix, orphan, sizeStr),
	}
}

func (d *discordSender) buildCodeBlock(data []Field) string {
	var maxLabelLen int
	for _, dt := range data {
		if len(dt.Name) > maxLabelLen {
			maxLabelLen = len(dt.Name)
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

func (d *discordSender) buildFooter(progress int, totalFields int, runTime string) string {
	if totalFields == 0 || totalFields > maxTotalFields || totalFields <= maxFieldsPerEmbed {
		return fmt.Sprintf("Started: %s ago", runTime)
	}

	return fmt.Sprintf("Progress: %d/%d | Started: %s ago", progress, totalFields, runTime)
}
