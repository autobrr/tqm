[![made-with-golang](https://img.shields.io/badge/Made%20with-Golang-blue.svg?style=flat-square)](https://golang.org/)
[![License: GPL v3](https://img.shields.io/badge/License-GPL%203-blue.svg?style=flat-square)](https://github.com/autobrr/tqm/blob/master/LICENSE.md)
[![Discord](https://img.shields.io/discord/381077432285003776.svg?colorB=177DC1&label=Discord&style=flat-square)](https://discord.io/cloudbox)
[![Contributing](https://img.shields.io/badge/Contributing-gray.svg?style=flat-square)](CONTRIBUTING.md)
[![Donate](https://img.shields.io/badge/Donate-gray.svg?style=flat-square)](#donate)

# tqm

CLI tool to manage your torrent client queues. Primary focus is on removing torrents that meet specific criteria.

This is a fork from [l3uddz](https://github.com/l3uddz/tqm).

## Example Configuration

```yaml
clients:
  deluge:
    enabled: true
    filter: default
    download_path: /mnt/local/downloads/torrents/deluge
    free_space_path: /mnt/local/downloads/torrents/deluge
    download_path_mapping:
      /downloads/torrents/deluge: /mnt/local/downloads/torrents/deluge
    host: localhost
    login: localclient
    password: password-from-/opt/deluge/auth
    port: 58846
    type: deluge
    v2: true
  qbt:
    download_path: /mnt/local/downloads/torrents/qbittorrent/completed
    download_path_mapping:
      /downloads/torrents/qbittorrent/completed: /mnt/local/downloads/torrents/qbittorrent/completed
    enabled: true
    filter: default
    type: qbittorrent
    url: https://qbittorrent.domain.com/
    user: user
    password: password
    # NEW: If this option is set to true, AutoTmm aka Auto Torrent Managment Mode,
    # will be enabled for torrents after a relabel.
    # This ensures the torrent is also moved in the filesystem to the new category path, and not only changes category in qbit
    # enableAutoTmmAfterRelabel: true
filters:
  default:
    ignore:
      # general
      - IsTrackerDown()
      - Downloaded == false && !IsUnregistered()
      - SeedingHours < 26 && !IsUnregistered()
      # permaseed / un-sorted (unless torrent has been deleted)
      - Label startsWith "permaseed-" && !IsUnregistered()
      # Filter based on qbittorrent tags (only qbit at the moment)
      - '"permaseed" in Tags && !IsUnregistered()'
    remove:
      # general
      - IsUnregistered()
      # imported
      - Label in ["sonarr-imported", "radarr-imported", "lidarr-imported"] && (Ratio > 4.0 || SeedingDays >= 15.0)
      # ipt
      - Label in ["autoremove-ipt"] && (Ratio > 3.0 || SeedingDays >= 15.0)
      # hdt
      - Label in ["autoremove-hdt"] && (Ratio > 3.0 || SeedingDays >= 15.0)
      # bhd
      - Label in ["autoremove-bhd"] && (Ratio > 3.0 || SeedingDays >= 15.0)
      # ptp
      - Label in ["autoremove-ptp"] && (Ratio > 3.0 || SeedingDays >= 15.0)
      # btn
      - Label in ["autoremove-btn"] && (Ratio > 3.0 || SeedingDays >= 15.0)
      # hdb
      - Label in ["autoremove-hdb"] && (Ratio > 3.0 || SeedingDays >= 15.0)
      # Qbit tag utilities
      - HasAllTags("480p", "bad-encode") # match if all tags are present
      - HasAnyTag("remove-me", "gross") # match if at least 1 tag is present
    label:
      # btn 1080p season packs to permaseed (all must evaluate to true)
      - name: permaseed-btn
        update:
          - Label == "sonarr-imported"
          - TrackerName == "landof.tv"
          - Name contains "1080p"
          - len(Files) >= 3

      # cleanup btn season packs to autoremove-btn (all must evaluate to true)
      - name: autoremove-btn
        update:
          - Label == "sonarr-imported"
          - TrackerName == "landof.tv"
          - not (Name contains "1080p")
          - len(Files) >= 3
    # Change qbit tags based on filters
    tag:
      - name: low-seed
      # This must be set
      # "mode: full" means tag will be added to
      # torrent if matched and removed from torrent if not
      # use `add` or `remove` to only add/remove respectivly
      # NOTE: Mode does not change the way torrents are flagged,
      # meaning, even with "mode: remove",
      # tags will be removed if the torrent does NOT match the conditions.
      # "mode: remove" simply means that tags will not be added
      # to torrents that do match.
        mode: full
        update:
          - Seeds <= 3

```

## Optional - Tracker Configuration

```yaml
trackers:
  bhd:
    api_key: your-api-key
  ptp:
    api_user: your-api-user
    api_key: your-api-key
  hdb:
    username: your-username
    passkey: your-passkey
  red:
    api_key: your-api-key
  ops:
    api_key: your-api-key
  unit3d:
    aither:
      api_key: your_api_key
      domain: aither.cc
    blutopia:
      api_key: your_api_key
      domain: blutopia.cc
```

Allows tqm to validate if a torrent was removed from the tracker using the tracker's own API.

Currently implements:

- Beyond-HD
- HDB
- OPS
- PTP
- RED
- UNIT3D trackers

## Filtering Language Definition

The language definition used in the configuration filters is available [here](https://github.com/antonmedv/expr/blob/586b86b462d22497d442adbc924bfb701db3075d/docs/Language-Definition.md)

## Filterable Fields

The following torrent fields (along with their types) can be used in the configuration when filtering torrents:

```go
type Torrent struct {
 Hash            string  
 Name            string  
 Path            string  
 TotalBytes      int64   
 DownloadedBytes int64   
 State           string  
 Files           []string
 Tags            []string
 Downloaded      bool    
 Seeding         bool    
 Ratio           float32 
 AddedSeconds    int64   
 AddedHours      float32 
 AddedDays       float32 
 SeedingSeconds  int64   
 SeedingHours    float32 
 SeedingDays     float32 
 Label           string  
 Seeds           int64   
 Peers           int64   

 FreeSpaceGB  func() float64 
 FreeSpaceSet bool

 TrackerName   string
 TrackerStatus string
}
```

Number fields of types `int64`, `float32` and `float64` support [arithmetic](https://github.com/antonmedv/expr/blob/586b86b462d22497d442adbc924bfb701db3075d/docs/Language-Definition.md#arithmetic-operators) and [comparison](https://github.com/antonmedv/expr/blob/586b86b462d22497d442adbc924bfb701db3075d/docs/Language-Definition.md#comparison-operators) operators.

Fields of type `string` support [string operators](https://github.com/antonmedv/expr/blob/586b86b462d22497d442adbc924bfb701db3075d/docs/Language-Definition.md#string-operators).

Fields of type `[]string` (lists) such as the `Tags` and `Files` fields support [membership checks](https://github.com/antonmedv/expr/blob/586b86b462d22497d442adbc924bfb701db3075d/docs/Language-Definition.md#membership-operators) and various [built in functions](https://github.com/antonmedv/expr/blob/586b86b462d22497d442adbc924bfb701db3075d/docs/Language-Definition.md#builtin-functions).

All of this and more can be noted in the [language definition](https://github.com/antonmedv/expr/blob/586b86b462d22497d442adbc924bfb701db3075d/docs/Language-Definition.md) mentioned above.

## Helper Filtering Options

The following helper functions are available for usage while filtering, usage examples are available in the example config above.

```go
IsUnregistered() bool     // Evaluates to true if torrent is unregistered in the tracker
IsTrackerDown() bool      // Evaluates to true if the tracker appears to be down/unreachable
HasAllTags(tags ...string) bool // True if torrent has ALL tags specified
HasAnyTag(tags ...string) bool  // True if torrent has at least one tag specified
HasMissingFiles() bool // True if any of the torrent's files are missing from disk
Log(n float64) float64    // The natural logarithm function
```

### IsUnregistered and IsTrackerDown

When using both `IsUnregistered()` and `IsTrackerDown()` in filters:

- `IsUnregistered()` has built-in protection against tracker down states - it will return `false` if the tracker is down
- `IsTrackerDown()` checks if the tracker status indicates the tracker is unreachable/down
- The functions are independent but related - a torrent can be:
  - Unregistered with tracker up (IsUnregistered: true, IsTrackerDown: false)
  - Status unknown with tracker down (IsUnregistered: false, IsTrackerDown: true)
  - Registered with tracker up (IsUnregistered: false, IsTrackerDown: false)

Note: While `IsUnregistered()` automatically handles tracker down states, you may still want to explicitly check for `IsTrackerDown()` in your ignore filters to prevent any actions when tracker status is uncertain.

Example:

```yaml
filters:
  default:
    ignore:
      - IsTrackerDown()  # Skip any actions when tracker is down
    remove:
      - IsUnregistered() # Safe to use alone due to built-in protection
```

## BypassIgnoreIfUnregistered

If the top level config option `bypassIgnoreIfUnregistered` is set to `true`, unregistered torrents will not be ignored.
This helps making the config less verbose, so this:

```yaml
filters:
  default:
    ignore:
      # general
      - IsTrackerDown()
      - Downloaded == false && !IsUnregistered()
      - SeedingHours < 26 && !IsUnregistered()
      # permaseed / un-sorted (unless torrent has been deleted)
      - Label startsWith "permaseed-" && !IsUnregistered()
      # Filter based on qbittorrent tags (only qbit at the moment)
      - '"permaseed" in Tags && !IsUnregistered()'
```

can turn into this:

```yaml
bypassIgnoreIfUnregistered: true

filters:
  default:
    ignore:
      # general
      - IsTrackerDown()
      - Downloaded == false
      - SeedingHours < 26
      # permaseed / un-sorted (unless torrent has been deleted)
      - Label startsWith "permaseed-"
      # Filter based on qbittorrent tags (only qbit at the moment)
      - '"permaseed" in Tags
```

## Supported Clients

- Deluge
- qBittorrent

## Example Commands

1. Clean - Retrieve torrent client queue and remove torrents matching its configured filters

`tqm clean qbt --dry-run`

`tqm clean qbt`

2. Relabel - Retrieve torrent client queue and relabel torrents matching its configured filters

`tqm relabel qbt --dry-run`

`tqm relabel qbt`

3. Retag - Retrieve torrent client queue and retag torrents matching its configured filters (only qbittorrent supported as of now)

`tqm retag qbt --dry-run`

`tqm retag qbt`

4. Orphan - Retrieve torrent client queue and local files/folders in download_path, remove orphan files/folders

`tqm orphan qbt --dry-run`

`tqm orphan qbt`

***

## Notes

`FreeSpaceSet` and `FreeSpaceGB()` are currently only supported for the following clients (when `free_space_path` is set):

- Deluge
- qBittorrent

`FreeSpaceGB()` will only increase as torrents are hard-removed.

This only works with one disk referenced by `free_space_path` and will not account for torrents being on **different disks**.

## regexp2 Pattern Matching

TQM uses the regexp2 library for advanced pattern matching, providing .NET style regex capabilities. This offers several advantages over Go's standard regex package:

- More powerful pattern matching features
- Compatibility with .NET style regex patterns
- Reliable Unicode support

### Available Functions

```yaml
filters:
  default:
    tag:
      # Single pattern matching
      - RegexMatch("(?i)\\bpattern\\b")
      
      # Match any of multiple patterns (comma-separated)
      - RegexMatchAny("(?i)\\bpattern1\\b, (?i)\\bpattern2\\b")
      
      # Match all patterns (comma-separated)
      - RegexMatchAll("(?i)\\bpattern1\\b, (?i)\\bpattern2\\b")
```

### Pattern Features

- Case insensitive matching with `(?i)`
- Word boundaries with `\b`
- Lookahead assertions:
  - Positive `(?=...)`
  - Negative `(?!...)`
- Lookbehind assertions:
  - Positive `(?<=...)`
  - Negative `(?<!...)`
- Unicode support
- Timeout support for preventing catastrophic backtracking

### Tips

- Each pattern can have its own case sensitivity flag:

  ```yaml
  # First pattern is case-insensitive, second is case-sensitive
  - RegexMatchAny("(?i)pattern1, pattern2")
  
  # Both patterns are case-insensitive
  - RegexMatchAny("(?i)pattern1, (?i)pattern2")
  ```

- Patterns are comma-separated
- Use word boundaries to prevent partial matches
- Lookbehind assertions must be fixed-width
