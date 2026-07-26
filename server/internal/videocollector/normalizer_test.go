package videocollector

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeMediaInfoSortsUsefulFormats(t *testing.T) {
	raw := json.RawMessage(`{
  "id": "7609535074618395925",
  "webpage_url": "https://www.tiktok.com/@wowohpanda/video/7609535074618395925",
  "title": "Test video",
  "uploader": "wowohpanda",
  "duration": 83.9,
  "extractor": "TikTok",
  "view_count": 1200,
  "like_count": 75,
  "comment_count": 9,
  "repost_count": 4,
  "thumbnails": [
    {"id":"small","url":"https://cdn.example/small.jpg","width":320,"height":180},
    {"id":"large","url":"https://cdn.example/large.jpg","width":1280,"height":720},
    {"id":"duplicate","url":"https://cdn.example/large.jpg","width":1280,"height":720}
  ],
  "subtitles": {"en":[{"name":"English","ext":"vtt","url":"https://cdn.example/en.vtt"}]},
  "automatic_captions": {"zh-Hans":[{"name":"Chinese","ext":"vtt","url":"https://cdn.example/zh.vtt"}]},
  "formats": [
    {"format_id":"audio","ext":"m4a","vcodec":"none","acodec":"aac","abr":64},
    {"format_id":"muxed","ext":"mp4","width":576,"height":1148,"vcodec":"h264","acodec":"aac","filesize":6008055},
    {"format_id":"video-1080","ext":"mp4","width":1080,"height":1920,"vcodec":"h264","acodec":"none","tbr":1800},
    {"format_id":"storyboard","ext":"mhtml","vcodec":"none","acodec":"none"}
  ]
}`)

	info, err := NormalizeMediaInfo(raw, "https://example.com/fallback")

	require.NoError(t, err)
	require.Equal(t, "7609535074618395925", info.ID)
	require.Len(t, info.Formats, 3)
	require.Equal(t, "video-1080", info.Formats[0].ID)
	require.False(t, info.Formats[0].HasAudio)
	require.Contains(t, info.Formats[0].Label, "1920p")
	require.Equal(t, "muxed", info.Formats[1].ID)
	require.True(t, info.Formats[1].HasAudio)
	require.EqualValues(t, 6008055, info.Formats[1].ApproximateBytes)
	require.Len(t, info.Images, 2)
	require.Equal(t, "large", info.Images[0].ID)
	require.Len(t, info.Subtitles, 2)
	require.False(t, info.Subtitles[0].Automatic)
	require.True(t, info.Subtitles[1].Automatic)
	require.EqualValues(t, 1200, info.Metrics.Views)
	require.EqualValues(t, 75, info.Metrics.Likes)
	require.EqualValues(t, 9, info.Metrics.Comments)
	require.EqualValues(t, 4, info.Metrics.Reposts)
}

func TestNormalizeMediaInfoAcceptsMuxedHLSWithUnknownCodecs(t *testing.T) {
	raw := json.RawMessage(`{
  "id": "48722683",
  "title": "AcFun public video",
  "extractor": "AcFunVideo",
  "formats": [
    {
      "format_id": "2",
      "ext": "mp4",
      "width": 1280,
      "height": 720,
      "vcodec": null,
      "acodec": null,
      "protocol": "m3u8_native",
      "filesize_approx": 5933793
    }
  ]
}`)

	info, err := NormalizeMediaInfo(raw, "https://www.acfun.cn/v/ac48722683")

	require.NoError(t, err)
	require.Len(t, info.Formats, 1)
	require.True(t, info.Formats[0].HasVideo)
	require.True(t, info.Formats[0].HasAudio)
	require.Contains(t, info.Formats[0].Label, "720p")
}

func TestNormalizeMediaInfoRejectsEmptyFormats(t *testing.T) {
	_, err := NormalizeMediaInfo(json.RawMessage(`{"id":"x","formats":[]}`), "https://example.com/video")
	require.Error(t, err)
}

func TestNormalizeCollectionInfoCapsPublicEntriesAtTen(t *testing.T) {
	entries := make([]map[string]any, 0, 12)
	for index := 0; index < 12; index++ {
		entries = append(entries, map[string]any{
			"id": fmt.Sprintf("item-%d", index), "title": fmt.Sprintf("Item %d", index),
			"url": fmt.Sprintf("https://example.com/watch/%d", index), "thumbnail": "https://cdn.example/cover.jpg",
			"duration": 10 + index, "view_count": 100 + index,
		})
	}
	payload, err := json.Marshal(map[string]any{"id": "creator", "title": "Creator uploads", "entries": entries})
	require.NoError(t, err)

	collection, err := NormalizeCollectionInfo(payload, "https://example.com/creator", 10)
	require.NoError(t, err)
	require.Equal(t, "Creator uploads", collection.Title)
	require.Len(t, collection.Items, 10)
	require.Equal(t, "item-0", collection.Items[0].ID)
	require.EqualValues(t, 100, collection.Items[0].Metrics.Views)
}
