package videocollector

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrNoDownloadableFormats = errors.New("no downloadable media formats found")

type rawMediaInfo struct {
	ID                string                   `json:"id"`
	WebpageURL        string                   `json:"webpage_url"`
	OriginalURL       string                   `json:"original_url"`
	Title             string                   `json:"title"`
	Uploader          string                   `json:"uploader"`
	UploaderID        string                   `json:"uploader_id"`
	Thumbnail         string                   `json:"thumbnail"`
	Duration          float64                  `json:"duration"`
	Extractor         string                   `json:"extractor"`
	ExtractorKey      string                   `json:"extractor_key"`
	Formats           []rawMediaFormat         `json:"formats"`
	Thumbnails        []rawMediaImage          `json:"thumbnails"`
	Subtitles         map[string][]rawSubtitle `json:"subtitles"`
	AutomaticCaptions map[string][]rawSubtitle `json:"automatic_captions"`
	ViewCount         int64                    `json:"view_count"`
	LikeCount         int64                    `json:"like_count"`
	CommentCount      int64                    `json:"comment_count"`
	RepostCount       int64                    `json:"repost_count"`
}

type rawMediaImage struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Extension string `json:"ext"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type rawSubtitle struct {
	Name      string `json:"name"`
	Extension string `json:"ext"`
}

type rawCollectionInfo struct {
	ID         string               `json:"id"`
	Title      string               `json:"title"`
	Uploader   string               `json:"uploader"`
	UploaderID string               `json:"uploader_id"`
	Entries    []rawCollectionEntry `json:"entries"`
}

type rawCollectionEntry struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	WebpageURL   string  `json:"webpage_url"`
	OriginalURL  string  `json:"original_url"`
	Title        string  `json:"title"`
	Thumbnail    string  `json:"thumbnail"`
	Duration     float64 `json:"duration"`
	ViewCount    int64   `json:"view_count"`
	LikeCount    int64   `json:"like_count"`
	CommentCount int64   `json:"comment_count"`
	RepostCount  int64   `json:"repost_count"`
}

type rawMediaFormat struct {
	ID             string  `json:"format_id"`
	Extension      string  `json:"ext"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	VideoCodec     string  `json:"vcodec"`
	AudioCodec     string  `json:"acodec"`
	FileSize       float64 `json:"filesize"`
	FileSizeApprox float64 `json:"filesize_approx"`
	TotalBitrate   float64 `json:"tbr"`
	AudioBitrate   float64 `json:"abr"`
}

func NormalizeMediaInfo(payload json.RawMessage, sourceURL string) (*MediaInfo, error) {
	var raw rawMediaInfo
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, errors.New("media extractor returned invalid metadata")
	}
	formats := make([]MediaFormat, 0, len(raw.Formats))
	for _, candidate := range raw.Formats {
		videoCodec := normalizedCodec(candidate.VideoCodec)
		audioCodec := normalizedCodec(candidate.AudioCodec)
		hasDimensions := candidate.Width > 0 || candidate.Height > 0
		videoCodecUnknown := strings.TrimSpace(candidate.VideoCodec) == ""
		audioCodecUnknown := strings.TrimSpace(candidate.AudioCodec) == ""
		hasVideo := videoCodec != "" || (videoCodecUnknown && hasDimensions)
		hasAudio := audioCodec != "" || (videoCodecUnknown && audioCodecUnknown && hasDimensions)
		if !hasVideo && !hasAudio {
			continue
		}
		size := candidate.FileSize
		if size <= 0 {
			size = candidate.FileSizeApprox
		}
		bitrate := candidate.TotalBitrate
		if bitrate <= 0 {
			bitrate = candidate.AudioBitrate
		}
		format := MediaFormat{
			ID:               fallback(strings.TrimSpace(candidate.ID), "unknown"),
			Extension:        fallback(strings.TrimSpace(candidate.Extension), defaultExtension(hasVideo)),
			Width:            candidate.Width,
			Height:           candidate.Height,
			VideoCodec:       videoCodec,
			AudioCodec:       audioCodec,
			ApproximateBytes: int64(size),
			BitrateKbps:      int64(bitrate),
			HasVideo:         hasVideo,
			HasAudio:         hasAudio,
		}
		format.Label = buildFormatLabel(format)
		formats = append(formats, format)
	}
	images := normalizeImages(raw.Thumbnail, raw.Thumbnails)
	subtitles := normalizeSubtitles(raw.Subtitles, raw.AutomaticCaptions)
	if len(formats) == 0 && len(images) == 0 && len(subtitles) == 0 {
		return nil, ErrNoDownloadableFormats
	}
	sort.SliceStable(formats, func(i, j int) bool {
		left, right := formats[i], formats[j]
		if left.HasVideo != right.HasVideo {
			return left.HasVideo
		}
		if left.Height != right.Height {
			return left.Height > right.Height
		}
		if left.HasAudio != right.HasAudio {
			return left.HasAudio
		}
		return left.BitrateKbps > right.BitrateKbps
	})

	resolvedSource := firstNonEmpty(raw.WebpageURL, raw.OriginalURL, sourceURL)
	return &MediaInfo{
		ID:        fallback(strings.TrimSpace(raw.ID), "unknown"),
		SourceURL: resolvedSource,
		Title:     fallback(strings.TrimSpace(raw.Title), "Untitled video"),
		Uploader:  fallback(firstNonEmpty(raw.Uploader, raw.UploaderID), "Unknown creator"),
		Thumbnail: strings.TrimSpace(raw.Thumbnail),
		Duration:  raw.Duration,
		Extractor: fallback(firstNonEmpty(raw.Extractor, raw.ExtractorKey), "Unknown platform"),
		Formats:   formats,
		Images:    images,
		Subtitles: subtitles,
		Metrics: MediaMetrics{
			Views: raw.ViewCount, Likes: raw.LikeCount, Comments: raw.CommentCount, Reposts: raw.RepostCount,
		},
	}, nil
}

func NormalizeCollectionInfo(payload json.RawMessage, sourceURL string, limit int) (*CollectionInfo, error) {
	var raw rawCollectionInfo
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, errors.New("media extractor returned invalid collection metadata")
	}
	if limit < 1 || limit > 10 {
		limit = 10
	}
	items := make([]CollectionItem, 0, min(limit, len(raw.Entries)))
	for _, entry := range raw.Entries {
		if len(items) >= limit {
			break
		}
		entryURL := firstNonEmpty(entry.WebpageURL, entry.OriginalURL, entry.URL)
		if entryURL == "" {
			continue
		}
		items = append(items, CollectionItem{
			ID:        fallback(strings.TrimSpace(entry.ID), fmt.Sprintf("item-%d", len(items)+1)),
			SourceURL: entryURL, Title: fallback(strings.TrimSpace(entry.Title), "Untitled media"),
			Thumbnail: strings.TrimSpace(entry.Thumbnail), Duration: entry.Duration,
			Metrics: MediaMetrics{Views: entry.ViewCount, Likes: entry.LikeCount, Comments: entry.CommentCount, Reposts: entry.RepostCount},
		})
	}
	if len(items) == 0 {
		return nil, errors.New("no public collection entries found")
	}
	return &CollectionInfo{
		ID: fallback(strings.TrimSpace(raw.ID), "collection"), SourceURL: sourceURL,
		Title:    fallback(strings.TrimSpace(raw.Title), "Public collection"),
		Uploader: fallback(firstNonEmpty(raw.Uploader, raw.UploaderID), "Unknown creator"), Items: items,
	}, nil
}

func normalizeImages(fallbackURL string, candidates []rawMediaImage) []MediaImage {
	seen := make(map[string]struct{})
	images := make([]MediaImage, 0, len(candidates)+1)
	for _, candidate := range candidates {
		url := strings.TrimSpace(candidate.URL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		images = append(images, MediaImage{
			ID:  fallback(strings.TrimSpace(candidate.ID), fmt.Sprintf("image-%d", len(images)+1)),
			URL: url, Extension: strings.TrimPrefix(strings.ToLower(strings.TrimSpace(candidate.Extension)), "."),
			Width: candidate.Width, Height: candidate.Height,
		})
	}
	if fallbackURL = strings.TrimSpace(fallbackURL); fallbackURL != "" {
		if _, ok := seen[fallbackURL]; !ok {
			images = append(images, MediaImage{ID: "thumbnail", URL: fallbackURL})
		}
	}
	sort.SliceStable(images, func(i, j int) bool {
		return images[i].Width*images[i].Height > images[j].Width*images[j].Height
	})
	return images
}

func normalizeSubtitles(manual, automatic map[string][]rawSubtitle) []SubtitleTrack {
	tracks := make([]SubtitleTrack, 0, len(manual)+len(automatic))
	appendTracks := func(source map[string][]rawSubtitle, isAutomatic bool) {
		for language, candidates := range source {
			language = strings.TrimSpace(language)
			if language == "" || len(candidates) == 0 {
				continue
			}
			candidate := candidates[0]
			tracks = append(tracks, SubtitleTrack{
				Language: language, Name: strings.TrimSpace(candidate.Name),
				Extension: strings.TrimPrefix(strings.ToLower(strings.TrimSpace(candidate.Extension)), "."),
				Automatic: isAutomatic,
			})
		}
	}
	appendTracks(manual, false)
	appendTracks(automatic, true)
	sort.SliceStable(tracks, func(i, j int) bool {
		if tracks[i].Automatic != tracks[j].Automatic {
			return !tracks[i].Automatic
		}
		return tracks[i].Language < tracks[j].Language
	})
	return tracks
}

func buildFormatLabel(format MediaFormat) string {
	codecs := strings.Join(nonEmpty(format.VideoCodec, format.AudioCodec), " + ")
	container := strings.ToUpper(format.Extension)
	if !format.HasVideo {
		return strings.Join(nonEmpty("仅音频", container, codecs), " · ")
	}
	dimensions := ""
	if format.Width > 0 && format.Height > 0 {
		dimensions = fmt.Sprintf("%d×%d", format.Width, format.Height)
	}
	resolution := "视频"
	if format.Height > 0 {
		resolution = fmt.Sprintf("%dp", format.Height)
	}
	audioStatus := ""
	if !format.HasAudio {
		audioStatus = "需合并音频"
	}
	return strings.Join(nonEmpty(dimensions, resolution, container, codecs, audioStatus), " · ")
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func normalizedCodec(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "none") {
		return ""
	}
	return value
}

func defaultExtension(hasVideo bool) string {
	if hasVideo {
		return "mp4"
	}
	return "m4a"
}

func fallback(value, fallbackValue string) string {
	if value == "" {
		return fallbackValue
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
