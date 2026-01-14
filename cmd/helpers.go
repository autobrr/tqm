package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/autobrr/go-qbittorrent"
	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"

	"github.com/autobrr/tqm/pkg/client"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/hardlinkfilemap"
	"github.com/autobrr/tqm/pkg/notification"
	"github.com/autobrr/tqm/pkg/torrentfilemap"
)

// retag torrent that meet required filters
func retagEligibleTorrents(ctx context.Context, log *logrus.Entry, c client.TagInterface, torrents map[string]config.Torrent, noti notification.Sender, client string, startTime time.Time) error {
	// vars
	var (
		ignoredTorrents       int
		retaggedTorrents      int
		errorRetaggedTorrents int

		fields []notification.Field
	)

	// iterate torrents
	for h, t := range torrents {
		// should we retag torrent and/or apply speed limit?
		retagInfo, err := c.ShouldRetag(ctx, &t)
		if err != nil {
			// error while determining whether to evaluate tag rules
			log.WithError(err).Errorf("Failed evaluating tag rules for: %+v", t)
			continue
		}

		// check if any action (tagging or speed limit) is needed
		shouldTakeAction := len(retagInfo.Add) > 0 || len(retagInfo.Remove) > 0 || retagInfo.UploadKb != nil

		if !shouldTakeAction {
			// torrent did not meet any tag rule conditions
			log.Tracef("No tag actions for %s: %s", h, t.Name)
			ignoredTorrents++
			continue
		}

		// Build final tags map
		finalTags := make(map[string]struct{})

		// Copy existing tags except those being removed
		for tag := range t.Tags {
			if _, shouldRemove := retagInfo.Remove[tag]; !shouldRemove {
				finalTags[tag] = struct{}{}
			}
		}

		// Add new tags
		for tag := range retagInfo.Add {
			finalTags[tag] = struct{}{}
		}

		limitKb := t.UpLimit

		// retag
		if !t.APIDividerPrinted {
			log.Info("-----")
		}

		// Convert final tags map to slice for display and API calls
		finalTagsSlice := make([]string, 0, len(finalTags))
		for tag := range finalTags {
			finalTagsSlice = append(finalTagsSlice, tag)
		}
		sort.Strings(finalTagsSlice)

		var actionLogs []string
		if len(retagInfo.Add) > 0 || len(retagInfo.Remove) > 0 {
			actionLogs = append(actionLogs, fmt.Sprintf("Retagging to: [%s]", strings.Join(finalTagsSlice, ", ")))
		}
		if retagInfo.UploadKb != nil {
			limitKb = *retagInfo.UploadKb
			if limitKb == -1 {
				actionLogs = append(actionLogs, "Setting upload limit: Unlimited")
			} else {
				actionLogs = append(actionLogs, fmt.Sprintf("Setting upload limit: %d KiB/s", limitKb))
			}
		}

		log.Infof("Actions for: %q - %s", t.Name, strings.Join(actionLogs, " | "))
		log.Infof("Ratio: %.3f / Seed days: %.3f / Seeds: %d / Label: %s / Tags: %s / Tracker: %s / "+
			"Tracker Status: %q", t.Ratio, t.SeedingDays, t.Seeds, t.Label, strings.Join(t.TagsSlice(), ", "), t.TrackerName, t.TrackerStatus)

		actionTaken := false
		actionFailed := false

		if !flagDryRun {
			// apply tag changes
			if err := c.SetTags(ctx, t.Hash, finalTagsSlice); err == nil {
				log.Debugf("Set tags: %v", finalTagsSlice)
				actionTaken = true
			} else if errors.Is(err, qbittorrent.ErrUnsupportedVersion) {
				log.Debug("Unsupported qBittorrent version, using AddTags and RemoveTags instead")

				// Convert Add map to slice
				if len(retagInfo.Add) > 0 {
					addTags := make([]string, 0, len(retagInfo.Add))
					for tag := range retagInfo.Add {
						addTags = append(addTags, tag)
					}
					sort.Strings(addTags)
					if err := c.AddTags(ctx, t.Hash, addTags); err != nil {
						log.WithError(err).Errorf("Failed adding tags %v to torrent: %+v", addTags, t)
						actionFailed = true
					} else {
						log.Debugf("Added tags: %v", addTags)
						actionTaken = true
					}
				}

				// Convert Remove map to slice
				if len(retagInfo.Remove) > 0 && !actionFailed {
					removeTags := make([]string, 0, len(retagInfo.Remove))
					for tag := range retagInfo.Remove {
						removeTags = append(removeTags, tag)
					}
					sort.Strings(removeTags)
					if err := c.RemoveTags(ctx, t.Hash, removeTags); err != nil {
						log.WithError(err).Errorf("Failed removing tags %v from torrent: %+v", removeTags, t)
						actionFailed = true
					} else {
						log.Debugf("Removed tags: %v", removeTags)
						actionTaken = true
					}
				}
			} else {
				log.WithError(err).Errorf("Failed setting tags %v for torrent: %+v", finalTagsSlice, t)
				actionFailed = true
			}

			// apply speed limit change
			if retagInfo.UploadKb != nil && !actionFailed {
				limitBytes := *retagInfo.UploadKb * 1024
				if *retagInfo.UploadKb == -1 {
					limitBytes = -1
				}
				if err := c.SetUploadLimit(ctx, t.Hash, limitBytes); err != nil {
					log.WithError(err).Errorf("Failed setting upload limit to %d KiB/s for torrent: %+v", *retagInfo.UploadKb, t)
					actionFailed = true
				} else {
					log.Debugf("Set upload limit to %d KiB/s", *retagInfo.UploadKb)
					actionTaken = true
				}
			}

			if actionFailed {
				errorRetaggedTorrents++
			} else if actionTaken {
				log.Info("Actions applied successfully.")
			}

		} else {
			log.Warn("Dry-run enabled, skipping actions...")
		}

		// don't check for shouldTakeAction again as it can't be false
		if actionTaken || flagDryRun {
			fields = append(fields, noti.BuildField(notification.ActionRetag, notification.BuildOptions{
				Torrent:    t,
				NewTags:    finalTagsSlice,
				NewUpLimit: limitKb,
			}))
			retaggedTorrents++
		}
	}

	// show result
	log.Info("-----")
	log.Infof("Ignored torrents: %d", ignoredTorrents)
	log.Infof("Retagged torrents: %d, %d failures", retaggedTorrents, errorRetaggedTorrents)

	if !noti.CanSend() {
		log.Debug("Notifications disabled, skipping...")
		return nil
	}

	sendErr := noti.Send(
		"Torrent Retag",
		fmt.Sprintf("Retagged **%d** torrent(s)", retaggedTorrents),
		client,
		time.Since(startTime),
		fields,
		flagDryRun,
	)
	if sendErr != nil {
		log.WithError(sendErr).Error("Failed sending notification")
	}

	return nil
}

// relabel torrent that meet required filters
func relabelEligibleTorrents(ctx context.Context, log *logrus.Entry, c client.Interface, torrents map[string]config.Torrent, tfm *torrentfilemap.TorrentFileMap, noti notification.Sender, client string, startTime time.Time) error {
	// vars
	var (
		ignoredTorrents      int
		nonUniqueTorrents    int
		relabeledTorrents    int
		errorRelabelTorrents int

		fields          []notification.Field
		processedHashes = make(map[string]bool)
	)

	// iterate torrents
	for h, t := range torrents {
		if processedHashes[h] {
			continue
		}
		// should we relabel torrent?
		label, relabel, err := c.ShouldRelabel(ctx, &t)
		if err != nil {
			// error while determining whether to relabel torrent
			log.WithError(err).Errorf("Failed determining whether to relabel: %+v", t)
			continue
		} else if !relabel {
			// torrent did not meet the relabel filters
			log.Tracef("Not relabeling %s: %s", h, t.Name)
			ignoredTorrents++
			continue
		} else if label == t.Label {
			// torrent already has the correct label
			log.Tracef("Torrent already has correct label: %s", t.Name)
			ignoredTorrents++
			continue
		}

		if !tfm.IsUnique(t) {
			if !flagExperimentalRelabelForCrossSeeds {
				nonUniqueTorrents++
				log.Warnf("Skipping non unique torrent | Name: %s / Label: %s / Tags: %s / Tracker: %s", t.Name, t.Label, strings.Join(t.TagsSlice(), ", "), t.TrackerName)
				processedHashes[h] = true
				continue
			}

			group, err := tfm.GetTorrentsSharingFiles(t, torrents)
			if err != nil {
				log.WithError(err).Errorf("Could not get torrent group for %s", t.Name)
				processedHashes[h] = true
				continue
			}

			canRelabelGroup := true
			for _, groupTorrent := range group {
				groupLabel, groupRelabel, err := c.ShouldRelabel(ctx, &groupTorrent)
				if err != nil || !groupRelabel || groupLabel != label {
					canRelabelGroup = false
					log.Warnf("Part of cross-seed group for %q does not meet relabel criteria: %q (target label: %s, group target: %s)", t.Name, groupTorrent.Name, label, groupLabel)
					break
				}
			}

			if canRelabelGroup {
				log.Infof("All %d torrents in cross-seed group for %q meet criteria for relabeling to %q. Proceeding.", len(group), t.Name, label)
				for _, groupTorrent := range group {
					relabelTorrent(ctx, log, c, groupTorrent, label, true, noti, &relabeledTorrents, &errorRelabelTorrents, &fields)
					processedHashes[groupTorrent.Hash] = true
				}
			} else {
				log.Warnf("Skipping relabel for cross-seed group of %q as not all torrents meet criteria.", t.Name)
				for _, groupTorrent := range group {
					processedHashes[groupTorrent.Hash] = true
					nonUniqueTorrents++
				}
			}

		} else {
			relabelTorrent(ctx, log, c, t, label, false, noti, &relabeledTorrents, &errorRelabelTorrents, &fields)
			processedHashes[h] = true
		}
	}

	// show result
	log.Info("-----")
	log.Infof("Ignored torrents: %d", ignoredTorrents)
	if nonUniqueTorrents > 0 {
		log.Infof("Non-unique torrents skipped: %d", nonUniqueTorrents)
	}
	log.Infof("Relabeled torrents: %d, %d failures", relabeledTorrents, errorRelabelTorrents)

	if !noti.CanSend() {
		log.Debug("Notifications disabled, skipping...")
		return nil
	}

	sendErr := noti.Send(
		"Torrent Relabel",
		fmt.Sprintf("Relabeled **%d** torrent(s)", relabeledTorrents),
		client,
		time.Since(startTime),
		fields,
		flagDryRun,
	)
	if sendErr != nil {
		log.WithError(sendErr).Error("Failed sending notification")
	}

	return nil
}

func relabelTorrent(ctx context.Context, log *logrus.Entry, c client.Interface, t config.Torrent, label string, hardlink bool, noti notification.Sender, relabeledTorrents *int, errorRelabelTorrents *int, fields *[]notification.Field) {
	if !t.APIDividerPrinted {
		log.Info("-----")
	}

	if !flagDryRun {
		if err := c.SetTorrentLabel(ctx, t.Hash, label, hardlink); err != nil {
			log.WithError(err).Errorf("Failed relabeling torrent: %+v", t)
			*errorRelabelTorrents++
			return
		}

		log.Info("Relabeled")
		time.Sleep(1 * time.Second)
	} else {
		log.Warn("Dry-run enabled, skipping relabel...")
	}

	*fields = append(*fields, noti.BuildField(notification.ActionRelabel, notification.BuildOptions{
		Torrent:  t,
		NewLabel: label,
	}))
	*relabeledTorrents++
}

// remove torrents that meet remove filters
func removeEligibleTorrents(ctx context.Context, log *logrus.Entry, c client.Interface, torrents map[string]config.Torrent, tfm *torrentfilemap.TorrentFileMap, hfm hardlinkfilemap.HardlinkFileMapI, filter *config.FilterConfiguration, noti notification.Sender, client string, startTime time.Time) error {
	// vars
	var (
		ignoredTorrents     int
		hardRemoveTorrents  int
		errorRemoveTorrents int
		removedTorrentBytes int64
	)

	deleteData := true
	if filter != nil && filter.DeleteData != nil {
		deleteData = *filter.DeleteData
	}

	var fields []notification.Field

	// helper function to remove torrent
	removeTorrent := func(ctx context.Context, h string, t *config.Torrent, reason string, isHardlinked bool, isUnique bool, isNotUniqueUnregistered bool) bool {
		// Log removal details
		if !t.APIDividerPrinted {
			log.Info("-----")
		}

		var logMsg string
		if isNotUniqueUnregistered && isHardlinked {
			logMsg = "removing unregistered non-unique torrent (hardlinked): %q - %s"
		} else if isNotUniqueUnregistered && !isHardlinked {
			logMsg = "removing unregistered non-unique torrent (file overlap): %q - %s"
		} else {
			if !t.FreeSpaceSet {
				logMsg = "removing: %q - %s"
			} else {
				logMsg = "removing: %q - %s - %.2f GB"
			}
		}

		if !t.FreeSpaceSet {
			log.Infof(logMsg, t.Name, humanize.IBytes(uint64(t.DownloadedBytes)))
		} else {
			log.Infof(logMsg, t.Name, humanize.IBytes(uint64(t.DownloadedBytes)), t.FreeSpaceGB())
		}

		log.Debugf("Removal reason: %s", reason)
		log.Debugf("isUnique: %t / isHardlinked: %t", isUnique, isHardlinked)
		log.Infof("Ratio: %.3f / Seed days: %.3f / Seeds: %d / Label: %s / Tags: %s / Tracker: %s / "+
			"Tracker Status: %q", t.Ratio, t.SeedingDays, t.Seeds, t.Label, strings.Join(t.TagsSlice(), ", "), t.TrackerName, t.TrackerStatus)

		// update the hardlink map before removing the torrent
		hfm.RemoveByTorrent(*t)

		// Determine whether to delete data
		localDeleteData := deleteData

		// For non-unique torrents with file overlap (not hardlinked), always keep the data
		if !isUnique && !isHardlinked {
			localDeleteData = false
		}

		if !flagDryRun {
			// Do remove
			removed, err := c.RemoveTorrent(ctx, t, localDeleteData)
			if err != nil {
				log.WithError(err).Errorf("Failed removing torrent: %+v", t)
				// don't remove from torrents file map, but prevent further operations on this torrent
				delete(torrents, h)
				errorRemoveTorrents++
				return false
			} else if !removed {
				log.Error("Failed removing torrent...")
				// don't remove from torrents file map, but prevent further operations on this torrent
				delete(torrents, h)
				errorRemoveTorrents++
				return false
			} else {
				if localDeleteData {
					log.Info("Removed with data")
				} else {
					log.Info("Removed (kept data on disk)")
				}

				// increase free space if we removed data
				if localDeleteData && t.FreeSpaceSet {
					log.Tracef("Increasing free space by: %s", humanize.IBytes(uint64(t.DownloadedBytes)))
					c.AddFreeSpace(t.DownloadedBytes)
					log.Tracef("New free space: %.2f GB", c.GetFreeSpace())
				}

				time.Sleep(1 * time.Second)
			}
		} else {
			log.Warnf("Dry-run enabled, skipping remove (would delete data: %t)...", localDeleteData)
		}

		fields = append(fields, noti.BuildField(notification.ActionClean, notification.BuildOptions{
			Torrent:       *t,
			RemovalReason: reason,
		}))

		// increased hard removed counters
		removedTorrentBytes += t.DownloadedBytes
		hardRemoveTorrents++

		// remove the torrent from the torrent maps
		tfm.Remove(*t)
		delete(torrents, h)
		return true
	}

	// iterate torrents
	hardlinkedCandidates := make(map[string]config.Torrent)
	fileOverlapCandidates := make(map[string]config.Torrent)
	candidateReasons := make(map[string]string)
	for h, t := range torrents {
		// should we ignore this torrent?
		ignore, reason, err := c.ShouldIgnore(ctx, &t)
		if err != nil {
			// error while determining whether to ignore torrent
			log.WithError(err).Errorf("Failed determining whether to ignore: %+v", t)
			delete(torrents, h)
			continue
		} else if ignore && !(config.Config.BypassIgnoreIfUnregistered && t.IsUnregistered(ctx)) {
			// torrent met ignore filter
			if reason != "" {
				log.Tracef("Ignoring torrent %s: %s (reason: %s)", h, t.Name, reason)
			} else {
				log.Tracef("Ignoring torrent %s: %s", h, t.Name)
			}
			delete(torrents, h)
			ignoredTorrents++
			continue
		}

		// should we remove this torrent?
		remove, reason, err := c.ShouldRemoveWithReason(ctx, &t)
		if err != nil {
			log.WithError(err).Errorf("Failed determining whether to remove: %+v", t)
			// dont do any further operations on this torrent, but keep in the torrent file map
			delete(torrents, h)
			continue
		} else if !remove {
			// torrent did not meet the remove filters
			log.Tracef("Not removing %s: %s", h, t.Name)
			continue
		}

		// torrent meets the remove filters

		// Check if the torrent is not unique (either through file mapping or hardlinks)
		isUnique := true
		isHardlinked := false

		if !tfm.IsUnique(t) {
			// the files are not unique and eligible for a hard deletion (remove data)
			isUnique = false
			isHardlinked = false
		}

		if !hfm.IsTorrentUnique(t) {
			// the files are hardlinked to other torrents
			isUnique = false
			isHardlinked = true
		}

		if !isUnique {
			// Check if torrent is unregistered (can bypass uniqueness checks)
			if t.IsUnregistered(ctx) {
				// For unregistered torrents, override safety checks and remove immediately
				removeTorrent(ctx, h, &t, reason, isHardlinked, isUnique, true)
				continue
			}

			// For regular non-unique torrents, add to appropriate candidates list
			if isHardlinked {
				log.Info("-----")
				log.Warnf("Skipping non-unique torrent (hardlinked) | Name: %s / Label: %s / Tags: %s / Tracker: %s",
					t.Name, t.Label, strings.Join(t.TagsSlice(), ", "), t.TrackerName)
				hardlinkedCandidates[h] = t
				candidateReasons[h] = reason
			} else {
				log.Info("-----")
				log.Warnf("Skipping non-unique torrent (file overlap) | Name: %s / Label: %s / Tags: %s / Tracker: %s",
					t.Name, t.Label, strings.Join(t.TagsSlice(), ", "), t.TrackerName)
				fileOverlapCandidates[h] = t
				candidateReasons[h] = reason
			}
			continue
		}

		// Remove unique torrents
		removeTorrent(ctx, h, &t, reason, false, isUnique, false)
	}

	log.Info("========================================")
	log.Infof("Finished initial check, %d hardlinked candidates and %d file overlap candidates for removal", len(hardlinkedCandidates), len(fileOverlapCandidates))
	log.Info("========================================")

	// imagine we removed all candidates,
	// now lets check if the candidates still have more versions
	// or can be safely removed
	for _, t := range fileOverlapCandidates {
		tfm.Remove(t)
		hfm.RemoveByTorrent(t)
	}

	for _, t := range hardlinkedCandidates {
		tfm.Remove(t)
		hfm.RemoveByTorrent(t)
	}

	// Process file overlap candidates - these can be removed without data deletion
	removedCandidates := 0
	removedFileOverlapCandidates := 0
	removedHardlinkedCandidates := 0
	for h, t := range fileOverlapCandidates {
		noInstances := tfm.NoInstances(t) && hfm.NoInstances(t)

		if !noInstances {
			log.Tracef("%s still not unique", t.Name)
			continue
		}

		reason := candidateReasons[h]
		if removeTorrent(ctx, h, &t, reason, false, false, false) {
			removedCandidates++
			removedFileOverlapCandidates++
		}
	}

	// Process hardlinked candidates - these can be removed with data deletion
	for h, t := range hardlinkedCandidates {
		noInstances := tfm.NoInstances(t) && hfm.NoInstances(t)

		if !noInstances {
			log.Tracef("%s still not unique", t.Name)
			continue
		}

		reason := candidateReasons[h]
		if removeTorrent(ctx, h, &t, reason, true, false, false) {
			removedCandidates++
			removedHardlinkedCandidates++
		}
	}

	reclaimedSpace := humanize.IBytes(uint64(removedTorrentBytes))

	// show result
	log.Info("-----")
	log.Infof("Ignored torrents: %d", ignoredTorrents)

	// Calculate skipped counts
	uniqueRemoved := hardRemoveTorrents - removedCandidates
	skippedHardlinked := len(hardlinkedCandidates) - removedHardlinkedCandidates
	skippedFileOverlap := len(fileOverlapCandidates) - removedFileOverlapCandidates
	totalSkipped := skippedHardlinked + skippedFileOverlap

	// Show skipped counts if any were skipped
	if totalSkipped > 0 {
		log.Infof("Skipped: %d hardlinked, %d file overlap", skippedHardlinked, skippedFileOverlap)
	}

	// Show removed torrents summary
	log.WithField("reclaimed_space", reclaimedSpace).
		Infof("Removed torrents: %d total (%d unique, %d hardlinked, %d file overlap)", hardRemoveTorrents, uniqueRemoved, removedHardlinkedCandidates, removedFileOverlapCandidates)

	// Show failures if any
	if errorRemoveTorrents > 0 {
		log.Infof("Failures: %d torrents failed to remove", errorRemoveTorrents)
	}

	if !noti.CanSend() {
		log.Debug("Notifications disabled, skipping...")
		return nil
	}

	sendErr := noti.Send(
		"Torrent Cleanup",
		fmt.Sprintf("Removed **%d** torrent(s) | Total reclaimed **%s**", hardRemoveTorrents, reclaimedSpace),
		client,
		time.Since(startTime),
		fields,
		flagDryRun,
	)
	if sendErr != nil {
		log.WithError(sendErr).Error("Failed sending notification")
	}
	return nil
}
