package videocollector

import (
	"encoding/json"
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
