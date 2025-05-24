package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"

	"github.com/autobrr/tqm/pkg/client"
	"github.com/autobrr/tqm/pkg/config"
	"github.com/autobrr/tqm/pkg/hardlinkfilemap"
	"github.com/autobrr/tqm/pkg/notification"
	"github.com/autobrr/tqm/pkg/torrentfilemap"
)

func removeSlice(slice []string, remove []string) []string {
	for _, item := range remove {
		for i, v := range slice {
			if v == item {
				slice = append(slice[:i], slice[i+1:]...)
			}
		}
	}
	return slice
}

// retag torrent that meet required filters
func retagEligibleTorrents(ctx context.Context, log *logrus.Entry, c client.TagInterface, torrents map[string]config.Torrent, noti notification.Sender) error {
	// vars
	var (
		ignoredTorrents       int
		retaggedTorrents      int
		errorRetaggedTorrents int

		fields []notification.DiscordEmbedsField
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

		// initialize with torrent values
		finalTags := t.Tags
		limitKb := t.UpLimit

		// retag
		log.Info("-----")
		actionLogs := []string{}
		if len(retagInfo.Add) > 0 || len(retagInfo.Remove) > 0 {
			currentTags := removeSlice(t.Tags, retagInfo.Remove)
			finalTags = append(currentTags, retagInfo.Add...)
			actionLogs = append(actionLogs, fmt.Sprintf("Retagging to: [%s]", strings.Join(finalTags, ", ")))
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
			"Tracker Status: %q", t.Ratio, t.SeedingDays, t.Seeds, t.Label, strings.Join(t.Tags, ", "), t.TrackerName, t.TrackerStatus)

		actionTaken := false
		actionFailed := false

		if !flagDryRun {
			// apply tag changes
			if len(retagInfo.Add) > 0 {
				if err := c.AddTags(ctx, t.Hash, retagInfo.Add); err != nil {
					log.WithError(err).Errorf("Failed adding tags %v to torrent: %+v", retagInfo.Add, t)
					actionFailed = true
				} else {
					log.Debugf("Added tags: %v", retagInfo.Add)
					actionTaken = true
				}
			}
			if len(retagInfo.Remove) > 0 && !actionFailed {
				if err := c.RemoveTags(ctx, t.Hash, retagInfo.Remove); err != nil {
					log.WithError(err).Errorf("Failed removing tags %v from torrent: %+v", retagInfo.Remove, t)
					actionFailed = true
				} else {
					log.Debugf("Removed tags: %v", retagInfo.Remove)
					actionTaken = true
				}
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

		if actionTaken || flagDryRun && shouldTakeAction {
			fields = append(fields, notification.BuildField(notification.ActionRetag, notification.BuildOptions{
				Torrent:    t,
				NewTags:    finalTags,
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
		fields,
	)
	if sendErr != nil {
		log.WithError(sendErr).Error("Failed sending notification")
	}

	return nil
}

// relabel torrent that meet required filters
func relabelEligibleTorrents(ctx context.Context, log *logrus.Entry, c client.Interface, torrents map[string]config.Torrent, tfm *torrentfilemap.TorrentFileMap, noti notification.Sender) error {
	// vars
	var (
		ignoredTorrents      int
		nonUniqueTorrents    int
		relabeledTorrents    int
		errorRelabelTorrents int

		fields []notification.DiscordEmbedsField
	)

	// iterate torrents
	for h, t := range torrents {
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

		hardlink := false
		if !tfm.IsUnique(t) {
			if !flagExperimentalRelabelForCrossSeeds {
				// torrent file is not unique, files are contained within another torrent
				// so we cannot safely change the label in-case of auto move
				nonUniqueTorrents++
				log.Warnf("Skipping non unique torrent | Name: %s / Label: %s / Tags: %s / Tracker: %s", t.Name, t.Label, strings.Join(t.Tags, ", "), t.TrackerName)
				continue
			}

			hardlink = true
		}

		// relabel
		log.Info("-----")
		if hardlink {
			log.Infof("Relabeling: %q - %s | with hardlinks to: %q", t.Name, label, c.LabelPathMap()[label])
		} else {
			log.Infof("Relabeling: %q - %s", t.Name, label)
		}
		log.Infof("Ratio: %.3f / Seed days: %.3f / Seeds: %d / Label: %s / Tags: %s / Tracker: %s / "+
			"Tracker Status: %q", t.Ratio, t.SeedingDays, t.Seeds, t.Label, strings.Join(t.Tags, ", "), t.TrackerName, t.TrackerStatus)

		if !flagDryRun {
			if err := c.SetTorrentLabel(ctx, t.Hash, label, hardlink); err != nil {
				log.WithError(err).Fatalf("Failed relabeling torrent: %+v", t)
				errorRelabelTorrents++
				continue
			}

			log.Info("Relabeled")
			time.Sleep(5 * time.Second)
		} else {
			log.Warn("Dry-run enabled, skipping relabel...")
		}

		fields = append(fields, notification.BuildField(notification.ActionRelabel, notification.BuildOptions{
			NewLabel: label,
		}))
		relabeledTorrents++
	}

	// show result
	log.Info("-----")
	log.Infof("Ignored torrents: %d", ignoredTorrents)
	if nonUniqueTorrents > 0 {
		log.Infof("Non-unique torrents: %d", nonUniqueTorrents)
	}
	log.Infof("Relabeled torrents: %d, %d failures", relabeledTorrents, errorRelabelTorrents)

	if !noti.CanSend() {
		log.Debug("Notifications disabled, skipping...")
		return nil
	}

	sendErr := noti.Send(
		"Torrent Relabel",
		fmt.Sprintf("Relabeled **%d** torrent(s)", relabeledTorrents),
		fields,
	)
	if sendErr != nil {
		log.WithError(sendErr).Error("Failed sending notification")
	}

	return nil
}

// remove torrents that meet remove filters
func removeEligibleTorrents(ctx context.Context, log *logrus.Entry, c client.Interface, torrents map[string]config.Torrent, tfm *torrentfilemap.TorrentFileMap, hfm hardlinkfilemap.HardlinkFileMapI, filter *config.FilterConfiguration, noti notification.Sender) error {
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

	// helper function to handle removal of torrents that aren't unique
	handleNonUniqueTorrent := func(ctx context.Context, h string, t *config.Torrent, isHardlinked bool) bool {
		// Check if torrent is unregistered (can bypass uniqueness checks)
		if t.IsUnregistered(ctx) {
			// For unregistered torrents, override safety checks
			log.Info("-----")
			if isHardlinked {
				log.Infof("removing unregistered non-unique torrent (hardlinked): %q - %s", t.Name, humanize.IBytes(uint64(t.DownloadedBytes)))
			} else {
				log.Infof("removing unregistered non-unique torrent (file overlap): %q - %s", t.Name, humanize.IBytes(uint64(t.DownloadedBytes)))
			}
			log.Infof("Ratio: %.3f / Seed days: %.3f / Seeds: %d / Label: %s / Tags: %s / Tracker: %s / "+
				"Tracker Status: %q", t.Ratio, t.SeedingDays, t.Seeds, t.Label, strings.Join(t.Tags, ", "), t.TrackerName, t.TrackerStatus)

			// update the hardlink map before removing the torrent
			hfm.RemoveByTorrent(*t)

			if !flagDryRun {
				// Use the global deleteData for hardlinked torrents
				// For file overlap (not hardlinked), always keep the data
				localDeleteData := deleteData

				// Only override deleteData for file overlap (not hardlinked) torrents
				if !isHardlinked {
					localDeleteData = false
				}

				removed, err := c.RemoveTorrent(ctx, t.Hash, localDeleteData)
				if err != nil {
					log.WithError(err).Errorf("Failed removing torrent: %+v", t)
					// dont remove from torrents file map, but prevent further operations on this torrent
					delete(torrents, h)
					errorRemoveTorrents++
					return true
				} else if !removed {
					log.Error("Failed removing torrent...")
					// dont remove from torrents file map, but prevent further operations on this torrent
					delete(torrents, h)
					errorRemoveTorrents++
					return true
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
				log.Warn("Dry-run enabled, skipping remove...")
			}

			// increased hard removed counters
			removedTorrentBytes += t.DownloadedBytes
			hardRemoveTorrents++

			// remove the torrent from the torrent maps
			tfm.Remove(*t)
			delete(torrents, h)
			return true
		}

		// For regular torrents, add to candidates for standard processing
		if isHardlinked {
			log.Warnf("Skipping non-unique torrent (hardlinked) | Name: %s / Label: %s / Tags: %s / Tracker: %s",
				t.Name, t.Label, strings.Join(t.Tags, ", "), t.TrackerName)
		} else {
			log.Warnf("Skipping non-unique torrent (file overlap) | Name: %s / Label: %s / Tags: %s / Tracker: %s",
				t.Name, t.Label, strings.Join(t.Tags, ", "), t.TrackerName)
		}

		// Important! We need to return the torrent so it can be added to candidates in the caller
		return false
	}

	var fields []notification.DiscordEmbedsField

	// helper function to remove torrent
	removeTorrent := func(ctx context.Context, h string, t *config.Torrent) {
		// remove the torrent
		log.Info("-----")
		if !t.FreeSpaceSet {
			log.Infof("removing: %q - %s", t.Name, humanize.IBytes(uint64(t.DownloadedBytes)))
		} else {
			// show current free-space as well
			log.Infof("removing: %q - %s - %.2f GB", t.Name,
				humanize.IBytes(uint64(t.DownloadedBytes)), t.FreeSpaceGB())
		}

		log.Infof("Ratio: %.3f / Seed days: %.3f / Seeds: %d / Label: %s / Tags: %s / Tracker: %s / "+
			"Tracker Status: %q", t.Ratio, t.SeedingDays, t.Seeds, t.Label, strings.Join(t.Tags, ", "), t.TrackerName, t.TrackerStatus)

		// update the hardlink map before removing the torrent files
		hfm.RemoveByTorrent(*t)

		if !flagDryRun {
			// do remove
			removed, err := c.RemoveTorrent(ctx, t.Hash, deleteData)
			if err != nil {
				log.WithError(err).Fatalf("Failed removing torrent: %+v", t)
				// dont remove from torrents file map, but prevent further operations on this torrent
				delete(torrents, h)
				errorRemoveTorrents++
				return
			} else if !removed {
				log.Error("Failed removing torrent...")
				// dont remove from torrents file map, but prevent further operations on this torrent
				delete(torrents, h)
				errorRemoveTorrents++
				return
			} else {
				if deleteData {
					log.Info("Removed with data")
				} else {
					log.Info("Removed (kept data on disk)")
				}

				// increase free space
				if t.FreeSpaceSet {
					log.Tracef("Increasing free space by: %s", humanize.IBytes(uint64(t.DownloadedBytes)))
					c.AddFreeSpace(t.DownloadedBytes)
					log.Tracef("New free space: %.2f GB", c.GetFreeSpace())
				}

				time.Sleep(1 * time.Second)
			}
		} else {
			log.Warn("Dry-run enabled, skipping remove...")
		}

		fields = append(fields, notification.BuildField(notification.ActionClean, notification.BuildOptions{
			Torrent: *t,
		}))

		// increased hard removed counters
		removedTorrentBytes += t.DownloadedBytes
		hardRemoveTorrents++

		// remove the torrent from the torrent maps
		tfm.Remove(*t)
		delete(torrents, h)
	}

	// iterate torrents
	candidates := make(map[string]config.Torrent)
	for h, t := range torrents {
		// should we ignore this torrent?
		ignore, err := c.ShouldIgnore(ctx, &t)
		if err != nil {
			// error while determining whether to ignore torrent
			log.WithError(err).Errorf("Failed determining whether to ignore: %+v", t)
			delete(torrents, h)
			continue
		} else if ignore && !(config.Config.BypassIgnoreIfUnregistered && t.IsUnregistered(ctx)) {
			// torrent met ignore filter
			log.Tracef("Ignoring torrent %s: %s", h, t.Name)
			delete(torrents, h)
			ignoredTorrents++
			continue
		}

		// should we remove this torrent?
		remove, err := c.ShouldRemove(ctx, &t)
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
		isNotUnique := false
		isHardlinked := false

		if !tfm.IsUnique(t) {
			// the are files unique and eligible for a hard deletion (remove data)
			isNotUnique = true
			isHardlinked = false
		} else if !hfm.IsTorrentUnique(t) {
			// the files are hardlinked to other torrents
			isNotUnique = true
			isHardlinked = true
		}

		if isNotUnique {
			if handled := handleNonUniqueTorrent(ctx, h, &t, isHardlinked); handled {
				// Torrent was handled (removed) in the function
				continue
			} else {
				// Torrent was not removed, add to candidates
				candidates[h] = t
				continue
			}
		}

		removeTorrent(ctx, h, &t)
	}

	log.Info("========================================")
	log.Infof("Finished initial check, %d cross-seeded torrents are candidates for removal", len(candidates))
	log.Info("========================================")

	// imagine we removed all candidates,
	// now lets check if the candidates still have more versions
	// or can be safely removed
	for _, t := range candidates {
		tfm.Remove(t)
		hfm.RemoveByTorrent(t)
	}

	// check again for unique torrents
	removedCandidates := 0
	for h, t := range candidates {
		noInstances := tfm.NoInstances(t) && hfm.NoInstances(t)

		if !noInstances {
			log.Tracef("%s still not unique unique", t.Name)
			continue
		}

		removeTorrent(ctx, h, &t)
		removedCandidates++
	}

	reclaimedSpace := humanize.IBytes(uint64(removedTorrentBytes))

	// show result
	log.Info("-----")
	log.Infof("Ignored torrents: %d", ignoredTorrents)
	log.WithField("reclaimed_space", reclaimedSpace).
		Infof("Removed torrents: %d initially removed, %d cross-seeded torrents were candidates for removal, only %d of them removed and %d failures",
			hardRemoveTorrents-removedCandidates, len(candidates), removedCandidates, errorRemoveTorrents)

	if !noti.CanSend() {
		log.Debug("Notifications disabled, skipping...")
		return nil
	}

	sendErr := noti.Send(
		"Torrent Cleanup",
		fmt.Sprintf("Removed **%d** torrent(s) | Total reclaimed **%s**", hardRemoveTorrents, reclaimedSpace),
		fields,
	)
	if sendErr != nil {
		log.WithError(sendErr).Error("Failed sending notification")
	}
	return nil
}
